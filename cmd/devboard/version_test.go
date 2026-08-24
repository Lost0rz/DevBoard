package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestVersionJSONReportsOnlyBuildMetadata(t *testing.T) {
	oldVersion, oldCommit := productVersion, gitCommit
	productVersion, gitCommit = "0.1.0-test", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	t.Cleanup(func() { productVersion, gitCommit = oldVersion, oldCommit })
	var output bytes.Buffer
	if err := writeVersionMetadata(&output, []string{"--json"}); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["schemaVersion"] != float64(1) || got["productVersion"] != "0.1.0-test" || got["gitCommit"] != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("metadata=%v", got)
	}
	if len(got) != 3 {
		t.Fatalf("metadata contains unexpected fields: %v", got)
	}
}

func TestVersionJSONRequiresExplicitJSONFlag(t *testing.T) {
	if err := writeVersionMetadata(&bytes.Buffer{}, nil); err == nil {
		t.Fatal("version command accepted missing --json")
	}
}
