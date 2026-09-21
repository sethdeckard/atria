package tui

import (
	"testing"

	"github.com/sethdeckard/atria/libatria/terminal"
)

// stubBackend satisfies terminal.Backend for derivePrimary and remap tests.
type stubBackend struct {
	label string
}

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
			for _, key := range []string{"TMUX", "KITTY_WINDOW_ID", "TERM_PROGRAM", "WEZTERM_UNIX_SOCKET", "DEVICETERM_SESSION"} {
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

func TestDemoteRemap(t *testing.T) {
	pty := &stubBackend{label: "pty"}
	wez := &stubBackend{label: "wezterm"}
	dt := &stubBackend{label: "deviceterm"}

	t.Run("pty gains an entry and its ids gain the prefix", func(t *testing.T) {
		comp := terminal.NewCompositeBackend(pty, "pty", nil)
		got := demoteRemap(comp, pty)
		want := &SourceRemap{Source: "pty", Prefix: "pty:", ToPrefixed: true}
		if got == nil || *got != *want {
			t.Errorf("remap = %+v, want %+v", got, want)
		}
		integs := comp.Integrations()
		if len(integs) != 1 || integs[0].Prefix != "pty:" {
			t.Errorf("integrations = %+v, want a pty: entry", integs)
		}
	})

	t.Run("a native primary keeps its entry and its ids gain the prefix", func(t *testing.T) {
		comp := terminal.NewCompositeBackend(wez, "wezterm", []terminal.Integration{
			{Prefix: "wezterm:", Source: "wezterm", Backend: wez},
		})
		got := demoteRemap(comp, pty)
		want := &SourceRemap{Source: "wezterm", Prefix: "wezterm:", ToPrefixed: true}
		if got == nil || *got != *want {
			t.Errorf("remap = %+v, want %+v", got, want)
		}
		if integs := comp.Integrations(); len(integs) != 1 || integs[0].Prefix != "wezterm:" {
			t.Errorf("integrations = %+v, want only the existing wezterm: entry", integs)
		}
	})

	t.Run("a primary with no entry yields no remap", func(t *testing.T) {
		comp := terminal.NewCompositeBackend(dt, "deviceterm", nil)
		if got := demoteRemap(comp, pty); got != nil {
			t.Errorf("remap = %+v, want nil", got)
		}
		if len(comp.Integrations()) != 0 {
			t.Errorf("no entry should be added for a source without one")
		}
	})
}

func TestPromoteRemap(t *testing.T) {
	pty := &stubBackend{label: "pty"}
	wez := &stubBackend{label: "wezterm"}

	t.Run("pty loses the prefix and its entry is detached", func(t *testing.T) {
		comp := terminal.NewCompositeBackend(pty, "pty", []terminal.Integration{
			{Prefix: "pty:", Source: "pty", Backend: pty},
		})
		got := promoteRemap(comp, "pty")
		want := &SourceRemap{Source: "pty", Prefix: "pty:", ToPrefixed: false}
		if got == nil || *got != *want {
			t.Errorf("remap = %+v, want %+v", got, want)
		}
		if len(comp.Integrations()) != 0 {
			t.Errorf("pty: entry should be detached once PTY is primary")
		}
	})

	t.Run("a native backend loses the prefix and keeps its entry", func(t *testing.T) {
		comp := terminal.NewCompositeBackend(wez, "wezterm", []terminal.Integration{
			{Prefix: "wezterm:", Source: "wezterm", Backend: wez},
		})
		got := promoteRemap(comp, "wezterm")
		want := &SourceRemap{Source: "wezterm", Prefix: "wezterm:", ToPrefixed: false}
		if got == nil || *got != *want {
			t.Errorf("remap = %+v, want %+v", got, want)
		}
		if integs := comp.Integrations(); len(integs) != 1 || integs[0].Prefix != "wezterm:" {
			t.Errorf("integrations = %+v, want the wezterm: entry kept", integs)
		}
	})

	t.Run("an unknown source yields no remap", func(t *testing.T) {
		comp := terminal.NewCompositeBackend(pty, "pty", nil)
		if got := promoteRemap(comp, "mystery"); got != nil {
			t.Errorf("remap = %+v, want nil", got)
		}
	})
}

func TestOutranksPrimary(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		enable  string
		current string
		want    bool
	}{
		// DeviceTerm outranks every other primary, in either toggle order.
		{"deviceterm over pty", map[string]string{"DEVICETERM_SESSION": "s"}, "deviceterm", "pty", true},
		{"deviceterm over wezterm", map[string]string{"DEVICETERM_SESSION": "s"}, "deviceterm", "wezterm", true},
		{"deviceterm over tmux", map[string]string{"DEVICETERM_SESSION": "s"}, "deviceterm", "tmux", true},
		{"wezterm never displaces deviceterm", map[string]string{"WEZTERM_UNIX_SOCKET": "/tmp/w"}, "wezterm", "deviceterm", false},
		{"kitty never displaces deviceterm", map[string]string{"KITTY_WINDOW_ID": "1"}, "kitty", "deviceterm", false},
		{"tmux never displaces deviceterm", map[string]string{"TMUX": "/tmp/t"}, "tmux", "deviceterm", false},
		{"iterm never displaces deviceterm", map[string]string{"TERM_PROGRAM": "iTerm.app"}, "iterm2", "deviceterm", false},
		// Other backends follow tmux > Kitty > WezTerm > iTerm > PTY precedence.
		{"tmux over kitty", map[string]string{"TMUX": "/tmp/t"}, "tmux", "kitty", true},
		{"kitty over wezterm", map[string]string{"KITTY_WINDOW_ID": "1"}, "kitty", "wezterm", true},
		{"kitty not over tmux", map[string]string{"KITTY_WINDOW_ID": "1"}, "kitty", "tmux", false},
		{"wezterm over iterm", map[string]string{"WEZTERM_UNIX_SOCKET": "/tmp/w"}, "wezterm", "iterm", true},
		{"wezterm not over kitty", map[string]string{"WEZTERM_UNIX_SOCKET": "/tmp/w"}, "wezterm", "kitty", false},
		{"iterm over pty", map[string]string{"TERM_PROGRAM": "iTerm.app"}, "iterm2", "pty", true},
		{"iterm not over wezterm", map[string]string{"TERM_PROGRAM": "iTerm.app"}, "iterm2", "wezterm", false},
		// Same backend or no environment match never promotes.
		{"same source", map[string]string{"DEVICETERM_SESSION": "s"}, "deviceterm", "deviceterm", false},
		{"env missing", map[string]string{}, "deviceterm", "pty", false},
		{"unknown name", map[string]string{}, "mystery", "pty", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, key := range []string{"TMUX", "KITTY_WINDOW_ID", "TERM_PROGRAM", "WEZTERM_UNIX_SOCKET", "DEVICETERM_SESSION"} {
				t.Setenv(key, "")
			}
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			if got := outranksPrimary(tt.enable, tt.current); got != tt.want {
				t.Errorf("outranksPrimary(%q, %q) = %v, want %v", tt.enable, tt.current, got, tt.want)
			}
		})
	}
}

func TestPrimaryRankMatchesStartupOrder(t *testing.T) {
	order := []string{"deviceterm", "tmux", "kitty", "wezterm", "iterm", "pty"}
	for i := 1; i < len(order); i++ {
		if primaryRank(order[i-1]) <= primaryRank(order[i]) {
			t.Errorf("%s should outrank %s", order[i-1], order[i])
		}
	}
	if primaryRank("unknown") != primaryRank("pty") {
		t.Errorf("unknown sources should rank with pty")
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
		{"wezterm", "wezterm:", "wezterm"},
		{"deviceterm", "deviceterm:", "deviceterm"},
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
