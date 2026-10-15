// Package config loads aerocycle's configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is the on-disk configuration.
type Config struct {
	// CycleTimeoutMS is how long a run of repeated presses keeps cycling
	// through the same app before a fresh press jumps back to the most
	// recently used window. Default 1200ms.
	CycleTimeoutMS int `toml:"cycle-timeout-ms"`

	// Apps maps a key name ("d", "1", ...) to an application bundle id.
	// The key is pressed together with right-Command and cycles that app's
	// windows.
	Apps map[string]string `toml:"apps"`

	// Actions maps a key name to an AeroSpace command expression, run via
	// `aerospace eval` (e.g. "move left", "fullscreen", "layout tiles").
	Actions map[string]string `toml:"actions"`
}

// DefaultPath is ~/.config/aerocycle/config.toml (honouring XDG_CONFIG_HOME).
func DefaultPath() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "aerocycle", "config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "config.toml"
	}
	return filepath.Join(home, ".config", "aerocycle", "config.toml")
}

// Dir is the directory holding aerocycle's config.
func Dir() string {
	return filepath.Dir(DefaultPath())
}

// PidPath is where the running daemon records its pid (used to reload it).
func PidPath() string {
	return filepath.Join(Dir(), "aerocycle.pid")
}

// Save writes cfg to path in a tidy, commented form.
func Save(path string, cfg *Config) error {
	if path == "" {
		path = DefaultPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	var b strings.Builder
	b.WriteString("# Managed by `aerocycle tui`\n")
	b.WriteString("# Keys are pressed with the RIGHT Command key.\n")
	b.WriteString("#   [apps]    right-\u2318+<key> cycles that app's windows\n")
	b.WriteString("#   [actions] right-\u2318+<key> runs an AeroSpace command (aerospace eval)\n\n")
	fmt.Fprintf(&b, "cycle-timeout-ms = %d\n\n", cfg.CycleTimeoutMS)

	writeTable := func(name string, m map[string]string) {
		if len(m) == 0 {
			return
		}
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Fprintf(&b, "[%s]\n", name)
		for _, k := range keys {
			fmt.Fprintf(&b, "%q = %q\n", k, m[k])
		}
		b.WriteString("\n")
	}
	writeTable("apps", cfg.Apps)
	writeTable("actions", cfg.Actions)

	// Keep a one-deep backup so a bad save is always recoverable.
	if prev, err := os.ReadFile(path); err == nil {
		_ = os.WriteFile(path+".bak", prev, 0o644)
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// Load reads and validates the config at path (or the default path if empty).
func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultPath()
	}
	cfg := &Config{CycleTimeoutMS: 1200, Apps: map[string]string{}, Actions: map[string]string{}}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if _, err := toml.Decode(string(raw), cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.CycleTimeoutMS <= 0 {
		cfg.CycleTimeoutMS = 1200
	}

	// Normalise and validate app keys.
	norm := make(map[string]string, len(cfg.Apps))
	for k, v := range cfg.Apps {
		key := strings.ToLower(strings.TrimSpace(k))
		if key == "" {
			return nil, fmt.Errorf("apps: empty key")
		}
		if strings.TrimSpace(v) == "" {
			return nil, fmt.Errorf("apps.%s: empty bundle id", key)
		}
		norm[key] = strings.TrimSpace(v)
	}
	cfg.Apps = norm

	normActions := make(map[string]string, len(cfg.Actions))
	for k, v := range cfg.Actions {
		key := strings.ToLower(strings.TrimSpace(k))
		if key == "" {
			return nil, fmt.Errorf("actions: empty key")
		}
		if _, dup := norm[key]; dup {
			return nil, fmt.Errorf("key %q is defined in both [apps] and [actions]", key)
		}
		if strings.TrimSpace(v) == "" {
			return nil, fmt.Errorf("actions.%s: empty command", key)
		}
		normActions[key] = strings.TrimSpace(v)
	}
	cfg.Actions = normActions
	return cfg, nil
}
