package model

import (
	"strings"
	"time"
)

type AgentType string

const (
	AgentClaude   AgentType = "claude"
	AgentCodex    AgentType = "codex"
	AgentOpenCode AgentType = "opencode"
	AgentCopilot  AgentType = "copilot"
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

// DisplayName returns the last two path segments of the project directory.
func (p *Project) DisplayName() string {
	parts := strings.Split(p.Dir, "/")
	// Remove trailing empty from trailing slash
	for len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) <= 2 {
		return p.Dir
	}
	return strings.Join(parts[len(parts)-2:], "/")
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
	LastScreenRead time.Time   `json:"-"`
	UnmatchedReads int         `json:"-"` // consecutive screen reads with no agent pattern
	OrphanTicks    int         `json:"-"` // consecutive refreshes where name doesn't match agent while idle
	Source         string      `json:"-"` // "pty", "iterm", "tmux"
}
