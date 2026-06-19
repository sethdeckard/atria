package pty

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/hinshun/vt10x"
	"github.com/sethdeckard/atria/internal/terminal"
)

// Compile-time check that the PTY backend supports styled reads.
var _ terminal.StyledReader = (*Client)(nil)

// vt10x Glyph.Mode attribute bits (mirrored from vt10x state.go, which keeps
// them unexported). Used to translate cell attributes back into SGR codes.
const (
	attrReverse   int16 = 1 << 0
	attrUnderline int16 = 1 << 1
	attrBold      int16 = 1 << 2
	attrGfx       int16 = 1 << 3
	attrItalic    int16 = 1 << 4
	attrBlink     int16 = 1 << 5
)

// defaultColorBase is vt10x.DefaultFG (1<<24); any Color at or above it is a
// default color (FG/BG/cursor) rather than a palette index or packed RGB.
const defaultColorBase = vt10x.Color(1 << 24)

// readScreenStyled returns the last N lines of the vt10x screen as text with
// SGR color/style escapes preserved. Display only — it does not touch bell
// state (the plain readScreen path owns that).
func (s *session) readScreenStyled(lines int) string {
	content := s.styledString()

	allLines := strings.Split(content, "\n")
	// Drop the trailing empty line produced by the final row's newline.
	if len(allLines) > 0 && allLines[len(allLines)-1] == "" {
		allLines = allLines[:len(allLines)-1]
	}
	return terminal.TrimScreenTail(strings.Join(allLines, "\n"), lines)
}

// styledString renders the full vt10x screen buffer to a string with SGR
// escapes. Mirrors vt10x's State.String() cell walk but emits color/style.
// Holds s.termMu; callers must not hold either mutex.
func (s *session) styledString() string {
	s.termMu.Lock()
	defer s.termMu.Unlock()

	cols, rows := s.term.Size()
	var b strings.Builder
	for y := 0; y < rows; y++ {
		prev := "" // currently applied SGR ("" means default/reset state)
		for x := 0; x < cols; x++ {
			g := s.term.Cell(x, y)
			style := glyphSGR(g)
			if style != prev {
				switch {
				case prev == "":
					b.WriteString(style)
				case style == "":
					b.WriteString(sgrResetSeq)
				default:
					b.WriteString(sgrResetSeq)
					b.WriteString(style)
				}
				prev = style
			}
			ch := g.Char
			if ch == 0 {
				ch = ' '
			}
			b.WriteRune(ch)
		}
		if prev != "" {
			b.WriteString(sgrResetSeq)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

const sgrResetSeq = "\x1b[0m"

// glyphSGR returns the SGR sequence that renders g's style starting from a
// reset state, or "" when g has default colors and no attributes.
func glyphSGR(g vt10x.Glyph) string {
	var codes []string
	if g.Mode&attrBold != 0 {
		codes = append(codes, "1")
	}
	if g.Mode&attrItalic != 0 {
		codes = append(codes, "3")
	}
	if g.Mode&attrUnderline != 0 {
		codes = append(codes, "4")
	}
	if g.Mode&attrBlink != 0 {
		codes = append(codes, "5")
	}
	if g.Mode&attrReverse != 0 {
		codes = append(codes, "7")
	}
	codes = append(codes, colorSGR(g.FG, true)...)
	codes = append(codes, colorSGR(g.BG, false)...)
	if len(codes) == 0 {
		return ""
	}
	return "\x1b[" + strings.Join(codes, ";") + "m"
}

// colorSGR translates a vt10x.Color into SGR parameter(s). Returns nil for
// default colors. fg selects foreground (true) vs background (false) codes.
func colorSGR(c vt10x.Color, fg bool) []string {
	if c >= defaultColorBase {
		return nil // DefaultFG/DefaultBG/DefaultCursor
	}
	switch {
	case c < 8:
		base := 30
		if !fg {
			base = 40
		}
		return []string{strconv.Itoa(base + int(c))}
	case c < 16:
		base := 90
		if !fg {
			base = 100
		}
		return []string{strconv.Itoa(base + int(c-8))}
	case c < 256:
		sel := "38;5;"
		if !fg {
			sel = "48;5;"
		}
		return []string{sel + strconv.Itoa(int(c))}
	default:
		// vt10x packs truecolor as r<<16 | g<<8 | b in [256, 1<<24).
		r := (c >> 16) & 0xff
		g := (c >> 8) & 0xff
		b := c & 0xff
		sel := "38;2"
		if !fg {
			sel = "48;2"
		}
		return []string{fmt.Sprintf("%s;%d;%d;%d", sel, r, g, b)}
	}
}
