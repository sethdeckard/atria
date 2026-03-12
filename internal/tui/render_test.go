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
