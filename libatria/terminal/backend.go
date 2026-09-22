package terminal

// Session represents a terminal multiplexer session.
type Session struct {
	ID     string
	Name   string
	TTY    string
	Job    string
	Source string // "pty", "iterm", "tmux" — set by composite backend
}

// Backend defines the interface for interacting with a terminal multiplexer.
type Backend interface {
	// Available checks whether the backend binary is installed and usable.
	Available() error

	// ListSessions returns all active sessions.
	ListSessions() ([]Session, error)

	// NewSession creates a new session and returns its ID.
	NewSession() (string, error)

	// SendText sends raw text (keystrokes) to a session.
	SendText(sessionID, text string) error

	// RunCommand executes a shell command inside a session.
	RunCommand(sessionID, cmd string) error

	// FocusSession brings a session to the foreground.
	FocusSession(sessionID string) error

	// ReadScreen captures visible terminal output from a session.
	ReadScreen(sessionID string, lines int) (string, error)

	// GetVar reads a multiplexer variable from a session.
	GetVar(sessionID, varName string) (string, error)

	// MonitorOutput starts monitoring a session's output, logging to a file
	// and watching for patterns. Returns a process ID or pipe identifier.
	MonitorOutput(sessionID, logPath, patterns string) (int, error)
}

// StyledReader is an optional interface for backends that can capture screen
// content with SGR color/style escape sequences preserved. It is used for
// display only (the chat stream preview and embedded terminal view) — never for
// status classification, which always reads plain text via Backend.ReadScreen.
//
// Callers should type-assert a Backend to StyledReader and fall back to
// ReadScreen when the assertion fails, so a missing implementation degrades to
// plain (colorless) output rather than an error.
type StyledReader interface {
	// ReadScreenStyled captures visible terminal output from a session with
	// SGR escape sequences (colors, bold, italic, underline, reverse) intact.
	ReadScreenStyled(sessionID string, lines int) (string, error)
}

// Optional interfaces. A Backend may implement any of these; callers
// type-assert and degrade when the assertion fails. CompositeBackend
// implements the routing and reporting interfaces (all but Invalidator);
// CachedBackend forwards them and adds Invalidator, so a caller holding either
// can assert once and stop worrying about which backend owns a session.
//
// Closing is expressed with io.Closer rather than a local interface.

// Resizer is implemented by backends whose sessions have a size the caller
// controls. The PTY backend implements it; terminal applications size their
// own panes.
type Resizer interface {
	Resize(cols, rows int)
}

// SourceLauncher is implemented by aggregating backends that can launch on a
// specific member, identified by its source label ("pty", "tmux", ...).
type SourceLauncher interface {
	NewSessionOn(source string) (string, error)
}

// Invalidator is implemented by caching backends. Call it after a mutation
// (a launch, a removed integration) so the next ListSessions refetches.
type Invalidator interface {
	Invalidate()
}

// FailureReporter is implemented by aggregating backends that can say which
// integration sources failed during the most recent ListSessions call that
// itself returned without error (a primary failure returns early and leaves
// the previous report in place). A caller that tracks sessions should keep
// those belonging to a failed source rather than treat their absence as an
// exit.
type FailureReporter interface {
	FailedSources() []string
}

// PrimaryReporter is implemented by aggregating backends and names the source
// whose sessions are listed unprefixed and that NewSession launches on.
type PrimaryReporter interface {
	PrimarySource() string
}

// BellSource is implemented by backends that track the terminal bell
// themselves. ConsumeBell reports whether the session rang its bell since the
// last time anyone asked and clears the flag. The PTY backend's plain
// ReadScreen already consumes the flag and prefixes "\x07" to its output; its
// styled read leaves the flag alone because a BEL in display output would
// ring the viewer's own terminal. ConsumeBell exists for callers that read
// only the styled screen and still need the bell.
type BellSource interface {
	ConsumeBell(sessionID string) bool
}
