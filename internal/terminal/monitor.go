package terminal

import (
	"strings"

	"github.com/sethdeckard/atria/internal/model"
)

// ClassifyOutput determines agent status from a single line of output text.
// Agent-specific patterns are checked first, then shared fallbacks.
func ClassifyOutput(text string, agentType model.AgentType) model.AgentStatus {
	// 1. Shared bell → needs_input
	if sharedBellPattern.MatchString(text) {
		return model.StatusNeedsInput
	}

	patterns := agentPatternRegistry[agentType]

	// 2. Agent-specific needs_input
	if patterns != nil {
		for _, re := range patterns.NeedsInput {
			if re.MatchString(text) {
				return model.StatusNeedsInput
			}
		}
	}

	// 3. Shared error
	if sharedErrorPattern.MatchString(text) {
		return model.StatusError
	}

	// 4. Agent-specific working (with exclusion check)
	if patterns != nil {
		excluded := false
		for _, re := range patterns.WorkingExclude {
			if re.MatchString(text) {
				excluded = true
				break
			}
		}
		if !excluded {
			for _, re := range patterns.Working {
				if re.MatchString(text) {
					return model.StatusWorking
				}
			}
		}
	}

	// 5. Shared idle (completed, shell prompt)
	if sharedCompletedPattern.MatchString(text) {
		return model.StatusIdle
	}
	if sharedShellPrompt.MatchString(text) {
		return model.StatusIdle
	}

	// 6. Agent-specific idle
	if patterns != nil {
		for _, re := range patterns.Idle {
			if re.MatchString(text) {
				return model.StatusIdle
			}
		}
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

// bottomRegion returns the start index of the bottom region, measured
// from the last non-blank line. Used to restrict active-status matching
// to the live UI area and ignore scrollback history.
func bottomRegion(lines []string) int {
	lastNonBlank := 0
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			lastNonBlank = i
			break
		}
	}
	start := lastNonBlank - bottomLineCount + 1
	if start < 0 {
		start = 0
	}
	return start
}

// ClassifyScreen checks each line of multi-line screen content and returns
// the highest priority status found. Active statuses (needs_input, error,
// working) are only matched in the bottom region where the live UI appears.
// The bottom region is measured from the last non-blank line (not the
// absolute bottom) to handle blank padding below dialog prompts.
// Idle/completed can match anywhere.
func ClassifyScreen(content string, agentType model.AgentType) (model.AgentStatus, string) {
	lines := strings.Split(content, "\n")
	bestStatus := model.AgentStatus("")
	bestLine := ""

	bottomStart := bottomRegion(lines)

	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		status := ClassifyOutput(line, agentType)
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

// HasAgentScreen checks whether agent-specific patterns appear in the bottom
// region of the screen content. Unlike ClassifyScreen (which matches idle
// patterns anywhere), this restricts ALL patterns to the bottom region so
// scrollback from a previously-exited agent doesn't count as a positive signal.
func HasAgentScreen(content string, agentType model.AgentType) bool {
	patterns := agentPatternRegistry[agentType]
	if patterns == nil {
		return false
	}
	lines := strings.Split(content, "\n")

	bottomStart := bottomRegion(lines)

	lastNonBlank := len(lines) - 1
	for lastNonBlank > 0 && strings.TrimSpace(lines[lastNonBlank]) == "" {
		lastNonBlank--
	}

	for i := bottomStart; i <= lastNonBlank; i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		for _, re := range patterns.NeedsInput {
			if re.MatchString(line) {
				return true
			}
		}
		excluded := false
		for _, re := range patterns.WorkingExclude {
			if re.MatchString(line) {
				excluded = true
				break
			}
		}
		if !excluded {
			for _, re := range patterns.Working {
				if re.MatchString(line) {
					return true
				}
			}
		}
		for _, re := range patterns.Idle {
			if re.MatchString(line) {
				return true
			}
		}
	}
	return false
}

