package pty

import (
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/hinshun/vt10x"
)

const bellChar = "\x07"

type session struct {
	id   string
	ptmx *os.File
	cmd  *exec.Cmd
	term vt10x.Terminal

	mu          sync.Mutex
	name        string // from OSC title escapes
	exited      bool
	cleaned     bool // true after cleanupSession has run
	bellPending bool
	inOSC       bool // tracks whether we're inside an OSC escape sequence across reads
	escPending  bool // true when last byte of previous read was ESC (for split ESC] or ESC\)
	done        chan struct{}
}

// readLoop reads from the PTY and feeds data to the vt10x terminal.
// It intercepts bell characters (\x07) before they are consumed by vt10x,
// distinguishing real bells from BEL bytes that terminate OSC sequences.
func (s *session) readLoop() {
	defer close(s.done)
	buf := make([]byte, readBufSize)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			data := buf[:n]

			// Count real bell characters, excluding BEL that terminates OSC sequences.
			// State persists across reads since sequences can span buffer boundaries.
			s.mu.Lock()
			bells := countBells(data, &s.inOSC, &s.escPending)
			s.mu.Unlock()

			s.term.Write(data)

			// Read title from vt10x (it parses OSC 0/1/2 natively)
			title := s.term.Title()

			s.mu.Lock()
			if bells > 0 {
				s.bellPending = true
			}
			if title != "" {
				s.name = title
			}
			s.mu.Unlock()
		}
		if err != nil {
			s.mu.Lock()
			s.exited = true
			s.mu.Unlock()
			return
		}
	}
}

// countBells counts real bell characters (\x07) in data, excluding BEL bytes
// that terminate OSC escape sequences (\x1b]...\x07). The inOSC and escPending
// states are persisted across calls to handle sequences that span buffer
// boundaries. escPending covers both ESC] (OSC start) and ESC\ (ST end)
// being split across reads.
func countBells(data []byte, inOSC *bool, escPending *bool) int {
	bells := 0
	for i := 0; i < len(data); i++ {
		// Handle ESC that was the last byte of the previous read.
		if *escPending {
			*escPending = false
			if *inOSC {
				// Inside OSC: ESC\ is ST (terminates OSC).
				if data[i] == '\\' {
					*inOSC = false
					continue
				}
				// Not a ST — stay inside OSC, fall through to normal processing.
			} else {
				// Outside OSC: ESC] starts an OSC sequence.
				if data[i] == ']' {
					*inOSC = true
					continue
				}
				// Not an OSC start — the ESC was something else, fall through.
			}
		}

		if *inOSC {
			if data[i] == '\x07' {
				*inOSC = false // BEL terminates OSC — not a real bell
			} else if data[i] == '\x1b' {
				if i+1 < len(data) {
					if data[i+1] == '\\' {
						*inOSC = false // ST (\x1b\) terminates OSC
						i++
					}
				} else {
					*escPending = true
				}
			}
		} else {
			if data[i] == '\x1b' {
				if i+1 < len(data) {
					if data[i+1] == ']' {
						*inOSC = true
						i++
					}
				} else {
					*escPending = true
				}
			} else if data[i] == '\x07' {
				bells++
			}
		}
	}
	return bells
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
		result = bellChar + result
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

// isCleaned returns whether cleanupSession has already run for this session.
func (s *session) isCleaned() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cleaned
}
