package model

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAddRemoveProject(t *testing.T) {
	s := NewStore(t.TempDir())

	p := s.AddProject("/tmp/myapp")
	if p == nil {
		t.Fatal("expected project, got nil")
	}
	if p.Name != "myapp" {
		t.Fatalf("expected name myapp, got %s", p.Name)
	}
	if p.Dir != "/tmp/myapp" {
		t.Fatalf("expected dir /tmp/myapp, got %s", p.Dir)
	}
	if p.AddedAt.IsZero() {
		t.Fatal("expected AddedAt to be set")
	}
	if len(s.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(s.Projects))
	}

	// Adding the same dir again should return the existing project.
	p2 := s.AddProject("/tmp/myapp")
	if p2 != p {
		t.Fatal("expected same project pointer on duplicate add")
	}
	if len(s.Projects) != 1 {
		t.Fatalf("expected 1 project after duplicate add, got %d", len(s.Projects))
	}

	s.RemoveProject("/tmp/myapp")
	if len(s.Projects) != 0 {
		t.Fatalf("expected 0 projects after remove, got %d", len(s.Projects))
	}
}

func TestProjectPersistence(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.AddProject("/home/user/alpha")
	s.AddProject("/home/user/beta")

	if err := s.SaveProjects(); err != nil {
		t.Fatalf("SaveProjects: %v", err)
	}

	// Verify the file exists.
	if _, err := os.Stat(filepath.Join(dir, "projects.json")); err != nil {
		t.Fatalf("projects.json not found: %v", err)
	}

	// Reload into a fresh store.
	s2 := NewStore(dir)
	if err := s2.LoadProjects(); err != nil {
		t.Fatalf("LoadProjects: %v", err)
	}
	if len(s2.Projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(s2.Projects))
	}
	if s2.Projects[0].Dir != "/home/user/alpha" {
		t.Fatalf("expected alpha, got %s", s2.Projects[0].Dir)
	}
	if s2.Projects[1].Dir != "/home/user/beta" {
		t.Fatalf("expected beta, got %s", s2.Projects[1].Dir)
	}
}

func TestSessionPersistence(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	now := time.Now()
	s.SetSession(&AgentSession{
		ProjectDir: "/proj/a",
		SessionID:  "sess-001",
		Type:       AgentClaude,
		Status:     StatusWorking,
		LastSent:   now,
	})

	if err := s.SaveSessions(); err != nil {
		t.Fatalf("SaveSessions: %v", err)
	}

	s2 := NewStore(dir)
	if err := s2.LoadSessions(); err != nil {
		t.Fatalf("LoadSessions: %v", err)
	}
	if len(s2.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(s2.Sessions))
	}
	sess := s2.Sessions[0]
	if sess.SessionID != "sess-001" {
		t.Fatalf("expected sess-001, got %s", sess.SessionID)
	}
	if sess.Type != AgentClaude {
		t.Fatalf("expected claude, got %s", sess.Type)
	}
	// Status is json:"-", so it should be zero value after reload.
	if sess.Status != "" {
		t.Fatalf("expected empty status after reload, got %s", sess.Status)
	}
}

func TestDisplayName(t *testing.T) {
	pGo := &Project{Name: "myapp", Dir: "/go/myapp"}
	pRuby := &Project{Name: "myapp", Dir: "/ruby/myapp"}
	pUnique := &Project{Name: "other", Dir: "/home/other"}

	all := []*Project{pGo, pRuby, pUnique}

	if got := pGo.DisplayName(all); got != "myapp (go)" {
		t.Fatalf("expected 'myapp (go)', got %q", got)
	}
	if got := pRuby.DisplayName(all); got != "myapp (ruby)" {
		t.Fatalf("expected 'myapp (ruby)', got %q", got)
	}
	if got := pUnique.DisplayName(all); got != "other" {
		t.Fatalf("expected 'other', got %q", got)
	}
}

func TestFindProject(t *testing.T) {
	s := NewStore(t.TempDir())

	s.AddProject("/a/b/c")
	s.AddProject("/x/y/z")

	if p := s.FindProject("/a/b/c"); p == nil || p.Dir != "/a/b/c" {
		t.Fatal("expected to find /a/b/c")
	}
	if p := s.FindProject("/no/such"); p != nil {
		t.Fatal("expected nil for unknown dir")
	}
}

func TestSetGetSession(t *testing.T) {
	s := NewStore(t.TempDir())

	sess := &AgentSession{
		ProjectDir: "/proj/x",
		SessionID:  "s1",
		Type:       AgentCodex,
		Status:     StatusIdle,
	}
	s.SetSession(sess)

	got := s.GetSession("/proj/x")
	if got == nil || got.SessionID != "s1" {
		t.Fatal("expected to get session s1")
	}

	// Add second session for same project (different agent).
	sess2 := &AgentSession{
		ProjectDir: "/proj/x",
		SessionID:  "s2",
		Type:       AgentCodex,
		Status:     StatusWorking,
	}
	s.SetSession(sess2)
	if len(s.Sessions) != 2 {
		t.Fatalf("expected 2 sessions after adding second agent, got %d", len(s.Sessions))
	}
	sessions := s.GetSessions("/proj/x")
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions for project, got %d", len(sessions))
	}

	// Update existing session by same SessionID.
	sess2updated := &AgentSession{
		ProjectDir: "/proj/x",
		SessionID:  "s2",
		Type:       AgentCodex,
		Status:     StatusIdle,
	}
	s.SetSession(sess2updated)
	if len(s.Sessions) != 2 {
		t.Fatalf("expected 2 sessions after update, got %d", len(s.Sessions))
	}

	// SessionByID
	if found := s.SessionByID("s2"); found == nil || found.ProjectDir != "/proj/x" {
		t.Fatal("SessionByID failed")
	}
	if found := s.SessionByID("no-such"); found != nil {
		t.Fatal("expected nil for unknown session ID")
	}

	// TrackedSessionIDs
	s.SetSession(&AgentSession{ProjectDir: "/proj/y", SessionID: "s3"})
	ids := s.TrackedSessionIDs()
	if len(ids) != 3 {
		t.Fatalf("expected 3 IDs, got %d", len(ids))
	}

	// RemoveSession by session ID
	s.RemoveSession("s1")
	if s.SessionByID("s1") != nil {
		t.Fatal("expected session s1 removed")
	}
	if len(s.Sessions) != 2 {
		t.Fatalf("expected 2 sessions after remove, got %d", len(s.Sessions))
	}
}
