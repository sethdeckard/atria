package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/sethdeckard/atria/internal/model"
)

type chatView struct {
	viewport viewport.Model
	input    textarea.Model
	entries  []model.ChatEntry
	context  string // dim screen context shown before entries
	ready    bool
}

func newChatView() chatView {
	ta := textarea.New()
	ta.Placeholder = "Type a prompt..."
	ta.ShowLineNumbers = false
	ta.SetHeight(3)
	ta.Focus()
	ta.CharLimit = 0

	vp := viewport.New(80, 20)

	return chatView{
		viewport: vp,
		input:    ta,
	}
}

func (c *chatView) setSize(width, height int) {
	headerHeight := 3
	inputHeight := 5
	borderHeight := 2
	vpHeight := height - headerHeight - inputHeight - borderHeight
	if vpHeight < 5 {
		vpHeight = 5
	}
	c.viewport.Width = width
	c.viewport.Height = vpHeight
	c.input.SetWidth(width - 2)
	c.ready = true
}

// setContext seeds the viewport with recent screen lines as initial
// context, so the user sees what the agent is doing before sending.
func (c *chatView) setContext(screen string) {
	if screen == "" {
		return
	}
	lines := strings.Split(screen, "\n")
	// Trim trailing blank lines
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > 15 {
		lines = lines[len(lines)-15:]
	}
	if len(lines) == 0 {
		return
	}
	c.context = dimStyle.Render(strings.Join(lines, "\n")) + "\n"
	c.updateViewport()
}

func (c *chatView) addEntry(entry model.ChatEntry) {
	c.entries = append(c.entries, entry)
	c.updateViewport()
}

func (c *chatView) updateViewport() {
	var sb strings.Builder
	sb.WriteString(c.context)
	for _, e := range c.entries {
		ts := e.Timestamp.Format("15:04")
		switch e.Direction {
		case "sent":
			line := chatSentStyle.Render(fmt.Sprintf("[%s] > %s", ts, e.Text))
			sb.WriteString(line)
		case "received":
			line := chatReceivedStyle.Render(fmt.Sprintf("[%s]   %s", ts, e.Text))
			sb.WriteString(line)
		}
		sb.WriteString("\n")
	}
	c.viewport.SetContent(sb.String())
	c.viewport.GotoBottom()
}

func (c *chatView) renderHeader(session *model.AgentSession, project *model.Project) string {
	if session == nil || project == nil {
		return chatHeaderStyle.Render("No agent selected")
	}
	agentStr := strings.ToUpper(string(session.Type)[:1]) + string(session.Type)[1:]
	header := fmt.Sprintf("  Agent: %s | Project: %s | Status: %s",
		agentStr,
		project.Name,
		string(session.Status),
	)
	return chatHeaderStyle.Render(header)
}

func (c *chatView) render(session *model.AgentSession, project *model.Project, width int) string {
	header := c.renderHeader(session, project)
	vpView := c.viewport.View()
	inputHint := dimStyle.Render("  Enter: newline  Ctrl+D: send  Esc: back")
	inputBorder := chatInputBorderStyle.Render(inputHint)
	inputView := c.input.View()

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		vpView,
		inputBorder,
		inputView,
	)
}
