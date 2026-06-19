package iterm

import (
	"strings"
	"testing"

	pb "github.com/sethdeckard/atria/internal/terminal/iterm/proto"
	"google.golang.org/protobuf/proto"
)

func TestCellStyleSGR(t *testing.T) {
	// Bold + standard red fg + standard blue bg.
	st := &pb.CellStyle{
		FgColor: &pb.CellStyle_FgStandard{FgStandard: 1}, // red
		BgColor: &pb.CellStyle_BgStandard{BgStandard: 4}, // blue
		Bold:    proto.Bool(true),
	}
	if got := cellStyleSGR(st); got != "\x1b[1;31;44m" {
		t.Errorf("cellStyleSGR = %q, want %q", got, "\x1b[1;31;44m")
	}

	// RGB foreground.
	rgb := &pb.CellStyle{
		FgColor: &pb.CellStyle_FgRgb{FgRgb: &pb.RGBColor{
			Red: proto.Uint32(16), Green: proto.Uint32(32), Blue: proto.Uint32(48),
		}},
	}
	if got := cellStyleSGR(rgb); got != "\x1b[38;2;16;32;48m" {
		t.Errorf("cellStyleSGR(rgb) = %q, want %q", got, "\x1b[38;2;16;32;48m")
	}

	// No color, no attrs → empty.
	if got := cellStyleSGR(&pb.CellStyle{}); got != "" {
		t.Errorf("cellStyleSGR(empty) = %q, want empty", got)
	}
}

func TestStyledLine(t *testing.T) {
	// "RED" in red, then " ok" default.
	lc := &pb.LineContents{
		Text: proto.String("RED ok"),
		Style: []*pb.CellStyle{
			{FgColor: &pb.CellStyle_FgStandard{FgStandard: 1}, Repeats: proto.Uint32(3)},
			{Repeats: proto.Uint32(3)},
		},
	}
	got := styledLine(lc)
	if !strings.Contains(got, "\x1b[31m") {
		t.Errorf("styledLine missing red SGR: %q", got)
	}
	if !strings.Contains(got, "RED") || !strings.Contains(got, "ok") {
		t.Errorf("styledLine missing text: %q", got)
	}
	if !strings.Contains(got, sgrReset) {
		t.Errorf("styledLine missing reset: %q", got)
	}

	// No style info → plain text passthrough.
	plain := &pb.LineContents{Text: proto.String("hello")}
	if got := styledLine(plain); got != "hello" {
		t.Errorf("styledLine(no style) = %q, want %q", got, "hello")
	}
}

func TestStyledLineNulCellsBecomeSpaces(t *testing.T) {
	// iTerm2 encodes blank cells as NUL in the text field (one per cell). They
	// must become spaces, not be deleted, so word spacing and the rune↔cell
	// alignment survive. "ab" red, then NUL NUL (blank), then "cd" default.
	lc := &pb.LineContents{
		Text: proto.String("ab\x00\x00cd"),
		Style: []*pb.CellStyle{
			{FgColor: &pb.CellStyle_FgStandard{FgStandard: 1}, Repeats: proto.Uint32(2)},
			{Repeats: proto.Uint32(4)}, // two blanks + "cd", default style
		},
	}
	got := styledLine(lc)
	if strings.Contains(got, "\x00") {
		t.Errorf("styledLine left NUL bytes: %q", got)
	}
	// Stripping SGR must yield the spaced-out plain text.
	wantPlain := "ab  cd"
	if plain := stripSGRTest(got); plain != wantPlain {
		t.Errorf("styledLine plain = %q, want %q (full: %q)", plain, wantPlain, got)
	}
	if !strings.Contains(got, "\x1b[31m") {
		t.Errorf("styledLine dropped color: %q", got)
	}
}

// stripSGRTest removes SGR sequences for assertion purposes.
func stripSGRTest(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !(s[j] >= 0x40 && s[j] <= 0x7e) {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
