package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/sethdeckard/atria/internal/model"
)

type projectRow struct {
	project *model.Project
	session *model.AgentSession
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
			rows = append(rows, projectRow{project: p, session: s})
		}
	}
	sortRows(rows)
	return rows
}

func sortRows(rows []projectRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		si := statusPriority(rows[i].session)
		sj := statusPriority(rows[j].session)
		if si != sj {
			return si < sj
		}
		return rows[i].project.Name < rows[j].project.Name
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

func renderProjectList(rows []projectRow, cursor int, allProjects []*model.Project, width int, spinnerFrame int, attentionSessions map[string]time.Time, defaultAgent model.AgentType, canToggle bool) string {
	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Agents"))
	sb.WriteString("\n\n")

	if len(rows) == 0 {
		sb.WriteString(renderEmptyState(defaultAgent, canToggle))
		return sb.String()
	}

	nameWidth := 20
	typeWidth := 10
	for _, r := range rows {
		dn := r.project.DisplayName(allProjects)
		if len(dn) > nameWidth-2 {
			nameWidth = len(dn) + 2
		}
	}
	if nameWidth > 30 {
		nameWidth = 30
	}

	for i, r := range rows {
		pathLine := formatPathLine(r.project.Dir, nameWidth)
		_, hasAttention := attentionSessions[r.session.SessionID]
		if i == cursor {
			style := selectedStyle
			if hasAttention {
				style = attentionSelectedStyle
			}
			line := formatRow(r, allProjects, nameWidth, typeWidth, width, spinnerFrame, true)
			sb.WriteString(style.Render(padToWidth(line, width)))
			sb.WriteString("\n")
			sb.WriteString(style.Render(padToWidth(pathLine, width)))
		} else if hasAttention {
			line := formatRow(r, allProjects, nameWidth, typeWidth, width, spinnerFrame, true)
			sb.WriteString(attentionRowStyle.Render(padToWidth(line, width)))
			sb.WriteString("\n")
			sb.WriteString(attentionRowStyle.Render(padToWidth(pathLine, width)))
		} else {
			line := formatRow(r, allProjects, nameWidth, typeWidth, width, spinnerFrame, false)
			sb.WriteString(line)
			sb.WriteString("\n")
			sb.WriteString(pathStyle.Render(pathLine))
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

func formatPathLine(dir string, nameWidth int) string {
	displayPath := contractHome(dir)
	gitInfo := detectGitWorktree(dir)
	line := displayPath
	if gitInfo.IsWorktree {
		ann := "worktree"
		if gitInfo.ParentRepo != "" {
			ann = "worktree of " + gitInfo.ParentRepo
		}
		if gitInfo.Branch != "" {
			ann += " \u00b7 " + gitInfo.Branch
		}
		line += "  " + ann
	}
	return "     " + line
}

// formatRow builds a row line. When plain is true, no inner styles are
// applied so a row-level style can wrap the whole line cleanly.
func formatRow(r projectRow, allProjects []*model.Project, nameWidth, typeWidth, totalWidth int, spinnerFrame int, plain bool) string {
	name := r.project.DisplayName(allProjects)
	if len(name) > nameWidth-2 {
		name = name[:nameWidth-3] + "\u2026"
	}
	name = fmt.Sprintf("  %-*s", nameWidth, name)

	agentStr := string(r.session.Type)
	agentStr = strings.ToUpper(agentStr[:1]) + agentStr[1:]
	agentCol := fmt.Sprintf("%-*s", typeWidth, agentStr)

	statusStr, style := formatStatus(r.session, spinnerFrame)

	timeStr := ""
	timeVisual := ""
	if !r.session.LastActivity.IsZero() {
		timeVisual = relativeTime(r.session.LastActivity)
		if plain {
			timeStr = timeVisual
		} else {
			timeStr = dimStyle.Render(timeVisual)
		}
	}

	// Build the line
	remaining := totalWidth - lipgloss.Width(name) - typeWidth - len(timeVisual) - 4
	if remaining < 0 {
		remaining = 20
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
	return name + agentCol + statusCol + strings.Repeat(" ", pad) + timeStr
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
		return "just now"
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

func renderEmptyState(defaultAgent model.AgentType, canToggle bool) string {
	var sb strings.Builder

	logo := logoStyle.Render(`         _        _
   __ _ | |_  _ _(_) __ _
  / _` + "`" + ` ||  _|| '_| |/ _` + "`" + ` |
  \__,_| \__||_| |_|\__,_|`)

	sb.WriteString(logo)
	sb.WriteString("\n\n")
	sb.WriteString(emptyHintStyle.Render("  Agent orchestration for your terminal."))
	sb.WriteString("\n\n")
	sb.WriteString("  " + emptyKeyStyle.Render("l") + emptyHintStyle.Render("  Launch an agent in a project"))
	sb.WriteString("\n")
	if canToggle {
		sb.WriteString("  " + emptyKeyStyle.Render("t") + emptyHintStyle.Render("  Toggle agent type (Claude/Codex)"))
		sb.WriteString("\n")
	}
	sb.WriteString("  " + emptyKeyStyle.Render("a") + emptyHintStyle.Render("  Add a project from your watch directories"))
	sb.WriteString("\n")
	sb.WriteString("  " + emptyKeyStyle.Render("?") + emptyHintStyle.Render("  Show all key bindings"))
	sb.WriteString("\n")
	sb.WriteString("  " + emptyKeyStyle.Render("q") + emptyHintStyle.Render("  Quit"))
	sb.WriteString("\n")

	return sb.String()
}

func renderFooter(rowCount int, selected *projectRow, defaultAgent model.AgentType, canToggle bool) string {
	left := fmt.Sprintf(" %d agents", rowCount)

	var parts []string
	if selected != nil {
		parts = append(parts, "enter:send  f:focus")
	}

	var global []string
	if defaultAgent != "" {
		agentName := strings.ToUpper(string(defaultAgent)[:1]) + string(defaultAgent)[1:]
		global = append(global, fmt.Sprintf("l:launch (%s)", agentName))
	}
	if canToggle {
		global = append(global, "t:toggle")
	}
	global = append(global, "a:add", "?:help", "q:quit")
	parts = append(parts, strings.Join(global, "  "))

	all := append([]string{left}, parts...)
	return footerStyle.Render(strings.Join(all, "  \u00b7  "))
}
