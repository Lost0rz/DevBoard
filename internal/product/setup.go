package product

// RunSetup performs the repeatable post-install configuration workflow used
// by the packaged macOS application. It deliberately stops short of claiming
// that Codex Desktop is observable: command hooks are a CLI integration, and
// the Desktop app does not expose the CLI /hooks trust browser.
func RunSetup() Result {
	return resultValue(runSetup(runService, runIntegration))
}

func runSetup(
	service func(string) operationResult,
	integration func(string, string) operationResult,
) operationResult {
	serviceResult := service("install")
	data := map[string]any{"service": resultValue(serviceResult)}
	if !serviceResult.OK {
		return errorResult("service_setup_failed", "background Node setup failed; provider configuration was not changed", data)
	}

	codex := integration(integrationCodex, "install")
	claude := integration(integrationClaude, "install")
	data["codex"] = resultValue(codex)
	data["claude-code"] = resultValue(claude)

	if !codex.OK || !claude.OK {
		status := "integration_setup_failed"
		if !codex.OK && codex.Status != "" {
			status = codex.Status
		} else if !claude.OK && claude.Status != "" {
			status = claude.Status
		}
		return errorResult(status, "background Node is installed, but one or more provider integrations require attention", data)
	}

	data["nextAction"] = "configure_codex_surface"
	return okResult(
		"setup_complete_requires_trust",
		"Automated Node and provider-file setup is complete. CLI hooks still require CLI trust; Codex Desktop requires the local session observer.",
		data,
	)
}
