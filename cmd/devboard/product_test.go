package main

import "testing"

func TestProductCommandUsesBoundedResultAndFailureCode(t *testing.T) {
	result, code := runProductCommand([]string{"service", "invalid"})
	if code != 1 || result.OK || result.SchemaVersion != 1 || result.Status != "invalid_command" {
		t.Fatalf("result=%+v code=%d", result, code)
	}
}
