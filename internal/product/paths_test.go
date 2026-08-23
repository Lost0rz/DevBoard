package product

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePathsUsesStableManagedLocations(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home with spaces")
	paths, err := ResolvePaths(home)
	if err != nil {
		t.Fatal(err)
	}
	if paths.Binary != filepath.Join(home, "Library", "Application Support", "DevBoard", "bin", "devboard") {
		t.Fatalf("binary=%q", paths.Binary)
	}
	if paths.LaunchAgentPlist != filepath.Join(home, "Library", "LaunchAgents", "com.devboard.node.plist") {
		t.Fatalf("plist=%q", paths.LaunchAgentPlist)
	}
	if got := shellQuote(paths.Binary); got != "'"+paths.Binary+"'" {
		t.Fatalf("quoted path=%q", got)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatal("path resolution should not create user directories")
	}
}
