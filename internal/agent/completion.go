package agent

import (
	"regexp"
	"strings"
)

const (
	maxCompletionInputBytes = 16 << 10
	maxCompletionLineBytes  = 160
	maxCompletionBytes      = 320
	maxResultIdentifier     = 96
)

var (
	shaRE    = regexp.MustCompile(`(?i)\b[0-9a-f]{7,40}\b`)
	branchRE = regexp.MustCompile(`(?i)\bbranch\s*[:=]\s*([A-Za-z0-9._/-]{1,80})`)
)

func safeCompletionLine(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" || unsafeDisplayText(line) {
		return false
	}
	u := strings.ToUpper(line)
	if strings.Contains(u, "TRACEBACK") || strings.Contains(u, "STACK TRACE") || strings.Contains(u, "GOROUTINE ") || strings.Contains(line, "\t") {
		return false
	}
	return completionLineUseful(line)
}

func completionLineUseful(line string) bool {
	l := strings.ToLower(line)
	for _, marker := range []string{
		"implemented", "completed", "complete", "finished", "done", "fixed", "resolved",
		"validated", "validation", "test", "tests", "build", "lint", "vet", "pass", "fail",
		"blocked", "blocker", "limitation", "remaining", "unable", "cannot", "could not",
		"audit", "published", "committed", "commit", "branch", "result", "no blocking",
	} {
		if strings.Contains(l, marker) {
			return true
		}
	}
	return shaRE.MatchString(line) || branchRE.MatchString(line)
}

func deriveCompletion(raw string) (*string, *string) {
	b := []byte(raw)
	if len(b) > maxCompletionInputBytes {
		b = b[:maxCompletionInputBytes]
	}
	s := strings.ToValidUTF8(string(b), "")
	if containsPEMOrPrivateKey(s) || containsSecretLike(s) {
		return nil, nil
	}
	var selected []string
	seen := map[string]bool{}
	for _, rawLine := range strings.Split(s, "\n") {
		line := normalizeSingleLine(rawLine)
		if !safeCompletionLine(line) {
			continue
		}
		line = truncateUTF8(line, maxCompletionLineBytes)
		if line == "" || seen[line] {
			continue
		}
		selected = append(selected, line)
		seen[line] = true
		if len(selected) == 3 {
			break
		}
	}
	var summary *string
	if len(selected) > 0 {
		joined := truncateUTF8(strings.Join(selected, "\n"), maxCompletionBytes)
		if joined != "" {
			summary = ptrString(joined)
		}
	}
	var identifier *string
	if m := shaRE.FindString(s); m != "" {
		identifier = ptrString(truncateUTF8(m, maxResultIdentifier))
	} else if m := branchRE.FindStringSubmatch(s); len(m) == 2 && !containsAbsolutePath(m[1]) {
		identifier = ptrString(truncateUTF8(m[1], maxResultIdentifier))
	}
	return summary, identifier
}
