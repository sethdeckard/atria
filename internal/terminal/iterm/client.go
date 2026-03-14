package iterm

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sethdeckard/atria/internal/terminal"
	pb "github.com/sethdeckard/atria/internal/terminal/iterm/proto"
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

// Client implements terminal.Backend using iTerm2's native protobuf-over-WebSocket API.
type Client struct {
	conn       *conn
	socketPath string // override for testing; empty uses default
	noPrompt   bool   // suppress interactive AppleScript auth
}

// NewClient creates a new Client. Optional socketPath overrides the default
// iTerm2 Unix socket location.
func NewClient(socketPath ...string) *Client {
	c := &Client{}
	if len(socketPath) > 0 {
		c.socketPath = socketPath[0]
	}
	return c
}

// SetNoPrompt suppresses interactive AppleScript auth dialogs. When set,
// the client returns an error instead of prompting if auth is required.
func (c *Client) SetNoPrompt(v bool) {
	c.noPrompt = v
}

// ensureConn lazily connects on first use.
func (c *Client) ensureConn() error {
	if c.conn == nil {
		c.conn = &conn{socketPath: c.socketPath, noPrompt: c.noPrompt}
	}
	if c.conn.ws == nil {
		return c.conn.connect()
	}
	return nil
}

// request sends a request and returns the response. No automatic retry —
// safe for all operations including non-idempotent ones.
func (c *Client) request(req *pb.ClientOriginatedMessage) (*pb.ServerOriginatedMessage, error) {
	if err := c.ensureConn(); err != nil {
		return nil, err
	}
	return c.conn.roundTrip(req)
}

// idempotentRequest sends a request, reconnecting once on failure. Only safe
// for idempotent operations (ListSessions, GetBuffer, GetVar, Focus, Activate).
func (c *Client) idempotentRequest(req *pb.ClientOriginatedMessage) (*pb.ServerOriginatedMessage, error) {
	if err := c.ensureConn(); err != nil {
		return nil, err
	}
	resp, err := c.conn.roundTrip(req)
	if err != nil {
		if connErr := c.conn.reconnect(); connErr != nil {
			return nil, connErr
		}
		return c.conn.roundTrip(req)
	}
	return resp, nil
}

// Close closes the underlying WebSocket connection.
func (c *Client) Close() {
	if c.conn != nil {
		c.conn.Close()
	}
}

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
				tty := c.ttyForSession(ss.GetUniqueIdentifier())
				sessions = append(sessions, terminal.Session{
					ID:   ss.GetUniqueIdentifier(),
					Name: unquoteJSON(ss.GetTitle()),
					TTY:  tty,
				})
			}
		}
	}
	return sessions, nil
}

// ttyForSession gets the TTY for a session via the "tty" variable.
func (c *Client) ttyForSession(sessionID string) string {
	resp, err := c.idempotentRequest(&pb.ClientOriginatedMessage{
		Submessage: &pb.ClientOriginatedMessage_VariableRequest{
			VariableRequest: &pb.VariableRequest{
				Scope: &pb.VariableRequest_SessionId{SessionId: sessionID},
				Get:   []string{"tty"},
			},
		},
	})
	if err != nil {
		return ""
	}
	vr := resp.GetVariableResponse()
	if vr == nil || len(vr.GetValues()) == 0 {
		return ""
	}
	return unquoteJSON(vr.GetValues()[0])
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
	resp, err := c.idempotentRequest(&pb.ClientOriginatedMessage{
		Submessage: &pb.ClientOriginatedMessage_ActivateRequest{
			ActivateRequest: &pb.ActivateRequest{
				Identifier:       &pb.ActivateRequest_SessionId{SessionId: sessionID},
				SelectTab:        proto.Bool(true),
				SelectSession:    proto.Bool(true),
				OrderWindowFront: proto.Bool(true),
			},
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

// ReadScreen captures the visible screen contents of a session.
func (c *Client) ReadScreen(sessionID string, lines int) (string, error) {
	resp, err := c.idempotentRequest(&pb.ClientOriginatedMessage{
		Submessage: &pb.ClientOriginatedMessage_GetBufferRequest{
			GetBufferRequest: &pb.GetBufferRequest{
				Session: proto.String(sessionID),
				LineRange: &pb.LineRange{
					ScreenContentsOnly: proto.Bool(true),
				},
			},
		},
	})
	if err != nil {
		return "", err
	}

	gbr := resp.GetGetBufferResponse()
	if gbr == nil {
		return "", fmt.Errorf("unexpected response type")
	}
	if gbr.GetStatus() != pb.GetBufferResponse_OK {
		return "", fmt.Errorf("get buffer failed: %s", gbr.GetStatus().String())
	}

	var allLines []string
	for _, lc := range gbr.GetContents() {
		allLines = append(allLines, lc.GetText())
	}
	if len(allLines) > lines {
		allLines = allLines[len(allLines)-lines:]
	}
	return strings.Join(allLines, "\n"), nil
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
