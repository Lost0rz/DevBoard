package quota

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

// CodexBarName is the logical read-only CLI name every caller uses. The
// production exec runner replaces it with a resolved absolute path before
// spawning anything, so no call site depends on an interactive shell PATH.
const CodexBarName = "codexbar"

// ErrCodexBarCLINotFound marks the "CodexBar CLI unavailable" state. It is
// deliberately distinct from a command failure: the menu bar must be able to
// tell the user the CLI is missing instead of a generic quota failure.
var ErrCodexBarCLINotFound = errors.New("codexbar cli unavailable")

// codexBarCandidates are absolute well-known installation paths checked
// before any PATH lookup. A per-user LaunchAgent inherits launchd's minimal
// PATH (/usr/bin:/bin:/usr/sbin:/sbin), which never contains Homebrew, so
// PATH-only lookup fails exactly in the background environment that needs
// quota collection. The PATH lookup remains a last-resort fallback for
// interactive environments with custom install locations.
var codexBarCandidates = []string{"/opt/homebrew/bin/codexbar", "/usr/local/bin/codexbar"}

// ResolveCodexBarCLI resolves the read-only CodexBar CLI to an absolute,
// executable path without invoking a shell. Symlinked candidates are followed
// because Homebrew's bin entries are symlinks into Cellar.
func ResolveCodexBarCLI() (string, error) {
	return resolveCodexBarCLI(codexBarCandidates, exec.LookPath)
}

func resolveCodexBarCLI(candidates []string, lookPath func(string) (string, error)) (string, error) {
	for _, candidate := range candidates {
		if isExecutableFile(candidate) {
			return candidate, nil
		}
	}
	if found, err := lookPath(CodexBarName); err == nil && isExecutableFile(found) {
		return found, nil
	}
	return "", ErrCodexBarCLINotFound
}

func isExecutableFile(path string) bool {
	if path == "" || !filepath.IsAbs(path) {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}
