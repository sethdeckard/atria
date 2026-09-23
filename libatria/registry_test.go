package libatria

import "testing"

func TestNamesSourcePrefix(t *testing.T) {
	want := []string{"deviceterm", "tmux", "kitty", "wezterm", "iterm2"}
	got := Names()
	if len(got) != len(want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Names()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	got[0] = "mutated"
	if Names()[0] != "deviceterm" {
		t.Error("Names() should return a copy")
	}

	tests := []struct{ name, source, prefix string }{
		{"iterm2", "iterm", "iterm:"},
		{"tmux", "tmux", "tmux:"},
		{"kitty", "kitty", "kitty:"},
		{"wezterm", "wezterm", "wezterm:"},
		{"deviceterm", "deviceterm", "deviceterm:"},
		{"pty", "pty", "pty:"},
		{"unknown", "unknown", "unknown:"},
	}
	for _, tt := range tests {
		if got := Source(tt.name); got != tt.source {
			t.Errorf("Source(%q) = %q, want %q", tt.name, got, tt.source)
		}
		if got := Prefix(tt.name); got != tt.prefix {
			t.Errorf("Prefix(%q) = %q, want %q", tt.name, got, tt.prefix)
		}
	}
}

func TestRankFollowsPrecedence(t *testing.T) {
	order := []string{"deviceterm", "tmux", "kitty", "wezterm", "iterm", "pty"}
	for i := 1; i < len(order); i++ {
		if Rank(order[i-1]) <= Rank(order[i]) {
			t.Errorf("%s should outrank %s", order[i-1], order[i])
		}
	}
	if Rank("unknown") != Rank("pty") || Rank("pty") != 0 {
		t.Errorf("unknown sources and pty should rank 0")
	}
	// Names order and Rank order agree.
	for i, name := range Names() {
		if i > 0 && Rank(Source(Names()[i-1])) <= Rank(Source(name)) {
			t.Errorf("Names() order disagrees with Rank at %q", name)
		}
	}
}

func TestEnvMatches(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"tmux", map[string]string{"TMUX": "/tmp/tmux-501/default,123,0"}, true},
		{"tmux", map[string]string{}, false},
		{"kitty", map[string]string{"KITTY_WINDOW_ID": "1"}, true},
		{"wezterm", map[string]string{"TERM_PROGRAM": "WezTerm"}, true},
		{"wezterm", map[string]string{"WEZTERM_UNIX_SOCKET": "/tmp/w"}, true},
		{"wezterm", map[string]string{"TERM_PROGRAM": "iTerm.app"}, false},
		{"iterm2", map[string]string{"TERM_PROGRAM": "iTerm.app"}, true},
		{"iterm2", map[string]string{"TERM_PROGRAM": "WezTerm"}, false},
		{"deviceterm", map[string]string{"DEVICETERM_SESSION": "s"}, true},
		{"deviceterm", map[string]string{}, false},
		{"pty", map[string]string{"TMUX": "x"}, false},
		{"mystery", map[string]string{"TMUX": "x"}, false},
	}
	for _, tt := range tests {
		getenv := func(k string) string { return tt.env[k] }
		if got := EnvMatches(tt.name, getenv); got != tt.want {
			t.Errorf("EnvMatches(%q, %v) = %v, want %v", tt.name, tt.env, got, tt.want)
		}
	}
	// nil getenv reads the process environment.
	t.Setenv("KITTY_WINDOW_ID", "7")
	if !EnvMatches("kitty", nil) {
		t.Error("nil getenv should consult os.Getenv")
	}
}
