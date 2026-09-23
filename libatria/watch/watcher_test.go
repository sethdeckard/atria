package watch

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sethdeckard/atria/libatria/agent"
	"github.com/sethdeckard/atria/libatria/terminal"
)

// newWatcher builds a Watcher over a fake backend with process resolution
// stubbed to nothing, so no test shells out.
func newWatcher(t *testing.T, b *fakeBackend, opts Options) (*Watcher, *fakeClock) {
	t.Helper()
	stubResolve(t, func(terminal.Session) terminal.Process { return terminal.Process{} }, func(int) bool { return true })
	clock := newClock()
	opts.Clock = clock
	if opts.WatchDirs == nil && !opts.ungatedMarker() {
		opts.WatchDirs = []string{"/home/me/projects"}
	}
	return New(b, opts), clock
}

// ungatedMarker lets a test ask for no watch dirs explicitly.
func (o Options) ungatedMarker() bool { return o.EventBuffer == 1 && o.Parallelism == 1 }

func claudeSession(b *fakeBackend, id string) terminal.Session {
	b.setVar(id, "path", "/home/me/projects/app")
	b.setScreen(id, "✻ Reading…\n")
	return terminal.Session{ID: id, Name: "✳ Editing main.go", Source: "pty"}
}

func TestDiscoverAddsSessionAndPollTransitions(t *testing.T) {
	b := newFake()
	b.setSessions(claudeSession(b, "s1"))
	w, clock := newWatcher(t, b, Options{})
	ctx := context.Background()

	if !w.discoverOnce(ctx) {
		t.Fatal("discover should not report cancellation")
	}
	ev := recvEvent(t, w.Events())
	if ev.Kind != SessionAdded || ev.Session.ID != "s1" || ev.Session.Type != agent.Claude || ev.Session.Status != agent.StatusWorking || ev.Session.Dir != "/home/me/projects/app" {
		t.Fatalf("added event = %+v", ev)
	}
	if ev.Session.Name != "✳ Editing main.go" || ev.Session.Source != "pty" {
		t.Fatalf("snapshot must embed the listed session: %+v", ev.Session.Session)
	}
	if !containsString(w.projectDirs, "/home/me/projects/app") {
		t.Fatal("learned dir should be added to projectDirs")
	}

	// First poll: working screen, no status change, no ScreenRead by default.
	w.pollOnce(ctx)
	if evs := drain(w.Events()); len(evs) != 0 {
		t.Fatalf("expected no events from a working read, got %+v", evs)
	}
	// Screen shows the prompt: working -> idle.
	b.setScreen("s1", "done\n❯ ")
	clock.Advance(time.Second)
	w.pollOnce(ctx)
	ev = recvEvent(t, w.Events())
	if ev.Kind != StatusChanged || ev.From != agent.StatusWorking || ev.To != agent.StatusIdle || ev.MatchLine != "❯" {
		t.Fatalf("status event = %+v", ev)
	}
	snap, ok := w.Session("s1")
	if !ok || snap.Status != agent.StatusIdle || snap.Screen != "done\n❯ " {
		t.Fatalf("snapshot = %+v", snap)
	}
}

func TestNeedsInputEdgeEmitsOnceWithAttention(t *testing.T) {
	b := newFake()
	b.setSessions(claudeSession(b, "s1"))
	w, clock := newWatcher(t, b, Options{})
	ctx := context.Background()
	w.discoverOnce(ctx)
	drain(w.Events())

	b.setScreen("s1", "Do you want to proceed?\n")
	w.pollOnce(ctx)
	ev := recvEvent(t, w.Events())
	if ev.Kind != StatusChanged || ev.To != agent.StatusNeedsInput || ev.MatchLine != "Do you want to proceed?" || ev.Session.Attention != "Do you want to proceed?" {
		t.Fatalf("needs_input event = %+v", ev)
	}
	// Same prompt again: stale read, no event.
	clock.Advance(time.Second)
	w.pollOnce(ctx)
	if evs := drain(w.Events()); len(evs) != 0 {
		t.Fatalf("stale needs_input read must not re-emit, got %+v", evs)
	}
}

func TestPollRespectsPerStatusIntervals(t *testing.T) {
	b := newFake()
	b.setSessions(claudeSession(b, "s1"))
	w, clock := newWatcher(t, b, Options{ActiveInterval: time.Second, IdleInterval: 3 * time.Second})
	ctx := context.Background()
	w.discoverOnce(ctx)
	drain(w.Events())
	b.setScreen("s1", "❯ ")
	w.pollOnce(ctx) // working -> idle, read #1
	drain(w.Events())
	reads := len(b.reads)

	clock.Advance(time.Second)
	w.pollOnce(ctx) // idle session: not due yet at 1s
	if len(b.reads) != reads {
		t.Fatal("idle session should not be re-read after 1s")
	}
	clock.Advance(2 * time.Second)
	w.pollOnce(ctx) // due at 3s
	if len(b.reads) != reads+1 {
		t.Fatalf("idle session should be read at 3s; reads=%d", len(b.reads)-reads)
	}
}

func TestOrphanAndGoneRemoval(t *testing.T) {
	b := newFake()
	b.setSessions(claudeSession(b, "s1"), claudeSession(b, "s2"))
	w, _ := newWatcher(t, b, Options{})
	ctx := context.Background()
	w.discoverOnce(ctx)
	drain(w.Events())

	// s1 goes idle, then its title stops naming an agent and its screen is a
	// shell: two refreshes later it is an orphan.
	b.setScreen("s1", "❯ ")
	w.pollOnce(ctx)
	drain(w.Events())
	b.setScreen("s1", "user@host ~ %")
	b.setSessions(terminal.Session{ID: "s1", Name: "zsh", Source: "pty"}, claudeSession(b, "s2"))
	// The tracker's LastScreen must show no agent UI; feed the shell read.
	w.mu.Lock()
	w.tracked["s1"].tr.LastScreen = "user@host ~ %"
	w.mu.Unlock()
	w.discoverOnce(ctx)
	drain(w.Events())
	if snap, _ := w.Session("s1"); snap.ID != "s1" {
		t.Fatal("s1 should survive the first orphan tick")
	}
	w.discoverOnce(ctx)
	evs := drain(w.Events())
	if len(evs) != 1 || evs[0].Kind != SessionRemoved || evs[0].Reason != RemovedOrphan || evs[0].Session.ID != "s1" {
		t.Fatalf("expected orphan removal of s1, got %+v", evs)
	}

	// s2 disappears from the listing: gone.
	b.setSessions()
	w.discoverOnce(ctx)
	evs = drain(w.Events())
	if len(evs) != 1 || evs[0].Kind != SessionRemoved || evs[0].Reason != RemovedGone || evs[0].Session.ID != "s2" {
		t.Fatalf("expected gone removal of s2, got %+v", evs)
	}
	if len(w.Sessions()) != 0 {
		t.Fatal("no sessions should remain")
	}
}

func TestFailedSourceRetainsSessions(t *testing.T) {
	b := newFake()
	s := claudeSession(b, "iterm:s1")
	s.Source = "iterm"
	b.setSessions(s)
	w, _ := newWatcher(t, b, Options{})
	ctx := context.Background()
	w.discoverOnce(ctx)
	drain(w.Events())

	// iTerm stops answering: the session vanishes from the list but its
	// source is reported failed, so it is kept.
	b.mu.Lock()
	b.sessions = nil
	b.failed = []string{"iterm"}
	b.mu.Unlock()
	w.discoverOnce(ctx)
	if evs := drain(w.Events()); len(evs) != 0 {
		t.Fatalf("failed source must not remove sessions, got %+v", evs)
	}
	if _, ok := w.Session("iterm:s1"); !ok {
		t.Fatal("session should be retained while its source is failed")
	}
	// The source recovers and the session is really gone.
	b.mu.Lock()
	b.failed = nil
	b.mu.Unlock()
	w.discoverOnce(ctx)
	evs := drain(w.Events())
	if len(evs) != 1 || evs[0].Kind != SessionRemoved || evs[0].Reason != RemovedGone {
		t.Fatalf("expected gone removal after recovery, got %+v", evs)
	}
}

func TestListErrorEmitsErrorAndKeepsState(t *testing.T) {
	b := newFake()
	b.setSessions(claudeSession(b, "s1"))
	w, _ := newWatcher(t, b, Options{})
	ctx := context.Background()
	w.discoverOnce(ctx)
	drain(w.Events())

	b.mu.Lock()
	b.listErr = terminal.Unavailable("iTerm2", errors.New("socket closed"))
	b.mu.Unlock()
	w.discoverOnce(ctx)
	ev := recvEvent(t, w.Events())
	if ev.Kind != Error || !errors.Is(ev.Err, terminal.ErrUnavailable) {
		t.Fatalf("expected an unavailable Error event, got %+v", ev)
	}
	if len(w.Sessions()) != 1 {
		t.Fatal("a failed listing must leave tracked sessions alone")
	}

	// A read error emits Error for that session and changes nothing.
	b.mu.Lock()
	b.listErr = nil
	b.readErr["s1"] = errors.New("read failed")
	b.mu.Unlock()
	w.pollOnce(ctx)
	ev = recvEvent(t, w.Events())
	if ev.Kind != Error || ev.Session.ID != "s1" || ev.Err == nil {
		t.Fatalf("expected a per-session Error, got %+v", ev)
	}
	if snap, _ := w.Session("s1"); snap.Status != agent.StatusWorking || !snap.LastRead.IsZero() {
		t.Fatalf("read error must not touch state: %+v", snap)
	}
}

func TestFilterAndAgentsLimitDiscovery(t *testing.T) {
	b := newFake()
	c := claudeSession(b, "c1")
	b.setVar("x1", "path", "/home/me/projects/app")
	b.setScreen("x1", codexScreen)
	x := terminal.Session{ID: "x1", Name: "codex"}
	skip := claudeSession(b, "skip")
	b.setSessions(c, x, skip)
	w, _ := newWatcher(t, b, Options{
		Filter: func(s terminal.Session) bool { return s.ID != "skip" },
		Agents: []agent.Type{agent.Claude},
	})
	w.discoverOnce(context.Background())
	evs := drain(w.Events())
	if len(evs) != 1 || evs[0].Session.ID != "c1" {
		t.Fatalf("expected only c1 to be added, got %+v", evs)
	}
}

func TestUngatedDiscoveryAdmitsSessionsWithoutDir(t *testing.T) {
	b := newFake()
	b.setScreen("s1", "✻ Reading…\n")
	b.setSessions(terminal.Session{ID: "s1", Name: "claude"})
	// EventBuffer 1 + Parallelism 1 is the test marker for "no WatchDirs".
	w, _ := newWatcher(t, b, Options{EventBuffer: 1, Parallelism: 1})
	if len(w.opts.WatchDirs) != 0 {
		t.Fatal("test setup: expected ungated options")
	}
	w.discoverOnce(context.Background())
	ev := recvEvent(t, w.Events())
	if ev.Kind != SessionAdded || ev.Session.Dir != "" || ev.Session.Type != agent.Claude {
		t.Fatalf("ungated add = %+v", ev)
	}

	// The same session under a gate is skipped: no dir under the watch list.
	gated, _ := newWatcher(t, b, Options{})
	gated.discoverOnce(context.Background())
	if evs := drain(gated.Events()); len(evs) != 0 {
		t.Fatalf("gated mode must skip a session with no dir, got %+v", evs)
	}
}

func TestStyledScreensReadOnceAndCarryBell(t *testing.T) {
	b := newFake()
	b.setSessions(claudeSession(b, "s1"))
	b.styled["s1"] = "\x1b[32m✻ Reading…\x1b[0m\n"
	w, clock := newWatcher(t, b, Options{StyledScreens: true, EmitScreenReads: true})
	ctx := context.Background()
	w.discoverOnce(ctx)
	drain(w.Events())

	w.pollOnce(ctx)
	ev := recvEvent(t, w.Events())
	if ev.Kind != ScreenRead || ev.Screen != "\x1b[32m✻ Reading…\x1b[0m\n" {
		t.Fatalf("screen event = %+v", ev)
	}
	if b.styledN != 1 || len(b.reads) != 1 {
		t.Fatalf("expected exactly one styled read, reads=%v", b.reads)
	}
	snap, _ := w.Session("s1")
	if snap.Screen != "\x1b[32m✻ Reading…\x1b[0m\n" || snap.Status != agent.StatusWorking {
		t.Fatalf("snapshot = %+v", snap)
	}
	w.mu.Lock()
	plain := w.tracked["s1"].tr.LastScreen
	w.mu.Unlock()
	if plain != "✻ Reading…\n" {
		t.Fatalf("tracker must classify on stripped text, got %q", plain)
	}

	// A pending bell reaches the classifier on the styled path.
	b.mu.Lock()
	b.styled["s1"] = "\x1b[1mstill working\x1b[0m\n"
	b.bells["s1"] = true
	b.mu.Unlock()
	clock.Advance(time.Second)
	w.pollOnce(ctx)
	evs := drain(w.Events())
	var status *Event
	for i := range evs {
		if evs[i].Kind == StatusChanged {
			status = &evs[i]
		}
	}
	if status == nil || status.To != agent.StatusNeedsInput {
		t.Fatalf("bell must produce needs_input on the styled path, got %+v", evs)
	}
	if b.bells["s1"] {
		t.Fatal("bell must be consumed")
	}
}

func TestPlainPathCarriesBellInText(t *testing.T) {
	b := newFake()
	b.setSessions(claudeSession(b, "s1"))
	w, _ := newWatcher(t, b, Options{})
	ctx := context.Background()
	w.discoverOnce(ctx)
	drain(w.Events())
	b.setScreen("s1", "\x07still working\n")
	w.pollOnce(ctx)
	ev := recvEvent(t, w.Events())
	if ev.Kind != StatusChanged || ev.To != agent.StatusNeedsInput {
		t.Fatalf("plain-path bell = %+v", ev)
	}
}

func TestProcessReresolvedOnRetypeAndDeath(t *testing.T) {
	b := newFake()
	b.setSessions(claudeSession(b, "s1"))
	calls := 0
	alive := true
	w, _ := newWatcher(t, b, Options{})
	stubResolve(t, func(s terminal.Session) terminal.Process {
		calls++
		return terminal.Process{PID: 100 + calls, Argv: []string{"claude"}}
	}, func(int) bool { return alive })
	ctx := context.Background()

	// Identify resolves the process when it admits the session.
	w.discoverOnce(ctx)
	drain(w.Events())
	snap, _ := w.Session("s1")
	if snap.Process.PID != 101 {
		t.Fatalf("expected PID resolved at add time, got %+v", snap.Process)
	}

	// Alive and same type: no re-resolution.
	w.discoverOnce(ctx)
	if snap, _ = w.Session("s1"); snap.Process.PID != 101 {
		t.Fatalf("live process must not be re-resolved, got %+v", snap.Process)
	}

	// Retype: the pane now runs codex.
	b.setSessions(terminal.Session{ID: "s1", Name: "codex", Source: "pty"})
	w.discoverOnce(ctx)
	if snap, _ = w.Session("s1"); snap.Process.PID != 102 || snap.Type != agent.Codex {
		t.Fatalf("retype must re-resolve, got %+v", snap)
	}

	// PID death: cleared and re-resolved.
	alive = false
	w.discoverOnce(ctx)
	if snap, _ = w.Session("s1"); snap.Process.PID != 103 {
		t.Fatalf("dead pid must be re-resolved, got %+v", snap.Process)
	}
}

func TestRunLifecycleWithFakeClock(t *testing.T) {
	b := newFake()
	b.setSessions(claudeSession(b, "s1"))
	w, clock := newWatcher(t, b, Options{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	ev := recvEvent(t, w.Events())
	if ev.Kind != SessionAdded {
		t.Fatalf("first event = %+v", ev)
	}
	clock.AwaitTimers(t, 2) // discovery and poll timers armed

	b.setScreen("s1", "❯ ")
	clock.Advance(time.Second) // poll fires
	ev = recvEvent(t, w.Events())
	if ev.Kind != StatusChanged || ev.To != agent.StatusIdle {
		t.Fatalf("poll event = %+v", ev)
	}

	if err := w.Run(ctx); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second Run = %v, want ErrAlreadyRunning", err)
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancel")
	}
	if _, ok := <-w.Events(); ok {
		t.Fatal("Events must be closed after Run returns")
	}
}

func TestRunReturnsOnCancelWithFullBufferAndStalledConsumer(t *testing.T) {
	b := newFake()
	b.setSessions(claudeSession(b, "s1"), claudeSession(b, "s2"), claudeSession(b, "s3"))
	w, _ := newWatcher(t, b, Options{EventBuffer: 1})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	// Nobody reads. The first add fills the buffer; the second blocks.
	time.Sleep(50 * time.Millisecond)
	if got := w.Sessions(); len(got) != 3 {
		t.Fatalf("Sessions must not deadlock against a blocked send; got %d", len(got))
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run must return after cancel even with a stalled consumer")
	}
}

func TestSessionsSortedByID(t *testing.T) {
	b := newFake()
	b.setSessions(claudeSession(b, "b"), claudeSession(b, "a"), claudeSession(b, "c"))
	w, _ := newWatcher(t, b, Options{})
	w.discoverOnce(context.Background())
	got := w.Sessions()
	if len(got) != 3 || got[0].ID != "a" || got[1].ID != "b" || got[2].ID != "c" {
		t.Fatalf("Sessions = %v", got)
	}
}

func TestPlainPathScreenDropsBellByte(t *testing.T) {
	b := newFake()
	b.setSessions(claudeSession(b, "s1"))
	w, _ := newWatcher(t, b, Options{EmitScreenReads: true})
	ctx := context.Background()
	w.discoverOnce(ctx)
	drain(w.Events())
	b.setScreen("s1", "\x07still working\n")
	w.pollOnce(ctx)
	evs := drain(w.Events())
	for _, ev := range evs {
		if ev.Kind == ScreenRead && ev.Screen != "still working\n" {
			t.Fatalf("ScreenRead.Screen must not carry the bell, got %q", ev.Screen)
		}
	}
	snap, _ := w.Session("s1")
	if snap.Screen != "still working\n" {
		t.Fatalf("Snapshot.Screen must not carry the bell, got %q", snap.Screen)
	}
	if snap.Status != agent.StatusNeedsInput {
		t.Fatalf("the bell must still reach the tracker, status = %q", snap.Status)
	}
}

func TestRetypeEmitsTypeChanged(t *testing.T) {
	b := newFake()
	b.setSessions(claudeSession(b, "s1"))
	w, _ := newWatcher(t, b, Options{})
	ctx := context.Background()
	w.discoverOnce(ctx)
	drain(w.Events())

	b.setSessions(terminal.Session{ID: "s1", Name: "codex", Source: "pty"})
	w.discoverOnce(ctx)
	evs := drain(w.Events())
	var typed *Event
	for i := range evs {
		if evs[i].Kind == TypeChanged {
			typed = &evs[i]
		}
	}
	if typed == nil || typed.FromType != agent.Claude || typed.ToType != agent.Codex || typed.Session.Type != agent.Codex {
		t.Fatalf("expected TypeChanged claude->codex, got %+v", evs)
	}
	if TypeChanged.String() != "type" {
		t.Fatal("TypeChanged must have a name")
	}
}

func TestOrphanRemovalDoesNotReaddInSamePass(t *testing.T) {
	b := newFake()
	b.setSessions(claudeSession(b, "s1"))
	w, _ := newWatcher(t, b, Options{})
	ctx := context.Background()
	w.discoverOnce(ctx)
	drain(w.Events())

	// Make s1 an orphan on the next refresh while it stays listed and still
	// identifiable from its screen, so a same-pass re-add would succeed.
	w.mu.Lock()
	tr := &w.tracked["s1"].tr
	tr.Status = agent.StatusIdle
	tr.ScreenChecked = true
	tr.LastScreen = "user@host ~ %"
	tr.OrphanTicks = 1
	w.mu.Unlock()
	b.setSessions(terminal.Session{ID: "s1", Name: "zsh", Source: "pty"})
	b.setScreen("s1", codexScreen) // would identify as codex if re-considered
	w.discoverOnce(ctx)
	evs := drain(w.Events())
	if len(evs) != 1 || evs[0].Kind != SessionRemoved || evs[0].Reason != RemovedOrphan {
		t.Fatalf("expected only the orphan removal this pass, got %+v", evs)
	}
	if _, ok := w.Session("s1"); ok {
		t.Fatal("s1 must not be tracked after removal in the same pass")
	}
	// The next pass may identify it afresh.
	w.discoverOnce(ctx)
	if evs := drain(w.Events()); len(evs) != 1 || evs[0].Kind != SessionAdded || evs[0].Session.Type != agent.Codex {
		t.Fatalf("expected a fresh add on the following pass, got %+v", evs)
	}
}

func TestActivityChangedSnapshotNeverCarriesDeadPID(t *testing.T) {
	b := newFake()
	b.setSessions(claudeSession(b, "s1"))
	alive := true
	w, _ := newWatcher(t, b, Options{})
	stubResolve(t, func(terminal.Session) terminal.Process {
		return terminal.Process{PID: 4242, Argv: []string{"claude"}}
	}, func(int) bool { return alive })
	ctx := context.Background()
	w.discoverOnce(ctx) // add, resolving PID 4242
	w.discoverOnce(ctx) // refresh with the existing live PID
	drain(w.Events())

	// The process dies and the title changes in the same pass.
	alive = false
	b.setSessions(terminal.Session{ID: "s1", Name: "✳ Running tests", Source: "pty"})
	w.discoverOnce(ctx)
	evs := drain(w.Events())
	var act *Event
	for i := range evs {
		if evs[i].Kind == ActivityChanged {
			act = &evs[i]
		}
	}
	if act == nil {
		t.Fatalf("expected ActivityChanged, got %+v", evs)
	}
	if act.Session.Process.PID == 4242 {
		t.Fatal("event snapshot must not carry the PID the pass just found dead")
	}
	// After re-resolution the snapshot has the fresh process again.
	if snap, _ := w.Session("s1"); snap.Process.PID != 4242 {
		t.Fatalf("expected re-resolved PID, got %+v", snap.Process)
	}
}

func TestCancelStopsQueuedWork(t *testing.T) {
	b := newFake()
	var sessions []terminal.Session
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		sessions = append(sessions, claudeSession(b, id))
	}
	b.setSessions(sessions...)
	w, _ := newWatcher(t, b, Options{Parallelism: 1})
	w.discoverOnce(context.Background())
	drain(w.Events())

	// The first read cancels the context; with one slot, the four queued
	// reads must never run.
	ctx, cancel := context.WithCancel(context.Background())
	b.mu.Lock()
	b.readHook = func() { cancel() }
	before := len(b.reads)
	b.mu.Unlock()
	if w.pollOnce(ctx) {
		t.Fatal("pollOnce should report cancellation")
	}
	b.mu.Lock()
	reads := len(b.reads) - before
	b.mu.Unlock()
	if reads != 1 {
		t.Fatalf("expected exactly one read before cancellation, got %d", reads)
	}
	if evs := drain(w.Events()); len(evs) != 0 {
		t.Fatalf("no events after cancellation, got %+v", evs)
	}

	// Discovery: identification of new sessions is queued work too.
	b2 := newFake()
	var more []terminal.Session
	for _, id := range []string{"n1", "n2", "n3", "n4"} {
		b2.setScreen(id, codexScreen) // unknown title forces a screen read
		b2.setVar(id, "path", "/home/me/projects/app")
		more = append(more, terminal.Session{ID: id, Name: "app"})
	}
	b2.setSessions(more...)
	w2, _ := newWatcher(t, b2, Options{Parallelism: 1})
	ctx2, cancel2 := context.WithCancel(context.Background())
	b2.readHook = func() { cancel2() }
	if w2.discoverOnce(ctx2) {
		t.Fatal("discoverOnce should report cancellation")
	}
	if len(b2.reads) != 1 {
		t.Fatalf("expected one identify read before cancellation, got %d", len(b2.reads))
	}
	if len(w2.Sessions()) != 0 {
		t.Fatal("a cancelled discovery pass must not add sessions")
	}
}

func TestIdentifyScreenSeedsTracker(t *testing.T) {
	b := newFake()
	b.setVar("s1", "path", "/home/me/projects/app")
	// Unknown title, screen shows Codex at its prompt with a pending bell.
	b.setScreen("s1", "\x07"+codexScreen)
	b.setSessions(terminal.Session{ID: "s1", Name: "app", Source: "pty"})
	w, _ := newWatcher(t, b, Options{})
	w.discoverOnce(context.Background())
	ev := recvEvent(t, w.Events())
	if ev.Kind != SessionAdded || ev.Session.Type != agent.Codex {
		t.Fatalf("added = %+v", ev)
	}
	if ev.Session.Status != agent.StatusNeedsInput {
		t.Fatalf("the bell consumed during Identify must reach tracking, status = %q", ev.Session.Status)
	}
	if strings.Contains(ev.Session.Screen, "\x07") || ev.Session.Screen == "" {
		t.Fatalf("Snapshot.Screen should hold the identify read without the bell, got %q", ev.Session.Screen)
	}
	if len(b.reads) != 1 {
		t.Fatalf("the identify read is the first observation; expected one read, got %v", b.reads)
	}
	if ev.Session.LastRead.IsZero() {
		t.Fatal("seeding must count as a read")
	}
}

func TestReresolveFollowsProcessCwdWithinGate(t *testing.T) {
	b := newFake()
	b.setSessions(claudeSession(b, "s1"))
	cwd := "/home/me/projects/other"
	w, _ := newWatcher(t, b, Options{})
	stubResolve(t, func(terminal.Session) terminal.Process {
		return terminal.Process{PID: 7, Argv: []string{"claude"}, Cwd: cwd}
	}, func(int) bool { return true })
	ctx := context.Background()
	w.discoverOnce(ctx) // add using the agent process's cwd
	snap, _ := w.Session("s1")
	if snap.Dir != cwd {
		t.Fatalf("Dir = %q, want the process cwd %q", snap.Dir, cwd)
	}

	// The pane is reused for an agent in another in-scope project: forcing a
	// re-resolution (as a dead PID would) follows the new cwd.
	cwd = "/home/me/projects/third"
	w.mu.Lock()
	w.tracked["s1"].proc = terminal.Process{}
	w.mu.Unlock()
	w.discoverOnce(ctx)
	if snap, _ = w.Session("s1"); snap.Dir != cwd {
		t.Fatalf("re-resolution should follow the in-scope cwd, Dir = %q", snap.Dir)
	}
	if !containsString(w.projectDirs, cwd) {
		t.Fatal("new dir should be learned")
	}

	// A cwd outside the gate means the pane now runs an out-of-scope agent:
	// the session is removed rather than kept under the old directory.
	cwd = "/elsewhere/secret"
	w.mu.Lock()
	w.tracked["s1"].proc = terminal.Process{} // force re-resolution
	w.mu.Unlock()
	drain(w.Events())
	w.discoverOnce(ctx)
	evs := drain(w.Events())
	if len(evs) != 1 || evs[0].Kind != SessionRemoved || evs[0].Reason != RemovedOutOfScope || evs[0].Reason.String() != "out-of-scope" {
		t.Fatalf("expected out-of-scope removal, got %+v", evs)
	}
	if _, ok := w.Session("s1"); ok {
		t.Fatal("out-of-scope session must not remain tracked")
	}

	// The pane stays listed with an in-scope shell directory, but the agent
	// process is still outside the gate: identification must not re-admit it.
	w.discoverOnce(ctx)
	if evs := drain(w.Events()); len(evs) != 0 {
		t.Fatalf("out-of-scope agent must not be re-added, got %+v", evs)
	}
	if len(w.Sessions()) != 0 {
		t.Fatal("no sessions expected after out-of-scope removal")
	}

	// Once the process is back in scope it is admitted under its own cwd.
	cwd = "/home/me/projects/fourth"
	w.discoverOnce(ctx)
	evs = drain(w.Events())
	if len(evs) != 1 || evs[0].Kind != SessionAdded || evs[0].Session.Dir != cwd {
		t.Fatalf("expected re-add under the process cwd, got %+v", evs)
	}
}
