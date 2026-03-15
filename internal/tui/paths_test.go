package tui

import (
	"os"
	"path/filepath"
	"testing"
)

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
