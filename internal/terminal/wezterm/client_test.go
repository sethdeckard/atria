package wezterm

import (
	"testing"
)

func TestNewClientDefaults(t *testing.T) {
	c := NewClient("")
	if c.weztermPath != "wezterm" {
		t.Errorf("expected weztermPath %q, got %q", "wezterm", c.weztermPath)
	}
}

func TestNewClientCustomPath(t *testing.T) {
	c := NewClient("/usr/local/bin/wezterm")
	if c.weztermPath != "/usr/local/bin/wezterm" {
		t.Errorf("expected weztermPath %q, got %q", "/usr/local/bin/wezterm", c.weztermPath)
	}
}

func TestParseListOutput(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantLen   int
		wantID    int
		wantTitle string
		wantCWD   string
		wantTTY   string
	}{
		{
			name: "single pane",
			input: `[{"window_id": 0, "tab_id": 0, "pane_id": 1, "workspace": "default",
				"title": "claude", "cwd": "/home/user/project", "tty_name": "/dev/pts/0"}]`,
			wantLen:   1,
			wantID:    1,
			wantTitle: "claude",
			wantCWD:   "/home/user/project",
			wantTTY:   "/dev/pts/0",
		},
		{
			name: "multiple panes",
			input: `[
				{"window_id": 0, "tab_id": 0, "pane_id": 1, "workspace": "default",
				 "title": "claude", "cwd": "/tmp", "tty_name": "/dev/pts/0"},
				{"window_id": 0, "tab_id": 1, "pane_id": 2, "workspace": "default",
				 "title": "codex", "cwd": "/home", "tty_name": "/dev/pts/1"},
				{"window_id": 1, "tab_id": 2, "pane_id": 3, "workspace": "default",
				 "title": "zsh", "cwd": "/var", "tty_name": "/dev/pts/2"}
			]`,
			wantLen:   3,
			wantID:    1,
			wantTitle: "claude",
		},
		{
			name:    "empty array",
			input:   `[]`,
			wantLen: 0,
		},
		{
			name: "missing optional fields",
			input: `[{"pane_id": 5, "title": "shell", "cwd": "", "tty_name": ""}]`,
			wantLen:   1,
			wantID:    5,
			wantTitle: "shell",
		},
		{
			name: "CWD with file:// URI",
			input: `[{"pane_id": 1, "title": "claude", "cwd": "file:///Users/test/project",
				"tty_name": "/dev/ttys001"}]`,
			wantLen: 1,
			wantID:  1,
			wantCWD: "file:///Users/test/project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseListOutput([]byte(tt.input))
			if err != nil {
				t.Fatalf("parseListOutput() error: %v", err)
			}
			if len(got) != tt.wantLen {
				t.Fatalf("parseListOutput() returned %d entries, want %d", len(got), tt.wantLen)
			}
			if tt.wantLen == 0 {
				return
			}
			if got[0].PaneID != tt.wantID {
				t.Errorf("PaneID = %d, want %d", got[0].PaneID, tt.wantID)
			}
			if tt.wantTitle != "" && got[0].Title != tt.wantTitle {
				t.Errorf("Title = %q, want %q", got[0].Title, tt.wantTitle)
			}
			if tt.wantCWD != "" && got[0].CWD != tt.wantCWD {
				t.Errorf("CWD = %q, want %q", got[0].CWD, tt.wantCWD)
			}
			if tt.wantTTY != "" && got[0].TTYName != tt.wantTTY {
				t.Errorf("TTYName = %q, want %q", got[0].TTYName, tt.wantTTY)
			}
		})
	}
}

func TestParseListOutputInvalidJSON(t *testing.T) {
	_, err := parseListOutput([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestNormalizeCWD(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain path", "/Users/test/project", "/Users/test/project"},
		{"file URI", "file:///Users/test/project", "/Users/test/project"},
		{"file URI with hostname", "file://localhost/Users/test/project", "/Users/test/project"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeCWD(tt.input)
			if got != tt.want {
				t.Errorf("normalizeCWD(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTrimToLastN(t *testing.T) {
	tests := []struct {
		name  string
		input string
		n     int
		want  string
	}{
		{"fewer lines than n", "a\nb\nc", 5, "a\nb\nc"},
		{"exact lines", "a\nb\nc", 3, "a\nb\nc"},
		{"more lines than n", "a\nb\nc\nd\ne", 3, "c\nd\ne"},
		{"single line", "hello", 3, "hello"},
		{"empty string", "", 3, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := trimToLastN(tt.input, tt.n)
			if got != tt.want {
				t.Errorf("trimToLastN(%q, %d) = %q, want %q", tt.input, tt.n, got, tt.want)
			}
		})
	}
}

func TestMonitorOutputUnsupported(t *testing.T) {
	c := NewClient("")
	pid, err := c.MonitorOutput("1", "/tmp/log", "pattern")
	if err == nil {
		t.Fatal("expected error from MonitorOutput")
	}
	if pid != 0 {
		t.Errorf("expected pid 0, got %d", pid)
	}
}

func TestGetVarUnsupported(t *testing.T) {
	c := NewClient("")
	_, err := c.GetVar("1", "title")
	if err == nil {
		t.Fatal("expected error for unsupported variable")
	}
}
