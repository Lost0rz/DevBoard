package quota

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/state"
)

// The LaunchAgent environment gets launchd's minimal PATH, which does not
// include Homebrew. Resolution must therefore work from absolute well-known
// candidates alone, and never depend on an interactive shell PATH.
func TestResolveCodexBarCLIUsesAbsoluteCandidateWithoutPATHLookup(t *testing.T) {
	candidate := filepath.Join(t.TempDir(), "codexbar")
	writeTestExecutable(t, candidate)
	lookPathUsed := false
	got, err := resolveCodexBarCLI([]string{candidate}, func(string) (string, error) {
		lookPathUsed = true
		return "", exec.ErrNotFound
	})
	if err != nil || got != candidate {
		t.Fatalf("resolve=%q err=%v", got, err)
	}
	if lookPathUsed {
		t.Fatal("an absolute executable candidate must win without any PATH lookup")
	}
}

func TestResolveCodexBarCLIFollowsHomebrewStyleSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "Cellar", "codexbar", "0.54.0", "bin", "codexbar")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestExecutable(t, target)
	link := filepath.Join(dir, "codexbar")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	got, err := resolveCodexBarCLI([]string{link}, func(string) (string, error) { return "", exec.ErrNotFound })
	if err != nil || got != link {
		t.Fatalf("resolve=%q err=%v", got, err)
	}
}

func TestResolveCodexBarCLIRejectsUnsafeCandidates(t *testing.T) {
	dir := t.TempDir()
	directory := filepath.Join(dir, "sub")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	nonExecutable := filepath.Join(dir, "plain")
	if err := os.WriteFile(nonExecutable, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing")
	got, err := resolveCodexBarCLI([]string{"codexbar", directory, nonExecutable, missing}, func(string) (string, error) {
		return "", exec.ErrNotFound
	})
	if err == nil || got != "" {
		t.Fatalf("unsafe candidates must not resolve: got=%q err=%v", got, err)
	}
}

func TestResolveCodexBarCLIFallsBackToAbsoluteLookPathResult(t *testing.T) {
	found := filepath.Join(t.TempDir(), "codexbar")
	writeTestExecutable(t, found)
	got, err := resolveCodexBarCLI(nil, func(string) (string, error) { return found, nil })
	if err != nil || got != found {
		t.Fatalf("resolve=%q err=%v", got, err)
	}
}

func TestResolveCodexBarCLIRejectsRelativeLookPathResult(t *testing.T) {
	got, err := resolveCodexBarCLI(nil, func(string) (string, error) { return "codexbar", nil })
	if err == nil || got != "" {
		t.Fatalf("relative lookPath result must be rejected: got=%q err=%v", got, err)
	}
}

func TestCodexBarCandidatesCoverAppBundleAndUserInstallations(t *testing.T) {
	candidates := codexBarCandidatesForHome("/Users/tester")
	for _, want := range []string{
		"/Applications/CodexBar.app/Contents/Helpers/CodexBarCLI",
		"/Users/tester/Applications/CodexBar.app/Contents/Helpers/CodexBarCLI",
		"/Users/tester/.local/bin/codexbar",
		"/Users/tester/bin/codexbar",
	} {
		found := false
		for _, candidate := range candidates {
			if candidate == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("candidate list missing %q: %v", want, candidates)
		}
	}
	if strings.Contains(strings.Join(candidates, "\x00"), "relative") {
		t.Fatal("candidate list must contain only absolute paths")
	}
}

func TestCodexBarCommandErrorsStayBoundedAndMachineReadable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := normalizeCodexBarCommandError(ctx, os.ErrPermission); got != ErrCodexBarCommandPermission {
		t.Fatalf("permission error=%v", got)
	}
	if got := codexBarErrorReason(ErrCodexBarCommandTimeout); got != "command_timeout" {
		t.Fatalf("timeout reason=%q", got)
	}
	if got := codexBarErrorReason(ErrCodexBarCommandFailed); got != "command_failed" {
		t.Fatalf("command reason=%q", got)
	}
}

// Real-machine acceptance: with the PATH stripped to the launchd default
// (/usr/bin:/bin:/usr/sbin:/sbin), a Homebrew CodexBar installation must still
// resolve. Skipped on machines without a well-known installation so the
// hermetic suite stays green anywhere.
func TestResolveCodexBarCLIWorksUnderLaunchAgentMinimalPath(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin:/usr/sbin:/sbin")
	installed := false
	for _, candidate := range codexBarCandidates {
		if isExecutableFile(candidate) {
			installed = true
			break
		}
	}
	if !installed {
		t.Skip("no well-known codexbar candidate installed on this machine")
	}
	got, err := ResolveCodexBarCLI()
	if err != nil || !filepath.IsAbs(got) || !isExecutableFile(got) {
		t.Fatalf("minimal-PATH resolve=%q err=%v", got, err)
	}
}

func writeTestExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// TestCodexBarRealCollectionUnderLaunchAgentMinimalPath is the real-machine
// LaunchAgent acceptance run: with PATH stripped to launchd's default, the
// production exec runner must still collect live sanitized quota from the
// locally installed CodexBar. The probe is strictly read-only, uses a
// throwaway in-memory identity key (never the product identity), and skips
// unless the frozen Mac A account profile (1-2 Codex accounts plus GLM) is
// visible. Assertions cover counts and statuses only — no identity-bearing
// value is printed.
func TestCodexBarRealCollectionUnderLaunchAgentMinimalPath(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin:/usr/sbin:/sbin")
	if _, err := ResolveCodexBarCLI(); err != nil {
		t.Skip("codexbar cli not installed on this machine")
	}
	key := make([]byte, IdentityKeyBytes)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	detection, err := DetectAccounts(context.Background(), DefaultRunner(), key, map[string]string{})
	if err != nil {
		t.Skipf("codexbar detection unavailable: %v", err)
	}
	codexKeys := []string{}
	glmCount := 0
	for _, account := range detection.Accounts {
		switch account.Provider {
		case "codex":
			codexKeys = append(codexKeys, account.AccountKey)
		case "zai":
			glmCount++
		}
	}
	if len(codexKeys) == 0 || len(codexKeys) > 2 || glmCount == 0 {
		t.Skipf("frozen Mac A quota profile not visible (codex=%d glm=%d)", len(codexKeys), glmCount)
	}

	// Deterministic throwaway aliases: sorted test-local keys get Codex A/B.
	sort.Strings(codexKeys)
	aliases := map[string]string{}
	for index, accountKey := range codexKeys {
		if index == 0 {
			aliases[accountKey] = "Codex A"
		} else {
			aliases[accountKey] = "Codex B"
		}
	}
	now := time.Now().UTC()
	store := state.NewStore(state.LiveInitialState(now, state.HostState{ID: "minimal-path-test", DisplayName: "Test"}))
	collector := NewCollector(store, "minimal-path-test", key, nil)
	collector.SetAliases(aliases)
	if err := collector.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	root := store.Snapshot()
	if root.Sources["quota"].Status != state.SourceAvailable {
		t.Fatalf("real collection under minimal PATH left quota source=%s reason=%s", root.Sources["quota"].Status, root.Sources["quota"].Reason)
	}
	if len(root.Quota) != len(codexKeys)+glmCount {
		t.Fatalf("collected rows=%d want=%d", len(root.Quota), len(codexKeys)+glmCount)
	}
	labels := map[string]int{}
	for _, item := range root.Quota {
		switch {
		case item.Provider == "codex" && (item.DisplayLabel == "Codex A" || item.DisplayLabel == "Codex B"):
		case item.Provider == "zai" && item.DisplayLabel == "GLM":
		default:
			t.Fatalf("unexpected provider/label combination for provider %q", item.Provider)
		}
		labels[item.DisplayLabel]++
		if item.Windows == nil || len(*item.Windows) == 0 || item.SampledAt == nil {
			t.Fatalf("provider %q observation missing sanitized windows/sampling", item.Provider)
		}
	}
	if labels["Codex A"] != 1 || labels["Codex B"] != len(codexKeys)-1 || labels["GLM"] != glmCount {
		t.Fatalf("label coverage mismatch: %v", labels)
	}
	public := state.ProjectPublic(root, state.RuntimeCapabilities{}, state.ProjectionConfig{}, now)
	encoded, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) == 0 {
		t.Fatal("empty public projection")
	}
}
