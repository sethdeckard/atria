package terminal

import (
	"testing"

	"github.com/sethdeckard/atria/internal/model"
)

func TestDetectAgent(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected model.AgentType
	}{
		{"sparkle prefix", "✳ Editing src/game.go", model.AgentClaude},
		{"sparkle only", "✳", model.AgentClaude},
		{"claude lowercase", "my-claude-session", model.AgentClaude},
		{"claude uppercase", "CLAUDE-CODE", model.AgentClaude},
		{"claude mixed case", "Claude Agent", model.AgentClaude},
		{"codex lowercase", "codex-session", model.AgentCodex},
		{"codex uppercase", "CODEX", model.AgentCodex},
		{"codex mixed case", "OpenAI Codex", model.AgentCodex},
		{"opencode lowercase", "opencode", model.AgentOpenCode},
		{"opencode in title", "OC | Reading file (opencode)", model.AgentOpenCode},
		{"opencode in session name", "my-opencode-session", model.AgentOpenCode},
		{"copilot lowercase", "copilot", model.AgentCopilot},
		{"copilot in session name", "my-copilot-session", model.AgentCopilot},
		{"copilot uppercase", "COPILOT", model.AgentCopilot},
		{"copilot mixed case", "GitHub Copilot", model.AgentCopilot},
		{"copilot robot prefix", "🤖 Asking clarifying question", model.AgentCopilot},
		{"copilot robot only", "🤖", model.AgentCopilot},
		{"plain session", "my-project", ""},
		{"empty string", "", ""},
		{"bash session", "bash", ""},
		{"numeric name", "12345", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectAgent(tt.input)
			if got != tt.expected {
				t.Errorf("DetectAgent(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestExtractActivity(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"full format", "✳ Editing src/game.go (sourcekit-lsp)", "Editing src/game.go"},
		{"no parentheses", "✳ Editing src/game.go", "Editing src/game.go"},
		{"sparkle only", "✳", ""},
		{"no sparkle with parens", "Running tests (jest)", "Running tests"},
		{"no sparkle no parens", "Running tests", "Running tests"},
		{"empty string", "", ""},
		{"sparkle with space", "✳ Idle", "Idle"},
		{"nested parens stripped last", "✳ Foo (bar) (baz)", "Foo (bar)"},
		{"opencode title", "OC | Reading file (opencode)", "Reading file"},
		{"opencode title no suffix", "OC | Editing code", "Editing code"},
		{"copilot robot prefix", "🤖 Asking clarifying question", "Asking clarifying question"},
		{"copilot no prefix", "GitHub Copilot", ""},
		{"product name claude code", "Claude Code", ""},
		{"product name codex", "codex", ""},
		{"product name opencode", "OpenCode", ""},
		{"product name claude", "✳ Claude Code", ""},
		{"cd command", "cd", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractActivity(tt.input)
			if got != tt.expected {
				t.Errorf("ExtractActivity(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
