package uplink

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestProductionFilesKeepNodeBoundary freezes the M5.4 architecture boundary:
// the node-side uplink runtime depends only on the shared state projection.
// Production files must never import the hub receiver runtime, the legacy
// pull multihost model or the web/dashboard layer; internal/hub is allowed in
// tests only, where the frozen receiver verifies wire compatibility.
func TestProductionFilesKeepNodeBoundary(t *testing.T) {
	forbiddenInProduction := map[string]bool{
		"github.com/Lost0rz/DevBoard/internal/hub":       true,
		"github.com/Lost0rz/DevBoard/internal/multihost": true,
		"github.com/Lost0rz/DevBoard/internal/web":       true,
		"github.com/Lost0rz/DevBoard/internal/dashboard": true,
	}
	forbiddenEverywhere := map[string]bool{
		"github.com/Lost0rz/DevBoard/internal/multihost": true,
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		isTest := strings.HasSuffix(entry.Name(), "_test.go")
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, entry.Name(), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", entry.Name(), err)
		}
		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if forbiddenEverywhere[path] {
				t.Errorf("%s imports forbidden %s", entry.Name(), path)
			}
			if !isTest && forbiddenInProduction[path] {
				t.Errorf("%s (production) imports node-boundary violation %s", entry.Name(), path)
			}
		}
	}
}
