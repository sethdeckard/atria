package kitty

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/sethdeckard/atria/libatria/terminal"
)

func TestNewClientDefaults(t *testing.T) {
	c := NewClient(Options{})
	if c.kittenPath != "kitten" {
		t.Errorf("expected kittenPath %q, got %q", "kitten", c.kittenPath)
	}
}

func TestNewClientCustomPath(t *testing.T) {
	c := NewClient(Options{Path: "/usr/local/bin/kitten"})
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
	c := NewClient(Options{})
	pid, err := c.MonitorOutput("42", "/tmp/log", "pattern")
	if err == nil {
		t.Fatal("expected error from MonitorOutput")
	}
	if pid != 0 {
		t.Errorf("expected pid 0, got %d", pid)
	}
}

func TestLookupWindowVar(t *testing.T) {
	windows := []kittyWindow{
		{ID: 42, Title: "claude", CWD: "/tmp", PID: 1234},
		{ID: 99, Title: "codex", CWD: "/home", PID: 5678},
	}

	tests := []struct {
		name      string
		windows   []kittyWindow
		sessionID string
		varName   string
		want      string
		wantErr   bool
	}{
		{
			name:      "get cwd for known window",
			windows:   windows,
			sessionID: "42",
			varName:   "path",
			want:      "/tmp",
		},
		{
			name:      "get pid for known window",
			windows:   windows,
			sessionID: "42",
			varName:   "pid",
			want:      "1234",
		},
		{
			name:      "unknown window ID",
			windows:   windows,
			sessionID: "55",
			varName:   "path",
			wantErr:   true,
		},
		{
			name:      "unsupported variable",
			windows:   windows,
			sessionID: "42",
			varName:   "title",
			wantErr:   true,
		},
		{
			name:      "empty windows slice",
			windows:   []kittyWindow{},
			sessionID: "42",
			varName:   "path",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := lookupWindowVar(tt.windows, tt.sessionID, tt.varName)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("lookupWindowVar() = %q, want %q", got, tt.want)
			}
		})
	}
}

func writeFakeKitten(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kitten")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body), 0o755); err != nil {
		t.Fatalf("write fake kitten: %v", err)
	}
	return path
}

func TestRunWrapsConnectFailureAndTimeout(t *testing.T) {
	kitten := writeFakeKitten(t, `
case "$4" in
  ls) echo "Failed to connect to unix:/tmp/kitty-1: Connection refused" >&2; exit 1 ;;
  get-text) sleep 3 ;;
  focus-window) echo "No matching windows" >&2; exit 1 ;;
esac
exit 0
`)
	c := NewClient(Options{Path: kitten, CommandTimeout: time.Second})
	if _, err := c.ListSessions(); !errors.Is(err, terminal.ErrUnavailable) {
		t.Fatalf("connect failure = %v, want ErrUnavailable", err)
	}
	start := time.Now()
	if _, err := c.ReadScreen("1", 10); !errors.Is(err, terminal.ErrUnavailable) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout = %v, want ErrUnavailable and DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("timeout took %s; the pipe wait was not bounded", elapsed)
	}
	if err := c.FocusSession("1"); err == nil || errors.Is(err, terminal.ErrUnavailable) {
		t.Fatalf("remote-control error = %v, want a plain error", err)
	}
}

func TestListSessionsUsesInjectedTTYLookupConcurrently(t *testing.T) {
	kitten := writeFakeKitten(t, `
if [ "$4" = "ls" ]; then
  echo '[{"tabs":[{"windows":[{"id":7,"title":"claude","pid":4242,"cwd":"/p"}]}]}]'
fi
exit 0
`)
	c := NewClient(Options{Path: kitten})
	c.ttyForPID = func(pid int) string { return "/dev/ttys" + strconv.Itoa(pid) }
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sessions, err := c.ListSessions()
			if err != nil || len(sessions) != 1 || sessions[0].TTY != "/dev/ttys4242" {
				t.Errorf("ListSessions = %+v, %v", sessions, err)
			}
		}()
	}
	wg.Wait()
}
