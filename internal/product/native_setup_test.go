package product

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Lost0rz/DevBoard/internal/config"
)

const nativeSetupToken = "abcdefghijklmnopqrstuvwxyz012345"

func TestMacSetupFirstInstallIsAtomicAndStartsLaunchAgent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Library", "Application Support", "DevBoard", "node.yaml")
	var actions []string
	result := runMacSetup(MacSetupOptions{
		ConfigPath: path,
		Request:    MacSetupRequest{NodeID: "mac-0123456789abcdef0123456789abcdef", DisplayName: "Studio Mac", HubEndpoint: "http://nas.local", NodeToken: nativeSetupToken},
		Service: func(action string) operationResult {
			actions = append(actions, action)
			if action == "status" {
				return errorResult("not_running", "not running", nil)
			}
			return okResult("installed", "installed", nil)
		},
		ConnectionCheck: func(context.Context, config.Config) string { return "connected" },
	})
	if !result.OK || result.Status != "configured_connected" {
		t.Fatalf("result=%+v", result)
	}
	if strings.Contains(string(mustJSON(resultValue(result))), nativeSetupToken) {
		t.Fatal("native setup result leaked Node token")
	}
	if strings.Join(actions, ",") != "status,install" {
		t.Fatalf("actions=%v", actions)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host.ID != "mac-0123456789abcdef0123456789abcdef" || cfg.Host.DisplayName != "Studio Mac" || cfg.Uplink.Token != nativeSetupToken {
		t.Fatalf("saved config=%+v", cfg)
	}
	if mode := mustNativeMode(t, filepath.Dir(path)); mode != 0o700 {
		t.Fatalf("config directory mode=%o", mode)
	}
	if mode := mustNativeMode(t, path); mode != 0o600 {
		t.Fatalf("config mode=%o", mode)
	}
}

func TestMacSetupDisplayNameChangePreservesConfiguredToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "node.yaml")
	initial := config.Defaults()
	initial.Host.ID = "mac-0123456789abcdef0123456789abcdef"
	initial.Host.DisplayName = "Old Name"
	initial.Uplink = config.UplinkConfig{Enabled: true, Endpoint: "http://nas.local", NodeID: initial.Host.ID, Token: nativeSetupToken}
	if err := config.SaveAtomic(path, initial); err != nil {
		t.Fatal(err)
	}
	var restart bool
	result := runMacSetup(MacSetupOptions{
		ConfigPath: path,
		Request:    MacSetupRequest{NodeID: initial.Host.ID, DisplayName: "New Name", HubEndpoint: "http://nas.local"},
		Service: func(action string) operationResult {
			if action == "restart" {
				restart = true
			}
			if action == "status" {
				return okResult("healthy", "healthy", nil)
			}
			return okResult("restarted", "restarted", nil)
		},
		ConnectionCheck: func(context.Context, config.Config) string { return "connected" },
	})
	if !result.OK || !restart {
		t.Fatalf("result=%+v restart=%v", result, restart)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host.DisplayName != "New Name" || cfg.Host.ID != initial.Host.ID || cfg.Uplink.Token != nativeSetupToken {
		t.Fatalf("saved config=%+v", cfg)
	}
}

func TestMacSetupRejectsEmptyTokenOnFirstInstall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.yaml")
	serviceCalled := false
	result := runMacSetup(MacSetupOptions{
		ConfigPath: path,
		Request:    MacSetupRequest{NodeID: "mac-0123456789abcdef0123456789abcdef", DisplayName: "Mac", HubEndpoint: "http://nas.local"},
		Service:    func(string) operationResult { serviceCalled = true; return okResult("ok", "", nil) },
	})
	if result.OK || result.Status != "empty_token" || serviceCalled {
		t.Fatalf("result=%+v serviceCalled=%v", result, serviceCalled)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("empty token wrote config: %v", err)
	}
}

func TestMacSetupRejectsNodeIDIdentityChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.yaml")
	cfg := config.Defaults()
	cfg.Host.ID = "mac-0123456789abcdef0123456789abcdef"
	cfg.Uplink = config.UplinkConfig{Enabled: true, Endpoint: "http://nas.local", NodeID: cfg.Host.ID, Token: nativeSetupToken}
	if err := config.SaveAtomic(path, cfg); err != nil {
		t.Fatal(err)
	}
	result := runMacSetup(MacSetupOptions{
		ConfigPath: path,
		Request:    MacSetupRequest{NodeID: "mac-fedcba9876543210fedcba9876543210", DisplayName: "Mac", HubEndpoint: "http://nas.local", NodeToken: nativeSetupToken},
		Service:    func(string) operationResult { t.Fatal("identity change called service"); return operationResult{} },
	})
	if result.OK || result.Status != "identity_binding_failed" {
		t.Fatalf("result=%+v", result)
	}
}

func TestMacSetupWrongTokenAndTimeoutAreBoundedSafeFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status string
		want   string
	}{
		{name: "wrong token", status: "auth_failed", want: "hub_authentication_failed"},
		{name: "timeout", status: "timeout", want: "setup_connection_timeout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "node.yaml")
			result := runMacSetup(MacSetupOptions{
				ConfigPath: path,
				Request:    MacSetupRequest{NodeID: "mac-0123456789abcdef0123456789abcdef", DisplayName: "Mac", HubEndpoint: "http://nas.local", NodeToken: nativeSetupToken},
				Service: func(action string) operationResult {
					if action == "status" {
						return errorResult("not_running", "not running", nil)
					}
					return okResult("installed", "installed", nil)
				},
				ConnectionCheck: func(context.Context, config.Config) string { return tc.status },
			})
			if result.OK || result.Status != tc.want {
				t.Fatalf("result=%+v", result)
			}
			if strings.Contains(string(mustJSON(resultValue(result))), nativeSetupToken) {
				t.Fatal("failure result leaked Node token")
			}
		})
	}
}

func TestMacSetupStatusGeneratesReadOnlyIdentityWithoutWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.yaml")
	result := runMacSetupStatus(path)
	if !result.OK || result.Status != "setup_status" {
		t.Fatalf("result=%+v", result)
	}
	data, ok := result.Data["nodeId"].(string)
	if !ok || !strings.HasPrefix(data, "mac-") || len(data) != len("mac-")+32 {
		t.Fatalf("generated node id=%v", result.Data["nodeId"])
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("status wrote config: %v", err)
	}
}

func TestMacSetupStatusNeverReturnsConfiguredToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.yaml")
	cfg := config.Defaults()
	cfg.Host.ID = "mac-0123456789abcdef0123456789abcdef"
	cfg.Uplink = config.UplinkConfig{Enabled: true, Endpoint: "http://nas.local", NodeID: cfg.Host.ID, Token: nativeSetupToken}
	if err := config.SaveAtomic(path, cfg); err != nil {
		t.Fatal(err)
	}
	result := RunMacSetupStatus(path)
	if !result.OK || result.Data["tokenConfigured"] != true {
		t.Fatalf("result=%+v", result)
	}
	if strings.Contains(string(mustJSON(result)), nativeSetupToken) {
		t.Fatal("status result leaked configured token")
	}
}

func TestMacSetupRequestJSONDoesNotExposeTokenInResult(t *testing.T) {
	request := MacSetupRequest{NodeID: "mac-a", DisplayName: "Mac", HubEndpoint: "http://nas.local", NodeToken: nativeSetupToken}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), nativeSetupToken) {
		t.Fatal("request fixture did not contain token")
	}
	result := resultValue(errorResult("empty_token", "safe", nil))
	if strings.Contains(string(mustJSON(result)), nativeSetupToken) {
		t.Fatal("result contained token")
	}
}

func TestMacConnectionMapsAuthConnectedAndTimeoutToSafeClasses(t *testing.T) {
	baseConfig := func(port int) config.Config {
		cfg := config.Defaults()
		cfg.Uplink = config.UplinkConfig{Enabled: true, Endpoint: "http://nas.local", NodeID: cfg.Host.ID, Token: nativeSetupToken}
		cfg.Server.Host = "127.0.0.1"
		cfg.Server.Port = port
		return cfg
	}

	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	authPort := authServer.Listener.Addr().(*net.TCPAddr).Port
	if got := checkMacConnection(context.Background(), baseConfig(authPort)); got != "auth_failed" {
		t.Fatalf("auth status=%q", got)
	}
	authServer.Close()

	connectedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"connected":true,"uplinkEnabled":true,"tokenConfigured":true,"uplinkRunning":true}`))
	}))
	connectedPort := connectedServer.Listener.Addr().(*net.TCPAddr).Port
	if got := checkMacConnection(context.Background(), baseConfig(connectedPort)); got != "connected" {
		t.Fatalf("connected status=%q", got)
	}
	connectedServer.Close()

	timeoutServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	timeoutPort := timeoutServer.Listener.Addr().(*net.TCPAddr).Port
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if got := checkMacConnection(ctx, baseConfig(timeoutPort)); got != "timeout" {
		t.Fatalf("timeout status=%q", got)
	}
	timeoutServer.Close()
}

func mustNativeMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}
