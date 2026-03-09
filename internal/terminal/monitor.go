package terminal

import (
	"os"
	"regexp"
	"strings"

	"github.com/sethdeckard/atria/internal/model"
)

var (
	needsInputPattern = regexp.MustCompile(`(?i)(Do you want to proceed|Allow .+\?|Esc to cancel|Waiting for .+ input|Permission required|Allow once)`)
	bellPattern       = regexp.MustCompile("\x07")
	errorPattern      = regexp.MustCompile(`Error:`)
	workingPattern    = regexp.MustCompile(`[✻✶·] \S+…|[•●] Working`)
	// Matches "esc to interrupt" only when NOT in a background task line (⏵⏵).
	escToInterrupt = regexp.MustCompile(`esc\s+(?:to\s+)?interrupt`)
	backgroundTask = regexp.MustCompile(`⏵`)

	idlePattern       = regexp.MustCompile(`❯|›|\? for shortcuts|(\$ $)|gpt-\S+-codex|ctrl\+p commands`)
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

	// "esc to interrupt" means working, but not when it appears in a
	// background task status line (⏵⏵ ... esc to interrupt).
	if escToInterrupt.MatchString(text) && !backgroundTask.MatchString(text) {
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

// bottomLineCount is the number of lines from the bottom of the screen
// where active status patterns (needs_input, error, working) are trusted.
// Conversation history higher up may contain quoted text that triggers
// false matches. Only idle/completed match anywhere since they're harmless.
const bottomLineCount = 8

// ClassifyScreen checks each line of multi-line screen content and returns
// the highest priority status found. Active statuses (needs_input, error,
// working) are only matched in the bottom region where the live UI appears.
// The bottom region is measured from the last non-blank line (not the
// absolute bottom) to handle blank padding below dialog prompts.
// Idle/completed can match anywhere.
func ClassifyScreen(content string) (model.AgentStatus, string) {
	lines := strings.Split(content, "\n")
	bestStatus := model.AgentStatus("")
	bestLine := ""

	// Find last non-blank line to anchor the bottom region
	lastNonBlank := 0
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			lastNonBlank = i
			break
		}
	}

	bottomStart := lastNonBlank - bottomLineCount + 1
	if bottomStart < 0 {
		bottomStart = 0
	}

	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		status := ClassifyOutput(line)
		if status == "" {
			continue
		}
		// Only trust active statuses from the bottom region
		if i < bottomStart && status != model.StatusIdle {
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
