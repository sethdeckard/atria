package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config holds the application configuration parsed from a TOML file.
type Config struct {
	WatchDirs    []string `toml:"watch_dirs"`
	IT2Path      string   `toml:"it2_path"`
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
		DataDir:    "~/.config/atria",
		MonitorDir: "/tmp/atria-monitors",
		CacheTTL:   5,
	}

	expanded := expandHome(path)
	_, err := toml.DecodeFile(expanded, cfg)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	cfg.DataDir = expandHome(cfg.DataDir)
	cfg.MonitorDir = expandHome(cfg.MonitorDir)
	cfg.IT2Path = expandHome(cfg.IT2Path)
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
	if len(cfg.WatchDirs) > 0 {
		sb.WriteString("watch_dirs = [")
		for i, d := range cfg.WatchDirs {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(fmt.Sprintf("%q", contractHome(d)))
		}
		sb.WriteString("]\n")
	} else {
		sb.WriteString("# watch_dirs = []\n")
	}
	sb.WriteString("\n")

	// integrations
	sb.WriteString("# Terminal integrations for agent discovery and launching\n")
	sb.WriteString("# Available: \"iterm2\" (macOS, requires iTerm2 Python API),\n")
	sb.WriteString("#            \"tmux\" (requires running inside tmux),\n")
	sb.WriteString("#            \"kitty\" (requires Kitty remote control)\n")
	if len(cfg.Integrations) > 0 {
		sb.WriteString("integrations = [")
		for i, name := range cfg.Integrations {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(fmt.Sprintf("%q", name))
		}
		sb.WriteString("]\n")
	} else {
		sb.WriteString("# integrations = []\n")
	}
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
	if cfg.PtyCols != 0 && cfg.PtyCols != 120 {
		sb.WriteString(fmt.Sprintf("pty_cols = %d\n", cfg.PtyCols))
	} else {
		sb.WriteString("# pty_cols = 120\n")
	}
	if cfg.PtyRows != 0 && cfg.PtyRows != 40 {
		sb.WriteString(fmt.Sprintf("pty_rows = %d\n", cfg.PtyRows))
	} else {
		sb.WriteString("# pty_rows = 40\n")
	}
	sb.WriteString("\n")

	// tmux settings
	sb.WriteString("# tmux backend settings\n")
	if cfg.TmuxSession != "" && cfg.TmuxSession != "atria" {
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

	// iterm2 settings
	sb.WriteString("# iTerm2 backend settings\n")
	if cfg.IT2Path != "" {
		sb.WriteString(fmt.Sprintf("it2_path = %q\n", contractHome(cfg.IT2Path)))
	} else {
		sb.WriteString("# it2_path = \"~/.local/share/atria/venv/bin/it2\"\n")
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

	if cfg.DataDir != "" && cfg.DataDir != defaultDataDir {
		if !hasAdvanced {
			sb.WriteString("# Advanced settings\n")
			hasAdvanced = true
		}
		sb.WriteString(fmt.Sprintf("data_dir = %q\n", contractHome(cfg.DataDir)))
	}
	if cfg.MonitorDir != "" && cfg.MonitorDir != "/tmp/atria-monitors" {
		if !hasAdvanced {
			sb.WriteString("# Advanced settings\n")
			hasAdvanced = true
		}
		sb.WriteString(fmt.Sprintf("monitor_dir = %q\n", contractHome(cfg.MonitorDir)))
	}
	if cfg.CacheTTL != 0 && cfg.CacheTTL != 5 {
		if !hasAdvanced {
			sb.WriteString("# Advanced settings\n")
			hasAdvanced = true
		}
		sb.WriteString(fmt.Sprintf("cache_ttl = %d\n", cfg.CacheTTL))
	}
	if cfg.LaunchDir != "" {
		if !hasAdvanced {
			sb.WriteString("# Advanced settings\n")
			hasAdvanced = true
		}
		sb.WriteString(fmt.Sprintf("launch_dir = %q\n", contractHome(cfg.LaunchDir)))
	}
	if cfg.FocusMode != "" {
		if !hasAdvanced {
			sb.WriteString("# Advanced settings\n")
			hasAdvanced = true
		}
		sb.WriteString(fmt.Sprintf("focus_mode = %q\n", cfg.FocusMode))
	}

	return os.WriteFile(expanded, []byte(sb.String()), 0o644)
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
