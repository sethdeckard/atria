package terminal

import (
	"os"
	"strings"
)

// ReadLastLine reads the last non-empty line from a log file.
// Returns an empty string if the file cannot be read or has no non-empty lines.
func ReadLastLine(logPath string) string {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return ""
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")

	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return line
		}
	}

	return ""
}

// ReadTail reads the last n bytes of a log file.
func ReadTail(logPath string, n int) string {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return ""
	}
	if len(data) <= n {
		return string(data)
	}
	return string(data[len(data)-n:])
}

// HasBell checks if text contains a bell character (0x07).
func HasBell(text string) bool {
	return strings.Contains(text, "\x07")
}
