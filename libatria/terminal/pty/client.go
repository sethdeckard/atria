package pty

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/hinshun/vt10x"
	"github.com/sethdeckard/atria/libatria/terminal"
)

const (
	// DefaultCols is the default terminal width when not configured.
	DefaultCols = 120
	// DefaultRows is the default terminal height when not configured.
	DefaultRows = 40

	readBufSize     = 4096
	shutdownTimeout = 2 * time.Second
)

// Client implements terminal.Backend using built-in PTY management.
// Each agent session runs in its own pseudo-terminal with a vt10x emulator.
type Client struct {
	mu       sync.Mutex
	sessions map[string]*session
	nextID   int
	cols     int
	rows     int
}

// NewClient creates a new PTY backend client with the given terminal dimensions.
func NewClient(cols, rows int) *Client {
	if cols <= 0 {
		cols = DefaultCols
	}
	if rows <= 0 {
		rows = DefaultRows
	}
	return &Client{
		sessions: make(map[string]*session),
		cols:     cols,
		rows:     rows,
	}
}

// Available always returns nil since the PTY backend has no external dependencies.
func (c *Client) Available() error {
	return nil
}

// ListSessions returns all active (non-exited) sessions.
// Exited sessions are cleaned up (fd closed, process reaped) but kept in the
// map so they remain addressable for ReadScreen until Close().
func (c *Client) ListSessions() ([]terminal.Session, error) {
	c.mu.Lock()
	var needsCleanup []*session
	sessions := make([]terminal.Session, 0, len(c.sessions))
	for _, s := range c.sessions {
		if s.isExited() {
			if !s.isCleaned() {
				needsCleanup = append(needsCleanup, s)
			}
			continue
		}
		sessions = append(sessions, terminal.Session{
			ID:   s.id,
			Name: s.getName(),
		})
	}
	c.mu.Unlock()

	for _, s := range needsCleanup {
		cleanupSession(s)
	}

	return sessions, nil
}

// NewSession spawns a new shell in a PTY and returns its session ID.
func (c *Client) NewSession() (string, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}

	cmd := exec.Command(shell)
	cmd.Env = append(filteredEnv(os.Environ()), "TERM=xterm-256color")

	// Snapshot the dimensions under the lock Resize writes them under, and
	// use the one snapshot for both the pty and the emulator so they agree.
	c.mu.Lock()
	cols, rows := c.cols, c.rows
	c.mu.Unlock()

	winSize := &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	}
	ptmx, err := pty.StartWithSize(cmd, winSize)
	if err != nil {
		return "", fmt.Errorf("pty start: %w", err)
	}

	c.mu.Lock()
	id := fmt.Sprintf("pty-%d", c.nextID)
	c.nextID++

	term := vt10x.New(vt10x.WithSize(cols, rows))

	s := &session{
		id:   id,
		ptmx: ptmx,
		cmd:  cmd,
		term: term,
		done: make(chan struct{}),
	}
	c.sessions[id] = s
	c.mu.Unlock()

	go s.readLoop()

	// Reap the child process in the background to avoid zombies
	go func() {
		cmd.Wait() //nolint:errcheck // reaping only
	}()

	return id, nil
}

func filteredEnv(env []string) []string {
	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		if strings.HasPrefix(entry, "ITERM2_COOKIE=") || strings.HasPrefix(entry, "ITERM2_KEY=") {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

// SendText writes raw text to the session's PTY.
func (c *Client) SendText(sessionID, text string) error {
	s, err := c.getSession(sessionID)
	if err != nil {
		return err
	}
	if _, err = s.ptmx.Write([]byte(text)); err != nil {
		return fmt.Errorf("pty send: %w", err)
	}
	return nil
}

// RunCommand sends a command string followed by a newline to the session's PTY.
func (c *Client) RunCommand(sessionID, cmd string) error {
	return c.SendText(sessionID, cmd+"\n")
}

// FocusSession is a no-op for the PTY backend. The TUI handles display
// by switching to the embedded terminal view.
func (c *Client) FocusSession(sessionID string) error {
	return nil
}

// ReadScreen returns the last N lines from the session's vt10x screen buffer.
func (c *Client) ReadScreen(sessionID string, lines int) (string, error) {
	s, err := c.getSession(sessionID)
	if err != nil {
		return "", err
	}
	return s.readScreen(lines), nil
}

// ReadScreenStyled returns the last N lines from the session's vt10x screen
// buffer with SGR color/style escapes preserved (for display only).
func (c *Client) ReadScreenStyled(sessionID string, lines int) (string, error) {
	s, err := c.getSession(sessionID)
	if err != nil {
		return "", err
	}
	return s.readScreenStyled(lines), nil
}

// GetVar reads session variables. Supported: "pid", "path".
func (c *Client) GetVar(sessionID, varName string) (string, error) {
	s, err := c.getSession(sessionID)
	if err != nil {
		return "", err
	}

	switch varName {
	case "pid":
		if s.cmd.Process == nil {
			return "", fmt.Errorf("process not started")
		}
		return strconv.Itoa(s.cmd.Process.Pid), nil
	case "path":
		if s.cmd.Process == nil {
			return "", fmt.Errorf("process not started")
		}
		return terminal.ProcessCWD(s.cmd.Process.Pid)
	default:
		return "", fmt.Errorf("unsupported variable: %s", varName)
	}
}

// MonitorOutput is not supported by the PTY backend. Screen reads every 3s
// are the primary status detection mechanism.
func (c *Client) MonitorOutput(sessionID, logPath, patterns string) (int, error) {
	return 0, fmt.Errorf("pty backend does not support output monitoring")
}

// Resize updates the terminal dimensions for all active sessions.
func (c *Client) Resize(cols, rows int) {
	if cols <= 0 || rows <= 0 {
		return
	}
	c.mu.Lock()
	c.cols = cols
	c.rows = rows
	sessions := make([]*session, 0, len(c.sessions))
	for _, s := range c.sessions {
		sessions = append(sessions, s)
	}
	c.mu.Unlock()

	winSize := &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	}
	for _, s := range sessions {
		if s.isExited() {
			continue
		}
		if err := pty.Setsize(s.ptmx, winSize); err == nil {
			s.resizeTerm(cols, rows)
		}
	}
}

// Close cleans up all sessions (both live and exited): each PTY is closed and
// its process sent SIGTERM, escalating to SIGKILL if the session's reader has
// not finished within two seconds. Cleanup is best-effort and always returns
// nil; a process that ignores both signals is not reported.
func (c *Client) Close() error {
	c.mu.Lock()
	sessions := make([]*session, 0, len(c.sessions))
	for _, s := range c.sessions {
		sessions = append(sessions, s)
	}
	c.mu.Unlock()

	for _, s := range sessions {
		cleanupSession(s)
	}
	return nil
}

// ConsumeBell reports whether the session rang its bell since the last plain
// ReadScreen or ConsumeBell, and clears the flag. It implements
// terminal.BellSource for callers that read only the styled screen. An unknown
// session reports false.
func (c *Client) ConsumeBell(sessionID string) bool {
	s, err := c.getSession(sessionID)
	if err != nil {
		return false
	}
	return s.takeBell()
}

// Compile-time checks for the optional interfaces the PTY backend implements.
var (
	_ terminal.Backend      = (*Client)(nil)
	_ terminal.StyledReader = (*Client)(nil)
	_ terminal.Resizer      = (*Client)(nil)
	_ terminal.BellSource   = (*Client)(nil)
	_ io.Closer             = (*Client)(nil)
)

// cleanupSession is best-effort and idempotent — safe to call on
// already-exited or previously-cleaned sessions. Errors from
// Close/Signal/Kill are ignored; s.done is the real completion signal.
func cleanupSession(s *session) {
	s.mu.Lock()
	if s.cleaned {
		s.mu.Unlock()
		return
	}
	s.cleaned = true
	s.mu.Unlock()

	s.ptmx.Close() //nolint:errcheck // best-effort cleanup
	if s.cmd.Process != nil {
		s.cmd.Process.Signal(syscall.SIGTERM) //nolint:errcheck // process may already be dead
	}
	select {
	case <-s.done:
	case <-time.After(shutdownTimeout):
		if s.cmd.Process != nil {
			s.cmd.Process.Kill() //nolint:errcheck // best-effort force kill
		}
	}
}

func (c *Client) getSession(id string) (*session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.sessions[id]
	if !ok {
		return nil, fmt.Errorf("session not found: %s", id)
	}
	return s, nil
}
