package tui

import (
	"testing"

	"github.com/sethdeckard/atria/internal/model"
)

func TestDetectAvailableAgents(t *testing.T) {
	agents := detectAvailableAgents()
	// We can't predict what's installed, but the function should not panic
	// and should return a valid slice.
	for _, a := range agents {
		if a != model.AgentClaude && a != model.AgentCodex && a != model.AgentOpenCode {
			t.Errorf("unexpected agent type: %q", a)
		}
	}
}

func TestDetectAvailableAgentsNoDuplicates(t *testing.T) {
	agents := detectAvailableAgents()
	seen := make(map[model.AgentType]bool)
	for _, a := range agents {
		if seen[a] {
			t.Errorf("duplicate agent type: %q", a)
		}
		seen[a] = true
	}
}
