package iterm

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultAuthTimeout bounds the AppleScript credential request when
// Options.AuthTimeout is zero. It is far longer than a round trip because a
// person has to answer iTerm2's dialog.
const DefaultAuthTimeout = 2 * time.Minute

// AuthGate decides whether the clients sharing it may request credentials.
// It is armed until a request fails (declined, errored, or timed out), and a
// disarmed gate refuses every request until Rearm, so a person who said no is
// not asked again on every reconnect or probe. A successful request leaves it
// armed. The zero value is armed and ready to use; share one gate between
// clients so that a refusal carries over to clients built later. It is safe
// for concurrent use. Only one request runs at a time: other clients sharing
// the gate that get a 401 meanwhile wait behind it, within their own
// AuthTimeout, rather than showing a dialog at the same time. If it fails
// they are refused without one; if it succeeds they make their own request,
// because credentials belong to each client.
type AuthGate struct {
	once   sync.Once
	ch     chan struct{}           // one request at a time across the sharing clients
	failed atomic.Pointer[failure] // nil while armed
}

// failure is the error that disarmed a gate.
type failure struct{ err error }

// Armed reports whether a credential request may be made.
func (g *AuthGate) Armed() bool { return g.failed.Load() == nil }

// Err returns the error, wrapping ErrAuthFailed, from the request that
// disarmed the gate, or nil while it is armed. It doesn't wait for a request
// in progress.
func (g *AuthGate) Err() error {
	if f := g.failed.Load(); f != nil {
		return f.err
	}
	return nil
}

// Rearm allows one more credential request after a failed one. It doesn't
// wait for a request in progress.
func (g *AuthGate) Rearm() { g.failed.Store(nil) }

// disarm records err, which wraps ErrAuthFailed, as the reason the gate is
// off.
func (g *AuthGate) disarm(err error) { g.failed.Store(&failure{err}) }

// request runs fn when the gate is armed and disarms it when fn fails. ctx
// bounds the whole wait, for the gate as well as for the answer, so limit is
// the total time to an answer. A caller that runs out of time waiting behind
// another request gets ErrAuthPending and leaves the gate alone, because the
// other request may still succeed. A caller that waited behind a failing
// request is refused without a dialog. name is the client name, for the error
// text.
func (g *AuthGate) request(ctx context.Context, name string, limit time.Duration, fn func(context.Context, string) (string, string, error)) (string, string, error) {
	pending := func() (string, string, error) {
		return "", "", fmt.Errorf("%w for %q: no answer within %s, waiting behind another request", ErrAuthPending, name, limit)
	}
	select {
	case g.slot() <- struct{}{}:
	case <-ctx.Done():
		return pending()
	}
	defer func() { <-g.slot() }()
	// select picks at random when the slot frees as the deadline passes; a
	// request started with an expired context would fail at once and disarm
	// the gate for every client, though no one was asked.
	if ctx.Err() != nil {
		return pending()
	}
	if !g.Armed() {
		return "", "", fmt.Errorf("%w for %q: an earlier request was declined or timed out, and prompting stays off until it is re-armed; or create ~/.config/iterm2/disable-automation-auth",
			ErrAuthFailed, name)
	}
	cookie, key, err := fn(ctx, name)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			err = fmt.Errorf("no answer within %s", limit)
		}
		err = fmt.Errorf("%w for %q: %v; prompting stays off until it is re-armed", ErrAuthFailed, name, err)
		g.disarm(err)
		return "", "", err
	}
	return cookie, key, nil
}

// slot returns the gate's one-request channel, making it on first use so the
// zero value works.
func (g *AuthGate) slot() chan struct{} {
	g.once.Do(func() { g.ch = make(chan struct{}, 1) })
	return g.ch
}

// requestCookieAndKey requests a cookie and key from iTerm2 via AppleScript,
// identifying the caller as appName. ctx bounds the wait for the person to
// answer. When ctx expires, osascript is killed; this function does not
// explicitly dismiss iTerm2's dialog.
func requestCookieAndKey(ctx context.Context, appName string) (string, string, error) {
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", "-")
	cmd.Stdin = strings.NewReader(
		`tell application "iTerm2" to request cookie and key for app named ` + strconv.Quote(appName))
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return "", "", ctx.Err()
	}
	if err != nil {
		return "", "", fmt.Errorf("AppleScript auth failed: %w", err)
	}
	parts := strings.Fields(strings.TrimSpace(string(out)))
	if len(parts) != 2 {
		return "", "", fmt.Errorf("unexpected auth response: %s", string(out))
	}
	return parts[0], parts[1], nil
}
