package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/sethdeckard/atria/internal/model"
)

type sortColumn int

const (
	sortByAgent sortColumn = iota
	sortByHarness
	sortByStatus
	sortByUpdated
	sortColumnCount // sentinel for cycling
)

type projectRow struct {
	project     *model.Project
	session     *model.AgentSession
	displayName string
}

func buildRows(store interface {
	Projects() []*model.Project
	GetSessions(string) []*model.AgentSession
}) []projectRow {
	projects := store.Projects()
	var rows []projectRow
	for _, p := range projects {
		sessions := store.GetSessions(p.Dir)
		for _, s := range sessions {
			rows = append(rows, projectRow{project: p, session: s, displayName: p.DisplayName()})
		}
	}

	// Disambiguate duplicate display names with #2, #3 suffixes
	counts := make(map[string]int)
	for _, r := range rows {
		counts[r.displayName]++
	}
	seen := make(map[string]int)
	for i, r := range rows {
		if counts[r.displayName] > 1 {
			seen[r.displayName]++
			if seen[r.displayName] > 1 {
				rows[i].displayName = fmt.Sprintf("%s #%d", r.displayName, seen[r.displayName])
			}
		}
	}

	return rows
}

func sortRows(rows []projectRow, col sortColumn, desc bool) {
	sort.SliceStable(rows, func(i, j int) bool {
		if desc {
			i, j = j, i
		}
		switch col {
		case sortByAgent:
			return rows[i].displayName < rows[j].displayName
		case sortByHarness:
			ti := string(rows[i].session.Type)
			tj := string(rows[j].session.Type)
			if ti != tj {
				return ti < tj
			}
			return rows[i].displayName < rows[j].displayName
		case sortByStatus:
			si := statusPriority(rows[i].session)
			sj := statusPriority(rows[j].session)
			if si != sj {
				return si < sj
			}
			return rows[i].displayName < rows[j].displayName
		case sortByUpdated:
			ti := rows[i].session.LastActivity
			tj := rows[j].session.LastActivity
			if !ti.Equal(tj) {
				return ti.After(tj) // most recent first in ascending
			}
			return rows[i].displayName < rows[j].displayName
		default:
			return rows[i].displayName < rows[j].displayName
		}
	})
}

func statusPriority(s *model.AgentSession) int {
	if s == nil {
		return 4
	}
	switch s.Status {
	case model.StatusNeedsInput:
		return 0
	case model.StatusWorking:
		return 1
	case model.StatusError:
		return 2
	case model.StatusIdle:
		return 3
	default:
		return 1
	}
}

// headerLineCount is the number of rendered header lines:
// title bar (2) + column headers (1).
const headerLineCount = 3

// footerLineCount is lines for the footer (margin + content).
const footerLineCount = 3

// timeColWidth is the fixed column budget for the "updated" / time column.
const timeColWidth = 10

func renderHeader(width int) string {
	return renderTitleBar("agents", width)
}

func sortIndicator(col, active sortColumn, desc bool) string {
	if col != active {
		return ""
	}
	if desc {
		return "▼"
	}
	return "▲"
}

func renderColumnHeaders(nameWidth, typeWidth, totalWidth int, col sortColumn, desc bool, showEnv bool, envWidth int) string {
	name := fmt.Sprintf("  %-*s", nameWidth, "agent"+sortIndicator(sortByAgent, col, desc))
	harness := fmt.Sprintf("%-*s", typeWidth, "type"+sortIndicator(sortByHarness, col, desc))

	envCol := ""
	if showEnv {
		envCol = fmt.Sprintf("%-*s", envWidth, "env")
	}

	// status + updated fill the rest
	remaining := totalWidth - lipgloss.Width(name) - typeWidth - timeColWidth
	if showEnv {
		remaining -= envWidth
	}
	if remaining < 10 {
		remaining = 10
	}
	status := fmt.Sprintf("%-*s", remaining, "status"+sortIndicator(sortByStatus, col, desc))

	updated := "updated" + sortIndicator(sortByUpdated, col, desc)

	line := name + harness + envCol + status + updated
	return dimStyle.Render(line)
}

func envLabel(source string) string {
	if source == "pty" {
		return "embedded"
	}
	if source == "" {
		return ""
	}
	return source
}

func renderProjectList(rows []projectRow, cursor int, width int, spinnerFrame int, attentionSessions map[string]time.Time, defaultAgent model.AgentType, availableAgents []model.AgentType, maxRows int, scrollOffset int, sortCol sortColumn, sortDesc bool, canSetup bool, streamOpen bool, lp layoutPolicy) string {
	var sb strings.Builder

	narrow := lp.mode != layoutWide

	// Title bar — include sort label when column headers are hidden
	if narrow {
		sortLabel := sortColumnLabel(sortCol, sortDesc)
		sb.WriteString(renderTitleBarWithSort("agents", sortLabel, width, lp.showBranding()))
	} else {
		sb.WriteString(renderHeader(width))
	}

	if len(rows) == 0 {
		sb.WriteString(renderEmptyState(defaultAgent, len(availableAgents) > 1, availableAgents, canSetup, lp))
		return sb.String()
	}

	if narrow {
		// Narrow: two-line card rows, no column headers
		sb.WriteString(renderNarrowRows(&sb, rows, cursor, maxRows, scrollOffset, spinnerFrame, attentionSessions, lp, width))
	} else {
		// Wide: columnar table
		sb.WriteString(renderWideRows(rows, cursor, width, maxRows, scrollOffset, spinnerFrame, attentionSessions, sortCol, sortDesc, streamOpen))
	}

	return sb.String()
}

func renderWideRows(rows []projectRow, cursor, width, maxRows, scrollOffset, spinnerFrame int, attentionSessions map[string]time.Time, sortCol sortColumn, sortDesc bool, streamOpen bool) string {
	var sb strings.Builder

	// Determine if env column should be shown
	showEnv := false
	for _, r := range rows {
		if r.session.Source != "" && r.session.Source != "pty" {
			showEnv = true
			break
		}
	}
	envWidth := 10

	// Compute column widths
	nameWidth := 20
	typeWidth := 10
	for _, r := range rows {
		if len(r.displayName) > nameWidth-2 {
			nameWidth = len(r.displayName) + 2
		}
	}
	if nameWidth > 40 {
		nameWidth = 40
	}

	sb.WriteString(renderColumnHeaders(nameWidth, typeWidth, width, sortCol, sortDesc, showEnv, envWidth))
	sb.WriteString("\n")

	rowWidth := width
	if streamOpen && rowWidth > 1 {
		rowWidth--
	}

	if len(rows) <= maxRows || maxRows <= 0 {
		scrollOffset = 0
	} else {
		if cursor < scrollOffset {
			scrollOffset = cursor
		} else if cursor >= scrollOffset+maxRows {
			scrollOffset = cursor - maxRows + 1
		}
	}
	visibleRows := rows
	if maxRows > 0 && len(rows) > maxRows {
		end := scrollOffset + maxRows
		if end > len(rows) {
			end = len(rows)
		}
		visibleRows = rows[scrollOffset:end]
	}

	rendered := 0
	for i, r := range visibleRows {
		actualIdx := i + scrollOffset
		_, hasAttention := attentionSessions[r.session.SessionID]
		isSelected := actualIdx == cursor

		switch {
		case isSelected && hasAttention:
			line := formatRow(r, nameWidth, typeWidth, rowWidth, spinnerFrame, true, showEnv, envWidth)
			sb.WriteString(attentionSelectedStyle.Render(padToWidth(line, rowWidth)))
		case isSelected:
			line := formatSelectedRow(r, nameWidth, typeWidth, rowWidth, spinnerFrame, showEnv, envWidth)
			sb.WriteString(line)
		case hasAttention:
			line := formatRow(r, nameWidth, typeWidth, rowWidth, spinnerFrame, true, showEnv, envWidth)
			sb.WriteString(attentionRowStyle.Render(padToWidth(line, rowWidth)))
		default:
			line := formatRow(r, nameWidth, typeWidth, rowWidth, spinnerFrame, false, showEnv, envWidth)
			sb.WriteString(line)
		}
		sb.WriteString("\n")
		rendered++
	}
	for rendered < maxRows && maxRows > 0 {
		sb.WriteString(strings.Repeat(" ", width) + "\n")
		rendered++
	}

	return sb.String()
}

func renderNarrowRows(_ *strings.Builder, rows []projectRow, cursor, maxRows, scrollOffset, spinnerFrame int, attentionSessions map[string]time.Time, lp layoutPolicy, width int) string {
	var sb strings.Builder

	if len(rows) <= maxRows || maxRows <= 0 {
		scrollOffset = 0
	} else {
		if cursor < scrollOffset {
			scrollOffset = cursor
		} else if cursor >= scrollOffset+maxRows {
			scrollOffset = cursor - maxRows + 1
		}
	}
	visibleRows := rows
	if maxRows > 0 && len(rows) > maxRows {
		end := scrollOffset + maxRows
		if end > len(rows) {
			end = len(rows)
		}
		visibleRows = rows[scrollOffset:end]
	}

	rendered := 0
	for i, r := range visibleRows {
		actualIdx := i + scrollOffset
		_, hasAttention := attentionSessions[r.session.SessionID]
		isSelected := actualIdx == cursor

		switch {
		case isSelected && hasAttention:
			card := formatNarrowRow(r, lp, spinnerFrame, true)
			for _, line := range strings.Split(card, "\n") {
				sb.WriteString(attentionSelectedStyle.Render(padToWidth(line, width)))
				sb.WriteString("\n")
			}
		case isSelected:
			card := formatNarrowSelectedRow(r, lp, spinnerFrame)
			sb.WriteString(card)
			sb.WriteString("\n")
		case hasAttention:
			card := formatNarrowRow(r, lp, spinnerFrame, true)
			for _, line := range strings.Split(card, "\n") {
				sb.WriteString(attentionRowStyle.Render(padToWidth(line, width)))
				sb.WriteString("\n")
			}
		default:
			card := formatNarrowRow(r, lp, spinnerFrame, false)
			sb.WriteString(card)
			sb.WriteString("\n")
		}
		rendered++
	}
	// Pad to maxRows*2 lines so list height is stable
	totalLines := rendered * 2
	targetLines := maxRows * 2
	for totalLines < targetLines && maxRows > 0 {
		sb.WriteString(strings.Repeat(" ", width) + "\n")
		totalLines++
	}

	return sb.String()
}

// sortColumnLabel returns a short label for the current sort, used in narrow mode title bar.
func sortColumnLabel(col sortColumn, desc bool) string {
	arrow := "▲"
	if desc {
		arrow = "▼"
	}
	switch col {
	case sortByAgent:
		return "name " + arrow
	case sortByHarness:
		return "type " + arrow
	case sortByStatus:
		return "status " + arrow
	case sortByUpdated:
		return "updated " + arrow
	default:
		return ""
	}
}

// padToWidth pads a string with spaces to reach the target visual width.
func padToWidth(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

var agentTypeInfo = map[model.AgentType]struct {
	label string
	style lipgloss.Style
}{
	model.AgentClaude:   {"Claude", agentClaudeStyle},
	model.AgentCodex:    {"Codex", agentCodexStyle},
	model.AgentOpenCode: {"OpenCode", agentOpenCodeStyle},
	model.AgentCopilot:  {"Copilot", agentCopilotStyle},
}

func agentTypeLabel(t model.AgentType) string {
	if info, ok := agentTypeInfo[t]; ok {
		return info.label
	}
	s := string(t)
	return strings.ToUpper(s[:1]) + s[1:]
}

func agentTypeStyle(t model.AgentType) lipgloss.Style {
	if info, ok := agentTypeInfo[t]; ok {
		return info.style
	}
	return normalStyle
}

// rowColumns holds pre-computed, unstyled column strings for a single row.
type rowColumns struct {
	name      string
	agent     string
	agentSty  lipgloss.Style
	env       string
	status    string
	statusSty lipgloss.Style
	time      string
	remaining int
}

func computeRowColumns(r projectRow, nameWidth, typeWidth, totalWidth, spinnerFrame int, showEnv bool, envWidth int) rowColumns {
	name := r.displayName
	if len(name) > nameWidth-2 {
		name = name[:nameWidth-3] + "\u2026"
	}
	nameStr := fmt.Sprintf("  %-*s", nameWidth, name)

	agentCol := fmt.Sprintf("%-*s", typeWidth, agentTypeLabel(r.session.Type))

	envCol := ""
	if showEnv {
		envCol = fmt.Sprintf("%-*s", envWidth, envLabel(r.session.Source))
	}

	statusStr, style := formatStatus(r.session, spinnerFrame)

	timeStr := ""
	if !r.session.LastActivity.IsZero() {
		timeStr = relativeTime(r.session.LastActivity)
	}

	remaining := totalWidth - lipgloss.Width(nameStr) - typeWidth - timeColWidth
	if showEnv {
		remaining -= envWidth
	}
	if remaining < 2 {
		remaining = 2
	}
	if lipgloss.Width(statusStr) > remaining {
		statusStr = statusStr[:remaining-1] + "\u2026"
	}

	return rowColumns{
		name:      nameStr,
		agent:     agentCol,
		agentSty:  agentTypeStyle(r.session.Type),
		env:       envCol,
		status:    statusStr,
		statusSty: style,
		time:      timeStr,
		remaining: remaining,
	}
}

// formatRow builds a single-line row. When plain is true, no inner styles are
// applied so a row-level style can wrap the whole line cleanly.
func formatRow(r projectRow, nameWidth, typeWidth, totalWidth int, spinnerFrame int, plain bool, showEnv bool, envWidth int) string {
	c := computeRowColumns(r, nameWidth, typeWidth, totalWidth, spinnerFrame, showEnv, envWidth)
	agentCol := c.agent
	statusCol := lipgloss.NewStyle().Width(c.remaining).Render(c.status)
	if !plain {
		agentCol = c.agentSty.Render(c.agent)
		statusCol = c.statusSty.Width(c.remaining).Render(c.status)
	}
	return c.name + agentCol + c.env + statusCol + c.time
}

// formatSelectedRow builds a selected row where the status retains its color
// on a purple background, while other columns get white text on purple.
func formatSelectedRow(r projectRow, nameWidth, typeWidth, totalWidth int, spinnerFrame int, showEnv bool, envWidth int) string {
	c := computeRowColumns(r, nameWidth, typeWidth, totalWidth, spinnerFrame, showEnv, envWidth)
	selEnv := ""
	if showEnv {
		selEnv = selectedTextStyle.Render(c.env)
	}
	selAgent := withSelectedBg(c.agentSty).Bold(true).Render(c.agent)
	selStatus := withSelectedBg(c.statusSty).Bold(true).Width(c.remaining).Render(c.status)
	return selectedTextStyle.Render(c.name) +
		selAgent +
		selEnv + selStatus +
		selectedTextStyle.Render(c.time)
}

// formatNarrowRow builds a two-line card for narrow layouts.
//
// Layout varies by mode to prioritize the right information at each width:
//
//	layoutNarrow  (36–55): Line 1: name [· Type]      Line 2: status [time]
//	layoutCompact (28–35): Line 1: name               Line 2: status [· Type]
//	layoutSurvival  (<28): Line 1: name               Line 2: status glyph only
//
// When plain is true, no inner styles are applied (for attention row wrapping).
func formatNarrowRow(r projectRow, lp layoutPolicy, spinnerFrame int, plain bool) string {
	maxW := lp.width - 2 // 2-char indent
	if maxW < 1 {
		maxW = 1
	}

	name := r.displayName
	statusStr, statusSty := formatStatus(r.session, spinnerFrame)
	typeLabel := agentTypeLabel(r.session.Type)

	var line1, line2 string

	switch lp.mode {
	case layoutSurvival:
		// Line 1: name only. Line 2: status glyph only.
		line1 = narrowPadLine("  "+truncateToWidth(name, maxW), lp.width)
		statusStr = truncateToWidth(statusStr, maxW)
		if plain {
			line2 = narrowPadLine("  "+statusStr, lp.width)
		} else {
			styled := "  " + statusSty.Render(statusStr)
			line2 = narrowPadLine(styled, lp.width)
		}

	case layoutCompact:
		// Line 1: name owns the full line. Line 2: status [· Type].
		line1 = narrowPadLine("  "+truncateToWidth(name, maxW), lp.width)

		// Append abbreviated type to status line if it fits
		typeSuffix := ""
		if lp.showType {
			candidate := " · " + typeLabel
			if lipgloss.Width(statusStr)+lipgloss.Width(candidate) <= maxW {
				typeSuffix = candidate
			}
		}
		statusStr = truncateToWidth(statusStr, maxW-lipgloss.Width(typeSuffix))

		if plain {
			line2 = narrowPadLine("  "+statusStr+typeSuffix, lp.width)
		} else {
			styled := "  " + statusSty.Render(statusStr)
			if typeSuffix != "" {
				styled += dimStyle.Render(" · ") + agentTypeStyle(r.session.Type).Render(typeLabel)
			}
			line2 = narrowPadLine(styled, lp.width)
		}

	default: // layoutNarrow
		// Line 1: name [· Type]. Line 2: status [time].
		typeStr := ""
		if lp.showType {
			candidate := " · " + typeLabel
			if lipgloss.Width(name)+lipgloss.Width(candidate) <= maxW {
				typeStr = candidate
			}
		}

		if plain {
			content := truncateToWidth(name+typeStr, maxW)
			// If type caused name to truncate, drop type
			if typeStr != "" && lipgloss.Width(content) < lipgloss.Width(name) {
				content = truncateToWidth(name, maxW)
			}
			line1 = narrowPadLine("  "+content, lp.width)
		} else {
			if typeStr != "" {
				styled := "  " + name + dimStyle.Render(" · ") + agentTypeStyle(r.session.Type).Render(typeLabel)
				line1 = narrowPadLine(styled, lp.width)
			} else {
				line1 = narrowPadLine("  "+truncateToWidth(name, maxW), lp.width)
			}
		}

		timeStr := ""
		if lp.showTime && !r.session.LastActivity.IsZero() {
			timeStr = relativeTime(r.session.LastActivity)
		}
		statusStr = truncateToWidth(statusStr, maxW-lipgloss.Width(timeStr))

		if plain {
			gap := maxW - lipgloss.Width(statusStr) - lipgloss.Width(timeStr)
			if gap < 0 {
				gap = 0
			}
			line2 = "  " + statusStr + strings.Repeat(" ", gap) + timeStr
		} else {
			styledStatus := statusSty.Render(statusStr)
			gap := lp.width - 2 - lipgloss.Width(styledStatus) - lipgloss.Width(timeStr)
			if gap < 0 {
				gap = 0
			}
			line2 = "  " + styledStatus + strings.Repeat(" ", gap) + timeStr
		}
	}

	return line1 + "\n" + line2
}

// narrowPadLine pads a rendered line to width using lipgloss.Width measurement.
func narrowPadLine(s string, width int) string {
	if pad := width - lipgloss.Width(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

// formatNarrowSelectedRow builds a two-line selected card.
// Selection styling overrides agent-type colors for a uniform highlight.
// Each line is styled independently to avoid lipgloss width issues with newlines.
func formatNarrowSelectedRow(r projectRow, lp layoutPolicy, spinnerFrame int) string {
	maxW := lp.width - 2
	if maxW < 1 {
		maxW = 1
	}

	name := r.displayName
	statusStr, _ := formatStatus(r.session, spinnerFrame)
	typeLabel := agentTypeLabel(r.session.Type)

	var line1, line2 string

	switch lp.mode {
	case layoutSurvival:
		// Line 1: name. Line 2: status.
		line1 = selectedTextStyle.Render(padToWidth("  "+truncateToWidth(name, maxW), lp.width))
		line2 = selectedTextStyle.Render(padToWidth("  "+truncateToWidth(statusStr, maxW), lp.width))

	case layoutCompact:
		// Line 1: name. Line 2: status [· Type].
		line1 = selectedTextStyle.Render(padToWidth("  "+truncateToWidth(name, maxW), lp.width))

		typeSuffix := ""
		if lp.showType {
			candidate := " · " + typeLabel
			if lipgloss.Width(statusStr)+lipgloss.Width(candidate) <= maxW {
				typeSuffix = candidate
			}
		}
		statusStr = truncateToWidth(statusStr, maxW-lipgloss.Width(typeSuffix))
		line2 = selectedTextStyle.Render(padToWidth("  "+statusStr+typeSuffix, lp.width))

	default: // layoutNarrow
		// Line 1: name [· Type]. Line 2: status [time].
		typeStr := ""
		if lp.showType {
			candidate := " · " + typeLabel
			if lipgloss.Width(name)+lipgloss.Width(candidate) <= maxW {
				typeStr = candidate
			}
		}
		if typeStr != "" {
			line1 = selectedTextStyle.Render(padToWidth("  "+name+typeStr, lp.width))
		} else {
			line1 = selectedTextStyle.Render(padToWidth("  "+truncateToWidth(name, maxW), lp.width))
		}

		timeStr := ""
		if lp.showTime && !r.session.LastActivity.IsZero() {
			timeStr = relativeTime(r.session.LastActivity)
		}
		statusStr = truncateToWidth(statusStr, maxW-lipgloss.Width(timeStr))
		gap := maxW - lipgloss.Width(statusStr) - lipgloss.Width(timeStr)
		if gap < 0 {
			gap = 0
		}
		line2 = selectedTextStyle.Render(padToWidth("  "+statusStr+strings.Repeat(" ", gap)+timeStr, lp.width))
	}

	return line1 + "\n" + line2
}

func formatStatus(s *model.AgentSession, spinnerFrame int) (string, lipgloss.Style) {
	switch s.Status {
	case model.StatusNeedsInput:
		text := "\u26a0 " + s.Attention
		if text == "\u26a0 " {
			text = "\u26a0 needs input"
		}
		return text, statusNeedsInputStyle
	case model.StatusWorking:
		spin := spinnerFrames[spinnerFrame%len(spinnerFrames)]
		text := spin + " "
		if s.Activity != "" {
			text += s.Activity
		} else {
			text += "working..."
		}
		return text, statusWorkingStyle
	case model.StatusIdle:
		text := "\u25cf idle"
		if s.Activity != "" {
			text = "\u25cf " + s.Activity
		}
		return text, statusIdleStyle
	case model.StatusError:
		text := "\u2717 error"
		if s.Attention != "" {
			text = "\u2717 " + s.Attention
		}
		return text, statusErrorStyle
	default:
		spin := spinnerFrames[spinnerFrame%len(spinnerFrames)]
		return spin + " working...", statusWorkingStyle
	}
}

func relativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		m := int(d.Minutes())
		return fmt.Sprintf("%dm ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		return fmt.Sprintf("%dh ago", h)
	default:
		days := int(d.Hours() / 24)
		return fmt.Sprintf("%dd ago", days)
	}
}

func renderEmptyState(defaultAgent model.AgentType, canToggle bool, availableAgents []model.AgentType, canSetup bool, lp layoutPolicy) string {
	var sb strings.Builder

	if lp.showLogo() {
		logo := logoStyle.Render(Logo)
		sb.WriteString(logo)
	} else {
		sb.WriteString(titleStyle.Render("  atria"))
	}
	sb.WriteString("\n\n")
	sb.WriteString(emptyHintStyle.Render("  Agent multiplexer for your terminal."))
	sb.WriteString("\n\n")
	if canSetup {
		sb.WriteString("  " + selectedTextStyle.Render("S") + emptyHintStyle.Render("  ") + titleStyle.Render("run setup") + emptyHintStyle.Render(" \u2014 configure integrations and watch directories"))
		sb.WriteString("\n\n")
	}
	sb.WriteString("  " + emptyKeyStyle.Render("n") + emptyHintStyle.Render("  new agent in a directory"))
	sb.WriteString("\n")
	if canToggle {
		var names []string
		for _, a := range availableAgents {
			names = append(names, agentTypeLabel(a))
		}
		sb.WriteString("  " + emptyKeyStyle.Render("t") + emptyHintStyle.Render(fmt.Sprintf("  cycle agent (%s)", strings.Join(names, "/"))))
		sb.WriteString("\n")
	}
	sb.WriteString("  " + emptyKeyStyle.Render("?") + emptyHintStyle.Render("  show all key bindings"))
	sb.WriteString("\n")
	sb.WriteString("  " + emptyKeyStyle.Render("q") + emptyHintStyle.Render("  quit"))
	sb.WriteString("\n")

	return sb.String()
}

func renderFooter(rowCount int, selected *projectRow, defaultAgent model.AgentType, streamOpen bool, width int, lp layoutPolicy) string {
	var footer string

	switch lp.mode {
	case layoutSurvival:
		// Bare minimum keys
		parts := []string{"v", "?", "q"}
		footer = " " + strings.Join(parts, " ")
	case layoutCompact:
		// Short key hints, no labels
		var parts []string
		if selected != nil {
			parts = append(parts, "↵", "f")
		}
		parts = append(parts, "v", "n", "?", "q")
		footer = " " + strings.Join(parts, " ")
	case layoutNarrow:
		// Abbreviated labels
		var parts []string
		if selected != nil {
			parts = append(parts, "↵:chat  f:focus")
		}
		var global []string
		if streamOpen {
			global = append(global, "v:close")
		} else {
			global = append(global, "v")
		}
		if defaultAgent != "" {
			global = append(global, "n")
		}
		global = append(global, "s", "?", "q")
		parts = append(parts, strings.Join(global, " "))
		footer = " " + strings.Join(parts, " · ")
	default:
		// Wide: full labels
		left := fmt.Sprintf(" %d agents", rowCount)

		var parts []string
		if selected != nil {
			parts = append(parts, "enter:chat  f:focus")
		}

		var global []string
		if streamOpen {
			global = append(global, "v:close")
		} else {
			global = append(global, "v:stream")
		}
		if defaultAgent != "" {
			global = append(global, "n:new")
		}
		global = append(global, "s:sort", "I:settings", "?:help", "q:quit")
		parts = append(parts, strings.Join(global, "  "))

		all := append([]string{left}, parts...)
		footer = strings.Join(all, "  \u00b7  ")
	}

	if width > 0 && lipgloss.Width(footer) > width && width > 2 {
		runes := []rune(footer)
		for lipgloss.Width(string(runes)) > width-1 {
			runes = runes[:len(runes)-1]
		}
		footer = string(runes) + "\u2026"
	}
	return footerStyle.Render(footer)
}
