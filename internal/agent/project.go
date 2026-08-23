package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Lost0rz/DevBoard/internal/state"
)

type gitRunner interface {
	Run(dir string, args ...string) (string, error)
}

type execGitRunner struct{}

func (execGitRunner) Run(dir string, args ...string) (string, error) {
	argv := append([]string{"-C", dir}, args...)
	out, err := exec.Command("git", argv...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func hashIdentity(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}

func sanitizeProjectLabel(s string, max int) string {
	s = filepath.Base(strings.TrimSpace(s))
	s = normalizeSingleLine(s)
	s = strings.Trim(s, ". ")
	return truncateUTF8(s, max)
}

func resolveProjectContext(cwd string) *state.TaskProjectContext {
	return resolveProjectContextWithRunner(cwd, execGitRunner{})
}

func resolveProjectContextWithRunner(cwd string, runner gitRunner) *state.TaskProjectContext {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" || runner == nil {
		return nil
	}
	info, err := os.Stat(cwd)
	if err != nil || !info.IsDir() {
		return nil
	}
	canonical, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return nil
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return nil
	}

	root, rootErr := runner.Run(canonical, "rev-parse", "--show-toplevel")
	if rootErr != nil || strings.TrimSpace(root) == "" {
		name := sanitizeProjectLabel(canonical, 80)
		if name == "" {
			return nil
		}
		return &state.TaskProjectContext{ProjectName: name, WorktreeIdentity: hashIdentity("dir", canonical)}
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil
	}
	commonDir, commonErr := runner.Run(canonical, "rev-parse", "--git-common-dir")
	gitDir, gitErr := runner.Run(canonical, "rev-parse", "--git-dir")
	branch, _ := runner.Run(canonical, "branch", "--show-current")

	projectName := sanitizeProjectLabel(root, 80)
	if commonErr == nil && commonDir != "" {
		if !filepath.IsAbs(commonDir) {
			commonDir = filepath.Join(root, commonDir)
		}
		if abs, e := filepath.Abs(commonDir); e == nil {
			commonDir = abs
		}
		if n := sanitizeProjectLabel(filepath.Dir(commonDir), 80); n != "" {
			projectName = n
		}
	}
	if projectName == "" {
		return nil
	}
	ctx := &state.TaskProjectContext{ProjectName: projectName, Branch: truncateUTF8(normalizeSingleLine(branch), 120)}
	rootLabel := sanitizeProjectLabel(root, 80)
	if rootLabel != "" && rootLabel != projectName {
		ctx.WorktreeLabel = rootLabel
	}
	privateGit := ""
	if gitErr == nil {
		privateGit = gitDir
	}
	ctx.WorktreeIdentity = hashIdentity("git", root, commonDir, privateGit)
	return ctx
}
