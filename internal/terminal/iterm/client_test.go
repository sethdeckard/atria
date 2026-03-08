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
