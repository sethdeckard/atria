package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load("/tmp/atria-test-nonexistent/config.toml")
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	if cfg.Integrations != nil {
		t.Errorf("expected nil integrations, got %v", cfg.Integrations)
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
integrations = ["iterm2", "tmux"]
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

	if len(cfg.Integrations) != 2 || cfg.Integrations[0] != "iterm2" || cfg.Integrations[1] != "tmux" {
		t.Errorf("expected integrations [iterm2, tmux], got %v", cfg.Integrations)
	}
	if cfg.CacheTTL != 10 {
		t.Errorf("expected cache_ttl 10, got %d", cfg.CacheTTL)
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
	if cfg.Integrations != nil {
		t.Errorf("expected nil integrations, got %v", cfg.Integrations)
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

func TestSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := &Config{
		WatchDirs:    []string{"/home/user/projects", "/tmp/work"},
		Integrations: []string{"iterm2"},
		DefaultAgent: "claude",
		Theme:        "ansi",
		PtyCols:      200,
		PtyRows:      40, // default, should be commented
		TmuxSession:  "myatria",
	}

	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	s := string(content)

	// watch_dirs should be present uncommented
	if !strings.Contains(s, `watch_dirs = [`) {
		t.Errorf("expected uncommented watch_dirs, got:\n%s", s)
	}

	// integrations should be present uncommented
	if !strings.Contains(s, `integrations = ["iterm2"]`) {
		t.Errorf("expected integrations = [\"iterm2\"], got:\n%s", s)
	}

	// default_agent uncommented
	if !strings.Contains(s, `default_agent = "claude"`) {
		t.Errorf("expected default_agent = \"claude\", got:\n%s", s)
	}

	// theme = "ansi" uncommented
	if !strings.Contains(s, `theme = "ansi"`) {
		t.Errorf("expected theme = \"ansi\", got:\n%s", s)
	}

	// pty_cols non-default should be uncommented
	if !strings.Contains(s, "pty_cols = 200") {
		t.Errorf("expected pty_cols = 200, got:\n%s", s)
	}

	// pty_rows default should be commented
	if !strings.Contains(s, "# pty_rows = 40") {
		t.Errorf("expected commented pty_rows, got:\n%s", s)
	}

	// tmux_session non-default should be uncommented
	if !strings.Contains(s, `tmux_session = "myatria"`) {
		t.Errorf("expected tmux_session = \"myatria\", got:\n%s", s)
	}

	// Verify it round-trips
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load after Save failed: %v", err)
	}
	if len(loaded.Integrations) != 1 || loaded.Integrations[0] != "iterm2" {
		t.Errorf("expected [iterm2], got %v", loaded.Integrations)
	}
	if loaded.DefaultAgent != "claude" {
		t.Errorf("expected claude, got %q", loaded.DefaultAgent)
	}
	if loaded.PtyCols != 200 {
		t.Errorf("expected 200, got %d", loaded.PtyCols)
	}
	if loaded.TmuxSession != "myatria" {
		t.Errorf("expected myatria, got %q", loaded.TmuxSession)
	}
	if loaded.Theme != "ansi" {
		t.Errorf("expected theme 'ansi', got %q", loaded.Theme)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected config file mode 0600, got %o", got)
	}
}

func TestSaveDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := &Config{}
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	s := string(content)

	// All should be commented
	if !strings.Contains(s, "# watch_dirs = []") {
		t.Errorf("expected commented watch_dirs, got:\n%s", s)
	}
	if !strings.Contains(s, "# integrations = []") {
		t.Errorf("expected commented integrations, got:\n%s", s)
	}
	if !strings.Contains(s, "# default_agent = \"claude\"") {
		t.Errorf("expected commented default_agent, got:\n%s", s)
	}
	if !strings.Contains(s, "# theme = \"builtin\"") {
		t.Errorf("expected commented theme, got:\n%s", s)
	}
	if !strings.Contains(s, "# tmux_session = \"atria\"  # optional override") {
		t.Errorf("expected commented tmux_session example, got:\n%s", s)
	}
}

func TestSaveCreatesPrivateConfigDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested")
	path := filepath.Join(dir, "config.toml")

	cfg := &Config{}
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("expected config dir mode 0700, got %o", got)
	}
}

func TestSavePreservesAdvancedFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := &Config{
		WatchDirs:    []string{"/projects"},
		Integrations: []string{"tmux"},
		DataDir:      "/custom/data",
		MonitorDir:   "/custom/monitors",
		CacheTTL:     15,
		LaunchDir:    "/custom/launch",
		FocusMode:    "terminal",
	}

	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load after Save failed: %v", err)
	}

	if loaded.DataDir != "/custom/data" {
		t.Errorf("expected data_dir '/custom/data', got %q", loaded.DataDir)
	}
	if loaded.MonitorDir != "/custom/monitors" {
		t.Errorf("expected monitor_dir '/custom/monitors', got %q", loaded.MonitorDir)
	}
	if loaded.CacheTTL != 15 {
		t.Errorf("expected cache_ttl 15, got %d", loaded.CacheTTL)
	}
	if loaded.LaunchDir != "/custom/launch" {
		t.Errorf("expected launch_dir '/custom/launch', got %q", loaded.LaunchDir)
	}
	if loaded.FocusMode != "terminal" {
		t.Errorf("expected focus_mode 'terminal', got %q", loaded.FocusMode)
	}
}

func TestSaveCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "dir", "config.toml")

	cfg := &Config{DefaultAgent: "codex"}
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("expected file to exist")
	}
}

func TestUpdateCheckEnabled(t *testing.T) {
	tests := []struct {
		name     string
		value    *bool
		expected bool
	}{
		{"nil (default)", nil, true},
		{"explicit true", boolPtr(true), true},
		{"explicit false", boolPtr(false), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{UpdateCheck: tc.value}
			if got := cfg.UpdateCheckEnabled(); got != tc.expected {
				t.Errorf("UpdateCheckEnabled() = %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestUpdateCheckSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	// Save with update_check = false
	cfg := &Config{UpdateCheck: boolPtr(false)}
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if !strings.Contains(string(content), "update_check = false") {
		t.Errorf("expected uncommented update_check = false, got:\n%s", content)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.UpdateCheckEnabled() {
		t.Error("expected UpdateCheckEnabled() = false after round-trip")
	}

	// Save with nil (default) — should be commented
	cfg2 := &Config{}
	if err := cfg2.Save(path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	content, _ = os.ReadFile(path)
	if !strings.Contains(string(content), "# update_check = true") {
		t.Errorf("expected commented update_check, got:\n%s", content)
	}
}

func boolPtr(v bool) *bool { return &v }

func TestNormalizeTheme(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ThemeBuiltin},
		{"builtin", ThemeBuiltin},
		{"ansi", ThemeANSI},
		{"garbage", ThemeBuiltin},
		{"ANSI", ThemeBuiltin}, // case-sensitive
	}
	for _, tc := range tests {
		if got := NormalizeTheme(tc.input); got != tc.expected {
			t.Errorf("NormalizeTheme(%q) = %q, want %q", tc.input, got, tc.expected)
		}
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
