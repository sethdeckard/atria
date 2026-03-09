package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load("/tmp/atria-test-nonexistent/config.toml")
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	if cfg.Backend != "" {
		t.Errorf("expected empty backend (auto-detect), got %q", cfg.Backend)
	}
	home, _ := os.UserHomeDir()
	expectedDataDir := filepath.Join(home, ".config/atria")
	if cfg.DataDir != expectedDataDir {
		t.Errorf("expected data_dir %q, got %q", expectedDataDir, cfg.DataDir)
	}
	if cfg.MonitorDir != "/tmp/atria-monitors" {
		t.Errorf("expected monitor_dir '/tmp/atria-monitors', got %q", cfg.MonitorDir)
	}
	if cfg.CacheTTL != 5 {
		t.Errorf("expected cache_ttl 5, got %d", cfg.CacheTTL)
	}
	if len(cfg.WatchDirs) != 0 {
		t.Errorf("expected empty watch_dirs, got %v", cfg.WatchDirs)
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `
watch_dirs = ["~/wallpapers", "/usr/share/backgrounds"]
backend = "sway"
it2_path = "~/bin/it2"
data_dir = "~/atria-data"
monitor_dir = "~/atria-monitors"
cache_ttl = 10
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	home, _ := os.UserHomeDir()

	if cfg.Backend != "sway" {
		t.Errorf("expected backend 'sway', got %q", cfg.Backend)
	}
	if cfg.CacheTTL != 10 {
		t.Errorf("expected cache_ttl 10, got %d", cfg.CacheTTL)
	}
	if cfg.IT2Path != filepath.Join(home, "bin/it2") {
		t.Errorf("expected it2_path %q, got %q", filepath.Join(home, "bin/it2"), cfg.IT2Path)
	}
	if cfg.DataDir != filepath.Join(home, "atria-data") {
		t.Errorf("expected data_dir %q, got %q", filepath.Join(home, "atria-data"), cfg.DataDir)
	}
	if cfg.MonitorDir != filepath.Join(home, "atria-monitors") {
		t.Errorf("expected monitor_dir %q, got %q", filepath.Join(home, "atria-monitors"), cfg.MonitorDir)
	}
	if len(cfg.WatchDirs) != 2 {
		t.Fatalf("expected 2 watch_dirs, got %d", len(cfg.WatchDirs))
	}
	if cfg.WatchDirs[0] != filepath.Join(home, "wallpapers") {
		t.Errorf("expected watch_dirs[0] %q, got %q", filepath.Join(home, "wallpapers"), cfg.WatchDirs[0])
	}
	if cfg.WatchDirs[1] != "/usr/share/backgrounds" {
		t.Errorf("expected watch_dirs[1] '/usr/share/backgrounds', got %q", cfg.WatchDirs[1])
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to get home dir: %v", err)
	}

	tests := []struct {
		input    string
		expected string
	}{
		{"~/Documents", filepath.Join(home, "Documents")},
		{"~", home},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
		{"", ""},
	}

	for _, tc := range tests {
		result := expandHome(tc.input)
		if result != tc.expected {
			t.Errorf("expandHome(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestLoadPartial(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `
watch_dirs = ["/photos"]
cache_ttl = 30
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Explicitly set fields
	if cfg.CacheTTL != 30 {
		t.Errorf("expected cache_ttl 30, got %d", cfg.CacheTTL)
	}
	if len(cfg.WatchDirs) != 1 || cfg.WatchDirs[0] != "/photos" {
		t.Errorf("expected watch_dirs [/photos], got %v", cfg.WatchDirs)
	}

	// Default fields
	if cfg.Backend != "" {
		t.Errorf("expected empty backend (auto-detect), got %q", cfg.Backend)
	}
	if cfg.MonitorDir != "/tmp/atria-monitors" {
		t.Errorf("expected default monitor_dir '/tmp/atria-monitors', got %q", cfg.MonitorDir)
	}
	if cfg.DefaultAgent != "" {
		t.Errorf("expected empty default_agent, got %q", cfg.DefaultAgent)
	}

	home, _ := os.UserHomeDir()
	expectedDataDir := filepath.Join(home, ".config/atria")
	if cfg.DataDir != expectedDataDir {
		t.Errorf("expected default data_dir %q, got %q", expectedDataDir, cfg.DataDir)
	}
}

func TestLoadDefaultAgent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `default_agent = "codex"`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DefaultAgent != "codex" {
		t.Errorf("expected default_agent 'codex', got %q", cfg.DefaultAgent)
	}
}
