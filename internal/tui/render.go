package tui

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// sgrReset is the SGR sequence that clears all colors/attributes. It is
// appended after styled content so color never bleeds into box borders,
// padding, or following cells.
const sgrReset = "\x1b[0m"

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

// truncateToWidthANSI truncates s to fit within maxWidth visual columns while
// preserving any SGR escape sequences, appending "…" if truncation occurs.
// Unlike truncateToWidth it never splits an escape sequence mid-bytes.
func truncateToWidthANSI(s string, maxWidth int) string {
	if ansi.StringWidth(s) <= maxWidth {
		return s
	}
	return ansi.Truncate(s, maxWidth, "…")
}

// maxSanitizeSpaces caps the spaces produced by a single horizontal-motion
// escape, guarding against pathological parameters.
const maxSanitizeSpaces = 4096

// sanitizeBoxTextStyled normalizes terminal text for the framed dashboard UI
// like sanitizeBoxText, but preserves SGR color/style sequences (CSI … m) so
// content keeps its colors. Horizontal cursor-motion escapes (cursor-forward
// and column-set, which terminals like WezTerm/tmux emit instead of literal
// spaces when dumping a styled screen) are translated into spaces so word
// spacing survives. Every other escape (vertical motion, erase, OSC titles)
// and control char is still stripped, and \r\n/\r/\t are normalized — so
// nothing that can corrupt the box layout passes through.
func sanitizeBoxTextStyled(s string) string {
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	var b strings.Builder
	b.Grow(len(s))
	col := 0 // visible columns written on the current line (for column-set)

	writeSpaces := func(n int) {
		if n <= 0 {
			return
		}
		if n > maxSanitizeSpaces {
			n = maxSanitizeSpaces
		}
		b.WriteString(strings.Repeat(" ", n))
		col += n
	}

	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch ch {
		case '\x1b':
			next := skipANSISequence(s, i)
			if i+1 < len(s) && s[i+1] == '[' && next > i+2 {
				final := s[next-1]
				params := s[i+2 : next-1]
				switch final {
				case 'm': // SGR — keep color/style verbatim
					b.WriteString(s[i:next])
				case 'C', 'a': // cursor forward / horizontal position relative
					writeSpaces(csiParamN(params, 1))
				case 'G', '`': // cursor horizontal absolute (1-based column)
					if target := csiParamN(params, 1) - 1; target > col {
						writeSpaces(target - col)
					}
				}
				// any other final byte (vertical motion, erase, …) is dropped
			}
			if next > i {
				i = next - 1
			}
		case '\n':
			b.WriteByte('\n')
			col = 0
		case '\t':
			b.WriteString("    ")
			col += 4
		default:
			if ch < 0x80 {
				if ch < 0x20 || ch == 0x7f {
					continue
				}
				b.WriteByte(ch)
				col++
				continue
			}
			r, size := decodeRuneAt(s, i)
			if unicode.IsControl(r) {
				i += size - 1
				continue
			}
			b.WriteRune(r)
			col++
			i += size - 1
		}
	}

	return b.String()
}

// csiParamN parses the leading numeric parameter of a CSI sequence (the bytes
// between "[" and the final byte), returning def when absent or invalid.
func csiParamN(params string, def int) int {
	end := 0
	for end < len(params) && params[end] >= '0' && params[end] <= '9' {
		end++
	}
	if end == 0 {
		return def
	}
	n, err := strconv.Atoi(params[:end])
	if err != nil || n <= 0 {
		return def
	}
	return n
}

// isBlankLine reports whether a line is visually empty, ignoring any SGR
// escape sequences it may contain (so styled blank lines trim correctly).
func isBlankLine(s string) bool {
	return strings.TrimSpace(ansi.Strip(s)) == ""
}

func skipANSISequence(s string, i int) int {
	if i+1 >= len(s) {
		return i + 1
	}
	switch s[i+1] {
	case '[': // CSI: ESC [ ... final (0x40-0x7e)
		j := i + 2
		for j < len(s) {
			c := s[j]
			if c >= 0x40 && c <= 0x7e {
				return j + 1
			}
			j++
		}
		return len(s)
	case ']', 'P', 'X', '^', '_': // string sequences: OSC/DCS/SOS/PM/APC, ended by BEL or ST (ESC \)
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
		// nF escapes: optional intermediate bytes (0x20-0x2f) then a final
		// byte. Covers charset designations like ESC ( B / ESC ) 0 (whose final
		// byte would otherwise leak through), and 2-byte escapes like ESC c.
		j := i + 1
		for j < len(s) && s[j] >= 0x20 && s[j] <= 0x2f {
			j++
		}
		if j < len(s) {
			j++
		}
		return j
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
	return renderTitleBarWithSort(title, "", width, true)
}

// renderTitleBarWithSort renders a title bar with an optional sort label
// appended to the title (for narrow mode where column headers are hidden).
// When showBranding is false, the right-aligned "atria" text is suppressed.
func renderTitleBarWithSort(title, sortLabel string, width int, showBranding bool) string {
	var sb strings.Builder
	leftText := "  " + title
	if sortLabel != "" {
		// Only append if it fits
		candidate := leftText + " · " + sortLabel
		if lipgloss.Width(candidate) <= width-4 {
			leftText = candidate
		}
	}
	left := titleStyle.Render(leftText)

	if showBranding {
		right := brandingStyle.Render("atria  ")
		leftW := lipgloss.Width(left)
		rightW := lipgloss.Width(right)
		gap := width - leftW - rightW
		if gap < 0 {
			sb.WriteString(left)
		} else {
			sb.WriteString(left + strings.Repeat(" ", gap) + right)
		}
	} else {
		sb.WriteString(left)
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
