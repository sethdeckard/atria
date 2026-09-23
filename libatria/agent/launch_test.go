package agent

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sethdeckard/atria/libatria/terminal"
)

// recorder is a terminal.Backend that records the calls Launch and
// SendPrompt make. launcher wraps it with NewSessionOn.
type recorder struct {
	newID   string
	newErr  error
	runErr  error
	sendErr error
	calls   []string
}

func (r *recorder) Available() error                          { return nil }
func (r *recorder) ListSessions() ([]terminal.Session, error) { return nil, nil }
func (r *recorder) NewSession() (string, error) {
	r.calls = append(r.calls, "new")
	return r.newID, r.newErr
}
func (r *recorder) SendText(id, text string) error {
	r.calls = append(r.calls, "send:"+id+":"+text)
	return r.sendErr
}
func (r *recorder) RunCommand(id, cmd string) error {
	r.calls = append(r.calls, "run:"+id+":"+cmd)
	return r.runErr
}
func (r *recorder) FocusSession(id string) error {
	r.calls = append(r.calls, "focus:"+id)
	return nil
}
func (r *recorder) ReadScreen(string, int) (string, error)            { return "", nil }
func (r *recorder) GetVar(string, string) (string, error)             { return "", nil }
func (r *recorder) MonitorOutput(string, string, string) (int, error) { return 0, nil }

type launcher struct{ *recorder }

func (l launcher) NewSessionOn(source string) (string, error) {
	l.calls = append(l.calls, "newOn:"+source)
	return l.newID, l.newErr
}

var _ terminal.SourceLauncher = launcher{}

// stubSleep replaces the sleep seam for the test and records the durations.
func stubSleep(t *testing.T) *[]time.Duration {
	t.Helper()
	var slept []time.Duration
	prev := sleep
	sleep = func(d time.Duration) { slept = append(slept, d) }
	t.Cleanup(func() { sleep = prev })
	return &slept
}

func TestShellQuote(t *testing.T) {
	tests := []struct{ in, want string }{
		{"/simple/path", "'/simple/path'"},
		{"/path with spaces/dir", "'/path with spaces/dir'"},
		{"/it's/here", `'/it'"'"'s/here'`},
		{"", "''"},
		{"a'b'c", `'a'"'"'b'"'"'c'`},
	}
	for _, tc := range tests {
		if got := ShellQuote(tc.in); got != tc.want {
			t.Errorf("ShellQuote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLaunchCommand(t *testing.T) {
	tests := []struct {
		name string
		dir  string
		typ  Type
		args []string
		want string
	}{
		{"bare", "/p", Claude, nil, "cd '/p' && claude"},
		{"args", "/p", Claude, []string{"--resume", "abc"}, "cd '/p' && claude '--resume' 'abc'"},
		{"quoting", "/my dir", Codex, []string{"-p", `say "hi" it's`}, `cd '/my dir' && codex '-p' 'say "hi" it'"'"'s'`},
		{"no dir", "", OpenCode, []string{"--model", "x"}, "opencode '--model' 'x'"},
	}
	for _, tc := range tests {
		if got := LaunchCommand(tc.dir, tc.typ, tc.args...); got != tc.want {
			t.Errorf("%s: LaunchCommand = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestLaunchUsesSourceLauncher(t *testing.T) {
	slept := stubSleep(t)
	rec := &recorder{newID: "tmux:%3"}
	id, err := Launch(launcher{rec}, "tmux", "/p", "claude")
	if err != nil || id != "tmux:%3" {
		t.Fatalf("Launch = %q, %v", id, err)
	}
	want := []string{"newOn:tmux", "focus:tmux:%3", "run:tmux:%3:cd '/p' && claude"}
	if strings.Join(rec.calls, "|") != strings.Join(want, "|") {
		t.Errorf("calls = %v, want %v", rec.calls, want)
	}
	if len(*slept) != 1 || (*slept)[0] != LaunchSettle {
		t.Errorf("slept %v, want one LaunchSettle", *slept)
	}
}

func TestLaunchFallsBackToNewSession(t *testing.T) {
	stubSleep(t)
	// A SourceLauncher with an empty source uses NewSession.
	rec := &recorder{newID: "pty-1"}
	if _, err := Launch(launcher{rec}, "", "/p", "claude"); err != nil {
		t.Fatal(err)
	}
	if rec.calls[0] != "new" {
		t.Errorf("empty source: first call %q, want new", rec.calls[0])
	}
	// A backend without NewSessionOn uses NewSession whatever the source.
	rec = &recorder{newID: "pty-2"}
	if _, err := Launch(rec, "tmux", "/p", "claude"); err != nil {
		t.Fatal(err)
	}
	if rec.calls[0] != "new" {
		t.Errorf("plain backend: first call %q, want new", rec.calls[0])
	}
}

func TestLaunchQuotesDirAndPassesCommandAsGiven(t *testing.T) {
	stubSleep(t)
	rec := &recorder{newID: "s"}
	if _, err := Launch(rec, "", "/it's here", "codex --model x"); err != nil {
		t.Fatal(err)
	}
	want := `run:s:cd '/it'"'"'s here' && codex --model x`
	if rec.calls[2] != want {
		t.Errorf("run = %q, want %q", rec.calls[2], want)
	}
	rec = &recorder{newID: "s"}
	if _, err := Launch(rec, "", "", LaunchCommand("/p", Claude)); err != nil {
		t.Fatal(err)
	}
	if rec.calls[2] != "run:s:cd '/p' && claude" {
		t.Errorf("empty dir should run cmd as given, got %q", rec.calls[2])
	}
}

func TestLaunchErrors(t *testing.T) {
	stubSleep(t)
	boom := errors.New("boom")
	rec := &recorder{newErr: boom}
	if id, err := Launch(rec, "", "/p", "claude"); !errors.Is(err, boom) || id != "" {
		t.Errorf("NewSession failure: got %q, %v", id, err)
	}
	if len(rec.calls) != 1 {
		t.Errorf("nothing should follow a failed NewSession, got %v", rec.calls)
	}
	rec = &recorder{newID: "s", runErr: boom}
	if id, err := Launch(rec, "", "/p", "claude"); !errors.Is(err, boom) || id != "s" {
		t.Errorf("RunCommand failure should return the id with the error, got %q, %v", id, err)
	}
}
