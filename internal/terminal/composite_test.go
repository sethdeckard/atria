package terminal

import (
	"errors"
	"strings"
	"testing"
)

// trackingBackend extends mockBackend with tracking for routed calls.
type trackingBackend struct {
	mockBackend
	lastSendID    string
	lastSendText  string
	lastRunID     string
	lastRunCmd    string
	lastFocusID   string
	lastReadID    string
	lastReadLines int
	lastGetVarID  string
	lastGetVar    string
	readScreen    string
	getVarResult  string
	listErr       error
}

func (t *trackingBackend) SendText(sessionID, text string) error {
	t.lastSendID = sessionID
	t.lastSendText = text
	return nil
}

func (t *trackingBackend) RunCommand(sessionID, cmd string) error {
	t.lastRunID = sessionID
	t.lastRunCmd = cmd
	return nil
}

func (t *trackingBackend) FocusSession(sessionID string) error {
	t.lastFocusID = sessionID
	return nil
}

func (t *trackingBackend) ReadScreen(sessionID string, lines int) (string, error) {
	t.lastReadID = sessionID
	t.lastReadLines = lines
	return t.readScreen, nil
}

func (t *trackingBackend) GetVar(sessionID, varName string) (string, error) {
	t.lastGetVarID = sessionID
	t.lastGetVar = varName
	return t.getVarResult, nil
}

func (t *trackingBackend) ListSessions() ([]Session, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls++
	if t.listErr != nil {
		return nil, t.listErr
	}
	return t.sessions, nil
}

func TestComposite_ListSessionsMergesWithPrefixes(t *testing.T) {
	primary := &trackingBackend{
		mockBackend: mockBackend{
			sessions: []Session{
				{ID: "pty-0", Name: "claude", TTY: "/dev/pts/0"},
			},
		},
	}
	itermInteg := &trackingBackend{
		mockBackend: mockBackend{
			sessions: []Session{
				{ID: "session-abc", Name: "✳ claude", TTY: "/dev/ttys001"},
			},
		},
	}

	comp := NewCompositeBackend(primary, "pty", []Integration{
		{Prefix: "iterm:", Source: "iterm", Backend: itermInteg},
	})

	sessions, err := comp.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}

	// Primary session unchanged.
	if sessions[0].ID != "pty-0" {
		t.Errorf("expected primary ID 'pty-0', got %q", sessions[0].ID)
	}
	if sessions[0].Source != "pty" {
		t.Errorf("expected primary Source 'pty', got %q", sessions[0].Source)
	}

	// Integration session prefixed.
	if sessions[1].ID != "iterm:session-abc" {
		t.Errorf("expected integration ID 'iterm:session-abc', got %q", sessions[1].ID)
	}
	if sessions[1].Source != "iterm" {
		t.Errorf("expected integration Source 'iterm', got %q", sessions[1].Source)
	}
}

func TestComposite_DeduplicateByTTY(t *testing.T) {
	primary := &trackingBackend{
		mockBackend: mockBackend{
			sessions: []Session{
				{ID: "pty-0", Name: "claude", TTY: "/dev/pts/0"},
			},
		},
	}
	tmuxInteg := &trackingBackend{
		mockBackend: mockBackend{
			sessions: []Session{
				{ID: "%0", Name: "claude", TTY: "/dev/pts/0"}, // same TTY
				{ID: "%1", Name: "codex", TTY: "/dev/pts/1"},  // different TTY
			},
		},
	}

	comp := NewCompositeBackend(primary, "pty", []Integration{
		{Prefix: "tmux:", Source: "tmux", Backend: tmuxInteg},
	})

	sessions, err := comp.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions (1 primary + 1 deduped), got %d", len(sessions))
	}
	if sessions[1].ID != "tmux:%1" {
		t.Errorf("expected non-duplicate session 'tmux:%%1', got %q", sessions[1].ID)
	}
}

func TestComposite_IntegrationErrorNonFatal(t *testing.T) {
	primary := &trackingBackend{
		mockBackend: mockBackend{
			sessions: []Session{
				{ID: "pty-0", Name: "claude"},
			},
		},
	}
	failingInteg := &trackingBackend{
		listErr: errors.New("connection refused"),
	}

	comp := NewCompositeBackend(primary, "pty", []Integration{
		{Prefix: "iterm:", Source: "iterm", Backend: failingInteg},
	})

	sessions, err := comp.ListSessions()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session from primary, got %d", len(sessions))
	}
}

func TestComposite_NewSessionAlwaysPrimary(t *testing.T) {
	primary := &trackingBackend{}
	primary.mockBackend.sessions = nil

	comp := NewCompositeBackend(primary, "pty", []Integration{
		{Prefix: "iterm:", Source: "iterm", Backend: &trackingBackend{}},
	})

	_, _ = comp.NewSession()
	// mockBackend.NewSession returns "", nil — just verify no panic.
}

func TestComposite_RouteSendText(t *testing.T) {
	primary := &trackingBackend{}
	itermInteg := &trackingBackend{}

	comp := NewCompositeBackend(primary, "pty", []Integration{
		{Prefix: "iterm:", Source: "iterm", Backend: itermInteg},
	})

	// Send to integration session.
	_ = comp.SendText("iterm:session-abc", "hello")
	if itermInteg.lastSendID != "session-abc" {
		t.Errorf("expected iterm send to 'session-abc', got %q", itermInteg.lastSendID)
	}
	if itermInteg.lastSendText != "hello" {
		t.Errorf("expected iterm send text 'hello', got %q", itermInteg.lastSendText)
	}

	// Send to primary session.
	_ = comp.SendText("pty-0", "world")
	if primary.lastSendID != "pty-0" {
		t.Errorf("expected primary send to 'pty-0', got %q", primary.lastSendID)
	}
}

func TestComposite_RouteFocusSession(t *testing.T) {
	primary := &trackingBackend{}
	tmuxInteg := &trackingBackend{}

	comp := NewCompositeBackend(primary, "pty", []Integration{
		{Prefix: "tmux:", Source: "tmux", Backend: tmuxInteg},
	})

	_ = comp.FocusSession("tmux:%5")
	if tmuxInteg.lastFocusID != "%5" {
		t.Errorf("expected tmux focus '%%5', got %q", tmuxInteg.lastFocusID)
	}

	_ = comp.FocusSession("pty-0")
	if primary.lastFocusID != "pty-0" {
		t.Errorf("expected primary focus 'pty-0', got %q", primary.lastFocusID)
	}
}

func TestComposite_RouteReadScreen(t *testing.T) {
	primary := &trackingBackend{readScreen: "primary content"}
	itermInteg := &trackingBackend{readScreen: "iterm content"}

	comp := NewCompositeBackend(primary, "pty", []Integration{
		{Prefix: "iterm:", Source: "iterm", Backend: itermInteg},
	})

	content, err := comp.ReadScreen("iterm:session-1", 25)
	if err != nil {
		t.Fatal(err)
	}
	if content != "iterm content" {
		t.Errorf("expected 'iterm content', got %q", content)
	}
	if itermInteg.lastReadID != "session-1" {
		t.Errorf("expected read ID 'session-1', got %q", itermInteg.lastReadID)
	}
}

func TestComposite_PrimarySource(t *testing.T) {
	ptyBackend := &trackingBackend{}
	itermBackend := &trackingBackend{}

	// PTY as primary — source is "pty".
	comp := NewCompositeBackend(ptyBackend, "pty", []Integration{
		{Prefix: "iterm:", Source: "iterm", Backend: itermBackend},
	})
	if src := comp.PrimarySource(); src != "pty" {
		t.Errorf("expected 'pty', got %q", src)
	}

	// iterm as primary (non-PTY launch) — source is "iterm".
	comp2 := NewCompositeBackend(itermBackend, "iterm", []Integration{
		{Prefix: "pty:", Source: "pty", Backend: ptyBackend},
	})
	if src := comp2.PrimarySource(); src != "iterm" {
		t.Errorf("expected 'iterm', got %q", src)
	}
}

func TestComposite_MultipleIntegrations(t *testing.T) {
	primary := &trackingBackend{
		mockBackend: mockBackend{
			sessions: []Session{{ID: "pty-0", Name: "claude"}},
		},
	}
	itermInteg := &trackingBackend{
		mockBackend: mockBackend{
			sessions: []Session{{ID: "s1", Name: "✳ agent1", TTY: "/dev/ttys001"}},
		},
	}
	tmuxInteg := &trackingBackend{
		mockBackend: mockBackend{
			sessions: []Session{{ID: "%0", Name: "codex", TTY: "/dev/pts/2"}},
		},
	}

	comp := NewCompositeBackend(primary, "pty", []Integration{
		{Prefix: "iterm:", Source: "iterm", Backend: itermInteg},
		{Prefix: "tmux:", Source: "tmux", Backend: tmuxInteg},
	})

	sessions, err := comp.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(sessions))
	}

	// Verify routing works for each.
	_ = comp.SendText("iterm:s1", "a")
	if itermInteg.lastSendID != "s1" {
		t.Errorf("expected iterm route, got %q", itermInteg.lastSendID)
	}

	_ = comp.SendText("tmux:%0", "b")
	if tmuxInteg.lastSendID != "%0" {
		t.Errorf("expected tmux route, got %q", tmuxInteg.lastSendID)
	}

	_ = comp.SendText("pty-0", "c")
	if primary.lastSendID != "pty-0" {
		t.Errorf("expected primary route, got %q", primary.lastSendID)
	}
}

func TestComposite_PrimarySourceLabeledCorrectly(t *testing.T) {
	// When primary is non-PTY, its sessions should be labeled with that source.
	tmuxPrimary := &trackingBackend{
		mockBackend: mockBackend{
			sessions: []Session{{ID: "%0", Name: "claude"}},
		},
	}
	ptyInteg := &trackingBackend{
		mockBackend: mockBackend{
			sessions: []Session{{ID: "pty-0", Name: "codex"}},
		},
	}

	comp := NewCompositeBackend(tmuxPrimary, "tmux", []Integration{
		{Prefix: "pty:", Source: "pty", Backend: ptyInteg},
	})

	sessions, err := comp.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}

	// Primary session (tmux) should be labeled "tmux", not "pty".
	if sessions[0].Source != "tmux" {
		t.Errorf("expected primary source 'tmux', got %q", sessions[0].Source)
	}
	// PTY integration session should be labeled "pty".
	if sessions[1].Source != "pty" {
		t.Errorf("expected integration source 'pty', got %q", sessions[1].Source)
	}
}

func TestComposite_AddRemoveIntegration(t *testing.T) {
	primary := &trackingBackend{
		mockBackend: mockBackend{
			sessions: []Session{{ID: "pty-0", Name: "claude"}},
		},
	}

	comp := NewCompositeBackend(primary, "pty", nil)

	// Initially no integrations.
	sessions, _ := comp.ListSessions()
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}

	// Add an integration.
	itermInteg := &trackingBackend{
		mockBackend: mockBackend{
			sessions: []Session{{ID: "s1", Name: "✳ agent1", TTY: "/dev/ttys001"}},
		},
	}
	comp.AddIntegration(Integration{Prefix: "iterm:", Source: "iterm", Backend: itermInteg})

	sessions, _ = comp.ListSessions()
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions after add, got %d", len(sessions))
	}

	// Remove the integration.
	comp.RemoveIntegration("iterm:")

	sessions, _ = comp.ListSessions()
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session after remove, got %d", len(sessions))
	}

	// Verify Integrations() returns a snapshot.
	integs := comp.Integrations()
	if len(integs) != 0 {
		t.Errorf("expected 0 integrations, got %d", len(integs))
	}
}

// resizableBackend extends trackingBackend with Resize support.
type resizableBackend struct {
	trackingBackend
	resizeCols int
	resizeRows int
}

func (r *resizableBackend) Resize(cols, rows int) {
	r.resizeCols = cols
	r.resizeRows = rows
}

func TestComposite_ResizeForwardsToIntegrations(t *testing.T) {
	primary := &trackingBackend{}
	ptyInteg := &resizableBackend{}

	comp := NewCompositeBackend(primary, "tmux", []Integration{
		{Prefix: "pty:", Source: "pty", Backend: ptyInteg},
	})

	comp.Resize(200, 50)

	if ptyInteg.resizeCols != 200 || ptyInteg.resizeRows != 50 {
		t.Errorf("expected PTY integration to receive resize(200,50), got (%d,%d)",
			ptyInteg.resizeCols, ptyInteg.resizeRows)
	}
}

func TestComposite_SetPrimary(t *testing.T) {
	pty := &trackingBackend{}
	tmuxB := &trackingBackend{}

	comp := NewCompositeBackend(pty, "pty", nil)
	if comp.PrimarySource() != "pty" {
		t.Errorf("expected pty, got %q", comp.PrimarySource())
	}

	comp.SetPrimary(tmuxB, "tmux")
	if comp.PrimarySource() != "tmux" {
		t.Errorf("expected tmux, got %q", comp.PrimarySource())
	}
}

func TestComposite_RouteEdgeCases(t *testing.T) {
	primary := &trackingBackend{}
	tmuxInteg := &trackingBackend{}

	comp := NewCompositeBackend(primary, "pty", []Integration{
		{Prefix: "tmux:", Source: "tmux", Backend: tmuxInteg},
	})

	tests := []struct {
		name      string
		sessionID string
		wantErr   bool
		wantDest  string // "primary" or "tmux"
		wantID    string // expected unprefixed ID
	}{
		{
			"path-like ID with colon routes to primary",
			"/some/path:extra",
			false,
			"primary",
			"/some/path:extra",
		},
		{
			"unrecognized integration prefix returns error",
			"unknown:sess-1",
			true,
			"",
			"",
		},
		{
			"no colon plain ID routes to primary",
			"pty-0",
			false,
			"primary",
			"pty-0",
		},
		{
			"empty string routes to primary",
			"",
			false,
			"primary",
			"",
		},
		{
			"registered prefix routes to integration",
			"tmux:win-1",
			false,
			"tmux",
			"win-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset tracking.
			primary.lastSendID = ""
			primary.lastSendText = ""
			tmuxInteg.lastSendID = ""
			tmuxInteg.lastSendText = ""

			err := comp.SendText(tt.sessionID, "test")
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			switch tt.wantDest {
			case "primary":
				if primary.lastSendID != tt.wantID {
					t.Errorf("expected primary got ID %q, got %q", tt.wantID, primary.lastSendID)
				}
			case "tmux":
				if tmuxInteg.lastSendID != tt.wantID {
					t.Errorf("expected tmux got ID %q, got %q", tt.wantID, tmuxInteg.lastSendID)
				}
			}
		})
	}
}

func TestComposite_NewSessionOn_Primary(t *testing.T) {
	primary := &trackingBackend{
		mockBackend: mockBackend{newSessionID: "pty-0"},
	}

	comp := NewCompositeBackend(primary, "pty", []Integration{
		{Prefix: "tmux:", Source: "tmux", Backend: &trackingBackend{}},
	})

	id, err := comp.NewSessionOn("pty")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "pty-0" {
		t.Errorf("expected 'pty-0', got %q", id)
	}
}

func TestComposite_NewSessionOn_Integration(t *testing.T) {
	primary := &trackingBackend{}
	ptyInteg := &trackingBackend{
		mockBackend: mockBackend{newSessionID: "pty-1"},
	}

	comp := NewCompositeBackend(primary, "tmux", []Integration{
		{Prefix: "pty:", Source: "pty", Backend: ptyInteg},
	})

	id, err := comp.NewSessionOn("pty")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "pty:pty-1" {
		t.Errorf("expected 'pty:pty-1', got %q", id)
	}
}

func TestComposite_NewSessionOn_Unknown(t *testing.T) {
	primary := &trackingBackend{}

	comp := NewCompositeBackend(primary, "pty", nil)

	_, err := comp.NewSessionOn("nosuch")
	if err == nil {
		t.Fatal("expected error for unknown source, got nil")
	}
	if !strings.Contains(err.Error(), "nosuch") {
		t.Errorf("expected error to mention 'nosuch', got %q", err.Error())
	}
}

func TestComposite_FailedSourcesTracked(t *testing.T) {
	primary := &trackingBackend{
		mockBackend: mockBackend{
			sessions: []Session{{ID: "pty-0", Name: "claude"}},
		},
	}
	failingInteg := &trackingBackend{
		listErr: errors.New("connection refused"),
	}
	healthyInteg := &trackingBackend{
		mockBackend: mockBackend{
			sessions: []Session{{ID: "s1", Name: "codex", TTY: "/dev/ttys001"}},
		},
	}

	comp := NewCompositeBackend(primary, "pty", []Integration{
		{Prefix: "iterm:", Source: "iterm", Backend: failingInteg},
		{Prefix: "tmux:", Source: "tmux", Backend: healthyInteg},
	})

	sessions, err := comp.ListSessions()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should have primary + healthy integration, not the failing one.
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}

	failed := comp.FailedSources()
	if len(failed) != 1 || failed[0] != "iterm" {
		t.Errorf("expected FailedSources [iterm], got %v", failed)
	}
}

func TestComposite_FailedSourcesClearedOnSuccess(t *testing.T) {
	integ := &trackingBackend{
		listErr: errors.New("connection refused"),
	}
	primary := &trackingBackend{
		mockBackend: mockBackend{sessions: []Session{{ID: "pty-0"}}},
	}

	comp := NewCompositeBackend(primary, "pty", []Integration{
		{Prefix: "iterm:", Source: "iterm", Backend: integ},
	})

	// First call — integration fails.
	_, _ = comp.ListSessions()
	if len(comp.FailedSources()) != 1 {
		t.Fatal("expected 1 failed source")
	}

	// Fix the integration.
	integ.listErr = nil
	integ.sessions = []Session{{ID: "s1", Name: "claude"}}

	// Second call — integration succeeds.
	_, _ = comp.ListSessions()
	if len(comp.FailedSources()) != 0 {
		t.Errorf("expected 0 failed sources after recovery, got %v", comp.FailedSources())
	}
}

func TestComposite_SelfTTYFiltering(t *testing.T) {
	tests := []struct {
		name     string
		selfTTY  string
		primary  []Session
		integ    []Session
		wantIDs  []string
	}{
		{
			name:    "matching primary TTY removed",
			selfTTY: "/dev/ttys000",
			primary: []Session{
				{ID: "pty-0", Name: "claude", TTY: "/dev/ttys000"},
				{ID: "pty-1", Name: "codex", TTY: "/dev/ttys001"},
			},
			integ:   nil,
			wantIDs: []string{"pty-1"},
		},
		{
			name:    "matching integration TTY removed",
			selfTTY: "/dev/ttys000",
			primary: []Session{
				{ID: "pty-0", Name: "claude", TTY: "/dev/ttys001"},
			},
			integ: []Session{
				{ID: "s1", Name: "copilot", TTY: "/dev/ttys000"},
				{ID: "s2", Name: "codex", TTY: "/dev/ttys002"},
			},
			wantIDs: []string{"pty-0", "iterm:s2"},
		},
		{
			name:    "different TTY kept",
			selfTTY: "/dev/ttys000",
			primary: []Session{
				{ID: "pty-0", Name: "claude", TTY: "/dev/ttys001"},
			},
			integ: []Session{
				{ID: "s1", Name: "codex", TTY: "/dev/ttys002"},
			},
			wantIDs: []string{"pty-0", "iterm:s1"},
		},
		{
			name:    "empty session TTY kept",
			selfTTY: "/dev/ttys000",
			primary: []Session{
				{ID: "pty-0", Name: "claude", TTY: ""},
			},
			integ: []Session{
				{ID: "s1", Name: "codex", TTY: ""},
			},
			wantIDs: []string{"pty-0", "iterm:s1"},
		},
		{
			name:    "empty selfTTY no filtering",
			selfTTY: "",
			primary: []Session{
				{ID: "pty-0", Name: "claude", TTY: "/dev/ttys000"},
			},
			integ: []Session{
				{ID: "s1", Name: "codex", TTY: "/dev/ttys000"},
			},
			// No self-filtering, but TTY dedup still applies.
			wantIDs: []string{"pty-0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			primary := &trackingBackend{
				mockBackend: mockBackend{sessions: tt.primary},
			}
			var integrations []Integration
			if tt.integ != nil {
				integrations = []Integration{
					{Prefix: "iterm:", Source: "iterm", Backend: &trackingBackend{
						mockBackend: mockBackend{sessions: tt.integ},
					}},
				}
			}
			comp := NewCompositeBackend(primary, "pty", integrations)
			comp.SetSelfTTY(tt.selfTTY)

			sessions, err := comp.ListSessions()
			if err != nil {
				t.Fatal(err)
			}
			var gotIDs []string
			for _, s := range sessions {
				gotIDs = append(gotIDs, s.ID)
			}
			if len(gotIDs) != len(tt.wantIDs) {
				t.Fatalf("expected %d sessions %v, got %d %v", len(tt.wantIDs), tt.wantIDs, len(gotIDs), gotIDs)
			}
			for i, id := range gotIDs {
				if id != tt.wantIDs[i] {
					t.Errorf("session[%d] = %q, want %q", i, id, tt.wantIDs[i])
				}
			}
		})
	}
}

func TestComposite_MissingIntegrationReturnsError(t *testing.T) {
	primary := &trackingBackend{}

	// No integrations registered — iterm: prefix is unrecognized.
	comp := NewCompositeBackend(primary, "pty", nil)

	err := comp.SendText("iterm:session-abc", "hello")
	if err == nil {
		t.Fatal("expected error for missing integration, got nil")
	}
	if !strings.Contains(err.Error(), "iterm") {
		t.Errorf("expected error to mention 'iterm', got %q", err.Error())
	}

	_, err = comp.ReadScreen("tmux:%5", 25)
	if err == nil {
		t.Fatal("expected error for missing integration, got nil")
	}

	err = comp.FocusSession("tmux:%5")
	if err == nil {
		t.Fatal("expected error for missing integration, got nil")
	}

	// Unprefixed IDs still route to primary.
	err = comp.SendText("pty-0", "hello")
	if err != nil {
		t.Fatalf("unexpected error for primary session: %v", err)
	}
}
