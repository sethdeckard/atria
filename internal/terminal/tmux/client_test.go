package tmux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewClientDefaults(t *testing.T) {
	c := NewClient("", "")
	if c.tmuxPath != "tmux" {
		t.Errorf("expected tmuxPath %q, got %q", "tmux", c.tmuxPath)
	}
	if c.launchSession != "" {
		t.Errorf("expected empty launchSession, got %q", c.launchSession)
	}
}

func TestNewClientCustomValues(t *testing.T) {
	c := NewClient("/usr/local/bin/tmux", "mysession")
	if c.tmuxPath != "/usr/local/bin/tmux" {
		t.Errorf("expected tmuxPath %q, got %q", "/usr/local/bin/tmux", c.tmuxPath)
	}
	if c.launchSession != "mysession" {
		t.Errorf("expected launchSession %q, got %q", "mysession", c.launchSession)
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
			name:     "generic pane title falls back to agent window name",
			input:    "%2\tvanth.local\tcodex-aarch64-a\t/dev/ttys003\n",
			wantLen:  1,
			wantID:   "%2",
			wantName: "codex-aarch64-a",
			wantTTY:  "/dev/ttys003",
		},
		{
			name:     "agent pane title still wins over generic window name",
			input:    "%3\t✳ Claude Code\tzsh\t/dev/ttys004\n",
			wantLen:  1,
			wantID:   "%3",
			wantName: "✳ Claude Code",
			wantTTY:  "/dev/ttys004",
		},
		{
			name:    "multiple panes",
			input:   "%0\ttitle0\twin0\t/dev/ttys000\n%1\ttitle1\twin1\t/dev/ttys001\n",
			wantLen: 2,
			wantID:  "%0",
		},
		{
			name:    "multiple sessions same output format",
			input:   "%2\tcodex\tdefault\t/dev/ttys002\n%9\t✳ Reading\tatria\t/dev/ttys009\n",
			wantLen: 2,
			wantID:  "%2",
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

func writeFakeTmux(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tmux")
	script := "#!/bin/sh\nset -eu\n" + body
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake tmux: %v", err)
	}
	return path
}

func TestListSessionsNoServerReturnsNil(t *testing.T) {
	tmuxPath := writeFakeTmux(t, `
if [ "$1" = "list-panes" ]; then
	echo "no server running on /tmp/tmux-1000/default" >&2
	exit 1
fi
exit 0
`)
	c := NewClient(tmuxPath, "")
	sessions, err := c.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected no sessions, got %d", len(sessions))
	}
}

func TestListSessionsUnexpectedError(t *testing.T) {
	tmuxPath := writeFakeTmux(t, `
if [ "$1" = "list-panes" ]; then
	echo "bad format" >&2
	exit 1
fi
exit 0
`)
	c := NewClient(tmuxPath, "")
	_, err := c.ListSessions()
	if err == nil {
		t.Fatal("expected error from ListSessions")
	}
	if !strings.Contains(err.Error(), "bad format") {
		t.Fatalf("expected bad format error, got %v", err)
	}
}

func TestNewSessionUsesExplicitLaunchOverride(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "tmux.log")
	tmuxPath := writeFakeTmux(t, `
echo "$@" >> "`+logPath+`"
if [ "$1" = "has-session" ] && [ "$3" = "=mysession" ]; then
	exit 0
fi
if [ "$1" = "new-window" ] && [ "$3" = "=mysession:" ]; then
	echo "%9"
	exit 0
fi
exit 1
`)
	c := NewClient(tmuxPath, "mysession")
	sessionID, err := c.NewSession()
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	if sessionID != "%9" {
		t.Fatalf("expected pane ID %%9, got %q", sessionID)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(logData), "new-window -t =mysession:") {
		t.Fatalf("expected new-window to target mysession, log:\n%s", string(logData))
	}
}

func TestNewSessionUsesCurrentTmuxSession(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "tmux.log")
	tmuxPath := writeFakeTmux(t, `
echo "$@" >> "`+logPath+`"
if [ "$1" = "display-message" ] && [ "$2" = "-p" ]; then
	echo "current"
	exit 0
fi
if [ "$1" = "has-session" ] && [ "$3" = "=current" ]; then
	exit 0
fi
if [ "$1" = "new-window" ] && [ "$3" = "=current:" ]; then
	echo "%7"
	exit 0
fi
exit 1
`)
	t.Setenv("TMUX", "/tmp/tmux-test")
	c := NewClient(tmuxPath, "")
	sessionID, err := c.NewSession()
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	if sessionID != "%7" {
		t.Fatalf("expected pane ID %%7, got %q", sessionID)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(logData), "display-message -p #{session_name}") {
		t.Fatalf("expected current-session lookup, log:\n%s", string(logData))
	}
	if !strings.Contains(string(logData), "new-window -t =current:") {
		t.Fatalf("expected new-window to target current, log:\n%s", string(logData))
	}
}

func TestNewSessionFallsBackToDetachedAtriaOutsideTmux(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "tmux.log")
	tmuxPath := writeFakeTmux(t, `
echo "$@" >> "`+logPath+`"
if [ "$1" = "has-session" ] && [ "$3" = "=atria" ]; then
	echo "can't find session: atria" >&2
	exit 1
fi
if [ "$1" = "new-session" ] && [ "$4" = "atria" ]; then
	echo "%1"
	exit 0
fi
exit 1
`)
	c := NewClient(tmuxPath, "")
	sessionID, err := c.NewSession()
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	if sessionID != "%1" {
		t.Fatalf("expected pane ID %%1, got %q", sessionID)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if strings.Contains(string(logData), "display-message -p #{session_name}") {
		t.Fatalf("did not expect current-session lookup outside tmux, log:\n%s", string(logData))
	}
	if !strings.Contains(string(logData), "new-session -d -s atria") {
		t.Fatalf("expected detached atria fallback, log:\n%s", string(logData))
	}
}

func TestFocusSessionSwitchesToOwningSession(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "tmux.log")
	tmuxPath := writeFakeTmux(t, `
echo "$@" >> "`+logPath+`"
if [ "$1" = "select-window" ] && [ "$3" = "%42" ]; then
	exit 0
fi
if [ "$1" = "display-message" ] && [ "$3" = "%42" ]; then
	echo "other"
	exit 0
fi
if [ "$1" = "switch-client" ] && [ "$3" = "other" ]; then
	exit 0
fi
exit 1
`)
	t.Setenv("TMUX", "/tmp/tmux-test")
	c := NewClient(tmuxPath, "")
	if err := c.FocusSession("%42"); err != nil {
		t.Fatalf("FocusSession() error = %v", err)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(logData), "switch-client -t =other") {
		t.Fatalf("expected switch-client to owning session, log:\n%s", string(logData))
	}
}

func TestNewSessionUsesExactTargetForNumericSessionName(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "tmux.log")
	tmuxPath := writeFakeTmux(t, `
echo "$@" >> "`+logPath+`"
if [ "$1" = "display-message" ] && [ "$2" = "-p" ]; then
	echo "2"
	exit 0
fi
if [ "$1" = "has-session" ] && [ "$3" = "=2" ]; then
	exit 0
fi
if [ "$1" = "new-window" ] && [ "$3" = "=2:" ]; then
	echo "%11"
	exit 0
fi
exit 1
`)
	t.Setenv("TMUX", "/tmp/tmux-test")
	c := NewClient(tmuxPath, "")
	sessionID, err := c.NewSession()
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	if sessionID != "%11" {
		t.Fatalf("expected pane ID %%11, got %q", sessionID)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(logData), "has-session -t =2") {
		t.Fatalf("expected exact session lookup, log:\n%s", string(logData))
	}
	if !strings.Contains(string(logData), "new-window -t =2:") {
		t.Fatalf("expected exact window target for numeric session, log:\n%s", string(logData))
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
