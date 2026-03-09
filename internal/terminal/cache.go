package terminal

import (
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

// NewCachedBackend creates a CachedBackend wrapping inner with the given TTL in seconds.
func NewCachedBackend(inner Backend, ttlSeconds int) *CachedBackend {
	return &CachedBackend{
		inner: inner,
		ttl:   time.Duration(ttlSeconds) * time.Second,
	}
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

// GetVar delegates to the inner backend.
func (c *CachedBackend) GetVar(sessionID, varName string) (string, error) {
	return c.inner.GetVar(sessionID, varName)
}

// MonitorOutput delegates to the inner backend.
func (c *CachedBackend) MonitorOutput(sessionID, logPath, patterns string) (int, error) {
	return c.inner.MonitorOutput(sessionID, logPath, patterns)
}

// Resize forwards to the inner backend if it supports resizing (e.g. PTY backend).
func (c *CachedBackend) Resize(cols, rows int) {
	if r, ok := c.inner.(interface{ Resize(int, int) }); ok {
		r.Resize(cols, rows)
	}
}

// Close forwards to the inner backend if it supports closing (e.g. PTY backend).
func (c *CachedBackend) Close() {
	if cl, ok := c.inner.(interface{ Close() }); ok {
		cl.Close()
	}
}

// Inner returns the wrapped backend.
func (c *CachedBackend) Inner() Backend {
	return c.inner
}

// Compile-time check that CachedBackend implements Backend.
var _ Backend = (*CachedBackend)(nil)
