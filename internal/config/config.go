package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Default values for configuration fields.
const (
	DefaultDataDir     = "~/.config/atria"
	DefaultMonitorDir  = "/tmp/atria-monitors"
	DefaultCacheTTL    = 5
	DefaultTmuxSession = "atria"
	DefaultPtyCols     = 120
	DefaultPtyRows     = 40
)

// Config holds the application configuration parsed from a TOML file.
type Config struct {
	WatchDirs    []string `toml:"watch_dirs"`
	TmuxPath     string   `toml:"tmux_path"`
	TmuxSession  string   `toml:"tmux_session"`
	KittenPath   string   `toml:"kitten_path"`
	DataDir      string   `toml:"data_dir"`
	MonitorDir   string   `toml:"monitor_dir"`
	CacheTTL     int      `toml:"cache_ttl"`
	DefaultAgent string   `toml:"default_agent"`
	LaunchDir    string   `toml:"launch_dir"`
	PtyCols      int      `toml:"pty_cols"`
	PtyRows      int      `toml:"pty_rows"`
	FocusMode    string   `toml:"focus_mode"`

	Integrations []string `toml:"integrations"` // ["iterm2", "tmux"]
}

// DefaultPath returns the default configuration file path.
func DefaultPath() string {
	return "~/.config/atria/config.toml"
}

// Load reads configuration from the given TOML file path. If the file does not
// exist, a Config with default values is returned without an error.
func Load(path string) (*Config, error) {
	cfg := &Config{
		DataDir:    DefaultDataDir,
		MonitorDir: DefaultMonitorDir,
		CacheTTL:   DefaultCacheTTL,
	}

	expanded := expandHome(path)
	_, err := toml.DecodeFile(expanded, cfg)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	cfg.DataDir = expandHome(cfg.DataDir)
	cfg.MonitorDir = expandHome(cfg.MonitorDir)
	cfg.TmuxPath = expandHome(cfg.TmuxPath)
	cfg.KittenPath = expandHome(cfg.KittenPath)
	cfg.LaunchDir = expandHome(cfg.LaunchDir)

	for i, dir := range cfg.WatchDirs {
		cfg.WatchDirs[i] = expandHome(dir)
	}

	return cfg, nil
}

// contractHome replaces the user's home directory prefix with ~.
func contractHome(path string) string {
	if path == "" {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+"/") {
		return "~" + path[len(home):]
	}
	return path
}

// Save writes the config to the given TOML file path with help comments.
// Fields at default values are written as comments. Non-default values
// are written uncommented. Creates parent directories if needed.
func (cfg *Config) Save(path string) error {
	expanded := expandHome(path)
	dir := filepath.Dir(expanded)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	var sb strings.Builder

	// watch_dirs
	sb.WriteString("# Directories to scan for agent projects\n")
	writeSliceField(&sb, "watch_dirs", cfg.WatchDirs, contractHome)
	sb.WriteString("\n")

	// integrations
	sb.WriteString("# Terminal integrations for agent discovery and launching\n")
	sb.WriteString("# Available: \"iterm2\" (macOS, requires iTerm2 Python API),\n")
	sb.WriteString("#            \"tmux\" (requires running inside tmux),\n")
	sb.WriteString("#            \"kitty\" (requires Kitty remote control)\n")
	writeSliceField(&sb, "integrations", cfg.Integrations, nil)
	sb.WriteString("\n")

	// default_agent
	sb.WriteString("# Default agent to launch: \"claude\", \"codex\", or \"opencode\"\n")
	if cfg.DefaultAgent != "" {
		sb.WriteString(fmt.Sprintf("default_agent = %q\n", cfg.DefaultAgent))
	} else {
		sb.WriteString("# default_agent = \"claude\"\n")
	}
	sb.WriteString("\n")

	// pty_cols, pty_rows
	sb.WriteString("# PTY terminal dimensions (built-in backend)\n")
	if cfg.PtyCols != 0 && cfg.PtyCols != DefaultPtyCols {
		sb.WriteString(fmt.Sprintf("pty_cols = %d\n", cfg.PtyCols))
	} else {
		sb.WriteString(fmt.Sprintf("# pty_cols = %d\n", DefaultPtyCols))
	}
	if cfg.PtyRows != 0 && cfg.PtyRows != DefaultPtyRows {
		sb.WriteString(fmt.Sprintf("pty_rows = %d\n", cfg.PtyRows))
	} else {
		sb.WriteString(fmt.Sprintf("# pty_rows = %d\n", DefaultPtyRows))
	}
	sb.WriteString("\n")

	// tmux settings
	sb.WriteString("# tmux backend settings\n")
	if cfg.TmuxSession != "" && cfg.TmuxSession != DefaultTmuxSession {
		sb.WriteString(fmt.Sprintf("tmux_session = %q\n", cfg.TmuxSession))
	} else {
		sb.WriteString("# tmux_session = \"atria\"\n")
	}
	if cfg.TmuxPath != "" {
		sb.WriteString(fmt.Sprintf("tmux_path = %q\n", contractHome(cfg.TmuxPath)))
	} else {
		sb.WriteString("# tmux_path = \"/usr/bin/tmux\"\n")
	}
	sb.WriteString("\n")

	// kitty settings
	sb.WriteString("# Kitty backend settings\n")
	if cfg.KittenPath != "" {
		sb.WriteString(fmt.Sprintf("kitten_path = %q\n", contractHome(cfg.KittenPath)))
	} else {
		sb.WriteString("# kitten_path = \"kitten\"\n")
	}
	sb.WriteString("\n")

	// Advanced settings — only written when non-default
	home, _ := os.UserHomeDir()
	defaultDataDir := filepath.Join(home, ".config/atria")
	hasAdvanced := false

	type advField struct {
		key   string
		value string
		show  bool
	}
	fields := []advField{
		{"data_dir", fmt.Sprintf("%q", contractHome(cfg.DataDir)), cfg.DataDir != "" && cfg.DataDir != defaultDataDir},
		{"monitor_dir", fmt.Sprintf("%q", contractHome(cfg.MonitorDir)), cfg.MonitorDir != "" && cfg.MonitorDir != DefaultMonitorDir},
		{"cache_ttl", fmt.Sprintf("%d", cfg.CacheTTL), cfg.CacheTTL != 0 && cfg.CacheTTL != DefaultCacheTTL},
		{"launch_dir", fmt.Sprintf("%q", contractHome(cfg.LaunchDir)), cfg.LaunchDir != ""},
		{"focus_mode", fmt.Sprintf("%q", cfg.FocusMode), cfg.FocusMode != ""},
	}
	for _, f := range fields {
		if f.show {
			if !hasAdvanced {
				sb.WriteString("# Advanced settings\n")
				hasAdvanced = true
			}
			sb.WriteString(f.key + " = " + f.value + "\n")
		}
	}

	return os.WriteFile(expanded, []byte(sb.String()), 0o644)
}

// writeSliceField writes a TOML array field, or a commented-out empty array if values is empty.
func writeSliceField(sb *strings.Builder, key string, values []string, transform func(string) string) {
	if len(values) > 0 {
		sb.WriteString(key + " = [")
		for i, v := range values {
			if i > 0 {
				sb.WriteString(", ")
			}
			val := v
			if transform != nil {
				val = transform(v)
			}
			sb.WriteString(fmt.Sprintf("%q", val))
		}
		sb.WriteString("]\n")
	} else {
		sb.WriteString("# " + key + " = []\n")
	}
}

// expandHome replaces a leading ~ with the user's home directory.
func expandHome(path string) string {
	if path == "" {
		return path
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[1:])
	}
	return path
}
