package main

import (
	"encoding/json"
	"fmt"
	"io"
)

// buildMetadata is intentionally limited to provenance. It contains no
// filesystem paths, runtime configuration, credentials, or host data.
type buildMetadata struct {
	SchemaVersion  int    `json:"schemaVersion"`
	ProductVersion string `json:"productVersion"`
	GitCommit      string `json:"gitCommit"`
}

func writeVersionMetadata(w io.Writer, args []string) error {
	if len(args) != 1 || args[0] != "--json" {
		return fmt.Errorf("usage: devboard version --json")
	}
	return json.NewEncoder(w).Encode(buildMetadata{
		SchemaVersion:  1,
		ProductVersion: productVersion,
		GitCommit:      gitCommit,
	})
}
