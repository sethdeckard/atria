package kitty

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sethdeckard/atria/libatria/terminal"
)

// Options configures a Client. The zero value finds kitten on PATH and uses
// terminal.DefaultCommandTimeout.
type Options struct {
	// Path is the kitten binary; empty means "kitten".
	Path string
	// CommandTimeout bounds each kitten invocation; zero means
	// terminal.DefaultCommandTimeout. A hung kitten is reported as
	// terminal.ErrUnavailable.
	CommandTimeout time.Duration
}

// Client implements terminal.Backend using the kitten @ CLI.
// Communication uses Kitty's Unix socket (KITTY_LISTEN_ON) to avoid
// TTY-based escape sequences that conflict with Bubble Tea's alt screen.
type Client struct {
	kittenPath string
	timeout    time.Duration
	ttyForPID  func(pid int) string // terminal.TTYForPID; replaced in tests

	mu       sync.RWMutex
	listenOn string // socket address from KITTY_LISTEN_ON, set by Available
}

// NewClient creates a Kitty Client from opts.
func NewClient(opts Options) *Client {
	path := opts.Path
	if path == "" {
		path = "kitten"
	}
	return &Client{
		kittenPath: path,
		timeout:    terminal.TimeoutOr(opts.CommandTimeout),
		ttyForPID:  terminal.TTYForPID,
	}
}

func (c *Client) socket() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.listenOn
}

// run executes kitten @ with the given arguments under the command timeout
// and returns stdout. Always uses --to for socket-based communication to
// avoid TTY conflicts. A timeout, a failure to start kitten, or a failure to
// reach the socket is wrapped as terminal.ErrUnavailable.
func (c *Client) run(args ...string) ([]byte, error) {
	fullArgs := []string{"@", "--to", c.socket()}
	fullArgs = append(fullArgs, args...)
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.kittenPath, fullArgs...)
	cmd.WaitDelay = terminal.PipeGrace
	out, err := cmd.Output()
	if err == nil {
		return out, nil
	}
	op := "kitten @ " + verb(args)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, terminal.Timeout(op, c.timeout)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		msg := strings.TrimSpace(string(exitErr.Stderr))
		if isConnectMessage(msg) {
			return nil, terminal.Unavailable(op, errors.New(msg))
		}
		return nil, fmt.Errorf("%s failed: %s", op, msg)
	}
	return nil, terminal.Unavailable(op, err)
}

func verb(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

// isConnectMessage reports whether kitten's stderr describes a failure to
// reach the socket rather than a remote-control error.
func isConnectMessage(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "failed to connect") ||
		strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "no such file or directory") ||
		strings.Contains(lower, "could not connect")
}

// Available checks if kitten is installed and socket-based remote control
// is available. Requires KITTY_LISTEN_ON to be set (listen_on in kitty.conf).
// It may be called again later to re-read the socket address.
func (c *Client) Available() error {
	if _, err := exec.LookPath(c.kittenPath); err != nil {
		return fmt.Errorf("kitten not found in PATH")
	}

	listenOn := os.Getenv("KITTY_LISTEN_ON")
	if listenOn == "" {
		if os.Getenv("KITTY_WINDOW_ID") == "" {
			return fmt.Errorf("not running inside Kitty")
		}
		return fmt.Errorf("KITTY_LISTEN_ON not set — add listen_on to kitty.conf")
	}
	c.mu.Lock()
	c.listenOn = listenOn
	c.mu.Unlock()

	// Verify remote control is enabled by running ls via socket.
	if _, err := c.run("ls"); err != nil {
		return fmt.Errorf("kitty remote control is not enabled")
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
			TTY:  c.ttyForPID(w.PID),
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
