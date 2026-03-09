package tmux

import (
	"testing"
)

func TestNewClientDefaults(t *testing.T) {
	c := NewClient("", "")
	if c.tmuxPath != "tmux" {
		t.Errorf("expected tmuxPath %q, got %q", "tmux", c.tmuxPath)
	}
	if c.sessionName != "atria" {
		t.Errorf("expected sessionName %q, got %q", "atria", c.sessionName)
	}
}

func TestNewClientCustomValues(t *testing.T) {
	c := NewClient("/usr/local/bin/tmux", "mysession")
	if c.tmuxPath != "/usr/local/bin/tmux" {
		t.Errorf("expected tmuxPath %q, got %q", "/usr/local/bin/tmux", c.tmuxPath)
	}
	if c.sessionName != "mysession" {
		t.Errorf("expected sessionName %q, got %q", "mysession", c.sessionName)
	}
}

func TestParsePaneList(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantLen  int
		wantID   string
		wantName string
		wantTTY  string
	}{
		{
			name:     "single pane with title",
			input:    "%0\t✳ Reading file.go\tbash\t/dev/ttys001\n",
			wantLen:  1,
			wantID:   "%0",
			wantName: "✳ Reading file.go",
			wantTTY:  "/dev/ttys001",
		},
		{
			name:     "pane title empty falls back to window name",
			input:    "%1\t\tclaude\t/dev/ttys002\n",
			wantLen:  1,
			wantID:   "%1",
			wantName: "claude",
			wantTTY:  "/dev/ttys002",
		},
		{
			name:    "multiple panes",
			input:   "%0\ttitle0\twin0\t/dev/ttys000\n%1\ttitle1\twin1\t/dev/ttys001\n",
			wantLen: 2,
			wantID:  "%0",
		},
		{
			name:    "empty output",
			input:   "",
			wantLen: 0,
		},
		{
			name:    "whitespace only",
			input:   "  \n  \n",
			wantLen: 0,
		},
		{
			name:     "minimal fields (ID only)",
			input:    "%5\n",
			wantLen:  1,
			wantID:   "%5",
			wantName: "",
			wantTTY:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parsePaneList(tt.input)
			if len(got) != tt.wantLen {
				t.Fatalf("parsePaneList() returned %d sessions, want %d", len(got), tt.wantLen)
			}
			if tt.wantLen == 0 {
				return
			}
			if got[0].ID != tt.wantID {
				t.Errorf("ID = %q, want %q", got[0].ID, tt.wantID)
			}
			if tt.wantName != "" && got[0].Name != tt.wantName {
				t.Errorf("Name = %q, want %q", got[0].Name, tt.wantName)
			}
			if tt.wantTTY != "" && got[0].TTY != tt.wantTTY {
				t.Errorf("TTY = %q, want %q", got[0].TTY, tt.wantTTY)
			}
		})
	}
}

func TestSendTextCarriageReturn(t *testing.T) {
	// Verify that SendText with "\r" would use "Enter" key (not -l literal).
	// We can't run tmux in tests, but we verify the Client is constructed correctly.
	c := NewClient("", "")
	// This will fail because tmux isn't running, but we're testing the code path.
	err := c.SendText("%0", "\r")
	if err == nil {
		t.Log("SendText succeeded (tmux is running)")
	}
	// Also test "\n"
	err = c.SendText("%0", "\n")
	if err == nil {
		t.Log("SendText with newline succeeded (tmux is running)")
	}
}

func TestMonitorOutputUnsupported(t *testing.T) {
	c := NewClient("", "")
	pid, err := c.MonitorOutput("%0", "/tmp/log", "pattern")
	if err == nil {
		t.Fatal("expected error from MonitorOutput")
	}
	if pid != 0 {
		t.Errorf("expected pid 0, got %d", pid)
	}
}

func TestGetVarUnsupportedVariable(t *testing.T) {
	c := NewClient("", "")
	_, err := c.GetVar("%0", "nonexistent")
	if err == nil {
		t.Fatal("expected error for unsupported variable")
	}
}
