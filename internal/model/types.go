package model

import (
	"strings"
	"time"

	"github.com/sethdeckard/atria/libatria/watch"
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

// AgentSession is one tracked agent: where it lives, which terminal session
// it is, and the watch.Tracker that holds its type, status, and screen state.
// Sessions are not persisted (see Store).
type AgentSession struct {
	ProjectDir string
	SessionID  string
	watch.Tracker
	LastScreenStyled string // screen content with SGR color escapes (display only)
	MonitorPID       int
	MonitorLog       string
}
