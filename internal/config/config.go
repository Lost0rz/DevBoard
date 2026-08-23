package config

import (
	"bufio"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const maxProbeTimeoutMilliseconds = 60000

// Node bearer credentials (M5.2 §9): tokens are generated out of band from
// at least 32 cryptographically random bytes. Both sides of the wire — the
// hub nodes registry and the node uplink — share one length invariant and one
// character grammar, so a Node can never configure a credential the Hub
// registry would reject.
const (
	nodeTokenMinLength = 32
	nodeTokenMaxLength = 128
)

// validNodeTokenCharset enforces the shared opaque-token grammar: ASCII
// letters, digits, '.', '_', '~', '+' and '-' only.
func validNodeTokenCharset(token string) bool {
	for i := 0; i < len(token); i++ {
		c := token[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.' || c == '_' || c == '~' || c == '+' || c == '-':
		default:
			return false
		}
	}
	return true
}

type RuntimeRole string

const (
	RuntimeRoleNode RuntimeRole = "node"
	RuntimeRoleHub  RuntimeRole = "hub"
)

type Config struct {
	Runtime   RuntimeConfig
	Server    ServerConfig
	Host      HostConfig
	Display   DisplayConfig
	Agent     AgentConfig
	Network   NetworkConfig
	MultiHost MultiHostConfig
	Nodes     NodesConfig
	Uplink    UplinkConfig
	Admin     AdminConfig
}

type RuntimeConfig struct {
	Role RuntimeRole
}

type ServerConfig struct {
	Host string
	Port int
}

type HostConfig struct {
	ID          string
	DisplayName string
}

type DisplayConfig struct {
	DashboardRefreshSeconds       int
	KindleRefreshSeconds          int
	CompleteHighVisibilitySeconds int
	CompleteRetentionSeconds      int
}

type AgentConfig struct {
	StaleAfterSeconds int
}

type NetworkConfig struct {
	ProbeAddress             string
	ProbeTimeoutMilliseconds int
}

type MultiHostConfig struct {
	Enabled bool
	Peers   []PeerConfig
}

type PeerConfig struct {
	ExpectedHostID string
	Endpoint       string
}

// NodesConfig is the M5.3 hub-side push node registry. It is hub-only
// authority: a NODE runtime must not configure a node registry.
type NodesConfig struct {
	Registered []NodeConfig
	Disabled   []string
}

// NodeConfig is one configured registered node. Enabled is derived from the
// optional nodes.disabled list. The Token is an opaque per-node bearer
// credential; it is never exposed through read APIs.
type NodeConfig struct {
	NodeID      string
	DisplayName string
	Token       string
}

// UplinkConfig is the M5.4 node-side push configuration. It is node-only
// authority: it never reuses the hub-side nodes registry, and the hub role
// must not configure an uplink. Endpoint is the hub base address; NodeID must
// equal host.id so the M5.2 identity binding (envelope nodeId ==
// state.host.id) holds; Token is the per-node bearer credential registered in
// the hub's nodes registry.
type UplinkConfig struct {
	Enabled  bool
	Endpoint string
	NodeID   string
	Token    string
}

// AdminConfig is the M5.5A hub-only admin surface. The admin secret itself is
// NEVER stored in the YAML: it lives in the referenced mode-0600 token file
// so config saves and log output can never carry it.
type AdminConfig struct {
	Enabled   bool
	TokenFile string
}

func Defaults() Config {
	return Config{
		Runtime: RuntimeConfig{Role: RuntimeRoleNode},
		Server:  ServerConfig{Host: "127.0.0.1", Port: 8787},
		Host:    HostConfig{ID: "local", DisplayName: "Local Mac"},
		Display: DisplayConfig{
			DashboardRefreshSeconds:       2,
			KindleRefreshSeconds:          20,
			CompleteHighVisibilitySeconds: 600,
			CompleteRetentionSeconds:      1800,
		},
		Agent:     AgentConfig{StaleAfterSeconds: 900},
		Network:   NetworkConfig{ProbeAddress: "1.1.1.1:443", ProbeTimeoutMilliseconds: 1500},
		MultiHost: MultiHostConfig{Enabled: false},
		Nodes:     NodesConfig{},
		Uplink:    UplinkConfig{Enabled: false},
		Admin:     AdminConfig{Enabled: false},
	}
}

func Load(path string) (Config, error) {
	cfg := Defaults()
	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	var section string
	s := bufio.NewScanner(f)
	lineNo := 0
	for s.Scan() {
		lineNo++
		raw := strings.TrimSpace(s.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		if !strings.Contains(raw, ":") {
			return Config{}, fmt.Errorf("config line %d: expected key: value", lineNo)
		}
		if strings.HasSuffix(raw, ":") {
			section = strings.TrimSuffix(raw, ":")
			switch section {
			case "runtime", "server", "host", "display", "agent", "network", "multi_host", "nodes", "uplink", "admin":
			default:
				return Config{}, fmt.Errorf("config line %d: unsupported section %q", lineNo, section)
			}
			continue
		}
		parts := strings.SplitN(raw, ":", 2)
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if section == "" {
			return Config{}, fmt.Errorf("config line %d: key %q has no section", lineNo, key)
		}
		parsed, err := scalar(value)
		if err != nil {
			return Config{}, fmt.Errorf("config line %d: %w", lineNo, err)
		}
		if err := apply(&cfg, section, key, parsed); err != nil {
			return Config{}, fmt.Errorf("config line %d: %w", lineNo, err)
		}
	}
	if err := s.Err(); err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	if err := Validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func scalar(v string) (string, error) {
	if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
		if v[0] == '\'' {
			return v[1 : len(v)-1], nil
		}
		u, err := strconv.Unquote(v)
		if err != nil {
			return "", fmt.Errorf("invalid quoted value: %w", err)
		}
		return u, nil
	}
	return v, nil
}

func apply(cfg *Config, section, key, value string) error {
	toInt := func() (int, error) {
		n, err := strconv.Atoi(value)
		if err != nil {
			return 0, fmt.Errorf("%s.%s must be an integer", section, key)
		}
		return n, nil
	}

	switch section + "." + key {
	case "runtime.role":
		cfg.Runtime.Role = RuntimeRole(strings.ToLower(strings.TrimSpace(value)))
	case "server.host":
		cfg.Server.Host = value
	case "server.port":
		n, err := toInt()
		if err != nil {
			return err
		}
		cfg.Server.Port = n
	case "host.id":
		cfg.Host.ID = value
	case "host.display_name":
		cfg.Host.DisplayName = value
	case "display.dashboard_refresh_seconds":
		n, err := toInt()
		if err != nil {
			return err
		}
		cfg.Display.DashboardRefreshSeconds = n
	case "display.kindle_refresh_seconds":
		n, err := toInt()
		if err != nil {
			return err
		}
		cfg.Display.KindleRefreshSeconds = n
	case "display.complete_high_visibility_seconds":
		n, err := toInt()
		if err != nil {
			return err
		}
		cfg.Display.CompleteHighVisibilitySeconds = n
	case "display.complete_retention_seconds":
		n, err := toInt()
		if err != nil {
			return err
		}
		cfg.Display.CompleteRetentionSeconds = n
	case "agent.stale_after_seconds":
		n, err := toInt()
		if err != nil {
			return err
		}
		cfg.Agent.StaleAfterSeconds = n
	case "network.probe_address":
		cfg.Network.ProbeAddress = value
	case "network.probe_timeout_milliseconds":
		n, err := toInt()
		if err != nil {
			return err
		}
		cfg.Network.ProbeTimeoutMilliseconds = n
	case "multi_host.enabled":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("multi_host.enabled must be true or false")
		}
		cfg.MultiHost.Enabled = v
	case "multi_host.peers":
		peers, err := parsePeers(value)
		if err != nil {
			return err
		}
		cfg.MultiHost.Peers = peers
	case "nodes.registered":
		nodes, err := parseNodes(value)
		if err != nil {
			return err
		}
		cfg.Nodes.Registered = nodes
	case "nodes.disabled":
		ids, err := parseIDList(value)
		if err != nil {
			return err
		}
		cfg.Nodes.Disabled = ids
	case "uplink.enabled":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("uplink.enabled must be true or false")
		}
		cfg.Uplink.Enabled = v
	case "uplink.endpoint":
		cfg.Uplink.Endpoint = value
	case "uplink.node_id":
		cfg.Uplink.NodeID = value
	case "uplink.token":
		cfg.Uplink.Token = value
	case "admin.enabled":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("admin.enabled must be true or false")
		}
		cfg.Admin.Enabled = v
	case "admin.token_file":
		cfg.Admin.TokenFile = value
	default:
		return fmt.Errorf("unsupported key %s.%s", section, key)
	}
	return nil
}

func parsePeers(value string) ([]PeerConfig, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	peers := make([]PeerConfig, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 || strings.TrimSpace(kv[0]) == "" || strings.TrimSpace(kv[1]) == "" {
			return nil, fmt.Errorf("multi_host.peers entries must be expected_host_id=ip:port")
		}
		peers = append(peers, PeerConfig{ExpectedHostID: strings.TrimSpace(kv[0]), Endpoint: strings.TrimSpace(kv[1])})
	}
	return peers, nil
}

// parseNodes parses the M5.3 hub registry line. Entries are
// node_id=display_name=token, comma separated; display_name may be empty so
// the node id becomes the dashboard label.
func parseNodes(value string) ([]NodeConfig, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	nodes := make([]NodeConfig, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		fields := strings.Split(part, "=")
		if len(fields) != 3 {
			return nil, fmt.Errorf("nodes.registered entries must be node_id=display_name=token")
		}
		node := NodeConfig{
			NodeID:      strings.TrimSpace(fields[0]),
			DisplayName: strings.TrimSpace(fields[1]),
			Token:       fields[2],
		}
		if node.NodeID == "" || node.Token == "" {
			return nil, fmt.Errorf("nodes.registered entries must be node_id=display_name=token")
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func parseIDList(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	ids := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("id list entries must not be empty")
		}
		ids = append(ids, part)
	}
	return ids, nil
}

func ParsePeerEndpoint(endpoint string) (netip.AddrPort, error) {
	endpoint = strings.TrimSpace(endpoint)
	if strings.ContainsAny(endpoint, "/?#@") || strings.Contains(endpoint, "://") {
		return netip.AddrPort{}, fmt.Errorf("peer endpoint must be an IP literal plus port")
	}
	addrPort, err := netip.ParseAddrPort(endpoint)
	if err != nil || !addrPort.IsValid() || addrPort.Port() == 0 {
		return netip.AddrPort{}, fmt.Errorf("peer endpoint must be an IP literal plus port")
	}
	addr := addrPort.Addr().Unmap()
	if !allowedPeerAddr(addr) {
		return netip.AddrPort{}, fmt.Errorf("peer endpoint must use an allowed private address")
	}
	return netip.AddrPortFrom(addr, addrPort.Port()), nil
}

func allowedPeerAddr(addr netip.Addr) bool {
	if !addr.IsValid() || addr.IsLoopback() || addr.IsUnspecified() || addr.IsMulticast() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() {
		return false
	}
	if addr.Is4() {
		return netip.MustParsePrefix("10.0.0.0/8").Contains(addr) ||
			netip.MustParsePrefix("172.16.0.0/12").Contains(addr) ||
			netip.MustParsePrefix("192.168.0.0/16").Contains(addr) ||
			netip.MustParsePrefix("100.64.0.0/10").Contains(addr)
	}
	return netip.MustParsePrefix("fc00::/7").Contains(addr)
}

func validHostID(id string) bool {
	if len(id) < 1 || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func validProbeHost(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
	host = strings.TrimSuffix(host, ".")
	if host == "" || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
				continue
			}
			return false
		}
	}
	return true
}

func Validate(cfg Config) error {
	if cfg.Runtime.Role != RuntimeRoleNode && cfg.Runtime.Role != RuntimeRoleHub {
		return fmt.Errorf("runtime.role must be node or hub")
	}
	if strings.TrimSpace(cfg.Server.Host) == "" {
		return fmt.Errorf("server.host must not be empty")
	}
	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
		return fmt.Errorf("server.port must be between 1 and 65535")
	}
	if cfg.Display.DashboardRefreshSeconds < 1 || cfg.Display.DashboardRefreshSeconds > 2 {
		return fmt.Errorf("display.dashboard_refresh_seconds must be between 1 and 2")
	}
	if cfg.MultiHost.Enabled {
		return fmt.Errorf("multi_host.enabled=true is superseded; set runtime.role to hub")
	}

	if cfg.Runtime.Role == RuntimeRoleNode {
		if len(cfg.MultiHost.Peers) != 0 {
			return fmt.Errorf("multi_host.peers requires runtime.role hub")
		}
		if strings.TrimSpace(cfg.Host.ID) == "" {
			return fmt.Errorf("host.id must not be empty")
		}
		if cfg.Display.KindleRefreshSeconds <= 0 {
			return fmt.Errorf("display.kindle_refresh_seconds must be positive")
		}
		if cfg.Display.CompleteHighVisibilitySeconds < 0 {
			return fmt.Errorf("display.complete_high_visibility_seconds must be non-negative")
		}
		if cfg.Display.CompleteRetentionSeconds < cfg.Display.CompleteHighVisibilitySeconds {
			return fmt.Errorf("display.complete_retention_seconds must be >= complete_high_visibility_seconds")
		}
		if cfg.Agent.StaleAfterSeconds <= 0 {
			return fmt.Errorf("agent.stale_after_seconds must be positive")
		}
		host, port, err := net.SplitHostPort(strings.TrimSpace(cfg.Network.ProbeAddress))
		if err != nil || !validProbeHost(host) {
			return fmt.Errorf("network.probe_address must be a valid host:port")
		}
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("network.probe_address must use a port between 1 and 65535")
		}
		if cfg.Network.ProbeTimeoutMilliseconds <= 0 || cfg.Network.ProbeTimeoutMilliseconds > maxProbeTimeoutMilliseconds {
			return fmt.Errorf("network.probe_timeout_milliseconds must be between 1 and %d", maxProbeTimeoutMilliseconds)
		}
	}

	seenIDs := make(map[string]struct{}, len(cfg.MultiHost.Peers))
	seenEndpoints := make(map[string]struct{}, len(cfg.MultiHost.Peers))
	for _, peer := range cfg.MultiHost.Peers {
		if !validHostID(peer.ExpectedHostID) {
			return fmt.Errorf("multi_host peer host id %q is invalid", peer.ExpectedHostID)
		}
		if _, ok := seenIDs[peer.ExpectedHostID]; ok {
			return fmt.Errorf("duplicate multi_host peer host id %q", peer.ExpectedHostID)
		}
		seenIDs[peer.ExpectedHostID] = struct{}{}
		addrPort, err := ParsePeerEndpoint(peer.Endpoint)
		if err != nil {
			return fmt.Errorf("multi_host peer %q: %w", peer.ExpectedHostID, err)
		}
		key := addrPort.String()
		if _, ok := seenEndpoints[key]; ok {
			return fmt.Errorf("duplicate multi_host peer endpoint")
		}
		seenEndpoints[key] = struct{}{}
	}

	if err := validateNodes(cfg); err != nil {
		return err
	}
	if err := validateUplink(cfg); err != nil {
		return err
	}
	if err := validateAdmin(cfg); err != nil {
		return err
	}
	return nil
}

// validateAdmin enforces the M5.5A admin boundary: the admin surface is
// hub-only, and an enabled admin must reference an absolute token-file path.
// The admin secret itself is never part of the config.
func validateAdmin(cfg Config) error {
	if !cfg.Admin.Enabled {
		return nil
	}
	if cfg.Runtime.Role != RuntimeRoleHub {
		return fmt.Errorf("admin.enabled requires runtime.role hub")
	}
	if strings.TrimSpace(cfg.Admin.TokenFile) == "" {
		return fmt.Errorf("admin.enabled requires admin.token_file")
	}
	if !filepath.IsAbs(strings.TrimSpace(cfg.Admin.TokenFile)) {
		return fmt.Errorf("admin.token_file must be an absolute path")
	}
	return nil
}

// validateUplink enforces the M5.4 node-only uplink boundary. The section is
// inert unless configured, and it never applies to the hub role.
func validateUplink(cfg Config) error {
	u := cfg.Uplink
	configured := u.Enabled || u.Endpoint != "" || u.NodeID != "" || u.Token != ""
	if !configured {
		return nil
	}
	if cfg.Runtime.Role != RuntimeRoleNode {
		return fmt.Errorf("uplink requires runtime.role node")
	}
	if !u.Enabled {
		return nil
	}
	if err := validateUplinkEndpoint(u.Endpoint); err != nil {
		return err
	}
	if !validHostID(u.NodeID) {
		return fmt.Errorf("uplink.node_id is invalid")
	}
	if u.NodeID != cfg.Host.ID {
		return fmt.Errorf("uplink.node_id must equal host.id for node identity binding")
	}
	if len(u.Token) < nodeTokenMinLength || len(u.Token) > nodeTokenMaxLength {
		return fmt.Errorf("uplink.token must be %d-%d characters", nodeTokenMinLength, nodeTokenMaxLength)
	}
	if !validNodeTokenCharset(u.Token) {
		return fmt.Errorf("uplink.token contains unsupported characters")
	}
	return nil
}

// validateUplinkEndpoint accepts an explicit http or https hub base address
// only. Userinfo, query strings and fragments are rejected so no credential
// or query secret can hide inside the endpoint, and sub-paths are rejected so
// the frozen machine route is always exactly /api/node/v1/snapshot.
func validateUplinkEndpoint(endpoint string) error {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return fmt.Errorf("uplink.endpoint must be a valid URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("uplink.endpoint must use http or https")
	}
	if parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("uplink.endpoint must be a bare host address without credentials or query")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return fmt.Errorf("uplink.endpoint must not include a path")
	}
	return nil
}

func validateNodes(cfg Config) error {
	hasRegistry := len(cfg.Nodes.Registered) > 0 || len(cfg.Nodes.Disabled) > 0
	if hasRegistry && cfg.Runtime.Role != RuntimeRoleHub {
		return fmt.Errorf("nodes registry requires runtime.role hub")
	}
	seenIDs := make(map[string]struct{}, len(cfg.Nodes.Registered))
	seenTokens := make(map[string]struct{}, len(cfg.Nodes.Registered))
	for _, node := range cfg.Nodes.Registered {
		if !validHostID(node.NodeID) {
			return fmt.Errorf("nodes.registered node id %q is invalid", node.NodeID)
		}
		if _, ok := seenIDs[node.NodeID]; ok {
			return fmt.Errorf("duplicate nodes.registered node id %q", node.NodeID)
		}
		seenIDs[node.NodeID] = struct{}{}
		if err := validateNodeDisplayName(node.DisplayName); err != nil {
			return fmt.Errorf("nodes.registered %q: %w", node.NodeID, err)
		}
		if len(node.Token) < nodeTokenMinLength || len(node.Token) > nodeTokenMaxLength {
			return fmt.Errorf("nodes.registered %q: token must be %d-%d characters", node.NodeID, nodeTokenMinLength, nodeTokenMaxLength)
		}
		if !validNodeTokenCharset(node.Token) {
			return fmt.Errorf("nodes.registered %q: token contains unsupported characters", node.NodeID)
		}
		if _, ok := seenTokens[node.Token]; ok {
			return fmt.Errorf("duplicate nodes.registered token")
		}
		seenTokens[node.Token] = struct{}{}
	}
	seenDisabled := make(map[string]struct{}, len(cfg.Nodes.Disabled))
	for _, id := range cfg.Nodes.Disabled {
		if _, ok := seenIDs[id]; !ok {
			return fmt.Errorf("nodes.disabled references unknown node id %q", id)
		}
		if _, ok := seenDisabled[id]; ok {
			return fmt.Errorf("duplicate nodes.disabled node id %q", id)
		}
		seenDisabled[id] = struct{}{}
	}
	return nil
}

func validateNodeDisplayName(name string) error {
	if len(name) > 64 {
		return fmt.Errorf("display name exceeds 64 bytes")
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c < 0x20 || c == 0x7f {
			return fmt.Errorf("display name contains control characters")
		}
		// '=' and ',' are the single-line registry separators: a display name
		// containing either cannot survive an atomic config save/reload, so
		// the grammar rejects it up front.
		if c == '=' || c == ',' {
			return fmt.Errorf("display name must not contain '=' or ','")
		}
	}
	return nil
}
