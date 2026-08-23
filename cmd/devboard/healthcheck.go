package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"time"
)

// healthcheckTimeout bounds the container healthcheck request (M5.5A B10:
// bounded, GET-only, no secrets).
const healthcheckTimeout = 2 * time.Second

// runHealthcheck implements `devboard healthcheck`: it GETs the /health
// endpoint of a running DevBoard server, requires HTTP 200 with
// {"status":"ok"}, and optionally validates the reported role. Exit code 0
// means healthy; any failure is a non-zero exit with a bounded, generic
// error message.
func runHealthcheck(args []string) error {
	fs := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	url := fs.String("url", "http://127.0.0.1:8787/health", "health endpoint URL")
	expectRole := fs.String("expect-role", "", "require this runtime role (node or hub)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if *expectRole != "" && *expectRole != "node" && *expectRole != "hub" {
		return fmt.Errorf("--expect-role must be node or hub")
	}

	client := &http.Client{Timeout: healthcheckTimeout}
	resp, err := client.Get(*url)
	if err != nil {
		return fmt.Errorf("healthcheck: request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck: status %d", resp.StatusCode)
	}
	var body struct {
		Status string `json:"status"`
		Role   string `json:"role"`
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 64<<10))
	if err := decoder.Decode(&body); err != nil {
		return fmt.Errorf("healthcheck: malformed response")
	}
	// A second JSON value or trailing non-whitespace makes the response
	// malformed even if the first object looked healthy.
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("healthcheck: malformed response")
	}
	if body.Status != "ok" {
		return fmt.Errorf("healthcheck: status not ok")
	}
	if *expectRole != "" && body.Role != *expectRole {
		return fmt.Errorf("healthcheck: role %q, want %q", body.Role, *expectRole)
	}
	return nil
}
