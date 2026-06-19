package kitty

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/sethdeckard/atria/internal/terminal"
)

// ttyForPID delegates to the shared terminal.TTYForPID helper.
var ttyForPID = terminal.TTYForPID

// Client implements terminal.Backend using the kitten @ CLI.
// Communication uses Kitty's Unix socket (KITTY_LISTEN_ON) to avoid
// TTY-based escape sequences that conflict with Bubble Tea's alt screen.
type Client struct {
	kittenPath string
	listenOn   string // socket address from KITTY_LISTEN_ON
}

// NewClient creates a new Kitty Client. Empty kittenPath defaults to "kitten".
func NewClient(kittenPath string) *Client {
	if kittenPath == "" {
		kittenPath = "kitten"
	}
	return &Client{kittenPath: kittenPath}
}

// run executes kitten @ with the given arguments and returns stdout.
// Always uses --to for socket-based communication to avoid TTY conflicts.
func (c *Client) run(args ...string) ([]byte, error) {
	fullArgs := []string{"@", "--to", c.listenOn}
	fullArgs = append(fullArgs, args...)
	cmd := exec.Command(c.kittenPath, fullArgs...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("kitten @ %v failed: %s", args, string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("kitten @ %v failed: %w", args, err)
	}
	return out, nil
}

// Available checks if kitten is installed and socket-based remote control
// is available. Requires KITTY_LISTEN_ON to be set (listen_on in kitty.conf).
func (c *Client) Available() error {
	path, err := exec.LookPath(c.kittenPath)
	if err != nil {
		return fmt.Errorf("kitten not found in PATH")
	}
	c.kittenPath = path

	listenOn := os.Getenv("KITTY_LISTEN_ON")
	if listenOn == "" {
		if os.Getenv("KITTY_WINDOW_ID") == "" {
			return fmt.Errorf("not running inside Kitty")
		}
		return fmt.Errorf("KITTY_LISTEN_ON not set — add listen_on to kitty.conf")
	}
	c.listenOn = listenOn

	// Verify remote control is enabled by running ls via socket.
	if _, err := c.run("ls"); err != nil {
		return fmt.Errorf("Kitty remote control is not enabled")
	}
	return nil
}

// lsOutput represents the nested JSON from kitten @ ls.
type lsOutput struct {
	ID   int `json:"id"`
	Tabs []struct {
		ID      int `json:"id"`
		Windows []struct {
			ID        int    `json:"id"`
			Title     string `json:"title"`
			CWD       string `json:"cwd"`
			PID       int    `json:"pid"`
			IsFocused bool   `json:"is_focused"`
		} `json:"windows"`
	} `json:"tabs"`
}

// kittyWindow holds the parsed fields for a single Kitty window.
type kittyWindow struct {
	ID    int
	Title string
	CWD   string
	PID   int
}

// parseLSOutput flattens the nested kitten @ ls JSON into kitty windows.
func parseLSOutput(data []byte) ([]kittyWindow, error) {
	var osWindows []lsOutput
	if err := json.Unmarshal(data, &osWindows); err != nil {
		return nil, fmt.Errorf("parse kitten @ ls: %w", err)
	}
	var windows []kittyWindow
	for _, osWin := range osWindows {
		for _, tab := range osWin.Tabs {
			for _, win := range tab.Windows {
				windows = append(windows, kittyWindow{
					ID:    win.ID,
					Title: win.Title,
					CWD:   win.CWD,
					PID:   win.PID,
				})
			}
		}
	}
	return windows, nil
}

// ListSessions returns all Kitty windows as terminal sessions.
// TTY is resolved from the window's PID so DiscoverCWD's lsof fallback works.
func (c *Client) ListSessions() ([]terminal.Session, error) {
	out, err := c.run("ls")
	if err != nil {
		return nil, err
	}
	windows, err := parseLSOutput(out)
	if err != nil {
		return nil, err
	}
	sessions := make([]terminal.Session, 0, len(windows))
	for _, w := range windows {
		sessions = append(sessions, terminal.Session{
			ID:   strconv.Itoa(w.ID),
			Name: w.Title,
			TTY:  ttyForPID(w.PID),
		})
	}
	return sessions, nil
}

// NewSession launches a new tab in Kitty and returns its window ID.
func (c *Client) NewSession() (string, error) {
	out, err := c.run("launch", "--type=tab")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// SendText sends literal text to a Kitty window. Carriage return and newline
// are sent directly (Kitty handles them as keypresses).
func (c *Client) SendText(sessionID, text string) error {
	_, err := c.run("send-text", "--match", "id:"+sessionID, text)
	return err
}

// RunCommand sends a command string followed by Enter to a Kitty window.
func (c *Client) RunCommand(sessionID, cmd string) error {
	if err := c.SendText(sessionID, cmd); err != nil {
		return err
	}
	return c.SendText(sessionID, "\r")
}

// FocusSession focuses the Kitty window with the given ID.
func (c *Client) FocusSession(sessionID string) error {
	_, err := c.run("focus-window", "--match", "id:"+sessionID)
	return err
}

// ReadScreen captures the visible screen text from a Kitty window.
func (c *Client) ReadScreen(sessionID string, lines int) (string, error) {
	out, err := c.run("get-text", "--match", "id:"+sessionID, "--extent", "screen")
	if err != nil {
		return "", err
	}
	return terminal.TrimScreenTail(string(out), lines), nil
}

// Compile-time check that the Kitty backend supports styled reads.
var _ terminal.StyledReader = (*Client)(nil)

// ReadScreenStyled captures the visible screen text from a Kitty window with
// ANSI color/style escapes preserved (for display only).
func (c *Client) ReadScreenStyled(sessionID string, lines int) (string, error) {
	out, err := c.run("get-text", "--match", "id:"+sessionID, "--extent", "screen", "--ansi")
	if err != nil {
		return "", err
	}
	return terminal.TrimScreenTail(string(out), lines), nil
}

// lookupWindowVar finds a window by ID string and returns the requested variable.
func lookupWindowVar(windows []kittyWindow, sessionID, varName string) (string, error) {
	if varName != "path" && varName != "pid" {
		return "", fmt.Errorf("unsupported variable: %s", varName)
	}
	for _, w := range windows {
		if strconv.Itoa(w.ID) == sessionID {
			switch varName {
			case "path":
				return w.CWD, nil
			case "pid":
				return strconv.Itoa(w.PID), nil
			}
		}
	}
	return "", fmt.Errorf("window %s not found", sessionID)
}

// GetVar reads a variable from a Kitty window. Supported: "path", "pid".
// Re-runs kitten @ ls and filters by window ID.
func (c *Client) GetVar(sessionID, varName string) (string, error) {
	out, err := c.run("ls")
	if err != nil {
		return "", err
	}
	windows, err := parseLSOutput(out)
	if err != nil {
		return "", err
	}
	return lookupWindowVar(windows, sessionID, varName)
}

// MonitorOutput is not supported by the Kitty backend. Screen reads are the
// primary status detection mechanism.
func (c *Client) MonitorOutput(sessionID, logPath, patterns string) (int, error) {
	return 0, fmt.Errorf("kitty backend does not support output monitoring")
}
