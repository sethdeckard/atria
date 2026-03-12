package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// truncateToWidth truncates s to fit within maxWidth visual columns,
// appending "…" if truncation occurs. Uses lipgloss.Width for accurate
// measurement of multi-byte and wide characters.
func truncateToWidth(s string, maxWidth int) string {
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	truncated := ""
	for _, r := range s {
		if lipgloss.Width(truncated+string(r)) > maxWidth-1 {
			return truncated + "\u2026"
		}
		truncated += string(r)
	}
	return truncated
}

// renderTitleBar renders a title bar with left-aligned title and right-aligned
// "atria" branding, followed by a separator line.
func renderTitleBar(title string, width int) string {
	var sb strings.Builder
	left := titleStyle.Render("  " + title)
	right := brandingStyle.Render("atria  ")
	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	gap := width - leftW - rightW
	if gap < 0 {
		sb.WriteString(left)
	} else {
		sb.WriteString(left + strings.Repeat(" ", gap) + right)
	}
	sb.WriteString("\n")
	sepWidth := width - 2
	if sepWidth < 1 {
		sepWidth = 1
	}
	sb.WriteString(dimStyle.Render("  " + strings.Repeat("\u2500", sepWidth)))
	sb.WriteString("\n")
	return sb.String()
}
