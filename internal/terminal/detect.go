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

	if hasClaudePrefix(name) || strings.Contains(lower, "claude") {
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

	// Strip the Claude prefix glyph (with optional trailing space).
	if prefix := claudePrefix(s); prefix != "" {
		s = strings.TrimPrefix(s, prefix)
		s = strings.TrimLeft(s, " ")
	}

	// Strip "OC | " prefix for OpenCode sessions.
	s = strings.TrimPrefix(s, "OC | ")

	// Strip "🤖 " prefix for Copilot sessions.
	s = strings.TrimPrefix(s, "\U0001F916 ")

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
	case "claude code", "claude", "codex", "openai codex", "opencode", "github copilot", "copilot", "cd":
		return ""
	}

	return s
}

func hasClaudePrefix(name string) bool {
	return claudePrefix(name) != ""
}

func claudePrefix(name string) string {
	for _, prefix := range []string{"\u2733", "✻", "✶"} {
		if strings.HasPrefix(name, prefix) {
			return prefix
		}
	}
	return ""
}
