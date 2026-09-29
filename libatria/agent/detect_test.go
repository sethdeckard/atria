package agent

import (
	"testing"
)

func TestDetect(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected Type
	}{
		{"sparkle prefix", "✳ Editing src/game.go", Claude},
		{"asterisk flower prefix", "✻ Reading…", Claude},
		{"asterisk star prefix", "✶ Doodling…", Claude},
		{"asterisk eight spoke prefix", "✽ Frosting…", Claude},
		{"asterisk four teardrop prefix", "✢ Frosting…", Claude},
		{"sparkle only", "✳", Claude},
		{"claude lowercase", "my-claude-session", Claude},
		{"claude uppercase", "CLAUDE-CODE", Claude},
		{"claude mixed case", "Claude Agent", Claude},
		{"codex lowercase", "codex-session", Codex},
		{"codex uppercase", "CODEX", Codex},
		{"codex mixed case", "OpenAI Codex", Codex},
		{"opencode lowercase", "opencode", OpenCode},
		{"opencode in title", "OC | Reading file (opencode)", OpenCode},
		{"opencode in session name", "my-opencode-session", OpenCode},
		{"copilot lowercase", "copilot", Copilot},
		{"copilot in session name", "my-copilot-session", Copilot},
		{"copilot uppercase", "COPILOT", Copilot},
		{"copilot mixed case", "GitHub Copilot", Copilot},
		{"copilot robot prefix", "🤖 Asking clarifying question", Copilot},
		{"copilot robot only", "🤖", Copilot},
		{"plain session", "my-project", ""},
		{"empty string", "", ""},
		{"bash session", "bash", ""},
		{"numeric name", "12345", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Detect(tt.input)
			if got != tt.expected {
				t.Errorf("Detect(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestExtractActivity(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		job      string
		expected string
	}{
		{"job suffix", "✳ Editing src/game.go (sourcekit-lsp)", "sourcekit-lsp", "Editing src/game.go"},
		{"job given as a path", "✳ Editing src/game.go (sourcekit-lsp)", "/usr/bin/sourcekit-lsp", "Editing src/game.go"},
		{"job suffix case-insensitive", "Running tests (Jest)", "jest", "Running tests"},
		{"login shell suffix", "..ts/go/loadout (-zsh)", "zsh", "..ts/go/loadout"},
		{"suffix not the job", "✳ Editing src/game.go (sourcekit-lsp)", "zsh", "Editing src/game.go (sourcekit-lsp)"},
		{"own parenthetical, no job", "✳ Fix parser (phase 2)", "", "Fix parser (phase 2)"},
		{"own parenthetical then agent suffix", "✳ Fix parser (phase 2) (claude)", "", "Fix parser (phase 2)"},
		{"own parenthetical then job suffix", "✳ Fix parser (phase 2) (node)", "node", "Fix parser (phase 2)"},
		{"agent suffix without job", "✳ Editing main.go (claude)", "", "Editing main.go"},
		{"flower prefix", "✻ Reading…", "", "Reading…"},
		{"star prefix", "✶ Doodling…", "", "Doodling…"},
		{"eight spoke prefix", "✽ Frosting…", "", "Frosting…"},
		{"four teardrop prefix", "✢ Frosting…", "", "Frosting…"},
		{"no parentheses", "✳ Editing src/game.go", "", "Editing src/game.go"},
		{"sparkle only", "✳", "", ""},
		{"no sparkle no parens", "Running tests", "", "Running tests"},
		{"empty string", "", "", ""},
		{"empty parens kept", "Running tests ()", "", "Running tests ()"},
		{"sparkle with space", "✳ Idle", "", "Idle"},
		{"opencode title", "OC | Reading file (opencode)", "", "Reading file"},
		{"opencode title no suffix", "OC | Editing code", "", "Editing code"},
		{"copilot robot prefix", "🤖 Asking clarifying question", "", "Asking clarifying question"},
		{"copilot no prefix", "GitHub Copilot", "", ""},
		{"product name claude code", "Claude Code", "", ""},
		{"product name codex", "codex", "", ""},
		{"product name codex with job", "codex (codex)", "codex", ""},
		{"product name opencode", "OpenCode", "", ""},
		{"product name claude", "✳ Claude Code", "", ""},
		{"cd command", "cd", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractActivity(tt.input, tt.job)
			if got != tt.expected {
				t.Errorf("ExtractActivity(%q, %q) = %q, want %q", tt.input, tt.job, got, tt.expected)
			}
		})
	}
}
