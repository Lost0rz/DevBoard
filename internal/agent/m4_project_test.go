package agent

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	argv := append([]string{"-C", dir}, args...)
	out, err := exec.Command("git", argv...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func makeGitRepo(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	runGit(t, d, "init", "-b", "main")
	runGit(t, d, "config", "user.name", "DevBoard Test")
	runGit(t, d, "config", "user.email", "devboard@example.invalid")
	if err := os.WriteFile(filepath.Join(d, "README.md"), []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, d, "add", "README.md")
	runGit(t, d, "commit", "-m", "fixture")
	return d
}

func TestM4ProjectResolverGitWorktreeSymlinkDetachedAndFallback(t *testing.T) {
	repo := makeGitRepo(t)
	main := resolveProjectContext(repo)
	if main == nil || main.ProjectName != filepath.Base(repo) || main.Branch != "main" || main.WorktreeIdentity == "" {
		t.Fatalf("main context=%+v", main)
	}
	if strings.Contains(main.ProjectName, string(filepath.Separator)+"tmp") || len(main.ProjectName) > 80 || len(main.Branch) > 120 {
		t.Fatalf("unsafe public context=%+v", main)
	}

	wt := filepath.Join(t.TempDir(), "linked-worktree")
	runGit(t, repo, "worktree", "add", "-b", "feature/m4", wt)
	linked := resolveProjectContext(wt)
	if linked == nil || linked.ProjectName != filepath.Base(repo) || linked.WorktreeLabel != filepath.Base(wt) || linked.Branch != "feature/m4" || linked.WorktreeIdentity == main.WorktreeIdentity {
		t.Fatalf("linked context=%+v main=%+v", linked, main)
	}

	symlink := filepath.Join(t.TempDir(), "repo-link")
	if err := os.Symlink(wt, symlink); err != nil {
		t.Fatal(err)
	}
	viaLink := resolveProjectContext(symlink)
	if viaLink == nil || viaLink.WorktreeIdentity != linked.WorktreeIdentity || viaLink.ProjectName != linked.ProjectName {
		t.Fatalf("symlink context=%+v linked=%+v", viaLink, linked)
	}

	runGit(t, wt, "checkout", "--detach", "HEAD")
	detached := resolveProjectContext(wt)
	if detached == nil || detached.Branch != "" || detached.WorktreeIdentity != linked.WorktreeIdentity {
		t.Fatalf("detached context=%+v", detached)
	}

	nonGit := t.TempDir()
	fallback := resolveProjectContext(nonGit)
	if fallback == nil || fallback.ProjectName != filepath.Base(nonGit) || fallback.Branch != "" || fallback.WorktreeIdentity == "" {
		t.Fatalf("non-git context=%+v", fallback)
	}

	deleted := filepath.Join(t.TempDir(), "gone")
	if err := os.Mkdir(deleted, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(deleted); err != nil {
		t.Fatal(err)
	}
	if got := resolveProjectContext(deleted); got != nil {
		t.Fatalf("deleted cwd unexpectedly resolved: %+v", got)
	}
}

type recordingGitRunner struct {
	calls [][]string
}

func (r *recordingGitRunner) Run(dir string, args ...string) (string, error) {
	call := append([]string{dir}, args...)
	r.calls = append(r.calls, call)
	switch strings.Join(args, " ") {
	case "rev-parse --show-toplevel":
		return dir, nil
	case "rev-parse --git-common-dir":
		return filepath.Join(dir, ".git"), nil
	case "rev-parse --git-dir":
		return filepath.Join(dir, ".git"), nil
	case "branch --show-current":
		return "main", nil
	default:
		return "", errors.New("unexpected argv")
	}
}

func TestM4ProjectResolverUsesDirectGitArgv(t *testing.T) {
	d := t.TempDir()
	r := &recordingGitRunner{}
	ctx := resolveProjectContextWithRunner(d, r)
	if ctx == nil {
		t.Fatal("missing context")
	}
	want := [][]string{
		{d, "rev-parse", "--show-toplevel"},
		{d, "rev-parse", "--git-common-dir"},
		{d, "rev-parse", "--git-dir"},
		{d, "branch", "--show-current"},
	}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("git argv calls=%#v want=%#v", r.calls, want)
	}
}
