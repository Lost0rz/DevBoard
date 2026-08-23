package product

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Lost0rz/DevBoard/internal/config"
)

type operationResult struct {
	OK      bool
	Status  string
	Message string
	Data    map[string]any
}

// Result is the bounded public result used by every `devboard product ...`
// command. Data is intentionally limited to status metadata and never carries
// configuration, credentials, provider files, or logs.
type Result struct {
	SchemaVersion int            `json:"schemaVersion"`
	OK            bool           `json:"ok"`
	Status        string         `json:"status"`
	Message       string         `json:"message,omitempty"`
	Data          map[string]any `json:"data,omitempty"`
}

func resultValue(value operationResult) Result {
	return Result{SchemaVersion: 1, OK: value.OK, Status: value.Status, Message: value.Message, Data: value.Data}
}

func okResult(status, message string, data map[string]any) operationResult {
	return operationResult{OK: true, Status: status, Message: message, Data: data}
}

func errorResult(status, message string, data map[string]any) operationResult {
	return operationResult{Status: status, Message: message, Data: data}
}

// ServiceOptions contains injectable process boundaries used by the service
// manager tests. Production callers use the platform defaults.
type ServiceOptions struct {
	Paths           Paths
	Executable      string
	UserID          string
	Launchctl       func(args ...string) error
	LaunchctlOutput func(args ...string) ([]byte, error)
	ValidatePlist   func(path string) error
	Health          func(url string) error
	HealthRole      func(url, expectedRole string) error
	ListenPID       func(pid, port int) error
	Now             func() time.Time
	Sleep           func(time.Duration)
}

func defaultServiceOptions() (ServiceOptions, error) {
	paths, err := ResolvePaths("")
	if err != nil {
		return ServiceOptions{}, err
	}
	executable, err := os.Executable()
	if err != nil {
		return ServiceOptions{}, fmt.Errorf("resolve running helper: %w", err)
	}
	uid := strconv.Itoa(os.Getuid())
	return ServiceOptions{Paths: paths, Executable: executable, UserID: uid, Now: time.Now}, nil
}

func runService(action string) operationResult {
	opts, err := defaultServiceOptions()
	if err != nil {
		return errorResult("service_unavailable", "product service could not resolve its managed paths", nil)
	}
	return runServicePlatform(action, opts)
}

func RunService(action string) Result { return resultValue(runService(action)) }

func copyExecutableAtomic(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("helper source is a directory")
	}
	dir := filepath.Dir(destination)
	tmp, err := os.CreateTemp(dir, ".devboard-helper-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o755); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := io.Copy(tmp, input); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, destination); err != nil {
		return err
	}
	tmpName = ""
	return syncDirectory(filepath.Dir(destination))
}

func writeAtomic(path string, body []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(dir, ".devboard-write-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	tmpName = ""
	return syncDirectory(dir)
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func ensureNodeConfig(paths Paths) error {
	if _, err := os.Stat(paths.Config); err == nil {
		return os.Chmod(paths.Config, 0o600)
	} else if !os.IsNotExist(err) {
		return err
	}
	return config.SaveAtomic(paths.Config, config.Defaults())
}

func defaultPlist(paths Paths) []byte {
	return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key><string>%s</string>
    <key>ProgramArguments</key>
    <array><string>%s</string><string>serve</string><string>--config</string><string>%s</string></array>
    <key>RunAtLoad</key><true/>
    <key>KeepAlive</key><true/>
    <key>StandardOutPath</key><string>%s</string>
    <key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, xmlEscape(LaunchAgentLabel()), xmlEscape(paths.Binary), xmlEscape(paths.Config), xmlEscape(filepath.Join(paths.LogDir, "node.out.log")), xmlEscape(filepath.Join(paths.LogDir, "node.err.log"))))
}

func xmlEscape(value string) string {
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	value = strings.ReplaceAll(value, ">", "&gt;")
	value = strings.ReplaceAll(value, `"`, "&quot;")
	return value
}

func launchctlDefaults(opts *ServiceOptions) {
	if opts.Launchctl == nil {
		opts.Launchctl = func(args ...string) error {
			return exec.Command("/bin/launchctl", args...).Run()
		}
	}
	if opts.LaunchctlOutput == nil {
		opts.LaunchctlOutput = func(args ...string) ([]byte, error) {
			return exec.Command("/bin/launchctl", args...).Output()
		}
	}
	if opts.ValidatePlist == nil {
		opts.ValidatePlist = func(path string) error { return exec.Command("/usr/bin/plutil", "-lint", path).Run() }
	}
	if opts.HealthRole == nil {
		if opts.Health != nil {
			opts.HealthRole = func(url, _ string) error { return opts.Health(url) }
		} else {
			opts.HealthRole = defaultHealthRole
		}
	}
	if opts.Health == nil {
		opts.Health = func(url string) error { return opts.HealthRole(url, "") }
	}
	if opts.ListenPID == nil {
		opts.ListenPID = defaultListenerPID
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Sleep == nil {
		opts.Sleep = time.Sleep
	}
}

func defaultHealth(url string) error {
	return defaultHealthRole(url, "")
}

func defaultHealthRole(url, expectedRole string) error {
	client := &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health status %d", resp.StatusCode)
	}
	var body struct {
		Status string `json:"status"`
		Role   string `json:"role"`
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 64<<10))
	if err := decoder.Decode(&body); err != nil || body.Status != "ok" {
		return fmt.Errorf("health response is not ok")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("health response is malformed")
	}
	if expectedRole != "" && body.Role != expectedRole {
		return fmt.Errorf("health role %q, want %q", body.Role, expectedRole)
	}
	return nil
}

func launchDomain(opts ServiceOptions) (string, string) {
	uid := opts.UserID
	if uid == "" {
		uid = strconv.Itoa(os.Getuid())
	}
	domain := "gui/" + uid
	return domain, domain + "/" + LaunchAgentLabel()
}

func runLaunchAgent(opts ServiceOptions, restart bool) error {
	launchctlDefaults(&opts)
	domain, job := launchDomain(opts)
	if restart {
		if err := opts.Launchctl("kickstart", "-k", job); err != nil {
			return err
		}
		return nil
	}
	_ = opts.Launchctl("bootout", job)
	if err := opts.Launchctl("bootstrap", domain, opts.Paths.LaunchAgentPlist); err != nil {
		return err
	}
	return opts.Launchctl("kickstart", "-k", job)
}

func bootoutManagedLaunchAgent(opts ServiceOptions) error {
	launchctlDefaults(&opts)
	_, job := launchDomain(opts)
	if err := opts.Launchctl("bootout", job); err != nil {
		// bootout is idempotent for product purposes when the job is already
		// absent. If launchctl can still print it, however, uninstall must not
		// claim success while the managed service remains loaded.
		if _, printErr := opts.LaunchctlOutput("print", job); printErr == nil {
			return err
		}
	}
	return nil
}

var launchctlStatePattern = regexp.MustCompile(`(?m)^\s*state\s*=\s*([^\s]+)\s*$`)
var launchctlPIDPattern = regexp.MustCompile(`(?m)^\s*pid\s*=\s*([0-9]+)\s*$`)

func parseLaunchctlOwnership(output []byte) (int, error) {
	state := launchctlStatePattern.FindSubmatch(output)
	if len(state) != 2 || string(state[1]) != "running" {
		return 0, fmt.Errorf("LaunchAgent is not running")
	}
	pidMatch := launchctlPIDPattern.FindSubmatch(output)
	if len(pidMatch) != 2 {
		return 0, fmt.Errorf("LaunchAgent has no PID")
	}
	pid, err := strconv.Atoi(string(pidMatch[1]))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("LaunchAgent PID is invalid")
	}
	return pid, nil
}

func launchAgentPID(opts ServiceOptions) (int, error) {
	_, job := launchDomain(opts)
	output, err := opts.LaunchctlOutput("print", job)
	if err != nil {
		return 0, err
	}
	return parseLaunchctlOwnership(output)
}

func defaultListenerPID(pid, port int) error {
	portFilter := fmt.Sprintf("-iTCP:%d", port)
	cmd := exec.Command("/usr/sbin/lsof", "-nP", "-a", "-p", strconv.Itoa(pid), portFilter, "-sTCP:LISTEN")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("PID %d is not listening on TCP/%d", pid, port)
	}
	return nil
}

func verifyOwnedNodePID(opts ServiceOptions, pid int) (int, error) {
	if err := opts.ListenPID(pid, 8787); err != nil {
		return 0, err
	}
	if err := opts.HealthRole("http://127.0.0.1:8787/health", "node"); err != nil {
		return 0, err
	}
	afterHealthPID, err := launchAgentPID(opts)
	if err != nil {
		return 0, fmt.Errorf("LaunchAgent ownership could not be re-verified: %w", err)
	}
	if afterHealthPID != pid {
		return 0, fmt.Errorf("LaunchAgent PID changed from %d to %d during health check", pid, afterHealthPID)
	}
	if err := opts.ListenPID(afterHealthPID, 8787); err != nil {
		return 0, err
	}
	return pid, nil
}

func verifyOwnedNode(opts ServiceOptions) (int, error) {
	pid, err := launchAgentPID(opts)
	if err != nil {
		return 0, err
	}
	return verifyOwnedNodePID(opts, pid)
}

func waitForVerifiedNode(opts ServiceOptions) (int, error) {
	deadline := opts.Now().Add(10 * time.Second)
	var lastErr error
	for {
		if pid, err := verifyOwnedNode(opts); err == nil {
			return pid, nil
		} else {
			lastErr = err
		}
		if !opts.Now().Before(deadline) {
			break
		}
		opts.Sleep(200 * time.Millisecond)
	}
	return 0, fmt.Errorf("verified Node health timed out: %w", lastErr)
}

func serviceData(paths Paths, running bool, pid ...int) map[string]any {
	data := map[string]any{"serviceRunning": running, "nodeId": "", "displayName": "", "binaryPath": paths.Binary}
	if len(pid) > 0 && pid[0] > 0 {
		data["pid"] = pid[0]
	}
	return data
}

func validateJSONShape(body []byte) (map[string]any, error) {
	var value map[string]any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("json has trailing content")
	}
	if value == nil {
		return nil, fmt.Errorf("json root is not an object")
	}
	return value, nil
}
