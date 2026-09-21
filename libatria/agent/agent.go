package agent

// Type names a supported agent. The string value is the agent's CLI binary
// name, so string(t) is what you exec to launch it.
type Type string

// Supported agents.
const (
	Claude   Type = "claude"
	Codex    Type = "codex"
	OpenCode Type = "opencode"
	Copilot  Type = "copilot"
)

// Status is what an agent is doing, as read from its screen.
type Status string

// Statuses, in priority order from most to least urgent. When a screen
// matches more than one, the more urgent status wins.
const (
	StatusNeedsInput Status = "needs_input"
	StatusError      Status = "error"
	StatusWorking    Status = "working"
	StatusIdle       Status = "idle"
)

// Types returns the supported agents in this order: Claude, Codex, OpenCode,
// Copilot. The slice is a fresh copy each call.
func Types() []Type {
	out := make([]Type, len(detectableAgents))
	copy(out, detectableAgents)
	return out
}
