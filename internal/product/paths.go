package product

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	launchAgentLabel = "com.devboard.node"
	productName      = "DevBoard"
)

// Paths is the complete set of user-level paths managed by the PC1 product.
// Keeping these in one value makes it difficult for the installer and the
// integration manager to drift apart.
type Paths struct {
	Home             string
	SupportDir       string
	BinDir           string
	Binary           string
	Config           string
	LogDir           string
	LaunchAgentsDir  string
	LaunchAgentPlist string
	CodexDir         string
	CodexHooks       string
	CodexConfig      string
	ClaudeDir        string
	ClaudeSettings   string
}

func ResolvePaths(home string) (Paths, error) {
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return Paths{}, fmt.Errorf("resolve home directory: %w", err)
		}
	}
	home, err := filepath.Abs(home)
	if err != nil {
		return Paths{}, fmt.Errorf("resolve home directory: %w", err)
	}
	support := filepath.Join(home, "Library", "Application Support", productName)
	return Paths{
		Home:             home,
		SupportDir:       support,
		BinDir:           filepath.Join(support, "bin"),
		Binary:           filepath.Join(support, "bin", "devboard"),
		Config:           filepath.Join(support, "node.yaml"),
		LogDir:           filepath.Join(home, "Library", "Logs", productName),
		LaunchAgentsDir:  filepath.Join(home, "Library", "LaunchAgents"),
		LaunchAgentPlist: filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist"),
		CodexDir:         filepath.Join(home, ".codex"),
		CodexHooks:       filepath.Join(home, ".codex", "hooks.json"),
		CodexConfig:      filepath.Join(home, ".codex", "config.toml"),
		ClaudeDir:        filepath.Join(home, ".claude"),
		ClaudeSettings:   filepath.Join(home, ".claude", "settings.json"),
	}, nil
}

func LaunchAgentLabel() string { return launchAgentLabel }

func ensurePrivateDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return os.Chmod(path, 0o700)
}

func stableBinaryExists(paths Paths) bool {
	info, err := os.Stat(paths.Binary)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}
