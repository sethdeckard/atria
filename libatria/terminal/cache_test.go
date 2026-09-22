package terminal

import (
	"sync"
	"testing"
	"time"
)

// mockBackend is a test double that tracks how many times ListSessions is called.
type mockBackend struct {
	mu           sync.Mutex
	calls        int
	sessions     []Session
	newSessionID string
	sent         []string
}

func (m *mockBackend) Available() error            { return nil }
func (m *mockBackend) NewSession() (string, error) { return m.newSessionID, nil }
func (m *mockBackend) SendText(sessionID, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, text)
	return nil
}
func (m *mockBackend) RunCommand(sessionID, cmd string) error                 { return nil }
func (m *mockBackend) FocusSession(sessionID string) error                    { return nil }
func (m *mockBackend) ReadScreen(sessionID string, lines int) (string, error) { return "", nil }
func (m *mockBackend) GetVar(sessionID, varName string) (string, error)       { return "", nil }
func (m *mockBackend) MonitorOutput(sessionID, logPath, patterns string) (int, error) {
	return 0, nil
}

func (m *mockBackend) ListSessions() ([]Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	return m.sessions, nil
}

func (m *mockBackend) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func TestCachedBackend_ReturnsCachedWithinTTL(t *testing.T) {
	mock := &mockBackend{
		sessions: []Session{
			{ID: "1", Name: "session-1", TTY: "/dev/pts/0"},
		},
	}

	cached := NewCachedBackend(mock, 5*time.Second)

	// First call should hit the inner backend.
	sessions1, err := cached.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions1) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions1))
	}

	// Second call within TTL should return cached data.
	sessions2, err := cached.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions2) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions2))
	}

	if mock.callCount() != 1 {
		t.Errorf("expected 1 inner call, got %d", mock.callCount())
	}
}

func TestCachedBackend_RefreshesAfterTTL(t *testing.T) {
	mock := &mockBackend{
		sessions: []Session{
			{ID: "1", Name: "session-1", TTY: "/dev/pts/0"},
		},
	}

	// Use a very short TTL so it expires quickly.
	cached := NewCachedBackend(mock, 0)
	// Override TTL to something tiny for testing.
	cached.ttl = 10 * time.Millisecond

	_, err := cached.ListSessions()
	if err != nil {
		t.Fatal(err)
	}

	// Wait for TTL to expire.
	time.Sleep(20 * time.Millisecond)

	_, err = cached.ListSessions()
	if err != nil {
		t.Fatal(err)
	}

	if mock.callCount() != 2 {
		t.Errorf("expected 2 inner calls after TTL expiry, got %d", mock.callCount())
	}
}

func TestCachedBackend_NewSessionOn(t *testing.T) {
	primary := &mockBackend{newSessionID: "pty-0"}
	integ := &mockBackend{newSessionID: "pty-1"}

	comp := NewCompositeBackend(primary, "tmux", []Integration{
		{Prefix: "pty:", Source: "pty", Backend: integ},
	})
	cached := NewCachedBackend(comp, 5*time.Second)

	id, err := cached.NewSessionOn("pty")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "pty:pty-1" {
		t.Errorf("expected 'pty:pty-1', got %q", id)
	}

	// Primary source works too.
	id, err = cached.NewSessionOn("tmux")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "pty-0" {
		t.Errorf("expected 'pty-0', got %q", id)
	}
}

func TestCachedBackend_InvalidateForcesRefresh(t *testing.T) {
	mock := &mockBackend{
		sessions: []Session{
			{ID: "1", Name: "session-1", TTY: "/dev/pts/0"},
		},
	}

	cached := NewCachedBackend(mock, time.Minute) // Long TTL.

	_, err := cached.ListSessions()
	if err != nil {
		t.Fatal(err)
	}

	if mock.callCount() != 1 {
		t.Fatalf("expected 1 call, got %d", mock.callCount())
	}

	// Invalidate and call again - should fetch fresh.
	cached.Invalidate()

	_, err = cached.ListSessions()
	if err != nil {
		t.Fatal(err)
	}

	if mock.callCount() != 2 {
		t.Errorf("expected 2 calls after invalidation, got %d", mock.callCount())
	}
}

func TestCachedBackendForwardsOptionalInterfaces(t *testing.T) {
	primary := &trackingBackend{}
	comp := NewCompositeBackend(primary, "tmux", nil)
	cached := NewCachedBackend(comp, time.Minute)

	if got := cached.PrimarySource(); got != "tmux" {
		t.Fatalf("PrimarySource = %q, want tmux", got)
	}
	if got := cached.FailedSources(); got != nil {
		t.Fatalf("FailedSources = %v, want nil before any listing", got)
	}
	if err := cached.SendKey("%1", KeyDown); err != nil {
		t.Fatal(err)
	}
	if primary.lastSendText != "\x1b[B" {
		t.Fatalf("sent = %q, want down-arrow sequence", primary.lastSendText)
	}
	if cached.ConsumeBell("%1") {
		t.Fatal("no bell state anywhere: must be false")
	}
	if err := cached.Close(); err != nil {
		t.Fatalf("Close = %v", err)
	}

	// A bare backend with none of the optional interfaces degrades cleanly.
	bare := NewCachedBackend(&mockBackend{}, time.Minute)
	if bare.PrimarySource() != "" || bare.FailedSources() != nil || bare.ConsumeBell("x") {
		t.Fatal("bare backend should report zero values")
	}
	if err := bare.Close(); err != nil {
		t.Fatalf("bare Close = %v", err)
	}
	if _, err := bare.NewSessionOn("pty"); err == nil {
		t.Fatal("NewSessionOn without SourceLauncher must error")
	}
}
