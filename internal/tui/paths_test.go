package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShortenPath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/Users/seth/projects/go/atria", "go/atria"},
		{"/a/b", "a/b"},
		{"/a", "/a"},
		{"relative/path/here", "path/here"},
		{"/trailing/slash/dir/", "slash/dir"},
		{"/one/two/three", "two/three"},
		{"", ""},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := shortenPath(tc.input)
			if got != tc.expected {
				t.Errorf("shortenPath(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestContractHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}

	tests := []struct {
		input    string
		expected string
	}{
		{filepath.Join(home, "projects/go/myapp"), "~/projects/go/myapp"},
		{home, "~"},
		{"/var/tmp/other", "/var/tmp/other"},
		{filepath.Join(home, "a"), "~/a"},
	}
	for _, tc := range tests {
		got := contractHome(tc.input)
		if got != tc.expected {
			t.Errorf("contractHome(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}
