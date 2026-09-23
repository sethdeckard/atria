package watch

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sethdeckard/atria/libatria/agent"
	"github.com/sethdeckard/atria/libatria/terminal"
)

// fakeBackend is an in-memory terminal.Backend with FailureReporter,
// StyledReader, and BellSource, so every Watcher path can be exercised
// without a terminal. Sessions carry no TTY, which keeps DiscoverCWD off ps
// and lsof; a GetVar "path" under the watch dirs resolves the directory.
type fakeBackend struct {
	mu       sync.Mutex
	sessions []terminal.Session
	listErr  error
	screens  map[string]string
	styled   map[string]string
	readErr  map[string]error
	vars     map[string]map[string]string // id -> var -> value
	failed   []string
	bells    map[string]bool
	reads    []string
	styledN  int
	readHook func() // called on each ReadScreen, before recording
}

func newFake() *fakeBackend {
	return &fakeBackend{
		screens: map[string]string{}, styled: map[string]string{}, readErr: map[string]error{},
		vars: map[string]map[string]string{}, bells: map[string]bool{},
	}
}

func (f *fakeBackend) setSessions(s ...terminal.Session) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions = s
}

func (f *fakeBackend) setScreen(id, plain string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.screens[id] = plain
}

func (f *fakeBackend) setVar(id, name, val string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.vars[id] == nil {
		f.vars[id] = map[string]string{}
	}
	f.vars[id][name] = val
}

func (f *fakeBackend) Available() error { return nil }
func (f *fakeBackend) ListSessions() ([]terminal.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	return append([]terminal.Session(nil), f.sessions...), nil
}
func (f *fakeBackend) NewSession() (string, error)     { return "", errors.New("unsupported") }
func (f *fakeBackend) SendText(string, string) error   { return nil }
func (f *fakeBackend) RunCommand(string, string) error { return nil }
func (f *fakeBackend) FocusSession(string) error       { return nil }
func (f *fakeBackend) MonitorOutput(string, string, string) (int, error) {
	return 0, errors.New("unsupported")
}
func (f *fakeBackend) ReadScreen(id string, _ int) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readHook != nil {
		f.readHook()
	}
	f.reads = append(f.reads, id)
	if err := f.readErr[id]; err != nil {
		return "", err
	}
	return f.screens[id], nil
}
func (f *fakeBackend) ReadScreenStyled(id string, _ int) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.styledN++
	f.reads = append(f.reads, "styled:"+id)
	if err := f.readErr[id]; err != nil {
		return "", err
	}
	if s, ok := f.styled[id]; ok {
		return s, nil
	}
	return f.screens[id], nil
}
func (f *fakeBackend) GetVar(id, name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if v, ok := f.vars[id][name]; ok {
		return v, nil
	}
	return "", errors.New("no such var")
}
func (f *fakeBackend) FailedSources() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.failed...)
}
func (f *fakeBackend) ConsumeBell(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	b := f.bells[id]
	f.bells[id] = false
	return b
}

// fakeClock lets a test drive the Watcher's timers.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []waiter
}

type waiter struct {
	at time.Time
	ch chan time.Time
}

func newClock() *fakeClock { return &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.waiters = append(c.waiters, waiter{at: c.now.Add(d), ch: ch})
	return ch
}

// Advance moves time forward and fires every timer that is now due.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var keep []waiter
	var fire []waiter
	for _, w := range c.waiters {
		if !w.at.After(c.now) {
			fire = append(fire, w)
		} else {
			keep = append(keep, w)
		}
	}
	c.waiters = keep
	now := c.now
	c.mu.Unlock()
	for _, w := range fire {
		w.ch <- now
	}
}

// AwaitTimers blocks until n timers are armed, so a test doesn't advance the
// clock before Run has scheduled its next tick.
func (c *fakeClock) AwaitTimers(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		got := len(c.waiters)
		c.mu.Unlock()
		if got >= n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d timers", n)
}

// recvEvent reads one event or fails after a second.
func recvEvent(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("events channel closed")
		}
		return ev
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for an event")
	}
	return Event{}
}

// drain collects every event currently buffered.
func drain(ch <-chan Event) []Event {
	var out []Event
	for {
		select {
		case ev := <-ch:
			out = append(out, ev)
		default:
			return out
		}
	}
}

// stubResolve replaces process resolution and liveness for a test.
func stubResolve(t *testing.T, resolve func(terminal.Session) terminal.Process, alive func(int) bool) {
	t.Helper()
	prevR, prevA := resolveProcess, processAlive
	resolveProcess = func(_ terminal.Backend, s terminal.Session) (terminal.Process, agent.Type) {
		p := resolve(s)
		return p, ""
	}
	processAlive = alive
	t.Cleanup(func() { resolveProcess, processAlive = prevR, prevA })
}
