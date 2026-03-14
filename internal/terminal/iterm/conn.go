package iterm

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
	pb "github.com/sethdeckard/atria/internal/terminal/iterm/proto"
	"google.golang.org/protobuf/proto"
)

// conn manages a persistent WebSocket connection to iTerm2's Unix socket.
type conn struct {
	ws         *websocket.Conn
	mu         sync.Mutex // serialize writes + reads (request-response pairs)
	nextID     atomic.Int64
	socketPath string
	noPrompt   bool // suppress interactive AppleScript auth
	cookie     string
	key        string
}

// defaultSocketPath returns the standard iTerm2 API socket path.
func defaultSocketPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "Application Support", "iTerm2", "private", "socket")
}

// requestCookieAndKey requests a cookie and key from iTerm2 via AppleScript.
// Returns cookie, key, or an error.
func requestCookieAndKey() (string, string, error) {
	cmd := exec.Command("/usr/bin/osascript", "-")
	cmd.Stdin = strings.NewReader(
		`tell application "iTerm2" to request cookie and key for app named "atria"`)
	out, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("AppleScript auth failed: %w", err)
	}
	parts := strings.Fields(strings.TrimSpace(string(out)))
	if len(parts) != 2 {
		return "", "", fmt.Errorf("unexpected auth response: %s", string(out))
	}
	return parts[0], parts[1], nil
}

// captureAuthFromEnv reads any pre-seeded iTerm2 auth from the current
// environment and removes it so PTY children cannot inherit the credentials.
func (c *conn) captureAuthFromEnv() {
	if c.cookie == "" {
		c.cookie = os.Getenv("ITERM2_COOKIE")
	}
	if c.key == "" {
		c.key = os.Getenv("ITERM2_KEY")
	}
	if c.cookie != "" || c.key != "" {
		os.Unsetenv("ITERM2_COOKIE")
		os.Unsetenv("ITERM2_KEY")
	}
}

func (c *conn) buildHeaders() http.Header {
	headers := http.Header{
		"Origin":                   {"ws://localhost/"},
		"x-iterm2-library-version": {"go-atria 1.0"},
		"x-iterm2-advisory-name":   {"atria"},
		"x-iterm2-disable-auth-ui": {"true"},
	}
	if c.cookie != "" {
		headers.Set("x-iterm2-cookie", c.cookie)
	}
	if c.key != "" {
		headers.Set("x-iterm2-key", c.key)
	}
	return headers
}

func (c *conn) dial(sockPath string, headers http.Header) (*websocket.Conn, *http.Response, error) {
	dialer := websocket.Dialer{
		NetDial: func(_, _ string) (net.Conn, error) {
			return net.Dial("unix", sockPath)
		},
		Subprotocols: []string{"api.iterm2.com"},
	}
	return dialer.Dial("ws://localhost/", headers)
}

// connect dials the Unix socket and performs the WebSocket handshake.
// Tries without auth first. If the handshake returns 401, requests
// credentials via AppleScript and retries — no AppleScript runs unless
// iTerm2 is reachable and actually requires authentication.
func (c *conn) connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ws != nil {
		return nil
	}

	sockPath := c.socketPath
	if sockPath == "" {
		sockPath = defaultSocketPath()
	}

	if _, err := os.Stat(sockPath); err != nil {
		return fmt.Errorf("iTerm2 socket not found — is iTerm2 running?")
	}

	c.captureAuthFromEnv()
	headers := c.buildHeaders()

	// First attempt: use existing credentials (or none).
	ws, resp, err := c.dial(sockPath, headers)
	if err == nil {
		c.ws = ws
		return nil
	}

	// If not a 401, no point trying auth.
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		return fmt.Errorf("WebSocket dial failed: %w", err)
	}

	// 401: auth required. Skip AppleScript when noPrompt is set (e.g.,
	// settings toggle during TUI) to avoid system dialogs over alt screen.
	if c.noPrompt {
		return fmt.Errorf("iTerm2 requires authorization — restart Atria inside iTerm2 to authorize")
	}

	// Clear stale credentials and request fresh ones via AppleScript.
	c.cookie = ""
	c.key = ""
	cookie, key, authErr := requestCookieAndKey()
	if authErr != nil {
		return fmt.Errorf("iTerm2 auth: %w", authErr)
	}
	c.cookie = cookie
	c.key = key

	// Retry with fresh credentials.
	headers = c.buildHeaders()
	ws, _, err = c.dial(sockPath, headers)
	if err != nil {
		c.cookie = ""
		c.key = ""
		return fmt.Errorf("WebSocket dial failed after auth: %w", err)
	}

	c.ws = ws
	return nil
}

// roundTrip sends a request and reads the matching response.
func (c *conn) roundTrip(req *pb.ClientOriginatedMessage) (*pb.ServerOriginatedMessage, error) {
	id := c.nextID.Add(1)
	req.Id = proto.Int64(id)

	data, err := proto.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ws == nil {
		return nil, fmt.Errorf("not connected")
	}

	if err := c.ws.WriteMessage(websocket.BinaryMessage, data); err != nil {
		c.close()
		return nil, fmt.Errorf("write: %w", err)
	}

	_, respData, err := c.ws.ReadMessage()
	if err != nil {
		c.close()
		return nil, fmt.Errorf("read: %w", err)
	}

	resp := &pb.ServerOriginatedMessage{}
	if err := proto.Unmarshal(respData, resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if resp.GetId() != id {
		return nil, fmt.Errorf("response ID mismatch: got %d, want %d", resp.GetId(), id)
	}

	if errMsg := resp.GetError(); errMsg != "" {
		return nil, fmt.Errorf("iTerm2 error: %s", errMsg)
	}

	return resp, nil
}

// close closes the WebSocket without locking (caller must hold mu).
func (c *conn) close() {
	if c.ws != nil {
		c.ws.Close()
		c.ws = nil
	}
}

// Close closes the WebSocket connection.
func (c *conn) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.close()
}

// reconnect closes the existing connection and re-establishes it.
// Preserves existing credentials for the first attempt so that a transient
// socket drop doesn't discard valid auth (important for noPrompt mode).
func (c *conn) reconnect() error {
	c.Close()
	return c.connect()
}
