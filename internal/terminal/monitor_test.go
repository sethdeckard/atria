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
		name     string
		input    string
		expected model.AgentStatus
	}{
		{"allow prompt", "Allow this action?", model.StatusNeedsInput},
		{"allow edit", "Allow file edit?", model.StatusNeedsInput},
		{"proceed prompt", "Do you want to proceed?", model.StatusNeedsInput},
		{"esc to cancel", "Esc to cancel · Tab to amend", model.StatusNeedsInput},
		{"waiting for input", "Waiting for user input", model.StatusNeedsInput},
		{"error message", "Error: file not found", model.StatusError},
		{"error with context", "compilation Error: syntax", model.StatusError},
		{"generic question no match", "Do you want to continue?", ""},
		{"bare continue no match", "Press Continue to proceed", ""},
		{"idle prompt", "❯ ", model.StatusIdle},
		{"idle prompt with path", "~/projects ❯", model.StatusIdle},
		{"codex prompt", "› Write tests for @filename", model.StatusIdle},
		{"claude shortcuts", "? for shortcuts", model.StatusIdle},
		{"shell prompt", "user@host $ ", model.StatusIdle},
		{"completed check", "✓ All tests passed", model.StatusIdle},
		{"completed text", "Task completed successfully", model.StatusIdle},
		{"no findings", "No findings reported", model.StatusIdle},
		{"bell character", "\x07", model.StatusNeedsInput},
		{"bell with text", "prompt\x07here", model.StatusNeedsInput},
		{"claude working spinner", "✻ Reading…", model.StatusWorking},
		{"claude thinking", "✶ Doodling… (thought for 6s)", model.StatusWorking},
		{"claude dot spinner", "· Doodling… (48s)", model.StatusWorking},
		{"claude esc to interrupt", "esc to interrupt", model.StatusWorking},
		{"background task not working", "⏵⏵ accept edits on · tail -f log (running) · esc to interrupt", ""},
		{"codex working bullet", "• Working (30s • esc to interrupt)", model.StatusWorking},
		{"codex working simple", "• Working", model.StatusWorking},
		{"codex status bar idle", "gpt-5.3-codex default · 73% left · ~/projects/foo", model.StatusIdle},
		{"opencode idle footer", "ctrl+t variants  tab agents  ctrl+p commands", model.StatusIdle},
		{"opencode permission required", "△ Permission required", model.StatusNeedsInput},
		{"opencode allow once button", "Allow once   Allow always   Reject", model.StatusNeedsInput},
		{"opencode working", "■ ..... esc interrupt", model.StatusWorking},
		{"claude done static", "✻", ""},
		{"generic working output", "Compiling main.go...", ""},
		{"empty string", "", ""},
		{"random text", "Hello world", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyOutput(tt.input)
			if got != tt.expected {
				t.Errorf("ClassifyOutput(%q) = %q, want %q", tt.input, got, tt.expected)
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
		wantStatus model.AgentStatus
		wantLine   string
	}{
		{
			"needs_input wins over idle",
			"Do you want to proceed?\n❯ prompt here\n? for shortcuts",
			model.StatusNeedsInput,
			"Do you want to proceed?",
		},
		{
			"working wins over idle",
			"✻ Reading…\n❯ \n? for shortcuts",
			model.StatusWorking,
			"✻ Reading…",
		},
		{
			"idle only",
			"some output\n❯ \n? for shortcuts",
			model.StatusIdle,
			"❯",
		},
		{
			"error wins over idle",
			"Error: something broke\n❯ prompt",
			model.StatusError,
			"Error: something broke",
		},
		{
			"empty content",
			"",
			"",
			"",
		},
		{
			"no match",
			"just some random text\nnothing special",
			"",
			"",
		},
		{
			"needs_input in scrollback ignored",
			"line1\nline2\nDo you want to proceed?\nline4\nline5\nline6\nline7\nline8\nline9\nline10\n❯ \n? for shortcuts",
			model.StatusIdle,
			"❯",
		},
		{
			"needs_input near bottom detected",
			"line1\nline2\nline3\nline4\n❯ \nDo you want to proceed?\n1. Yes\n2. No\nEsc to cancel",
			model.StatusNeedsInput,
			"Do you want to proceed?",
		},
		{
			"opencode permission prompt layout",
			"Build · big-pickle · 9.2s\n\nread the file ../RESEARCH.md\n\nThinking: user wants to read a file\n\nRead /Users/seth/projects/go/RESEARCH.md\n\nBuild · big-pickle\n\n△ Permission required\nAccess external directory ~/projects/go\n\nPatterns\n\n- /Users/seth/projects/go/*\n\n\nAllow once   Allow always   Reject   ctrl+f fullscreen  enter confirm\n• OpenCode 1.2.21\n",
			model.StatusNeedsInput,
			"Allow once",
		},
		{
			"working in scrollback ignored",
			"line1\n✻ Reading…\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\n❯ \n? for shortcuts",
			model.StatusIdle,
			"❯",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, line := ClassifyScreen(tt.content)
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
