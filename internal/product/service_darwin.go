//go:build darwin

package product

import "os"

func runServicePlatform(action string, opts ServiceOptions) operationResult {
	if opts.Paths.Binary == "" || opts.Paths.Config == "" || opts.Paths.LaunchAgentPlist == "" {
		return errorResult("service_unavailable", "product service paths are unavailable", nil)
	}
	launchctlDefaults(&opts)
	switch action {
	case "install":
		for _, dir := range []string{opts.Paths.SupportDir, opts.Paths.BinDir, opts.Paths.LogDir, opts.Paths.LaunchAgentsDir} {
			if err := ensurePrivateDir(dir); err != nil {
				return errorResult("install_failed", "could not create private product directories", nil)
			}
		}
		if opts.Executable == "" {
			return errorResult("install_failed", "running product helper is unavailable", nil)
		}
		if err := copyExecutableAtomic(opts.Executable, opts.Paths.Binary); err != nil {
			return errorResult("install_failed", "could not install the background helper", nil)
		}
		if err := ensureNodeConfig(opts.Paths); err != nil {
			return errorResult("install_failed", "could not create the node configuration", nil)
		}
		if err := writeAtomic(opts.Paths.LaunchAgentPlist, defaultPlist(opts.Paths), 0o600); err != nil {
			return errorResult("install_failed", "could not write the LaunchAgent", nil)
		}
		if err := opts.ValidatePlist(opts.Paths.LaunchAgentPlist); err != nil {
			return errorResult("install_failed", "LaunchAgent validation failed", nil)
		}
		if err := runLaunchAgent(opts, false); err != nil {
			return errorResult("install_failed", "could not start the per-user LaunchAgent", nil)
		}
		if err := boundedLaunchAgentHealth(opts); err != nil {
			return errorResult("install_failed", "the background Node did not become healthy", nil)
		}
		return okResult("installed", "background Node installed and healthy", serviceData(opts.Paths, true))
	case "restart":
		if err := runLaunchAgent(opts, true); err != nil {
			return errorResult("restart_failed", "could not restart the per-user LaunchAgent", nil)
		}
		return okResult("restarted", "background Node restarted", serviceData(opts.Paths, true))
	case "status":
		_, job := launchDomain(opts)
		_, err := opts.LaunchctlOutput("print", job)
		if err != nil {
			return errorResult("not_running", "background Node is not running", serviceData(opts.Paths, false))
		}
		if err := opts.Health("http://127.0.0.1:8787/health"); err != nil {
			return errorResult("unhealthy", "background Node is registered but not healthy", serviceData(opts.Paths, false))
		}
		return okResult("healthy", "background Node is healthy", serviceData(opts.Paths, true))
	case "uninstall":
		_, job := launchDomain(opts)
		_ = opts.Launchctl("bootout", job)
		for _, path := range []string{opts.Paths.LaunchAgentPlist, opts.Paths.Binary} {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return errorResult("uninstall_failed", "could not remove the managed Node files", nil)
			}
		}
		return okResult("uninstalled", "background Node removed; configuration and logs preserved", serviceData(opts.Paths, false))
	default:
		return errorResult("invalid_command", "unsupported product service action", nil)
	}
}
