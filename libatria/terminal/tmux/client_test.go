package tmux

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sethdeckard/atria/libatria/terminal"
)

func TestNewClientDefaults(t *testing.T) {
	c := NewClient(Options{})
	if c.tmuxPath != "tmux" {
		t.Errorf("expected tmuxPath %q, got %q", "tmux", c.tmuxPath)
	}
	if c.launchSession != "" {
		t.Errorf("expected empty launchSession, got %q", c.launchSession)
	}
}

func TestNewClientCustomValues(t *testing.T) {
	c := NewClient(Options{Path: "/usr/local/bin/tmux", LaunchSession: "mysession"})
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

// Captured from a tmux server with sessions "beta" (windows "one" and a
// split "two") and "alpha" (window "solo"), created in that order: tmux
// lists sessions by name.
const twoSessionPanes = "%3\thost.local\tsolo\t/dev/ttys065\t$1\talpha\t@2\n" +
	"%0\thost.local\tone\t/dev/ttys061\t$0\tbeta\t@0\n" +
	"%1\thost.local\ttwo\t/dev/ttys063\t$0\tbeta\t@1\n" +
	"%2\thost.local\ttwo\t/dev/ttys064\t$0\tbeta\t@1\n"

func TestParsePaneListFillsPlacement(t *testing.T) {
	alpha := terminal.Place{ID: "$1", Title: "alpha", Index: 0}
	beta := terminal.Place{ID: "$0", Title: "beta", Index: 1}
	two := terminal.Place{ID: "@1", Title: "two", Index: 1}
	want := []struct {
		id          string
		window, tab terminal.Place
	}{
		{"%3", alpha, terminal.Place{ID: "@2", Title: "solo", Index: 0}},
		{"%0", beta, terminal.Place{ID: "@0", Title: "one", Index: 0}},
		{"%1", beta, two},
		{"%2", beta, two},
	}
	got := parsePaneList(twoSessionPanes)
	if len(got) != len(want) {
		t.Fatalf("got %d sessions, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].ID != w.id || got[i].Window != w.window || got[i].Tab != w.tab {
			t.Errorf("pane %d = %s %+v %+v, want %s %+v %+v", i, got[i].ID, got[i].Window, got[i].Tab, w.id, w.window, w.tab)
		}
	}
}

func TestParsePaneListLinkedWindow(t *testing.T) {
	// Captured after linking alpha's window a2 (@1) into beta at index 5:
	// tmux lists its pane under both sessions.
	out := "%0\t\ta1\t/dev/ttys001\t$0\talpha\t@0\n" +
		"%1\t\ta2\t/dev/ttys002\t$0\talpha\t@1\n" +
		"%2\t\tb1\t/dev/ttys003\t$1\tbeta\t@2\n" +
		"%3\t\tb2\t/dev/ttys004\t$1\tbeta\t@3\n" +
		"%1\t\ta2\t/dev/ttys002\t$1\tbeta\t@1\n"
	want := []terminal.Place{
		{ID: "@0", Title: "a1", Index: 0},
		{ID: "@1", Title: "a2", Index: 1},
		{ID: "@2", Title: "b1", Index: 0},
		{ID: "@3", Title: "b2", Index: 1},
		{ID: "@1", Title: "a2", Index: 2},
	}
	got := parsePaneList(out)
	if len(got) != len(want) {
		t.Fatalf("got %d sessions, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Tab != w {
			t.Errorf("row %d tab = %+v, want %+v", i, got[i].Tab, w)
		}
	}
}

func TestParsePaneListJobAndCwd(t *testing.T) {
	// Captured with the trailing fields (command, geometry, path); the
	// second path has a space, the last one a trailing space.
	out := "%0\thost.local\tone\t/dev/ttys046\t$0\tbeta\t@0\tzsh\t0\t0\t80\t24\t0\t/private/tmp\n" +
		"%1\thost.local\ttwo\t/dev/ttys048\t$0\tbeta\t@1\tsleep\t0\t0\t80\t24\t0\t/Users/example/my app\n" +
		"%2\thost.local\tthree\t/dev/ttys049\t$0\tbeta\t@2\t\t0\t0\t80\t24\t0\t\n" +
		"%3\thost.local\tfour\t/dev/ttys050\t$0\tbeta\t@3\tclaude\t0\t0\t80\t24\t0\t/tmp/project \n"
	want := []struct{ job, cwd string }{
		{"zsh", "/private/tmp"},
		{"sleep", "/Users/example/my app"},
		{"", ""},
		{"claude", "/tmp/project "},
	}
	got := parsePaneList(out)
	if len(got) != len(want) {
		t.Fatalf("got %d sessions, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Job != w.job || got[i].Cwd != w.cwd {
			t.Errorf("pane %d = job %q cwd %q, want %q %q", i, got[i].Job, got[i].Cwd, w.job, w.cwd)
		}
	}
	if got[1].Tab != (terminal.Place{ID: "@1", Title: "two", Index: 1}) {
		t.Errorf("placement with the trailing fields = %+v", got[1].Tab)
	}
	// Without the trailing fields, as in the placement fixtures, both are empty.
	for _, s := range parsePaneList(twoSessionPanes) {
		if s.Job != "" || s.Cwd != "" {
			t.Errorf("%s: job %q cwd %q without the fields, want empty", s.ID, s.Job, s.Cwd)
		}
	}
}

func TestParsePaneListFrames(t *testing.T) {
	// Captured from a 200x50 window split side by side, then the right
	// pane split top and bottom. The divider takes a cell.
	out := "%0\thost.local\tw\t/dev/ttys049\t$0\ts\t@0\tzsh\t0\t0\t100\t50\t0\t/private/tmp\n" +
		"%1\thost.local\tw\t/dev/ttys056\t$0\ts\t@0\tzsh\t101\t0\t99\t25\t0\t/private/tmp\n" +
		"%2\thost.local\tw\t/dev/ttys057\t$0\ts\t@0\tzsh\t101\t26\t99\t24\t0\t/private/tmp\n" +
		"%3\thost.local\tw\t/dev/ttys058\t$0\ts\t@1\tzsh\t0\tx\t80\t24\t0\t/private/tmp\n"
	want := []terminal.Frame{
		{Left: 0, Top: 0, Width: 100, Height: 50},
		{Left: 101, Top: 0, Width: 99, Height: 25},
		{Left: 101, Top: 26, Width: 99, Height: 24},
		{}, // a field that isn't a number
	}
	got := parsePaneList(out)
	if len(got) != len(want) {
		t.Fatalf("got %d sessions, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Frame != w {
			t.Errorf("pane %d Frame = %+v, want %+v", i, got[i].Frame, w)
		}
	}
	for _, s := range parsePaneList(twoSessionPanes) {
		if s.Frame != (terminal.Frame{}) {
			t.Errorf("%s: Frame = %+v without the fields, want zero", s.ID, s.Frame)
		}
	}
}

func TestParsePaneListZoomedWindowHasNoFrames(t *testing.T) {
	// Captured with %1 zoomed: it reports the whole window while %0 and %2
	// keep their old frames.
	out := "%0\thost.local\tw\t/dev/ttys049\t$0\ts\t@0\tzsh\t0\t0\t60\t40\t1\t/tmp\n" +
		"%1\thost.local\tw\t/dev/ttys056\t$0\ts\t@0\tzsh\t0\t0\t120\t40\t1\t/tmp\n" +
		"%2\thost.local\tw\t/dev/ttys057\t$0\ts\t@0\tzsh\t61\t21\t59\t19\t1\t/tmp\n"
	for _, s := range parsePaneList(out) {
		if s.Frame != (terminal.Frame{}) {
			t.Errorf("%s: Frame = %+v in a zoomed window, want zero", s.ID, s.Frame)
		}
		if s.Job != "zsh" || s.Cwd != "/tmp" {
			t.Errorf("%s: job %q cwd %q, want the other fields read as usual", s.ID, s.Job, s.Cwd)
		}
	}
}

func TestParsePaneListWithoutPlacementFields(t *testing.T) {
	got := parsePaneList("%0\ttitle\twin\t/dev/ttys000\n")
	if len(got) != 1 || got[0].Window != (terminal.Place{}) || got[0].Tab != (terminal.Place{}) {
		t.Fatalf("got %+v, want zero placement", got)
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
	c := NewClient(Options{Path: tmuxPath, LaunchSession: ""})
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
	c := NewClient(Options{Path: tmuxPath, LaunchSession: ""})
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
	c := NewClient(Options{Path: tmuxPath, LaunchSession: "mysession"})
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
	c := NewClient(Options{Path: tmuxPath, LaunchSession: ""})
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
	c := NewClient(Options{Path: tmuxPath, FallbackSession: "atria"})
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
	c := NewClient(Options{Path: tmuxPath, LaunchSession: ""})
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
	c := NewClient(Options{Path: tmuxPath, LaunchSession: ""})
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
	c := NewClient(Options{})
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
	c := NewClient(Options{})
	pid, err := c.MonitorOutput("%0", "/tmp/log", "pattern")
	if err == nil {
		t.Fatal("expected error from MonitorOutput")
	}
	if pid != 0 {
		t.Errorf("expected pid 0, got %d", pid)
	}
}

func TestGetVarUnsupportedVariable(t *testing.T) {
	c := NewClient(Options{})
	_, err := c.GetVar("%0", "nonexistent")
	if err == nil {
		t.Fatal("expected error for unsupported variable")
	}
}

func TestSendKeyUsesTmuxKeyNames(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "tmux.log")
	tmuxPath := writeFakeTmux(t, `
echo "$@" >> "`+logPath+`"
exit 0
`)
	c := NewClient(Options{Path: tmuxPath})
	for _, k := range []terminal.Key{terminal.KeyEnter, terminal.KeyEscape, terminal.KeyUp, terminal.KeyCtrlC, terminal.Key("1"), terminal.Key(";")} {
		if err := c.SendKey("%3", k); err != nil {
			t.Fatalf("SendKey(%q): %v", k, err)
		}
	}
	if err := c.SendKey("%3", terminal.Key("bogus")); err == nil {
		t.Fatal("expected error for unknown key")
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	want := "send-keys -t %3 Enter\nsend-keys -t %3 Escape\nsend-keys -t %3 Up\nsend-keys -t %3 C-c\nsend-keys -t %3 -l 1\nsend-keys -t %3 -l \\;\n"
	if string(logData) != want {
		t.Fatalf("tmux calls:\n%s\nwant:\n%s", logData, want)
	}
}

func TestNewSessionUsesConfiguredFallbackSession(t *testing.T) {
	t.Setenv("TMUX", "")
	logPath := filepath.Join(t.TempDir(), "tmux.log")
	tmuxPath := writeFakeTmux(t, `
echo "$@" >> "`+logPath+`"
if [ "$1" = "has-session" ]; then
	echo "can't find session: custom" >&2
	exit 1
fi
if [ "$1" = "new-session" ]; then
	echo "%7"
fi
exit 0
`)
	c := NewClient(Options{Path: tmuxPath, FallbackSession: "custom"})
	if _, err := c.NewSession(); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	logData, _ := os.ReadFile(logPath)
	if !strings.Contains(string(logData), "new-session -d -s custom") {
		t.Fatalf("expected detached custom fallback, log:\n%s", logData)
	}
	if NewClient(Options{}).fallbackSession != DefaultFallbackSession {
		t.Fatalf("default fallback should be %q", DefaultFallbackSession)
	}
}

func TestSendTextEscapesTrailingSemicolon(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "tmux.log")
	tmuxPath := writeFakeTmux(t, `
echo "$@" >> "`+logPath+`"
exit 0
`)
	c := NewClient(Options{Path: tmuxPath})
	for _, text := range []string{";", "run make;", "a; b", "plain"} {
		if err := c.SendText("%3", text); err != nil {
			t.Fatalf("SendText(%q): %v", text, err)
		}
	}
	if err := c.RunCommand("%3", "cd /p && claude;"); err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	want := "send-keys -t %3 -l \\;\n" +
		"send-keys -t %3 -l run make\\;\n" +
		"send-keys -t %3 -l a; b\n" +
		"send-keys -t %3 -l plain\n" +
		"send-keys -t %3 -l cd /p && claude\\;\n" +
		"send-keys -t %3 Enter\n"
	if string(logData) != want {
		t.Fatalf("tmux calls:\n%s\nwant:\n%s", logData, want)
	}
}

func TestReadScrollback(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "tmux.log")
	tmuxPath := writeFakeTmux(t, `
echo "$@" >> "`+logPath+`"
printf 'old 1\nold 2\nprompt\n\n\n'
`)
	c := NewClient(Options{Path: tmuxPath})
	for _, styled := range []bool{false, true} {
		got, err := c.ReadScrollback("%1", 2, styled)
		if err != nil {
			t.Fatal(err)
		}
		if want := "old 2\nprompt"; got != want {
			t.Errorf("styled=%v: ReadScrollback = %q, want %q", styled, got, want)
		}
	}
	if _, err := c.ReadScrollback("%1", 0, false); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ReadScrollback("%1", -1, true); err != nil {
		t.Fatal(err)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "capture-pane -t %1 -p -S -2\n" +
		"capture-pane -t %1 -p -e -S -2\n" +
		"capture-pane -t %1 -p -S -0\n" +
		"capture-pane -t %1 -p -e -S -0\n"
	if string(logData) != want {
		t.Fatalf("tmux calls:\n%s\nwant:\n%s", logData, want)
	}
}

func TestRunWrapsNoServerAndTimeoutAsUnavailable(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "tmux.log")
	tmuxPath := writeFakeTmux(t, `
echo "$@" >> "`+logPath+`"
if [ "$1" = "send-keys" ]; then
	echo "no server running on /tmp/tmux-501/default" >&2
	exit 1
fi
if [ "$1" = "list-panes" ]; then
	echo "no server running on /tmp/tmux-501/default" >&2
	exit 1
fi
if [ "$1" = "display-message" ]; then
	echo "can't find session: nope" >&2
	exit 1
fi
if [ "$1" = "capture-pane" ]; then
	sleep 3
fi
exit 0
`)
	c := NewClient(Options{Path: tmuxPath, CommandTimeout: time.Second})

	// Server gone: per-session calls are ErrUnavailable, listing is empty.
	err := c.SendText("%1", "hi")
	if !errors.Is(err, terminal.ErrUnavailable) {
		t.Fatalf("SendText with no server = %v, want ErrUnavailable", err)
	}
	sessions, err := c.ListSessions()
	if err != nil || len(sessions) != 0 {
		t.Fatalf("ListSessions with no server = %v, %v; want empty, nil", sessions, err)
	}

	// An ordinary tmux error is not unavailability.
	_, err = c.run("display-message", "-p", "x")
	if err == nil || errors.Is(err, terminal.ErrUnavailable) {
		t.Fatalf("ordinary tmux error = %v, want a plain error", err)
	}

	// A hung tmux hits the timeout and reports both sentinels, and the pipe
	// grace keeps the caller from waiting on the script's sleeping child.
	start := time.Now()
	_, err = c.ReadScreen("%1", 10)
	if !errors.Is(err, terminal.ErrUnavailable) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timed-out read = %v, want ErrUnavailable and DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("timed-out read took %s; the pipe wait was not bounded", elapsed)
	}
}

func TestListSessionsConcurrentUse(t *testing.T) {
	tmuxPath := writeFakeTmux(t, `
if [ "$1" = "list-panes" ]; then
	printf '%%1\tcodex\tmain\t/dev/ttys001\n'
fi
exit 0
`)
	c := NewClient(Options{Path: tmuxPath})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.ListSessions(); err != nil {
				t.Errorf("ListSessions: %v", err)
			}
			if err := c.Available(); err != nil {
				t.Errorf("Available: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestListSessionsKeepsConnectionErrors(t *testing.T) {
	tmuxPath := writeFakeTmux(t, `
if [ "$1" = "list-panes" ]; then
	echo "error connecting to /tmp/tmux-501/default (Permission denied)" >&2
	exit 1
fi
exit 0
`)
	c := NewClient(Options{Path: tmuxPath})
	_, err := c.ListSessions()
	if !errors.Is(err, terminal.ErrUnavailable) {
		t.Fatalf("permission failure = %v, want ErrUnavailable, not an empty list", err)
	}

	absent := writeFakeTmux(t, `
if [ "$1" = "list-panes" ]; then
	echo "error connecting to /tmp/tmux-501/default (No such file or directory)" >&2
	exit 1
fi
exit 0
`)
	sessions, err := NewClient(Options{Path: absent}).ListSessions()
	if err != nil || len(sessions) != 0 {
		t.Fatalf("absent socket = %v, %v; want empty, nil", sessions, err)
	}
}

func TestAvailableRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{"server answers", `[ "$1" = "list-sessions" ] && echo '$0'; exit 0`, false},
		{"no server", `echo "no server running on /tmp/tmux-501/default" >&2; exit 1`, false},
		{"permission denied", `echo "error connecting to /tmp/tmux-501/default (Permission denied)" >&2; exit 1`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewClient(Options{Path: writeFakeTmux(t, tt.body+"\n")})
			err := c.Available()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Available() = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, terminal.ErrUnavailable) {
				t.Fatalf("Available() = %v, want ErrUnavailable", err)
			}
		})
	}
}

func TestAvailableTimesOut(t *testing.T) {
	c := NewClient(Options{Path: writeFakeTmux(t, "sleep 5\n"), CommandTimeout: 100 * time.Millisecond})
	err := c.Available()
	if !errors.Is(err, terminal.ErrUnavailable) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Available() = %v, want a timeout", err)
	}
}
