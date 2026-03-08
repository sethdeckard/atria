package iterm

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/sethdeckard/atria/internal/terminal"
)

// defaultVenvDir is where atria recommends installing it2.
const defaultVenvDir = "~/.local/share/atria/venv"

// Client implements terminal.Backend by wrapping the it2 CLI tool.
type Client struct {
	it2Path string
}

// NewClient creates a new Client with the given path to the it2 binary.
func NewClient(it2Path string) *Client {
	return &Client{it2Path: it2Path}
}

// run executes it2 with the given arguments and returns stdout.
func (c *Client) run(args ...string) ([]byte, error) {
	cmd := exec.Command(c.it2Path, args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("it2 %v failed: %s", args, string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("it2 %v failed: %w", args, err)
	}
	return out, nil
}

// Available checks if it2 is usable by running "it2 session list --json".
// If it2Path is empty, searches PATH then common venv locations.
// Returns concise errors suitable for the TUI status bar. For detailed
// pre-flight output, use Preflight() before starting the TUI.
func (c *Client) Available() error {
	if c.it2Path == "" {
		c.it2Path = findIT2()
	}
	if c.it2Path == "" {
		return fmt.Errorf("it2 not found — run atria again to install")
	}
	_, err := c.run("session", "list", "--json")
	if err != nil {
		return fmt.Errorf("it2 cannot connect to iTerm2 — check Python API setting")
	}
	return nil
}

// findIT2 searches for the it2 binary in PATH and common venv locations.
func findIT2() string {
	// 1. Check PATH
	if path, err := exec.LookPath("it2"); err == nil {
		return path
	}

	// 2. Check atria's recommended venv
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	candidates := []string{
		filepath.Join(home, ".local", "share", "atria", "venv", "bin", "it2"),
		filepath.Join(home, ".venvs", "iterm2", "bin", "it2"),
		filepath.Join(home, ".venv", "bin", "it2"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// it2Session represents a session as returned by it2 session list --json.
type it2Session struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	TTY      string `json:"tty"`
	Rows     int    `json:"rows"`
	Cols     int    `json:"cols"`
	IsTmux   bool   `json:"is_tmux"`
	WindowID string `json:"window_id"`
	TabID    string `json:"tab_id"`
}

// ListSessions runs "it2 session list --json" and parses the JSON output.
func (c *Client) ListSessions() ([]terminal.Session, error) {
	out, err := c.run("session", "list", "--json")
	if err != nil {
		return nil, err
	}
	var raw []it2Session
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse session list: %w", err)
	}
	sessions := make([]terminal.Session, len(raw))
	for i, s := range raw {
		sessions[i] = terminal.Session{
			ID:   s.ID,
			Name: s.Name,
			TTY:  s.TTY,
		}
	}
	return sessions, nil
}

// NewSession runs "it2 tab new", waits 300ms, then returns the ID of the newest session.
func (c *Client) NewSession() (string, error) {
	_, err := c.run("tab", "new")
	if err != nil {
		return "", err
	}
	time.Sleep(300 * time.Millisecond)
	sessions, err := c.ListSessions()
	if err != nil {
		return "", err
	}
	if len(sessions) == 0 {
		return "", fmt.Errorf("no sessions found after creating new tab")
	}
	return sessions[len(sessions)-1].ID, nil
}

// SendText runs "it2 session send -s <id> <text>".
func (c *Client) SendText(sessionID, text string) error {
	_, err := c.run("session", "send", "-s", sessionID, text)
	return err
}

// RunCommand runs "it2 session run -s <id> <cmd>".
func (c *Client) RunCommand(sessionID, cmd string) error {
	_, err := c.run("session", "run", "-s", sessionID, cmd)
	return err
}

// FocusSession runs "it2 session focus <id>".
func (c *Client) FocusSession(sessionID string) error {
	_, err := c.run("session", "focus", sessionID)
	return err
}

// ReadScreen runs "it2 session read -s <id> -n <lines>".
func (c *Client) ReadScreen(sessionID string, lines int) (string, error) {
	out, err := c.run("session", "read", "-s", sessionID, "-n", strconv.Itoa(lines))
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// GetVar runs "it2 session get-var -s <id> <varName>".
func (c *Client) GetVar(sessionID, varName string) (string, error) {
	out, err := c.run("session", "get-var", "-s", sessionID, varName)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// MonitorOutput spawns "it2 monitor output -s <id> -f -p <patterns>" as a background process,
// redirecting stdout to logPath. Returns the PID.
func (c *Client) MonitorOutput(sessionID, logPath, patterns string) (int, error) {
	cmd := exec.Command(c.it2Path, "monitor", "output", "-s", sessionID, "-f", "-p", patterns)
	f, err := os.Create(logPath)
	if err != nil {
		return 0, fmt.Errorf("failed to create log file: %w", err)
	}
	cmd.Stdout = f
	if err := cmd.Start(); err != nil {
		f.Close()
		return 0, fmt.Errorf("failed to start monitor: %w", err)
	}
	return cmd.Process.Pid, nil
}
