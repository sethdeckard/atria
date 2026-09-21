package model

import (
	"strings"
	"time"

	"github.com/sethdeckard/atria/libatria/agent"
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
	ProjectDir       string       `json:"project_dir"`
	SessionID        string       `json:"session_id"`
	Type             agent.Type   `json:"type"`
	Status           agent.Status `json:"-"`
	Activity         string       `json:"-"`
	Attention        string       `json:"-"`
	MonitorPID       int          `json:"-"`
	MonitorLog       string       `json:"-"`
	LastActivity     time.Time    `json:"-"`
	ScreenChecked    bool         `json:"-"`
	LastScreen       string       `json:"-"`
	LastScreenStyled string       `json:"-"` // screen content with SGR color escapes (display only)
	LastScreenRead   time.Time    `json:"-"`
	UnmatchedReads   int          `json:"-"` // consecutive screen reads with no agent pattern
	OrphanTicks      int          `json:"-"` // consecutive refreshes where name doesn't match agent while idle
	Source           string       `json:"-"` // "pty", "iterm", "tmux"
}
