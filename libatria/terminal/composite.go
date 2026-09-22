package terminal

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
)

// Integration represents a discovery-only backend that contributes sessions.
type Integration struct {
	Prefix  string // "iterm:" or "tmux:"
	Source  string // "iterm" or "tmux"
	Backend Backend
}

// CompositeBackend merges a primary backend (for launching) with optional
// integration backends (for discovering existing sessions).
type CompositeBackend struct {
	mu            sync.RWMutex
	primary       Backend
	primarySource string // "pty", "iterm", "tmux"
	integrations  []Integration
	failedSources []string // sources whose ListSessions failed on last call
	selfTTY       string   // Atria's own TTY — sessions matching this are filtered
}

// NewCompositeBackend creates a composite that delegates launches to primary
// and merges session lists from all backends. primarySource labels sessions
// from the primary backend (e.g. "pty", "iterm", "tmux").
func NewCompositeBackend(primary Backend, primarySource string, integrations []Integration) *CompositeBackend {
	return &CompositeBackend{
		primary:       primary,
		primarySource: primarySource,
		integrations:  integrations,
	}
}

// SetSelfTTY sets Atria's own controlling TTY so that sessions on this TTY
// are filtered from ListSessions results. This prevents Atria's own pane
// from appearing as a phantom agent session when integration backends
// discover it via auto-title inheritance.
func (c *CompositeBackend) SetSelfTTY(tty string) {
	c.mu.Lock()
	c.selfTTY = tty
	c.mu.Unlock()
}

// Available checks the primary backend only. Integration failures are non-fatal.
func (c *CompositeBackend) Available() error {
	c.mu.RLock()
	p := c.primary
	c.mu.RUnlock()
	return p.Available()
}

// ListSessions merges sessions from primary and all integrations.
// Integration sessions are prefixed and tagged with Source.
// Deduplication by TTY ensures the same terminal isn't listed twice.
func (c *CompositeBackend) ListSessions() ([]Session, error) {
	// Snapshot backends under read lock — the actual ListSessions calls
	// (which may involve subprocesses/sockets) run without holding any lock,
	// so interactive paths (SendText, ReadScreen, etc.) are not blocked.
	c.mu.RLock()
	primary := c.primary
	primarySource := c.primarySource
	selfTTY := c.selfTTY
	integrations := make([]Integration, len(c.integrations))
	copy(integrations, c.integrations)
	c.mu.RUnlock()

	primarySessions, err := primary.ListSessions()
	if err != nil {
		return nil, fmt.Errorf("primary backend: %w", err)
	}

	// Track TTYs from primary for deduplication.
	seenTTY := make(map[string]bool)
	var result []Session
	for _, s := range primarySessions {
		s.Source = primarySource
		if selfTTY != "" && s.TTY == selfTTY {
			continue
		}
		result = append(result, s)
		if s.TTY != "" {
			seenTTY[s.TTY] = true
		}
	}

	var failedSources []string
	for _, integ := range integrations {
		sessions, err := integ.Backend.ListSessions()
		if err != nil {
			// Integration errors are non-fatal — skip silently.
			failedSources = append(failedSources, integ.Source)
			continue
		}
		for _, s := range sessions {
			if selfTTY != "" && s.TTY == selfTTY {
				continue
			}
			// Deduplicate by TTY.
			if s.TTY != "" && seenTTY[s.TTY] {
				continue
			}
			if s.TTY != "" {
				seenTTY[s.TTY] = true
			}
			s.ID = integ.Prefix + s.ID
			s.Source = integ.Source
			result = append(result, s)
		}
	}

	// Only the failedSources assignment needs write access.
	c.mu.Lock()
	c.failedSources = failedSources
	c.mu.Unlock()

	return result, nil
}

// FailedSources returns the integration sources that failed during the last
// ListSessions call. This allows callers to skip cleanup for sessions
// belonging to transiently unavailable integrations.
func (c *CompositeBackend) FailedSources() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.failedSources
}

// NewSession always delegates to the primary backend.
func (c *CompositeBackend) NewSession() (string, error) {
	c.mu.RLock()
	p := c.primary
	c.mu.RUnlock()
	return p.NewSession()
}

// NewSessionOn creates a session on a specific backend identified by source name.
// If source matches the primary backend, delegates directly. Otherwise, looks
// for a matching integration and prefixes the returned session ID.
func (c *CompositeBackend) NewSessionOn(source string) (string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if source == c.primarySource {
		return c.primary.NewSession()
	}
	for _, integ := range c.integrations {
		if integ.Source == source {
			id, err := integ.Backend.NewSession()
			if err != nil {
				return "", err
			}
			return integ.Prefix + id, nil
		}
	}
	return "", fmt.Errorf("backend %q not available", source)
}

// SendText routes to the correct backend based on session ID prefix.
func (c *CompositeBackend) SendText(sessionID, text string) error {
	c.mu.RLock()
	b, id, err := c.route(sessionID)
	c.mu.RUnlock()
	if err != nil {
		return err
	}
	return b.SendText(id, text)
}

// RunCommand routes to the correct backend based on session ID prefix.
func (c *CompositeBackend) RunCommand(sessionID, cmd string) error {
	c.mu.RLock()
	b, id, err := c.route(sessionID)
	c.mu.RUnlock()
	if err != nil {
		return err
	}
	return b.RunCommand(id, cmd)
}

// FocusSession routes to the correct backend based on session ID prefix.
func (c *CompositeBackend) FocusSession(sessionID string) error {
	c.mu.RLock()
	b, id, err := c.route(sessionID)
	c.mu.RUnlock()
	if err != nil {
		return err
	}
	return b.FocusSession(id)
}

// ReadScreen routes to the correct backend based on session ID prefix.
func (c *CompositeBackend) ReadScreen(sessionID string, lines int) (string, error) {
	c.mu.RLock()
	b, id, err := c.route(sessionID)
	c.mu.RUnlock()
	if err != nil {
		return "", err
	}
	return b.ReadScreen(id, lines)
}

// SendKey routes to the correct backend based on session ID prefix, using the
// owner's KeySender when it has one and the key's byte sequence otherwise.
func (c *CompositeBackend) SendKey(sessionID string, key Key) error {
	c.mu.RLock()
	b, id, err := c.route(sessionID)
	c.mu.RUnlock()
	if err != nil {
		return err
	}
	return SendKey(b, id, key)
}

// ConsumeBell routes to the owning backend's BellSource. A backend without
// bell state reports false.
func (c *CompositeBackend) ConsumeBell(sessionID string) bool {
	c.mu.RLock()
	b, id, err := c.route(sessionID)
	c.mu.RUnlock()
	if err != nil {
		return false
	}
	if bs, ok := b.(BellSource); ok {
		return bs.ConsumeBell(id)
	}
	return false
}

// ReadScreenStyled routes to the correct backend based on session ID prefix,
// preferring its styled-read path and falling back to plain ReadScreen when the
// owning backend does not implement StyledReader.
func (c *CompositeBackend) ReadScreenStyled(sessionID string, lines int) (string, error) {
	c.mu.RLock()
	b, id, err := c.route(sessionID)
	c.mu.RUnlock()
	if err != nil {
		return "", err
	}
	if sr, ok := b.(StyledReader); ok {
		return sr.ReadScreenStyled(id, lines)
	}
	return b.ReadScreen(id, lines)
}

// GetVar routes to the correct backend based on session ID prefix.
func (c *CompositeBackend) GetVar(sessionID, varName string) (string, error) {
	c.mu.RLock()
	b, id, err := c.route(sessionID)
	c.mu.RUnlock()
	if err != nil {
		return "", err
	}
	return b.GetVar(id, varName)
}

// MonitorOutput routes to the correct backend based on session ID prefix.
func (c *CompositeBackend) MonitorOutput(sessionID, logPath, patterns string) (int, error) {
	c.mu.RLock()
	b, id, err := c.route(sessionID)
	c.mu.RUnlock()
	if err != nil {
		return 0, err
	}
	return b.MonitorOutput(id, logPath, patterns)
}

// Resize forwards to all backends that support resizing (primary + integrations).
// This ensures PTY sessions receive resize updates even when PTY is an integration.
func (c *CompositeBackend) Resize(cols, rows int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if r, ok := c.primary.(Resizer); ok {
		r.Resize(cols, rows)
	}
	for _, integ := range c.integrations {
		if r, ok := integ.Backend.(Resizer); ok {
			r.Resize(cols, rows)
		}
	}
}

// Close closes every backend that implements io.Closer (primary and
// integrations), so PTY sessions are cleaned up even when PTY is an
// integration. Every backend is closed regardless of earlier failures; the
// returned error joins whatever they reported.
func (c *CompositeBackend) Close() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var errs []error
	if cl, ok := c.primary.(io.Closer); ok {
		if err := cl.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	for _, integ := range c.integrations {
		if cl, ok := integ.Backend.(io.Closer); ok {
			if err := cl.Close(); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", integ.Source, err))
			}
		}
	}
	return errors.Join(errs...)
}

// PrimarySource returns the source label for sessions from the primary backend.
func (c *CompositeBackend) PrimarySource() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.primarySource
}

// route resolves a session ID to its owning backend and the unprefixed ID.
// Returns an error if the session ID has an integration prefix but that
// integration is not available (e.g. disabled or failed to start).
// Caller must hold at least a read lock.
func (c *CompositeBackend) route(sessionID string) (Backend, string, error) {
	for _, integ := range c.integrations {
		if strings.HasPrefix(sessionID, integ.Prefix) {
			return integ.Backend, strings.TrimPrefix(sessionID, integ.Prefix), nil
		}
	}
	// Check for unrecognized integration prefix (contains ":" before any "/").
	if i := strings.Index(sessionID, ":"); i > 0 && !strings.Contains(sessionID[:i], "/") {
		prefix := sessionID[:i]
		return nil, "", fmt.Errorf("integration %q not available for session %s", prefix, sessionID)
	}
	return c.primary, sessionID, nil
}

// AddIntegration adds an integration backend. Thread-safe.
func (c *CompositeBackend) AddIntegration(integ Integration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.integrations = append(c.integrations, integ)
}

// RemoveIntegration removes integrations matching the given prefix. Thread-safe.
// If the removed backend implements io.Closer, it is closed; a close error is
// not reported because the backend is gone from the composite either way.
func (c *CompositeBackend) RemoveIntegration(prefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	filtered := c.integrations[:0]
	for _, integ := range c.integrations {
		if integ.Prefix == prefix {
			if closer, ok := integ.Backend.(io.Closer); ok {
				closer.Close() //nolint:errcheck // best-effort; the integration is removed regardless
			}
		} else {
			filtered = append(filtered, integ)
		}
	}
	c.integrations = filtered
}

// DetachIntegration removes integrations matching the given prefix without
// closing their backends. Use it when the backend stays in service elsewhere,
// such as PTY being promoted back to primary. Thread-safe.
func (c *CompositeBackend) DetachIntegration(prefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	filtered := c.integrations[:0]
	for _, integ := range c.integrations {
		if integ.Prefix != prefix {
			filtered = append(filtered, integ)
		}
	}
	c.integrations = filtered
}

// SetPrimary changes the primary backend and its source label. Thread-safe.
func (c *CompositeBackend) SetPrimary(b Backend, source string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.primary = b
	c.primarySource = source
}

// Integrations returns a snapshot of the current integrations. Thread-safe.
func (c *CompositeBackend) Integrations() []Integration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]Integration, len(c.integrations))
	copy(result, c.integrations)
	return result
}

// Compile-time check that CompositeBackend implements Backend.
var _ Backend = (*CompositeBackend)(nil)

// Compile-time checks for the optional interfaces the composite implements.
var (
	_ StyledReader    = (*CompositeBackend)(nil)
	_ Resizer         = (*CompositeBackend)(nil)
	_ SourceLauncher  = (*CompositeBackend)(nil)
	_ FailureReporter = (*CompositeBackend)(nil)
	_ PrimaryReporter = (*CompositeBackend)(nil)
	_ KeySender       = (*CompositeBackend)(nil)
	_ BellSource      = (*CompositeBackend)(nil)
	_ io.Closer       = (*CompositeBackend)(nil)
)
