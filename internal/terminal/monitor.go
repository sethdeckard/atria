package terminal

import (
	"os"
	"regexp"
	"strings"

	"github.com/sethdeckard/atria/internal/model"
)

var (
	needsInputPattern = regexp.MustCompile(`(?i)(Allow|Permission|\?$|Continue|Waiting for|Do you want to proceed|Esc to cancel)`)
	bellPattern       = regexp.MustCompile("\x07")
	errorPattern      = regexp.MustCompile(`Error:`)
	idlePattern       = regexp.MustCompile(`❯|›|\? for shortcuts|(\$ $)`)
	completedPattern  = regexp.MustCompile(`✓|completed|No findings`)
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
// Returns needs_input for permission prompts, questions, or bell characters.
// Returns error for error messages.
// Returns idle for shell/agent prompts and completion signals.
// Returns "" (empty) if no match.
func ClassifyOutput(text string) model.AgentStatus {
	if bellPattern.MatchString(text) {
		return model.StatusNeedsInput
	}

	if needsInputPattern.MatchString(text) {
		return model.StatusNeedsInput
	}

	if errorPattern.MatchString(text) {
		return model.StatusError
	}

	if completedPattern.MatchString(text) {
		return model.StatusIdle
	}

	if idlePattern.MatchString(text) {
		return model.StatusIdle
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
