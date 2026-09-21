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
