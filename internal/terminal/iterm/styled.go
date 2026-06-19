package iterm

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/sethdeckard/atria/internal/terminal"
	pb "github.com/sethdeckard/atria/internal/terminal/iterm/proto"
	"google.golang.org/protobuf/proto"
)

const sgrReset = "\x1b[0m"

// Compile-time check that the iTerm2 backend supports styled reads.
var _ terminal.StyledReader = (*Client)(nil)

// ReadScreenStyled captures the visible screen contents of a session with SGR
// color/style escapes reconstructed from iTerm2's per-cell style runs.
func (c *Client) ReadScreenStyled(sessionID string, lines int) (string, error) {
	content, err := c.getBufferStyled(sessionID, &pb.LineRange{
		ScreenContentsOnly: proto.Bool(true),
	}, lines)
	if err != nil {
		return "", err
	}
	if !isSemanticallyBlank(content) {
		return content, nil
	}

	lineInfoJSON, err := c.getProperty(sessionID, "number_of_lines")
	if err != nil {
		return content, nil
	}
	lineRange, err := lineRangeForVisibleScreen(lineInfoJSON)
	if err != nil {
		return content, nil
	}
	visibleContent, err := c.getBufferStyled(sessionID, lineRange, lines)
	if err != nil {
		return content, nil
	}
	return visibleContent, nil
}

func (c *Client) getBufferStyled(sessionID string, lr *pb.LineRange, lines int) (string, error) {
	gbr, err := c.getBufferResponse(sessionID, lr)
	if err != nil {
		return "", err
	}
	return joinBufferLinesStyled(gbr.GetContents(), lines), nil
}

// joinBufferLinesStyled mirrors joinBufferLines but renders each line with SGR
// escapes. Trailing-blank trimming uses the plain text so it matches the plain
// path exactly.
func joinBufferLinesStyled(contents []*pb.LineContents, lines int) string {
	styled := make([]string, len(contents))
	for i, lc := range contents {
		styled[i] = styledLine(lc)
	}
	if len(styled) > lines && lines > 0 {
		end := len(styled)
		for i := len(contents) - 1; i >= 0; i-- {
			if !isSemanticallyBlank(contents[i].GetText()) {
				end = i + 1
				break
			}
		}
		start := end - lines
		if start < 0 {
			start = 0
		}
		styled = styled[start:end]
	}
	return strings.Join(styled, "\n")
}

// styledLine renders a single LineContents to text with SGR escapes by walking
// its style runs (each CellStyle covers `repeats` cells) alongside the line's
// runes, using code_points_per_cell to map runes to cells. Falls back to plain
// text when no style information is present.
//
// iTerm2 represents blank cells as NUL (\x00) in the text field, one per cell.
// They must be replaced with a single space (not deleted) so spacing is kept
// and the rune↔cell alignment used by the style walk stays correct — the plain
// ReadScreen path does the same NUL→space substitution downstream.
func styledLine(lc *pb.LineContents) string {
	text := strings.ReplaceAll(lc.GetText(), "\x00", " ")
	styles := lc.GetStyle()
	if len(styles) == 0 {
		return text
	}
	runes := []rune(text)

	// Flatten code_points_per_cell into a per-cell rune count.
	var cellCounts []int
	for _, cp := range lc.GetCodePointsPerCell() {
		n := int(cp.GetNumCodePoints())
		if n <= 0 {
			n = 1
		}
		r := int(cp.GetRepeats())
		if r <= 0 {
			r = 1
		}
		for i := 0; i < r; i++ {
			cellCounts = append(cellCounts, n)
		}
	}

	var b strings.Builder
	prev := ""
	ri := 0   // rune index
	cell := 0 // cell index
	for _, st := range styles {
		reps := int(st.GetRepeats())
		if reps <= 0 {
			reps = 1
		}
		sgr := cellStyleSGR(st)
		for k := 0; k < reps; k++ {
			if sgr != prev {
				switch {
				case prev == "":
					b.WriteString(sgr)
				case sgr == "":
					b.WriteString(sgrReset)
				default:
					b.WriteString(sgrReset)
					b.WriteString(sgr)
				}
				prev = sgr
			}
			n := 1
			if cell < len(cellCounts) {
				n = cellCounts[cell]
			}
			for j := 0; j < n && ri < len(runes); j++ {
				b.WriteRune(runes[ri])
				ri++
			}
			cell++
		}
	}
	if prev != "" {
		b.WriteString(sgrReset)
	}
	// Append any runes the style runs didn't cover, unstyled.
	for ri < len(runes) {
		b.WriteRune(runes[ri])
		ri++
	}
	return b.String()
}

// cellStyleSGR returns the SGR sequence for a CellStyle from a reset state, or
// "" when the cell has default colors and no attributes.
func cellStyleSGR(st *pb.CellStyle) string {
	var codes []string
	if st.GetBold() {
		codes = append(codes, "1")
	}
	if st.GetFaint() {
		codes = append(codes, "2")
	}
	if st.GetItalic() {
		codes = append(codes, "3")
	}
	if st.GetUnderline() {
		codes = append(codes, "4")
	}
	if st.GetBlink() {
		codes = append(codes, "5")
	}
	if st.GetInverse() {
		codes = append(codes, "7")
	}
	if st.GetStrikethrough() {
		codes = append(codes, "9")
	}
	codes = append(codes, fgColorSGR(st)...)
	codes = append(codes, bgColorSGR(st)...)
	if len(codes) == 0 {
		return ""
	}
	return "\x1b[" + strings.Join(codes, ";") + "m"
}

func fgColorSGR(st *pb.CellStyle) []string {
	switch fg := st.GetFgColor().(type) {
	case *pb.CellStyle_FgStandard:
		return standardColorSGR(fg.FgStandard, true)
	case *pb.CellStyle_FgRgb:
		return rgbColorSGR(fg.FgRgb, true)
	default:
		return nil // alternate/default/image placement → leave default
	}
}

func bgColorSGR(st *pb.CellStyle) []string {
	switch bg := st.GetBgColor().(type) {
	case *pb.CellStyle_BgStandard:
		return standardColorSGR(bg.BgStandard, false)
	case *pb.CellStyle_BgRgb:
		return rgbColorSGR(bg.BgRgb, false)
	default:
		return nil
	}
}

// standardColorSGR maps an iTerm2 standard color index (0-255 palette) to SGR.
func standardColorSGR(c uint32, fg bool) []string {
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
		return nil
	}
}

func rgbColorSGR(c *pb.RGBColor, fg bool) []string {
	sel := "38;2"
	if !fg {
		sel = "48;2"
	}
	return []string{fmt.Sprintf("%s;%d;%d;%d", sel, c.GetRed(), c.GetGreen(), c.GetBlue())}
}
