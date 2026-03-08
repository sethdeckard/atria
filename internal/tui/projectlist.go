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

func renderProjectList(rows []projectRow, cursor int, allProjects []*model.Project, width int, spinnerFrame int, attentionDirs map[string]time.Time) string {
	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Projects"))
	sb.WriteString("\n\n")

	if len(rows) == 0 {
		sb.WriteString(renderEmptyState())
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
		line := formatRow(r, allProjects, nameWidth, typeWidth, width, spinnerFrame)
		_, hasAttention := attentionDirs[r.project.Dir]
		if i == cursor {
			if hasAttention {
				sb.WriteString(attentionSelectedStyle.Render(line))
			} else {
				sb.WriteString(selectedStyle.Render(line))
			}
		} else if hasAttention {
			sb.WriteString(attentionRowStyle.Render(line))
		} else {
			sb.WriteString(line)
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func formatRow(r projectRow, allProjects []*model.Project, nameWidth, typeWidth, totalWidth int, spinnerFrame int) string {
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

	statusStr, style := formatStatus(r.session, spinnerFrame)
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
		return "\u25cf idle", statusIdleStyle
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

func renderEmptyState() string {
	var sb strings.Builder

	logo := logoStyle.Render(`         _        _
   __ _ | |_  _ _(_) __ _
  / _` + "`" + ` ||  _|| '_| |/ _` + "`" + ` |
  \__,_| \__||_| |_|\__,_|`)

	sb.WriteString(logo)
	sb.WriteString("\n\n")
	sb.WriteString(emptyHintStyle.Render("  Agent orchestration for your terminal."))
	sb.WriteString("\n\n")
	sb.WriteString("  " + emptyKeyStyle.Render("a") + emptyHintStyle.Render("  Add a project from your watch directories"))
	sb.WriteString("\n")
	sb.WriteString("  " + emptyKeyStyle.Render("?") + emptyHintStyle.Render("  Show all key bindings"))
	sb.WriteString("\n")
	sb.WriteString("  " + emptyKeyStyle.Render("q") + emptyHintStyle.Render("  Quit"))
	sb.WriteString("\n")

	return sb.String()
}

func renderFooter(rowCount, activeCount int, selected *projectRow) string {
	left := fmt.Sprintf(" %d projects  %d active", rowCount, activeCount)

	var hints []string
	if selected != nil {
		if selected.session != nil {
			hints = append(hints, "enter send", "f focus")
		} else {
			hints = append(hints, "c claude", "x codex")
		}
	}
	hints = append(hints, "a add", "? help", "q quit")

	right := strings.Join(hints, "  ")
	return footerStyle.Render(left + "    " + right)
}
