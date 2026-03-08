package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config holds the application configuration parsed from a TOML file.
type Config struct {
	WatchDirs    []string `toml:"watch_dirs"`
	Backend      string   `toml:"backend"`
	IT2Path      string   `toml:"it2_path"`
	DataDir      string   `toml:"data_dir"`
	MonitorDir   string   `toml:"monitor_dir"`
	CacheTTL     int      `toml:"cache_ttl"`
	DefaultAgent string   `toml:"default_agent"`
	LaunchDir    string   `toml:"launch_dir"`
}

// DefaultPath returns the default configuration file path.
func DefaultPath() string {
	return "~/.config/atria/config.toml"
}

// Load reads configuration from the given TOML file path. If the file does not
// exist, a Config with default values is returned without an error.
func Load(path string) (*Config, error) {
	cfg := &Config{
		Backend:    "iterm2",
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
	cfg.LaunchDir = expandHome(cfg.LaunchDir)

	for i, dir := range cfg.WatchDirs {
		cfg.WatchDirs[i] = expandHome(dir)
	}

	return cfg, nil
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
