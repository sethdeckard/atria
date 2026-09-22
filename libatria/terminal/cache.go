package terminal

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// CachedBackend wraps any Backend and caches ListSessions results with a TTL.
type CachedBackend struct {
	inner    Backend
	sessions []Session
	fetched  time.Time
	ttl      time.Duration
	mu       sync.Mutex
}

// NewCachedBackend wraps inner and serves ListSessions from a cache for ttl
// after each fetch. A zero or negative ttl disables caching. Backend
// operations delegate to inner, with fallbacks for optional interfaces inner
// lacks; Invalidate clears this wrapper's cache.
func NewCachedBackend(inner Backend, ttl time.Duration) *CachedBackend {
	return &CachedBackend{inner: inner, ttl: ttl}
}

// ListSessions returns cached sessions if TTL hasn't expired, otherwise fetches fresh.
func (c *CachedBackend) ListSessions() ([]Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sessions != nil && time.Since(c.fetched) < c.ttl {
		return c.sessions, nil
	}

	sessions, err := c.inner.ListSessions()
	if err != nil {
		return nil, err
	}

	c.sessions = sessions
	c.fetched = time.Now()
	return sessions, nil
}

// Invalidate clears the cache, forcing the next ListSessions call to fetch fresh.
func (c *CachedBackend) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessions = nil
	c.fetched = time.Time{}
}

// Available delegates to the inner backend.
func (c *CachedBackend) Available() error {
	return c.inner.Available()
}

// NewSession delegates to the inner backend.
func (c *CachedBackend) NewSession() (string, error) {
	return c.inner.NewSession()
}

// NewSessionOn delegates to the inner backend's NewSessionOn if it supports it.
func (c *CachedBackend) NewSessionOn(source string) (string, error) {
	if ns, ok := c.inner.(SourceLauncher); ok {
		return ns.NewSessionOn(source)
	}
	return "", fmt.Errorf("inner backend does not support NewSessionOn")
}

// SendText delegates to the inner backend.
func (c *CachedBackend) SendText(sessionID, text string) error {
	return c.inner.SendText(sessionID, text)
}

// RunCommand delegates to the inner backend.
func (c *CachedBackend) RunCommand(sessionID, cmd string) error {
	return c.inner.RunCommand(sessionID, cmd)
}

// FocusSession delegates to the inner backend.
func (c *CachedBackend) FocusSession(sessionID string) error {
	return c.inner.FocusSession(sessionID)
}

// ReadScreen delegates to the inner backend.
func (c *CachedBackend) ReadScreen(sessionID string, lines int) (string, error) {
	return c.inner.ReadScreen(sessionID, lines)
}

// ReadScreenStyled delegates to the inner backend's styled-read path, falling
// back to plain ReadScreen when the inner backend does not implement it.
func (c *CachedBackend) ReadScreenStyled(sessionID string, lines int) (string, error) {
	if sr, ok := c.inner.(StyledReader); ok {
		return sr.ReadScreenStyled(sessionID, lines)
	}
	return c.inner.ReadScreen(sessionID, lines)
}

// SendKey forwards to the inner backend's KeySender, or sends the key's byte
// sequence when it has none.
func (c *CachedBackend) SendKey(sessionID string, key Key) error {
	return SendKey(c.inner, sessionID, key)
}

// ConsumeBell forwards to the inner backend's BellSource; false when it has none.
func (c *CachedBackend) ConsumeBell(sessionID string) bool {
	if bs, ok := c.inner.(BellSource); ok {
		return bs.ConsumeBell(sessionID)
	}
	return false
}

// FailedSources forwards to the inner backend's FailureReporter; nil when it
// has none.
func (c *CachedBackend) FailedSources() []string {
	if fr, ok := c.inner.(FailureReporter); ok {
		return fr.FailedSources()
	}
	return nil
}

// PrimarySource forwards to the inner backend's PrimaryReporter; "" when it
// has none.
func (c *CachedBackend) PrimarySource() string {
	if pr, ok := c.inner.(PrimaryReporter); ok {
		return pr.PrimarySource()
	}
	return ""
}

// GetVar delegates to the inner backend.
func (c *CachedBackend) GetVar(sessionID, varName string) (string, error) {
	return c.inner.GetVar(sessionID, varName)
}

// MonitorOutput delegates to the inner backend.
func (c *CachedBackend) MonitorOutput(sessionID, logPath, patterns string) (int, error) {
	return c.inner.MonitorOutput(sessionID, logPath, patterns)
}

// Resize forwards to the inner backend's Resizer; a no-op when it has none.
func (c *CachedBackend) Resize(cols, rows int) {
	if r, ok := c.inner.(Resizer); ok {
		r.Resize(cols, rows)
	}
}

// Close closes the inner backend when it implements io.Closer; nil otherwise.
func (c *CachedBackend) Close() error {
	if cl, ok := c.inner.(io.Closer); ok {
		return cl.Close()
	}
	return nil
}

// Inner returns the wrapped backend.
func (c *CachedBackend) Inner() Backend {
	return c.inner
}

// Compile-time check that CachedBackend implements Backend.
var _ Backend = (*CachedBackend)(nil)

// Compile-time checks for the optional interfaces the cache provides.
var (
	_ StyledReader    = (*CachedBackend)(nil)
	_ Resizer         = (*CachedBackend)(nil)
	_ SourceLauncher  = (*CachedBackend)(nil)
	_ Invalidator     = (*CachedBackend)(nil)
	_ FailureReporter = (*CachedBackend)(nil)
	_ PrimaryReporter = (*CachedBackend)(nil)
	_ KeySender       = (*CachedBackend)(nil)
	_ BellSource      = (*CachedBackend)(nil)
	_ io.Closer       = (*CachedBackend)(nil)
)
