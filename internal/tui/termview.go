package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sethdeckard/atria/internal/model"
	"github.com/sethdeckard/atria/internal/terminal"
)

// termRefreshMsg triggers a terminal view refresh at 100ms intervals.
type termRefreshMsg struct{}

// termView is the embedded terminal view component for the PTY backend.
type termView struct {
	sessionID    string
	backend      terminal.Backend
	content      string
	status       model.AgentStatus
	agentType    model.AgentType
	spinnerFrame int
	width        int
	height       int
}

func newTermView(sessionID string, backend terminal.Backend) termView {
	return termView{
		sessionID: sessionID,
		backend:   backend,
	}
}

func (tv termView) headerBar() string {
	title := tv.sessionID
	sessions, err := tv.backend.ListSessions()
	if err == nil {
		for _, s := range sessions {
			if s.ID == tv.sessionID {
				if s.Name != "" {
					title = s.Name
				}
				break
			}
		}
	}

	var icon string
	var style lipgloss.Style
	switch tv.status {
	case model.StatusWorking:
		icon = spinnerFrames[tv.spinnerFrame%len(spinnerFrames)]
		style = statusWorkingStyle
	case model.StatusNeedsInput:
		icon = "⚠"
		style = statusNeedsInputStyle
	case model.StatusError:
		icon = "✗"
		style = statusErrorStyle
	default:
		icon = "●"
		style = statusIdleStyle
	}
	style = style.Bold(true)

	left := " " + icon + " " + title
	right := "Ctrl+\\ to return "
	gap := tv.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return style.Render(left + strings.Repeat(" ", gap) + right)
}

func (tv termView) render() string {
	var sb strings.Builder
	sb.WriteString(tv.headerBar())
	sb.WriteString("\n")

	contentHeight := tv.height - 2 // header + separator
	if contentHeight < 1 {
		contentHeight = 1
	}

	lines := strings.Split(tv.content, "\n")
	// Trim trailing empty lines
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > contentHeight {
		lines = lines[len(lines)-contentHeight:]
	}

	for _, line := range lines {
		line = truncateToWidth(line, tv.width)
		sb.WriteString(line)
		sb.WriteString("\n")
	}

	// Pad remaining lines
	for i := len(lines); i < contentHeight; i++ {
		sb.WriteString("\n")
	}

	return sb.String()
}

func termRefreshCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return termRefreshMsg{}
	})
}

// keyToBytes translates a Bubble Tea KeyMsg to raw PTY bytes.
func keyToBytes(msg tea.KeyMsg) []byte {
	switch msg.Type {
	case tea.KeyRunes:
		return []byte(string(msg.Runes))
	case tea.KeyEnter:
		return []byte("\r")
	case tea.KeyTab:
		return []byte("\t")
	case tea.KeySpace:
		return []byte(" ")
	case tea.KeyBackspace:
		return []byte{0x7f}
	case tea.KeyDelete:
		return []byte("\x1b[3~")
	case tea.KeyUp:
		return []byte("\x1b[A")
	case tea.KeyDown:
		return []byte("\x1b[B")
	case tea.KeyRight:
		return []byte("\x1b[C")
	case tea.KeyLeft:
		return []byte("\x1b[D")
	case tea.KeyHome:
		return []byte("\x1b[H")
	case tea.KeyEnd:
		return []byte("\x1b[F")
	case tea.KeyPgUp:
		return []byte("\x1b[5~")
	case tea.KeyPgDown:
		return []byte("\x1b[6~")
	case tea.KeyEscape:
		return []byte("\x1b")
	case tea.KeyCtrlA:
		return []byte{0x01}
	case tea.KeyCtrlB:
		return []byte{0x02}
	case tea.KeyCtrlC:
		return []byte{0x03}
	case tea.KeyCtrlD:
		return []byte{0x04}
	case tea.KeyCtrlE:
		return []byte{0x05}
	case tea.KeyCtrlF:
		return []byte{0x06}
	case tea.KeyCtrlG:
		return []byte{0x07}
	case tea.KeyCtrlH:
		return []byte{0x08}
	case tea.KeyCtrlK:
		return []byte{0x0b}
	case tea.KeyCtrlL:
		return []byte{0x0c}
	case tea.KeyCtrlN:
		return []byte{0x0e}
	case tea.KeyCtrlO:
		return []byte{0x0f}
	case tea.KeyCtrlP:
		return []byte{0x10}
	case tea.KeyCtrlR:
		return []byte{0x12}
	case tea.KeyCtrlS:
		return []byte{0x13}
	case tea.KeyCtrlT:
		return []byte{0x14}
	case tea.KeyCtrlU:
		return []byte{0x15}
	case tea.KeyCtrlW:
		return []byte{0x17}
	case tea.KeyCtrlX:
		return []byte{0x18}
	case tea.KeyCtrlY:
		return []byte{0x19}
	case tea.KeyCtrlZ:
		return []byte{0x1a}
	case tea.KeyF1:
		return []byte("\x1bOP")
	case tea.KeyF2:
		return []byte("\x1bOQ")
	case tea.KeyF3:
		return []byte("\x1bOR")
	case tea.KeyF4:
		return []byte("\x1bOS")
	case tea.KeyF5:
		return []byte("\x1b[15~")
	case tea.KeyF6:
		return []byte("\x1b[17~")
	case tea.KeyF7:
		return []byte("\x1b[18~")
	case tea.KeyF8:
		return []byte("\x1b[19~")
	case tea.KeyF9:
		return []byte("\x1b[20~")
	case tea.KeyF10:
		return []byte("\x1b[21~")
	case tea.KeyF11:
		return []byte("\x1b[23~")
	case tea.KeyF12:
		return []byte("\x1b[24~")
	}
	// Fallback: if it's a string representation we didn't handle
	if s := fmt.Sprintf("%s", msg); len(s) > 0 {
		return []byte(s)
	}
	return nil
}
