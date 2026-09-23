package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sethdeckard/atria/internal/config"
	"github.com/sethdeckard/atria/libatria"
	"github.com/sethdeckard/atria/libatria/terminal"
)

// stubBackend is a terminal.Backend that does nothing, for tests that need
// a backend without the StyledReader surface.
type stubBackend struct{}

func (s *stubBackend) Available() error                                       { return nil }
func (s *stubBackend) ListSessions() ([]terminal.Session, error)              { return nil, nil }
func (s *stubBackend) NewSession() (string, error)                            { return "", nil }
func (s *stubBackend) SendText(sessionID, text string) error                  { return nil }
func (s *stubBackend) RunCommand(sessionID, cmd string) error                 { return nil }
func (s *stubBackend) FocusSession(sessionID string) error                    { return nil }
func (s *stubBackend) ReadScreen(sessionID string, lines int) (string, error) { return "", nil }
func (s *stubBackend) GetVar(sessionID, varName string) (string, error)       { return "", nil }
func (s *stubBackend) MonitorOutput(sessionID, logPath, patterns string) (int, error) {
	return 0, nil
}

func TestRemoveString(t *testing.T) {
	tests := []struct {
		name     string
		slice    []string
		s        string
		expected []string
	}{
		{"empty slice", []string{}, "a", []string{}},
		{"no match", []string{"a", "b", "c"}, "d", []string{"a", "b", "c"}},
		{"single match", []string{"a", "b", "c"}, "b", []string{"a", "c"}},
		{"multiple matches", []string{"a", "b", "a", "c"}, "a", []string{"b", "c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := removeString(tt.slice, tt.s)
			if len(got) != len(tt.expected) {
				t.Fatalf("removeString(%v, %q) = %v, want %v", tt.slice, tt.s, got, tt.expected)
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("removeString(%v, %q)[%d] = %q, want %q", tt.slice, tt.s, i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestSanitizeForPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"pty-0", "pty-0"},
		{"iterm:session-abc", "iterm_session-abc"},
		{"tmux:%1", "tmux_%1"},
		{"wezterm:42", "wezterm_42"},
		{"path/with/slashes", "path_with_slashes"},
		{"back\\slash", "back_slash"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeForPath(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeForPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestContainsString(t *testing.T) {
	tests := []struct {
		name     string
		slice    []string
		s        string
		expected bool
	}{
		{"present", []string{"a", "b", "c"}, "b", true},
		{"absent", []string{"a", "b", "c"}, "d", false},
		{"empty slice", []string{}, "a", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsString(tt.slice, tt.s)
			if got != tt.expected {
				t.Errorf("containsString(%v, %q) = %v, want %v", tt.slice, tt.s, got, tt.expected)
			}
		})
	}
}

func TestToggleIntegrationPassesSavedSettingsToEnable(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "tmux.log")
	tmuxPath := filepath.Join(dir, "tmux")
	script := "#!/bin/sh\necho \"$@\" >> \"" + logPath + "\"\ncase \"$1\" in new-window|new-session) echo '%9';; esac\nexit 0\n"
	if err := os.WriteFile(tmuxPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	// The stack opened with the session name atria started with.
	stack, err := libatria.Open(libatria.Options{
		TmuxPath:        tmuxPath,
		TmuxSession:     "old",
		Getenv:          func(k string) string { return map[string]string{"TMUX": "/tmp/t"}[k] },
		NoSelfTTYFilter: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stack.Close()

	// The user has since edited tmux_session in settings and saved.
	cfg := &config.Config{TmuxPath: tmuxPath, TmuxSession: "fresh"}
	msg := toggleIntegration("tmux", true, cfg, filepath.Join(dir, "config.toml"), stack)()
	toggled, ok := msg.(IntegrationToggledMsg)
	if !ok || toggled.Err != nil || !toggled.Status.Active {
		t.Fatalf("toggle = %+v", msg)
	}
	if _, err := stack.Backend().NewSession(); err != nil {
		t.Fatal(err)
	}
	log, _ := os.ReadFile(logPath)
	if !strings.Contains(string(log), "new-window -t =fresh:") {
		t.Errorf("launch should use the saved tmux_session, got:\n%s", log)
	}
	if len(cfg.Integrations) != 1 || cfg.Integrations[0] != "tmux" {
		t.Errorf("config should record the toggle, got %v", cfg.Integrations)
	}
}
