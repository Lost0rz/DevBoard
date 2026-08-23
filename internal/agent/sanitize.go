package agent

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxTaskTitleInputBytes = 8 << 10
	maxTaskTitleBytes      = 96
	maxTaskCheckpointBytes = 120
	maxAttentionTextBytes  = 160
)

var (
	credentialURLRE = regexp.MustCompile(`(?i)https?://[^\s/:@]+:[^\s/@]+@`)
	secretRE        = regexp.MustCompile(`(?i)(api[_-]?key|access[_-]?token|auth[_-]?token|password|passwd|secret|bearer)[\s"']*[:=][\s"']*\S+`)
	windowsPathRE   = regexp.MustCompile(`(?i)\b[A-Z]:\\`)
	absolutePathRE  = regexp.MustCompile(`(?:^|[\s\(\[\{\"'=:])/(?:[^\s/]+)(?:/[^\s]+)?`)
)

func truncateUTF8(s string, max int) string {
	s = strings.ToValidUTF8(s, "")
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	b := []byte(s[:max])
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return strings.TrimSpace(string(b))
}

func normalizeSingleLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(s, ""))
	return strings.Join(strings.Fields(s), " ")
}

func containsPEMOrPrivateKey(s string) bool {
	u := strings.ToUpper(s)
	if strings.Contains(u, "-----BEGIN ") && (strings.Contains(u, "PRIVATE KEY-----") || strings.Contains(u, "OPENSSH PRIVATE KEY-----")) {
		return true
	}
	return strings.Contains(u, "BEGIN RSA PRIVATE KEY") || strings.Contains(u, "BEGIN EC PRIVATE KEY")
}

func containsSecretLike(s string) bool {
	l := strings.ToLower(s)
	if credentialURLRE.MatchString(s) || secretRE.MatchString(s) {
		return true
	}
	for _, marker := range []string{"sk-", "ghp_", "github_pat_", "xoxb-", "akia"} {
		if strings.Contains(l, marker) {
			return true
		}
	}
	return false
}

func containsAbsolutePath(s string) bool {
	l := strings.ToLower(s)
	if strings.Contains(s, "/Users/") || strings.Contains(l, "/home/") || strings.Contains(l, "/private/") || strings.Contains(l, "/var/folders/") || windowsPathRE.MatchString(s) || absolutePathRE.MatchString(s) {
		return true
	}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "/") && len(line) > 1 {
			return true
		}
	}
	return false
}

func looksLikeShellOrCode(s string) bool {
	t := strings.TrimSpace(s)
	l := strings.ToLower(t)
	if strings.Contains(t, "```") || strings.Contains(t, "\x00") || strings.Contains(t, "$(") || strings.Contains(t, " && ") || strings.Contains(t, " || ") {
		return true
	}
	codePrefixes := []string{"package ", "func ", "import ", "class ", "def ", "const ", "let ", "var ", "#!/", "select ", "insert ", "update ", "delete from "}
	for _, p := range codePrefixes {
		if strings.HasPrefix(l, p) {
			return true
		}
	}
	shellPrefixes := []string{"git ", "go ", "npm ", "pnpm ", "yarn ", "python ", "python3 ", "pip ", "pip3 ", "brew ", "curl ", "wget ", "rm ", "mv ", "cp ", "bash ", "sh ", "zsh ", "make ", "docker ", "kubectl ", "sudo ", "ssh ", "scp ", "./"}
	line := strings.TrimLeft(t, "$#> %")
	ll := strings.ToLower(line)
	for _, p := range shellPrefixes {
		if strings.HasPrefix(ll, p) {
			return true
		}
	}
	return false
}

func looksLikeJSONOrLog(s string) bool {
	t := strings.TrimSpace(s)
	if (strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[")) && json.Valid([]byte(t)) {
		return true
	}
	lines := strings.Split(t, "\n")
	if len(lines) >= 3 {
		logish := 0
		for _, line := range lines {
			u := strings.ToUpper(strings.TrimSpace(line))
			if strings.HasPrefix(u, "ERROR ") || strings.HasPrefix(u, "WARN ") || strings.HasPrefix(u, "INFO ") || strings.HasPrefix(u, "DEBUG ") || strings.Contains(u, "TRACEBACK") {
				logish++
			}
		}
		if logish >= 2 {
			return true
		}
	}
	return false
}

func looksOpaqueHighEntropy(s string) bool {
	t := strings.TrimSpace(s)
	if len(t) < 48 || strings.ContainsAny(t, " \t\n") {
		return false
	}
	letters, digits, symbols := 0, 0, 0
	for _, r := range t {
		switch {
		case unicode.IsLetter(r):
			letters++
		case unicode.IsDigit(r):
			digits++
		default:
			symbols++
		}
	}
	return letters > 8 && digits > 8 && symbols > 2
}

// looksLikeIdentifier reports identifier/token-shaped text such as
// CONSTANT_CASE or snake_case names. Titles must be natural-language
// per the frozen title policy, so an identifier-shaped prompt falls
// back to the safe default instead of republishing the raw token.
func looksLikeIdentifier(s string) bool {
	return strings.Contains(s, "_")
}

func unsafeDisplayText(s string) bool {
	return containsPEMOrPrivateKey(s) || containsSecretLike(s) || containsAbsolutePath(s) || looksLikeShellOrCode(s) || looksLikeJSONOrLog(s) || looksOpaqueHighEntropy(s)
}

func deriveTaskTitle(raw string) *string {
	b := []byte(raw)
	if len(b) > maxTaskTitleInputBytes {
		b = b[:maxTaskTitleInputBytes]
	}
	candidate := strings.ToValidUTF8(string(b), "")
	if unsafeDisplayText(candidate) {
		return nil
	}
	var line string
	for _, rawLine := range strings.Split(candidate, "\n") {
		rawLine = strings.TrimSpace(rawLine)
		rawLine = strings.TrimLeft(rawLine, "#*-0123456789. )\t")
		if rawLine != "" {
			line = rawLine
			break
		}
	}
	line = normalizeSingleLine(line)
	if len(line) < 3 || unsafeDisplayText(line) || looksLikeIdentifier(line) {
		return nil
	}
	for _, sep := range []string{"。", "！", "？"} {
		if i := strings.Index(line, sep); i > 8 {
			line = line[:i+len(sep)]
			break
		}
	}
	for _, sep := range []string{". ", "! ", "? "} {
		if i := strings.Index(line, sep); i > 8 {
			line = line[:i+1]
			break
		}
	}
	line = truncateUTF8(line, maxTaskTitleBytes)
	if line == "" {
		return nil
	}
	return ptrString(line)
}

func safeChildSubject(raw string) *string {
	s := normalizeSingleLine(raw)
	if s == "" || unsafeDisplayText(s) {
		return nil
	}
	s = truncateUTF8(s, maxTaskTitleBytes)
	if s == "" {
		return nil
	}
	return ptrString(s)
}
