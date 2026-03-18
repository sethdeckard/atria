package pty

import (
	"strings"
	"testing"

	"github.com/hinshun/vt10x"
)

func newTestSession() *session {
	term := vt10x.New(vt10x.WithSize(80, 24))
	return &session{
		id:   "test-0",
		term: term,
		done: make(chan struct{}),
	}
}

func TestReadScreenLastNLines(t *testing.T) {
	s := newTestSession()

	// Write multiple lines to the terminal
	var lines []string
	for i := 0; i < 10; i++ {
		lines = append(lines, strings.Repeat("x", i+1))
	}
	for _, l := range lines {
		_, _ = s.term.Write([]byte(l + "\r\n"))
	}

	tests := []struct {
		name      string
		reqLines  int
		wantCount int // number of non-empty lines expected
	}{
		{"last 3 lines", 3, 3},
		{"last 5 lines", 5, 5},
		{"more than available", 50, 24}, // terminal is 24 rows
		{"zero means all", 0, 24},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := s.readScreen(tt.reqLines)
			got := strings.Split(content, "\n")
			if len(got) != tt.wantCount {
				t.Errorf("readScreen(%d) returned %d lines, want %d", tt.reqLines, len(got), tt.wantCount)
			}
		})
	}
}

func TestReadScreenBellPending(t *testing.T) {
	s := newTestSession()
	_, _ = s.term.Write([]byte("hello\r\n"))

	// Set bell pending
	s.mu.Lock()
	s.bellPending = true
	s.mu.Unlock()

	content := s.readScreen(5)
	if !strings.HasPrefix(content, bellChar) {
		t.Error("expected bell character prefix when bellPending is true")
	}

	// Second read should not have bell
	content2 := s.readScreen(5)
	if strings.HasPrefix(content2, bellChar) {
		t.Error("bell should be consumed after first read")
	}
}

func TestReadScreenNoBell(t *testing.T) {
	s := newTestSession()
	_, _ = s.term.Write([]byte("hello\r\n"))

	content := s.readScreen(5)
	if strings.Contains(content, bellChar) {
		t.Error("expected no bell character when bellPending is false")
	}
}

func TestGetName(t *testing.T) {
	s := newTestSession()

	if name := s.getName(); name != "" {
		t.Errorf("expected empty name, got %q", name)
	}

	s.mu.Lock()
	s.name = "test-title"
	s.mu.Unlock()

	if name := s.getName(); name != "test-title" {
		t.Errorf("expected 'test-title', got %q", name)
	}
}

func TestIsExited(t *testing.T) {
	s := newTestSession()

	if s.isExited() {
		t.Error("expected isExited() = false initially")
	}

	s.mu.Lock()
	s.exited = true
	s.mu.Unlock()

	if !s.isExited() {
		t.Error("expected isExited() = true after setting exited")
	}
}

func TestCountBells(t *testing.T) {
	tests := []struct {
		name           string
		data           string
		initInOSC      bool
		initEscPending bool
		wantBells      int
		wantInOSC      bool
		wantEscPending bool
	}{
		{"bare bell", "\x07", false, false, 1, false, false},
		{"multiple bare bells", "\x07\x07\x07", false, false, 3, false, false},
		{"no bells", "hello world", false, false, 0, false, false},
		{"OSC title with BEL terminator", "\x1b]0;my-title\x07", false, false, 0, false, false},
		{"OSC title with ST terminator", "\x1b]0;my-title\x1b\\", false, false, 0, false, false},
		{"OSC then real bell", "\x1b]0;title\x07\x07", false, false, 1, false, false},
		{"real bell then OSC", "\x07\x1b]0;title\x07", false, false, 1, false, false},
		{"OSC split across reads - start", "\x1b]0;tit", false, false, 0, true, false},
		{"OSC split across reads - end BEL", "le\x07", true, false, 0, false, false},
		{"BEL terminates OSC not real bell", "\x07", true, false, 0, false, false},
		// ST (\x1b\) split across reads while inside OSC
		{"ST split - ESC at end of read", "\x1b]0;title\x1b", false, false, 0, true, true},
		{"ST split - backslash continues", "\\", true, true, 0, false, false},
		{"ST split - non-backslash continues", "x", true, true, 0, true, false},
		{"ST split then real bell", "\\\x07", true, true, 1, false, false},
		// OSC introducer (\x1b]) split across reads
		{"OSC start split - ESC at end", "text\x1b", false, false, 0, false, true},
		{"OSC start split - ] continues", "]0;title\x07", false, true, 0, false, false},
		{"OSC start split - non-] continues", "[A", false, true, 0, false, false},
		{"OSC start split - BEL after non-]", "[A\x07", false, true, 1, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inOSC := tt.initInOSC
			escPending := tt.initEscPending
			bells := countBells([]byte(tt.data), &inOSC, &escPending)
			if bells != tt.wantBells {
				t.Errorf("countBells() = %d, want %d", bells, tt.wantBells)
			}
			if inOSC != tt.wantInOSC {
				t.Errorf("inOSC = %v, want %v", inOSC, tt.wantInOSC)
			}
			if escPending != tt.wantEscPending {
				t.Errorf("escPending = %v, want %v", escPending, tt.wantEscPending)
			}
		})
	}
}

func TestOSCTitleDoesNotTriggerBell(t *testing.T) {
	s := newTestSession()

	// Simulate readLoop processing: write OSC title sequence
	data := []byte("\x1b]0;✳ my-agent\x07")
	s.mu.Lock()
	bells := countBells(data, &s.inOSC, &s.escPending)
	if bells > 0 {
		s.bellPending = true
	}
	s.mu.Unlock()
	_, _ = s.term.Write(data)

	if s.bellPending {
		t.Error("OSC title BEL should not trigger bellPending")
	}
}

func TestBareBellTriggersBellPending(t *testing.T) {
	s := newTestSession()

	// Simulate readLoop processing: bare bell
	data := []byte("some output\x07")
	s.mu.Lock()
	bells := countBells(data, &s.inOSC, &s.escPending)
	if bells > 0 {
		s.bellPending = true
	}
	s.mu.Unlock()

	if !s.bellPending {
		t.Error("bare \\x07 should trigger bellPending")
	}
}

func TestReadScreenWithOSCTitle(t *testing.T) {
	s := newTestSession()

	// Write an OSC title sequence then content on a new line
	_, _ = s.term.Write([]byte("\033]0;my-agent\007"))
	_, _ = s.term.Write([]byte("some content\r\n"))

	title := s.term.Title()
	if title != "my-agent" {
		t.Errorf("expected title 'my-agent', got %q", title)
	}

	content := s.readScreen(24)
	if !strings.Contains(content, "some content") {
		t.Errorf("expected screen to contain 'some content', got %q", content)
	}
}
