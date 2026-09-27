package iterm

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestCaptureAuthFromEnv(t *testing.T) {
	t.Setenv("ITERM2_COOKIE", "cookie-123")
	t.Setenv("ITERM2_KEY", "key-456")

	c := &conn{}
	c.captureAuthFromEnv()

	if c.cookie != "cookie-123" {
		t.Fatalf("expected cookie captured, got %q", c.cookie)
	}
	if c.key != "key-456" {
		t.Fatalf("expected key captured, got %q", c.key)
	}
	if got := c.buildHeaders().Get("x-iterm2-cookie"); got != "cookie-123" {
		t.Fatalf("expected header cookie, got %q", got)
	}
	if got := c.buildHeaders().Get("x-iterm2-key"); got != "key-456" {
		t.Fatalf("expected header key, got %q", got)
	}
	if got := os.Getenv("ITERM2_COOKIE"); got != "" {
		t.Fatalf("expected cookie removed from env, got %q", got)
	}
	if got := os.Getenv("ITERM2_KEY"); got != "" {
		t.Fatalf("expected key removed from env, got %q", got)
	}
}

// authStub records calls to a conn's requestAuth and answers with the given
// credentials or error.
type authStub struct {
	cookie, key string
	err         error
	calls       []string
}

func (a *authStub) request(appName string) (string, string, error) {
	a.calls = append(a.calls, appName)
	return a.cookie, a.key, a.err
}

// newAuthClient builds a client for sock whose AppleScript request is stub.
// The iTerm2 credential variables are cleared so the host can't leak in.
func newAuthClient(t *testing.T, sock string, noPrompt bool, stub *authStub) *Client {
	t.Helper()
	t.Setenv("ITERM2_COOKIE", "")
	t.Setenv("ITERM2_KEY", "")
	c := NewClient(Options{SocketPath: sock, NoPrompt: noPrompt, ClientName: "dash"})
	c.requestAuth = stub.request
	t.Cleanup(func() { _ = c.Close() }) // srv.Close doesn't reach hijacked WebSocket connections
	return c
}

func TestAuthRequiredWithoutPrompt(t *testing.T) {
	sock, conns := startFakeITermAuth(t, "good-cookie")
	stub := &authStub{cookie: "good-cookie", key: "k"}
	c := newAuthClient(t, sock, true, stub)

	err := c.Available()
	if !errors.Is(err, ErrAuthRequired) {
		t.Fatalf("Available() = %v, want ErrAuthRequired", err)
	}
	for _, want := range []string{`"dash"`, "allow dash to prompt", "disable-automation-auth"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "Atria") {
		t.Errorf("error %q names Atria", err)
	}
	if len(stub.calls) != 0 {
		t.Errorf("requestAuth called %d times with NoPrompt", len(stub.calls))
	}
	if n := conns.Load(); n != 0 {
		t.Errorf("accepted %d connections, want 0", n)
	}
}

func TestAuthPromptRetriesWithCredentials(t *testing.T) {
	sock, conns := startFakeITermAuth(t, "good-cookie")
	stub := &authStub{cookie: "good-cookie", key: "good-key"}
	c := newAuthClient(t, sock, false, stub)

	if err := c.Available(); err != nil {
		t.Fatalf("Available() = %v", err)
	}
	if len(stub.calls) != 1 || stub.calls[0] != "dash" {
		t.Fatalf("requestAuth calls = %q, want [dash]", stub.calls)
	}
	if n := conns.Load(); n != 1 {
		t.Fatalf("accepted %d connections, want 1", n)
	}
	h := c.conn.buildHeaders()
	if h.Get("x-iterm2-cookie") != "good-cookie" || h.Get("x-iterm2-key") != "good-key" {
		t.Fatalf("credentials not kept: cookie=%q key=%q", h.Get("x-iterm2-cookie"), h.Get("x-iterm2-key"))
	}
}

func TestAuthPromptFailureClearsCredentials(t *testing.T) {
	sock, _ := startFakeITermAuth(t, "good-cookie")

	t.Run("request fails", func(t *testing.T) {
		stub := &authStub{err: errors.New("user declined")}
		c := newAuthClient(t, sock, false, stub)
		err := c.Available()
		if err == nil || !strings.Contains(err.Error(), "iTerm2 auth: user declined") {
			t.Fatalf("Available() = %v, want wrapped auth error", err)
		}
		if errors.Is(err, ErrAuthRequired) {
			t.Errorf("a failed request is not ErrAuthRequired: %v", err)
		}
		if c.conn.cookie != "" || c.conn.key != "" {
			t.Errorf("credentials kept after failure: %q %q", c.conn.cookie, c.conn.key)
		}
	})

	t.Run("credentials refused", func(t *testing.T) {
		stub := &authStub{cookie: "stale", key: "stale"}
		c := newAuthClient(t, sock, false, stub)
		if err := c.Available(); err == nil {
			t.Fatal("Available() succeeded with refused credentials")
		}
		if len(stub.calls) != 1 {
			t.Errorf("requestAuth calls = %d, want 1", len(stub.calls))
		}
		if c.conn.cookie != "" || c.conn.key != "" {
			t.Errorf("credentials kept after refusal: %q %q", c.conn.cookie, c.conn.key)
		}
	})
}
