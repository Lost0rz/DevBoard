package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// mu serializes config saves process-wide so concurrent writers targeting the
// same path never interleave their temp-file renames.
var mu sync.Mutex

// SaveAtomic validates cfg, renders it as the complete canonical YAML the
// strict loader understands, and installs it at path atomically: the payload
// is written to a mode-0600 temporary file in the same directory, fsynced,
// and renamed over the destination, so a crash can never leave a partially
// written config behind. Config contents — including any node bearer token —
// are never logged here.
func SaveAtomic(path string, cfg Config) error {
	return saveAtomic(path, cfg, syncDirectory)
}

func saveAtomic(path string, cfg Config, syncDir func(string) error) error {
	if err := Validate(cfg); err != nil {
		return fmt.Errorf("config invalid: %w", err)
	}
	body := render(cfg)
	dir := filepath.Dir(path)
	mu.Lock()
	defer mu.Unlock()
	tmp, err := os.CreateTemp(dir, ".devboard-config-*")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp config: %w", err)
	}
	if _, err := tmp.WriteString(body); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("install config: %w", err)
	}
	tmpName = "" // renamed: the deferred cleanup must not remove the destination
	// Persist the directory entry as well as the file contents. The rename is
	// already atomic; syncing its parent closes the final crash-durability gap
	// on the Linux/macOS filesystems used for dogfood.
	// Once rename succeeds, the new destination is the committed config. A
	// directory-sync failure may reduce crash durability, but returning an
	// error here would falsely tell the caller that the config was not
	// installed and could cause a duplicate or contradictory retry.
	_ = syncDir(dir)
	return nil
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// render writes the complete canonical config. Every active section is
// emitted with plain `key: value` lines (the strict loader does not strip
// inline comments) and quoted string scalars, so Load(SaveAtomic(cfg)) is
// guaranteed to roundtrip for any validated config.
func render(cfg Config) string {
	var b strings.Builder
	q := strconv.Quote

	b.WriteString("runtime:\n")
	b.WriteString("  role: " + q(string(cfg.Runtime.Role)) + "\n")
	b.WriteString("server:\n")
	b.WriteString("  host: " + q(cfg.Server.Host) + "\n")
	b.WriteString("  port: " + strconv.Itoa(cfg.Server.Port) + "\n")
	b.WriteString("host:\n")
	b.WriteString("  id: " + q(cfg.Host.ID) + "\n")
	b.WriteString("  display_name: " + q(cfg.Host.DisplayName) + "\n")
	b.WriteString("display:\n")
	b.WriteString("  dashboard_refresh_seconds: " + strconv.Itoa(cfg.Display.DashboardRefreshSeconds) + "\n")
	b.WriteString("  kindle_refresh_seconds: " + strconv.Itoa(cfg.Display.KindleRefreshSeconds) + "\n")
	b.WriteString("  complete_high_visibility_seconds: " + strconv.Itoa(cfg.Display.CompleteHighVisibilitySeconds) + "\n")
	b.WriteString("  complete_retention_seconds: " + strconv.Itoa(cfg.Display.CompleteRetentionSeconds) + "\n")
	b.WriteString("agent:\n")
	b.WriteString("  stale_after_seconds: " + strconv.Itoa(cfg.Agent.StaleAfterSeconds) + "\n")
	b.WriteString("network:\n")
	b.WriteString("  probe_address: " + q(cfg.Network.ProbeAddress) + "\n")
	b.WriteString("  probe_timeout_milliseconds: " + strconv.Itoa(cfg.Network.ProbeTimeoutMilliseconds) + "\n")
	b.WriteString("multi_host:\n")
	b.WriteString("  enabled: " + strconv.FormatBool(cfg.MultiHost.Enabled) + "\n")
	b.WriteString("  peers: " + q(renderPeers(cfg.MultiHost.Peers)) + "\n")
	b.WriteString("nodes:\n")
	b.WriteString("  registered: " + q(renderNodes(cfg.Nodes.Registered)) + "\n")
	b.WriteString("  disabled: " + q(strings.Join(cfg.Nodes.Disabled, ",")) + "\n")
	b.WriteString("uplink:\n")
	b.WriteString("  enabled: " + strconv.FormatBool(cfg.Uplink.Enabled) + "\n")
	b.WriteString("  endpoint: " + q(cfg.Uplink.Endpoint) + "\n")
	b.WriteString("  node_id: " + q(cfg.Uplink.NodeID) + "\n")
	b.WriteString("  token: " + q(cfg.Uplink.Token) + "\n")
	b.WriteString("admin:\n")
	b.WriteString("  enabled: " + strconv.FormatBool(cfg.Admin.Enabled) + "\n")
	b.WriteString("  token_file: " + q(cfg.Admin.TokenFile) + "\n")
	return b.String()
}

func renderPeers(peers []PeerConfig) string {
	if len(peers) == 0 {
		return ""
	}
	parts := make([]string, 0, len(peers))
	for _, peer := range peers {
		parts = append(parts, peer.ExpectedHostID+"="+peer.Endpoint)
	}
	return strings.Join(parts, ",")
}

func renderNodes(nodes []NodeConfig) string {
	if len(nodes) == 0 {
		return ""
	}
	parts := make([]string, 0, len(nodes))
	for _, node := range nodes {
		parts = append(parts, node.NodeID+"="+node.DisplayName+"="+node.Token)
	}
	return strings.Join(parts, ",")
}
