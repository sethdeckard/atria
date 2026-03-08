package model

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Store holds projects and sessions in memory with JSON persistence.
type Store struct {
	Projects []*Project
	Sessions []*AgentSession
	dataDir  string
}

// NewStore creates a new Store backed by the given data directory.
func NewStore(dataDir string) *Store {
	return &Store{
		dataDir: dataDir,
	}
}

func (s *Store) projectsPath() string {
	return filepath.Join(s.dataDir, "projects.json")
}

func (s *Store) sessionsPath() string {
	return filepath.Join(s.dataDir, "sessions.json")
}

// LoadProjects reads projects from dataDir/projects.json.
func (s *Store) LoadProjects() error {
	data, err := os.ReadFile(s.projectsPath())
	if err != nil {
		if os.IsNotExist(err) {
			s.Projects = nil
			return nil
		}
		return err
	}
	return json.Unmarshal(data, &s.Projects)
}

// SaveProjects writes projects to dataDir/projects.json, creating the
// directory if needed.
func (s *Store) SaveProjects() error {
	if err := os.MkdirAll(s.dataDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.Projects, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.projectsPath(), data, 0o644)
}

// AddProject adds a project for dir if it is not already tracked. It sets
// Name from the basename and AddedAt to the current time.
func (s *Store) AddProject(dir string) *Project {
	if p := s.FindProject(dir); p != nil {
		return p
	}
	p := &Project{
		Name:    filepath.Base(dir),
		Dir:     dir,
		AddedAt: time.Now(),
	}
	s.Projects = append(s.Projects, p)
	return p
}

// RemoveProject removes the project with the given directory.
func (s *Store) RemoveProject(dir string) {
	for i, p := range s.Projects {
		if p.Dir == dir {
			s.Projects = append(s.Projects[:i], s.Projects[i+1:]...)
			return
		}
	}
}

// FindProject returns the project with the given directory, or nil.
func (s *Store) FindProject(dir string) *Project {
	for _, p := range s.Projects {
		if p.Dir == dir {
			return p
		}
	}
	return nil
}

// LoadSessions reads sessions from dataDir/sessions.json.
func (s *Store) LoadSessions() error {
	data, err := os.ReadFile(s.sessionsPath())
	if err != nil {
		if os.IsNotExist(err) {
			s.Sessions = nil
			return nil
		}
		return err
	}
	return json.Unmarshal(data, &s.Sessions)
}

// SaveSessions writes sessions to dataDir/sessions.json.
func (s *Store) SaveSessions() error {
	if err := os.MkdirAll(s.dataDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.Sessions, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.sessionsPath(), data, 0o644)
}

// SetSession adds or updates a session keyed by ProjectDir.
func (s *Store) SetSession(session *AgentSession) {
	for i, existing := range s.Sessions {
		if existing.ProjectDir == session.ProjectDir {
			s.Sessions[i] = session
			return
		}
	}
	s.Sessions = append(s.Sessions, session)
}

// GetSession returns the session for the given project directory, or nil.
func (s *Store) GetSession(projectDir string) *AgentSession {
	for _, sess := range s.Sessions {
		if sess.ProjectDir == projectDir {
			return sess
		}
	}
	return nil
}

// RemoveSession removes the session for the given project directory.
func (s *Store) RemoveSession(projectDir string) {
	for i, sess := range s.Sessions {
		if sess.ProjectDir == projectDir {
			s.Sessions = append(s.Sessions[:i], s.Sessions[i+1:]...)
			return
		}
	}
}

// SessionByID returns the session with the given session ID, or nil.
func (s *Store) SessionByID(sessionID string) *AgentSession {
	for _, sess := range s.Sessions {
		if sess.SessionID == sessionID {
			return sess
		}
	}
	return nil
}

// TrackedSessionIDs returns the session IDs of all tracked sessions.
func (s *Store) TrackedSessionIDs() []string {
	ids := make([]string, 0, len(s.Sessions))
	for _, sess := range s.Sessions {
		ids = append(ids, sess.SessionID)
	}
	return ids
}
