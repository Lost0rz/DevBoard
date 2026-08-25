package product

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Lost0rz/DevBoard/internal/config"
)

// MacSetupRequest is the protected input boundary for the native setup
// window. The request is transported to the helper over stdin, never through
// process arguments. NodeToken is intentionally absent from every Result.
type MacSetupRequest struct {
	NodeID      string `json:"nodeId"`
	DisplayName string `json:"displayName"`
	HubEndpoint string `json:"hubEndpoint"`
	NodeToken   string `json:"nodeToken"`
}

type MacSetupOptions struct {
	ConfigPath      string
	Request         MacSetupRequest
	Service         func(string) operationResult
	SaveConfig      func(string, config.Config) error
	ConnectionCheck func(context.Context, config.Config) string
}

// RunMacSetupStatus returns only setup-safe metadata. It never returns the
// configured token or any credential-derived value.
func RunMacSetupStatus(configPath string) Result {
	return resultValue(runMacSetupStatus(configPath))
}

func runMacSetupStatus(configPath string) operationResult {
	path, err := macConfigPath(configPath)
	if err != nil {
		return errorResult("setup_unavailable", "Mac setup is temporarily unavailable", nil)
	}
	cfg, exists, err := loadMacConfig(path)
	if err != nil {
		return errorResult("setup_config_unreadable", "Mac setup configuration could not be read", nil)
	}
	nodeID := strings.TrimSpace(cfg.Host.ID)
	if !exists || nodeID == "" || (nodeID == "local" && !cfg.Uplink.Enabled) {
		nodeID, err = generateMacNodeID()
		if err != nil {
			return errorResult("setup_identity_unavailable", "Mac identity could not be prepared", nil)
		}
	}
	endpoint := strings.TrimSpace(cfg.Uplink.Endpoint)
	tokenConfigured := strings.TrimSpace(cfg.Uplink.Token) != ""
	configurationReady := cfg.Uplink.Enabled && nodeID != "" && endpoint != "" && tokenConfigured
	return okResult("setup_status", "Mac setup status loaded", map[string]any{
		"nodeId":             nodeID,
		"displayName":        cfg.Host.DisplayName,
		"hubEndpoint":        endpoint,
		"tokenConfigured":    tokenConfigured,
		"configurationReady": configurationReady,
		"configPresent":      exists,
	})
}

// RunMacSetup validates, atomically persists, and tests one native setup
// submission. SaveConfig and Service are injectable so tests never touch a
// real LaunchAgent or the user's product directory.
func RunMacSetup(opts MacSetupOptions) Result {
	return resultValue(runMacSetup(opts))
}

func runMacSetup(opts MacSetupOptions) operationResult {
	path, err := macConfigPath(opts.ConfigPath)
	if err != nil {
		return errorResult("setup_unavailable", "Mac setup is temporarily unavailable", nil)
	}
	cfg, exists, err := loadMacConfig(path)
	if err != nil {
		return errorResult("setup_config_unreadable", "Mac setup configuration could not be read", nil)
	}

	nodeID := strings.TrimSpace(opts.Request.NodeID)
	if nodeID == "" {
		nodeID = strings.TrimSpace(cfg.Host.ID)
	}
	if nodeID == "" || (nodeID == "local" && !cfg.Uplink.Enabled) {
		nodeID, err = generateMacNodeID()
		if err != nil {
			return errorResult("setup_identity_unavailable", "Mac identity could not be prepared", nil)
		}
	}
	existingID := strings.TrimSpace(cfg.Host.ID)
	if exists && existingID != "" && !(existingID == "local" && !cfg.Uplink.Enabled) && nodeID != existingID {
		return errorResult("identity_binding_failed", "Node ID is fixed by the existing Mac identity", nil)
	}
	if existing := strings.TrimSpace(cfg.Uplink.NodeID); existing != "" && nodeID != existing {
		return errorResult("identity_binding_failed", "Node ID does not match the configured Node identity", nil)
	}

	displayName := strings.TrimSpace(opts.Request.DisplayName)
	if displayName == "" {
		displayName = strings.TrimSpace(cfg.Host.DisplayName)
	}
	endpoint := strings.TrimSpace(opts.Request.HubEndpoint)
	if endpoint == "" {
		endpoint = strings.TrimSpace(cfg.Uplink.Endpoint)
	}
	token := strings.TrimSpace(opts.Request.NodeToken)
	if token == "" {
		// A blank SecureField on an already configured Mac means “keep the
		// current credential”; the credential is never sent back to the UI.
		token = strings.TrimSpace(cfg.Uplink.Token)
	}
	if token == "" {
		return errorResult("empty_token", "Enter a Node Token to configure this Mac", nil)
	}

	cfg.Runtime.Role = config.RuntimeRoleNode
	cfg.Host.ID = nodeID
	cfg.Host.DisplayName = displayName
	cfg.Uplink = config.UplinkConfig{Enabled: true, Endpoint: endpoint, NodeID: nodeID, Token: token}
	if err := config.Validate(cfg); err != nil {
		return errorResult("setup_invalid", "Mac setup values are invalid", nil)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return errorResult("setup_config_directory_failed", "Mac setup storage could not be prepared", nil)
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return errorResult("setup_config_directory_failed", "Mac setup storage could not be secured", nil)
	}
	if opts.SaveConfig == nil {
		opts.SaveConfig = config.SaveAtomic
	}
	if err := opts.SaveConfig(path, cfg); err != nil {
		return errorResult("setup_save_failed", "Mac setup could not be saved securely", nil)
	}

	if opts.Service == nil {
		opts.Service = runService
	}
	serviceStatus := opts.Service("status")
	action := "restart"
	if !serviceStatus.OK {
		action = "install"
	}
	service := opts.Service(action)
	if !service.OK {
		return errorResult("service_setup_failed", "Background Node could not be started", nil)
	}

	if opts.ConnectionCheck == nil {
		opts.ConnectionCheck = checkMacConnection
	}
	connection := normalizeMacConnectionStatus(opts.ConnectionCheck(context.Background(), cfg))
	data := map[string]any{
		"nodeId":      nodeID,
		"displayName": displayName,
		"connection":  connection,
	}
	switch connection {
	case "connected":
		return okResult("configured_connected", "Mac saved and connected", data)
	case "auth_failed":
		return errorResult("hub_authentication_failed", "Mac saved, but the Node Token was rejected by the Hub", data)
	case "timeout":
		return errorResult("setup_connection_timeout", "Mac saved, but the connection test timed out", data)
	default:
		return errorResult("setup_connection_failed", "Mac saved, but the Hub connection could not be verified", data)
	}
}

func macConfigPath(path string) (string, error) {
	if strings.TrimSpace(path) != "" {
		return filepath.Abs(path)
	}
	paths, err := ResolvePaths("")
	if err != nil {
		return "", err
	}
	return paths.Config, nil
}

func loadMacConfig(path string) (config.Config, bool, error) {
	cfg, err := config.Load(path)
	if err == nil {
		return cfg, true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return config.Defaults(), false, nil
	}
	return config.Config{}, false, err
}

func generateMacNodeID() (string, error) {
	var random [16]byte
	if _, err := io.ReadFull(rand.Reader, random[:]); err != nil {
		return "", err
	}
	return "mac-" + hex.EncodeToString(random[:]), nil
}

// checkMacConnection exposes only a bounded status class. It intentionally
// does not decode or return any credential-bearing response field.
func checkMacConnection(ctx context.Context, cfg config.Config) string {
	if !cfg.Uplink.Enabled || strings.TrimSpace(cfg.Uplink.Endpoint) == "" || strings.TrimSpace(cfg.Uplink.Token) == "" {
		return "degraded"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, localStatusURLForMac(cfg), nil)
	if err != nil {
		return "degraded"
	}
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || strings.Contains(strings.ToLower(err.Error()), "timeout") {
			return "timeout"
		}
		return "degraded"
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return "auth_failed"
	}
	if response.StatusCode != http.StatusOK {
		return "degraded"
	}
	var body struct {
		Connected       bool `json:"connected"`
		UplinkEnabled   bool `json:"uplinkEnabled"`
		TokenConfigured bool `json:"tokenConfigured"`
		UplinkRunning   bool `json:"uplinkRunning"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 64<<10))
	if err := decoder.Decode(&body); err != nil {
		return "degraded"
	}
	if body.Connected && body.UplinkEnabled && body.TokenConfigured && body.UplinkRunning {
		return "connected"
	}
	return "degraded"
}

func localStatusURLForMac(cfg config.Config) string {
	host := strings.Trim(strings.TrimSpace(cfg.Server.Host), "[]")
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return (&url.URL{Scheme: "http", Host: net.JoinHostPort(host, strconv.Itoa(cfg.Server.Port)), Path: "/api/node/status"}).String()
}

func normalizeMacConnectionStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "connected", "complete", "healthy":
		return "connected"
	case "auth_failed", "unauthorized", "forbidden", "wrong_token":
		return "auth_failed"
	case "timeout", "timed_out":
		return "timeout"
	default:
		return "degraded"
	}
}
