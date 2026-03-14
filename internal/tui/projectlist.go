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

func renderColumnHeaders(nameWidth, typeWidth, dirWidth, totalWidth int, col sortColumn, desc bool, showEnv bool, envWidth int) string {
	name := fmt.Sprintf("  %-*s", nameWidth, "agent"+sortIndicator(sortByAgent, col, desc))
	harness := fmt.Sprintf("%-*s", typeWidth, "type"+sortIndicator(sortByHarness, col, desc))

	envCol := ""
	if showEnv {
		envCol = fmt.Sprintf("%-*s", envWidth, "env")
	}

	// status + updated fill the rest
	remaining := totalWidth - lipgloss.Width(name) - typeWidth - dirWidth - timeColWidth
	if showEnv {
		remaining -= envWidth
	}
	if remaining < 10 {
		remaining = 10
	}
	status := fmt.Sprintf("%-*s", remaining, "status"+sortIndicator(sortByStatus, col, desc))

	dir := fmt.Sprintf("%-*s", dirWidth, "directory"+sortIndicator(sortByDir, col, desc))
	updated := "updated" + sortIndicator(sortByUpdated, col, desc)

	line := name + harness + envCol + status + dir + updated
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

func renderProjectList(rows []projectRow, cursor int, width int, spinnerFrame int, attentionSessions map[string]time.Time, defaultAgent model.AgentType, availableAgents []model.AgentType, maxRows int, scrollOffset int, sortCol sortColumn, sortDesc bool, canSetup bool) string {
	var sb strings.Builder

	sb.WriteString(renderHeader(width))

	if len(rows) == 0 {
		sb.WriteString(renderEmptyState(defaultAgent, len(availableAgents) > 1, availableAgents, canSetup))
		return sb.String()
	}

	// Determine if env column should be shown
	showEnv := false
	for _, r := range rows {
		if r.session.Source != "" && r.session.Source != "pty" {
			showEnv = true
			break
		}
	}
	envWidth := 10 // fits "embedded" + padding

	// Compute column widths
	nameWidth := 20
	typeWidth := 10
	for _, r := range rows {
		if len(r.displayName) > nameWidth-2 {
			nameWidth = len(r.displayName) + 2
		}
	}
	if nameWidth > 30 {
		nameWidth = 30
	}

	// Status gets a fixed width; directory absorbs remaining space
	statusWidth := 22
	dirWidth := width - (nameWidth + 2) - typeWidth - statusWidth - timeColWidth
	if showEnv {
		dirWidth -= envWidth
	}
	if dirWidth < 8 {
		dirWidth = 8
	}
	if dirWidth > 50 {
		dirWidth = 50
	}

	sb.WriteString(renderColumnHeaders(nameWidth, typeWidth, dirWidth, width, sortCol, sortDesc, showEnv, envWidth))
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
			line := formatRow(r, nameWidth, typeWidth, dirWidth, width, spinnerFrame, true, showEnv, envWidth)
			sb.WriteString(attentionSelectedStyle.Render(padToWidth(line, width)))
		} else if isSelected {
			line := formatSelectedRow(r, nameWidth, typeWidth, dirWidth, width, spinnerFrame, showEnv, envWidth)
			sb.WriteString(line)
		} else if hasAttention {
			line := formatRow(r, nameWidth, typeWidth, dirWidth, width, spinnerFrame, true, showEnv, envWidth)
			sb.WriteString(attentionRowStyle.Render(padToWidth(line, width)))
		} else {
			line := formatRow(r, nameWidth, typeWidth, dirWidth, width, spinnerFrame, false, showEnv, envWidth)
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
	dir       string
	time      string
	remaining int
}

func computeRowColumns(r projectRow, nameWidth, typeWidth, dirWidth, totalWidth, spinnerFrame int, showEnv bool, envWidth int) rowColumns {
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

	dir := shortenPath(r.project.Dir)
	if len(dir) > dirWidth-2 {
		dir = dir[:dirWidth-3] + "\u2026"
	}
	dirCol := fmt.Sprintf("%-*s", dirWidth, dir)

	statusStr, style := formatStatus(r.session, spinnerFrame)

	timeStr := ""
	if !r.session.LastActivity.IsZero() {
		timeStr = relativeTime(r.session.LastActivity)
	}

	remaining := totalWidth - lipgloss.Width(nameStr) - typeWidth - dirWidth - timeColWidth
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
		dir:       dirCol,
		time:      timeStr,
		remaining: remaining,
	}
}

// formatRow builds a single-line row. When plain is true, no inner styles are
// applied so a row-level style can wrap the whole line cleanly.
func formatRow(r projectRow, nameWidth, typeWidth, dirWidth, totalWidth int, spinnerFrame int, plain bool, showEnv bool, envWidth int) string {
	c := computeRowColumns(r, nameWidth, typeWidth, dirWidth, totalWidth, spinnerFrame, showEnv, envWidth)
	agentCol := c.agent
	statusCol := lipgloss.NewStyle().Width(c.remaining).Render(c.status)
	if !plain {
		agentCol = c.agentSty.Render(c.agent)
		statusCol = c.statusSty.Width(c.remaining).Render(c.status)
	}
	return c.name + agentCol + c.env + statusCol + c.dir + c.time
}

// formatSelectedRow builds a selected row where the status retains its color
// on a purple background, while other columns get white text on purple.
func formatSelectedRow(r projectRow, nameWidth, typeWidth, dirWidth, totalWidth int, spinnerFrame int, showEnv bool, envWidth int) string {
	c := computeRowColumns(r, nameWidth, typeWidth, dirWidth, totalWidth, spinnerFrame, showEnv, envWidth)
	selEnv := ""
	if showEnv {
		selEnv = selectedTextStyle.Render(c.env)
	}
	selAgent := withSelectedBg(c.agentSty).Bold(true).Render(c.agent)
	selStatus := withSelectedBg(c.statusSty).Bold(true).Width(c.remaining).Render(c.status)
	return selectedTextStyle.Render(c.name) +
		selAgent +
		selEnv + selStatus +
		selectedTextStyle.Render(c.dir) +
		selectedTextStyle.Render(c.time)
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

func renderFooter(rowCount int, selected *projectRow, defaultAgent model.AgentType, streamOpen bool, width int) string {
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
