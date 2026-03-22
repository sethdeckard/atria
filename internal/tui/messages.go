package tui

import (
	"github.com/sethdeckard/atria/internal/model"
	"github.com/sethdeckard/atria/internal/terminal"
)

// SessionsRefreshedMsg is sent when the session list has been refreshed.
type SessionsRefreshedMsg struct {
	Sessions []terminal.Session
	Err      error
}

// AgentLaunchedMsg is sent after launching an agent in a new session.
type AgentLaunchedMsg struct {
	ProjectDir string
	SessionID  string
	AgentType  model.AgentType
	Source     string // backend source ("pty", "tmux", etc.), empty = use primary
	Err        error
}

// PromptSentMsg is sent after sending a prompt to an agent.
type PromptSentMsg struct {
	ProjectDir string
	Err        error
}

// StatusUpdatedMsg is sent when agent status has been checked.
type StatusUpdatedMsg struct {
	SessionID  string
	ProjectDir string
	Status     model.AgentStatus
	Activity   string
	Attention  string
}

// MonitorStartedMsg is sent after starting a monitor process.
type MonitorStartedMsg struct {
	SessionID  string
	ProjectDir string
	PID        int
	LogPath    string
	Err        error
}

// FocusedMsg is sent after focusing a session.
type FocusedMsg struct {
	Err error
}

// DiscoveryTickMsg triggers periodic session discovery refresh.
type DiscoveryTickMsg struct{}

// StatusTickMsg triggers background status polling.
type StatusTickMsg struct{}

// VisibleRefreshMsg triggers a fast refresh for the currently visible session.
type VisibleRefreshMsg struct {
	SessionID string
}

// SpinnerTickMsg advances the spinner animation.
type SpinnerTickMsg struct{}

// StatusMsg is a transient message shown in the status bar.
type StatusMsg struct {
	Text string
}

// DirBrowserItem represents a directory in the add-project browser.
type DirBrowserItem struct {
	Path     string
	Name     string
	IsParent bool // true for ".." entry
}

// DirBrowserMsg contains directories for the browser view.
type DirBrowserMsg struct {
	Dirs       []DirBrowserItem
	CurrentDir string
}

// BackendAvailableMsg indicates whether the backend is usable.
type BackendAvailableMsg struct {
	Err error
}

// ScreenReadMsg contains screen content read from a session.
type ScreenReadMsg struct {
	SessionID  string
	ProjectDir string
	Content    string
	Err        error
}

// AgentDiscoveredMsg is sent when an untracked agent has been identified and
// its CWD resolved.
type AgentDiscoveredMsg struct {
	SessionID string
	AgentType model.AgentType
	Source    string // backend source ("pty", "iterm", "tmux", etc.)
	Dir       string // resolved working directory, empty if not found
	DebugSkip string // optional discovery skip reason for debug logging
}

// IntegrationToggledMsg is sent after toggling an integration on or off.
type IntegrationToggledMsg struct {
	Name        string
	Status      BackendStatus
	Err         error
	RemappedIDs map[string]string // old session ID → new session ID (e.g. PTY demotion)
	NewPrimary  string            // source of the new launch target (set on both enable and disable)
}

// ConfigSavedMsg is sent after persisting config to disk.
// If Err is non-nil and Rollback is set, the handler calls Rollback
// to revert in-memory mutations that were applied optimistically.
type ConfigSavedMsg struct {
	Err      error
	Rollback func(m *Model) // reverts optimistic mutations on save failure
}

// QuickResponseArmExpiredMsg clears the transient armed quick-response state.
type QuickResponseArmExpiredMsg struct {
	SessionID string
}
