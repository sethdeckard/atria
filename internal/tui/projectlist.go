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
	GetSession(string) *model.AgentSession
}) []projectRow {
	projects := store.Projects()
	rows := make([]projectRow, len(projects))
	for i, p := range projects {
		rows[i] = projectRow{project: p, session: store.GetSession(p.Dir)}
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

func renderProjectList(rows []projectRow, cursor int, allProjects []*model.Project, width int) string {
	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Projects"))
	sb.WriteString("\n\n")

	if len(rows) == 0 {
		sb.WriteString(dimStyle.Render("  No projects yet. Press 'a' to add one."))
		sb.WriteString("\n")
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
		line := formatRow(r, allProjects, nameWidth, typeWidth, width)
		if i == cursor {
			sb.WriteString(selectedStyle.Render(line))
		} else {
			sb.WriteString(line)
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func formatRow(r projectRow, allProjects []*model.Project, nameWidth, typeWidth, totalWidth int) string {
	name := r.project.DisplayName(allProjects)
	if len(name) > nameWidth-2 {
		name = name[:nameWidth-3] + "\u2026"
	}
	name = fmt.Sprintf("  %-*s", nameWidth, name)

	if r.session == nil {
		dash := dimStyle.Render(fmt.Sprintf("%-*s", typeWidth, "\u2014"))
		return name + dash
	}

	agentStr := string(r.session.Type)
	agentStr = strings.ToUpper(agentStr[:1]) + agentStr[1:]
	agentCol := fmt.Sprintf("%-*s", typeWidth, agentStr)

	statusStr, style := formatStatus(r.session)
	statusCol := style.Render(statusStr)

	timeStr := ""
	if !r.session.LastActivity.IsZero() {
		timeStr = dimStyle.Render(relativeTime(r.session.LastActivity))
	}

	// Build the line
	remaining := totalWidth - lipgloss.Width(name) - typeWidth - lipgloss.Width(timeStr) - 4
	if remaining < 0 {
		remaining = 20
	}
	if lipgloss.Width(statusStr) > remaining {
		statusStr = statusStr[:remaining-1] + "\u2026"
		statusCol = style.Render(statusStr)
	}

	padded := fmt.Sprintf("%-*s", remaining, statusCol)
	return name + agentCol + padded + timeStr
}

func formatStatus(s *model.AgentSession) (string, lipgloss.Style) {
	switch s.Status {
	case model.StatusNeedsInput:
		text := "\u26a0 " + s.Attention
		if text == "\u26a0 " {
			text = "\u26a0 Needs input"
		}
		return text, statusNeedsInputStyle
	case model.StatusWorking:
		text := "\u25cf "
		if s.Activity != "" {
			text += s.Activity
		} else {
			text += "Working..."
		}
		return text, statusWorkingStyle
	case model.StatusIdle:
		return "\u25cb idle", statusIdleStyle
	case model.StatusError:
		text := "\u2717 error"
		if s.Attention != "" {
			text = "\u2717 " + s.Attention
		}
		return text, statusErrorStyle
	default:
		return "\u25cf Working...", statusWorkingStyle
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

func renderFooter(rowCount, activeCount int) string {
	left := fmt.Sprintf(" %d projects  %d active", rowCount, activeCount)
	right := "? help  q quit"
	return footerStyle.Render(left + "    " + right)
}
