package pty

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/hinshun/vt10x"
)

type session struct {
	id   string
	ptmx *os.File
	cmd  *exec.Cmd
	term vt10x.Terminal

	mu          sync.Mutex
	name        string // from OSC title escapes
	exited      bool
	bellPending bool
	done        chan struct{}
}

// readLoop reads from the PTY and feeds data to the vt10x terminal.
// It intercepts bell characters (\x07) before they are consumed by vt10x.
func (s *session) readLoop() {
	defer close(s.done)
	buf := make([]byte, 4096)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			data := buf[:n]

			// Check for bell characters in the raw data.
			// Note: \x07 also terminates OSC sequences, but we accept the
			// minor false-positive risk since the status classifier handles it.
			if bytes.ContainsRune(data, '\a') {
				s.mu.Lock()
				s.bellPending = true
				s.mu.Unlock()
			}

			s.term.Write(data)

			// Read title from vt10x (it parses OSC 0/1/2 natively)
			title := s.term.Title()
			if title != "" {
				s.mu.Lock()
				s.name = title
				s.mu.Unlock()
			}
		}
		if err != nil {
			s.mu.Lock()
			s.exited = true
			s.mu.Unlock()
			return
		}
	}
}

// readScreen returns the last N lines from the vt10x screen buffer.
// If a bell is pending, it prepends \x07 so HasBell() in the monitor works.
func (s *session) readScreen(lines int) string {
	content := s.term.String()

	s.mu.Lock()
	bell := s.bellPending
	s.bellPending = false
	s.mu.Unlock()

	// Split into lines and take the last N
	allLines := strings.Split(content, "\n")
	// Trim trailing empty line from String() (it always ends with \n)
	if len(allLines) > 0 && allLines[len(allLines)-1] == "" {
		allLines = allLines[:len(allLines)-1]
	}
	if lines > 0 && len(allLines) > lines {
		allLines = allLines[len(allLines)-lines:]
	}
	result := strings.Join(allLines, "\n")

	if bell {
		result = "\x07" + result
	}
	return result
}

// getName returns the current session name (from OSC title).
func (s *session) getName() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.name
}

// isExited returns whether the child process has exited.
func (s *session) isExited() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exited
}
