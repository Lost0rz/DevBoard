package main

import "github.com/Lost0rz/DevBoard/internal/product"

func runProductCommand(args []string) (product.Result, int) {
	invalid := func() (product.Result, int) {
		return product.Result{SchemaVersion: 1, Status: "invalid_command", Message: "usage: devboard product service <install|status|restart|uninstall> | devboard product integrations status | devboard product integrations <install|remove> <codex|claude-code>"}, 1
	}
	if len(args) < 2 {
		return invalid()
	}
	switch args[0] {
	case "service":
		if len(args) != 2 {
			return invalid()
		}
		result := product.RunService(args[1])
		return result, resultCode(result)
	case "integrations":
		if len(args) == 2 && args[1] == "status" {
			result := product.RunIntegrationsStatus()
			return result, resultCode(result)
		}
		if len(args) != 3 || (args[1] != "install" && args[1] != "remove") || (args[2] != "codex" && args[2] != "claude-code") {
			return invalid()
		}
		result := product.RunIntegration(args[2], args[1])
		return result, resultCode(result)
	default:
		return invalid()
	}
}

func resultCode(result product.Result) int {
	if result.OK {
		return 0
	}
	return 1
}
