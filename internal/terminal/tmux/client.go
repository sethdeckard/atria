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
	tmuxPath      string
	launchSession string
}

// NewClient creates a new tmux Client. Empty tmuxPath defaults to "tmux".
// Empty launchSession means "use the current tmux session when inside tmux".
func NewClient(tmuxPath, launchSession string) *Client {
	if tmuxPath == "" {
		tmuxPath = "tmux"
	}
	return &Client{tmuxPath: tmuxPath, launchSession: launchSession}
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

// ListSessions returns all panes in the tmux server as terminal sessions.
// Returns an empty list when no tmux server is running.
func (c *Client) ListSessions() ([]terminal.Session, error) {
	out, err := c.run("list-panes", "-a",
		"-F", "#{pane_id}\t#{pane_title}\t#{window_name}\t#{pane_tty}")
	if err != nil {
		if isNoServerError(err) {
			return nil, nil
		}
		return nil, err
	}
	return parsePaneList(string(out)), nil
}

func isNoServerError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "no server running") ||
		strings.Contains(msg, "failed to connect to server")
}

func isSessionNotFoundError(err error) bool {
	return strings.Contains(err.Error(), "can't find session")
}

func (c *Client) currentSession() (string, error) {
	out, err := c.run("display-message", "-p", "#{session_name}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (c *Client) targetSession() (string, error) {
	if c.launchSession != "" {
		return c.launchSession, nil
	}
	if os.Getenv("TMUX") != "" {
		session, err := c.currentSession()
		if err == nil && session != "" {
			return session, nil
		}
	}
	return "atria", nil
}

func sessionTarget(session string) string {
	return "=" + session
}

func windowTarget(session string) string {
	return "=" + session + ":"
}

func (c *Client) sessionExists(session string) (bool, error) {
	_, err := c.run("has-session", "-t", sessionTarget(session))
	if err == nil {
		return true, nil
	}
	if isNoServerError(err) || isSessionNotFoundError(err) {
		return false, nil
	}
	return false, err
}

func chooseSessionName(paneTitle, windowName string) string {
	title := strings.TrimSpace(paneTitle)
	window := strings.TrimSpace(windowName)

	switch {
	case title != "" && terminal.DetectAgent(title) != "":
		return title
	case window != "" && terminal.DetectAgent(window) != "":
		return window
	case title != "":
		return title
	default:
		return window
	}
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
		var paneTitle, windowName string
		if len(fields) >= 2 {
			paneTitle = fields[1]
		}
		if len(fields) >= 3 {
			windowName = fields[2]
		}
		s.Name = chooseSessionName(paneTitle, windowName)
		if len(fields) >= 4 {
			s.TTY = fields[3]
		}
		sessions = append(sessions, s)
	}
	return sessions
}

// NewSession creates a new window in the target tmux session and returns its
// pane ID. If the session doesn't exist yet, it creates it with this window.
func (c *Client) NewSession() (string, error) {
	session, err := c.targetSession()
	if err != nil {
		return "", err
	}
	exists, err := c.sessionExists(session)
	if err != nil {
		return "", err
	}
	if !exists {
		out, err := c.run("new-session", "-d", "-s", session, "-P", "-F", "#{pane_id}")
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	}
	out, err := c.run("new-window", "-t", windowTarget(session), "-P", "-F", "#{pane_id}")
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
		// Best-effort: only works when running inside tmux.
		out, lookupErr := c.run("display-message", "-t", sessionID, "-p", "#{session_name}")
		if lookupErr == nil {
			owningSession := strings.TrimSpace(string(out))
			if owningSession != "" {
				c.run("switch-client", "-t", sessionTarget(owningSession)) //nolint:errcheck // best-effort client switch
			}
		}
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
