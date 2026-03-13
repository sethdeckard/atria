package wezterm

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"strconv"
	"strings"

	"github.com/sethdeckard/atria/internal/terminal"
)

// Client implements terminal.Backend using the wezterm CLI.
// Communication uses WezTerm's Unix socket (auto-discovered via WEZTERM_UNIX_SOCKET).
type Client struct {
	weztermPath string
}

// NewClient creates a new WezTerm Client. Empty weztermPath defaults to "wezterm".
func NewClient(weztermPath string) *Client {
	if weztermPath == "" {
		weztermPath = "wezterm"
	}
	return &Client{weztermPath: weztermPath}
}

// run executes wezterm cli with the given arguments and returns stdout.
func (c *Client) run(args ...string) ([]byte, error) {
	fullArgs := append([]string{"cli"}, args...)
	cmd := exec.Command(c.weztermPath, fullArgs...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("wezterm cli %v failed: %s", args, string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("wezterm cli %v failed: %w", args, err)
	}
	return out, nil
}

// listEntry represents a single pane from wezterm cli list --format json.
type listEntry struct {
	WindowID  int    `json:"window_id"`
	TabID     int    `json:"tab_id"`
	PaneID    int    `json:"pane_id"`
	Workspace string `json:"workspace"`
	Title     string `json:"title"`
	CWD       string `json:"cwd"`
	TTYName   string `json:"tty_name"`
}

// parseListOutput parses the flat JSON array from wezterm cli list.
func parseListOutput(data []byte) ([]listEntry, error) {
	var entries []listEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse wezterm cli list: %w", err)
	}
	return entries, nil
}

// normalizeCWD strips the file:// URI prefix that WezTerm may use for CWD values.
func normalizeCWD(raw string) string {
	if !strings.HasPrefix(raw, "file://") {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		// Fallback: strip prefix manually.
		return strings.TrimPrefix(raw, "file://")
	}
	return u.Path
}

// Available checks if wezterm is installed and its CLI can reach a running
// instance. Unlike Kitty, wezterm cli auto-discovers the Unix socket without
// needing WEZTERM_UNIX_SOCKET, so this succeeds as long as any WezTerm
// instance is reachable — enabling the "enabled but inactive" state when
// Atria runs outside WezTerm.
func (c *Client) Available() error {
	path, err := exec.LookPath(c.weztermPath)
	if err != nil {
		return fmt.Errorf("wezterm not found in PATH")
	}
	c.weztermPath = path

	// Probe with list to verify connectivity. wezterm cli auto-discovers
	// the socket, so this works from any terminal as long as WezTerm is running.
	if _, err := c.run("list", "--format", "json"); err != nil {
		return fmt.Errorf("wezterm cli probe failed: %w", err)
	}
	return nil
}

// ListSessions returns all WezTerm panes as terminal sessions.
func (c *Client) ListSessions() ([]terminal.Session, error) {
	out, err := c.run("list", "--format", "json")
	if err != nil {
		return nil, err
	}
	entries, err := parseListOutput(out)
	if err != nil {
		return nil, err
	}
	sessions := make([]terminal.Session, 0, len(entries))
	for _, e := range entries {
		sessions = append(sessions, terminal.Session{
			ID:   strconv.Itoa(e.PaneID),
			Name: e.Title,
			TTY:  e.TTYName,
		})
	}
	return sessions, nil
}

// NewSession launches a new window in WezTerm and returns its pane ID.
func (c *Client) NewSession() (string, error) {
	out, err := c.run("spawn")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// SendText sends literal text to a WezTerm pane via stdin to avoid shell escaping.
func (c *Client) SendText(sessionID, text string) error {
	fullArgs := []string{"cli", "send-text", "--pane-id", sessionID, "--no-paste"}
	cmd := exec.Command(c.weztermPath, fullArgs...)
	cmd.Stdin = strings.NewReader(text)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("wezterm cli send-text failed: %s", string(out))
	}
	return nil
}

// RunCommand sends a command string followed by Enter to a WezTerm pane.
func (c *Client) RunCommand(sessionID, cmd string) error {
	if err := c.SendText(sessionID, cmd); err != nil {
		return err
	}
	return c.SendText(sessionID, "\r")
}

// FocusSession activates the WezTerm pane with the given ID.
func (c *Client) FocusSession(sessionID string) error {
	_, err := c.run("activate-pane", "--pane-id", sessionID)
	return err
}

// ReadScreen captures the visible screen text from a WezTerm pane.
func (c *Client) ReadScreen(sessionID string, lines int) (string, error) {
	out, err := c.run("get-text", "--pane-id", sessionID)
	if err != nil {
		return "", err
	}
	return trimToLastN(string(out), lines), nil
}

// trimToLastN returns the last n lines of text.
func trimToLastN(text string, n int) string {
	allLines := strings.Split(text, "\n")
	if len(allLines) > n {
		allLines = allLines[len(allLines)-n:]
	}
	return strings.Join(allLines, "\n")
}

// GetVar reads a variable from a WezTerm pane. Supported: "path".
func (c *Client) GetVar(sessionID, varName string) (string, error) {
	if varName != "path" {
		return "", fmt.Errorf("unsupported variable: %s", varName)
	}
	out, err := c.run("list", "--format", "json")
	if err != nil {
		return "", err
	}
	entries, err := parseListOutput(out)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if strconv.Itoa(e.PaneID) == sessionID {
			return normalizeCWD(e.CWD), nil
		}
	}
	return "", fmt.Errorf("pane %s not found", sessionID)
}

// MonitorOutput is not supported by the WezTerm backend. Screen reads are the
// primary status detection mechanism.
func (c *Client) MonitorOutput(sessionID, logPath, patterns string) (int, error) {
	return 0, fmt.Errorf("wezterm backend does not support output monitoring")
}
