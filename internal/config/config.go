package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Server  ServerConfig
	Host    HostConfig
	Display DisplayConfig
	Agent   AgentConfig
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

func Defaults() Config {
	return Config{
		Server: ServerConfig{Host: "127.0.0.1", Port: 8787},
		Host:   HostConfig{ID: "local", DisplayName: "Local Mac"},
		Display: DisplayConfig{
			KindleRefreshSeconds:          20,
			CompleteHighVisibilitySeconds: 600,
			CompleteRetentionSeconds:      1800,
		},
		Agent: AgentConfig{StaleAfterSeconds: 900},
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
			case "server", "host", "display", "agent":
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
	default:
		return fmt.Errorf("unsupported key %s.%s", section, key)
	}
	return nil
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
	return nil
}
