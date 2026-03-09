package terminal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sethdeckard/atria/internal/model"
)

func TestClassifyOutput(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		agentType model.AgentType
		expected  model.AgentStatus
	}{
		// Claude patterns
		{"claude allow prompt", "Allow this action?", model.AgentClaude, model.StatusNeedsInput},
		{"claude allow edit", "Allow file edit?", model.AgentClaude, model.StatusNeedsInput},
		{"claude proceed prompt", "Do you want to proceed?", model.AgentClaude, model.StatusNeedsInput},
		{"claude esc to cancel", "Esc to cancel · Tab to amend", model.AgentClaude, model.StatusNeedsInput},
		{"claude plan mode prompt", "Would you like to proceed?", model.AgentClaude, model.StatusNeedsInput},
		{"claude working spinner", "✻ Reading…", model.AgentClaude, model.StatusWorking},
		{"claude thinking", "✶ Doodling… (thought for 6s)", model.AgentClaude, model.StatusWorking},
		{"claude dot spinner", "· Doodling… (48s)", model.AgentClaude, model.StatusWorking},
		{"claude esc to interrupt", "esc to interrupt", model.AgentClaude, model.StatusWorking},
		{"claude background task not working", "⏵⏵ accept edits on · tail -f log (running) · esc to interrupt", model.AgentClaude, ""},
		{"claude idle prompt", "❯ ", model.AgentClaude, model.StatusIdle},
		{"claude idle prompt with path", "~/projects ❯", model.AgentClaude, model.StatusIdle},
		{"claude shortcuts", "? for shortcuts", model.AgentClaude, model.StatusIdle},
		{"claude done static", "✻", model.AgentClaude, ""},

		// Codex patterns
		{"codex working bullet", "• Working (30s • esc to interrupt)", model.AgentCodex, model.StatusWorking},
		{"codex working simple", "• Working", model.AgentCodex, model.StatusWorking},
		{"codex waiting for input", "Waiting for user input", model.AgentCodex, model.StatusNeedsInput},
		{"codex prompt", "› Write tests for @filename", model.AgentCodex, model.StatusIdle},
		{"codex status bar idle", "gpt-5.3-codex default · 73% left · ~/projects/foo", model.AgentCodex, model.StatusIdle},

		// OpenCode patterns
		{"opencode permission required", "△ Permission required", model.AgentOpenCode, model.StatusNeedsInput},
		{"opencode allow once button", "Allow once   Allow always   Reject", model.AgentOpenCode, model.StatusNeedsInput},
		{"opencode working", "■ ..... esc interrupt", model.AgentOpenCode, model.StatusWorking},
		{"opencode idle footer", "ctrl+t variants  tab agents  ctrl+p commands", model.AgentOpenCode, model.StatusIdle},

		// Shared patterns (work for any agent type)
		{"shared bell character", "\x07", model.AgentClaude, model.StatusNeedsInput},
		{"shared bell with text", "prompt\x07here", model.AgentCodex, model.StatusNeedsInput},
		{"shared error message", "Error: file not found", model.AgentClaude, model.StatusError},
		{"shared error with context", "compilation Error: syntax", model.AgentOpenCode, model.StatusError},
		{"shared completed check", "✓ All tests passed", model.AgentClaude, model.StatusIdle},
		{"shared completed text", "Task completed successfully", model.AgentCodex, model.StatusIdle},
		{"shared no findings", "No findings reported", model.AgentOpenCode, model.StatusIdle},
		{"shared shell prompt", "user@host $ ", model.AgentClaude, model.StatusIdle},

		// Cross-agent isolation: agent-specific patterns must NOT match other agents
		{"claude prompt not codex", "❯ ", model.AgentCodex, ""},
		{"claude prompt not opencode", "❯ ", model.AgentOpenCode, ""},
		{"claude shortcuts not codex", "? for shortcuts", model.AgentCodex, ""},
		{"codex prompt not claude", "› Write tests", model.AgentClaude, ""},
		{"codex prompt not opencode", "› Write tests", model.AgentOpenCode, ""},
		{"codex working not claude", "• Working", model.AgentClaude, ""},
		{"codex working not opencode", "• Working", model.AgentOpenCode, ""},
		{"opencode idle not claude", "ctrl+p commands", model.AgentClaude, ""},
		{"opencode idle not codex", "ctrl+p commands", model.AgentCodex, ""},
		{"claude proceed not codex", "Do you want to proceed?", model.AgentCodex, ""},
		{"opencode permission not claude", "Permission required", model.AgentClaude, ""},

		// Unknown agent type: only shared patterns match
		{"unknown bell", "\x07", "unknown", model.StatusNeedsInput},
		{"unknown error", "Error: oops", "unknown", model.StatusError},
		{"unknown completed", "✓ done", "unknown", model.StatusIdle},
		{"unknown shell prompt", "user@host $ ", "unknown", model.StatusIdle},
		{"unknown claude spinner", "✻ Reading…", "unknown", ""},
		{"unknown codex working", "• Working", "unknown", ""},
		{"unknown no match", "Hello world", "unknown", ""},

		// Misc
		{"generic question no match", "Do you want to continue?", model.AgentClaude, ""},
		{"bare continue no match", "Press Continue to proceed", model.AgentClaude, ""},
		{"generic working output", "Compiling main.go...", model.AgentClaude, ""},
		{"empty string", "", model.AgentClaude, ""},
		{"random text", "Hello world", model.AgentClaude, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyOutput(tt.input, tt.agentType)
			if got != tt.expected {
				t.Errorf("ClassifyOutput(%q, %q) = %q, want %q", tt.input, tt.agentType, got, tt.expected)
			}
		})
	}
}

func TestReadLastLine(t *testing.T) {
	t.Run("multi-line file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.log")
		content := "first line\nsecond line\nthird line\n"
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		got := ReadLastLine(path)
		if got != "third line" {
			t.Errorf("ReadLastLine() = %q, want %q", got, "third line")
		}
	})

	t.Run("file with trailing blank lines", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.log")
		content := "first line\nsecond line\n\n\n"
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		got := ReadLastLine(path)
		if got != "second line" {
			t.Errorf("ReadLastLine() = %q, want %q", got, "second line")
		}
	})

	t.Run("single line", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.log")
		if err := os.WriteFile(path, []byte("only line"), 0644); err != nil {
			t.Fatal(err)
		}

		got := ReadLastLine(path)
		if got != "only line" {
			t.Errorf("ReadLastLine() = %q, want %q", got, "only line")
		}
	})

	t.Run("empty file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.log")
		if err := os.WriteFile(path, []byte(""), 0644); err != nil {
			t.Fatal(err)
		}

		got := ReadLastLine(path)
		if got != "" {
			t.Errorf("ReadLastLine() = %q, want %q", got, "")
		}
	})

	t.Run("nonexistent file", func(t *testing.T) {
		got := ReadLastLine("/nonexistent/path/file.log")
		if got != "" {
			t.Errorf("ReadLastLine() = %q, want %q", got, "")
		}
	})
}

func TestClassifyScreen(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		agentType  model.AgentType
		wantStatus model.AgentStatus
		wantLine   string
	}{
		{
			"claude needs_input wins over idle",
			"Do you want to proceed?\n❯ prompt here\n? for shortcuts",
			model.AgentClaude,
			model.StatusNeedsInput,
			"Do you want to proceed?",
		},
		{
			"claude working wins over idle",
			"✻ Reading…\n❯ \n? for shortcuts",
			model.AgentClaude,
			model.StatusWorking,
			"✻ Reading…",
		},
		{
			"claude idle only",
			"some output\n❯ \n? for shortcuts",
			model.AgentClaude,
			model.StatusIdle,
			"❯",
		},
		{
			"shared error wins over idle",
			"Error: something broke\n❯ prompt",
			model.AgentClaude,
			model.StatusError,
			"Error: something broke",
		},
		{
			"empty content",
			"",
			model.AgentClaude,
			"",
			"",
		},
		{
			"no match",
			"just some random text\nnothing special",
			model.AgentClaude,
			"",
			"",
		},
		{
			"claude needs_input in scrollback ignored",
			"line1\nline2\nDo you want to proceed?\nline4\nline5\nline6\nline7\nline8\nline9\nline10\n❯ \n? for shortcuts",
			model.AgentClaude,
			model.StatusIdle,
			"❯",
		},
		{
			"claude needs_input near bottom detected",
			"line1\nline2\nline3\nline4\n❯ \nDo you want to proceed?\n1. Yes\n2. No\nEsc to cancel",
			model.AgentClaude,
			model.StatusNeedsInput,
			"Do you want to proceed?",
		},
		{
			"opencode permission prompt layout",
			"Build · big-pickle · 9.2s\n\nread the file ../RESEARCH.md\n\nThinking: user wants to read a file\n\nRead /Users/seth/projects/go/RESEARCH.md\n\nBuild · big-pickle\n\n△ Permission required\nAccess external directory ~/projects/go\n\nPatterns\n\n- /Users/seth/projects/go/*\n\n\nAllow once   Allow always   Reject   ctrl+f fullscreen  enter confirm\n• OpenCode 1.2.21\n",
			model.AgentOpenCode,
			model.StatusNeedsInput,
			"Allow once",
		},
		{
			"claude plan mode prompt with selection cursor",
			"Would you like to proceed?\n\n ❯ 1. Yes, clear context (62% used) and auto-accept edits (shift+tab)\n   2. Yes, auto-accept edits\n   3. Yes, manually approve edits\n   4. Type here to tell Claude what to change\n\n ctrl-g to edit in Mvim · ~/.claude/plans/...",
			model.AgentClaude,
			model.StatusNeedsInput,
			"Would you like to proceed?",
		},
		{
			"claude working in scrollback ignored",
			"line1\n✻ Reading…\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\n❯ \n? for shortcuts",
			model.AgentClaude,
			model.StatusIdle,
			"❯",
		},
		{
			"codex screen with working",
			"some output\n• Working (30s • esc to interrupt)\n",
			model.AgentCodex,
			model.StatusWorking,
			"• Working",
		},
		{
			"cross-agent: claude spinner not detected for codex",
			"✻ Reading…\n",
			model.AgentCodex,
			"",
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, line := ClassifyScreen(tt.content, tt.agentType)
			if status != tt.wantStatus {
				t.Errorf("ClassifyScreen() status = %q, want %q", status, tt.wantStatus)
			}
			if tt.wantLine != "" && !strings.Contains(line, tt.wantLine) {
				t.Errorf("ClassifyScreen() line = %q, want containing %q", line, tt.wantLine)
			}
		})
	}
}

func TestHasBell(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"with bell", "some text\x07more", true},
		{"no bell", "just normal text", false},
		{"bell only", "\x07", true},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasBell(tt.input); got != tt.expected {
				t.Errorf("HasBell(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestReadTail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	content := "abcdefghij"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	got := ReadTail(path, 5)
	if got != "fghij" {
		t.Errorf("ReadTail(5) = %q, want %q", got, "fghij")
	}

	got = ReadTail(path, 100)
	if got != content {
		t.Errorf("ReadTail(100) = %q, want %q", got, content)
	}

	got = ReadTail("/nonexistent", 10)
	if got != "" {
		t.Errorf("ReadTail(nonexistent) = %q, want %q", got, "")
	}
}
