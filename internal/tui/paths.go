package tui

import (
	"os"
	"strings"
)

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
