package watch

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/sethdeckard/atria/libatria/agent"
	"github.com/sethdeckard/atria/libatria/terminal"
)

// Defaults for Options fields left zero.
const (
	DefaultDiscoveryInterval = 3 * time.Second
	DefaultActiveInterval    = 1 * time.Second
	DefaultIdleInterval      = 3 * time.Second
	DefaultParallelism       = 4
	DefaultEventBuffer       = 256
	DefaultRecheckInterval   = 30 * time.Second
)

// ErrAlreadyRunning is returned by Run when Run has already been called on
// this Watcher, whether or not that call has returned. A Watcher is single-use.
var ErrAlreadyRunning = errors.New("watch: already running")

// Options configures a Watcher. The zero value is usable.
type Options struct {
	DiscoveryInterval time.Duration // ListSessions, Refresh, and Identify cadence; 0 means DefaultDiscoveryInterval
	ActiveInterval    time.Duration // screen-read cadence for working, needs_input, and error sessions; 0 means DefaultActiveInterval
	IdleInterval      time.Duration // screen-read cadence for idle sessions; 0 means DefaultIdleInterval
	ScreenLines       int           // lines per screen read; 0 means DefaultScreenLines

	// WatchDirs is passed to Identify and decides its mode: with directories,
	// only sessions under them are added; with none, every agent session the
	// backend lists is a candidate.
	WatchDirs []string
	// ProjectDirs seeds Identify's title matching; the directories of tracked
	// sessions are added as they are learned.
	ProjectDirs []string
	// Filter, when set, must return true for a listed session to be
	// considered at all. It runs before anything else.
	Filter func(terminal.Session) bool
	// Agents, when set, limits which agent types are added.
	Agents []agent.Type
	// RecheckInterval is how long a session ruled out as an agent is left
	// alone. It is identified again sooner when its title, foreground job,
	// TTY, or the processes on that TTY change, which is how an agent
	// starting in it is noticed on the next pass, and on every pass for a
	// few seconds after, while a new agent draws its UI. 0 means
	// DefaultRecheckInterval; a negative value identifies every unknown
	// session on every pass.
	RecheckInterval time.Duration

	Parallelism     int  // concurrent ReadScreen and Identify calls; 0 means DefaultParallelism
	EventBuffer     int  // Events channel capacity; 0 means DefaultEventBuffer
	EmitScreenReads bool // emit a ScreenRead event on every poll
	// StyledScreens reads once through terminal.StyledReader when the backend
	// has one, classifies on the ANSI-stripped text, and carries the styled
	// text in ScreenRead events and Snapshot.Screen. Bell delivery is
	// unchanged: the Watcher asks terminal.BellSource after each styled read.
	StyledScreens bool

	Clock Clock                            // nil means the wall clock
	Logf  func(format string, args ...any) // optional debug logging
}

// Watcher discovers agent sessions and follows their status, delivering
// changes as Events. Create one with New and drive it with Run.
type Watcher struct {
	backend terminal.Backend
	opts    Options
	clock   Clock
	events  chan Event
	running atomic.Bool

	mu          sync.Mutex
	tracked     map[string]*tracked
	ruledOut    map[string]ruledOut
	projectDirs []string
}

// ruledOut is what a listed session looked like when Identify last ruled it
// out, so a later pass can tell whether anything that decides the answer
// has changed.
type ruledOut struct {
	name, job, tty string
	pids           string // the TTY's process IDs, from terminal.PIDsByTTY
	at             time.Time
	settleUntil    time.Time // identify on every pass until then
}

// settleWindow is how long a session stays under watch after it first
// appears or something about it changes. An agent that has just started may
// not have drawn anything recognisable when the change is noticed, and once
// it has, nothing else about the pane may change until RecheckInterval.
const settleWindow = 10 * time.Second

// tracked is one session under watch.
type tracked struct {
	sess   terminal.Session
	dir    string
	proc   terminal.Process
	tr     Tracker
	screen string // last screen as reported (styled or plain)
}

// Seams for tests: process resolution shells out, and liveness is a signal.
var (
	resolveProcess = ResolveProcess
	pidsByTTY      = terminal.PIDsByTTY
	processAlive   = func(pid int) bool {
		p, err := os.FindProcess(pid)
		if err != nil {
			return false
		}
		err = p.Signal(syscall.Signal(0))
		return err == nil || errors.Is(err, syscall.EPERM)
	}
)

// New creates a Watcher over b. Nothing happens until Run.
func New(b terminal.Backend, opts Options) *Watcher {
	if opts.DiscoveryInterval <= 0 {
		opts.DiscoveryInterval = DefaultDiscoveryInterval
	}
	if opts.ActiveInterval <= 0 {
		opts.ActiveInterval = DefaultActiveInterval
	}
	if opts.IdleInterval <= 0 {
		opts.IdleInterval = DefaultIdleInterval
	}
	if opts.ScreenLines <= 0 {
		opts.ScreenLines = DefaultScreenLines
	}
	if opts.Parallelism <= 0 {
		opts.Parallelism = DefaultParallelism
	}
	if opts.EventBuffer <= 0 {
		opts.EventBuffer = DefaultEventBuffer
	}
	if opts.RecheckInterval == 0 {
		opts.RecheckInterval = DefaultRecheckInterval
	}
	clock := opts.Clock
	if clock == nil {
		clock = realClock{}
	}
	return &Watcher{
		backend:     b,
		opts:        opts,
		clock:       clock,
		events:      make(chan Event, opts.EventBuffer),
		tracked:     make(map[string]*tracked),
		ruledOut:    make(map[string]ruledOut),
		projectDirs: append([]string(nil), opts.ProjectDirs...),
	}
}

// Events returns the channel Run delivers on. It is closed when Run returns.
func (w *Watcher) Events() <-chan Event { return w.events }

// Run discovers immediately, then alternates discovery and screen polls on
// their intervals until ctx is done. It returns ctx.Err() and closes Events.
// Run may be called once per Watcher; create a new Watcher to watch again.
// Cancellation is honoured between backend calls and at every send, so Run
// returns once the calls in flight finish; ctx does not interrupt a backend
// call. A second concurrent Run returns ErrAlreadyRunning.
func (w *Watcher) Run(ctx context.Context) error {
	if !w.running.CompareAndSwap(false, true) {
		return ErrAlreadyRunning
	}
	defer close(w.events)

	if !w.discoverOnce(ctx) {
		return ctx.Err()
	}
	discovery := w.clock.After(w.opts.DiscoveryInterval)
	poll := w.clock.After(w.opts.ActiveInterval)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-discovery:
			if !w.discoverOnce(ctx) {
				return ctx.Err()
			}
			discovery = w.clock.After(w.opts.DiscoveryInterval)
		case <-poll:
			if !w.pollOnce(ctx) {
				return ctx.Err()
			}
			poll = w.clock.After(w.opts.ActiveInterval)
		}
	}
}

// Sessions returns a snapshot of every tracked session, sorted by ID.
func (w *Watcher) Sessions() []Snapshot {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]Snapshot, 0, len(w.tracked))
	for _, t := range w.tracked {
		out = append(out, t.snapshot())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Session returns a snapshot of one tracked session.
func (w *Watcher) Session(id string) (Snapshot, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	t, ok := w.tracked[id]
	if !ok {
		return Snapshot{}, false
	}
	return t.snapshot(), true
}

func (t *tracked) snapshot() Snapshot {
	return Snapshot{
		Session:      t.sess,
		Dir:          t.dir,
		Type:         t.tr.Type,
		Process:      t.proc,
		Status:       t.tr.Status,
		Activity:     t.tr.Activity,
		Attention:    t.tr.Attention,
		LastActivity: t.tr.LastActivity,
		LastRead:     t.tr.LastRead,
		Screen:       t.screen,
	}
}

// emit delivers events in order, blocking on a full channel but giving up
// when ctx is done. It reports false once ctx is done; the remaining events
// are abandoned.
func (w *Watcher) emit(ctx context.Context, events []Event) bool {
	for _, ev := range events {
		select {
		case w.events <- ev:
		case <-ctx.Done():
			return false
		}
	}
	return true
}

func (w *Watcher) logf(format string, args ...any) {
	if w.opts.Logf != nil {
		w.opts.Logf(format, args...)
	}
}

// discoverOnce runs one discovery pass. It reports false when ctx ended.
func (w *Watcher) discoverOnce(ctx context.Context) bool {
	now := w.clock.Now()
	listing, err := w.list()
	if err != nil {
		w.logf("watch: list sessions: %v", err)
		return w.emit(ctx, []Event{{Kind: Error, Time: now, Err: err}})
	}
	var events []Event
	failed := map[string]bool{}
	for _, f := range listing.Failed {
		failed[f.Source] = true
		if f.Err != nil {
			w.logf("watch: list sessions: %v", f)
			events = append(events, Event{Kind: Error, Time: now, Err: f})
		}
	}

	live := make(map[string]terminal.Session, len(listing.Sessions))
	var candidates []terminal.Session
	for _, s := range listing.Sessions {
		if w.opts.Filter != nil && !w.opts.Filter(s) {
			continue
		}
		live[s.ID] = s
	}

	var reresolve []*tracked
	removed := map[string]bool{}
	retainedTTY := map[string]bool{}
	w.mu.Lock()
	for id, t := range w.tracked {
		sess, ok := live[id]
		if !ok {
			if failed[t.tr.Source] {
				if t.sess.TTY != "" {
					retainedTTY[t.sess.TTY] = true
				}
				continue // its source didn't answer; keep it
			}
			delete(w.tracked, id)
			removed[id] = true
			events = append(events, Event{Kind: SessionRemoved, Time: now, Session: t.snapshot(), Reason: RemovedGone})
			continue
		}
		r := t.tr.Refresh(sess, now)
		t.sess = sess
		if r.Orphan {
			delete(w.tracked, id)
			removed[id] = true
			events = append(events, Event{Kind: SessionRemoved, Time: now, Session: t.snapshot(), Reason: RemovedOrphan})
			continue
		}
		// Settle the process before taking any snapshot, so an event never
		// carries a PID this pass is about to clear.
		if r.Retyped || t.proc.PID == 0 || !processAlive(t.proc.PID) {
			t.proc = terminal.Process{} // stale or unknown: never report a process not seen alive
			reresolve = append(reresolve, t)
		}
		if r.Retyped {
			events = append(events, Event{Kind: TypeChanged, Time: now, Session: t.snapshot(), FromType: r.PrevType, ToType: t.tr.Type})
		}
		if r.ActivityChanged {
			events = append(events, Event{Kind: ActivityChanged, Time: now, Session: t.snapshot()})
		}
	}
	// A session removed in this pass is not a candidate in this pass; it can
	// be identified afresh on the next one if it is still listed. Nor is one
	// on the TTY of a session kept because its source failed: the composite
	// dedups by TTY only within a listing, so with that source missing
	// another source can list the same pane under its own ID.
	for id, s := range live {
		if _, ok := w.tracked[id]; !ok && !removed[id] && (s.TTY == "" || !retainedTTY[s.TTY]) {
			candidates = append(candidates, s)
		}
	}
	for id := range w.ruledOut {
		if _, ok := live[id]; !ok {
			delete(w.ruledOut, id)
		}
	}
	projectDirs := append([]string(nil), w.projectDirs...)
	w.mu.Unlock()

	if !w.emit(ctx, events) {
		return false
	}
	events = nil
	candidates, pids := w.reopened(candidates, now)

	// Identify new sessions and re-resolve processes outside the lock; both
	// shell out. Bounded parallelism keeps process pressure predictable, and
	// queued work is dropped once ctx is done so shutdown waits only for
	// calls already in flight.
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
	identities := make([]Identity, len(candidates))
	procs := make([]terminal.Process, len(reresolve))
	sem := make(chan struct{}, w.opts.Parallelism)
	var wg sync.WaitGroup
	for i, s := range candidates {
		wg.Add(1)
		go func(i int, s terminal.Session) {
			defer wg.Done()
			if !acquire(ctx, sem) {
				return
			}
			defer func() { <-sem }()
			identities[i] = Identify(w.backend, s, IdentifyOptions{
				WatchDirs: w.opts.WatchDirs, ProjectDirs: projectDirs, ScreenLines: w.opts.ScreenLines,
			})
		}(i, s)
	}
	for i, t := range reresolve {
		wg.Add(1)
		go func(i int, sess terminal.Session) {
			defer wg.Done()
			if !acquire(ctx, sem) {
				return
			}
			defer func() { <-sem }()
			procs[i], _ = resolveProcess(w.backend, sess)
		}(i, t.sess)
	}
	wg.Wait()
	if ctx.Err() != nil {
		return false
	}

	w.mu.Lock()
	for i, t := range reresolve {
		if _, still := w.tracked[t.sess.ID]; !still {
			continue
		}
		t.proc = procs[i]
		// A replacement agent may run in another project: follow its cwd.
		// In gated mode a cwd outside the watch list means the pane now runs
		// an agent out of scope, so the session is dropped rather than kept
		// under a directory that is no longer true. It is identified afresh
		// if a later listing finds it in scope.
		cwd := procs[i].Cwd
		if cwd == "" || cwd == t.dir {
			continue
		}
		if len(w.opts.WatchDirs) > 0 && !terminal.UnderAnyDir(cwd, w.opts.WatchDirs) {
			delete(w.tracked, t.sess.ID)
			removed[t.sess.ID] = true
			events = append(events, Event{Kind: SessionRemoved, Time: now, Session: t.snapshot(), Reason: RemovedOutOfScope})
			continue
		}
		t.dir = cwd
		if !containsString(w.projectDirs, cwd) {
			w.projectDirs = append(w.projectDirs, cwd)
		}
	}
	for i, s := range candidates {
		id := identities[i]
		if !id.OK() {
			if id.Skip != SkipNone {
				w.logf("watch: skip %s (%s)", s.ID, id.Skip)
			}
			w.ruleOut(s, id, pids, now)
			continue
		}
		if !w.wantsAgent(id.Type) {
			w.ruleOut(s, id, pids, now)
			continue
		}
		if _, exists := w.tracked[s.ID]; exists {
			continue
		}
		delete(w.ruledOut, s.ID)
		t := &tracked{
			sess: s,
			dir:  id.Dir,
			proc: id.Process,
			tr:   Tracker{Type: id.Type, Source: s.Source, Status: agent.StatusWorking},
		}
		if id.Observed {
			// Identify already read the screen (and may have consumed a
			// pending bell); that read is the session's first observation.
			t.tr.Observe(id.Screen, now)
			t.screen = strings.ReplaceAll(id.Screen, "\x07", "")
		}
		w.tracked[s.ID] = t
		if id.Dir != "" && !containsString(w.projectDirs, id.Dir) {
			w.projectDirs = append(w.projectDirs, id.Dir)
		}
		events = append(events, Event{Kind: SessionAdded, Time: now, Session: t.snapshot()})
	}
	w.mu.Unlock()
	return w.emit(ctx, events)
}

// list returns one listing and the sources that failed while producing it.
// A terminal.Lister gives both from one call. Otherwise the failed sources
// come from terminal.FailureReporter, with a nil Err because the reporter
// doesn't say what went wrong, and they are retained but not reported as
// Error events.
func (w *Watcher) list() (terminal.Listing, error) {
	if l, ok := w.backend.(terminal.Lister); ok {
		return l.List()
	}
	sessions, err := w.backend.ListSessions()
	if err != nil {
		return terminal.Listing{}, err
	}
	listing := terminal.Listing{Sessions: sessions}
	if fr, ok := w.backend.(terminal.FailureReporter); ok {
		for _, src := range fr.FailedSources() {
			listing.Failed = append(listing.Failed, &terminal.SourceError{Source: src})
		}
	}
	return listing, nil
}

// reopened skips unchanged, previously rejected candidates once their settle
// window ends, until RecheckInterval expires. It returns the candidates to
// identify and the process lists it read, which also record what a new
// ruling saw. The process lists come from one ps run, made only
// when a candidate has a TTY; if it fails, candidates with a TTY are all
// identified.
func (w *Watcher) reopened(candidates []terminal.Session, now time.Time) ([]terminal.Session, map[string]string) {
	if w.opts.RecheckInterval < 0 {
		return candidates, nil
	}
	var pids map[string]string
	for _, s := range candidates {
		if s.TTY != "" {
			var err error
			if pids, err = pidsByTTY(); err != nil {
				w.logf("watch: process list: %v", err)
				pids = nil
			}
			break
		}
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	var keep []terminal.Session
	for _, s := range candidates {
		r, ok := w.ruledOut[s.ID]
		same := ok && r.name == s.Name && r.job == s.Job && r.tty == s.TTY &&
			now.Sub(r.at) < w.opts.RecheckInterval && !now.Before(r.settleUntil) &&
			(s.TTY == "" || (pids != nil && r.pids == ttyPIDs(pids, s.TTY)))
		if !same {
			keep = append(keep, s)
		}
	}
	return keep, pids
}

// ruleOut remembers a candidate that Identify did not make a tracked agent,
// so later passes can skip it. Two rulings prove nothing and are not
// remembered, so the session is tried again on the next pass: a failed
// screen read, and an agent process found on the TTY before its title or
// screen names it. A session that is new, or whose title, job, TTY, or
// processes changed since the last ruling, is also identified on every pass
// for settleWindow. Caller holds w.mu.
func (w *Watcher) ruleOut(s terminal.Session, id Identity, pids map[string]string, now time.Time) {
	if w.opts.RecheckInterval < 0 || id.Skip == SkipScreenReadFailed || (id.Type == "" && id.Process.PID != 0) {
		delete(w.ruledOut, s.ID)
		return
	}
	r := ruledOut{name: s.Name, job: s.Job, tty: s.TTY, pids: ttyPIDs(pids, s.TTY), at: now}
	prev, ok := w.ruledOut[s.ID]
	switch {
	case !ok || prev.name != r.name || prev.job != r.job || prev.tty != r.tty || prev.pids != r.pids:
		r.settleUntil = now.Add(settleWindow)
	case now.Before(prev.settleUntil):
		r.settleUntil = prev.settleUntil
	}
	w.ruledOut[s.ID] = r
}

// ttyPIDs looks up a session's TTY ("/dev/ttys003" or "ttys003") in a
// terminal.PIDsByTTY result.
func ttyPIDs(pids map[string]string, tty string) string {
	return pids[strings.TrimPrefix(tty, "/dev/")]
}

func (w *Watcher) wantsAgent(t agent.Type) bool {
	if len(w.opts.Agents) == 0 {
		return true
	}
	for _, a := range w.opts.Agents {
		if a == t {
			return true
		}
	}
	return false
}

type screenResult struct {
	id     string
	plain  string
	styled string
	err    error
}

// pollOnce reads the screen of every session whose interval has elapsed and
// applies the results. It reports false when ctx ended.
func (w *Watcher) pollOnce(ctx context.Context) bool {
	now := w.clock.Now()
	w.mu.Lock()
	var due []terminal.Session
	for _, t := range w.tracked {
		if t.tr.LastRead.IsZero() || now.Sub(t.tr.LastRead) >= PollInterval(t.tr.Status, w.opts.ActiveInterval, w.opts.IdleInterval) {
			due = append(due, t.sess)
		}
	}
	w.mu.Unlock()
	if len(due) == 0 {
		return true
	}
	sort.Slice(due, func(i, j int) bool { return due[i].ID < due[j].ID })

	styledReader, styled := w.backend.(terminal.StyledReader)
	styled = styled && w.opts.StyledScreens
	bells, _ := w.backend.(terminal.BellSource)

	results := make([]screenResult, len(due))
	sem := make(chan struct{}, w.opts.Parallelism)
	var wg sync.WaitGroup
	for i, s := range due {
		wg.Add(1)
		go func(i int, s terminal.Session) {
			defer wg.Done()
			if !acquire(ctx, sem) {
				results[i] = screenResult{id: s.ID, err: ctx.Err()}
				return
			}
			defer func() { <-sem }()
			res := screenResult{id: s.ID}
			if styled {
				res.styled, res.err = styledReader.ReadScreenStyled(s.ID, w.opts.ScreenLines)
				if res.err == nil {
					res.plain = ansi.Strip(res.styled)
					// The plain read consumes the bell inside the text; the
					// styled read can't carry it, so ask for it separately.
					if bells != nil && bells.ConsumeBell(s.ID) {
						res.plain = "\x07" + res.plain
					}
				}
			} else {
				res.plain, res.err = w.backend.ReadScreen(s.ID, w.opts.ScreenLines)
				// The PTY backend prefixes a pending bell to the plain read
				// for the classifier; the display copy must not carry it.
				res.styled = strings.ReplaceAll(res.plain, "\x07", "")
			}
			results[i] = res
		}(i, s)
	}
	wg.Wait()
	if ctx.Err() != nil {
		return false
	}

	var events []Event
	readAt := w.clock.Now()
	w.mu.Lock()
	for _, res := range results {
		t, ok := w.tracked[res.id]
		if !ok {
			continue
		}
		if res.err != nil {
			events = append(events, Event{Kind: Error, Time: readAt, Session: t.snapshot(), Err: res.err})
			continue
		}
		tr := t.tr.Observe(res.plain, readAt)
		t.screen = res.styled
		if w.opts.EmitScreenReads {
			events = append(events, Event{Kind: ScreenRead, Time: readAt, Session: t.snapshot(), Screen: res.styled})
		}
		if tr.StatusChanged() {
			line := tr.MatchLine
			if tr.To == agent.StatusNeedsInput {
				line = t.tr.Attention
			}
			events = append(events, Event{Kind: StatusChanged, Time: readAt, Session: t.snapshot(), From: tr.From, To: tr.To, MatchLine: line})
		}
	}
	w.mu.Unlock()
	return w.emit(ctx, events)
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// acquire takes a semaphore slot unless ctx ends first. When a slot frees at
// the same moment ctx ends, select may pick either case, so the slot is
// re-checked against ctx and released if the work should not start.
func acquire(ctx context.Context, sem chan struct{}) bool {
	select {
	case sem <- struct{}{}:
		if ctx.Err() != nil {
			<-sem
			return false
		}
		return true
	case <-ctx.Done():
		return false
	}
}
