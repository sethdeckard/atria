package tui

import (
	"strings"
	"unicode"

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

// sanitizeBoxText normalizes terminal text before rendering it inside
// framed dashboard UI. This strips escape/control sequences that can
// corrupt layout while preserving plain text content.
func sanitizeBoxText(s string) string {
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	var b strings.Builder
	b.Grow(len(s))

	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch ch {
		case '\x1b':
			if next := skipANSISequence(s, i); next > i {
				i = next - 1
			}
		case '\n':
			b.WriteByte('\n')
		case '\t':
			b.WriteString("    ")
		default:
			if ch < 0x80 {
				if ch < 0x20 || ch == 0x7f {
					continue
				}
				b.WriteByte(ch)
				continue
			}
			r, size := decodeRuneAt(s, i)
			if unicode.IsControl(r) {
				i += size - 1
				continue
			}
			b.WriteRune(r)
			i += size - 1
		}
	}

	return b.String()
}

func skipANSISequence(s string, i int) int {
	if i+1 >= len(s) {
		return i + 1
	}
	switch s[i+1] {
	case '[':
		j := i + 2
		for j < len(s) {
			c := s[j]
			if c >= 0x40 && c <= 0x7e {
				return j + 1
			}
			j++
		}
		return len(s)
	case ']':
		j := i + 2
		for j < len(s) {
			switch s[j] {
			case '\x07':
				return j + 1
			case '\x1b':
				if j+1 < len(s) && s[j+1] == '\\' {
					return j + 2
				}
			}
			j++
		}
		return len(s)
	default:
		return i + 2
	}
}

func decodeRuneAt(s string, i int) (rune, int) {
	for _, r := range s[i:] {
		return r, len(string(r))
	}
	return rune(s[i]), 1
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
