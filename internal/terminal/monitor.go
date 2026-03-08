package terminal

import (
	"os"
	"regexp"
	"strings"

	"github.com/sethdeckard/atria/internal/model"
)

var (
	needsInputPattern = regexp.MustCompile(`(?i)(Allow|Permission|\?$|Continue)`)
	errorPattern      = regexp.MustCompile(`Error:`)
	idlePattern       = regexp.MustCompile(`❯`)
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

// ClassifyOutput determines agent status from output text.
// Returns needs_input if text matches Allow/Permission/?$/Continue.
// Returns error if text matches "Error:".
// Returns idle if text matches ❯.
// Returns "" (empty) if no match.
func ClassifyOutput(text string) model.AgentStatus {
	if needsInputPattern.MatchString(text) {
		return model.StatusNeedsInput
	}

	if errorPattern.MatchString(text) {
		return model.StatusError
	}

	if idlePattern.MatchString(text) {
		return model.StatusIdle
	}

	return ""
}
