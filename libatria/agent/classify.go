package agent

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
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
	status, i := classifyLines(lines, agentType)
	if i < 0 {
		return status, ""
	}
	return status, strings.TrimSpace(lines[i])
}

// classifyLines is ClassifyScreen over split lines. It returns the index of
// the matched line, or -1 when nothing matched.
func classifyLines(lines []string, agentType Type) (Status, int) {
	for i, line := range lines {
		if sharedBellPattern.MatchString(line) {
			return StatusNeedsInput, i
		}
	}

	bottomStart := bottomRegion(lines, agentType)
	bestStatus, best := Status(""), -1
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
			bestStatus, best = status, i
		}
	}
	return bestStatus, best
}

// questionLookback bounds how far above a hint row PromptText reads, both
// to find the dialog's question and to join the rows it wrapped from. It is
// the shortest screen read atria takes. Blank rows can't bound the search,
// because Codex leaves the same two-row gap inside its approval dialog as
// between the dialog and the conversation above it.
const questionLookback = 25

// PromptText returns the text to show for a needs_input screen: the
// dialog's question, or the line ClassifyScreen matched when it finds none.
// Some dialogs are detected by a row below their question, such as a footer
// of key hints or a button row; for those it looks at most questionLookback
// rows upward from that row for the nearest line ending in "?", with
// box-drawing borders and cursor glyphs removed, and joins the rows that
// line wrapped from. When the dialog has numbered options, the question must
// sit just above them, and the search stops at the dialog's title and at
// rows that start a conversation message; see findQuestion. It returns ""
// when the screen isn't needs_input. The status, and ClassifyScreen's
// results, are the same either way.
func PromptText(screen string, agentType Type) string {
	lines := strings.Split(normalizeScreenText(screen), "\n")
	status, i := classifyLines(lines, agentType)
	if status != StatusNeedsInput {
		return ""
	}
	line := strings.TrimSpace(lines[i])
	if !sharedBellPattern.MatchString(line) && isHint(line, agentType) {
		if q := findQuestion(lines, i); q != "" {
			return q
		}
	}
	return line
}

// isHint reports whether the NeedsInput pattern that classifies line, the
// first in the agent's list that matches it, is a hint pattern.
func isHint(line string, agentType Type) bool {
	patterns := registry[agentType]
	if patterns == nil {
		return false
	}
	for _, re := range patterns.NeedsInput {
		if re.MatchString(line) {
			return hintPatterns[re]
		}
	}
	return false
}

// questionGap bounds how far above a dialog's first option its question may
// sit. Codex's approval dialog puts its Reason row five rows above its
// first option, with its command in between; the rest is room for a
// command that wraps.
const questionGap = 8

// findQuestion looks upward from the line before lines[from] for the
// dialog's question, a line ending in "?" once decoration is stripped, and
// returns it joined with the rows it wrapped from, or "" when there isn't
// one. It reads no row above lines[top], where top is questionLookback rows
// above from.
//
// A dialog with numbered options anchors the search on its first option,
// the nearest row starting "1." above the footer. The question must sit
// above that row, so an option or a description ending in "?" is never
// taken, and within questionGap rows of it. A text row that runs straight
// into the row below it, rather than ending at a blank row, is the dialog's
// title or subtitle, and the search stops there, so a dialog with no
// question doesn't borrow one from the conversation above it. A row that
// only wraps into the next one doesn't count. The search also stops at a
// conversation row, and rejects a question whose first row is one, which
// covers a dialog title followed by a blank row. A dialog without numbered
// options gets the nearest "?" line in the window.
func findQuestion(lines []string, from int) string {
	top := max(0, from-questionLookback)
	width := screenWidth(lines)
	first := -1
	for i := from - 1; i >= top; i-- {
		if strings.HasPrefix(stripDecoration(lines[i]), "1. ") {
			first = i
			break
		}
	}
	if first < 0 {
		for i := from - 1; i >= top; i-- {
			if s := stripDecoration(lines[i]); strings.HasSuffix(s, "?") {
				q, _ := joinWrapped(lines, top, i, s, width)
				return q
			}
		}
		return ""
	}
	for i := first - 1; i >= max(top, first-questionGap); i-- {
		s := stripDecoration(lines[i])
		switch {
		case conversationRow(lines[i]):
			return ""
		case s == "": // a blank row or a border
		case strings.HasSuffix(s, "?"):
			q, start := joinWrapped(lines, top, i, s, width)
			if conversationRow(lines[start]) {
				return "" // a wrapped prompt or reply, not the dialog's question
			}
			return q
		case strings.TrimSpace(lines[i+1]) == "":
			// Body text that ends at a blank row, like Codex's command.
		case i+1 != first && wrapped(lines[i], stripDecoration(lines[i+1]), width):
			// Body text that wraps into the row below.
		default:
			return ""
		}
	}
	return ""
}

// screenWidth estimates the pane's width from the widest row in the capture.
func screenWidth(lines []string) int {
	width := 0
	for _, l := range lines {
		width = max(width, ansi.StringWidth(strings.TrimRightFunc(l, unicode.IsSpace)))
	}
	return width
}

// wrapSlack is how many columns short of the pane's width a row can stop
// and still count as wrapped. Claude Code wraps a row whose next word would
// fit exactly, so the test can't demand a strict overflow.
const wrapSlack = 1

// joinWrapped rebuilds a question that wrapped onto several rows, given its
// last row, lines[end], stripped as q. A row above belongs to the question
// when the next row's first word wouldn't have fit on it, which is what a
// word-wrapping terminal UI does, so a short row ending before a long word
// still counts. It stops at a blank or structural row (a border, a tab bar,
// a numbered option), and at a row ending a sentence when the text below it
// starts with a capital, which is where a long row that happens to reach
// the edge most likely isn't a wrap. It joins no row above lines[top], and
// width is the pane's, from screenWidth. It also returns the index of the
// question's first row.
func joinWrapped(lines []string, top, end int, q string, width int) (string, int) {
	parts := []string{q}
	start := end
	for i := end - 1; i >= top; i-- {
		above := stripDecoration(lines[i])
		next := parts[0]
		if structuralRow(above) || !wrapped(lines[i], next, width) ||
			(endsSentence(above) && startsUpper(next)) {
			break
		}
		parts = append([]string{above}, parts...)
		start = i
	}
	return strings.Join(parts, " "), start
}

// conversationMarkers start a conversation row at column 0: Claude Code's
// prompt (❯) and reply (⏺), and Codex's prompt (›) and reply (•). A
// dialog's own rows are indented, except an option cursor, which is
// followed by its number.
var conversationMarkers = []string{"❯ ", "⏺ ", "› ", "• "}

// conversationRow reports whether row starts a message in the conversation
// rather than belonging to a dialog.
func conversationRow(row string) bool {
	for _, m := range conversationMarkers {
		if rest, ok := strings.CutPrefix(row, m); ok {
			return !numberedOption.MatchString(rest)
		}
	}
	return false
}

// wrapped reports whether row ran close enough to width that next's first
// word wouldn't have fit after it.
func wrapped(row, next string, width int) bool {
	words := strings.Fields(next)
	if len(words) == 0 {
		return false
	}
	used := ansi.StringWidth(strings.TrimRightFunc(row, unicode.IsSpace))
	return used+1+ansi.StringWidth(words[0]) > width-wrapSlack
}

var numberedOption = regexp.MustCompile(`^\d+\.\s`)

// structuralRow reports a stripped row that is dialog furniture rather than
// question text: empty (a blank row or a border), a tab bar, or an option.
func structuralRow(s string) bool {
	if s == "" {
		return true
	}
	r, _ := utf8.DecodeRuneInString(s)
	return strings.ContainsRune("←☐☒✔", r) || numberedOption.MatchString(s)
}

func endsSentence(s string) bool {
	return strings.HasSuffix(s, ".") || strings.HasSuffix(s, "!") || strings.HasSuffix(s, "?")
}

func startsUpper(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsUpper(r)
}

// stripDecoration removes what a dialog draws around its text: leading
// box-drawing borders and cursor glyphs, and trailing borders, along with
// the spaces around them.
func stripDecoration(s string) string {
	s = strings.TrimLeftFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || isBoxDrawing(r) || strings.ContainsRune("❯›>▶•", r)
	})
	return strings.TrimRightFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || isBoxDrawing(r)
	})
}

func isBoxDrawing(r rune) bool { return r >= 0x2500 && r <= 0x257F }

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
