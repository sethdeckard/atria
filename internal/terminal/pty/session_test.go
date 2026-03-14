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
		s.term.Write([]byte(l + "\r\n"))
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
	s.term.Write([]byte("hello\r\n"))

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
	s.term.Write([]byte("hello\r\n"))

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

func TestReadScreenWithOSCTitle(t *testing.T) {
	s := newTestSession()

	// Write an OSC title sequence then content on a new line
	s.term.Write([]byte("\033]0;my-agent\007"))
	s.term.Write([]byte("some content\r\n"))

	title := s.term.Title()
	if title != "my-agent" {
		t.Errorf("expected title 'my-agent', got %q", title)
	}

	content := s.readScreen(24)
	if !strings.Contains(content, "some content") {
		t.Errorf("expected screen to contain 'some content', got %q", content)
	}
}
