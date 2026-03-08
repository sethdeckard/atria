package iterm

import (
	"testing"
)

func TestNewClient(t *testing.T) {
	path := "/usr/local/bin/it2"
	c := NewClient(path)
	if c.it2Path != path {
		t.Errorf("expected it2Path %q, got %q", path, c.it2Path)
	}
}

func TestNewClientEmptyPath(t *testing.T) {
	c := NewClient("")
	if c.it2Path != "" {
		t.Errorf("expected empty it2Path, got %q", c.it2Path)
	}
}

func TestAvailableErrorWhenNotFound(t *testing.T) {
	c := NewClient("/nonexistent/path/to/it2")
	err := c.Available()
	if err == nil {
		t.Fatal("expected error when it2 binary does not exist")
	}
}

func TestParseTabID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Created new tab: 69\n", "69"},
		{"Created new tab: 123", "123"},
		{"Created new tab: 0\n", "0"},
		{"unexpected output", ""},
		{"", ""},
	}
	for _, tc := range tests {
		got := parseTabID(tc.input)
		if got != tc.expected {
			t.Errorf("parseTabID(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestAvailableErrorWhenEmptyPathAndNotInPATH(t *testing.T) {
	// Set PATH to empty and HOME to nonexistent to ensure it2 is not found
	// via PATH or venv fallback paths.
	t.Setenv("PATH", "")
	t.Setenv("HOME", "/nonexistent-home-for-test")
	c := NewClient("")
	err := c.Available()
	if err == nil {
		t.Fatal("expected error when it2 is not in PATH")
	}
}
