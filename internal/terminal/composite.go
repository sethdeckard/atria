package terminal

import (
	"fmt"
	"strings"
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
	primary       Backend
	primarySource string // "pty", "iterm", "tmux"
	integrations  []Integration
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
	return c.primary.Available()
}

// ListSessions merges sessions from primary and all integrations.
// Integration sessions are prefixed and tagged with Source.
// Deduplication by TTY ensures the same terminal isn't listed twice.
func (c *CompositeBackend) ListSessions() ([]Session, error) {
	primary, err := c.primary.ListSessions()
	if err != nil {
		return nil, fmt.Errorf("primary backend: %w", err)
	}

	// Track TTYs from primary for deduplication.
	seenTTY := make(map[string]bool)
	primarySource := c.PrimarySource()
	var result []Session
	for _, s := range primary {
		s.Source = primarySource
		result = append(result, s)
		if s.TTY != "" {
			seenTTY[s.TTY] = true
		}
	}

	for _, integ := range c.integrations {
		sessions, err := integ.Backend.ListSessions()
		if err != nil {
			// Integration errors are non-fatal — skip silently.
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

	return result, nil
}

// NewSession always delegates to the primary backend.
func (c *CompositeBackend) NewSession() (string, error) {
	return c.primary.NewSession()
}

// SendText routes to the correct backend based on session ID prefix.
func (c *CompositeBackend) SendText(sessionID, text string) error {
	b, id, err := c.route(sessionID)
	if err != nil {
		return err
	}
	return b.SendText(id, text)
}

// RunCommand routes to the correct backend based on session ID prefix.
func (c *CompositeBackend) RunCommand(sessionID, cmd string) error {
	b, id, err := c.route(sessionID)
	if err != nil {
		return err
	}
	return b.RunCommand(id, cmd)
}

// FocusSession routes to the correct backend based on session ID prefix.
func (c *CompositeBackend) FocusSession(sessionID string) error {
	b, id, err := c.route(sessionID)
	if err != nil {
		return err
	}
	return b.FocusSession(id)
}

// ReadScreen routes to the correct backend based on session ID prefix.
func (c *CompositeBackend) ReadScreen(sessionID string, lines int) (string, error) {
	b, id, err := c.route(sessionID)
	if err != nil {
		return "", err
	}
	return b.ReadScreen(id, lines)
}

// GetVar routes to the correct backend based on session ID prefix.
func (c *CompositeBackend) GetVar(sessionID, varName string) (string, error) {
	b, id, err := c.route(sessionID)
	if err != nil {
		return "", err
	}
	return b.GetVar(id, varName)
}

// MonitorOutput routes to the correct backend based on session ID prefix.
func (c *CompositeBackend) MonitorOutput(sessionID, logPath, patterns string) (int, error) {
	b, id, err := c.route(sessionID)
	if err != nil {
		return 0, err
	}
	return b.MonitorOutput(id, logPath, patterns)
}

// Resize forwards to the primary backend (only PTY needs this).
func (c *CompositeBackend) Resize(cols, rows int) {
	if r, ok := c.primary.(interface{ Resize(int, int) }); ok {
		r.Resize(cols, rows)
	}
}

// Close forwards to the primary backend (only PTY needs this).
func (c *CompositeBackend) Close() {
	if cl, ok := c.primary.(interface{ Close() }); ok {
		cl.Close()
	}
}

// PrimarySource returns the source label for sessions from the primary backend.
func (c *CompositeBackend) PrimarySource() string {
	return c.primarySource
}

// route resolves a session ID to its owning backend and the unprefixed ID.
// Returns an error if the session ID has an integration prefix but that
// integration is not available (e.g. disabled or failed to start).
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

// Compile-time check that CompositeBackend implements Backend.
var _ Backend = (*CompositeBackend)(nil)
