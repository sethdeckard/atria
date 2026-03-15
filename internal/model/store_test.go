package model

import (
	"os"
	"path/filepath"
	"testing"
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
	dir := filepath.Join(t.TempDir(), "data")
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

	fileInfo, err := os.Stat(filepath.Join(dir, "projects.json"))
	if err != nil {
		t.Fatalf("Stat projects.json: %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected projects.json mode 0600, got %o", got)
	}

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat data dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("expected data dir mode 0700, got %o", got)
	}
}

func TestDisplayName(t *testing.T) {
	p := &Project{Name: "myapp", Dir: "/go/myapp"}
	if got := p.DisplayName(); got != "go/myapp" {
		t.Fatalf("expected 'go/myapp', got %q", got)
	}

	// Short path with ≤2 segments returns as-is
	p2 := &Project{Name: "myapp", Dir: "/myapp"}
	if got := p2.DisplayName(); got != "/myapp" {
		t.Fatalf("expected '/myapp', got %q", got)
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

	got := s.FirstSession("/proj/x")
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

	// Verify all tracked sessions
	s.SetSession(&AgentSession{ProjectDir: "/proj/y", SessionID: "s3"})
	if len(s.Sessions) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(s.Sessions))
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
