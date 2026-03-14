package terminal

import (
	"fmt"
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
	if r, ok := c.primary.(interface{ Resize(int, int) }); ok {
		r.Resize(cols, rows)
	}
	for _, integ := range c.integrations {
		if r, ok := integ.Backend.(interface{ Resize(int, int) }); ok {
			r.Resize(cols, rows)
		}
	}
}

// Close forwards to all backends that support closing (primary + integrations).
// This ensures PTY sessions are cleaned up even when PTY is an integration.
func (c *CompositeBackend) Close() {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if cl, ok := c.primary.(interface{ Close() }); ok {
		cl.Close()
	}
	for _, integ := range c.integrations {
		if cl, ok := integ.Backend.(interface{ Close() }); ok {
			cl.Close()
		}
	}
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
// If the removed backend implements a Close() method, it is called.
func (c *CompositeBackend) RemoveIntegration(prefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	filtered := c.integrations[:0]
	for _, integ := range c.integrations {
		if integ.Prefix == prefix {
			if closer, ok := integ.Backend.(interface{ Close() }); ok {
				closer.Close()
			}
		} else {
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
