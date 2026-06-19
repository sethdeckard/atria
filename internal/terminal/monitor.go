package terminal

import (
	"regexp"
	"strings"

	"github.com/sethdeckard/atria/internal/model"
)

var detectableAgents = []model.AgentType{
	model.AgentClaude,
	model.AgentCodex,
	model.AgentOpenCode,
	model.AgentCopilot,
}

func normalizeScreenText(s string) string {
	return strings.ReplaceAll(s, "\x00", " ")
}

var brandedAgentPatterns = map[model.AgentType][]*regexp.Regexp{
	model.AgentClaude: {
		regexp.MustCompile(`(?m)^\s*Claude Code(?:\s+v[\d.]+)?\b`),
	},
	model.AgentCodex: {
		regexp.MustCompile(`(?m)\bgpt-\S+-codex\b`),
		regexp.MustCompile(`(?m)^\s*OpenAI Codex\b`),
	},
	model.AgentOpenCode: {
		regexp.MustCompile(`(?m)^\s*OC \| .+\(opencode\)\s*$`),
		regexp.MustCompile(`(?m)^\s*(?:•\s*)?OpenCode(?:\s+\d[\w.]*)?\b`),
	},
	model.AgentCopilot: {
		regexp.MustCompile(`(?m)^\s*GitHub Copilot\b`),
	},
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

// todoFooterMaxScan bounds how far above the last non-blank line we look for a
// trailing todo-footer header, so a stray match in scrollback can't relocate
// the anchor.
const todoFooterMaxScan = 24

// todoFooterPattern matches the header of Claude Code's persistent todo/task
// summary (e.g. "9 tasks (4 done, 1 in progress, 4 open)"), which renders
// below the live prompt or working indicator.
var todoFooterPattern = regexp.MustCompile(`(?i)^\s*\d+\s+tasks?\s+\(\d+\s+(?:done|open|in\s+progress)`)

// bottomRegion returns the start index of the bottom region, measured from the
// live UI anchor. Used to restrict active-status matching to the live UI area
// and ignore scrollback history.
func bottomRegion(lines []string, agentType model.AgentType) int {
	lastNonBlank := 0
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			lastNonBlank = i
			break
		}
	}
	start := liveAnchor(lines, lastNonBlank, agentType) - bottomLineCount + 1
	if start < 0 {
		start = 0
	}
	return start
}

// liveAnchor returns the index of the last line of the live UI. Claude Code
// renders a persistent todo/task footer below the active prompt or working
// indicator; left alone, that footer pulls the bottom region down and hides the
// prompt, so needs_input/working go undetected. When a trailing todo-footer
// header is found near the bottom, the anchor moves to the last non-blank line
// above it. The footer pattern is Claude-specific, so the adjustment is applied
// only for Claude — other agents keep the plain lastNonBlank anchor, avoiding
// false anchor shifts from coincidental "N tasks (...)" text in their output.
func liveAnchor(lines []string, lastNonBlank int, agentType model.AgentType) int {
	if agentType != model.AgentClaude {
		return lastNonBlank
	}
	limit := lastNonBlank - todoFooterMaxScan
	if limit < 0 {
		limit = 0
	}
	for i := lastNonBlank; i >= limit; i-- {
		if todoFooterPattern.MatchString(lines[i]) {
			for j := i - 1; j >= 0; j-- {
				if strings.TrimSpace(lines[j]) != "" {
					return j
				}
			}
			return 0
		}
	}
	return lastNonBlank
}

// ClassifyScreen checks each line of multi-line screen content and returns
// the highest priority status found. Active statuses (needs_input, error,
// working) are only matched in the bottom region where the live UI appears.
// The bottom region is measured from the last non-blank line (not the
// absolute bottom) to handle blank padding below dialog prompts.
// Idle/completed can match anywhere.
func ClassifyScreen(content string, agentType model.AgentType) (model.AgentStatus, string) {
	lines := strings.Split(normalizeScreenText(content), "\n")
	bestStatus := model.AgentStatus("")
	bestLine := ""

	bottomStart := bottomRegion(lines, agentType)

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
	return hasAgentScreen(content, agentType, true)
}

func hasAgentScreen(content string, agentType model.AgentType, includeIdle bool) bool {
	patterns := agentPatternRegistry[agentType]
	if patterns == nil {
		return false
	}
	lines := strings.Split(normalizeScreenText(content), "\n")

	bottomStart := bottomRegion(lines, agentType)

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
		if includeIdle {
			for _, re := range patterns.Idle {
				if re.MatchString(line) {
					return true
				}
			}
		}
	}
	return false
}

func hasActiveAgentScreen(content string, agentType model.AgentType) bool {
	return hasAgentScreen(content, agentType, false)
}

// InferAgentFromScreen tries to identify the agent from screen content.
// It prefers explicit product text, then falls back to agent-specific bottom
// region patterns. If multiple agents remain plausible, it returns "".
func InferAgentFromScreen(content string) model.AgentType {
	content = normalizeScreenText(content)
	for agentType, patterns := range brandedAgentPatterns {
		for _, re := range patterns {
			if re.MatchString(content) {
				return agentType
			}
		}
	}

	var activeMatches []model.AgentType
	for _, agentType := range detectableAgents {
		if hasActiveAgentScreen(content, agentType) {
			activeMatches = append(activeMatches, agentType)
		}
	}
	if len(activeMatches) == 1 {
		return activeMatches[0]
	}

	var matches []model.AgentType
	for _, agentType := range detectableAgents {
		if HasAgentScreen(content, agentType) {
			matches = append(matches, agentType)
		}
	}
	if len(matches) == 1 {
		return matches[0]
	}
	return ""
}
