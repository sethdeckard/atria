package model

import (
	"path/filepath"
	"time"
)

type AgentType string

const (
	AgentClaude   AgentType = "claude"
	AgentCodex    AgentType = "codex"
	AgentOpenCode AgentType = "opencode"
)

type AgentStatus string

const (
	StatusWorking    AgentStatus = "working"
	StatusIdle       AgentStatus = "idle"
	StatusNeedsInput AgentStatus = "needs_input"
	StatusError      AgentStatus = "error"
)

type Project struct {
	Name           string    `json:"name"`
	Dir            string    `json:"dir"`
	AddedAt        time.Time `json:"added_at"`
	LastLaunchedAt time.Time `json:"last_launched_at,omitempty"`
}

// DisplayName returns a unique name. If multiple projects share the same
// basename, disambiguate with the shortest unique parent suffix.
func (p *Project) DisplayName(all []*Project) string {
	base := filepath.Base(p.Dir)

	// Check if any other project shares the same basename.
	var conflicts []*Project
	for _, other := range all {
		if other.Dir != p.Dir && filepath.Base(other.Dir) == base {
			conflicts = append(conflicts, other)
		}
	}

	if len(conflicts) == 0 {
		return base
	}

	// Disambiguate using the parent directory name.
	parent := filepath.Base(filepath.Dir(p.Dir))
	return base + " (" + parent + ")"
}

type AgentSession struct {
	ProjectDir    string      `json:"project_dir"`
	SessionID     string      `json:"session_id"`
	Type          AgentType   `json:"type"`
	Status        AgentStatus `json:"-"`
	Activity      string      `json:"-"`
	Attention     string      `json:"-"`
	MonitorPID    int         `json:"-"`
	MonitorLog    string      `json:"-"`
	LastSent      time.Time   `json:"last_sent"`
	LastActivity  time.Time   `json:"-"`
	ScreenChecked  bool        `json:"-"`
	InitialStatus  AgentStatus `json:"-"`
	LastScreen     string      `json:"-"`
	UnmatchedReads int         `json:"-"` // consecutive screen reads with no agent pattern
}

type ChatEntry struct {
	Timestamp time.Time
	Direction string // "sent" | "received"
	Text      string
}
