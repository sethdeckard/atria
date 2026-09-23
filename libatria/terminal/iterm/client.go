package iterm

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/sethdeckard/atria/libatria/terminal"
	pb "github.com/sethdeckard/atria/libatria/terminal/iterm/proto"
	"google.golang.org/protobuf/proto"
)

// unquoteJSON strips JSON string encoding (quotes + escapes) that iTerm2's
// API wraps around variable values and some fields. Returns the original
// string unchanged if it isn't valid JSON.
func unquoteJSON(s string) string {
	if len(s) >= 2 && s[0] == '"' {
		var out string
		if err := json.Unmarshal([]byte(s), &out); err == nil {
			return out
		}
	}
	return s
}

//go:generate protoc --go_out=. --go_opt=paths=source_relative proto/api.proto

// DefaultClientName is the name iTerm2 sees for this client when Options.ClientName is empty.
const DefaultClientName = "libatria"

// Options configures a Client. The zero value connects to the default socket,
// prompts for credentials when iTerm2 asks, and identifies itself as
// DefaultClientName.
type Options struct {
	// SocketPath overrides the iTerm2 API socket; empty uses
	// ~/Library/Application Support/iTerm2/private/socket.
	SocketPath string
	// NoPrompt suppresses the AppleScript credential dialog. With it set, a
	// 401 from iTerm2 is returned as an error. Set it whenever the caller is
	// a TUI or has no user at the keyboard.
	NoPrompt bool
	// ClientName is the application name in the AppleScript credential
	// request and the x-iterm2-advisory-name header. iTerm2 shows it in its
	// automation dialog and remembers the grant under it.
	ClientName string
	// CommandTimeout bounds each round trip on the socket, one deadline
	// covering the write and the read; zero means
	// terminal.DefaultCommandTimeout. A timeout closes the connection and is
	// reported as terminal.ErrUnavailable; the next call reconnects. The
	// initial handshake and the AppleScript credential dialog are not bounded.
	CommandTimeout time.Duration
}

// Client implements terminal.Backend using iTerm2's native protobuf-over-WebSocket API.
type Client struct {
	mu         sync.Mutex // guards conn; the conn serializes its own use
	conn       *conn
	socketPath string // override for testing; empty uses default
	noPrompt   bool   // suppress interactive AppleScript auth
	clientName string
	timeout    time.Duration
}

type lineInfo struct {
	Grid         int64  `json:"grid"`
	History      int64  `json:"history"`
	Overflow     int64  `json:"overflow"`
	FirstVisible *int64 `json:"first_visible"`
}

// NewClient creates a Client. Nothing connects until the first call.
func NewClient(opts Options) *Client {
	name := opts.ClientName
	if name == "" {
		name = DefaultClientName
	}
	return &Client{
		socketPath: opts.SocketPath,
		noPrompt:   opts.NoPrompt,
		clientName: name,
		timeout:    terminal.TimeoutOr(opts.CommandTimeout),
	}
}

// ensureConn returns the shared connection, connecting on demand.
// c.mu guards connection creation; conn.connect checks and opens the
// socket under the connection's own lock.
func (c *Client) ensureConn() (*conn, error) {
	c.mu.Lock()
	if c.conn == nil {
		c.conn = &conn{socketPath: c.socketPath, noPrompt: c.noPrompt, clientName: c.clientName, timeout: c.timeout}
	}
	cn := c.conn
	c.mu.Unlock()
	return cn, cn.connect()
}

// request sends a request and returns the response. No automatic retry —
// safe for all operations including non-idempotent ones.
func (c *Client) request(req *pb.ClientOriginatedMessage) (*pb.ServerOriginatedMessage, error) {
	cn, err := c.ensureConn()
	if err != nil {
		return nil, err
	}
	return cn.roundTrip(req)
}

// idempotentRequest sends a request, reconnecting once on failure. Only safe
// for idempotent operations (ListSessions, GetBuffer, GetVar, Focus, Activate).
func (c *Client) idempotentRequest(req *pb.ClientOriginatedMessage) (*pb.ServerOriginatedMessage, error) {
	cn, err := c.ensureConn()
	if err != nil {
		return nil, err
	}
	resp, err := cn.roundTrip(req)
	if err != nil {
		if connErr := cn.reconnect(); connErr != nil {
			return nil, connErr
		}
		return cn.roundTrip(req)
	}
	return resp, nil
}

// Close closes the WebSocket connection. It always returns nil; the next call
// reconnects on demand.
func (c *Client) Close() error {
	c.mu.Lock()
	cn := c.conn
	c.mu.Unlock()
	if cn != nil {
		cn.Close()
	}
	return nil
}

// Compile-time checks for the interfaces the iTerm2 backend implements.
var (
	_ terminal.Backend = (*Client)(nil)
	_ io.Closer        = (*Client)(nil)
)

// Available checks if iTerm2 is reachable by listing sessions via Unix socket.
func (c *Client) Available() error {
	_, err := c.idempotentRequest(&pb.ClientOriginatedMessage{
		Submessage: &pb.ClientOriginatedMessage_ListSessionsRequest{
			ListSessionsRequest: &pb.ListSessionsRequest{},
		},
	})
	if err != nil {
		return fmt.Errorf("cannot connect to iTerm2: %w", err)
	}
	return nil
}

// collectSessions recursively walks a SplitTreeNode to extract all leaf sessions.
func collectSessions(node *pb.SplitTreeNode) []*pb.SessionSummary {
	if node == nil {
		return nil
	}
	var sessions []*pb.SessionSummary
	for _, link := range node.GetLinks() {
		if s := link.GetSession(); s != nil {
			sessions = append(sessions, s)
		}
		if n := link.GetNode(); n != nil {
			sessions = append(sessions, collectSessions(n)...)
		}
	}
	return sessions
}

// ListSessions returns all iTerm2 sessions (panes), including those in splits.
func (c *Client) ListSessions() ([]terminal.Session, error) {
	resp, err := c.idempotentRequest(&pb.ClientOriginatedMessage{
		Submessage: &pb.ClientOriginatedMessage_ListSessionsRequest{
			ListSessionsRequest: &pb.ListSessionsRequest{},
		},
	})
	if err != nil {
		return nil, err
	}

	lsr := resp.GetListSessionsResponse()
	if lsr == nil {
		return nil, fmt.Errorf("unexpected response type")
	}

	var sessions []terminal.Session
	for _, w := range lsr.GetWindows() {
		for _, tab := range w.GetTabs() {
			for _, ss := range collectSessions(tab.GetRoot()) {
				tty, job := c.sessionTTYAndJob(ss.GetUniqueIdentifier())
				sessions = append(sessions, terminal.Session{
					ID:   ss.GetUniqueIdentifier(),
					Name: unquoteJSON(ss.GetTitle()),
					TTY:  tty,
					Job:  job,
				})
			}
		}
	}
	return sessions, nil
}

// sessionTTYAndJob gets the TTY and foreground job for a session.
func (c *Client) sessionTTYAndJob(sessionID string) (tty, job string) {
	resp, err := c.idempotentRequest(&pb.ClientOriginatedMessage{
		Submessage: &pb.ClientOriginatedMessage_VariableRequest{
			VariableRequest: &pb.VariableRequest{
				Scope: &pb.VariableRequest_SessionId{SessionId: sessionID},
				Get:   []string{"tty", "jobName"},
			},
		},
	})
	if err != nil {
		return "", ""
	}
	vr := resp.GetVariableResponse()
	if vr == nil || vr.GetStatus() != pb.VariableResponse_OK || len(vr.GetValues()) == 0 {
		return "", ""
	}
	tty = unquoteJSON(vr.GetValues()[0])
	if len(vr.GetValues()) > 1 {
		job = unquoteJSON(vr.GetValues()[1])
	}
	return tty, job
}

// extractFocusedWindowID scans FocusResponse notifications for the focused window.
// Returns the window ID and true if found, or "" and false if no focused window.
func extractFocusedWindowID(fr *pb.FocusResponse) (string, bool) {
	if fr == nil {
		return "", false
	}
	for _, notif := range fr.GetNotifications() {
		w := notif.GetWindow()
		if w == nil {
			continue
		}
		status := w.GetWindowStatus()
		if status == pb.FocusChangedNotification_Window_TERMINAL_WINDOW_BECAME_KEY ||
			status == pb.FocusChangedNotification_Window_TERMINAL_WINDOW_IS_CURRENT {
			return w.GetWindowId(), true
		}
	}
	return "", false
}

func lineRangeForVisibleScreen(jsonValue string) (*pb.LineRange, error) {
	var info lineInfo
	if err := json.Unmarshal([]byte(jsonValue), &info); err != nil {
		return nil, fmt.Errorf("parse line info: %w", err)
	}
	if info.Grid <= 0 {
		return nil, fmt.Errorf("invalid grid height %d", info.Grid)
	}
	startY := info.Overflow + info.History
	if info.FirstVisible != nil {
		startY = *info.FirstVisible
	}
	return &pb.LineRange{
		WindowedCoordRange: &pb.WindowedCoordRange{
			CoordRange: &pb.CoordRange{
				Start: &pb.Coord{X: proto.Int32(0), Y: proto.Int64(startY)},
				End:   &pb.Coord{X: proto.Int32(0), Y: proto.Int64(startY + info.Grid)},
			},
		},
	}, nil
}

func normalizeBufferText(s string) string {
	return strings.ReplaceAll(s, "\x00", "")
}

func isSemanticallyBlank(s string) bool {
	return strings.TrimSpace(normalizeBufferText(s)) == ""
}

func joinBufferLines(contents []*pb.LineContents, lines int) string {
	var allLines []string
	for _, lc := range contents {
		allLines = append(allLines, lc.GetText())
	}
	if len(allLines) > lines && lines > 0 {
		end := len(allLines)
		for i := len(allLines) - 1; i >= 0; i-- {
			if !isSemanticallyBlank(allLines[i]) {
				end = i + 1
				break
			}
		}
		start := end - lines
		if start < 0 {
			start = 0
		}
		allLines = allLines[start:end]
	}
	return strings.Join(allLines, "\n")
}

// focusedWindowID queries iTerm2's FocusRequest API to find the currently
// focused (key) window. Falls back to the first window from ListSessions.
func (c *Client) focusedWindowID() (string, error) {
	resp, err := c.idempotentRequest(&pb.ClientOriginatedMessage{
		Submessage: &pb.ClientOriginatedMessage_FocusRequest{
			FocusRequest: &pb.FocusRequest{},
		},
	})
	if err != nil {
		return "", err
	}

	if id, ok := extractFocusedWindowID(resp.GetFocusResponse()); ok {
		return id, nil
	}

	// Fallback: use the first window from ListSessions.
	lsResp, err := c.idempotentRequest(&pb.ClientOriginatedMessage{
		Submessage: &pb.ClientOriginatedMessage_ListSessionsRequest{
			ListSessionsRequest: &pb.ListSessionsRequest{},
		},
	})
	if err != nil {
		return "", err
	}
	lsr := lsResp.GetListSessionsResponse()
	if lsr == nil || len(lsr.GetWindows()) == 0 {
		return "", fmt.Errorf("no iTerm2 windows found")
	}
	return lsr.GetWindows()[0].GetWindowId(), nil
}

// NewSession creates a new tab in the focused iTerm2 window and returns its session ID.
func (c *Client) NewSession() (string, error) {
	windowID, err := c.focusedWindowID()
	if err != nil {
		return "", fmt.Errorf("cannot find focused window: %w", err)
	}

	resp, err := c.request(&pb.ClientOriginatedMessage{
		Submessage: &pb.ClientOriginatedMessage_CreateTabRequest{
			CreateTabRequest: &pb.CreateTabRequest{
				WindowId: proto.String(windowID),
			},
		},
	})
	if err != nil {
		return "", err
	}

	ctr := resp.GetCreateTabResponse()
	if ctr == nil {
		return "", fmt.Errorf("unexpected response type")
	}
	if ctr.GetStatus() != pb.CreateTabResponse_OK {
		return "", fmt.Errorf("create tab failed: %s", ctr.GetStatus().String())
	}
	return ctr.GetSessionId(), nil
}

// SendText sends raw text to a session.
func (c *Client) SendText(sessionID, text string) error {
	resp, err := c.request(&pb.ClientOriginatedMessage{
		Submessage: &pb.ClientOriginatedMessage_SendTextRequest{
			SendTextRequest: &pb.SendTextRequest{
				Session: proto.String(sessionID),
				Text:    proto.String(text),
			},
		},
	})
	if err != nil {
		return err
	}
	str := resp.GetSendTextResponse()
	if str != nil && str.GetStatus() != pb.SendTextResponse_OK {
		return fmt.Errorf("send text failed: %s", str.GetStatus().String())
	}
	return nil
}

// RunCommand sends a command followed by Enter to a session.
func (c *Client) RunCommand(sessionID, cmd string) error {
	return c.SendText(sessionID, cmd+"\n")
}

// FocusSession activates the session, its tab, and brings the window to front.
// Uses idempotentRequest since activating is safe to retry.
func (c *Client) FocusSession(sessionID string) error {
	return c.activate(&pb.ActivateRequest{
		Identifier:       &pb.ActivateRequest_SessionId{SessionId: sessionID},
		SelectTab:        proto.Bool(true),
		SelectSession:    proto.Bool(true),
		OrderWindowFront: proto.Bool(true),
	})
}

func (c *Client) activate(req *pb.ActivateRequest) error {
	resp, err := c.idempotentRequest(&pb.ClientOriginatedMessage{
		Submessage: &pb.ClientOriginatedMessage_ActivateRequest{
			ActivateRequest: req,
		},
	})
	if err != nil {
		return err
	}
	ar := resp.GetActivateResponse()
	if ar != nil && ar.GetStatus() != pb.ActivateResponse_OK {
		return fmt.Errorf("focus session failed: %s", ar.GetStatus().String())
	}
	return nil
}

func (c *Client) getProperty(sessionID, name string) (string, error) {
	resp, err := c.idempotentRequest(&pb.ClientOriginatedMessage{
		Submessage: &pb.ClientOriginatedMessage_GetPropertyRequest{
			GetPropertyRequest: &pb.GetPropertyRequest{
				Identifier: &pb.GetPropertyRequest_SessionId{SessionId: sessionID},
				Name:       proto.String(name),
			},
		},
	})
	if err != nil {
		return "", err
	}
	gpr := resp.GetGetPropertyResponse()
	if gpr == nil {
		return "", fmt.Errorf("unexpected response type")
	}
	if gpr.GetStatus() != pb.GetPropertyResponse_OK {
		return "", fmt.Errorf("get property failed: %s", gpr.GetStatus().String())
	}
	return gpr.GetJsonValue(), nil
}

func (c *Client) getBufferResponse(sessionID string, lr *pb.LineRange) (*pb.GetBufferResponse, error) {
	resp, err := c.idempotentRequest(&pb.ClientOriginatedMessage{
		Submessage: &pb.ClientOriginatedMessage_GetBufferRequest{
			GetBufferRequest: &pb.GetBufferRequest{
				Session:       proto.String(sessionID),
				LineRange:     lr,
				IncludeStyles: proto.Bool(true),
			},
		},
	})
	if err != nil {
		return nil, err
	}

	gbr := resp.GetGetBufferResponse()
	if gbr == nil {
		return nil, fmt.Errorf("unexpected response type")
	}
	if gbr.GetStatus() != pb.GetBufferResponse_OK {
		return nil, fmt.Errorf("get buffer failed: %s", gbr.GetStatus().String())
	}
	return gbr, nil
}

func (c *Client) getBuffer(sessionID string, lr *pb.LineRange, lines int) (string, error) {
	gbr, err := c.getBufferResponse(sessionID, lr)
	if err != nil {
		return "", err
	}
	return joinBufferLines(gbr.GetContents(), lines), nil
}

// ReadScreen captures the visible screen contents of a session.
func (c *Client) ReadScreen(sessionID string, lines int) (string, error) {
	content, err := c.getBuffer(sessionID, &pb.LineRange{
		ScreenContentsOnly: proto.Bool(true),
	}, lines)
	if err != nil {
		return "", err
	}
	if !isSemanticallyBlank(content) {
		return content, nil
	}

	lineInfoJSON, err := c.getProperty(sessionID, "number_of_lines")
	if err != nil {
		return content, nil
	}
	lineRange, err := lineRangeForVisibleScreen(lineInfoJSON)
	if err != nil {
		return content, nil
	}
	visibleContent, err := c.getBuffer(sessionID, lineRange, lines)
	if err != nil {
		return content, nil
	}
	if !isSemanticallyBlank(visibleContent) {
		return visibleContent, nil
	}
	return visibleContent, nil
}

// GetVar reads a variable from a session.
func (c *Client) GetVar(sessionID, varName string) (string, error) {
	resp, err := c.idempotentRequest(&pb.ClientOriginatedMessage{
		Submessage: &pb.ClientOriginatedMessage_VariableRequest{
			VariableRequest: &pb.VariableRequest{
				Scope: &pb.VariableRequest_SessionId{SessionId: sessionID},
				Get:   []string{varName},
			},
		},
	})
	if err != nil {
		return "", err
	}
	vr := resp.GetVariableResponse()
	if vr == nil {
		return "", fmt.Errorf("unexpected response type")
	}
	if vr.GetStatus() != pb.VariableResponse_OK {
		return "", fmt.Errorf("get var failed: %s", vr.GetStatus().String())
	}
	if len(vr.GetValues()) == 0 {
		return "", nil
	}
	return unquoteJSON(vr.GetValues()[0]), nil
}

// MonitorOutput is not supported by the iTerm2 backend. Screen reads are the
// primary status detection mechanism.
func (c *Client) MonitorOutput(sessionID, logPath, patterns string) (int, error) {
	return 0, fmt.Errorf("iTerm2 backend does not support output monitoring")
}
