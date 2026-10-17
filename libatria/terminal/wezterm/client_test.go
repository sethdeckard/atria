package wezterm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sethdeckard/atria/libatria/terminal"
)

func TestNewClientDefaults(t *testing.T) {
	c := NewClient(Options{})
	if c.weztermPath != "wezterm" {
		t.Errorf("expected weztermPath %q, got %q", "wezterm", c.weztermPath)
	}
}

func TestNewClientCustomPath(t *testing.T) {
	c := NewClient(Options{Path: "/usr/local/bin/wezterm"})
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
			name:      "missing optional fields",
			input:     `[{"pane_id": 5, "title": "shell", "cwd": "", "tty_name": ""}]`,
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

func TestSessionsFromEntriesFillsPlacement(t *testing.T) {
	// Window 0 holds a split tab without its own title and a titled tab;
	// window 3 holds one tab.
	data := `[
	  {"window_id":0,"tab_id":0,"pane_id":0,"title":"✳ Claude Code","tty_name":"/dev/ttys001","window_title":"✳ Claude Code","tab_title":"","is_active":false,
	   "left_col":0,"top_row":0,"size":{"rows":40,"cols":80,"pixel_width":640,"pixel_height":800,"dpi":72}},
	  {"window_id":0,"tab_id":0,"pane_id":1,"title":"zsh","tty_name":"/dev/ttys002","window_title":"✳ Claude Code","tab_title":"","is_active":true,
	   "left_col":81,"top_row":0,"size":{"rows":40,"cols":79,"pixel_width":632,"pixel_height":800,"dpi":72}},
	  {"window_id":0,"tab_id":4,"pane_id":5,"title":"htop","tty_name":"/dev/ttys003","window_title":"✳ Claude Code","tab_title":"monitor","is_active":true},
	  {"window_id":3,"tab_id":7,"pane_id":8,"title":"vim","tty_name":"/dev/ttys004","window_title":"vim","tab_title":"","is_active":true,"cwd":"file://host/Users/example/my%20app"}
	]`
	entries, err := parseListOutput([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	got := sessionsFromEntries(entries)

	w0 := terminal.Place{ID: "0", Title: "✳ Claude Code", Index: 0}
	split := terminal.Place{ID: "0", Title: "zsh", Index: 0}
	want := []struct {
		id          string
		window, tab terminal.Place
	}{
		{"0", w0, split},
		{"1", w0, split},
		{"5", w0, terminal.Place{ID: "4", Title: "monitor", Index: 1}},
		{"8", terminal.Place{ID: "3", Title: "vim", Index: 1}, terminal.Place{ID: "7", Title: "vim", Index: 0}},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d sessions, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].ID != w.id || got[i].Window != w.window || got[i].Tab != w.tab {
			t.Errorf("session %d = %s %+v %+v, want %s %+v %+v", i, got[i].ID, got[i].Window, got[i].Tab, w.id, w.window, w.tab)
		}
	}
	if got[3].Cwd != "/Users/example/my app" {
		t.Errorf("Cwd from a file:// URI = %q, want the decoded path", got[3].Cwd)
	}
	if got[0].Cwd != "" {
		t.Errorf("Cwd without a cwd field = %q, want empty", got[0].Cwd)
	}
	if got[0].Frame != (terminal.Frame{Width: 80, Height: 40}) || got[1].Frame != (terminal.Frame{Left: 81, Width: 79, Height: 40}) {
		t.Errorf("split frames = %+v, %+v", got[0].Frame, got[1].Frame)
	}
	if got[2].Frame != (terminal.Frame{}) {
		t.Errorf("Frame without a size = %+v, want zero", got[2].Frame)
	}
}

func TestSessionsFromEntriesOlderWezTerm(t *testing.T) {
	// Before window_title, tab_title, and is_active: ids and order only.
	entries, err := parseListOutput([]byte(`[{"window_id":2,"tab_id":5,"pane_id":9,"title":"zsh"}]`))
	if err != nil {
		t.Fatal(err)
	}
	got := sessionsFromEntries(entries)
	if len(got) != 1 || got[0].Window != (terminal.Place{ID: "2"}) || got[0].Tab != (terminal.Place{ID: "5"}) {
		t.Fatalf("got %+v, want untitled window 2 and tab 5", got)
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
	c := NewClient(Options{})
	pid, err := c.MonitorOutput("1", "/tmp/log", "pattern")
	if err == nil {
		t.Fatal("expected error from MonitorOutput")
	}
	if pid != 0 {
		t.Errorf("expected pid 0, got %d", pid)
	}
}

func TestGetVarUnsupported(t *testing.T) {
	c := NewClient(Options{})
	_, err := c.GetVar("1", "title")
	if err == nil {
		t.Fatal("expected error for unsupported variable")
	}
}

func writeFakeWezterm(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wezterm")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body), 0o755); err != nil {
		t.Fatalf("write fake wezterm: %v", err)
	}
	return path
}

func TestRunWrapsConnectFailureAndTimeout(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "wezterm.log")
	wez := writeFakeWezterm(t, `
echo "$@" >> "`+logPath+`"
case "$2" in
  list) echo "failed to connect to /tmp/wezterm/gui-sock: No such file or directory" >&2; exit 1 ;;
  get-text) sleep 3 ;;
  activate-pane) echo "pane 9 not found" >&2; exit 1 ;;
  send-text) cat >> "`+logPath+`" ;;
esac
exit 0
`)
	c := NewClient(Options{Path: wez, CommandTimeout: time.Second})
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
	if err := c.FocusSession("9"); err == nil || errors.Is(err, terminal.ErrUnavailable) {
		t.Fatalf("per-pane error = %v, want a plain error", err)
	}
	if err := c.SendText("1", "hello"); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	logData, _ := os.ReadFile(logPath)
	if !strings.Contains(string(logData), "send-text --pane-id 1 --no-paste") || !strings.Contains(string(logData), "hello") {
		t.Fatalf("send-text should pass text on stdin, log:\n%s", logData)
	}
}

func TestStripLogPrefix(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"wezterm's log line", "22:12:41.878  ERROR  wezterm > failed to connect to /tmp/sock", "wezterm > failed to connect to /tmp/sock"},
		{"every line", "22:12:41.878  ERROR  one\n22:12:41.901  WARN   two", "one\ntwo"},
		{"no prefix", "pane 9 not found", "pane 9 not found"},
		{"a time later in the line stays", "failed at 22:12:41.878 ERROR here", "failed at 22:12:41.878 ERROR here"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripLogPrefix(tt.in); got != tt.want {
				t.Errorf("stripLogPrefix(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestUnavailableReasonIgnoresLogTimestamp(t *testing.T) {
	countPath := filepath.Join(t.TempDir(), "count")
	wez := writeFakeWezterm(t, `
n=$(cat "`+countPath+`" 2>/dev/null || echo 0); n=$((n+1)); echo "$n" > "`+countPath+`"
echo "22:12:4$n.878  ERROR  wezterm > failed to connect to /tmp/wezterm/gui-sock: Connection refused" >&2
exit 1
`)
	c := NewClient(Options{Path: wez, CommandTimeout: time.Second})
	_, first := c.ListSessions()
	_, second := c.ListSessions()
	if !errors.Is(first, terminal.ErrUnavailable) || first.Error() != second.Error() {
		t.Fatalf("reasons differ between runs: %q and %q", first, second)
	}
	if strings.Contains(first.Error(), "22:12") {
		t.Fatalf("reason kept wezterm's timestamp: %q", first)
	}
}
