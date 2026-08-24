package main

import (
	"flag"
	"os"
	"strings"

	"github.com/Lost0rz/DevBoard/internal/product"
)

func validProductServiceAction(action string) bool {
	switch action {
	case "install", "status", "restart", "uninstall":
		return true
	default:
		return false
	}
}

func runProductCommand(args []string) (product.Result, int) {
	invalid := func() (product.Result, int) {
		return product.Result{SchemaVersion: 1, Status: "invalid_command", Message: "usage: devboard product setup | devboard product node onboard [--config PATH] [--node-id ID] [--display-name NAME] [--hub-endpoint URL] [--node-token-file PATH|--admin-token-file PATH] [--quota-identity-key-file PATH] [--quota-alias-file PATH] [--check|--dry-run] | devboard product service <install|status|restart|uninstall> | devboard product integrations status | devboard product integrations <install|remove> <codex|claude-code>"}, 1
	}
	if len(args) == 0 {
		return invalid()
	}
	switch args[0] {
	case "setup":
		if len(args) != 1 {
			return invalid()
		}
		result := product.RunSetup()
		return result, resultCode(result)
	case "node":
		if len(args) < 2 || args[1] != "onboard" {
			return invalid()
		}
		fs := flag.NewFlagSet("product node onboard", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		configPath := fs.String("config", "", "local Node config path")
		nodeID := fs.String("node-id", "", "stable Node ID")
		displayName := fs.String("display-name", "", "safe Pad display name")
		hubEndpoint := fs.String("hub-endpoint", "", "NAS Hub base URL")
		nodeTokenFile := fs.String("node-token-file", "", "0600 file containing a pre-provisioned Node token")
		adminTokenFile := fs.String("admin-token-file", "", "0600 file containing the Hub admin secret")
		quotaIdentityKeyFile := fs.String("quota-identity-key-file", "", "absolute path to an existing mode-0600 shared quota identity key")
		quotaAliasFile := fs.String("quota-alias-file", "", "absolute path to an existing mode-0600 quota account alias map")
		dryRun := fs.Bool("dry-run", false, "plan without writing files or contacting the Hub")
		check := fs.Bool("check", false, "check existing installation without writing files")
		if err := fs.Parse(args[2:]); err != nil || fs.NArg() != 0 || (*dryRun && *check) {
			return invalid()
		}
		read := func(path string) (string, error) {
			if strings.TrimSpace(path) == "" {
				return "", nil
			}
			return product.ReadSecretFileForCLI(path)
		}
		nodeToken, err := read(*nodeTokenFile)
		if err != nil {
			return product.Result{SchemaVersion: 1, Status: "secret_file_unreadable", Message: err.Error()}, 1
		}
		adminToken, err := read(*adminTokenFile)
		if err != nil {
			return product.Result{SchemaVersion: 1, Status: "secret_file_unreadable", Message: err.Error()}, 1
		}
		result := product.RunNodeOnboarding(product.OnboardingOptions{
			ConfigPath: *configPath, NodeID: *nodeID, DisplayName: *displayName,
			HubEndpoint: *hubEndpoint, NodeToken: nodeToken, AdminToken: adminToken,
			QuotaIdentityKeyFile: *quotaIdentityKeyFile, QuotaAliasFile: *quotaAliasFile,
			DryRun: *dryRun, Check: *check,
		})
		return result, resultCode(result)
	case "service":
		if len(args) != 2 || !validProductServiceAction(args[1]) {
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
