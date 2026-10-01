package libatria

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/sethdeckard/atria/libatria/terminal"
	"github.com/sethdeckard/atria/libatria/terminal/deviceterm"
	"github.com/sethdeckard/atria/libatria/terminal/iterm"
	"github.com/sethdeckard/atria/libatria/terminal/kitty"
	"github.com/sethdeckard/atria/libatria/terminal/pty"
	"github.com/sethdeckard/atria/libatria/terminal/tmux"
	"github.com/sethdeckard/atria/libatria/terminal/wezterm"
)

// DefaultCacheTTL is how long Stack's cached backend serves a session list
// before asking the terminals again, when Options.CacheTTL is zero.
const DefaultCacheTTL = 5 * time.Second

// Options configures Open. The zero value opens a PTY-only stack.
type Options struct {
	// Integrations names the terminals to probe, from Names. Unknown names
	// are recorded by Stack.Ignored and don't fail Open.
	Integrations []string

	TmuxPath       string // tmux binary; "" means $PATH
	TmuxSession    string // tmux session to launch into; "" means the current one, or a detached fallback
	KittenPath     string // kitten binary; "" means $PATH
	WezTermPath    string // wezterm binary; "" means $PATH
	DeviceTermPath string // deviceterm binary; "" means $PATH, then $DEVICETERM_SHIM_DIR
	PTYCols        int    // embedded terminal width; 0 means pty.DefaultCols
	PTYRows        int    // embedded terminal height; 0 means pty.DefaultRows

	CacheTTL       time.Duration // session-list cache; 0 means DefaultCacheTTL
	CommandTimeout time.Duration // per-call bound on CLI clients and the iTerm2 round trip; 0 means terminal.DefaultCommandTimeout

	// SelfTTY is the calling process's terminal; sessions on it are dropped
	// from listings so a dashboard doesn't discover its own pane. "" means
	// terminal.TTYForPID(os.Getpid()). NoSelfTTYFilter disables the filter
	// for a daemon that has no terminal or wants to see everything.
	SelfTTY         string
	NoSelfTTYFilter bool

	// ProgramName is how the terminals see the caller: the iTerm2
	// AppleScript app name and advisory header, the detached tmux fallback
	// session, and the DeviceTerm reason text. Each client applies its own
	// default when it is empty ("libatria" for iTerm2 and tmux, "this
	// program" for DeviceTerm).
	ProgramName string

	// AllowITermPrompt lets the iTerm2 client Open builds request credentials
	// through AppleScript when the process runs inside iTerm2 and the socket
	// demands authentication. That client keeps the setting, so it can prompt
	// again if it reconnects and is refused. The clients Enable and Reprobe
	// build don't prompt under this flag, because a dialog over a running
	// TUI is what it exists to prevent. A background process that should
	// prompt wherever it runs sets ITermPromptAnywhere instead.
	AllowITermPrompt bool

	// ITermPromptAnywhere lets every iTerm2 client the stack builds, in
	// Open, Enable, and Reprobe, request credentials through AppleScript
	// when the socket demands authentication, whatever terminal the process
	// runs in or none. It is for a background process with no screen of its
	// own for a dialog to cover; one started before iTerm2 authenticates
	// through Reprobe. The first request triggers macOS's Automation consent,
	// which macOS attributes to the app the process was started from, not
	// to the binary. It supersedes AllowITermPrompt. Leave it false in a TUI.
	//
	// Open waits for the person to answer, up to iterm.DefaultAuthTimeout.
	// Enable and Reprobe wait too, without holding the Stack's lock, though
	// Close and calls reaching a live iTerm2 client being retried may still
	// wait. A request that is declined or times out turns prompting off
	// for every iTerm2 client of the Stack, reconnects included, until
	// Reauthorize, so nothing asks again on its own.
	ITermPromptAnywhere bool

	// Getenv reads the environment for primary selection and EnvMatches;
	// nil means os.Getenv. The clients themselves still read the real
	// environment (DEVICETERM_SESSION, KITTY_LISTEN_ON, ITERM2_COOKIE).
	Getenv func(string) string
}

// Status is the state of one backend in the stack.
type Status struct {
	Name      string // integration name, or "pty"
	Source    string // composite source label, Source(Name)
	Enabled   bool   // named in Options.Integrations or enabled since; always true for pty
	Available bool   // the last probe passed
	Active    bool   // Available and the environment matches; for DeviceTerm, Available and primary
	Launch    bool   // this backend is the primary and takes NewSession
	Reason    string // the probe error when Enabled and not Available
	// Err is the probe error behind Reason, for errors.Is: ErrUnavailable
	// (the terminal isn't running), iterm.ErrAuthRequired (prompting is
	// off), or iterm.ErrAuthFailed (a credential request was declined or
	// timed out; see Stack.Reauthorize). Status reflects probes and the
	// iTerm2 credential gate, not the health of every later call: a live
	// client that starts failing surfaces through FailureReporter on
	// listings, as the watch.Watcher reports it.
	Err error
}

// RoleChange reports a backend moving between the primary and integration
// roles. Its sessions are listed unprefixed while primary and as Prefix +
// id while an integration, so anything held by session ID has to follow.
// It is returned whenever the primary changed. Source and Prefix name the
// backend whose IDs moved; Source is empty when none did, which happens
// only when the outgoing primary had no integration entry (DeviceTerm is the
// one backend without one).
type RoleChange struct {
	Source     string // composite source whose sessions change id
	Prefix     string // that source's integration prefix, e.g. "pty:"
	ToPrefixed bool   // leaving primary adds Prefix; becoming primary strips it
	NewPrimary string // source of the launch target after the change
}

// Stack is an opened set of terminal backends: a PTY client that is always
// present, the integrations that answered their probe, a CompositeBackend
// that routes between them with one of them as the launch primary, and a
// CachedBackend in front. Build one with Open. All methods are safe for
// concurrent use, including Enable, Disable, and Reprobe while a
// watch.Watcher polls Backend. Enable and Reprobe probe without holding the
// Stack's lock, so a probe waiting on a terminal or on iTerm2's credential
// dialog doesn't hold up Statuses or the other methods, except Close. When
// Reprobe retries a live iTerm2 client, calls through Backend that reach it
// wait for its request.
type Stack struct {
	mu        sync.Mutex
	opts      Options
	getenv    func(string) string
	pty       *pty.Client
	composite *terminal.CompositeBackend
	cached    *terminal.CachedBackend
	clients   map[string]terminal.Backend // enabled integrations whose probe passed, by name
	statuses  map[string]*Status          // every known name plus pty
	ignored   []string
	probing   map[string]bool                                 // names with a probe in flight
	gen       map[string]uint64                               // bumped by Disable, so a probe that outlived it is dropped
	authGate  *iterm.AuthGate                                 // shared by every iTerm2 client, so a refusal outlives the client
	build     func(name string, atOpen bool) terminal.Backend // newClient; tests replace it
	gateErr   func() error                                    // authGate.Err; tests replace it
}

// Open builds the stack. It always creates the PTY client, probes each named
// integration with Available, and records a Status for every known name
// whether or not it was asked for. The primary is the highest-ranked
// available integration whose environment matches (EnvMatches), else PTY;
// when PTY isn't primary it is added as the "pty:" integration so its
// sessions stay listed. DeviceTerm is never added as an integration: it is
// the primary or absent.
//
// The error return is reserved; nothing in the current assembly fails, and
// probe failures are reported through Statuses instead.
func Open(opts Options) (*Stack, error) {
	s := &Stack{
		opts:     opts,
		getenv:   opts.Getenv,
		clients:  make(map[string]terminal.Backend),
		statuses: make(map[string]*Status),
		probing:  make(map[string]bool),
		gen:      make(map[string]uint64),
		authGate: &iterm.AuthGate{},
	}
	s.build = s.newClient
	s.gateErr = s.authGate.Err
	if s.getenv == nil {
		s.getenv = os.Getenv
	}
	s.pty = pty.NewClient(opts.PTYCols, opts.PTYRows)
	s.statuses[PTY] = &Status{Name: PTY, Source: PTY, Enabled: true, Available: true}
	for _, name := range names {
		s.statuses[name] = &Status{Name: name, Source: Source(name)}
	}

	var integrations []terminal.Integration
	seen := make(map[string]bool)
	for _, name := range opts.Integrations {
		st, known := s.statuses[name]
		if !known || name == PTY {
			s.ignored = append(s.ignored, name)
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		st.Enabled = true
		b := s.build(name, true)
		if err := b.Available(); err != nil {
			st.Reason, st.Err = err.Error(), err
			closeBackend(b)
			continue
		}
		st.Available = true
		s.clients[name] = b
		if name != DeviceTerm {
			integrations = append(integrations, terminal.Integration{Prefix: Prefix(name), Source: st.Source, Backend: b})
		}
	}

	var primary terminal.Backend = s.pty
	primarySource := PTY
	if name, b := s.bestCandidate(); b != nil {
		primary, primarySource = b, Source(name)
		integrations = append(integrations, terminal.Integration{Prefix: Prefix(PTY), Source: PTY, Backend: s.pty})
	}
	s.composite = terminal.NewCompositeBackend(primary, primarySource, integrations)
	if !opts.NoSelfTTYFilter {
		tty := opts.SelfTTY
		if tty == "" {
			tty = terminal.TTYForPID(os.Getpid())
		}
		if tty != "" {
			s.composite.SetSelfTTY(tty)
		}
	}
	ttl := opts.CacheTTL
	if ttl <= 0 {
		ttl = DefaultCacheTTL
	}
	s.cached = terminal.NewCachedBackend(s.composite, ttl)
	s.reconcile()
	return s, nil
}

// newClient constructs the client for name. atOpen is true only for the
// clients Open builds, the one place AllowITermPrompt applies.
func (s *Stack) newClient(name string, atOpen bool) terminal.Backend {
	o := s.opts
	switch name {
	case ITerm2:
		return iterm.NewClient(s.itermOptions(atOpen))
	case Tmux:
		return tmux.NewClient(tmux.Options{
			Path:            o.TmuxPath,
			LaunchSession:   o.TmuxSession,
			FallbackSession: o.ProgramName,
			CommandTimeout:  o.CommandTimeout,
		})
	case Kitty:
		return kitty.NewClient(kitty.Options{Path: o.KittenPath, CommandTimeout: o.CommandTimeout})
	case WezTerm:
		return wezterm.NewClient(wezterm.Options{Path: o.WezTermPath, CommandTimeout: o.CommandTimeout})
	case DeviceTerm:
		return deviceterm.NewClient(deviceterm.Options{
			Path:           o.DeviceTermPath,
			ProgramName:    o.ProgramName,
			CommandTimeout: o.CommandTimeout,
		})
	}
	panic("libatria: no client for " + name)
}

// itermOptions returns the iTerm2 client options. It may prompt everywhere
// under ITermPromptAnywhere, and otherwise only when Open builds it inside
// iTerm2 with AllowITermPrompt.
func (s *Stack) itermOptions(atOpen bool) iterm.Options {
	o := s.opts
	prompt := o.ITermPromptAnywhere ||
		(atOpen && o.AllowITermPrompt && s.getenv("TERM_PROGRAM") == "iTerm.app")
	return iterm.Options{
		NoPrompt:       !prompt,
		ClientName:     o.ProgramName,
		CommandTimeout: o.CommandTimeout,
		AuthGate:       s.authGate,
	}
}

// bestCandidate returns the highest-ranked available integration whose
// environment matches, or "" and nil. Caller holds mu (or is Open).
func (s *Stack) bestCandidate() (string, terminal.Backend) {
	for _, name := range names {
		if b, ok := s.clients[name]; ok && EnvMatches(name, s.getenv) {
			return name, b
		}
	}
	return "", nil
}

// reconcile recomputes Active and Launch from the probes and the current
// primary. Caller holds mu (or is Open).
func (s *Stack) reconcile() {
	primary := s.composite.PrimarySource()
	for name, st := range s.statuses {
		switch name {
		case PTY:
			st.Active = true
		case DeviceTerm:
			// Never a secondary discoverer: active only when it launches.
			st.Active = st.Available && primary == st.Source
		default:
			st.Active = st.Available && EnvMatches(name, s.getenv)
		}
		st.Launch = st.Active && st.Source == primary
	}
}

// Backend returns the cached composite. Hold this one; it is what a
// watch.Watcher and every per-session call should use.
func (s *Stack) Backend() terminal.Backend { return s.cached }

// Composite returns the composite under the cache, for callers that need
// its mutation surface directly. Enable and Disable cover the common cases.
func (s *Stack) Composite() *terminal.CompositeBackend { return s.composite }

// PTY returns the embedded terminal client, which is always present.
func (s *Stack) PTY() *pty.Client { return s.pty }

// PrimarySource returns the composite source label of the launch target.
func (s *Stack) PrimarySource() string { return s.composite.PrimarySource() }

// Statuses returns every backend's state: pty first, then the integrations
// in Names order, disabled ones included.
func (s *Stack) Statuses() []Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncAuth()
	return s.statusList()
}

// syncAuth marks a live iTerm2 client not available when the credential gate
// is off: a reconnect was refused and declined after its probe passed, so the
// client can't connect until Reauthorize, and Reprobe should try it again.
// The client stays in the composite, so routing and the primary don't change.
// Caller holds mu.
func (s *Stack) syncAuth() {
	st := s.statuses[ITerm2]
	if _, live := s.clients[ITerm2]; !live || !st.Available {
		return
	}
	if err := s.gateErr(); err != nil {
		st.Available = false
		st.Reason, st.Err = err.Error(), err
		s.reconcile()
	}
}

func (s *Stack) statusList() []Status {
	out := make([]Status, 0, len(names)+1)
	out = append(out, *s.statuses[PTY])
	for _, name := range names {
		out = append(out, *s.statuses[name])
	}
	return out
}

// Ignored returns the entries of Options.Integrations that name no known
// integration, in the order given.
func (s *Stack) Ignored() []string {
	return append([]string(nil), s.ignored...)
}

// Configure replaces the options used to build clients for later Enable and
// Reprobe calls: the binary paths, TmuxSession, ProgramName, and
// CommandTimeout. Clients already running are untouched, and the fields
// fixed at Open (Integrations, PTY size, CacheTTL, SelfTTY, Getenv,
// AllowITermPrompt, ITermPromptAnywhere) are ignored. A settings screen that
// edits the tmux launch session calls this before re-enabling tmux.
func (s *Stack) Configure(opts Options) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.opts.TmuxPath = opts.TmuxPath
	s.opts.TmuxSession = opts.TmuxSession
	s.opts.KittenPath = opts.KittenPath
	s.opts.WezTermPath = opts.WezTermPath
	s.opts.DeviceTermPath = opts.DeviceTermPath
	s.opts.ProgramName = opts.ProgramName
	s.opts.CommandTimeout = opts.CommandTimeout
}

// Enable probes name with a fresh client and, when the probe passes, adds
// it to the composite and promotes it to primary if its environment matches
// and it outranks the primary at that moment. The returned Status reports the
// outcome either way (a failed probe leaves Enabled set with Reason and Err
// filled and changes nothing else). An integration that is already running,
// or already being probed, is left alone. The probe runs without the Stack's
// lock, so it may wait on the terminal (and, under ITermPromptAnywhere, on a
// person answering iTerm2's dialog, up to iterm.DefaultAuthTimeout) without
// holding up the other methods; a Disable that lands meanwhile wins, and the
// probed client is closed. Enable doesn't re-arm a declined credential
// prompt; see Reauthorize. The error is non-nil only for an unknown name. A
// successful change invalidates Backend's session cache, so the next listing
// already carries the new roles' IDs.
func (s *Stack) Enable(name string) (Status, *RoleChange, error) {
	s.mu.Lock()
	st, ok := s.statuses[name]
	if !ok || name == PTY {
		s.mu.Unlock()
		return Status{}, nil, fmt.Errorf("libatria: unknown integration %q", name)
	}
	st.Enabled = true
	if _, live := s.clients[name]; live || s.probing[name] {
		out := *st
		s.mu.Unlock()
		return out, nil, nil
	}
	p := s.startProbe(name)
	s.mu.Unlock()

	err := p.b.Available()

	s.mu.Lock()
	defer s.mu.Unlock()
	rc := s.finishProbe(p, err)
	return *s.statuses[name], rc, nil
}

// probe is one integration probe: picked and built under mu, run without it,
// and wired in under mu again by finishProbe.
type probe struct {
	name string
	gen  uint64
	b    terminal.Backend
	live bool // b is already wired in; only its status is updated
}

// startProbe marks name as probing and builds its client, or reuses the
// client already in the composite when syncAuth marked it unavailable. Caller
// holds mu.
func (s *Stack) startProbe(name string) probe {
	s.probing[name] = true
	if b, live := s.clients[name]; live {
		return probe{name: name, gen: s.gen[name], b: b, live: true}
	}
	return probe{name: name, gen: s.gen[name], b: s.build(name, false)}
}

// finishProbe wires in the result of p's Available call, err, against the
// state as it is now rather than when the probe started: a probe that a
// Disable overtook is dropped, and promotion compares against the current
// primary. A client that isn't kept is closed. Caller holds mu.
func (s *Stack) finishProbe(p probe, err error) *RoleChange {
	if s.gen[p.name] != p.gen {
		// A Disable overtook the probe. Close the client even when it was
		// live: the probe may have reconnected it after Disable's close.
		go closeBackend(p.b)
		return nil
	}
	delete(s.probing, p.name)
	st := s.statuses[p.name]
	if p.live {
		st.Available = err == nil
		st.Err = err
		st.Reason = ""
		if err != nil {
			st.Reason = err.Error()
		}
		s.reconcile()
		return nil
	}
	if err != nil {
		st.Reason, st.Err = err.Error(), err
		st.Available = false
		s.reconcile()
		closeBackend(p.b)
		return nil
	}
	st.Reason, st.Err = "", nil
	st.Available = true
	s.clients[p.name] = p.b
	if p.name != DeviceTerm {
		s.composite.AddIntegration(terminal.Integration{Prefix: Prefix(p.name), Source: st.Source, Backend: p.b})
	}
	var rc *RoleChange
	current := s.composite.PrimarySource()
	if EnvMatches(p.name, s.getenv) && Rank(st.Source) > Rank(current) {
		rc = s.demote(current)
		s.composite.SetPrimary(p.b, st.Source)
		rc.NewPrimary = st.Source
	}
	s.reconcile()
	// Routing changed under the cache; a stale listing would hand out IDs
	// the composite now resolves to a different backend.
	s.cached.Invalidate()
	return rc
}

// closeBackend closes b when it implements io.Closer and discards the close
// error.
func closeBackend(b terminal.Backend) {
	if c, ok := b.(io.Closer); ok {
		_ = c.Close()
	}
}

// Disable removes name from the composite and, when it was the primary,
// re-derives the launch target from what remains by rank and environment,
// falling back to PTY. The removed client is closed in the background (when
// it has a Close), because closing an iTerm2 client waits for any credential
// dialog it has open, and Disable must not. A probe of name still in flight
// is abandoned and its client closed when it returns. The error is non-nil
// only for an unknown name. The session cache is invalidated so the removed
// backend's sessions and any role change show on the next listing.
func (s *Stack) Disable(name string) (*RoleChange, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.statuses[name]
	if !ok || name == PTY {
		return nil, fmt.Errorf("libatria: unknown integration %q", name)
	}
	st.Enabled = false
	st.Available = false
	st.Reason, st.Err = "", nil
	s.gen[name]++
	delete(s.probing, name)
	removed, live := s.clients[name]
	delete(s.clients, name)
	// Detach rather than RemoveIntegration, which closes under the
	// composite's lock and would stall every listing behind an open dialog.
	s.composite.DetachIntegration(Prefix(name))

	var rc *RoleChange
	if s.composite.PrimarySource() == st.Source {
		var b terminal.Backend = s.pty
		source := PTY
		if n, cb := s.bestCandidate(); cb != nil {
			b, source = cb, Source(n)
		}
		s.composite.SetPrimary(b, source)
		rc = s.promote(source)
		rc.NewPrimary = source
	}
	s.reconcile()
	s.cached.Invalidate()
	if live {
		go closeBackend(removed)
	}
	return rc, nil
}

// Reprobe retries every enabled integration whose last probe failed and
// isn't being probed already, and wires in the ones that now answer exactly
// as Enable would, in precedence order, so at most one promotion happens.
// Like Enable it probes without the Stack's lock. A declined iTerm2
// credential prompt stays declined here (the retry fails with
// iterm.ErrAuthFailed and shows no dialog) until Reauthorize. A live iTerm2
// client whose reconnect was declined is retried in place, without leaving
// the composite. It returns the resulting statuses and the RoleChange when the
// primary changed.
func (s *Stack) Reprobe() ([]Status, *RoleChange) {
	s.mu.Lock()
	s.syncAuth()
	var probes []probe
	for _, name := range names {
		st := s.statuses[name]
		if !st.Enabled || st.Available || s.probing[name] {
			continue
		}
		probes = append(probes, s.startProbe(name))
	}
	s.mu.Unlock()

	var rc *RoleChange
	for _, p := range probes {
		// A probe queued behind a slow one may have been disabled meanwhile;
		// running it would reopen a closed client or show a dialog for a
		// terminal nobody wants.
		s.mu.Lock()
		stale := s.gen[p.name] != p.gen
		s.mu.Unlock()
		if stale {
			if !p.live {
				closeBackend(p.b)
			}
			continue
		}
		err := p.b.Available()
		s.mu.Lock()
		if r := s.finishProbe(p, err); r != nil {
			rc = r
		}
		s.mu.Unlock()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncAuth()
	return s.statusList(), rc
}

// Reauthorize re-arms the iTerm2 credential prompt after a request was
// declined or timed out, which otherwise stays off for the life of the Stack
// so that nothing asks again on its own. Only "iterm2" has a prompt; any other
// name is an error. It doesn't probe: call Reprobe afterwards to retry.
// Enable asks again only when no iTerm2 client is installed. It has no effect on clients built with prompting off (see
// AllowITermPrompt and ITermPromptAnywhere).
func (s *Stack) Reauthorize(name string) error {
	if name != ITerm2 {
		return fmt.Errorf("libatria: %q has no credential prompt to re-arm", name)
	}
	s.authGate.Rearm()
	return nil
}

// demote prepares the current primary for the integration role and returns
// the resulting id change (NewPrimary unset). PTY gains the entry it lacks;
// every other backend keeps the entry it already has and the composite
// dedups by TTY. A primary with no entry cannot be routed afterwards, so its
// Source is left empty and its sessions drop on the next refresh. Caller
// holds mu.
func (s *Stack) demote(current string) *RoleChange {
	if current == PTY {
		s.composite.AddIntegration(terminal.Integration{Prefix: Prefix(PTY), Source: PTY, Backend: s.pty})
	}
	prefix, ok := s.integrationPrefix(current)
	if !ok {
		return &RoleChange{}
	}
	return &RoleChange{Source: current, Prefix: prefix, ToPrefixed: true}
}

// promote is the reverse: source has just become primary, so its sessions
// lose their prefix. PTY's entry is detached rather than removed, because
// removing would close the client and kill every embedded session; other
// backends keep their entry. Caller holds mu.
func (s *Stack) promote(source string) *RoleChange {
	prefix, ok := s.integrationPrefix(source)
	if !ok {
		return &RoleChange{}
	}
	if source == PTY {
		s.composite.DetachIntegration(prefix)
	}
	return &RoleChange{Source: source, Prefix: prefix, ToPrefixed: false}
}

// integrationPrefix returns the prefix serving source in the integration
// role. PTY has no standing entry and is answered directly.
func (s *Stack) integrationPrefix(source string) (string, bool) {
	if source == PTY {
		return Prefix(PTY), true
	}
	for _, integ := range s.composite.Integrations() {
		if integ.Source == source {
			return integ.Prefix, true
		}
	}
	return "", false
}

// Resize changes the embedded terminal size for PTY sessions created from
// now on and resizes the live ones.
func (s *Stack) Resize(cols, rows int) { s.pty.Resize(cols, rows) }

// Close closes the cached composite, which closes the primary and every
// integration that has a Close, PTY sessions included.
func (s *Stack) Close() error { return s.cached.Close() }
