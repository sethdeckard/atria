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
	sortByDir
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
		case sortByDir:
			if rows[i].project.Dir != rows[j].project.Dir {
				return rows[i].project.Dir < rows[j].project.Dir
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

// headerLines returns the number of lines the header occupies (branding + separator + column headers + blank).
const headerLineCount = 3

// footerLineCount is lines for the footer (margin + content).
const footerLineCount = 3

func renderHeader(width int) string {
	var sb strings.Builder

	// Line 1: "Agents" left, "Atria" right
	left := titleStyle.Render("  agents")
	right := brandingStyle.Render("atria  ")
	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	gap := width - leftW - rightW
	if gap < 0 {
		// Too narrow for branding — show title only
		sb.WriteString(left)
	} else {
		sb.WriteString(left + strings.Repeat(" ", gap) + right)
	}
	sb.WriteString("\n")

	// Line 2: separator (account for 2-char indent)
	sepWidth := width - 2
	if sepWidth < 1 {
		sepWidth = 1
	}
	sb.WriteString(dimStyle.Render("  " + strings.Repeat("\u2500", sepWidth)))
	sb.WriteString("\n")

	return sb.String()
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

func renderColumnHeaders(nameWidth, typeWidth, dirWidth, totalWidth int, col sortColumn, desc bool) string {
	name := fmt.Sprintf("  %-*s", nameWidth, "agent"+sortIndicator(sortByAgent, col, desc))
	harness := fmt.Sprintf("%-*s", typeWidth, "type"+sortIndicator(sortByHarness, col, desc))
	dir := fmt.Sprintf("%-*s", dirWidth, "directory"+sortIndicator(sortByDir, col, desc))

	// status + updated fill the rest
	remaining := totalWidth - lipgloss.Width(name) - typeWidth - dirWidth - 10
	if remaining < 10 {
		remaining = 10
	}
	status := fmt.Sprintf("%-*s", remaining, "status"+sortIndicator(sortByStatus, col, desc))
	updated := "updated" + sortIndicator(sortByUpdated, col, desc)

	line := name + harness + dir + status + updated
	return dimStyle.Render(line)
}

func renderProjectList(rows []projectRow, cursor int, width int, spinnerFrame int, attentionSessions map[string]time.Time, defaultAgent model.AgentType, availableAgents []model.AgentType, maxRows int, scrollOffset int, sortCol sortColumn, sortDesc bool, canSetup bool) string {
	var sb strings.Builder

	sb.WriteString(renderHeader(width))

	if len(rows) == 0 {
		sb.WriteString(renderEmptyState(defaultAgent, len(availableAgents) > 1, availableAgents, canSetup))
		return sb.String()
	}

	// Compute column widths
	nameWidth := 20
	typeWidth := 10
	dirWidth := 20
	for _, r := range rows {
		if len(r.displayName) > nameWidth-2 {
			nameWidth = len(r.displayName) + 2
		}
		dp := shortenPath(r.project.Dir)
		if len(dp)+2 > dirWidth {
			dirWidth = len(dp) + 2
		}
	}
	if nameWidth > 30 {
		nameWidth = 30
	}
	if dirWidth > 40 {
		dirWidth = 40
	}

	sb.WriteString(renderColumnHeaders(nameWidth, typeWidth, dirWidth, width, sortCol, sortDesc))
	sb.WriteString("\n")

	// Clamp scroll to keep cursor visible regardless of how maxRows changed
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

	for i, r := range visibleRows {
		actualIdx := i + scrollOffset
		_, hasAttention := attentionSessions[r.session.SessionID]
		isSelected := actualIdx == cursor

		if isSelected && hasAttention {
			line := formatRow(r, nameWidth, typeWidth, dirWidth, width, spinnerFrame, true)
			sb.WriteString(attentionSelectedStyle.Render(padToWidth(line, width)))
		} else if isSelected {
			line := formatSelectedRow(r, nameWidth, typeWidth, dirWidth, width, spinnerFrame)
			sb.WriteString(line)
		} else if hasAttention {
			line := formatRow(r, nameWidth, typeWidth, dirWidth, width, spinnerFrame, true)
			sb.WriteString(attentionRowStyle.Render(padToWidth(line, width)))
		} else {
			line := formatRow(r, nameWidth, typeWidth, dirWidth, width, spinnerFrame, false)
			sb.WriteString(line)
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// padToWidth pads a string with spaces to reach the target visual width.
func padToWidth(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

func agentTypeLabel(t model.AgentType) string {
	switch t {
	case model.AgentClaude:
		return "Claude"
	case model.AgentCodex:
		return "Codex"
	case model.AgentOpenCode:
		return "OpenCode"
	default:
		s := string(t)
		return strings.ToUpper(s[:1]) + s[1:]
	}
}

// formatRow builds a single-line row. When plain is true, no inner styles are
// applied so a row-level style can wrap the whole line cleanly.
func formatRow(r projectRow, nameWidth, typeWidth, dirWidth, totalWidth int, spinnerFrame int, plain bool) string {
	name := r.displayName
	if len(name) > nameWidth-2 {
		name = name[:nameWidth-3] + "\u2026"
	}
	name = fmt.Sprintf("  %-*s", nameWidth, name)

	agentStr := agentTypeLabel(r.session.Type)
	agentCol := fmt.Sprintf("%-*s", typeWidth, agentStr)

	dir := shortenPath(r.project.Dir)
	if len(dir) > dirWidth-2 {
		dir = dir[:dirWidth-3] + "\u2026"
	}
	dirCol := fmt.Sprintf("%-*s", dirWidth, dir)

	statusStr, style := formatStatus(r.session, spinnerFrame)

	timeStr := ""
	timeVisual := ""
	if !r.session.LastActivity.IsZero() {
		timeVisual = relativeTime(r.session.LastActivity)
		timeStr = timeVisual
	}

	// Build the line
	remaining := totalWidth - lipgloss.Width(name) - typeWidth - dirWidth - len(timeVisual) - 4
	if remaining < 2 {
		remaining = 2
	}
	if lipgloss.Width(statusStr) > remaining {
		statusStr = statusStr[:remaining-1] + "\u2026"
	}

	statusCol := statusStr
	if !plain {
		statusCol = style.Render(statusStr)
	}

	pad := remaining - lipgloss.Width(statusStr)
	if pad < 0 {
		pad = 0
	}
	return name + agentCol + dirCol + statusCol + strings.Repeat(" ", pad) + timeStr
}

// formatSelectedRow builds a selected row where the status retains its color
// on a purple background, while other columns get white text on purple.
func formatSelectedRow(r projectRow, nameWidth, typeWidth, dirWidth, totalWidth int, spinnerFrame int) string {
	name := r.displayName
	if len(name) > nameWidth-2 {
		name = name[:nameWidth-3] + "\u2026"
	}
	nameStr := fmt.Sprintf("  %-*s", nameWidth, name)

	agentStr := agentTypeLabel(r.session.Type)
	agentCol := fmt.Sprintf("%-*s", typeWidth, agentStr)

	dir := shortenPath(r.project.Dir)
	if len(dir) > dirWidth-2 {
		dir = dir[:dirWidth-3] + "\u2026"
	}
	dirCol := fmt.Sprintf("%-*s", dirWidth, dir)

	statusStr, style := formatStatus(r.session, spinnerFrame)

	timeVisual := ""
	if !r.session.LastActivity.IsZero() {
		timeVisual = relativeTime(r.session.LastActivity)
	}

	remaining := totalWidth - lipgloss.Width(nameStr) - typeWidth - dirWidth - len(timeVisual) - 4
	if remaining < 2 {
		remaining = 2
	}
	if lipgloss.Width(statusStr) > remaining {
		statusStr = statusStr[:remaining-1] + "\u2026"
	}

	pad := remaining - lipgloss.Width(statusStr)
	if pad < 0 {
		pad = 0
	}

	// Render each column with selected background
	selName := selectedTextStyle.Render(nameStr)
	selAgent := selectedTextStyle.Render(agentCol)
	selDir := selectedTextStyle.Render(dirCol)
	selStatus := withSelectedBg(style).Bold(true).Render(statusStr)
	selPad := selectedTextStyle.Render(strings.Repeat(" ", pad))
	selTime := selectedTextStyle.Render(timeVisual)

	return selName + selAgent + selDir + selStatus + selPad + selTime
}

func formatStatus(s *model.AgentSession, spinnerFrame int) (string, lipgloss.Style) {
	switch s.Status {
	case model.StatusNeedsInput:
		text := "\u26a0 " + s.Attention
		if text == "\u26a0 " {
			text = "\u26a0 Needs input"
		}
		return text, statusNeedsInputStyle
	case model.StatusWorking:
		spin := spinnerFrames[spinnerFrame%len(spinnerFrames)]
		text := spin + " "
		if s.Activity != "" {
			text += s.Activity
		} else {
			text += "Working..."
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
		return spin + " Working...", statusWorkingStyle
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

func renderEmptyState(defaultAgent model.AgentType, canToggle bool, availableAgents []model.AgentType, canSetup bool) string {
	var sb strings.Builder

	logo := logoStyle.Render(Logo)

	sb.WriteString(logo)
	sb.WriteString("\n\n")
	sb.WriteString(emptyHintStyle.Render("  Agent multiplexer for your terminal."))
	sb.WriteString("\n\n")
	if canSetup {
		sb.WriteString("  " + selectedTextStyle.Render("S") + emptyHintStyle.Render("  ") + titleStyle.Render("run setup") + emptyHintStyle.Render(" \u2014 configure integrations and watch directories"))
		sb.WriteString("\n\n")
	}
	sb.WriteString("  " + emptyKeyStyle.Render("l") + emptyHintStyle.Render("  launch an agent in a directory"))
	sb.WriteString("\n")
	if canToggle {
		var names []string
		for _, a := range availableAgents {
			names = append(names, agentDisplayName(a))
		}
		sb.WriteString("  " + emptyKeyStyle.Render("t") + emptyHintStyle.Render(fmt.Sprintf("  toggle agent type (%s)", strings.Join(names, "/"))))
		sb.WriteString("\n")
	}
	sb.WriteString("  " + emptyKeyStyle.Render("?") + emptyHintStyle.Render("  show all key bindings"))
	sb.WriteString("\n")
	sb.WriteString("  " + emptyKeyStyle.Render("q") + emptyHintStyle.Render("  quit"))
	sb.WriteString("\n")

	return sb.String()
}

func renderFooter(rowCount int, selected *projectRow, defaultAgent model.AgentType, canToggle bool, streamOpen bool, width int) string {
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
		global = append(global, fmt.Sprintf("l:launch (%s)", agentDisplayName(defaultAgent)))
	}
	if canToggle {
		global = append(global, "t:toggle")
	}
	global = append(global, "s:sort", "I:settings", "?:help", "q:quit")
	parts = append(parts, strings.Join(global, "  "))

	all := append([]string{left}, parts...)
	footer := strings.Join(all, "  \u00b7  ")
	if width > 0 && lipgloss.Width(footer) > width && width > 2 {
		runes := []rune(footer)
		for lipgloss.Width(string(runes)) > width-1 {
			runes = runes[:len(runes)-1]
		}
		footer = string(runes) + "\u2026"
	}
	return footerStyle.Render(footer)
}
