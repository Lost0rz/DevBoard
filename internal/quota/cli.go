package quota

import (
	"context"
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

// These bounded errors become identity-free SourceHealth reasons. The
// original subprocess error is intentionally not exposed because stderr may
// contain account paths or provider details.
var (
	ErrCodexBarCommandTimeout    = errors.New("codexbar command timed out")
	ErrCodexBarCommandPermission = errors.New("codexbar command permission denied")
	ErrCodexBarCommandFailed     = errors.New("codexbar command failed")
)

// codexBarCandidates are absolute well-known installation paths checked
// before any PATH lookup. A per-user LaunchAgent inherits launchd's minimal
// PATH (/usr/bin:/bin:/usr/sbin:/sbin), which never contains Homebrew, so
// PATH-only lookup fails exactly in the background environment that needs
// quota collection. The PATH lookup remains a last-resort fallback for
// interactive environments with custom install locations.
var codexBarCandidates = []string{
	"/opt/homebrew/bin/codexbar",
	"/usr/local/bin/codexbar",
	"/Applications/CodexBar.app/Contents/Helpers/CodexBarCLI",
}

// ResolveCodexBarCLI resolves the read-only CodexBar CLI to an absolute,
// executable path without invoking a shell. Symlinked candidates are followed
// because Homebrew's bin entries are symlinks into Cellar.
func ResolveCodexBarCLI() (string, error) {
	home, _ := os.UserHomeDir()
	return resolveCodexBarCLI(codexBarCandidatesForHome(home), exec.LookPath)
}

func codexBarCandidatesForHome(home string) []string {
	candidates := append([]string(nil), codexBarCandidates...)
	if home != "" {
		candidates = append(candidates,
			filepath.Join(home, "Applications", "CodexBar.app", "Contents", "Helpers", "CodexBarCLI"),
			filepath.Join(home, ".local", "bin", "codexbar"),
			filepath.Join(home, "bin", "codexbar"),
		)
	}
	return candidates
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

func codexBarErrorReason(err error) string {
	switch {
	case errors.Is(err, ErrCodexBarCLINotFound):
		return "cli_unavailable"
	case errors.Is(err, ErrCodexBarCommandTimeout):
		return "command_timeout"
	case errors.Is(err, ErrCodexBarCommandPermission):
		return "permission_denied"
	default:
		return "command_failed"
	}
}

func normalizeCodexBarCommandError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ErrCodexBarCommandTimeout
	}
	if errors.Is(err, os.ErrPermission) {
		return ErrCodexBarCommandPermission
	}
	return ErrCodexBarCommandFailed
}
