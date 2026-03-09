package terminal

import (
	"os"
	"regexp"
	"strings"

	"github.com/sethdeckard/atria/internal/model"
)

// AgentPatterns holds compiled regexes for a specific agent type.
type AgentPatterns struct {
	NeedsInput     []*regexp.Regexp
	Working        []*regexp.Regexp
	WorkingExclude []*regexp.Regexp // lines matching these skip working detection
	Idle           []*regexp.Regexp
}

// Per-agent pattern definitions.

var claudePatterns = &AgentPatterns{
	NeedsInput: []*regexp.Regexp{
		regexp.MustCompile(`Do you want to proceed`),
		regexp.MustCompile(`Allow .+\?`),
		regexp.MustCompile(`Esc to cancel`),
	},
	Working: []*regexp.Regexp{
		regexp.MustCompile(`[✻✶·] \S+…`),
		regexp.MustCompile(`esc\s+to\s+interrupt`),
	},
	WorkingExclude: []*regexp.Regexp{
		regexp.MustCompile(`⏵`),
	},
	Idle: []*regexp.Regexp{
		regexp.MustCompile(`❯`),
		regexp.MustCompile(`\? for shortcuts`),
	},
}

var codexPatterns = &AgentPatterns{
	NeedsInput: []*regexp.Regexp{
		regexp.MustCompile(`Waiting for .+ input`),
	},
	Working: []*regexp.Regexp{
		regexp.MustCompile(`[•●] Working`),
		regexp.MustCompile(`esc\s+to\s+interrupt`),
	},
	WorkingExclude: []*regexp.Regexp{
		regexp.MustCompile(`⏵`),
	},
	Idle: []*regexp.Regexp{
		regexp.MustCompile(`›`),
		regexp.MustCompile(`gpt-\S+-codex`),
	},
}

var openCodePatterns = &AgentPatterns{
	NeedsInput: []*regexp.Regexp{
		regexp.MustCompile(`Permission required`),
		regexp.MustCompile(`Allow once`),
	},
	Working: []*regexp.Regexp{
		regexp.MustCompile(`esc\s+interrupt`),
	},
	WorkingExclude: []*regexp.Regexp{
		regexp.MustCompile(`⏵`),
	},
	Idle: []*regexp.Regexp{
		regexp.MustCompile(`ctrl\+p commands`),
	},
}

var agentPatternRegistry = map[model.AgentType]*AgentPatterns{
	model.AgentClaude:   claudePatterns,
	model.AgentCodex:    codexPatterns,
	model.AgentOpenCode: openCodePatterns,
}

// Shared patterns that apply to all agent types.
var (
	sharedBellPattern      = regexp.MustCompile("\x07")
	sharedErrorPattern     = regexp.MustCompile(`Error:`)
	sharedCompletedPattern = regexp.MustCompile(`✓|completed|No findings`)
	sharedShellPrompt      = regexp.MustCompile(`\$ $`)
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
