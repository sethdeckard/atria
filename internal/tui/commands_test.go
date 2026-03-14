package tui

import (
	"testing"

	"github.com/sethdeckard/atria/internal/terminal"
)

// stubBackend satisfies terminal.Backend for derivePrimary tests.
type stubBackend struct {
	label string
}

func (s *stubBackend) Available() error                                          { return nil }
func (s *stubBackend) ListSessions() ([]terminal.Session, error)                { return nil, nil }
func (s *stubBackend) NewSession() (string, error)                              { return "", nil }
func (s *stubBackend) SendText(sessionID, text string) error                    { return nil }
func (s *stubBackend) RunCommand(sessionID, cmd string) error                   { return nil }
func (s *stubBackend) FocusSession(sessionID string) error                      { return nil }
func (s *stubBackend) ReadScreen(sessionID string, lines int) (string, error)   { return "", nil }
func (s *stubBackend) GetVar(sessionID, varName string) (string, error)         { return "", nil }
func (s *stubBackend) MonitorOutput(sessionID, logPath, patterns string) (int, error) {
	return 0, nil
}

func TestDerivePrimary(t *testing.T) {
	ptyClient := &stubBackend{label: "pty"}
	tmuxClient := &stubBackend{label: "tmux"}
	itermClient := &stubBackend{label: "iterm"}
	kittyClient := &stubBackend{label: "kitty"}

	tests := []struct {
		name         string
		envVars      map[string]string
		integrations []terminal.Integration
		wantSource   string
	}{
		{
			"tmux env set with tmux integration",
			map[string]string{"TMUX": "/tmp/tmux-501/default,123,0"},
			[]terminal.Integration{
				{Prefix: "tmux:", Source: "tmux", Backend: tmuxClient},
			},
			"tmux",
		},
		{
			"kitty env with kitty integration",
			map[string]string{"KITTY_WINDOW_ID": "1"},
			[]terminal.Integration{
				{Prefix: "kitty:", Source: "kitty", Backend: kittyClient},
			},
			"kitty",
		},
		{
			"iterm env with iterm integration",
			map[string]string{"TERM_PROGRAM": "iTerm.app"},
			[]terminal.Integration{
				{Prefix: "iterm:", Source: "iterm", Backend: itermClient},
			},
			"iterm",
		},
		{
			"multiple envs tmux wins",
			map[string]string{"TMUX": "/tmp/tmux-501/default,123,0", "TERM_PROGRAM": "iTerm.app"},
			[]terminal.Integration{
				{Prefix: "tmux:", Source: "tmux", Backend: tmuxClient},
				{Prefix: "iterm:", Source: "iterm", Backend: itermClient},
			},
			"tmux",
		},
		{
			"no matching env falls back to pty",
			map[string]string{},
			[]terminal.Integration{
				{Prefix: "tmux:", Source: "tmux", Backend: tmuxClient},
				{Prefix: "iterm:", Source: "iterm", Backend: itermClient},
			},
			"pty",
		},
		{
			"no integrations falls back to pty",
			map[string]string{},
			nil,
			"pty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear relevant env vars then set test values.
			for _, key := range []string{"TMUX", "KITTY_WINDOW_ID", "TERM_PROGRAM"} {
				t.Setenv(key, "")
			}
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			_, source := derivePrimary(tt.integrations, ptyClient)
			if source != tt.wantSource {
				t.Errorf("derivePrimary() source = %q, want %q", source, tt.wantSource)
			}
		})
	}
}

func TestIntegrationMeta(t *testing.T) {
	tests := []struct {
		name       string
		wantPrefix string
		wantSource string
	}{
		{"iterm2", "iterm:", "iterm"},
		{"tmux", "tmux:", "tmux"},
		{"kitty", "kitty:", "kitty"},
		{"unknown", "unknown:", "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prefix, source := integrationMeta(tt.name)
			if prefix != tt.wantPrefix {
				t.Errorf("integrationMeta(%q) prefix = %q, want %q", tt.name, prefix, tt.wantPrefix)
			}
			if source != tt.wantSource {
				t.Errorf("integrationMeta(%q) source = %q, want %q", tt.name, source, tt.wantSource)
			}
		})
	}
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
