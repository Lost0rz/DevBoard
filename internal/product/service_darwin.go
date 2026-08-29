//go:build darwin

package product

import (
	"fmt"
	"os"
)

func ensureLaunchAgentsDir(path string) error {
	info, err := os.Stat(path)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("LaunchAgents path is not a directory")
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	return os.MkdirAll(path, 0o700)
}

func runServicePlatform(action string, opts ServiceOptions) operationResult {
	if opts.Paths.Binary == "" || opts.Paths.Config == "" || opts.Paths.LaunchAgentPlist == "" {
		return errorResult("service_unavailable", "product service paths are unavailable", nil)
	}
	launchctlDefaults(&opts)
	switch action {
	case "install":
		for _, dir := range []string{opts.Paths.SupportDir, opts.Paths.BinDir, opts.Paths.LogDir} {
			if err := ensurePrivateDir(dir); err != nil {
				return errorResult("install_failed", "could not create private product directories", nil)
			}
		}
		if err := ensureLaunchAgentsDir(opts.Paths.LaunchAgentsDir); err != nil {
			return errorResult("install_failed", "could not access the user LaunchAgents directory", nil)
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
		pid, err := waitForVerifiedNode(opts)
		if err != nil {
			return errorResult("install_failed", "the background Node did not become healthy", nil)
		}
		return okResult("installed", "background Node installed and healthy", serviceData(opts.Paths, true, pid))
	case "restart":
		// A restart can be triggered by the newly installed macOS App while
		// the LaunchAgent still points at the helper copied by an older DMG.
		// Synchronize the managed executable before kickstarting the job so a
		// newer config cannot be handed back to an incompatible old Node.
		// This is atomic and also works when the helper is already the managed
		// binary (the source file remains open while its replacement is made).
		if opts.Executable != "" {
			if err := copyExecutableAtomic(opts.Executable, opts.Paths.Binary); err != nil {
				return errorResult("restart_failed", "could not update the managed background helper", nil)
			}
		}
		if err := runLaunchAgent(opts, true); err != nil {
			return errorResult("restart_failed", "could not restart the per-user LaunchAgent", nil)
		}
		pid, err := waitForVerifiedNode(opts)
		if err != nil {
			return errorResult("restart_failed", "the restarted Node did not become a verified healthy owned Node", nil)
		}
		return okResult("restarted", "background Node restarted and is healthy", serviceData(opts.Paths, true, pid))
	case "status":
		pid, err := launchAgentPID(opts)
		if err != nil {
			return errorResult("not_running", "background Node is not running", serviceData(opts.Paths, false))
		}
		if _, err := verifyOwnedNodePID(opts, pid); err != nil {
			return errorResult("unhealthy", "background Node is not a verified healthy owned Node", serviceData(opts.Paths, false))
		}
		return okResult("healthy", "background Node is healthy and owned by the LaunchAgent", serviceData(opts.Paths, true, pid))
	case "uninstall":
		if err := bootoutManagedLaunchAgent(opts); err != nil {
			return errorResult("uninstall_failed", "could not stop the managed per-user LaunchAgent", nil)
		}
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
