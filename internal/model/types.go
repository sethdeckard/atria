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

// DisplayName returns the project's directory basename.
func (p *Project) DisplayName() string {
	return filepath.Base(p.Dir)
}

type AgentSession struct {
	ProjectDir     string      `json:"project_dir"`
	SessionID      string      `json:"session_id"`
	Type           AgentType   `json:"type"`
	Status         AgentStatus `json:"-"`
	Activity       string      `json:"-"`
	Attention      string      `json:"-"`
	MonitorPID     int         `json:"-"`
	MonitorLog     string      `json:"-"`
	LastActivity   time.Time   `json:"-"`
	ScreenChecked  bool        `json:"-"`
	LastScreen     string      `json:"-"`
	UnmatchedReads int         `json:"-"` // consecutive screen reads with no agent pattern
	OrphanTicks    int         `json:"-"` // consecutive refreshes where name doesn't match agent while idle
	Source         string      `json:"-"` // "pty", "iterm", "tmux"
}
