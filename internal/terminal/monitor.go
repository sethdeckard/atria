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
	workingPattern    = regexp.MustCompile(`[✻✶·] \S+…|esc to interrupt`)
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

// ClassifyOutput determines agent status from a single line of output text.
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

	if workingPattern.MatchString(text) {
		return model.StatusWorking
	}

	if completedPattern.MatchString(text) {
		return model.StatusIdle
	}

	if idlePattern.MatchString(text) {
		return model.StatusIdle
	}

	return ""
}

// statusPriority returns a numeric priority for status (lower = more urgent).
func statusPriority(s model.AgentStatus) int {
	switch s {
	case model.StatusNeedsInput:
		return 0
	case model.StatusError:
		return 1
	case model.StatusWorking:
		return 2
	case model.StatusIdle:
		return 3
	default:
		return 4
	}
}

// ClassifyScreen checks each line of multi-line screen content and returns
// the highest priority status found. This prevents idle prompts (always
// visible) from masking needs_input or error states on other lines.
func ClassifyScreen(content string) (model.AgentStatus, string) {
	lines := strings.Split(content, "\n")
	bestStatus := model.AgentStatus("")
	bestLine := ""

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		status := ClassifyOutput(line)
		if status == "" {
			continue
		}
		if bestStatus == "" || statusPriority(status) < statusPriority(bestStatus) {
			bestStatus = status
			bestLine = line
		}
	}

	return bestStatus, bestLine
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
