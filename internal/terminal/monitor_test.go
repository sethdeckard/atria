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
		{"permission prompt", "Permission required to proceed", model.StatusNeedsInput},
		{"question mark ending", "Do you want to continue?", model.StatusNeedsInput},
		{"continue prompt", "Press Continue to proceed", model.StatusNeedsInput},
		{"error message", "Error: file not found", model.StatusError},
		{"error with context", "compilation Error: syntax", model.StatusError},
		{"waiting for", "Waiting for response", model.StatusNeedsInput},
		{"proceed prompt", "Do you want to proceed?", model.StatusNeedsInput},
		{"esc to cancel", "Esc to cancel · Tab to amend", model.StatusNeedsInput},
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
