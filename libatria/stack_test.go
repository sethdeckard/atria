package libatria

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sethdeckard/atria/libatria/terminal"
	"github.com/sethdeckard/atria/libatria/terminal/iterm"
	"github.com/sethdeckard/atria/libatria/watch"
)

// writeFake writes an executable shell script named name under a temp dir
// and returns its path. Every fake accepts the probe its client runs and
// answers listings with an empty result.
func writeFake(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body), 0o755); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
	return path
}

// fakeTmux answers every command with nothing; Available only checks the path.
func fakeTmux(t *testing.T) string {
	return writeFake(t, "tmux", "exit 0\n")
}

// fakeWezterm answers `cli list --format json` with no panes.
func fakeWezterm(t *testing.T) string {
	return writeFake(t, "wezterm", `case "$*" in *list*) echo '[]';; esac
exit 0
`)
}

// fakeDeviceterm reports a granted Automation tab and no panes.
func fakeDeviceterm(t *testing.T) string {
	return writeFake(t, "deviceterm", `case "$*" in
*"session show"*) echo '{"id":"tab-1","role":"automation","automationGrant":true}';;
*"pane list"*) echo '[]';;
esac
exit 0
`)
}

func env(vals map[string]string) func(string) string {
	return func(k string) string { return vals[k] }
}

func statusOf(t *testing.T, s *Stack, name string) Status {
	t.Helper()
	for _, st := range s.Statuses() {
		if st.Name == name {
			return st
		}
	}
	t.Fatalf("no status for %q", name)
	return Status{}
}

func prefixes(c *terminal.CompositeBackend) []string {
	var out []string
	for _, integ := range c.Integrations() {
		out = append(out, integ.Prefix)
	}
	return out
}

func TestOpenSelectsPrimaryByRankAndEnvironment(t *testing.T) {
	s, err := Open(Options{
		Integrations:    []string{"wezterm", "tmux", "mystery", "tmux"},
		TmuxPath:        fakeTmux(t),
		WezTermPath:     fakeWezterm(t),
		Getenv:          env(map[string]string{"TMUX": "/tmp/t", "WEZTERM_UNIX_SOCKET": "/tmp/w"}),
		NoSelfTTYFilter: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if got := s.PrimarySource(); got != "tmux" {
		t.Fatalf("primary = %q, want tmux (outranks wezterm)", got)
	}
	if got := strings.Join(prefixes(s.Composite()), ","); got != "wezterm:,tmux:,pty:" {
		t.Errorf("integrations = %q, want wezterm:,tmux:,pty: (PTY demoted)", got)
	}
	if got := s.Ignored(); len(got) != 1 || got[0] != "mystery" {
		t.Errorf("Ignored = %v, want [mystery]", got)
	}

	sts := s.Statuses()
	wantOrder := []string{"pty", "deviceterm", "tmux", "kitty", "wezterm", "iterm2"}
	for i, name := range wantOrder {
		if sts[i].Name != name {
			t.Fatalf("Statuses()[%d] = %q, want %q", i, sts[i].Name, name)
		}
	}
	check := func(name string, want Status) {
		got := statusOf(t, s, name)
		if got != want {
			t.Errorf("%s status = %+v, want %+v", name, got, want)
		}
	}
	check("pty", Status{Name: "pty", Source: "pty", Enabled: true, Available: true, Active: true})
	check("tmux", Status{Name: "tmux", Source: "tmux", Enabled: true, Available: true, Active: true, Launch: true})
	check("wezterm", Status{Name: "wezterm", Source: "wezterm", Enabled: true, Available: true, Active: true})
	check("kitty", Status{Name: "kitty", Source: "kitty"})
	check("iterm2", Status{Name: "iterm2", Source: "iterm"})
}

func TestOpenFallsBackToPTYWithoutEnvironment(t *testing.T) {
	s, err := Open(Options{
		Integrations:    []string{"tmux"},
		TmuxPath:        fakeTmux(t),
		Getenv:          env(nil),
		NoSelfTTYFilter: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.PrimarySource() != "pty" {
		t.Fatalf("primary = %q, want pty", s.PrimarySource())
	}
	if got := prefixes(s.Composite()); len(got) != 1 || got[0] != "tmux:" {
		t.Errorf("integrations = %v, want only tmux: (no pty: entry while PTY is primary)", got)
	}
	tm := statusOf(t, s, "tmux")
	if !tm.Enabled || !tm.Available || tm.Active || tm.Launch {
		t.Errorf("tmux outside tmux should be enabled and available but not active: %+v", tm)
	}
	if !statusOf(t, s, "pty").Launch {
		t.Error("pty should be the launch target")
	}
}

func TestOpenRecordsProbeFailure(t *testing.T) {
	s, err := Open(Options{
		Integrations:    []string{"tmux"},
		TmuxPath:        filepath.Join(t.TempDir(), "missing-tmux"),
		Getenv:          env(map[string]string{"TMUX": "/tmp/t"}),
		NoSelfTTYFilter: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tm := statusOf(t, s, "tmux")
	if !tm.Enabled || tm.Available || tm.Active || tm.Launch || tm.Reason == "" {
		t.Errorf("failed probe should leave Enabled with a Reason: %+v", tm)
	}
	if s.PrimarySource() != "pty" || len(s.Composite().Integrations()) != 0 {
		t.Errorf("a failed integration must not join the composite")
	}
}

func TestOpenDeviceTermIsPrimaryOrAbsent(t *testing.T) {
	t.Setenv("DEVICETERM_SESSION", "tab-1") // the client reads the real environment
	s, err := Open(Options{
		Integrations:    []string{"deviceterm", "tmux"},
		DeviceTermPath:  fakeDeviceterm(t),
		TmuxPath:        fakeTmux(t),
		Getenv:          env(map[string]string{"DEVICETERM_SESSION": "tab-1", "TMUX": "/tmp/t"}),
		NoSelfTTYFilter: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.PrimarySource() != "deviceterm" {
		t.Fatalf("primary = %q, want deviceterm", s.PrimarySource())
	}
	if got := strings.Join(prefixes(s.Composite()), ","); got != "tmux:,pty:" {
		t.Errorf("integrations = %q; deviceterm must never be listed", got)
	}
	dt := statusOf(t, s, "deviceterm")
	if !dt.Active || !dt.Launch {
		t.Errorf("deviceterm as primary should be active and launch: %+v", dt)
	}
	if statusOf(t, s, "tmux").Launch {
		t.Error("tmux should not be the launch target under deviceterm")
	}
}

func TestEnableAndDisableReportRoleChanges(t *testing.T) {
	s, err := Open(Options{
		TmuxPath:        fakeTmux(t),
		WezTermPath:     fakeWezterm(t),
		KittenPath:      filepath.Join(t.TempDir(), "missing-kitten"),
		Getenv:          env(map[string]string{"TMUX": "/tmp/t", "WEZTERM_UNIX_SOCKET": "/tmp/w", "KITTY_WINDOW_ID": "1"}),
		NoSelfTTYFilter: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Enabling the only environment-matched backend promotes it; PTY gains
	// its entry and its ids gain the prefix.
	st, rc, err := s.Enable("tmux")
	if err != nil {
		t.Fatal(err)
	}
	if !st.Active || !st.Launch {
		t.Errorf("tmux status = %+v, want active launch target", st)
	}
	want := &RoleChange{Source: "pty", Prefix: "pty:", ToPrefixed: true, NewPrimary: "tmux"}
	if rc == nil || *rc != *want {
		t.Errorf("RoleChange = %+v, want %+v", rc, want)
	}
	if got := strings.Join(prefixes(s.Composite()), ","); got != "tmux:,pty:" {
		t.Errorf("integrations = %q", got)
	}

	// Enabling again is a no-op.
	if _, rc, _ := s.Enable("tmux"); rc != nil || len(s.Composite().Integrations()) != 2 {
		t.Errorf("re-enabling a live integration should change nothing, got %+v", rc)
	}

	// A lower-ranked backend joins without displacing the primary.
	st, rc, err = s.Enable("wezterm")
	if err != nil || rc != nil || !st.Active || st.Launch {
		t.Errorf("wezterm: status=%+v rc=%+v err=%v; want active, not launch, no role change", st, rc, err)
	}

	// A failed probe records the reason and touches nothing.
	st, rc, err = s.Enable("kitty")
	if err != nil || rc != nil || !st.Enabled || st.Available || st.Active || st.Reason == "" {
		t.Errorf("kitty: status=%+v rc=%+v err=%v", st, rc, err)
	}
	if got := strings.Join(prefixes(s.Composite()), ","); got != "tmux:,pty:,wezterm:" {
		t.Errorf("integrations = %q", got)
	}

	// Disabling the primary hands launch to the next ranked match; its ids
	// lose the prefix and PTY keeps its entry.
	rc, err = s.Disable("tmux")
	if err != nil {
		t.Fatal(err)
	}
	want = &RoleChange{Source: "wezterm", Prefix: "wezterm:", ToPrefixed: false, NewPrimary: "wezterm"}
	if rc == nil || *rc != *want {
		t.Errorf("RoleChange = %+v, want %+v", rc, want)
	}
	if s.PrimarySource() != "wezterm" || strings.Join(prefixes(s.Composite()), ",") != "pty:,wezterm:" {
		t.Errorf("after disabling tmux: primary=%q integrations=%v", s.PrimarySource(), prefixes(s.Composite()))
	}
	tm := statusOf(t, s, "tmux")
	if tm.Enabled || tm.Available || tm.Active || tm.Launch {
		t.Errorf("disabled tmux status = %+v", tm)
	}
	if !statusOf(t, s, "wezterm").Launch {
		t.Error("wezterm should now be the launch target")
	}

	// Disabling a non-primary changes no roles.
	if rc, err := s.Disable("kitty"); err != nil || rc != nil {
		t.Errorf("disabling kitty: rc=%+v err=%v", rc, err)
	}

	// Disabling the last integration promotes PTY and detaches its entry
	// without closing it.
	rc, err = s.Disable("wezterm")
	if err != nil {
		t.Fatal(err)
	}
	want = &RoleChange{Source: "pty", Prefix: "pty:", ToPrefixed: false, NewPrimary: "pty"}
	if rc == nil || *rc != *want {
		t.Errorf("RoleChange = %+v, want %+v", rc, want)
	}
	if s.PrimarySource() != "pty" || len(s.Composite().Integrations()) != 0 {
		t.Errorf("after disabling wezterm: primary=%q integrations=%v", s.PrimarySource(), prefixes(s.Composite()))
	}
	if !statusOf(t, s, "pty").Launch {
		t.Error("pty should be the launch target again")
	}

	if _, _, err := s.Enable("mystery"); err == nil {
		t.Error("Enable(unknown) should fail")
	}
	if _, err := s.Disable("pty"); err == nil {
		t.Error("Disable(pty) should fail")
	}
}

func TestReprobePromotesLateIntegration(t *testing.T) {
	tmuxPath := filepath.Join(t.TempDir(), "tmux")
	s, err := Open(Options{
		Integrations:    []string{"tmux", "wezterm"},
		TmuxPath:        tmuxPath,
		WezTermPath:     fakeWezterm(t),
		Getenv:          env(map[string]string{"TMUX": "/tmp/t", "WEZTERM_UNIX_SOCKET": "/tmp/w"}),
		NoSelfTTYFilter: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.PrimarySource() != "wezterm" {
		t.Fatalf("primary = %q, want wezterm while tmux is missing", s.PrimarySource())
	}

	// Nothing to do while the binary is still missing.
	sts, rc := s.Reprobe()
	if rc != nil || sts[2].Name != "tmux" || sts[2].Available {
		t.Errorf("Reprobe with a missing binary: rc=%+v tmux=%+v", rc, sts[2])
	}

	if err := os.WriteFile(tmuxPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	sts, rc = s.Reprobe()
	want := &RoleChange{Source: "wezterm", Prefix: "wezterm:", ToPrefixed: true, NewPrimary: "tmux"}
	if rc == nil || *rc != *want {
		t.Errorf("RoleChange = %+v, want %+v", rc, want)
	}
	if s.PrimarySource() != "tmux" {
		t.Errorf("primary = %q, want tmux", s.PrimarySource())
	}
	for _, st := range sts {
		switch st.Name {
		case "tmux":
			if !st.Available || !st.Active || !st.Launch || st.Reason != "" {
				t.Errorf("tmux after reprobe = %+v", st)
			}
		case "wezterm":
			if st.Launch {
				t.Errorf("wezterm should have lost the launch role: %+v", st)
			}
		}
	}
	if got := strings.Join(prefixes(s.Composite()), ","); got != "wezterm:,pty:,tmux:" {
		t.Errorf("integrations = %q", got)
	}
}

// fakeTmuxLogging records every tmux invocation to logPath, reports every
// session as existing, and answers new-window with a pane id.
func fakeTmuxLogging(t *testing.T, logPath string) string {
	return writeFake(t, "tmux", `echo "$@" >> "`+logPath+`"
case "$1" in
new-window|new-session) echo "%9";;
esac
exit 0
`)
}

func TestMutationsInvalidateSessionCache(t *testing.T) {
	s, err := Open(Options{
		TmuxPath:        fakeTmux(t),
		Getenv:          env(map[string]string{"TMUX": "/tmp/t"}),
		NoSelfTTYFilter: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	id, err := s.PTY().NewSession()
	if err != nil {
		t.Fatal(err)
	}
	listIDs := func() string {
		sessions, err := s.Backend().ListSessions()
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, sess := range sessions {
			ids = append(ids, sess.ID)
		}
		return strings.Join(ids, ",")
	}
	if got := listIDs(); got != id {
		t.Fatalf("PTY primary should list %q bare, got %q", id, got)
	}

	// Enabling tmux demotes PTY; the very next listing (well inside the
	// default TTL) must already carry the prefix.
	if _, _, err := s.Enable("tmux"); err != nil {
		t.Fatal(err)
	}
	if got := listIDs(); got != "pty:"+id {
		t.Errorf("after Enable: listed %q, want %q", got, "pty:"+id)
	}
	if _, err := s.Disable("tmux"); err != nil {
		t.Fatal(err)
	}
	if got := listIDs(); got != id {
		t.Errorf("after Disable: listed %q, want %q", got, id)
	}
}

func TestConfigureAppliesToLaterEnable(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "tmux.log")
	s, err := Open(Options{
		Integrations:    []string{"tmux"},
		TmuxPath:        fakeTmuxLogging(t, logPath),
		TmuxSession:     "old",
		Getenv:          env(map[string]string{"TMUX": "/tmp/t"}),
		NoSelfTTYFilter: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	launched := func() string {
		if err := os.WriteFile(logPath, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Backend().NewSession(); err != nil {
			t.Fatal(err)
		}
		log, _ := os.ReadFile(logPath)
		return string(log)
	}
	if got := launched(); !strings.Contains(got, "new-window -t =old:") {
		t.Fatalf("launch should target the old session, got:\n%s", got)
	}

	// Configure alone leaves the running client on the old session.
	s.Configure(Options{TmuxPath: s.opts.TmuxPath, TmuxSession: "fresh"})
	if got := launched(); !strings.Contains(got, "-t =old:") {
		t.Errorf("a live client should keep its session until rebuilt, got:\n%s", got)
	}

	// Re-enabling builds the client from the new options.
	if _, err := s.Disable("tmux"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Enable("tmux"); err != nil {
		t.Fatal(err)
	}
	if got := launched(); !strings.Contains(got, "new-window -t =fresh:") {
		t.Errorf("launch should target the configured session, got:\n%s", got)
	}
}

func TestToggleWhileWatching(t *testing.T) {
	s, err := Open(Options{
		TmuxPath:        fakeTmux(t),
		WezTermPath:     fakeWezterm(t),
		Getenv:          env(map[string]string{"TMUX": "/tmp/t", "WEZTERM_UNIX_SOCKET": "/tmp/w"}),
		CacheTTL:        time.Millisecond,
		NoSelfTTYFilter: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx, cancel := context.WithCancel(context.Background())
	w := watch.New(s.Backend(), watch.Options{
		DiscoveryInterval: time.Millisecond,
		ActiveInterval:    time.Millisecond,
		IdleInterval:      time.Millisecond,
	})
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	go func() {
		for range w.Events() {
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for n := 0; n < 20; n++ {
				var err error
				switch (i + n) % 4 {
				case 0:
					_, _, err = s.Enable("tmux")
				case 1:
					_, err = s.Disable("tmux")
				case 2:
					_, _, err = s.Enable("wezterm")
					s.Reprobe()
				case 3:
					s.Statuses()
					s.PrimarySource()
					_, err = s.Backend().ListSessions()
				}
				if err != nil {
					t.Errorf("worker %d step %d: %v", i, n, err)
				}
			}
		}(i)
	}
	wg.Wait()
	cancel()
	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Errorf("Run returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watcher did not stop")
	}
}

func TestITermOptionsPrompt(t *testing.T) {
	inITerm := map[string]string{"TERM_PROGRAM": "iTerm.app"}
	elsewhere := map[string]string{"TERM_PROGRAM": "WezTerm"}
	tests := []struct {
		name   string
		opts   Options
		env    map[string]string
		atOpen bool
		prompt bool
	}{
		{"zero options", Options{}, inITerm, true, false},
		{"allow at Open inside iTerm2", Options{AllowITermPrompt: true}, inITerm, true, true},
		{"allow at Open elsewhere", Options{AllowITermPrompt: true}, elsewhere, true, false},
		{"allow at Open with no terminal", Options{AllowITermPrompt: true}, nil, true, false},
		{"allow on Enable inside iTerm2", Options{AllowITermPrompt: true}, inITerm, false, false},
		{"anywhere at Open with no terminal", Options{ITermPromptAnywhere: true}, nil, true, true},
		{"anywhere on Enable with no terminal", Options{ITermPromptAnywhere: true}, nil, false, true},
		{"anywhere on Enable elsewhere", Options{ITermPromptAnywhere: true}, elsewhere, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Stack{opts: tt.opts, getenv: env(tt.env)}
			if got := s.itermOptions(tt.atOpen).NoPrompt; got != !tt.prompt {
				t.Errorf("NoPrompt = %v, want %v", got, !tt.prompt)
			}
		})
	}

	s := &Stack{opts: Options{ProgramName: "dash", CommandTimeout: 2 * time.Second}, getenv: env(nil)}
	o := s.itermOptions(false)
	if o.ClientName != "dash" || o.CommandTimeout != 2*time.Second {
		t.Errorf("options not passed through: %+v", o)
	}
}

// probeBackend is a Backend whose Available blocks until the test sends the
// probe's result on release. Like the iTerm2 client, whose connection mutex
// is held through a credential dialog, Close waits for an Available in
// progress. Only Available, ListSessions, and Close are called on it.
type probeBackend struct {
	terminal.Backend
	started   chan struct{}
	release   chan error
	mu        sync.Mutex // held through Available
	closed    atomic.Bool
	closeOnce sync.Once
	done      chan struct{} // closed by Close
}

func newProbeBackend() *probeBackend {
	return &probeBackend{started: make(chan struct{}, 1), release: make(chan error, 1), done: make(chan struct{})}
}

func (p *probeBackend) Available() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.started <- struct{}{}
	return <-p.release
}

func (p *probeBackend) ListSessions() ([]terminal.Session, error) { return nil, nil }

func (p *probeBackend) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed.Store(true)
	p.closeOnce.Do(func() { close(p.done) })
	return nil
}

// waitClosed fails the test unless p is closed within a second; Stack closes
// dropped clients in the background.
func waitClosed(t *testing.T, p *probeBackend) {
	t.Helper()
	select {
	case <-p.done:
	case <-time.After(time.Second):
		t.Fatal("client was never closed")
	}
}

// openWithProbes opens a PTY-only stack whose later Enable and Reprobe
// calls build the given backend for each name.
func openWithProbes(t *testing.T, vals map[string]string, backends map[string]*probeBackend) *Stack {
	t.Helper()
	s, err := Open(Options{Getenv: env(vals), NoSelfTTYFilter: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	s.build = func(name string, _ bool) terminal.Backend {
		b, ok := backends[name]
		if !ok {
			panic("no probe backend for " + name) // may run off the test goroutine
		}
		return b
	}
	return s
}

// waitStarted fails the test unless p's probe starts within a second.
func waitStarted(t *testing.T, p *probeBackend) {
	t.Helper()
	select {
	case <-p.started:
	case <-time.After(time.Second):
		t.Fatal("probe never started")
	}
}

// returnsPromptly fails the test unless fn returns within a second.
func returnsPromptly(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		fn()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("%s blocked behind a probe in flight", what)
	}
}

// The regression: a probe waiting on the terminal (or a credential dialog)
// must not hold the Stack's lock.
func TestProbeRunsOutsideTheLock(t *testing.T) {
	tmux := newProbeBackend()
	s := openWithProbes(t, map[string]string{"TMUX": "/tmp/tmux-1/default,1,0"}, map[string]*probeBackend{"tmux": tmux})

	type result struct {
		st Status
		rc *RoleChange
	}
	out := make(chan result, 1)
	go func() {
		st, rc, _ := s.Enable("tmux")
		out <- result{st, rc}
	}()
	waitStarted(t, tmux)

	returnsPromptly(t, "Statuses", func() {
		if st := statusOf(t, s, "tmux"); !st.Enabled || st.Available {
			t.Errorf("status during probe = %+v, want enabled and not yet available", st)
		}
	})
	returnsPromptly(t, "Configure", func() { s.Configure(Options{}) })
	returnsPromptly(t, "a second Enable", func() {
		if _, rc, _ := s.Enable("tmux"); rc != nil {
			t.Errorf("Enable while probing returned %+v, want nil", rc)
		}
	})
	returnsPromptly(t, "Reprobe", func() { s.Reprobe() })

	tmux.release <- nil
	r := <-out
	if !r.st.Available || r.rc == nil || r.rc.NewPrimary != "tmux" {
		t.Fatalf("Enable = %+v, %+v; want tmux available and primary", r.st, r.rc)
	}
}

func TestDisableDuringProbeDropsIt(t *testing.T) {
	tmux := newProbeBackend()
	s := openWithProbes(t, map[string]string{"TMUX": "/tmp/tmux-1/default,1,0"}, map[string]*probeBackend{"tmux": tmux})

	out := make(chan *RoleChange, 1)
	go func() {
		_, rc, _ := s.Enable("tmux")
		out <- rc
	}()
	waitStarted(t, tmux)
	if _, err := s.Disable("tmux"); err != nil {
		t.Fatal(err)
	}
	tmux.release <- nil

	if rc := <-out; rc != nil {
		t.Errorf("RoleChange = %+v, want nil", rc)
	}
	if st := statusOf(t, s, "tmux"); st.Enabled || st.Available {
		t.Errorf("status = %+v, want disabled", st)
	}
	if got := s.PrimarySource(); got != "pty" {
		t.Errorf("primary = %q, want pty", got)
	}
	if len(prefixes(s.Composite())) != 0 {
		t.Errorf("integrations = %q, want none", prefixes(s.Composite()))
	}
	waitClosed(t, tmux)
}

// Promotion compares against the primary when the probe returns: kitty
// started first against PTY, but tmux (higher rank) took primary meanwhile.
func TestPromotionDecidedAtWireIn(t *testing.T) {
	kitty, tmux := newProbeBackend(), newProbeBackend()
	s := openWithProbes(t,
		map[string]string{"TMUX": "/tmp/tmux-1/default,1,0", "KITTY_WINDOW_ID": "1"},
		map[string]*probeBackend{"kitty": kitty, "tmux": tmux})

	out := make(chan *RoleChange, 1)
	go func() {
		_, rc, _ := s.Enable("kitty")
		out <- rc
	}()
	waitStarted(t, kitty)

	tmux.release <- nil
	if _, rc, _ := s.Enable("tmux"); rc == nil || rc.NewPrimary != "tmux" {
		t.Fatalf("tmux Enable RoleChange = %+v, want tmux primary", rc)
	}
	kitty.release <- nil
	if rc := <-out; rc != nil {
		t.Errorf("kitty RoleChange = %+v, want nil (tmux outranks it)", rc)
	}
	if got := s.PrimarySource(); got != "tmux" {
		t.Errorf("primary = %q, want tmux", got)
	}
	if st := statusOf(t, s, "kitty"); !st.Available || st.Launch {
		t.Errorf("kitty status = %+v, want available, not launching", st)
	}
}

func TestFailedProbeRecordsErrAndCloses(t *testing.T) {
	tmux := newProbeBackend()
	s := openWithProbes(t, nil, map[string]*probeBackend{"tmux": tmux})
	probeErr := fmt.Errorf("probe: %w", terminal.ErrUnavailable)
	tmux.release <- probeErr

	st, rc, err := s.Enable("tmux")
	if err != nil || rc != nil {
		t.Fatalf("Enable = %v, %+v", err, rc)
	}
	if !errors.Is(st.Err, terminal.ErrUnavailable) || st.Reason != probeErr.Error() || st.Available {
		t.Errorf("status = %+v, want Err and Reason from the probe", st)
	}
	if !tmux.closed.Load() {
		t.Error("failed client wasn't closed")
	}
}

func TestReauthorize(t *testing.T) {
	s, err := Open(Options{Getenv: env(nil), NoSelfTTYFilter: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	for _, name := range []string{"tmux", "pty", "bogus"} {
		if err := s.Reauthorize(name); err == nil {
			t.Errorf("Reauthorize(%q) = nil, want an error", name)
		}
	}
	if s.itermOptions(false).AuthGate != s.authGate {
		t.Fatal("iTerm2 clients don't share the Stack's gate")
	}
	if err := s.Reauthorize("iterm2"); err != nil || !s.authGate.Armed() {
		t.Fatalf("Reauthorize(iterm2) = %v, armed %v", err, s.authGate.Armed())
	}
}

// A live iTerm2 client whose reconnect is declined disarms the shared gate
// without a probe; Statuses must still report it, and Reauthorize then
// Reprobe must retry that same client in place.
func TestDisarmedGateReportedForLiveClient(t *testing.T) {
	it := newProbeBackend()
	s := openWithProbes(t, map[string]string{"TERM_PROGRAM": "iTerm.app"}, map[string]*probeBackend{"iterm2": it})
	it.release <- nil
	if st, _, _ := s.Enable("iterm2"); !st.Available {
		t.Fatalf("Enable = %+v, want available", st)
	}
	<-it.started

	var gateErr error
	s.gateErr = func() error { return gateErr }
	gateErr = fmt.Errorf("%w for %q: user declined; prompting stays off until it is re-armed", iterm.ErrAuthFailed, "libatria")

	st := statusOf(t, s, "iterm2")
	if st.Available || st.Active || !errors.Is(st.Err, iterm.ErrAuthFailed) || st.Reason != gateErr.Error() {
		t.Fatalf("status after declined reconnect = %+v, want unavailable with ErrAuthFailed", st)
	}
	if got := s.PrimarySource(); got != "iterm" {
		t.Errorf("primary = %q, want iterm unchanged", got)
	}

	// Still disarmed: Reprobe retries the live client and it fails again.
	it.release <- fmt.Errorf("cannot connect to iTerm2: %w", gateErr)
	statuses, rc := s.Reprobe()
	<-it.started
	if rc != nil {
		t.Errorf("Reprobe RoleChange = %+v, want nil", rc)
	}
	for _, st := range statuses {
		if st.Name == "iterm2" && (st.Available || !errors.Is(st.Err, iterm.ErrAuthFailed)) {
			t.Errorf("status after disarmed Reprobe = %+v", st)
		}
	}

	// Re-armed: the same client connects again.
	if err := s.Reauthorize("iterm2"); err != nil {
		t.Fatal(err)
	}
	gateErr = nil
	it.release <- nil
	s.Reprobe()
	<-it.started
	if st := statusOf(t, s, "iterm2"); !st.Available || st.Err != nil || st.Reason != "" || !st.Launch {
		t.Errorf("status after Reauthorize and Reprobe = %+v, want available and launching", st)
	}
	if it.closed.Load() {
		t.Error("the live client was closed")
	}
	if got := prefixes(s.Composite()); len(got) != 2 || got[0] != "iterm:" {
		t.Errorf("integrations = %q, want the same iterm entry plus pty", got)
	}
}

// liveITermDeclined makes iTerm2 live through it, then has its reconnect
// declined (a disarmed gate), so Reprobe will retry it in place.
func liveITermDeclined(t *testing.T, s *Stack, it *probeBackend) {
	t.Helper()
	it.release <- nil
	if st, _, _ := s.Enable("iterm2"); !st.Available {
		t.Fatalf("Enable(iterm2) = %+v, want available", st)
	}
	<-it.started
	declined := fmt.Errorf("%w for %q: user declined", iterm.ErrAuthFailed, "libatria")
	s.gateErr = func() error { return declined }
}

// A probe queued behind a slow one and disabled meanwhile must not run: no
// Available call on the disabled client, and the client ends up closed.
func TestReprobeSkipsProbeDisabledWhileQueued(t *testing.T) {
	tmux, it := newProbeBackend(), newProbeBackend()
	s := openWithProbes(t, nil, map[string]*probeBackend{"tmux": tmux, "iterm2": it})
	tmux.release <- errors.New("no server")
	_, _, _ = s.Enable("tmux")
	<-tmux.started
	liveITermDeclined(t, s, it)
	it.release <- errors.New("probed after Disable") // so a wrongful probe returns rather than hangs

	done := make(chan struct{})
	go func() {
		s.Reprobe()
		close(done)
	}()
	waitStarted(t, tmux) // iTerm2 is queued behind it
	returnsPromptly(t, "Disable", func() {
		if _, err := s.Disable("iterm2"); err != nil {
			t.Error(err)
		}
	})
	tmux.release <- errors.New("no server")
	<-done

	select {
	case <-it.started:
		t.Fatal("the disabled client was probed after Disable")
	default:
	}
	waitClosed(t, it)
	if st := statusOf(t, s, "iterm2"); st.Enabled || st.Available {
		t.Errorf("iterm2 status = %+v, want disabled", st)
	}
}

// Disabling iTerm2 while its live client is blocked in a probe (a credential
// dialog open) must not wait for it: Disable, Statuses, and listings all
// return, and the client is closed once the probe lets go.
func TestDisableDoesNotWaitForBlockedLiveProbe(t *testing.T) {
	it := newProbeBackend()
	s := openWithProbes(t, map[string]string{"TERM_PROGRAM": "iTerm.app"}, map[string]*probeBackend{"iterm2": it})
	liveITermDeclined(t, s, it)
	t.Cleanup(func() { // so a failure doesn't leave the probe, and Close, stuck
		select {
		case it.release <- nil:
		default:
		}
	})
	if got := s.PrimarySource(); got != "iterm" {
		t.Fatalf("primary = %q, want iterm", got)
	}

	done := make(chan struct{})
	go func() {
		s.Reprobe()
		close(done)
	}()
	waitStarted(t, it)

	returnsPromptly(t, "Disable", func() {
		if rc, err := s.Disable("iterm2"); err != nil || rc == nil || rc.NewPrimary != "pty" {
			t.Errorf("Disable = %+v, %v; want pty primary", rc, err)
		}
	})
	returnsPromptly(t, "Statuses", func() { s.Statuses() })
	returnsPromptly(t, "ListSessions", func() {
		if _, err := s.Backend().ListSessions(); err != nil {
			t.Errorf("ListSessions: %v", err)
		}
	})
	if it.closed.Load() {
		t.Fatal("closed while its probe still held it")
	}

	it.release <- nil
	<-done
	waitClosed(t, it)
	if st := statusOf(t, s, "iterm2"); st.Enabled || st.Available {
		t.Errorf("iterm2 status = %+v, want disabled", st)
	}
}
