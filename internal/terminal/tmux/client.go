package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/sethdeckard/atria/internal/terminal"
)

// Client implements terminal.Backend using the tmux CLI.
type Client struct {
	tmuxPath    string
	sessionName string
}

// NewClient creates a new tmux Client. Empty tmuxPath defaults to "tmux",
// empty sessionName defaults to "atria".
func NewClient(tmuxPath, sessionName string) *Client {
	if tmuxPath == "" {
		tmuxPath = "tmux"
	}
	if sessionName == "" {
		sessionName = "atria"
	}
	return &Client{tmuxPath: tmuxPath, sessionName: sessionName}
}

// run executes tmux with the given arguments and returns stdout.
func (c *Client) run(args ...string) ([]byte, error) {
	cmd := exec.Command(c.tmuxPath, args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("tmux %v failed: %s", args, string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("tmux %v failed: %w", args, err)
	}
	return out, nil
}

// Available checks if tmux is installed.
func (c *Client) Available() error {
	path, err := exec.LookPath(c.tmuxPath)
	if err != nil {
		return fmt.Errorf("tmux not found in PATH")
	}
	c.tmuxPath = path
	return nil
}

// ListSessions returns all panes in the atria tmux session as terminal sessions.
// Returns an empty list if the session does not exist yet.
func (c *Client) ListSessions() ([]terminal.Session, error) {
	out, err := c.run("list-panes", "-s", "-t", c.sessionName,
		"-F", "#{pane_id}\t#{pane_title}\t#{window_name}\t#{pane_tty}")
	if err != nil {
		// Session doesn't exist yet — no agents launched
		if !c.hasSession() {
			return nil, nil
		}
		return nil, err
	}
	return parsePaneList(string(out)), nil
}

// hasSession checks if the atria tmux session exists.
func (c *Client) hasSession() bool {
	return exec.Command(c.tmuxPath, "has-session", "-t", c.sessionName).Run() == nil
}

// parsePaneList parses tab-separated list-panes output into terminal sessions.
func parsePaneList(output string) []terminal.Session {
	var sessions []terminal.Session
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 4)
		if len(fields) < 1 {
			continue
		}
		s := terminal.Session{ID: fields[0]}
		if len(fields) >= 2 {
			s.Name = fields[1]
		}
		// Prefer pane_title over window_name; fall back if title is empty
		if s.Name == "" && len(fields) >= 3 {
			s.Name = fields[2]
		}
		if len(fields) >= 4 {
			s.TTY = fields[3]
		}
		sessions = append(sessions, s)
	}
	return sessions
}

// NewSession creates a new window in the atria tmux session and returns its pane ID.
// If the session doesn't exist yet, it creates it with this window (no orphan shell).
func (c *Client) NewSession() (string, error) {
	if !c.hasSession() {
		out, err := c.run("new-session", "-d", "-s", c.sessionName, "-P", "-F", "#{pane_id}")
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	}
	out, err := c.run("new-window", "-t", c.sessionName, "-P", "-F", "#{pane_id}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// SendText sends literal text to a tmux pane. Carriage return and newline
// are sent as the Enter key instead of literal characters.
func (c *Client) SendText(sessionID, text string) error {
	if text == "\r" || text == "\n" {
		_, err := c.run("send-keys", "-t", sessionID, "Enter")
		return err
	}
	_, err := c.run("send-keys", "-t", sessionID, "-l", text)
	return err
}

// RunCommand sends a command string followed by Enter to a tmux pane.
func (c *Client) RunCommand(sessionID, cmd string) error {
	if _, err := c.run("send-keys", "-t", sessionID, "-l", cmd); err != nil {
		return err
	}
	_, err := c.run("send-keys", "-t", sessionID, "Enter")
	return err
}

// FocusSession selects the window containing the pane and attempts to switch
// the tmux client. The switch-client is best-effort (fails silently when
// Atria is not running inside tmux).
func (c *Client) FocusSession(sessionID string) error {
	_, err := c.run("select-window", "-t", sessionID)
	if err != nil {
		return err
	}
	if os.Getenv("TMUX") != "" {
		// Best-effort: only works when running inside tmux
		c.run("switch-client", "-t", c.sessionName)
	}
	return nil
}

// ReadScreen captures the last N lines from a tmux pane as plain text.
func (c *Client) ReadScreen(sessionID string, lines int) (string, error) {
	out, err := c.run("capture-pane", "-t", sessionID, "-p",
		"-S", fmt.Sprintf("-%d", lines))
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// GetVar reads a tmux variable from a pane. Supported variables:
// "path" -> #{pane_current_path}, "pid" -> #{pane_pid}.
func (c *Client) GetVar(sessionID, varName string) (string, error) {
	var fmtStr string
	switch varName {
	case "path":
		fmtStr = "#{pane_current_path}"
	case "pid":
		fmtStr = "#{pane_pid}"
	default:
		return "", fmt.Errorf("unsupported variable: %s", varName)
	}
	out, err := c.run("display-message", "-t", sessionID, "-p", fmtStr)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// MonitorOutput is not supported by the tmux backend. Screen reads every 3s
// are the primary status detection mechanism.
func (c *Client) MonitorOutput(sessionID, logPath, patterns string) (int, error) {
	return 0, fmt.Errorf("tmux backend does not support output monitoring")
}
