package config

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

const maxProbeTimeoutMilliseconds = 60000

type Config struct {
	Server  ServerConfig
	Host    HostConfig
	Display DisplayConfig
	Agent   AgentConfig
	Network NetworkConfig
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

func Defaults() Config {
	return Config{
		Server: ServerConfig{Host: "127.0.0.1", Port: 8787},
		Host:   HostConfig{ID: "local", DisplayName: "Local Mac"},
		Display: DisplayConfig{
			KindleRefreshSeconds:          20,
			CompleteHighVisibilitySeconds: 600,
			CompleteRetentionSeconds:      1800,
		},
		Agent:   AgentConfig{StaleAfterSeconds: 900},
		Network: NetworkConfig{ProbeAddress: "1.1.1.1:443", ProbeTimeoutMilliseconds: 1500},
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
			case "server", "host", "display", "agent", "network":
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
	default:
		return fmt.Errorf("unsupported key %s.%s", section, key)
	}
	return nil
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
	if strings.TrimSpace(cfg.Server.Host) == "" {
		return fmt.Errorf("server.host must not be empty")
	}
	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
		return fmt.Errorf("server.port must be between 1 and 65535")
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
	return nil
}
