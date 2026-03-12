package pty

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/hinshun/vt10x"
	"github.com/sethdeckard/atria/internal/terminal"
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
func (c *Client) ListSessions() ([]terminal.Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var sessions []terminal.Session
	for _, s := range c.sessions {
		if s.isExited() {
			continue
		}
		sessions = append(sessions, terminal.Session{
			ID:   s.id,
			Name: s.getName(),
		})
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
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	winSize := &pty.Winsize{
		Cols: uint16(c.cols),
		Rows: uint16(c.rows),
	}
	ptmx, err := pty.StartWithSize(cmd, winSize)
	if err != nil {
		return "", fmt.Errorf("pty start: %w", err)
	}

	c.mu.Lock()
	id := fmt.Sprintf("pty-%d", c.nextID)
	c.nextID++

	term := vt10x.New(vt10x.WithSize(c.cols, c.rows))

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
		cmd.Wait()
	}()

	return id, nil
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
		return cwdFromPID(s.cmd.Process.Pid)
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
		pty.Setsize(s.ptmx, winSize)
		s.term.Resize(cols, rows)
	}
}

// Close closes PTY fds (unblocking reader goroutines), sends SIGTERM to all
// child processes, and waits briefly for reader goroutines to finish.
func (c *Client) Close() {
	c.mu.Lock()
	sessions := make([]*session, 0, len(c.sessions))
	for _, s := range c.sessions {
		sessions = append(sessions, s)
	}
	c.mu.Unlock()

	for _, s := range sessions {
		// Close ptmx first to unblock the readLoop
		s.ptmx.Close()
		if s.cmd.Process != nil {
			s.cmd.Process.Signal(syscall.SIGTERM)
		}
		// Wait for readLoop with timeout
		select {
		case <-s.done:
		case <-time.After(shutdownTimeout):
			if s.cmd.Process != nil {
				s.cmd.Process.Kill()
			}
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

// cwdFromPID discovers the current working directory for a given PID.
func cwdFromPID(pid int) (string, error) {
	// Try /proc first (Linux)
	procPath := fmt.Sprintf("/proc/%d/cwd", pid)
	if target, err := os.Readlink(procPath); err == nil {
		return target, nil
	}

	// Fall back to lsof (macOS)
	out, err := exec.Command("lsof", "-a", "-d", "cwd", "-p", strconv.Itoa(pid), "-Fn").Output()
	if err != nil {
		return "", fmt.Errorf("cwd lookup failed for pid %d: %w", pid, err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "n") {
			return line[1:], nil
		}
	}
	return "", fmt.Errorf("cwd not found for pid %d", pid)
}
