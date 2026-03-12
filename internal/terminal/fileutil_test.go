package terminal

import (
	"os"
	"path/filepath"
	"testing"
)

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
