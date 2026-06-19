package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestTruncateToWidth(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxWidth int
		want     string
	}{
		{"empty string", "", 10, ""},
		{"fits exactly", "hello", 5, "hello"},
		{"fits within", "hi", 10, "hi"},
		{"truncates ASCII", "hello world", 8, "hello w\u2026"},
		{"maxWidth 1", "hello", 1, "\u2026"},
		{"maxWidth 0", "hello", 0, "\u2026"},
		{"multi-byte runes", "héllo", 4, "hél\u2026"},
		{"single char no truncation", "x", 5, "x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateToWidth(tt.input, tt.maxWidth)
			if got != tt.want {
				t.Errorf("truncateToWidth(%q, %d) = %q, want %q", tt.input, tt.maxWidth, got, tt.want)
			}
		})
	}
}

func TestSanitizeBoxText(t *testing.T) {
	input := "alpha\tbeta\r\ngamma\rdelta\x00" +
		"\x1b[33m warn\x1b[0m" +
		"\x1b]0;title\x07" +
		string(rune(0x0007))
	got := sanitizeBoxText(input)
	want := "alpha    beta\ngamma\ndelta warn"
	if got != want {
		t.Fatalf("sanitizeBoxText() = %q, want %q", got, want)
	}
}

func TestSanitizeBoxTextStyled(t *testing.T) {
	// SGR color codes are preserved; cursor moves, OSC titles, bare BEL,
	// control chars are dropped; tabs/CRLF normalized — same layout safety as
	// the plain sanitizer, only color codes survive.
	input := "alpha\tbeta\r\ngamma\rdelta\x00" +
		"\x1b[33m warn\x1b[0m" +
		"\x1b[2J\x1b[10;5H" + // cursor/erase — must be dropped
		"\x1b]0;title\x07" + // OSC — dropped
		string(rune(0x0007))
	got := sanitizeBoxTextStyled(input)
	want := "alpha    beta\ngamma\ndelta\x1b[33m warn\x1b[0m"
	if got != want {
		t.Fatalf("sanitizeBoxTextStyled() = %q, want %q", got, want)
	}
}

func TestSanitizeBoxTextStyledHorizontalMotion(t *testing.T) {
	// Terminals like WezTerm emit cursor-forward (\x1b[<n>C) and column-set
	// (\x1b[<n>G) instead of literal spaces when dumping styled screens. These
	// must become spaces so word spacing survives; SGR is still preserved.
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"cursor forward n", "a\x1b[3Cb", "a   b"},
		{"cursor forward default", "a\x1b[Cb", "a b"},
		{"column absolute", "ab\x1b[6Gc", "ab   c"},
		{"sgr survives across motion", "\x1b[31ma\x1b[2Cb\x1b[0m", "\x1b[31ma  b\x1b[0m"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeBoxTextStyled(tt.input); got != tt.want {
				t.Errorf("sanitizeBoxTextStyled(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSanitizeBoxTextStyledStripsNonSGREscapes(t *testing.T) {
	// Charset-designation escapes (ESC ( B, ESC ) 0, …) and other non-SGR
	// escapes must be fully consumed — their final byte must not leak as text
	// (e.g. the "B" from ESC ( B). SGR is still preserved.
	cases := []struct{ name, in, want string }{
		{"charset G0 ascii", "a\x1b(Bb", "ab"},
		{"charset G0 line-draw", "a\x1b(0b", "ab"},
		{"charset G1", "a\x1b)Bb", "ab"},
		{"two-byte escape (RIS)", "a\x1bcb", "ab"},
		{"DCS string", "a\x1bP1;2|x\x1b\\b", "ab"},
		{"sgr preserved alongside charset", "\x1b(B\x1b[31mx\x1b[0m", "\x1b[31mx\x1b[0m"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeBoxTextStyled(tt.in); got != tt.want {
				t.Errorf("sanitizeBoxTextStyled(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestTruncateToWidthANSI(t *testing.T) {
	// Visual width ignores SGR; the escape is never split mid-sequence.
	colored := "\x1b[31mhello world\x1b[0m"
	got := truncateToWidthANSI(colored, 8)
	if w := lipgloss.Width(got); w > 8 {
		t.Errorf("truncated width = %d, want <= 8 (got %q)", w, got)
	}
	if !strings.Contains(got, "\x1b[31m") {
		t.Errorf("truncation dropped the leading SGR: %q", got)
	}
	// Short colored text that fits is returned unchanged.
	if out := truncateToWidthANSI(colored, 50); out != colored {
		t.Errorf("fitting string altered: %q", out)
	}
}

func TestIsBlankLine(t *testing.T) {
	cases := map[string]bool{
		"":                   true,
		"   ":                true,
		"\x1b[0m":            true,
		"\x1b[44m   \x1b[0m": true,
		"\x1b[31mx\x1b[0m":   false,
		"text":               false,
	}
	for in, want := range cases {
		if got := isBlankLine(in); got != want {
			t.Errorf("isBlankLine(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestNextSelectableItem(t *testing.T) {
	tests := []struct {
		name  string
		items []settingsItem
		cur   int
		dir   int
		want  int
	}{
		{"empty", nil, 0, 1, 0},
		{"forward skip header", []settingsItem{
			{itemType: "toggle"},
			{itemType: "header"},
			{itemType: "toggle"},
		}, 0, 1, 2},
		{"backward skip header", []settingsItem{
			{itemType: "toggle"},
			{itemType: "header"},
			{itemType: "toggle"},
		}, 2, -1, 0},
		{"forward at end returns cur", []settingsItem{
			{itemType: "toggle"},
			{itemType: "header"},
		}, 0, 1, 0},
		{"backward at start returns cur", []settingsItem{
			{itemType: "header"},
			{itemType: "toggle"},
		}, 1, -1, 1},
		{"forward to next", []settingsItem{
			{itemType: "toggle"},
			{itemType: "toggle"},
		}, 0, 1, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nextSelectableItem(tt.items, tt.cur, tt.dir)
			if got != tt.want {
				t.Errorf("nextSelectableItem(items, %d, %d) = %d, want %d", tt.cur, tt.dir, got, tt.want)
			}
		})
	}
}

func TestRenderTitleBar(t *testing.T) {
	result := renderTitleBar("test", 80)
	if !strings.Contains(result, "test") {
		t.Error("renderTitleBar should contain the title")
	}
	if !strings.Contains(result, "atria") {
		t.Error("renderTitleBar should contain branding")
	}
	if !strings.Contains(result, "\u2500") {
		t.Error("renderTitleBar should contain separator")
	}
	// Should end with newline
	if !strings.HasSuffix(result, "\n") {
		t.Error("renderTitleBar should end with newline")
	}
	// Narrow width — branding may be dropped
	narrow := renderTitleBar("test", 5)
	if lipgloss.Width(strings.Split(narrow, "\n")[0]) > 10 {
		t.Error("renderTitleBar with narrow width should not overflow")
	}
}
