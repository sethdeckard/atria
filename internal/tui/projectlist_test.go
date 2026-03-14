package tui

import (
	"testing"

	"github.com/sethdeckard/atria/internal/model"
)

// mockStore implements the buildRows store interface for testing.
type mockStore struct {
	projects []*model.Project
	sessions map[string][]*model.AgentSession
}

func (m *mockStore) Projects() []*model.Project { return m.projects }
func (m *mockStore) GetSessions(dir string) []*model.AgentSession {
	return m.sessions[dir]
}

func TestBuildRows(t *testing.T) {
	t.Run("empty store", func(t *testing.T) {
		s := &mockStore{}
		rows := buildRows(s)
		if len(rows) != 0 {
			t.Errorf("expected 0 rows, got %d", len(rows))
		}
	})

	t.Run("project with no sessions", func(t *testing.T) {
		s := &mockStore{
			projects: []*model.Project{{Name: "foo", Dir: "/tmp/foo"}},
			sessions: map[string][]*model.AgentSession{},
		}
		rows := buildRows(s)
		if len(rows) != 0 {
			t.Errorf("expected 0 rows (no sessions), got %d", len(rows))
		}
	})

	t.Run("project with sessions", func(t *testing.T) {
		s := &mockStore{
			projects: []*model.Project{{Name: "foo", Dir: "/tmp/foo"}},
			sessions: map[string][]*model.AgentSession{
				"/tmp/foo": {
					{SessionID: "s1", Type: model.AgentClaude},
					{SessionID: "s2", Type: model.AgentCodex},
				},
			},
		}
		rows := buildRows(s)
		if len(rows) != 2 {
			t.Fatalf("expected 2 rows, got %d", len(rows))
		}
		if rows[0].session.SessionID != "s1" || rows[1].session.SessionID != "s2" {
			t.Error("rows should preserve session order")
		}
	})

	t.Run("three duplicates get #2 #3 suffixes", func(t *testing.T) {
		s := &mockStore{
			projects: []*model.Project{
				{Name: "svc", Dir: "/a/svc"},
				{Name: "svc", Dir: "/b/svc"},
				{Name: "svc", Dir: "/c/svc"},
			},
			sessions: map[string][]*model.AgentSession{
				"/a/svc": {{SessionID: "s1", Type: model.AgentClaude}},
				"/b/svc": {{SessionID: "s2", Type: model.AgentClaude}},
				"/c/svc": {{SessionID: "s3", Type: model.AgentClaude}},
			},
		}
		rows := buildRows(s)
		if len(rows) != 3 {
			t.Fatalf("expected 3 rows, got %d", len(rows))
		}
		if rows[0].displayName != "svc" {
			t.Errorf("first should be 'svc', got %q", rows[0].displayName)
		}
		if rows[1].displayName != "svc #2" {
			t.Errorf("second should be 'svc #2', got %q", rows[1].displayName)
		}
		if rows[2].displayName != "svc #3" {
			t.Errorf("third should be 'svc #3', got %q", rows[2].displayName)
		}
	})

	t.Run("unique names not disambiguated", func(t *testing.T) {
		s := &mockStore{
			projects: []*model.Project{
				{Name: "alpha", Dir: "/a/alpha"},
				{Name: "beta", Dir: "/b/beta"},
			},
			sessions: map[string][]*model.AgentSession{
				"/a/alpha": {{SessionID: "s1", Type: model.AgentClaude}},
				"/b/beta":  {{SessionID: "s2", Type: model.AgentClaude}},
			},
		}
		rows := buildRows(s)
		if rows[0].displayName != "alpha" || rows[1].displayName != "beta" {
			t.Errorf("unique names should not be modified, got %q and %q", rows[0].displayName, rows[1].displayName)
		}
	})
}

func TestAgentTypeLabel(t *testing.T) {
	tests := []struct {
		agentType model.AgentType
		expected  string
	}{
		{model.AgentClaude, "Claude"},
		{model.AgentCodex, "Codex"},
		{model.AgentOpenCode, "OpenCode"},
		{model.AgentCopilot, "Copilot"},
		{"mystery", "Mystery"}, // fallback capitalizes first letter
	}
	for _, tt := range tests {
		t.Run(string(tt.agentType), func(t *testing.T) {
			got := agentTypeLabel(tt.agentType)
			if got != tt.expected {
				t.Errorf("agentTypeLabel(%q) = %q, want %q", tt.agentType, got, tt.expected)
			}
		})
	}
}

func TestAgentTypeStyle(t *testing.T) {
	t.Run("known type returns specific style", func(t *testing.T) {
		style := agentTypeStyle(model.AgentClaude)
		if style.GetForeground() == normalStyle.GetForeground() {
			t.Error("expected a distinct style for Claude, got normalStyle")
		}
	})

	t.Run("unknown type returns normalStyle", func(t *testing.T) {
		style := agentTypeStyle("unknown")
		if style.GetForeground() != normalStyle.GetForeground() {
			t.Error("expected normalStyle for unknown agent type")
		}
	})
}

func TestPadToWidth(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		width    int
		wantLen  int
		wantSame bool // true if input should be returned unchanged
	}{
		{"shorter than width", "hi", 10, 10, false},
		{"equal to width", "hello", 5, 5, true},
		{"longer than width", "hello world", 5, 11, true},
		{"empty string", "", 5, 5, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := padToWidth(tt.input, tt.width)
			if tt.wantSame && got != tt.input {
				t.Errorf("expected unchanged input, got %q", got)
			}
			if len(got) != tt.wantLen {
				t.Errorf("padToWidth(%q, %d) len = %d, want %d", tt.input, tt.width, len(got), tt.wantLen)
			}
		})
	}
}
