# libatria API

Status: proposal. Nothing under `libatria/` exists yet; this document is what the extraction will produce, and it's the thing to review before any code moves.

libatria is the importable part of atria: terminal discovery, agent detection, status classification, and a watcher that follows agent sessions over time. It lives in the atria module at `github.com/sethdeckard/atria/libatria` and shares atria's version. It's built for programs like atria itself and for daemons that drive Claude and Codex sessions remotely.

## Packages

| Import path | Package | Role |
|---|---|---|
| `github.com/sethdeckard/atria/libatria` | `libatria` | Build a backend stack from integration names; enable, disable, and reprobe at runtime |
| `github.com/sethdeckard/atria/libatria/terminal` | `terminal` | The `Backend` interface, `Session`, composite and cached backends, process and cwd lookup, named keys |
| `github.com/sethdeckard/atria/libatria/terminal/{iterm,tmux,kitty,wezterm,deviceterm,pty}` | one per terminal | A `Client` per terminal application |
| `github.com/sethdeckard/atria/libatria/agent` | `agent` | Agent vocabulary (`Type`, `Status`), detection from titles and screens, classification, launch and send helpers |
| `github.com/sethdeckard/atria/libatria/watch` | `watch` | Per-session state machine (`Tracker`), discovery policy (`Identify`), goroutine poller (`Watcher`) |

Dependencies point one way. `terminal` imports nothing else in the library. `agent` imports `terminal`. `terminal/tmux` imports `agent` for title detection, which is fine because `terminal` itself never imports `agent`. `watch` imports both, and the root package imports everything. Nothing under `libatria/` imports `internal/tui`, `internal/config`, bubbletea, or lipgloss.

If you only want terminals and don't care about agents, import `terminal` and the clients you need. The `pty` client pulls in creack/pty and vt10x; `iterm` pulls in gorilla/websocket and protobuf. The other four clients are stdlib plus `charmbracelet/x/ansi`.

## Quick Start

Open a stack, list sessions, read a screen, send input, then watch:

```go
stack, err := libatria.Open(libatria.Options{
    Integrations: []string{libatria.Tmux, libatria.WezTerm},
    ProgramName:  "agent-dashboard",
})
if err != nil {
    return err
}
defer stack.Close()

b := stack.Backend()
sessions, err := b.ListSessions()
if err != nil {
    return err
}

var target terminal.Session
var targetType agent.Type
for _, s := range sessions {
    t := agent.Detect(s.Name)
    if t == "" {
        continue
    }
    screen, _ := b.ReadScreen(s.ID, 40)
    status, line := agent.ClassifyScreen(screen, t)
    fmt.Println(s.ID, t, status, line)
    if target.ID == "" {
        target, targetType = s, t
    }
}
if target.ID == "" {
    return errors.New("no agent sessions found")
}

_ = agent.SendPrompt(b, target.ID, "run the tests", targetType)
_ = terminal.SendKey(b, target.ID, terminal.KeyEscape)

w := watch.New(b, watch.Options{WatchDirs: []string{"/Users/me/projects"}})
go w.Run(ctx)
for ev := range w.Events() {
    if ev.Kind == watch.StatusChanged && ev.To == agent.StatusNeedsInput {
        notify(ev.Session.ID, ev.MatchLine)
    }
}
```

## Package terminal

### Core

These move from `internal/terminal`; only `NewCachedBackend` changes, taking its TTL as a `time.Duration`:

```go
type Session struct {
    ID     string // backend-specific; a composite prefixes integration sessions ("tmux:%3", "pty:pty-1") and leaves the primary's unprefixed
    Name   string // live title
    TTY    string // controlling terminal device, "" when unknown
    Job    string // foreground job name when the backend reports one (iTerm2)
    Source string // "pty", "iterm", "tmux", "kitty", "wezterm", "deviceterm"; set by the composite
}

type Backend interface {
    Available() error
    ListSessions() ([]Session, error)
    NewSession() (string, error)
    SendText(sessionID, text string) error
    RunCommand(sessionID, cmd string) error
    FocusSession(sessionID string) error
    ReadScreen(sessionID string, lines int) (string, error)
    GetVar(sessionID, varName string) (string, error)
    MonitorOutput(sessionID, logPath, patterns string) (int, error)
}

type StyledReader interface {
    ReadScreenStyled(sessionID string, lines int) (string, error)
}

type Integration struct {
    Prefix  string
    Source  string
    Backend Backend
}

func NewCompositeBackend(primary Backend, primarySource string, integrations []Integration) *CompositeBackend
func NewCachedBackend(inner Backend, ttl time.Duration) *CachedBackend

func TTYForPID(pid int) string
func DiscoverCWD(b Backend, sess Session, watchDirs, projectDirs []string) string
func TrimScreenTail(text string, n int) string
func ReadLastLine(path string) string
func ReadTail(path string, n int) string
func HasBell(text string) bool
```

`GetVar` supports `"path"` everywhere and `"pid"` on pty, tmux, and kitty. `MonitorOutput` returns an error from every backend today; it stays on the interface so a consumer that wants log-based monitoring has a place to implement it. `NewCachedBackend` takes a `time.Duration` instead of the current int seconds.

A composite prefixes every integration session's ID with its source and a colon, and leaves the primary's sessions unprefixed. An ID is stable for the life of the session unless the backend changes role (primary to integration or back). `libatria.Stack` reports that as a `RoleChange` so you can rewrite the IDs you hold.

### Optional Interfaces

Optional behaviour is discovered by anonymous type assertions today. These become named types you can assert against:

```go
type Resizer interface{ Resize(cols, rows int) }
type SourceLauncher interface{ NewSessionOn(source string) (string, error) }
type Invalidator interface{ Invalidate() }
type FailureReporter interface{ FailedSources() []string }
type PrimaryReporter interface{ PrimarySource() string }
type BellSource interface{ ConsumeBell(sessionID string) bool }
// plus io.Closer
```

`CompositeBackend` implements all of them. `CachedBackend` forwards all of them. `pty.Client` is a `Resizer`, a `BellSource`, and an `io.Closer`; `iterm.Client` is an `io.Closer`. `Close` currently returns nothing; it becomes `Close() error` so the clients satisfy `io.Closer`.

`FailedSources` names the integrations whose `ListSessions` failed on the most recent composite call. Use it to keep sessions from a source that is temporarily unreachable instead of dropping them.

`ConsumeBell` reports whether the session rang its bell since the last time anyone asked, and clears the flag. Only the PTY client has bell state; the other terminals don't include BEL in captured text. The PTY's plain `ReadScreen` already consumes the flag and prepends `"\x07"` to its output, which is how the classifier sees a bell today. Its styled read doesn't touch the flag, because a BEL in display output would ring the viewer's own terminal. `ConsumeBell` exists so a caller that reads only the styled screen can still get the bell; see `StyledScreens` in the `watch` package. The composite and cache route it and return false when the owning backend has no bell state.

### Named Keys

```go
type Key string

const (
    KeyEnter     Key = "enter"
    KeyEscape    Key = "escape"
    KeyTab       Key = "tab"
    KeyBackTab   Key = "backtab"
    KeyBackspace Key = "backspace"
    KeyUp        Key = "up"
    KeyDown      Key = "down"
    KeyLeft      Key = "left"
    KeyRight     Key = "right"
    KeyCtrlC     Key = "ctrl-c"
    KeyCtrlD     Key = "ctrl-d"
    KeySpace     Key = "space"
)

func ParseKey(s string) (Key, error)
func (k Key) Sequence() string

type KeySender interface{ SendKey(sessionID string, key Key) error }

func SendKey(b Backend, sessionID string, key Key) error
```

`ParseKey` accepts a name from the list or exactly one printable rune, so `"1"` is a valid key for answering a numbered prompt. `Sequence` returns the bytes a terminal would send for that key (`"\r"`, `"\x1b"`, `"\x1b[A"`, `"\x03"`, and so on; a rune returns itself).

`SendKey` uses the backend's `KeySender` when it has one and otherwise sends `Sequence()` through `SendText`. Every backend except tmux delivers `SendText` bytes verbatim (a PTY write, iTerm2's `SendTextRequest`, `wezterm cli send-text --no-paste` over stdin, `deviceterm pane send-input --raw`, `kitten @ send-text`), so the raw sequence is enough there. tmux sends text with `send-keys -l`, which isn't reliable for control bytes, so `tmux.Client` implements `KeySender` and maps names to tmux key names (`Enter`, `Escape`, `Tab`, `BTab`, `BSpace`, `Up`, `Down`, `Left`, `Right`, `C-c`, `C-d`). Printable runes still go through `-l`. Composite and cached backends implement `KeySender` by routing to the owner.

### Process Identity

```go
type Process struct {
    PID  int
    PPID int
    Argv []string
    Cwd  string
}

func ProcessesOnTTY(tty string) ([]Process, error)
func ProcessCWD(pid int) (string, error)
```

`ProcessesOnTTY` runs `ps -t <tty>` and fills `Cwd` the way `DiscoverCWD`'s TTY strategy does today (`lsof -a -d cwd`), adding `/proc/<pid>/cwd` on Linux, which the PTY client already uses for its own child. `Cwd` is best-effort and may be empty. `terminal` doesn't know which of those processes is the agent; `agent.FindProcess` does that. `DiscoverCWD` is rebuilt on these two functions with no change in result.

### Terminal Loss

```go
var ErrUnavailable = errors.New("terminal unavailable")
```

Every client wraps connection-level failure so `errors.Is(err, terminal.ErrUnavailable)` holds: the iTerm2 socket failing to dial or closing, tmux failing to reach its server (permission denied, connection refused, a timeout), kitty and WezTerm socket errors, DeviceTerm `transport.*` errors, and any command timeout. When no tmux server is running, `ListSessions` returns an empty list (a server that exited took every session with it, and treating that as an outage would leave phantom rows until it came back); per-session operations against it return `ErrUnavailable`. A composite `ListSessions` reports an integration failure through `FailedSources` and returns the error only when the primary fails.

Recovery doesn't need a restart. The iTerm2 client already reconnects on the next call after a drop, and the CLI clients spawn a fresh process per call. `libatria.Stack.Reprobe` covers integrations that failed their initial probe.

### Bounded Calls

The CLI clients (tmux, kitty, wezterm, deviceterm) run each subprocess under a per-call timeout, `CommandTimeout` in their options, default 5 seconds. The iTerm2 round trip gets a read deadline of the same length. A timeout is reported as `ErrUnavailable` and also satisfies `errors.Is(err, context.DeadlineExceeded)`.

`Backend` methods don't take a `context.Context` in v0.x. That's the one change already planned for v1; see Stability.

### Concurrency

Every client, `CompositeBackend`, and `CachedBackend` is safe for concurrent use. Concurrent `ReadScreen` calls on different sessions can run in parallel; the iTerm2 connection serializes its round trips internally, so parallel iTerm2 reads queue rather than fail.

### Client Constructors

```go
iterm.NewClient(iterm.Options{
    SocketPath     string        // "" → ~/Library/Application Support/iTerm2/private/socket
    NoPrompt       bool          // never trigger the AppleScript auth dialog
    ClientName     string        // "" → "libatria"; the app name iTerm2 shows and the advisory header
    CommandTimeout time.Duration
}) *iterm.Client

tmux.NewClient(tmux.Options{
    Path            string // "" → "tmux" on $PATH
    LaunchSession   string // "" → the current session when inside tmux, else FallbackSession
    FallbackSession string // "" → "libatria"; the detached session created when not inside tmux
    CommandTimeout  time.Duration
}) *tmux.Client

kitty.NewClient(kitty.Options{Path string; CommandTimeout time.Duration}) *kitty.Client
wezterm.NewClient(wezterm.Options{Path string; CommandTimeout time.Duration}) *wezterm.Client

deviceterm.NewClient(deviceterm.Options{
    Path           string // "" → "deviceterm" on $PATH, then $DEVICETERM_SHIM_DIR
    ProgramName    string // "" → "this program"; appears in the Automation-tab reason text
    CommandTimeout time.Duration
}) *deviceterm.Client

pty.NewClient(cols, rows int) *pty.Client // unchanged; DefaultCols 120, DefaultRows 40
```

The iTerm2 constructor changes shape: today it is `NewClient(socketPath ...string)` plus a `SetNoPrompt` setter. tmux changes from two positional strings to an options struct. The names atria hardcodes into these clients (`"atria"` in the AppleScript request, the advisory header, the DeviceTerm reason text, and the tmux fallback session) become options, and atria passes `"atria"` so its wire traffic and UI text don't change.

`deviceterm.CLIError{Code, Message}` stays exported. Branch on `Code`, never on `Message`.

## Package agent

```go
type Type string

const (
    Claude   Type = "claude"
    Codex    Type = "codex"
    OpenCode Type = "opencode"
    Copilot  Type = "copilot"
)

type Status string

const (
    StatusWorking    Status = "working"
    StatusIdle       Status = "idle"
    StatusNeedsInput Status = "needs_input"
    StatusError      Status = "error"
)

func Types() []Type
func Installed() []Type

func Detect(name string) Type
func ExtractActivity(name string) string
func ClassifyOutput(line string, t Type) Status
func ClassifyScreen(screen string, t Type) (Status, string)
func HasScreen(screen string, t Type) bool
func InferFromScreen(screen string) Type

type Patterns struct {
    NeedsInput, Working, WorkingExclude, Idle []*regexp.Regexp
}
func PatternsFor(t Type) *Patterns

func ShellQuote(s string) string
func LaunchCommand(dir string, t Type, args ...string) string
func Launch(b terminal.Backend, source, dir, cmd string) (string, error)
func SendPrompt(b terminal.Backend, sessionID, text string, t Type) error
func FindProcess(procs []terminal.Process) (terminal.Process, Type, bool)

const SubmitDelay = 50 * time.Millisecond
const CopilotRuneDelay = 5 * time.Millisecond
const LaunchSettle = 300 * time.Millisecond
```

A `Type`'s string value is the agent's CLI binary name, and `Installed` reports which of them `exec.LookPath` can find.

`Detect` reads a session title and returns `""` when it isn't an agent. `ClassifyScreen` returns the highest-priority status found and the line that matched; `""` means nothing matched, which the `watch` package treats as a signal in its own right. `HasScreen` reports whether the agent's UI is present in the bottom region of a screen. `InferFromScreen` identifies the agent from product text first (when several products match, which one wins is unspecified) and otherwise from agent-specific patterns, returning `""` when the patterns leave more than one agent plausible. The classification rules move from `internal/terminal/monitor.go` and `patterns.go` without behaviour change, including the per-agent pattern registry, bottom-region anchoring, and Claude's todo-footer anchor.

`PatternsFor` returns the registry's own entry; don't modify the struct, its slices, or the regular expressions. There's no way to register a new agent type in this version.

`LaunchCommand("/p", agent.Claude, "--resume", "abc")` returns `cd '/p' && claude '--resume' 'abc'`. `Launch` creates a session on `source` (through `SourceLauncher` when the backend has one and `source` is non-empty, else `NewSession`), focuses it, waits `LaunchSettle`, runs `cd '<dir>' && <cmd>`, and returns the new session ID. It runs `cmd` as given, so quote your own arguments or build the string with `LaunchCommand`. The focus step matters: iTerm2 doesn't populate a background tab's buffer until it has been shown once.

`SendPrompt` sends the text, waits `SubmitDelay`, then sends a carriage return as a separate write, because the raw-mode TUIs these agents run drop a trailing newline that arrives in the same write. Copilot gets its own path: newlines are flattened to spaces (its input treats Enter as newline, not submit), and the text goes one rune per write with `CopilotRuneDelay` between them.

`FindProcess` returns the first process whose `argv[0]` basename is a known agent binary.

## Package watch

### Tracker

One `Tracker` per session. Its fields are exported so you can render them and tests can seed them; change them only through `Observe` and `Refresh`.

```go
const StableUnmatchedReads = 3
const BlankReads = 2
const OrphanThreshold = 2

type Tracker struct {
    Type   agent.Type
    Source string

    Status       agent.Status
    Activity     string
    Attention    string
    LastActivity time.Time

    ScreenChecked  bool
    LastScreen     string
    LastRead       time.Time
    UnmatchedReads int
    OrphanTicks    int
}

type Reason int // ReasonNone, ReasonMatched, ReasonMovedOn, ReasonStableUnmatched, ReasonBlank

type Transition struct {
    From, To  agent.Status
    Applied   bool
    Changed   bool
    Matched   agent.Status
    MatchLine string
    Reason    Reason
}

func (t Transition) StatusChanged() bool
func (t Transition) EnteredNeedsInput() bool
func (t Transition) LeftNeedsInput() bool

type RefreshResult struct {
    ActivityChanged bool
    Retyped         bool
    PrevType        agent.Type
    Orphan          bool
}

func (t *Tracker) Observe(screen string, now time.Time) Transition
func (t *Tracker) Refresh(sess terminal.Session, now time.Time) RefreshResult
func PollInterval(s agent.Status, active, idle time.Duration) time.Duration
```

`Observe` takes one screen read and applies the rules atria applies today:

| Matched | Screen changed | Current status | Result |
|---|---|---|---|
| S | yes | any | apply S |
| S | no | S | no-op (stale read); counter reset |
| S | no | not S | apply S |
| none | yes | needs_input | apply working; the agent moved on |
| none | yes | other | no-op; counter reset |
| none | no | working | counter++; apply idle at 3, or at 2 if the screen is all blank |
| none | no | other | counter++; no-op |

When a status is applied, `LastActivity` is stamped, `Attention` becomes `MatchLine` on entering or staying in needs_input, and `Attention` clears on leaving it. `Matched` and `MatchLine` always carry the raw `ClassifyScreen` result, applied or not, so a debug log can show what the classifier saw.

The counter only advances on identical reads. A screen that keeps changing without matching any pattern is an agent producing output the patterns don't know, and it stays working.

`Refresh` takes the session as the backend listed it on a discovery tick. It updates `Activity` from the title (stamping `LastActivity` only when the new activity is non-empty), retypes the tracker when the title names a different agent, and takes `Source` from the session. Then it counts an orphan tick when the session is idle, has been screen-checked at least once, and either the title no longer names an agent and the last screen shows no agent UI, or (iTerm2 only) the foreground job is a shell. `Orphan` is true once `OrphanTicks` reaches `OrphanThreshold`; drop the session when you see it.

`PollInterval` returns `active` for working, needs_input, and error, and `idle` otherwise.

### Identify

```go
type SkipReason int // SkipNone, SkipUnknownTitleNoDir, SkipScreenReadFailed, SkipUnknownTitleAndScreen, SkipOutsideWatchDirs
func (r SkipReason) String() string

type IdentifyOptions struct {
    WatchDirs   []string
    ProjectDirs []string
    ScreenLines int // 0 → 40
}

type Identity struct {
    Type    agent.Type
    Dir     string
    Process terminal.Process
    Gated   bool // WatchDirs were given, so Dir had to validate against them
    Skip    SkipReason
    Err     error
}

func (id Identity) OK() bool // Type != "" && (Dir != "" || !Gated)
func Identify(b terminal.Backend, sess terminal.Session, opts IdentifyOptions) Identity
func ResolveProcess(b terminal.Backend, sess terminal.Session) (terminal.Process, agent.Type)
```

`Identify` runs in one of two modes, decided by whether `WatchDirs` is empty.

With watch directories (atria's mode), it resolves the directory with `DiscoverCWD` and then checks the result against `WatchDirs` itself, skipping with `SkipOutsideWatchDirs` when the path isn't under one of them. That second check matters because `DiscoverCWD`'s last strategy matches project basenames in the title against `ProjectDirs`, and a `ProjectDirs` entry needn't be under any watch directory; without the check, the gate would leak. Then it reads the agent from the title. When the title is unknown and a directory was found, it reads the screen and calls `InferFromScreen`; a read failure sets `SkipScreenReadFailed` and `Err`. An unknown title with no directory is `SkipUnknownTitleNoDir` and costs no screen read. A known title with no directory is not a skip; `OK()` is false and the caller decides.

With no watch directories (a daemon's mode), nothing is gated. `Dir` comes from the agent process's cwd when `ResolveProcess` finds one, else from `GetVar("path")`, and it may be empty. An unknown title always gets a screen read. `OK()` needs only `Type`, so any agent session the backend lists qualifies. `DiscoverCWD` itself is unchanged: with an empty watch list its two path-validating strategies match nothing, and only the title-to-`ProjectDirs` name match can still return a directory. The ungated behaviour is `Identify`'s alone, so atria's discovery doesn't change.

`ResolveProcess` finds the agent process for a session: `ProcessesOnTTY` then `FindProcess`, or the PTY child pid and its descendants when the backend owns the process. It returns the zero `Process` and `""` when nothing matches. `Identify` calls it, and it's exported so a consumer that hears about a session from a hook can refresh the process on demand. The lookup is best-effort and never changes the skip decision.

### Watcher

```go
type Clock interface {
    Now() time.Time
    After(d time.Duration) <-chan time.Time
}

type Options struct {
    DiscoveryInterval time.Duration // 0 → 3s
    ActiveInterval    time.Duration // 0 → 1s
    IdleInterval      time.Duration // 0 → 3s
    ScreenLines       int           // 0 → 40
    WatchDirs         []string
    ProjectDirs       []string
    Filter            func(terminal.Session) bool
    Agents            []agent.Type  // nil → all
    Parallelism       int           // 0 → 4
    EventBuffer       int           // 0 → 256
    EmitScreenReads   bool
    StyledScreens     bool
    Clock             Clock         // nil → wall clock
    Logf              func(format string, args ...any)
}

type EventKind int    // SessionAdded, SessionRemoved, StatusChanged, ActivityChanged, ScreenRead, Error
type RemoveReason int // RemovedGone, RemovedOrphan

type Snapshot struct {
    terminal.Session
    Dir          string
    Type         agent.Type
    Process      terminal.Process
    Status       agent.Status
    Activity     string
    Attention    string
    LastActivity time.Time
    LastRead     time.Time
    Screen       string
}

type Event struct {
    Kind      EventKind
    Time      time.Time
    Session   Snapshot
    From, To  agent.Status // StatusChanged
    MatchLine string       // StatusChanged
    Reason    RemoveReason // SessionRemoved
    Screen    string       // ScreenRead
    Err       error        // Error
}

func New(b terminal.Backend, opts Options) *Watcher
func (w *Watcher) Run(ctx context.Context) error
func (w *Watcher) Events() <-chan Event
func (w *Watcher) Sessions() []Snapshot
func (w *Watcher) Session(id string) (Snapshot, bool)
```

`Run` blocks until the context is cancelled and closes `Events` on return. Calling it twice returns `ErrAlreadyRunning`. On each discovery tick it lists sessions, refreshes tracked ones, identifies new ones, and removes sessions that are gone or orphaned. On each poll tick it reads the screen of every session whose interval has elapsed, `ActiveInterval` for working, needs_input, and error, `IdleInterval` otherwise. New sessions start as working, matching what atria does.

`WatchDirs` is passed to `Identify` and decides its mode: with directories, only sessions under them are added; with none, directory filtering is off and every agent session the backend lists is a candidate. `Filter` runs on every listed session before anything else; `Agents` limits which types are added.

Events for one session arrive in causal order, with `SessionAdded` first and `SessionRemoved` last. Sends block when the buffer is full. Nothing is dropped while `Run` is live, and a slow consumer pauses polling until it catches up. Backpressure was chosen over dropping because a lost needs_input event leaves an agent waiting until someone notices. Every send also selects on the context, so cancelling it returns from `Run` even with a full buffer and a consumer that has stopped reading. Events pending at that point are abandoned. Events are sent outside the Watcher's lock, so `Sessions()` and `Session()` never wait on a blocked send.

`ScreenRead` events are off by default because they fire on every poll. `Parallelism` bounds concurrent `ReadScreen` and `Identify` calls; every non-PTY backend spends a subprocess or a socket round trip per read, so unbounded fan-out across many sessions is process pressure you can feel.

`StyledScreens` is for a consumer that displays the screen as well as classifying it. When set and the backend is a `StyledReader`, the Watcher reads once through `ReadScreenStyled`, classifies on the ANSI-stripped text, and carries the styled text in `ScreenRead` events and `Snapshot.Screen`. Without it, or when the backend can't do styled reads, both are plain text. `Tracker.LastScreen` is always plain.

Bell delivery is the same on both paths. The plain path gets the bell inside the text, because the PTY's `ReadScreen` prepends `"\x07"` when one is pending. On the styled path the Watcher asks the backend's `BellSource` after each read and prepends the same byte to the stripped text before classifying, so a needs_input signal from an embedded PTY session is not lost by turning colors on. A backend without bell state answers false and nothing changes.

`Snapshot.Process` is resolved when the session is added and again whenever it might have gone stale: when `Refresh` reports the session was retyped (the user quit one agent and started another in the same pane) and when the recorded PID no longer exists. The stale value is cleared first, so a `Process` with a non-zero `PID` is one the Watcher has seen alive. Call `ResolveProcess` yourself when you can't wait for the next tick.

Terminal loss: a failed `ListSessions` emits `Error` and leaves every tracked session as it was. A single failed source (reported through `FailureReporter`) keeps its sessions while the rest proceed. A failed `ReadScreen` emits `Error` for that session and changes nothing. Check `errors.Is(ev.Err, terminal.ErrUnavailable)` to tell a lost terminal from an ordinary read error. When the terminal comes back, retained sessions resume on the next successful tick, and sessions that really ended are removed as `RemovedGone`.

The Watcher tolerates the backend changing under it. `Stack.Enable`, `Disable`, and `Reprobe` can run while it polls; sessions whose IDs change role are removed and rediscovered under the new ID.

## Package libatria

```go
const (
    ITerm2     = "iterm2"
    Tmux       = "tmux"
    Kitty      = "kitty"
    WezTerm    = "wezterm"
    DeviceTerm = "deviceterm"
    PTY        = "pty"
)

func Names() []string
func Source(name string) string
func Prefix(name string) string
func Rank(source string) int

type Options struct {
    Integrations []string

    TmuxPath       string
    TmuxSession    string
    KittenPath     string
    WezTermPath    string
    DeviceTermPath string
    PTYCols        int
    PTYRows        int

    CacheTTL        time.Duration // 0 → 5s
    CommandTimeout  time.Duration // 0 → 5s
    SelfTTY         string        // "" → TTYForPID(os.Getpid())
    NoSelfTTYFilter bool
    ProgramName     string        // "" → "libatria"
    AllowITermPrompt bool
    Getenv          func(string) string // nil → os.Getenv
}

type Status struct {
    Name      string
    Source    string
    Enabled   bool
    Available bool
    Active    bool
    Launch    bool
    Reason    string
}

type RoleChange struct {
    Source     string
    Prefix     string
    ToPrefixed bool
    NewPrimary string
}

func Open(opts Options) (*Stack, error)
func (s *Stack) Backend() terminal.Backend
func (s *Stack) Composite() *terminal.CompositeBackend
func (s *Stack) PTY() *pty.Client
func (s *Stack) PrimarySource() string
func (s *Stack) Statuses() []Status
func (s *Stack) Ignored() []string
func (s *Stack) Enable(name string) (Status, *RoleChange, error)
func (s *Stack) Disable(name string) (*RoleChange, error)
func (s *Stack) Reprobe() ([]Status, *RoleChange)
func (s *Stack) Resize(cols, rows int)
func (s *Stack) Close() error
```

`Names` lists the integration names in precedence order, highest first: deviceterm, tmux, kitty, wezterm, iterm2. `Source` maps a name to the composite source label (`"iterm2"` becomes `"iterm"`; the rest are unchanged), `Prefix` appends the colon, and `Rank` orders sources for primary selection with PTY at zero.

`Open` always builds a PTY client, probes each named integration with `Available`, and records a `Status` for every known name whether or not you asked for it. The primary is the highest-ranked available integration whose environment matches: `DEVICETERM_SESSION` for DeviceTerm, `TMUX` for tmux, `KITTY_WINDOW_ID` for kitty, `TERM_PROGRAM=WezTerm` or `WEZTERM_UNIX_SOCKET` for WezTerm, `TERM_PROGRAM=iTerm.app` for iTerm2. With no match, PTY is primary. When PTY isn't primary it's added as the `pty:` integration so its sessions stay listed.

DeviceTerm is the exception to the discovery model. It's either the primary or absent, never a discovery integration, because a granted Automation tab is never inside another terminal and outside one the CLI can't capture, send, or focus. `Status.Active` for DeviceTerm is true only when it's the primary.

`Backend()` returns the cached composite and is what you should hold. `Composite()` and `PTY()` exist for callers that need the mutation surface or the embedded terminal.

`Statuses` lists PTY first, then the integrations in `Names` order. `Available` means the probe passed, `Active` means the environment matched too, `Launch` marks the primary, and `Reason` carries the probe error when there is one. `Ignored` returns names in `Options.Integrations` that the library doesn't recognize; it doesn't fail `Open`.

`Enable` probes and adds an integration at runtime, promoting it to primary when it outranks the current one and its environment matches. `Disable` removes one and re-derives the primary from what remains. Both return a `RoleChange` when a backend moved between the primary and integration roles, because its session IDs gain or lose their prefix and anything you hold by ID has to follow. A `nil` `RoleChange` means no IDs moved. `Enable` never triggers the iTerm2 AppleScript dialog; `AllowITermPrompt` applies to `Open` only.

`Reprobe` retries every enabled integration whose last probe failed and adds or promotes the ones that now answer, following the same precedence as `Open`.

`SelfTTY` is the calling process's terminal, and sessions on it are filtered out of listings so a dashboard doesn't discover its own pane. Set `NoSelfTTYFilter` for a daemon that has no terminal of its own or wants to see everything.

All `Stack` methods are safe for concurrent use, including while a `watch.Watcher` polls `stack.Backend()`.

## Stability

libatria ships inside the atria module and takes atria's version. While the module is v0.x, a minor release may change the libatria API, and every breaking change is listed in CHANGELOG.md under that release.

`Backend`, `Session`, `agent.Type`, `agent.Status`, and `watch.Event` are the surfaces most likely to hold still. The `Options` structs will grow fields. One change is already planned for v1: `Backend` methods will take a `context.Context`, and until then calls are bounded by `CommandTimeout`. The concurrency guarantees above are part of the contract from v0.7.0. A `v1.0.0` tag freezes the API.
