package wezterm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sethdeckard/atria/libatria/terminal"
)

// Client implements terminal.Backend using the wezterm CLI.
// Communication uses WezTerm's Unix socket (auto-discovered via WEZTERM_UNIX_SOCKET).
type Client struct {
	weztermPath string
	timeout     time.Duration
}

// Options configures a Client. The zero value finds wezterm on PATH and uses
// terminal.DefaultCommandTimeout.
type Options struct {
	// Path is the wezterm binary; empty means "wezterm".
	Path string
	// CommandTimeout bounds each wezterm invocation; zero means
	// terminal.DefaultCommandTimeout. A hung wezterm is reported as
	// terminal.ErrUnavailable.
	CommandTimeout time.Duration
}

// NewClient creates a WezTerm Client from opts.
func NewClient(opts Options) *Client {
	path := opts.Path
	if path == "" {
		path = "wezterm"
	}
	return &Client{weztermPath: path, timeout: terminal.TimeoutOr(opts.CommandTimeout)}
}

// run executes wezterm cli with the given arguments under the command timeout
// and returns stdout. A timeout, a failure to start wezterm, or a failure to
// reach its socket is wrapped as terminal.ErrUnavailable.
func (c *Client) run(args ...string) ([]byte, error) {
	return c.runWithStdin(nil, args...)
}

// runWithStdin is run with an optional stdin, used by SendText.
func (c *Client) runWithStdin(stdin io.Reader, args ...string) ([]byte, error) {
	fullArgs := append([]string{"cli"}, args...)
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.weztermPath, fullArgs...)
	cmd.WaitDelay = terminal.PipeGrace
	cmd.Stdin = stdin
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err == nil {
		return out, nil
	}
	op := "wezterm cli " + verb(args)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, terminal.Timeout(op, c.timeout)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		msg := strings.TrimSpace(stripLogPrefix(stderr.String()))
		if isConnectMessage(msg) {
			return nil, terminal.Unavailable(op, errors.New(msg))
		}
		return nil, fmt.Errorf("%s failed: %s", op, msg)
	}
	return nil, terminal.Unavailable(op, err)
}

// logPrefix is the timestamp and level wezterm puts in front of each line it
// logs to stderr, as in "22:12:41.878  ERROR  wezterm > failed to connect".
var logPrefix = regexp.MustCompile(`(?m)^\d{2}:\d{2}:\d{2}\.\d+\s+[A-Z]+\s+`)

// stripLogPrefix removes logPrefix from each line, so the same failure gives
// the same message every time; a caller that logs only when a reason
// changes would otherwise log it on every attempt.
func stripLogPrefix(s string) string {
	return logPrefix.ReplaceAllString(s, "")
}

func verb(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

// isConnectMessage reports whether wezterm's stderr describes a failure to
// reach the mux socket rather than a per-pane error.
func isConnectMessage(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "failed to connect") ||
		strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "no such file or directory") ||
		strings.Contains(lower, "unable to connect")
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
	// WindowTitle, TabTitle, and IsActive are absent from older WezTerm
	// releases and decode empty. TabTitle is empty too unless the tab was
	// given a title of its own.
	WindowTitle string `json:"window_title"`
	TabTitle    string `json:"tab_title"`
	IsActive    bool   `json:"is_active"`
	// LeftCol, TopRow, and Size place the pane within its tab, in cells.
	LeftCol int `json:"left_col"`
	TopRow  int `json:"top_row"`
	Size    struct {
		Rows int `json:"rows"`
		Cols int `json:"cols"`
	} `json:"size"`
}

// parseListOutput parses the flat JSON array from wezterm cli list.
func parseListOutput(data []byte) ([]listEntry, error) {
	var entries []listEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse wezterm cli list: %w", err)
	}
	return entries, nil
}

// entryFrame is the pane's rectangle within its tab, zero when the listing
// gives no size.
func entryFrame(e listEntry) terminal.Frame {
	if e.Size.Cols <= 0 || e.Size.Rows <= 0 {
		return terminal.Frame{}
	}
	return terminal.Frame{Left: e.LeftCol, Top: e.TopRow, Width: e.Size.Cols, Height: e.Size.Rows}
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
	if _, err := exec.LookPath(c.weztermPath); err != nil {
		return fmt.Errorf("wezterm not found in PATH")
	}

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
	return sessionsFromEntries(entries), nil
}

// sessionsFromEntries maps list entries to sessions. Windows and tabs are
// numbered in order of first appearance, which is WezTerm's own order. A tab
// without a title of its own takes its active pane's title, as WezTerm's tab
// bar shows it, when the listing marks a pane active; otherwise its title
// stays empty.
func sessionsFromEntries(entries []listEntry) []terminal.Session {
	activeTitle := map[int]string{} // tab id -> its active pane's title
	for _, e := range entries {
		if e.IsActive {
			activeTitle[e.TabID] = e.Title
		}
	}
	windows := map[int]int{}  // window id -> index
	tabs := map[int]int{}     // tab id -> index within its window
	tabCount := map[int]int{} // window id -> tabs seen
	sessions := make([]terminal.Session, 0, len(entries))
	for _, e := range entries {
		if _, ok := windows[e.WindowID]; !ok {
			windows[e.WindowID] = len(windows)
		}
		if _, ok := tabs[e.TabID]; !ok {
			tabs[e.TabID] = tabCount[e.WindowID]
			tabCount[e.WindowID]++
		}
		tabTitle := e.TabTitle
		if tabTitle == "" {
			tabTitle = activeTitle[e.TabID]
		}
		sessions = append(sessions, terminal.Session{
			ID:     strconv.Itoa(e.PaneID),
			Name:   e.Title,
			TTY:    e.TTYName,
			Window: terminal.Place{ID: strconv.Itoa(e.WindowID), Title: e.WindowTitle, Index: windows[e.WindowID]},
			Tab:    terminal.Place{ID: strconv.Itoa(e.TabID), Title: tabTitle, Index: tabs[e.TabID]},
			Cwd:    normalizeCWD(e.CWD),
			Frame:  entryFrame(e),
		})
	}
	return sessions
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
	_, err := c.runWithStdin(strings.NewReader(text), "send-text", "--pane-id", sessionID, "--no-paste")
	return err
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
	return terminal.TrimScreenTail(string(out), lines), nil
}

// Compile-time check that the WezTerm backend supports styled reads.
var _ terminal.StyledReader = (*Client)(nil)

// ReadScreenStyled captures the visible screen text from a WezTerm pane with
// ANSI color/style escapes preserved (for display only).
func (c *Client) ReadScreenStyled(sessionID string, lines int) (string, error) {
	out, err := c.run("get-text", "--pane-id", sessionID, "--escapes")
	if err != nil {
		return "", err
	}
	return terminal.TrimScreenTail(string(out), lines), nil
}

// ReadScrollback returns the visible screen, as ReadScreen does; this backend
// doesn't read the pane's history.
func (c *Client) ReadScrollback(sessionID string, lines int, styled bool) (string, error) {
	if styled {
		return c.ReadScreenStyled(sessionID, lines)
	}
	return c.ReadScreen(sessionID, lines)
}

func trimToLastN(text string, n int) string {
	return terminal.TrimScreenTail(text, n)
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
