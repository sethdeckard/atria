package pty

import (
	"strings"
	"testing"

	"github.com/hinshun/vt10x"
)

func TestColorSGR(t *testing.T) {
	tests := []struct {
		name string
		c    vt10x.Color
		fg   bool
		want []string
	}{
		{"default fg", vt10x.DefaultFG, true, nil},
		{"default bg", vt10x.DefaultBG, false, nil},
		{"standard red fg", vt10x.Red, true, []string{"31"}},
		{"standard red bg", vt10x.Red, false, []string{"41"}},
		{"bright color fg", vt10x.Color(9), true, []string{"91"}},
		{"bright color bg", vt10x.Color(9), false, []string{"101"}},
		{"256-palette fg", vt10x.Color(200), true, []string{"38;5;200"}},
		{"256-palette bg", vt10x.Color(200), false, []string{"48;5;200"}},
		{"truecolor fg", vt10x.Color(0x10<<16 | 0x20<<8 | 0x30), true, []string{"38;2;16;32;48"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := colorSGR(tt.c, tt.fg)
			if strings.Join(got, ";") != strings.Join(tt.want, ";") {
				t.Errorf("colorSGR(%d, %v) = %v, want %v", tt.c, tt.fg, got, tt.want)
			}
		})
	}
}

func TestGlyphSGR(t *testing.T) {
	// Default glyph → no SGR.
	if got := glyphSGR(vt10x.Glyph{FG: vt10x.DefaultFG, BG: vt10x.DefaultBG}); got != "" {
		t.Errorf("default glyph SGR = %q, want empty", got)
	}
	// Bold + red foreground.
	g := vt10x.Glyph{FG: vt10x.Red, BG: vt10x.DefaultBG, Mode: attrBold}
	if got := glyphSGR(g); got != "\x1b[1;31m" {
		t.Errorf("bold red SGR = %q, want %q", got, "\x1b[1;31m")
	}
}

func TestReadScreenStyledPreservesColor(t *testing.T) {
	s := newTestSession()
	writeTestTerm(t, s, "\x1b[31mRED\x1b[0m plain\r\n")

	styled := s.readScreenStyled(40)
	if !strings.Contains(styled, "\x1b[31m") {
		t.Errorf("styled read missing red SGR: %q", styled)
	}
	if !strings.Contains(styled, "RED") {
		t.Errorf("styled read missing text: %q", styled)
	}
	if !strings.Contains(styled, sgrResetSeq) {
		t.Errorf("styled read missing reset: %q", styled)
	}

	// Plain read must remain colorless so status classification is unaffected.
	plain := s.readScreen(40)
	if strings.Contains(plain, "\x1b") {
		t.Errorf("plain read leaked escape sequences: %q", plain)
	}
	if !strings.Contains(plain, "RED plain") {
		t.Errorf("plain read missing text: %q", plain)
	}
}
