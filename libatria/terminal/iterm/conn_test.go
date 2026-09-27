package iterm

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
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
// credentials or error. With block set it waits for the deadline instead,
// like a dialog nobody answers. With release set it signals entered and
// waits for release before answering, like a dialog someone answers later.
type authStub struct {
	cookie, key string
	err         error
	block       bool
	entered     chan struct{}
	release     chan struct{}
	mu          sync.Mutex
	calls       []string
}

func (a *authStub) request(ctx context.Context, appName string) (string, string, error) {
	a.mu.Lock()
	a.calls = append(a.calls, appName)
	a.mu.Unlock()
	if a.block {
		<-ctx.Done()
		return "", "", ctx.Err()
	}
	if a.release != nil {
		close(a.entered)
		select {
		case <-a.release:
		case <-ctx.Done():
			return "", "", ctx.Err()
		}
	}
	return a.cookie, a.key, a.err
}

func (a *authStub) callCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.calls)
}

// newAuthClient builds a client for sock from opts, named "dash", whose
// AppleScript request is stub. The iTerm2 credential variables are cleared
// so the host can't leak in.
func newAuthClient(t *testing.T, sock string, opts Options, stub *authStub) *Client {
	t.Helper()
	t.Setenv("ITERM2_COOKIE", "")
	t.Setenv("ITERM2_KEY", "")
	opts.SocketPath = sock
	opts.ClientName = "dash"
	c := NewClient(opts)
	c.requestAuth = stub.request
	t.Cleanup(func() { _ = c.Close() }) // srv.Close doesn't reach hijacked WebSocket connections
	return c
}

func TestAuthRequiredWithoutPrompt(t *testing.T) {
	sock, conns := startFakeITermAuth(t, "good-cookie")
	stub := &authStub{cookie: "good-cookie", key: "k"}
	c := newAuthClient(t, sock, Options{NoPrompt: true}, stub)

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
	if n := stub.callCount(); n != 0 {
		t.Errorf("requestAuth called %d times with NoPrompt", n)
	}
	if n := conns.Load(); n != 0 {
		t.Errorf("accepted %d connections, want 0", n)
	}
}

func TestAuthPromptRetriesWithCredentials(t *testing.T) {
	sock, conns := startFakeITermAuth(t, "good-cookie")
	stub := &authStub{cookie: "good-cookie", key: "good-key"}
	c := newAuthClient(t, sock, Options{}, stub)

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
	if !c.authGate.Armed() {
		t.Error("a successful request disarmed the gate")
	}
}

// A failed request, whatever the cause, disarms the gate: the next attempt,
// including a reconnect, fails with ErrAuthFailed and no second dialog.
func TestAuthFailureDisarms(t *testing.T) {
	sock, _ := startFakeITermAuth(t, "good-cookie")
	tests := []struct {
		name string
		stub *authStub
		opts Options
		want string
	}{
		{"declined", &authStub{err: errors.New("user declined")}, Options{}, "user declined"},
		{"timed out", &authStub{block: true}, Options{AuthTimeout: 50 * time.Millisecond}, "no answer within 50ms"},
		{"credentials refused", &authStub{cookie: "stale", key: "stale"}, Options{}, "refused the credentials"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newAuthClient(t, sock, tt.opts, tt.stub)
			err := c.Available()
			if !errors.Is(err, ErrAuthFailed) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Available() = %v, want ErrAuthFailed with %q", err, tt.want)
			}
			if !strings.Contains(err.Error(), `"dash"`) {
				t.Errorf("error %q doesn't name the client", err)
			}
			if c.conn.cookie != "" || c.conn.key != "" {
				t.Errorf("credentials kept after failure: %q %q", c.conn.cookie, c.conn.key)
			}
			if c.authGate.Armed() {
				t.Fatal("gate still armed after a failed request")
			}
			if gerr := c.authGate.Err(); !errors.Is(gerr, ErrAuthFailed) || !strings.Contains(gerr.Error(), tt.want) {
				t.Errorf("gate Err() = %v, want the failure with %q", gerr, tt.want)
			}

			// Exercise reconnect after the failed credential request.
			if err := c.conn.reconnect(); !errors.Is(err, ErrAuthFailed) || !strings.Contains(err.Error(), "re-armed") {
				t.Fatalf("reconnect() = %v, want ErrAuthFailed naming the re-arm", err)
			}
			if n := tt.stub.callCount(); n != 1 {
				t.Fatalf("requestAuth calls = %d, want 1", n)
			}
		})
	}
}

func TestRearmPromptAllowsOneMoreRequest(t *testing.T) {
	sock, _ := startFakeITermAuth(t, "good-cookie")
	stub := &authStub{err: errors.New("user declined")}
	c := newAuthClient(t, sock, Options{}, stub)

	_ = c.Available()
	_ = c.Available()
	if n := stub.callCount(); n != 1 {
		t.Fatalf("requestAuth calls before re-arm = %d, want 1", n)
	}
	c.RearmPrompt()
	if err := c.authGate.Err(); err != nil {
		t.Fatalf("gate Err() after re-arm = %v, want nil", err)
	}
	_ = c.Available()
	_ = c.Available()
	if n := stub.callCount(); n != 2 {
		t.Fatalf("requestAuth calls after re-arm = %d, want 2", n)
	}
}

// A failed request silences concurrent clients and clients built later that
// share the gate.
func TestSharedAuthGate(t *testing.T) {
	sock, _ := startFakeITermAuth(t, "good-cookie")
	gate := &AuthGate{}
	stub := &authStub{err: errors.New("user declined")}

	var wg sync.WaitGroup
	for range 4 {
		c := newAuthClient(t, sock, Options{AuthGate: gate}, stub)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := c.Available(); !errors.Is(err, ErrAuthFailed) {
				t.Errorf("Available() = %v, want ErrAuthFailed", err)
			}
		}()
	}
	wg.Wait()
	later := newAuthClient(t, sock, Options{AuthGate: gate}, stub)
	if err := later.Available(); !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("later client: Available() = %v, want ErrAuthFailed", err)
	}
	if n := stub.callCount(); n != 1 {
		t.Fatalf("requestAuth calls across shared clients = %d, want 1", n)
	}
	if later.RearmPrompt(); !gate.Armed() {
		t.Fatal("RearmPrompt didn't re-arm the shared gate")
	}
}

// A client that runs out of time waiting behind another client's dialog on a
// shared gate gets ErrAuthPending within its own limit, without a dialog, and
// leaves the gate armed so the request in progress can still succeed.
func TestAuthPendingBehindSharedGate(t *testing.T) {
	sock, _ := startFakeITermAuth(t, "good-cookie")
	gate := &AuthGate{}
	first := &authStub{cookie: "good-cookie", key: "k", entered: make(chan struct{}), release: make(chan struct{})}
	a := newAuthClient(t, sock, Options{AuthGate: gate}, first)
	waiter := &authStub{cookie: "good-cookie", key: "k"}
	b := newAuthClient(t, sock, Options{AuthGate: gate, AuthTimeout: 50 * time.Millisecond}, waiter)

	aErr := make(chan error, 1)
	go func() { aErr <- a.Available() }()
	<-first.entered

	start := time.Now()
	err := b.Available()
	if !errors.Is(err, ErrAuthPending) || errors.Is(err, ErrAuthFailed) {
		t.Fatalf("waiting client: Available() = %v, want ErrAuthPending only", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("waiting client took %s, want about its own 50ms limit", elapsed)
	}
	if !strings.Contains(err.Error(), `"dash"`) || !strings.Contains(err.Error(), "waiting behind another request") {
		t.Errorf("error %q doesn't explain the wait", err)
	}
	if n := waiter.callCount(); n != 0 {
		t.Errorf("waiting client made %d requests, want 0", n)
	}
	if !gate.Armed() {
		t.Fatal("a waiting client's timeout disarmed the gate")
	}

	close(first.release)
	if err := <-aErr; err != nil {
		t.Fatalf("request in progress: Available() = %v, want success", err)
	}
	if !gate.Armed() {
		t.Error("gate disarmed after the request succeeded")
	}
}

// When the slot is free and the deadline has already passed, both select
// cases are ready and Go picks one at random, so run it many times: the gate
// must never run the request or disarm, and the caller gets ErrAuthPending.
func TestAuthGateExpiredDeadlineNeverDisarms(t *testing.T) {
	gate := &AuthGate{}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	calls := 0
	fn := func(context.Context, string) (string, string, error) {
		calls++
		return "", "", ctx.Err()
	}
	for i := range 500 {
		_, _, err := gate.request(ctx, "dash", time.Millisecond, fn)
		if !errors.Is(err, ErrAuthPending) || errors.Is(err, ErrAuthFailed) {
			t.Fatalf("iteration %d: request = %v, want ErrAuthPending only", i, err)
		}
		if calls != 0 || !gate.Armed() {
			t.Fatalf("iteration %d: request ran %d times, armed %v; want 0 and armed", i, calls, gate.Armed())
		}
	}
}
