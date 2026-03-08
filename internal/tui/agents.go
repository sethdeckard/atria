package tui

import (
	"os/exec"

	"github.com/sethdeckard/atria/internal/model"
)

// detectAvailableAgents checks which agent binaries are in PATH.
func detectAvailableAgents() []model.AgentType {
	var agents []model.AgentType
	if _, err := exec.LookPath("claude"); err == nil {
		agents = append(agents, model.AgentClaude)
	}
	if _, err := exec.LookPath("codex"); err == nil {
		agents = append(agents, model.AgentCodex)
	}
	return agents
}
