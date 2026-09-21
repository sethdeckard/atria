package deviceterm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sethdeckard/atria/libatria/terminal"
)

// Environment variables DeviceTerm sets in every terminal session.
const (
	envSession = "DEVICETERM_SESSION"  // caller's session UUID (one per split)
	envShimDir = "DEVICETERM_SHIM_DIR" // per-session bin dir holding the deviceterm CLI
)

// Reasons shown in settings when the backend is enabled but not active.
// They render verbatim via backendStatusLabel, so keep them short.
const (
	automationTabReason = "open an Automation tab (Shell ▸ Open Automation Tab, ⇧⌘T) and run atria there"
	unrecognizedReason  = "not recognized as a DeviceTerm session; run atria directly in an Automation tab, not under tmux"
	tooOldReason        = "requires DeviceTerm 0.11.0 or later"
)

// Client implements terminal.Backend using the deviceterm CLI.
//
// Atria must run inside a DeviceTerm Automation tab: the daemon authenticates
// each CLI call by walking its parent chain back to that tab, so every call is
// spawned as a direct child process and the environment is inherited intact.
type Client struct {
	devicetermPath string
	selfSession    string // $DEVICETERM_SESSION, read in Available

	// runFn replaces the subprocess call in tests. Nil means exec.
	runFn func(args ...string) ([]byte, error)

	mu  sync.Mutex
	cwd map[string]string // pane id -> terminal.cwd from the last ListSessions
}

// NewClient creates a new DeviceTerm Client. Empty devicetermPath defaults to "deviceterm".
func NewClient(devicetermPath string) *Client {
	if devicetermPath == "" {
		devicetermPath = "deviceterm"
	}
	return &Client{devicetermPath: devicetermPath, cwd: map[string]string{}}
}

// CLIError is a typed failure decoded from the CLI's JSON error envelope.
// Callers branch on Code (stable dotted identifiers such as
// "session.unauthorized" or "transport.unavailable"), never on Message.
type CLIError struct {
	Code    string
	Message string
}

func (e *CLIError) Error() string {
	if e.Message == "" {
		return "deviceterm: " + e.Code
	}
	return fmt.Sprintf("deviceterm: %s (%s)", e.Message, e.Code)
}

// parseErrorEnvelope decodes {"error":{"code","message"}} from CLI stdout.
// ok is false when stdout is not a typed envelope.
func parseErrorEnvelope(data []byte) (code, message string, ok bool) {
	var env struct {
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(data), &env); err != nil || env.Error == nil || env.Error.Code == "" {
		return "", "", false
	}
	return env.Error.Code, env.Error.Message, true
}

// isUngranted reports whether an error code means the caller lacks a live
// automation grant. The grant-gated verbs refuse with session.unauthorized;
// intent.automationRequired is accepted defensively for owner-scoped verbs.
func isUngranted(code string) bool {
	return code == "session.unauthorized" || code == "intent.automationRequired"
}

// run executes deviceterm with the given arguments and returns stdout. On a
// non-zero exit it returns a *CLIError when stdout carries a typed envelope,
// otherwise an error built from stderr.
func (c *Client) run(args ...string) ([]byte, error) {
	if c.runFn != nil {
		return c.runFn(args...)
	}
	cmd := exec.Command(c.devicetermPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if code, msg, ok := parseErrorEnvelope(stdout.Bytes()); ok {
			return nil, &CLIError{Code: code, Message: msg}
		}
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("deviceterm %s failed: %s", verb(args), detail)
	}
	return stdout.Bytes(), nil
}

// verb returns up to two leading non-flag words of an argument list for
// error text.
func verb(args []string) string {
	words := make([]string, 0, 2)
	for _, a := range args {
		if strings.HasPrefix(a, "-") || len(words) == 2 {
			break
		}
		words = append(words, a)
	}
	return strings.Join(words, " ")
}

// sessionReport is `session show --json`: the caller's own identity and
// authority. id and role are omitted for a caller outside a DeviceTerm tab.
type sessionReport struct {
	ID              string `json:"id"`
	Role            string `json:"role"`
	AutomationGrant bool   `json:"automationGrant"`
}

func parseSessionReport(data []byte) (sessionReport, error) {
	var r sessionReport
	if err := json.Unmarshal(data, &r); err != nil {
		return sessionReport{}, fmt.Errorf("parse deviceterm session show: %w", err)
	}
	return r, nil
}

// grantState classifies a `session show` outcome into the reason the backend
// is not active, or "" when the caller holds a live automation grant.
//
// The four documented states are: granted; ungranted but inside a tab;
// outside any DeviceTerm tab (or with the process ancestry broken, as under
// tmux), where id is omitted; and daemon unreachable, a typed error. A CLI
// older than 0.11.0 has no session verb and fails with a usage error.
func grantState(report sessionReport, err error) string {
	if err != nil {
		var ce *CLIError
		if errors.As(err, &ce) {
			switch {
			case ce.Code == "cli.invalidUsage":
				return tooOldReason
			case isUngranted(ce.Code):
				return automationTabReason
			}
			return "DeviceTerm unreachable: " + ce.Code
		}
		return err.Error()
	}
	if report.AutomationGrant {
		return ""
	}
	if report.ID == "" {
		return unrecognizedReason
	}
	return automationTabReason
}

// checkGrant asks the daemon whether this session holds an automation grant.
// The grant is the authority: DEVICETERM_SESSION_ROLE is metadata and is not
// consulted. A non-nil error carries the settings reason.
func (c *Client) checkGrant() error {
	out, err := c.run("session", "show", "--json")
	var report sessionReport
	if err == nil {
		report, err = parseSessionReport(out)
	}
	if reason := grantState(report, err); reason != "" {
		return errors.New(reason)
	}
	return nil
}

// Available checks that the deviceterm CLI is present, that Atria runs inside
// DeviceTerm, and that the caller holds an automation grant. Atria requires
// DeviceTerm 0.11.0; there is no version check because an older CLI has no
// session verb, so the grant check fails and the backend stays inactive.
func (c *Client) Available() error {
	path, err := exec.LookPath(c.devicetermPath)
	if err != nil {
		if dir := os.Getenv(envShimDir); dir != "" {
			if p, shimErr := exec.LookPath(filepath.Join(dir, "deviceterm")); shimErr == nil {
				path, err = p, nil
			}
		}
	}
	if err != nil {
		return fmt.Errorf("deviceterm not found in PATH")
	}
	c.devicetermPath = path

	session := os.Getenv(envSession)
	if session == "" {
		return fmt.Errorf("not running inside DeviceTerm")
	}
	c.selfSession = session

	return c.checkGrant()
}

// workspacePane is the subset of a WorkspacePane row Atria reads. Exactly one
// of the kind-specific objects is present; only terminal panes are sessions.
// Row context fields (tabId, tabTitle, windowId, focused, …) are ignored.
type workspacePane struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Kind     string        `json:"kind"`
	Terminal *paneTerminal `json:"terminal"`
}

// paneTerminal is the terminal object on a pane row. title is the pane's own
// live label (OSC title, then user-assigned name, then cwd basename). tty is
// absent only transiently, before the shell attaches. cwd is grant-gated and
// omitted when DeviceTerm cannot resolve it.
type paneTerminal struct {
	SessionID string `json:"sessionId"`
	Title     string `json:"title"`
	TTY       string `json:"tty"`
	CWD       string `json:"cwd"`
}

// parsePaneList parses `pane list --all --json`, a flat array of pane rows
// across every visible window.
func parsePaneList(data []byte) ([]workspacePane, error) {
	var panes []workspacePane
	if err := json.Unmarshal(data, &panes); err != nil {
		return nil, fmt.Errorf("parse deviceterm pane list: %w", err)
	}
	return panes, nil
}

// sessionsFromPanes maps terminal panes to sessions and collects their
// working directories. Each pane carries its own title and tty, so the
// composite's TTY-based self filter and dedup apply without help from here.
func sessionsFromPanes(panes []workspacePane) ([]terminal.Session, map[string]string) {
	var sessions []terminal.Session
	cwd := make(map[string]string)
	for _, p := range panes {
		if p.Kind != "terminal" || p.Terminal == nil || p.ID == "" {
			continue
		}
		sessions = append(sessions, terminal.Session{
			ID:   p.ID,
			Name: p.Terminal.Title,
			TTY:  p.Terminal.TTY,
		})
		cwd[p.ID] = p.Terminal.CWD
	}
	return sessions, cwd
}

// ListSessions returns every terminal pane across all visible DeviceTerm
// windows as a session: two subprocesses per refresh.
//
// The grant is revalidated first because pane list is not grant-gated;
// without this a revoked grant would keep sessions listed while every read,
// send, focus, and launch failed. A lost grant fails the refresh with the
// settings reason, which preserves tracked sessions rather than dropping
// them. The grant belongs to the Automation tab and only the GUI issues one,
// so recovery means restarting Atria in a new Automation tab.
//
// Any failure fails the whole refresh: a partial list would make Atria drop
// the missing sessions as dead.
func (c *Client) ListSessions() ([]terminal.Session, error) {
	if c.selfSession != "" {
		if err := c.checkGrant(); err != nil {
			return nil, err
		}
	}
	out, err := c.run("pane", "list", "--all", "--json")
	if err != nil {
		return nil, err
	}
	panes, err := parsePaneList(out)
	if err != nil {
		return nil, err
	}
	sessions, cwd := sessionsFromPanes(panes)

	c.mu.Lock()
	c.cwd = cwd
	c.mu.Unlock()
	return sessions, nil
}

// mutationReceipt is the subset of a WorkspaceMutationReceipt Atria reads.
type mutationReceipt struct {
	Pane *workspacePane `json:"pane"`
}

// parseMutationReceipt returns the committed pane id from a `tab open --json`
// receipt. The receipt waits for session creation, so the id is live.
func parseMutationReceipt(data []byte) (string, error) {
	if code, msg, ok := parseErrorEnvelope(data); ok {
		return "", &CLIError{Code: code, Message: msg}
	}
	var r mutationReceipt
	if err := json.Unmarshal(data, &r); err != nil {
		return "", fmt.Errorf("parse deviceterm tab open: %w", err)
	}
	if r.Pane == nil || r.Pane.ID == "" {
		return "", fmt.Errorf("deviceterm tab open returned no pane")
	}
	return r.Pane.ID, nil
}

// NewSession opens a new DeviceTerm tab and returns its terminal pane id.
// It never returns an empty id: launching would otherwise target the
// caller's own pane.
func (c *Client) NewSession() (string, error) {
	out, err := c.run("tab", "open", "--json")
	if err != nil {
		return "", err
	}
	return parseMutationReceipt(out)
}

// SendText sends text to a DeviceTerm pane exactly as written. --raw skips
// the CLI's C-escape decoding, so backslashes are literal and a trailing
// "\r" byte still submits. Flags sit before "--", which guards dash-leading
// text; the text is one argv element.
func (c *Client) SendText(sessionID, text string) error {
	_, err := c.run("pane", "send-input", "--json", "--raw", sessionID, "--", text)
	return err
}

// RunCommand sends a command string followed by Enter to a DeviceTerm pane.
func (c *Client) RunCommand(sessionID, cmd string) error {
	if err := c.SendText(sessionID, cmd); err != nil {
		return err
	}
	return c.SendText(sessionID, "\r")
}

// FocusSession brings the pane's window, tab, and pane to the foreground.
func (c *Client) FocusSession(sessionID string) error {
	_, err := c.run("pane", "focus", sessionID)
	return err
}

// captureResult is `pane capture-text --json`.
type captureResult struct {
	Text string `json:"text"`
}

func parseCapture(data []byte) (string, error) {
	var r captureResult
	if err := json.Unmarshal(data, &r); err != nil {
		return "", fmt.Errorf("parse deviceterm capture-text: %w", err)
	}
	return r.Text, nil
}

func (c *Client) capture(sessionID string, ansi bool) (string, error) {
	args := []string{"pane", "capture-text", sessionID, "--json"}
	if ansi {
		args = append(args, "--ansi")
	}
	out, err := c.run(args...)
	if err != nil {
		return "", err
	}
	return parseCapture(out)
}

// ReadScreen captures the visible viewport of a DeviceTerm pane. The capture
// has no scrollback, so lines only trims the tail.
func (c *Client) ReadScreen(sessionID string, lines int) (string, error) {
	text, err := c.capture(sessionID, false)
	if err != nil {
		return "", err
	}
	return terminal.TrimScreenTail(text, lines), nil
}

// Compile-time checks that the DeviceTerm backend satisfies the interfaces.
var (
	_ terminal.Backend      = (*Client)(nil)
	_ terminal.StyledReader = (*Client)(nil)
)

// ReadScreenStyled captures the visible viewport with SGR color and style
// escapes preserved (for display only).
func (c *Client) ReadScreenStyled(sessionID string, lines int) (string, error) {
	text, err := c.capture(sessionID, true)
	if err != nil {
		return "", err
	}
	return terminal.TrimScreenTail(text, lines), nil
}

// GetVar reads a variable for a DeviceTerm pane. Supported: "path", served
// from the working directory captured by the last ListSessions. It is empty
// when DeviceTerm could not resolve the directory, which makes CWD discovery
// fall through to the TTY and name-matching strategies.
func (c *Client) GetVar(sessionID, varName string) (string, error) {
	if varName != "path" {
		return "", fmt.Errorf("unsupported variable: %s", varName)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cwd[sessionID], nil
}

// MonitorOutput is not supported by the DeviceTerm backend. Screen reads are
// the primary status detection mechanism.
func (c *Client) MonitorOutput(sessionID, logPath, patterns string) (int, error) {
	return 0, fmt.Errorf("deviceterm backend does not support output monitoring")
}
