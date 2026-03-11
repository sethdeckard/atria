package kitty

import (
	"strconv"
	"testing"
)

func TestNewClientDefaults(t *testing.T) {
	c := NewClient("")
	if c.kittenPath != "kitten" {
		t.Errorf("expected kittenPath %q, got %q", "kitten", c.kittenPath)
	}
}

func TestNewClientCustomPath(t *testing.T) {
	c := NewClient("/usr/local/bin/kitten")
	if c.kittenPath != "/usr/local/bin/kitten" {
		t.Errorf("expected kittenPath %q, got %q", "/usr/local/bin/kitten", c.kittenPath)
	}
}

func TestParseLSOutput(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantLen   int
		wantID    int
		wantTitle string
		wantCWD   string
		wantPID   int
	}{
		{
			name: "single OS window, single tab, single window",
			input: `[{"id": 1, "tabs": [{"id": 1, "windows": [
				{"id": 42, "title": "claude", "cwd": "/home/user/project", "pid": 1234,
				 "foreground_processes": [{"pid": 1234, "cmdline": ["zsh"], "cwd": "/home/user/project"}]}
			]}]}]`,
			wantLen:   1,
			wantID:    42,
			wantTitle: "claude",
			wantCWD:   "/home/user/project",
			wantPID:   1234,
		},
		{
			name: "multiple OS windows and tabs",
			input: `[
				{"id": 1, "tabs": [
					{"id": 1, "windows": [
						{"id": 10, "title": "tab1-win1", "cwd": "/tmp", "pid": 100}
					]},
					{"id": 2, "windows": [
						{"id": 20, "title": "tab2-win1", "cwd": "/tmp", "pid": 200},
						{"id": 21, "title": "tab2-win2", "cwd": "/tmp", "pid": 201}
					]}
				]},
				{"id": 2, "tabs": [
					{"id": 3, "windows": [
						{"id": 30, "title": "os2-win1", "cwd": "/home", "pid": 300}
					]}
				]}
			]`,
			wantLen:   4,
			wantID:    10,
			wantTitle: "tab1-win1",
		},
		{
			name:    "empty array",
			input:   `[]`,
			wantLen: 0,
		},
		{
			name:    "OS window with no tabs",
			input:   `[{"id": 1, "tabs": []}]`,
			wantLen: 0,
		},
		{
			name:    "tab with no windows",
			input:   `[{"id": 1, "tabs": [{"id": 1, "windows": []}]}]`,
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseLSOutput([]byte(tt.input))
			if err != nil {
				t.Fatalf("parseLSOutput() error: %v", err)
			}
			if len(got) != tt.wantLen {
				t.Fatalf("parseLSOutput() returned %d windows, want %d", len(got), tt.wantLen)
			}
			if tt.wantLen == 0 {
				return
			}
			if got[0].ID != tt.wantID {
				t.Errorf("ID = %d, want %d", got[0].ID, tt.wantID)
			}
			if tt.wantTitle != "" && got[0].Title != tt.wantTitle {
				t.Errorf("Title = %q, want %q", got[0].Title, tt.wantTitle)
			}
			if tt.wantCWD != "" && got[0].CWD != tt.wantCWD {
				t.Errorf("CWD = %q, want %q", got[0].CWD, tt.wantCWD)
			}
			if tt.wantPID != 0 && got[0].PID != tt.wantPID {
				t.Errorf("PID = %d, want %d", got[0].PID, tt.wantPID)
			}
		})
	}
}

func TestTTYForPID(t *testing.T) {
	// Invalid PID returns empty.
	if tty := ttyForPID(0); tty != "" {
		t.Errorf("expected empty TTY for PID 0, got %q", tty)
	}
	if tty := ttyForPID(-1); tty != "" {
		t.Errorf("expected empty TTY for PID -1, got %q", tty)
	}
	// Non-existent PID returns empty.
	if tty := ttyForPID(999999999); tty != "" {
		t.Errorf("expected empty TTY for non-existent PID, got %q", tty)
	}
}

func TestListSessionsPopulatesTTY(t *testing.T) {
	// Verify that parseLSOutput preserves PID so ListSessions can resolve TTY.
	input := `[{"id": 1, "tabs": [{"id": 1, "windows": [
		{"id": 42, "title": "claude", "cwd": "/tmp", "pid": 1234}
	]}]}]`
	windows, err := parseLSOutput([]byte(input))
	if err != nil {
		t.Fatalf("parseLSOutput() error: %v", err)
	}
	if len(windows) != 1 {
		t.Fatalf("expected 1 window, got %d", len(windows))
	}
	if windows[0].PID != 1234 {
		t.Errorf("PID = %d, want 1234", windows[0].PID)
	}
	if strconv.Itoa(windows[0].ID) != "42" {
		t.Errorf("ID = %d, want 42", windows[0].ID)
	}
}

func TestParseLSOutputInvalidJSON(t *testing.T) {
	_, err := parseLSOutput([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestMonitorOutputUnsupported(t *testing.T) {
	c := NewClient("")
	pid, err := c.MonitorOutput("42", "/tmp/log", "pattern")
	if err == nil {
		t.Fatal("expected error from MonitorOutput")
	}
	if pid != 0 {
		t.Errorf("expected pid 0, got %d", pid)
	}
}

func TestGetVarUnsupported(t *testing.T) {
	c := NewClient("")
	_, err := c.GetVar("42", "nonexistent")
	if err == nil {
		t.Fatal("expected error for unsupported variable")
	}
}
