package agent

import (
	"regexp"
	"strings"
)

var detectableAgents = []Type{
	Claude,
	Codex,
	OpenCode,
	Copilot,
}

func normalizeScreenText(s string) string {
	return strings.ReplaceAll(s, "\x00", " ")
}

var brandedAgentPatterns = map[Type][]*regexp.Regexp{
	Claude: {
		regexp.MustCompile(`(?m)^\s*Claude Code(?:\s+v[\d.]+)?\b`),
	},
	Codex: {
		regexp.MustCompile(`(?m)\bgpt-\S+-codex\b`),
		regexp.MustCompile(`(?m)^\s*OpenAI Codex\b`),
	},
	OpenCode: {
		regexp.MustCompile(`(?m)^\s*OC \| .+\(opencode\)\s*$`),
		regexp.MustCompile(`(?m)^\s*(?:•\s*)?OpenCode(?:\s+\d[\w.]*)?\b`),
	},
	Copilot: {
		regexp.MustCompile(`(?m)^\s*GitHub Copilot\b`),
	},
}

// ClassifyOutput classifies one line of screen text for the given agent and
// returns "" when nothing matches. The order is fixed: a bell character, the
// agent's needs_input patterns, the shared "Error:" pattern, the agent's
// working patterns (unless a WorkingExclude pattern also matches), the shared
// completed and shell-prompt patterns, then the agent's idle patterns. Use
// ClassifyScreen for whole screens; it adds the bottom-region rule.
func ClassifyOutput(text string, agentType Type) Status {
	// 1. Shared bell → needs_input
	if sharedBellPattern.MatchString(text) {
		return StatusNeedsInput
	}

	patterns := registry[agentType]

	// 2. Agent-specific needs_input
	if patterns != nil {
		for _, re := range patterns.NeedsInput {
			if re.MatchString(text) {
				return StatusNeedsInput
			}
		}
	}

	// 3. Shared error
	if sharedErrorPattern.MatchString(text) {
		return StatusError
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
					return StatusWorking
				}
			}
		}
	}

	// 5. Shared idle (completed, shell prompt)
	if sharedCompletedPattern.MatchString(text) {
		return StatusIdle
	}
	if sharedShellPrompt.MatchString(text) {
		return StatusIdle
	}

	// 6. Agent-specific idle
	if patterns != nil {
		for _, re := range patterns.Idle {
			if re.MatchString(text) {
				return StatusIdle
			}
		}
	}

	return ""
}

// statusPriority returns a numeric priority for status (lower = more urgent).
func statusPriority(s Status) int {
	switch s {
	case StatusNeedsInput:
		return 0
	case StatusError:
		return 1
	case StatusWorking:
		return 2
	case StatusIdle:
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
func bottomRegion(lines []string, agentType Type) int {
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
func liveAnchor(lines []string, lastNonBlank int, agentType Type) int {
	if agentType != Claude {
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

// ClassifyScreen classifies a multi-line screen capture and returns the most
// urgent status found together with the line that matched, or "" and "" when
// no line matched. Active statuses (needs_input, error, working) count only
// in the bottom region, because scrollback above quotes prompt text and
// spinners. The region starts seven lines above the live anchor (the last
// non-blank line, or for Claude Code the last line above a trailing todo/task
// footer) and runs to the end of the capture. Idle patterns count anywhere.
// Pass 25 lines or more: Codex pads its screen with blank lines and its
// prompt can sit 20 lines from the bottom.
//
// A bell character anywhere in the capture is needs_input regardless of
// region: a bell is an event the backend delivered with the read (the PTY
// backend prefixes it to the first line), not text that could be quoted in
// scrollback. The returned line is the one carrying the bell.
func ClassifyScreen(content string, agentType Type) (Status, string) {
	lines := strings.Split(normalizeScreenText(content), "\n")
	bestStatus := Status("")
	bestLine := ""

	for _, line := range lines {
		if sharedBellPattern.MatchString(line) {
			return StatusNeedsInput, strings.TrimSpace(line)
		}
	}

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
		if i < bottomStart && status != StatusIdle {
			continue
		}
		if bestStatus == "" || statusPriority(status) < statusPriority(bestStatus) {
			bestStatus = status
			bestLine = line
		}
	}

	return bestStatus, bestLine
}

// HasScreen reports whether the agent's UI (any of its needs_input, working,
// or idle patterns) appears in the bottom region of the screen. Unlike
// ClassifyScreen it restricts idle patterns to the bottom region too, so the
// scrollback of an agent that already exited is not a positive signal. It is
// the check to run before dropping a session whose title stopped naming an
// agent.
func HasScreen(content string, agentType Type) bool {
	return hasAgentScreen(content, agentType, true)
}

func hasAgentScreen(content string, agentType Type, includeIdle bool) bool {
	patterns := registry[agentType]
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

func hasActiveAgentScreen(content string, agentType Type) bool {
	return hasAgentScreen(content, agentType, false)
}

// InferFromScreen identifies the agent from screen text when the title gave
// nothing. Product text ("Claude Code", "OpenAI Codex", a gpt-*-codex model
// line, "OpenCode", "GitHub Copilot") takes precedence; when several products
// match, which one is returned is unspecified. Otherwise the agents' active
// patterns are tried in the bottom region, then all of their patterns; each
// stage returns a type only when exactly one agent matches, and "" when none
// or several do.
func InferFromScreen(content string) Type {
	content = normalizeScreenText(content)
	for agentType, patterns := range brandedAgentPatterns {
		for _, re := range patterns {
			if re.MatchString(content) {
				return agentType
			}
		}
	}

	var activeMatches []Type
	for _, agentType := range detectableAgents {
		if hasActiveAgentScreen(content, agentType) {
			activeMatches = append(activeMatches, agentType)
		}
	}
	if len(activeMatches) == 1 {
		return activeMatches[0]
	}

	var matches []Type
	for _, agentType := range detectableAgents {
		if HasScreen(content, agentType) {
			matches = append(matches, agentType)
		}
	}
	if len(matches) == 1 {
		return matches[0]
	}
	return ""
}
