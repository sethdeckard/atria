package tui

import (
	"os"
	"strings"
)

// shortenPath returns the last two path segments with a …/ prefix.
// e.g. "/Users/seth/projects/go/agent-tui" → "…/go/agent-tui"
func shortenPath(path string) string {
	parts := strings.Split(path, "/")
	// Remove trailing empty from trailing slash
	for len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) <= 2 {
		return path
	}
	return "\u2026/" + strings.Join(parts[len(parts)-2:], "/")
}

// contractHome replaces home dir prefix with ~ for display.
func contractHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+"/") {
		return "~" + path[len(home):]
	}
	return path
}
