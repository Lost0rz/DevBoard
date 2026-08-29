package product

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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

func TestAtomicHelperInstallFailurePreservesExistingBinary(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "devboard")
	before := []byte("working helper")
	if err := os.WriteFile(destination, before, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyExecutableAtomic(filepath.Join(dir, "missing-source"), destination); err == nil {
		t.Fatal("missing helper source unexpectedly installed")
	}
	after, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("failed helper install replaced existing binary: %q", after)
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
	if err := os.MkdirAll(paths.LaunchAgentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(paths.LaunchAgentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var calls []string
	var listenerPIDs []int
	var healthRoles []string
	opts := ServiceOptions{
		Paths:      paths,
		Executable: source,
		UserID:     "501",
		Launchctl: func(args ...string) error {
			calls = append(calls, strings.Join(args, "\x00"))
			return nil
		},
		LaunchctlOutput: func(args ...string) ([]byte, error) {
			return []byte("state = running\npid = 4242\n"), nil
		},
		ValidatePlist: func(path string) error { return nil },
		HealthRole: func(url, role string) error {
			healthRoles = append(healthRoles, role)
			return nil
		},
		ListenPID: func(pid, port int) error {
			listenerPIDs = append(listenerPIDs, pid)
			if port != 8787 {
				t.Fatalf("listener port=%d", port)
			}
			return nil
		},
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
	if got := len(healthRoles); got != 2 {
		t.Fatalf("health checks=%d want 2", got)
	}
	for _, role := range healthRoles {
		if role != "node" {
			t.Fatalf("health role=%q", role)
		}
	}
	if got := len(listenerPIDs); got != 4 {
		t.Fatalf("listener checks=%d want 4", got)
	}
	if mode := mustMode(t, paths.LaunchAgentsDir); mode != 0o755 {
		t.Fatalf("existing LaunchAgents mode changed to %o", mode)
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

func TestDarwinServiceStatusRequiresStableOwnedPIDAndListener(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("LaunchAgent behavior is Darwin-specific")
	}
	paths, err := ResolvePaths(filepath.Join(t.TempDir(), "home"))
	if err != nil {
		t.Fatal(err)
	}
	var printCalls int
	var listenerCalls []int
	var roles []string
	opts := ServiceOptions{
		Paths:  paths,
		UserID: "501",
		LaunchctlOutput: func(args ...string) ([]byte, error) {
			printCalls++
			return []byte("state = running\npid = 501\n"), nil
		},
		HealthRole: func(url, role string) error {
			roles = append(roles, role)
			return nil
		},
		ListenPID: func(pid, port int) error {
			listenerCalls = append(listenerCalls, pid)
			return nil
		},
	}
	result := runServicePlatform("status", opts)
	if !result.OK || result.Status != "healthy" {
		t.Fatalf("stable status=%+v", result)
	}
	if printCalls != 2 || len(listenerCalls) != 2 || len(roles) != 1 || roles[0] != "node" {
		t.Fatalf("ownership checks print=%d listeners=%v roles=%v", printCalls, listenerCalls, roles)
	}

	printCalls = 0
	opts.LaunchctlOutput = func(args ...string) ([]byte, error) {
		printCalls++
		if printCalls == 1 {
			return []byte("state = running\npid = 501\n"), nil
		}
		return []byte("state = running\npid = 502\n"), nil
	}
	result = runServicePlatform("status", opts)
	if result.OK || result.Status != "unhealthy" {
		t.Fatalf("changed status=%+v", result)
	}
}

func TestDarwinServiceRejectsForeignListenerBeforeHealth(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("LaunchAgent behavior is Darwin-specific")
	}
	paths, err := ResolvePaths(filepath.Join(t.TempDir(), "home"))
	if err != nil {
		t.Fatal(err)
	}
	healthCalled := false
	opts := ServiceOptions{
		Paths:  paths,
		UserID: "501",
		LaunchctlOutput: func(args ...string) ([]byte, error) {
			return []byte("state = running\npid = 701\n"), nil
		},
		ListenPID: func(pid, port int) error {
			return fmt.Errorf("PID %d does not own TCP/%d", pid, port)
		},
		HealthRole: func(url, role string) error {
			healthCalled = true
			return nil
		},
	}
	result := runServicePlatform("status", opts)
	if result.OK || result.Status != "unhealthy" {
		t.Fatalf("foreign-listener status=%+v", result)
	}
	if healthCalled {
		t.Fatal("a healthy response from a foreign listener must not be consulted after PID ownership fails")
	}
}

func TestDarwinServiceRestartWaitsForVerifiedNode(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("LaunchAgent behavior is Darwin-specific")
	}
	paths, err := ResolvePaths(filepath.Join(t.TempDir(), "home"))
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "devboard-bootstrap")
	if err := os.WriteFile(source, []byte("new helper"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.BinDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Binary, []byte("old helper"), 0o755); err != nil {
		t.Fatal(err)
	}
	var launchctlCalls []string
	opts := ServiceOptions{
		Paths:      paths,
		Executable: source,
		UserID:     "501",
		Launchctl: func(args ...string) error {
			launchctlCalls = append(launchctlCalls, strings.Join(args, "\x00"))
			return nil
		},
		LaunchctlOutput: func(args ...string) ([]byte, error) {
			return []byte("state = running\npid = 601\n"), nil
		},
		HealthRole: func(url, role string) error { return nil },
		ListenPID:  func(pid, port int) error { return nil },
	}
	result := runServicePlatform("restart", opts)
	if !result.OK || result.Status != "restarted" {
		t.Fatalf("restart result=%+v", result)
	}
	if len(launchctlCalls) != 1 || !strings.Contains(launchctlCalls[0], "kickstart") {
		t.Fatalf("restart launchctl calls=%v", launchctlCalls)
	}
	updated, err := os.ReadFile(paths.Binary)
	if err != nil {
		t.Fatal(err)
	}
	if string(updated) != "new helper" {
		t.Fatalf("restart did not update managed helper: %q", updated)
	}
}

func TestRunLaunchAgentRetriesTransientBootstrapFailure(t *testing.T) {
	var calls []string
	var bootstrapCalls int
	var sleeps int
	opts := ServiceOptions{
		Paths:  Paths{LaunchAgentPlist: "/tmp/com.devboard.node.plist"},
		UserID: "501",
		Launchctl: func(args ...string) error {
			calls = append(calls, strings.Join(args, "\x00"))
			if len(args) > 0 && args[0] == "bootstrap" {
				bootstrapCalls++
				if bootstrapCalls < 3 {
					return fmt.Errorf("label is still unloading")
				}
			}
			return nil
		},
		Sleep: func(_ time.Duration) { sleeps++ },
	}
	if err := runLaunchAgent(opts, false); err != nil {
		t.Fatalf("runLaunchAgent error=%v", err)
	}
	if bootstrapCalls != 3 || sleeps != 2 {
		t.Fatalf("bootstrap calls=%d sleeps=%d calls=%v", bootstrapCalls, sleeps, calls)
	}
	if got := calls[len(calls)-1]; !strings.Contains(got, "kickstart") {
		t.Fatalf("last launchctl call=%q, want kickstart", got)
	}
}

func TestDarwinUninstallDoesNotClaimSuccessWhenLoadedJobCannotStop(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("LaunchAgent behavior is Darwin-specific")
	}
	paths, err := ResolvePaths(filepath.Join(t.TempDir(), "home"))
	if err != nil {
		t.Fatal(err)
	}
	opts := ServiceOptions{
		Paths:  paths,
		UserID: "501",
		Launchctl: func(args ...string) error {
			return fmt.Errorf("bootout failed")
		},
		LaunchctlOutput: func(args ...string) ([]byte, error) {
			return []byte("state = running\npid = 808\n"), nil
		},
	}
	result := runServicePlatform("uninstall", opts)
	if result.OK || result.Status != "uninstall_failed" {
		t.Fatalf("uninstall result=%+v", result)
	}
}

func mustMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}
