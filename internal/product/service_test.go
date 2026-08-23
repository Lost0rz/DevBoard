package product

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNonDarwinServiceIsBoundedUnsupported(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("Darwin uses the LaunchAgent implementation")
	}
	result := runServicePlatform("install", ServiceOptions{})
	if result.OK || result.Status != "unsupported_platform" {
		t.Fatalf("result=%+v", result)
	}
}

func TestDarwinServiceInstallPreservesConfigAndUsesLaunchctlArgv(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("LaunchAgent behavior is Darwin-specific")
	}
	paths, err := ResolvePaths(filepath.Join(t.TempDir(), "home with spaces"))
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "devboard-bootstrap")
	if err := os.WriteFile(source, []byte("helper"), 0o755); err != nil {
		t.Fatal(err)
	}
	var calls []string
	opts := ServiceOptions{
		Paths:      paths,
		Executable: source,
		UserID:     "501",
		Launchctl: func(args ...string) error {
			calls = append(calls, strings.Join(args, "\x00"))
			return nil
		},
		ValidatePlist: func(path string) error { return nil },
		Health:        func(url string) error { return nil },
	}
	result := runServicePlatform("install", opts)
	if !result.OK || result.Status != "installed" {
		t.Fatalf("install result=%+v", result)
	}
	configBody := []byte("runtime:\n  role: \"node\"\n")
	if err := os.WriteFile(paths.Config, configBody, 0o600); err != nil {
		t.Fatal(err)
	}
	result = runServicePlatform("install", opts)
	if !result.OK {
		t.Fatalf("reinstall result=%+v", result)
	}
	gotConfig, err := os.ReadFile(paths.Config)
	if err != nil || string(gotConfig) != string(configBody) {
		t.Fatalf("config changed on reinstall: %q err=%v", gotConfig, err)
	}
	plist, err := os.ReadFile(paths.LaunchAgentPlist)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{paths.Binary, paths.Config, "<true/>", "serve", "--config"} {
		if !strings.Contains(string(plist), want) {
			t.Fatalf("plist missing %q: %s", want, plist)
		}
	}
	if len(calls) < 6 || !strings.Contains(calls[0], "bootout") || !strings.Contains(calls[1], "bootstrap") || !strings.Contains(calls[2], "kickstart") {
		t.Fatalf("launchctl argv calls=%v", calls)
	}
	if err := os.WriteFile(filepath.Join(paths.LogDir, "node.out.log"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	result = runServicePlatform("uninstall", opts)
	if !result.OK {
		t.Fatalf("uninstall result=%+v", result)
	}
	if _, err := os.Stat(paths.Config); err != nil {
		t.Fatalf("uninstall removed config: %v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.LogDir, "node.out.log")); err != nil {
		t.Fatalf("uninstall removed logs: %v", err)
	}
}
