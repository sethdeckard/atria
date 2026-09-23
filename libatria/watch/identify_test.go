package watch

import (
	"errors"
	"testing"

	"github.com/sethdeckard/atria/libatria/agent"
	"github.com/sethdeckard/atria/libatria/terminal"
)

const codexScreen = "\n\n› Write tests for @filename\n\n  gpt-5.3-codex default · 73% left · ~/projects/foo\n"

func TestIdentifyGatedKnownTitleNoScreenRead(t *testing.T) {
	b := newFake()
	b.setVar("s1", "path", "/home/me/projects/app")
	id := Identify(b, terminal.Session{ID: "s1", Name: "✳ Editing main.go"}, IdentifyOptions{WatchDirs: []string{"/home/me/projects"}})
	if !id.OK() || id.Type != agent.Claude || id.Dir != "/home/me/projects/app" || !id.Gated || id.Skip != SkipNone {
		t.Fatalf("identity = %+v", id)
	}
	if len(b.reads) != 0 {
		t.Fatalf("a known title must not spend a screen read, got %v", b.reads)
	}
}

func TestIdentifyGatedUnknownTitleNoDirSkipsWithoutRead(t *testing.T) {
	b := newFake()
	id := Identify(b, terminal.Session{ID: "s1", Name: "zsh"}, IdentifyOptions{WatchDirs: []string{"/home/me/projects"}})
	if id.OK() || id.Skip != SkipUnknownTitleNoDir || id.Skip.String() != "unknown title and empty dir" {
		t.Fatalf("identity = %+v", id)
	}
	if len(b.reads) != 0 {
		t.Fatalf("no read expected, got %v", b.reads)
	}
}

func TestIdentifyGatedInfersFromScreen(t *testing.T) {
	b := newFake()
	b.setVar("s1", "path", "/home/me/projects/app")
	b.setScreen("s1", codexScreen)
	id := Identify(b, terminal.Session{ID: "s1", Name: "app"}, IdentifyOptions{WatchDirs: []string{"/home/me/projects"}})
	if !id.OK() || id.Type != agent.Codex || id.Dir != "/home/me/projects/app" {
		t.Fatalf("identity = %+v", id)
	}

	b.readErr["s1"] = errors.New("boom")
	id = Identify(b, terminal.Session{ID: "s1", Name: "app"}, IdentifyOptions{WatchDirs: []string{"/home/me/projects"}})
	if id.OK() || id.Skip != SkipScreenReadFailed || id.Err == nil {
		t.Fatalf("read failure = %+v", id)
	}

	delete(b.readErr, "s1")
	b.setScreen("s1", "just a shell\n$ ")
	id = Identify(b, terminal.Session{ID: "s1", Name: "app"}, IdentifyOptions{WatchDirs: []string{"/home/me/projects"}})
	if id.OK() || id.Skip != SkipUnknownTitleAndScreen {
		t.Fatalf("unknown screen = %+v", id)
	}
}

func TestIdentifyGatedRejectsNameMatchOutsideWatchDirs(t *testing.T) {
	// GetVar gives nothing; DiscoverCWD's last strategy matches the title
	// against ProjectDirs and returns a project outside the watch list.
	b := newFake()
	id := Identify(b, terminal.Session{ID: "s1", Name: "✳ Editing myapp"}, IdentifyOptions{
		WatchDirs:   []string{"/home/me/projects"},
		ProjectDirs: []string{"/elsewhere/myapp"},
	})
	if id.OK() || id.Skip != SkipOutsideWatchDirs || id.Dir != "" {
		t.Fatalf("outside project must be skipped: %+v", id)
	}
	if id.Type != agent.Claude {
		t.Fatalf("the title's type is still reported for logging, got %q", id.Type)
	}
	// The same project inside the watch list is accepted.
	id = Identify(b, terminal.Session{ID: "s1", Name: "✳ Editing myapp"}, IdentifyOptions{
		WatchDirs:   []string{"/home/me/projects"},
		ProjectDirs: []string{"/home/me/projects/myapp"},
	})
	if !id.OK() || id.Dir != "/home/me/projects/myapp" {
		t.Fatalf("inside project = %+v", id)
	}
}

func TestIdentifyGatedKnownTitleNoDirIsNotASkip(t *testing.T) {
	b := newFake()
	id := Identify(b, terminal.Session{ID: "s1", Name: "claude"}, IdentifyOptions{WatchDirs: []string{"/home/me/projects"}})
	if id.OK() || id.Skip != SkipNone || id.Type != agent.Claude || id.Dir != "" {
		t.Fatalf("known title, no dir: %+v", id)
	}
}

func TestIdentifyUngatedAdmitsWithoutDir(t *testing.T) {
	b := newFake()
	id := Identify(b, terminal.Session{ID: "s1", Name: "claude"}, IdentifyOptions{})
	if !id.OK() || id.Gated || id.Type != agent.Claude || id.Dir != "" {
		t.Fatalf("ungated known title = %+v", id)
	}
	// With a path var, Dir is filled without any gate.
	b.setVar("s1", "path", "/anywhere/at/all")
	id = Identify(b, terminal.Session{ID: "s1", Name: "claude"}, IdentifyOptions{})
	if !id.OK() || id.Dir != "/anywhere/at/all" {
		t.Fatalf("ungated with path = %+v", id)
	}
}

func TestIdentifyUngatedAlwaysReadsScreenForUnknownTitle(t *testing.T) {
	b := newFake()
	b.setScreen("s1", codexScreen)
	id := Identify(b, terminal.Session{ID: "s1", Name: "zsh"}, IdentifyOptions{})
	if !id.OK() || id.Type != agent.Codex {
		t.Fatalf("ungated inference = %+v", id)
	}
	if len(b.reads) != 1 {
		t.Fatalf("expected one screen read, got %v", b.reads)
	}
	b.setScreen("s1", "plain shell\n$ ")
	if id := Identify(b, terminal.Session{ID: "s1", Name: "zsh"}, IdentifyOptions{}); id.OK() || id.Skip != SkipUnknownTitleAndScreen {
		t.Fatalf("ungated unknown = %+v", id)
	}
}

func TestResolveProcessWithoutTTYOrPidIsZero(t *testing.T) {
	b := newFake()
	p, typ := ResolveProcess(b, terminal.Session{ID: "s1"})
	if p.PID != 0 || typ != "" {
		t.Fatalf("expected zero process, got %+v %q", p, typ)
	}
	b.setVar("s1", "pid", "not-a-number")
	if p, _ := ResolveProcess(b, terminal.Session{ID: "s1"}); p.PID != 0 {
		t.Fatalf("bad pid must give zero process, got %+v", p)
	}
}

func TestSkipReasonStrings(t *testing.T) {
	want := map[SkipReason]string{
		SkipNone:                  "",
		SkipUnknownTitleNoDir:     "unknown title and empty dir",
		SkipScreenReadFailed:      "screen read failed",
		SkipUnknownTitleAndScreen: "unknown title and screen",
		SkipOutsideWatchDirs:      "outside watch dirs",
	}
	for r, s := range want {
		if r.String() != s {
			t.Errorf("%d.String() = %q, want %q", r, r.String(), s)
		}
	}
}

func TestIdentifyGatedProcessCwdIsAuthoritative(t *testing.T) {
	b := newFake()
	b.setVar("s1", "path", "/home/me/projects/app") // the shell's directory
	sess := terminal.Session{ID: "s1", Name: "claude"}
	opts := IdentifyOptions{WatchDirs: []string{"/home/me/projects"}}

	// Agent process in another in-scope project: its cwd wins.
	stubResolve(t, func(terminal.Session) terminal.Process {
		return terminal.Process{PID: 9, Argv: []string{"claude"}, Cwd: "/home/me/projects/other"}
	}, func(int) bool { return true })
	id := Identify(b, sess, opts)
	if !id.OK() || id.Dir != "/home/me/projects/other" || id.Process.PID != 9 {
		t.Fatalf("identity = %+v", id)
	}

	// Agent process outside the watch list: skipped even though the shell
	// reports an in-scope directory.
	stubResolve(t, func(terminal.Session) terminal.Process {
		return terminal.Process{PID: 9, Argv: []string{"claude"}, Cwd: "/elsewhere/secret"}
	}, func(int) bool { return true })
	id = Identify(b, sess, opts)
	if id.OK() || id.Skip != SkipOutsideWatchDirs || id.Dir != "" {
		t.Fatalf("out-of-scope process must be skipped: %+v", id)
	}

	// No process cwd known: the shell's directory stands.
	stubResolve(t, func(terminal.Session) terminal.Process { return terminal.Process{} }, func(int) bool { return true })
	if id = Identify(b, sess, opts); !id.OK() || id.Dir != "/home/me/projects/app" {
		t.Fatalf("fallback identity = %+v", id)
	}
}
