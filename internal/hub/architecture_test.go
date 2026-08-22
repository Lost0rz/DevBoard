package hub

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestPackageDoesNotImportLegacyMultihost freezes the M5.3.1 architecture
// boundary: the push-native hub runtime must never depend on the legacy pull
// multihost model. The shared dashboard read model lives in
// internal/dashboard. This test fails the moment any file of this package
// (production or test) grows a multihost import again.
func TestPackageDoesNotImportLegacyMultihost(t *testing.T) {
	const legacy = "github.com/Lost0rz/DevBoard/internal/multihost"
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}
	found := false
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, entry.Name(), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", entry.Name(), err)
		}
		for _, imp := range file.Imports {
			if strings.Trim(imp.Path.Value, `"`) == legacy {
				t.Errorf("%s imports legacy %s", entry.Name(), legacy)
				found = true
			}
		}
	}
	if found {
		t.Fatal("hub package must depend on internal/dashboard, never on internal/multihost")
	}
}
