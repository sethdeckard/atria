package terminal

import (
	"strings"

	"github.com/sethdeckard/atria/internal/model"
)

// DetectAgent returns the agent type from a session name, or "" if not an agent.
// Claude: name starts with ✳ (U+2733) or contains "claude" (case-insensitive)
// Copilot: name starts with 🤖 (U+1F916) or contains "copilot" (case-insensitive)
// Codex: name contains "codex" (case-insensitive)
func DetectAgent(name string) model.AgentType {
	lower := strings.ToLower(name)

	if strings.HasPrefix(name, "\u2733") || strings.Contains(lower, "claude") {
		return model.AgentClaude
	}

	if strings.Contains(lower, "opencode") {
		return model.AgentOpenCode
	}

	if strings.HasPrefix(name, "\U0001F916") || strings.Contains(lower, "copilot") {
		return model.AgentCopilot
	}

	if strings.Contains(lower, "codex") {
		return model.AgentCodex
	}

	return ""
}

// ExtractActivity extracts activity description from a dynamic session name.
// E.g., "✳ Editing src/game.go (sourcekit-lsp)" -> "Editing src/game.go"
// Returns the name without the ✳ prefix and without the parenthesized suffix.
func ExtractActivity(name string) string {
	s := name

	// Strip the ✳ prefix (with optional trailing space).
	if strings.HasPrefix(s, "\u2733") {
		s = strings.TrimPrefix(s, "\u2733")
		s = strings.TrimLeft(s, " ")
	}

	// Strip "OC | " prefix for OpenCode sessions.
	if strings.HasPrefix(s, "OC | ") {
		s = strings.TrimPrefix(s, "OC | ")
	}

	// Strip "🤖 " prefix for Copilot sessions.
	if strings.HasPrefix(s, "\U0001F916 ") {
		s = strings.TrimPrefix(s, "\U0001F916 ")
	}

	// Remove a trailing parenthesized suffix, e.g. " (sourcekit-lsp)" or " (opencode)".
	if idx := strings.LastIndex(s, "("); idx > 0 {
		if strings.HasSuffix(s, ")") {
			s = s[:idx]
		}
	}

	s = strings.TrimSpace(s)

	// If the result is just a known product name, return "" so the UI
	// shows "idle" instead of the product name as activity.
	switch strings.ToLower(s) {
	case "claude code", "claude", "codex", "openai codex", "opencode", "github copilot", "copilot":
		return ""
	}

	return s
}
