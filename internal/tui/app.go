package tui

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sethdeckard/atria/internal/model"
	"github.com/sethdeckard/atria/internal/terminal"
)

// monitorPatterns are the regex patterns for it2 monitor output.
// Captures permission prompts, errors, idle prompts, and completion signals.
const monitorPatterns = `Allow|Permission|Error:|\\?$|Waiting for|proceed|Esc to cancel|❯|›|shortcuts|\\$|completed|No findings|✓`

// spinnerFrames for the working activity indicator.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type viewState int

const (
	viewProjectList viewState = iota
	viewChat
	viewDirBrowser
	viewBatchPrompt
)


type Model struct {
	// Config
	watchDirs  []string
	monitorDir string
	launchDir  string

	// Backend
	backend    terminal.Backend
	backendOK  bool
	backendErr string

	// Data
	store *model.Store
	rows  []projectRow

	// UI state
	view         viewState
	cursor       int
	scrollOffset int
	width        int
	height       int
	statusText   string
	showHelp     bool

	// Chat view
	chat          chatView
	chatSessionID string // session ID for active chat

	// Spinner & attention
	spinnerFrame  int
	spinnerActive bool
	attentionSessions map[string]time.Time // session IDs needing attention, with timestamp

	// Agent type
	availableAgents []model.AgentType
	defaultAgent    model.AgentType

	// Directory browser
	browserDirs   []DirBrowserItem
	browserCursor int
	browserScroll int
	browserPath   string

	// Batch prompt
	batchInput textarea.Model

	// Monitor PIDs to clean up
	monitorPIDs []int

	// Debug logger (nil = no logging)
	debugLog *log.Logger
}

type storeAdapter struct {
	s *model.Store
}

func (a storeAdapter) Projects() []*model.Project {
	return a.s.Projects
}

func (a storeAdapter) GetSessions(dir string) []*model.AgentSession {
	return a.s.GetSessions(dir)
}

func NewModel(backend terminal.Backend, store *model.Store, watchDirs []string, monitorDir string) Model {
	return NewModelWithConfig(backend, store, watchDirs, monitorDir, "", "")
}

func NewModelWithConfig(backend terminal.Backend, store *model.Store, watchDirs []string, monitorDir string, defaultAgentCfg string, launchDir string) Model {
	ba := textarea.New()
	ba.Placeholder = "Batch prompt ({name} for project name)..."
	ba.ShowLineNumbers = false
	ba.SetHeight(3)
	ba.CharLimit = 0

	available := detectAvailableAgents()

	// Resolve default agent from config, falling back to first available.
	var defaultAgent model.AgentType
	switch model.AgentType(defaultAgentCfg) {
	case model.AgentClaude, model.AgentCodex:
		// Validate it's actually available.
		found := false
		for _, a := range available {
			if a == model.AgentType(defaultAgentCfg) {
				found = true
				break
			}
		}
		if found {
			defaultAgent = model.AgentType(defaultAgentCfg)
		}
	}
	if defaultAgent == "" {
		if len(available) > 0 {
			defaultAgent = available[0]
		} else {
			defaultAgent = model.AgentClaude
		}
	}

	// Sessions loaded from disk have no persisted status (json:"-");
	// default to idle since we don't know their state after restart.
	for _, s := range store.Sessions {
		if s.Status == "" {
			s.Status = model.StatusIdle
		}
	}

	return Model{
		watchDirs:       watchDirs,
		monitorDir:      monitorDir,
		launchDir:       launchDir,
		backend:         backend,
		store:           store,
		rows:            buildRows(storeAdapter{store}),
		chat:            newChatView(),
		batchInput:      ba,
		availableAgents: available,
		defaultAgent:    defaultAgent,
	}
}

func (m Model) Init() tea.Cmd {
	var cmds []tea.Cmd
	cmds = append(cmds, checkBackend(m.backend))
	cmds = append(cmds, tickCmd())
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.chat.setSize(msg.Width, msg.Height)
		m.adjustScroll()
		m.adjustBrowserScroll()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case BackendAvailableMsg:
		if msg.Err != nil {
			m.backendOK = false
			m.backendErr = msg.Err.Error()
			m.statusText = "Backend unavailable: " + msg.Err.Error()
		} else {
			m.backendOK = true
			return m, refreshSessions(m.backend)
		}
		return m, nil

	case SessionsRefreshedMsg:
		return m.handleSessionsRefreshed(msg)

	case AgentLaunchedMsg:
		return m.handleAgentLaunched(msg)

	case PromptSentMsg:
		if msg.Err != nil {
			m.statusText = "Send failed: " + msg.Err.Error()
		}
		return m, nil

	case StatusUpdatedMsg:
		return m.handleStatusUpdated(msg)

	case MonitorStartedMsg:
		return m.handleMonitorStarted(msg)

	case FocusedMsg:
		if msg.Err != nil {
			m.statusText = "Focus failed: " + msg.Err.Error()
		}
		return m, nil

	case AgentDiscoveredMsg:
		return m.handleAgentDiscovered(msg)

	case ScreenReadMsg:
		return m.handleScreenRead(msg)

	case SpinnerTickMsg:
		if !m.hasActiveAnimations() {
			m.spinnerActive = false
			return m, nil
		}
		m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerFrames)
		return m, spinnerTickCmd()

	case TickMsg:
		return m.handleTick()

	case DirBrowserMsg:
		m.browserDirs = msg.Dirs
		m.browserCursor = 0
		m.browserScroll = 0
		m.browserPath = msg.CurrentDir
		m.view = viewDirBrowser
		return m, nil

	case StatusMsg:
		m.statusText = msg.Text
		return m, nil
	}

	// Pass through to sub-components
	if m.view == viewChat {
		return m.updateChat(msg)
	}
	if m.view == viewBatchPrompt {
		return m.updateBatch(msg)
	}
	return m, nil
}

func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	var content string
	switch m.view {
	case viewProjectList:
		content = m.viewProjectList()
	case viewChat:
		content = m.viewChat()
	case viewDirBrowser:
		content = m.viewDirBrowser()
	case viewBatchPrompt:
		content = m.viewBatchPrompt()
	}

	if m.showHelp {
		content = m.viewHelp() + "\n\n" + content
	}

	return content
}

func (m Model) viewProjectList() string {
	var sb strings.Builder
	allProjects := m.store.Projects

	maxRows := m.maxVisibleRows()

	// Clamp scrollOffset for this frame in case layout changed without
	// adjustScroll (e.g. statusText set outside key handlers).
	scrollOffset := m.scrollOffset
	if len(m.rows) <= maxRows || maxRows <= 0 {
		scrollOffset = 0
	} else {
		if m.cursor < scrollOffset {
			scrollOffset = m.cursor
		} else if m.cursor >= scrollOffset+maxRows {
			scrollOffset = m.cursor - maxRows + 1
		}
	}

	list := renderProjectList(m.rows, m.cursor, allProjects, m.width, m.spinnerFrame, m.attentionSessions, m.defaultAgent, len(m.availableAgents) > 1, maxRows, scrollOffset)
	sb.WriteString(list)

	var selected *projectRow
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		selected = &m.rows[m.cursor]
	}
	sb.WriteString("\n")
	sb.WriteString(renderFooter(len(m.rows), selected, m.defaultAgent, len(m.availableAgents) > 1))

	if m.statusText != "" {
		sb.WriteString("\n")
		sb.WriteString(statusBarStyle.Render(m.statusText))
	}

	return sb.String()
}

// maxVisibleRows returns how many agent rows fit in the current terminal.
func (m Model) maxVisibleRows() int {
	overhead := headerLineCount + footerLineCount
	if m.statusText != "" {
		overhead++
	}
	if m.showHelp {
		overhead += strings.Count(m.viewHelp(), "\n") + 2
	}
	max := m.height - overhead
	if max < 1 {
		max = 1
	}
	return max
}

// adjustScroll ensures the cursor is visible within the scroll window.
func (m *Model) adjustScroll() {
	maxVisible := m.maxVisibleRows()
	if m.cursor < m.scrollOffset {
		m.scrollOffset = m.cursor
	} else if m.cursor >= m.scrollOffset+maxVisible {
		m.scrollOffset = m.cursor - maxVisible + 1
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
}

func (m Model) viewChat() string {
	var session *model.AgentSession
	var project *model.Project
	if m.chatSessionID != "" {
		session = m.store.SessionByID(m.chatSessionID)
		if session != nil {
			project = m.store.FindProject(session.ProjectDir)
		}
	}
	return m.chat.render(session, project, m.width)
}

func (m Model) viewDirBrowser() string {
	var sb strings.Builder
	agentName := strings.ToUpper(string(m.defaultAgent)[:1]) + string(m.defaultAgent)[1:]

	// Header: "launch Claude" left, contracted path right
	left := titleStyle.Render("  launch " + agentName)
	right := dimStyle.Render(contractHome(m.browserPath) + "  ")
	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	gap := m.width - leftW - rightW
	if gap < 0 {
		sb.WriteString(left)
	} else {
		sb.WriteString(left + strings.Repeat(" ", gap) + right)
	}
	sb.WriteString("\n")

	// Separator
	sepWidth := m.width - 2
	if sepWidth < 1 {
		sepWidth = 1
	}
	sb.WriteString(dimStyle.Render("  " + strings.Repeat("\u2500", sepWidth)))
	sb.WriteString("\n")

	// Build flat list of items for scrolling
	type browserLine struct {
		idx   int    // selectable index (-1 for labels/blanks)
		label string // section label, empty for selectable items
	}
	var lines []browserLine
	idx := 0

	recent := m.browserRecentProjects()
	if len(recent) > 0 {
		lines = append(lines, browserLine{idx: -1, label: "recent"})
		for range recent {
			lines = append(lines, browserLine{idx: idx})
			idx++
		}
		lines = append(lines, browserLine{idx: -1}) // blank separator
	}

	for range m.browserDirs {
		lines = append(lines, browserLine{idx: idx})
		idx++
	}

	// Blank + launch action
	lines = append(lines, browserLine{idx: -1}) // blank separator
	launchIdx := idx
	lines = append(lines, browserLine{idx: launchIdx})

	// Compute scroll window over selectable items, then map to lines
	maxVisible := m.browserMaxVisible()
	scrollOffset := m.browserScroll
	totalItems := m.browserRecentCount() + len(m.browserDirs) + 1
	if totalItems <= maxVisible {
		scrollOffset = 0
	} else {
		if m.browserCursor < scrollOffset {
			scrollOffset = m.browserCursor
		} else if m.browserCursor >= scrollOffset+maxVisible {
			scrollOffset = m.browserCursor - maxVisible + 1
		}
	}

	// Find line range to display: from first line containing scrollOffset
	// to last line containing scrollOffset+maxVisible-1
	firstLine := 0
	lastLine := len(lines) - 1
	if totalItems > maxVisible {
		// Find the line containing the first visible selectable item
		for i, l := range lines {
			if l.idx == scrollOffset {
				// Include preceding label if it's right before
				if i > 0 && lines[i-1].idx == -1 && lines[i-1].label != "" {
					firstLine = i - 1
				} else {
					firstLine = i
				}
				break
			}
		}
		endIdx := scrollOffset + maxVisible - 1
		if endIdx >= totalItems {
			endIdx = totalItems - 1
		}
		for i := len(lines) - 1; i >= 0; i-- {
			if lines[i].idx == endIdx {
				// Include trailing blank/launch after last visible
				lastLine = i
				// Include any non-selectable lines right after (blank before launch)
				for lastLine+1 < len(lines) && lines[lastLine+1].idx == -1 {
					lastLine++
				}
				break
			}
		}
	}

	// Render visible lines
	recentItems := recent
	dirItems := m.browserDirs
	recentCount := len(recentItems)
	for i := firstLine; i <= lastLine && i < len(lines); i++ {
		l := lines[i]
		if l.idx == -1 {
			if l.label != "" {
				sb.WriteString(dimStyle.Render("  " + l.label))
			}
			sb.WriteString("\n")
			continue
		}

		selected := l.idx == m.browserCursor
		if l.idx < recentCount {
			// Recent item
			p := recentItems[l.idx]
			if selected {
				sb.WriteString(selectedStyle.Render("> " + p.Name))
				sb.WriteString(dimStyle.Render(" " + contractHome(p.Dir)))
			} else {
				sb.WriteString("  " + p.Name)
				sb.WriteString(dimStyle.Render(" " + contractHome(p.Dir)))
			}
		} else if l.idx < recentCount+len(dirItems) {
			// Directory entry
			d := dirItems[l.idx-recentCount]
			if selected {
				sb.WriteString(selectedStyle.Render("> " + d.Name))
			} else {
				sb.WriteString("  " + d.Name)
			}
		} else {
			// Launch action
			launchLabel := "\u25b6 launch " + agentName + " here"
			if selected {
				sb.WriteString(selectedStyle.Render("> " + launchLabel))
			} else {
				sb.WriteString("  " + launchLabel)
			}
		}
		sb.WriteString("\n")
	}

	// Footer
	var hints []string
	hints = append(hints, "enter:select", "l/\u2192:open", "h/\u2190:back")
	if len(m.availableAgents) > 1 {
		hints = append(hints, "t:toggle")
	}
	hints = append(hints, "esc:cancel")
	sb.WriteString(footerStyle.Render(" " + strings.Join(hints, "  ")))
	return sb.String()
}

func (m Model) viewBatchPrompt() string {
	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Batch Send"))
	sb.WriteString("\n\n")
	sb.WriteString(dimStyle.Render("  Use {name} for project name placeholder."))
	sb.WriteString("\n\n")
	sb.WriteString(m.batchInput.View())
	sb.WriteString("\n")
	sb.WriteString(footerStyle.Render(" Ctrl+D: send to all  Esc: cancel"))
	return sb.String()
}

func (m Model) viewHelp() string {
	help := `Key Bindings:
  j/k, arrows   Navigate project list
  l              Launch agent in a project
  t              Toggle agent type (Claude/Codex)
  s              Send prompt (opens chat view)
  f              Focus agent's terminal tab
  d              Remove project from list
  B              Batch send to multiple agents
  Enter          Expand/collapse project details
  ?              Toggle this help
  q, Ctrl+C      Quit`
	return helpStyle.Render(help)
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.view {
	case viewProjectList:
		return m.handleProjectListKey(msg)
	case viewChat:
		return m.handleChatKey(msg)
	case viewDirBrowser:
		return m.handleDirBrowserKey(msg)
	case viewBatchPrompt:
		return m.handleBatchKey(msg)
	}
	return m, nil
}

func (m Model) handleProjectListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, keys.Help):
		m.showHelp = !m.showHelp
		m.adjustScroll()
		return m, nil

	case key.Matches(msg, keys.Down):
		if m.cursor < len(m.rows)-1 {
			m.cursor++
			m.adjustScroll()
		}
		return m, nil

	case key.Matches(msg, keys.Up):
		if m.cursor > 0 {
			m.cursor--
			m.adjustScroll()
		}
		return m, nil

	case key.Matches(msg, keys.Launch):
		if !m.backendOK {
			m.statusText = "Backend unavailable"
			return m, nil
		}
		startDir := m.launchDir
		if startDir == "" && len(m.watchDirs) > 0 {
			startDir = m.watchDirs[0]
		}
		if startDir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				m.statusText = "Cannot determine home directory"
				return m, nil
			}
			startDir = home
		}
		m.showHelp = false
		return m, listDir(startDir)

	case key.Matches(msg, keys.Toggle):
		if len(m.availableAgents) <= 1 {
			return m, nil
		}
		for i, a := range m.availableAgents {
			if a == m.defaultAgent {
				m.defaultAgent = m.availableAgents[(i+1)%len(m.availableAgents)]
				break
			}
		}
		agentName := strings.ToUpper(string(m.defaultAgent)[:1]) + string(m.defaultAgent)[1:]
		m.statusText = "Default agent: " + agentName
		return m, nil

	case key.Matches(msg, keys.Send):
		return m.openChat()

	case key.Matches(msg, keys.Focus):
		return m.focusSelected()

	case key.Matches(msg, keys.Delete):
		return m.deleteSelected()

	case key.Matches(msg, keys.Batch):
		if !m.backendOK {
			m.statusText = "Backend unavailable"
			return m, nil
		}
		m.view = viewBatchPrompt
		m.batchInput.Focus()
		return m, nil

	case key.Matches(msg, keys.Enter):
		return m.openChat()
	}
	return m, nil
}

func (m Model) handleChatKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Escape):
		m.view = viewProjectList
		m.statusText = ""
		return m, nil

	case key.Matches(msg, keys.CtrlD):
		text := m.chat.input.Value()
		if strings.TrimSpace(text) == "" {
			return m, nil
		}
		m.chat.input.Reset()

		entry := model.ChatEntry{
			Timestamp: time.Now(),
			Direction: "sent",
			Text:      text,
		}
		m.chat.addEntry(entry)

		session := m.store.SessionByID(m.chatSessionID)
		if session == nil {
			m.statusText = "No agent session"
			return m, nil
		}
		session.LastSent = time.Now()
		return m, sendPrompt(m.backend, session.SessionID, text, session.ProjectDir)
	}

	// Pass to textarea
	var cmd tea.Cmd
	m.chat.input, cmd = m.chat.input.Update(msg)
	return m, cmd
}

func (m Model) handleDirBrowserKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	recentCount := m.browserRecentCount()
	// Total items: recent + dirs + 1 launch action
	total := recentCount + len(m.browserDirs) + 1
	launchIdx := total - 1

	switch {
	case key.Matches(msg, keys.Escape):
		m.view = viewProjectList
		return m, nil

	case key.Matches(msg, keys.Toggle):
		if len(m.availableAgents) <= 1 {
			return m, nil
		}
		for i, a := range m.availableAgents {
			if a == m.defaultAgent {
				m.defaultAgent = m.availableAgents[(i+1)%len(m.availableAgents)]
				break
			}
		}
		return m, nil

	case key.Matches(msg, keys.Down):
		if m.browserCursor < total-1 {
			m.browserCursor++
			m.adjustBrowserScroll()
		}
		return m, nil

	case key.Matches(msg, keys.Up):
		if m.browserCursor > 0 {
			m.browserCursor--
			m.adjustBrowserScroll()
		}
		return m, nil

	case key.Matches(msg, keys.Launch), key.Matches(msg, keys.Right):
		// l/→: descend into directory
		if m.browserCursor < recentCount {
			recent := m.browserRecentProjects()
			p := recent[m.browserCursor]
			return m, listDir(p.Dir)
		}
		dirIdx := m.browserCursor - recentCount
		if dirIdx >= 0 && dirIdx < len(m.browserDirs) {
			dir := m.browserDirs[dirIdx]
			return m, listDir(dir.Path)
		}
		return m, nil

	case key.Matches(msg, keys.Left):
		parent := filepath.Dir(m.browserPath)
		if parent == m.browserPath {
			return m, nil
		}
		return m, listDir(parent)

	case key.Matches(msg, keys.Enter):
		// Launch action at bottom
		if m.browserCursor == launchIdx {
			return m.launchFromBrowser(m.browserPath, filepath.Base(m.browserPath))
		}
		// Recent item — launch immediately
		if m.browserCursor < recentCount {
			recent := m.browserRecentProjects()
			p := recent[m.browserCursor]
			return m.launchFromBrowser(p.Dir, p.Name)
		}
		// Directory entry — descend
		dirIdx := m.browserCursor - recentCount
		if dirIdx >= 0 && dirIdx < len(m.browserDirs) {
			dir := m.browserDirs[dirIdx]
			return m, listDir(dir.Path)
		}
		return m, nil
	}
	return m, nil
}

func (m Model) launchFromBrowser(dirPath, name string) (Model, tea.Cmd) {
	m.store.AddProject(dirPath)
	_ = m.store.SaveProjects()
	m.rows = buildRows(storeAdapter{m.store})
	m.view = viewProjectList
	agentName := strings.ToUpper(string(m.defaultAgent)[:1]) + string(m.defaultAgent)[1:]
	m.statusText = fmt.Sprintf("Launching %s for %s...", agentName, name)
	return m, launchAgent(m.backend, dirPath, m.defaultAgent)
}

func (m Model) browserRecentProjects() []*model.Project {
	var recent []*model.Project
	for _, p := range m.store.Projects {
		if !p.LastLaunchedAt.IsZero() {
			recent = append(recent, p)
		}
	}
	sort.Slice(recent, func(i, j int) bool {
		return recent[i].LastLaunchedAt.After(recent[j].LastLaunchedAt)
	})
	return recent
}

func (m Model) browserRecentCount() int {
	return len(m.browserRecentProjects())
}

// browserMaxVisible returns how many selectable items fit in the browser.
// Overhead: header (2) + section labels + launch action line + blank lines + footer (2).
func (m Model) browserMaxVisible() int {
	overhead := 6 // header(2) + launch blank + launch line + footer margin + footer
	if len(m.browserRecentProjects()) > 0 {
		overhead += 2 // "recent" label + blank line after
	}
	max := m.height - overhead
	if max < 1 {
		max = 1
	}
	return max
}

func (m *Model) adjustBrowserScroll() {
	max := m.browserMaxVisible()
	if m.browserCursor < m.browserScroll {
		m.browserScroll = m.browserCursor
	} else if m.browserCursor >= m.browserScroll+max {
		m.browserScroll = m.browserCursor - max + 1
	}
	if m.browserScroll < 0 {
		m.browserScroll = 0
	}
}

func (m Model) handleBatchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Escape):
		m.view = viewProjectList
		m.batchInput.Reset()
		return m, nil

	case key.Matches(msg, keys.CtrlD):
		template := m.batchInput.Value()
		if strings.TrimSpace(template) == "" {
			return m, nil
		}
		m.batchInput.Reset()
		m.view = viewProjectList
		return m.dispatchBatch(template)
	}

	var cmd tea.Cmd
	m.batchInput, cmd = m.batchInput.Update(msg)
	return m, cmd
}

func (m Model) openChat() (Model, tea.Cmd) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return m, nil
	}
	r := m.rows[m.cursor]
	if m.chatSessionID != r.session.SessionID {
		m.chatSessionID = r.session.SessionID
		m.chat = newChatView()
		m.chat.setSize(m.width, m.height)
		m.chat.setContext(r.session.LastScreen)
	}
	m.view = viewChat
	return m, nil
}

func (m Model) focusSelected() (Model, tea.Cmd) {
	if !m.backendOK {
		m.statusText = "Backend unavailable"
		return m, nil
	}
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return m, nil
	}
	r := m.rows[m.cursor]
	return m, focusSession(m.backend, r.session.SessionID)
}

func (m Model) deleteSelected() (Model, tea.Cmd) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return m, nil
	}
	r := m.rows[m.cursor]
	// Remove sessions for this project before removing the project,
	// so they don't linger as orphans consuming tick polls.
	for _, s := range m.store.GetSessions(r.project.Dir) {
		delete(m.attentionSessions, s.SessionID)
		m.store.RemoveSession(s.SessionID)
	}
	_ = m.store.SaveSessions()
	m.store.RemoveProject(r.project.Dir)
	_ = m.store.SaveProjects()
	m.rows = buildRows(storeAdapter{m.store})
	if m.cursor >= len(m.rows) && m.cursor > 0 {
		m.cursor--
	}
	m.adjustScroll()
	m.statusText = fmt.Sprintf("Removed %s", r.project.Name)
	return m, nil
}

func (m Model) dispatchBatch(template string) (Model, tea.Cmd) {
	var cmds []tea.Cmd
	sent := 0
	for _, r := range m.rows {
		if r.session == nil {
			continue
		}
		prompt := strings.ReplaceAll(template, "{name}", r.project.Name)
		cmds = append(cmds, sendPrompt(m.backend, r.session.SessionID, prompt, r.project.Dir))
		sent++
	}
	m.statusText = fmt.Sprintf("Batch sent to %d agents", sent)
	return m, tea.Batch(cmds...)
}

func (m Model) handleSessionsRefreshed(msg SessionsRefreshedMsg) (Model, tea.Cmd) {
	if msg.Err != nil {
		m.statusText = "Session refresh failed: " + msg.Err.Error()
		return m, nil
	}

	trackedIDs := make(map[string]bool)
	for _, s := range m.store.Sessions {
		trackedIDs[s.SessionID] = true
	}

	// Update activity text from session names for tracked sessions.
	// Activity is informational only (shown in all states). Screen reads
	// are the sole authority on status — session name changes should not
	// override status since Claude updates its title even while idle.
	for _, sess := range msg.Sessions {
		if as := m.store.SessionByID(sess.ID); as != nil {
			activity := terminal.ExtractActivity(sess.Name)
			if activity != "" && activity != as.Activity {
				as.Activity = activity
				as.LastActivity = time.Now()
			}
		}
	}

	// Auto-discover untracked agent sessions
	var cmds []tea.Cmd
	liveIDs := make(map[string]bool)
	projectDirs := make([]string, len(m.store.Projects))
	for i, p := range m.store.Projects {
		projectDirs[i] = p.Dir
	}
	for _, sess := range msg.Sessions {
		liveIDs[sess.ID] = true
		if trackedIDs[sess.ID] {
			continue
		}
		agentType := terminal.DetectAgent(sess.Name)
		if agentType == "" {
			continue
		}
		// Dispatch async CWD discovery (lsof, get-var, name match)
		cmds = append(cmds, discoverAgent(m.backend, sess, agentType, m.watchDirs, projectDirs))
	}

	// Remove sessions for dead iTerm sessions.
	// Collect IDs first to avoid mutating the slice during iteration.
	var deadIDs []string
	for _, s := range m.store.Sessions {
		if !liveIDs[s.SessionID] {
			deadIDs = append(deadIDs, s.SessionID)
		}
	}
	for _, id := range deadIDs {
		delete(m.attentionSessions, id)
		m.store.RemoveSession(id)
	}

	m.rows = buildRows(storeAdapter{m.store})
	if m.cursor >= len(m.rows) && m.cursor > 0 {
		m.cursor = len(m.rows) - 1
	}
	m.adjustScroll()

	if cmd := m.ensureSpinner(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m Model) handleAgentDiscovered(msg AgentDiscoveredMsg) (Model, tea.Cmd) {
	if msg.Dir == "" {
		return m, nil
	}
	// Skip if this session was already tracked (race with multiple ticks)
	if m.store.SessionByID(msg.SessionID) != nil {
		return m, nil
	}

	p := m.store.AddProject(msg.Dir)
	if p != nil {
		_ = m.store.SaveProjects()
	}

	as := &model.AgentSession{
		ProjectDir: msg.Dir,
		SessionID:  msg.SessionID,
		Type:       msg.AgentType,
		Status:     model.StatusWorking,
	}
	m.store.SetSession(as)
	_ = m.store.SaveSessions()

	m.rows = buildRows(storeAdapter{m.store})

	// Start monitoring
	logPath := filepath.Join(m.monitorDir, filepath.Base(msg.Dir)+".log")
	cmds := []tea.Cmd{startMonitor(m.backend, msg.SessionID, logPath,
		monitorPatterns, msg.Dir)}
	if cmd := m.ensureSpinner(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func matchProjectDir(sessionName string, projects []*model.Project) string {
	for _, p := range projects {
		if strings.Contains(strings.ToLower(sessionName), strings.ToLower(p.Name)) {
			return p.Dir
		}
	}
	return ""
}

func (m Model) handleAgentLaunched(msg AgentLaunchedMsg) (Model, tea.Cmd) {
	if msg.Err != nil {
		m.statusText = "Launch failed: " + msg.Err.Error()
		return m, nil
	}

	m.store.AddProject(msg.ProjectDir)
	proj := m.store.FindProject(msg.ProjectDir)
	if proj != nil {
		proj.LastLaunchedAt = time.Now()
	}
	_ = m.store.SaveProjects()

	as := &model.AgentSession{
		ProjectDir: msg.ProjectDir,
		SessionID:  msg.SessionID,
		Type:       msg.AgentType,
		Status:     model.StatusWorking,
		LastSent:   time.Now(),
	}
	m.store.SetSession(as)
	_ = m.store.SaveSessions()

	m.rows = buildRows(storeAdapter{m.store})
	m.statusText = fmt.Sprintf("Launched %s for %s", msg.AgentType, filepath.Base(msg.ProjectDir))

	// Start monitoring
	logPath := filepath.Join(m.monitorDir, filepath.Base(msg.ProjectDir)+".log")
	var cmds []tea.Cmd
	cmds = append(cmds, startMonitor(m.backend, msg.SessionID, logPath,
		monitorPatterns, msg.ProjectDir))
	cmds = append(cmds, func() tea.Msg {
		if cb, ok := m.backend.(interface{ Invalidate() }); ok {
			cb.Invalidate()
		}
		return nil
	})
	if cmd := m.ensureSpinner(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m Model) handleStatusUpdated(msg StatusUpdatedMsg) (Model, tea.Cmd) {
	as := m.store.GetSession(msg.ProjectDir)
	if as == nil {
		return m, nil
	}
	prevStatus := as.Status
	if msg.Status != "" {
		as.Status = msg.Status
		as.LastActivity = time.Now()
	}
	if msg.Attention != "" {
		as.Attention = msg.Attention
	}

	// Add received text to chat if viewing this session
	if m.view == viewChat && m.chatSessionID == as.SessionID && msg.Attention != "" {
		entry := model.ChatEntry{
			Timestamp: time.Now(),
			Direction: "received",
			Text:      msg.Attention,
		}
		m.chat.addEntry(entry)
	}

	m.rows = buildRows(storeAdapter{m.store})

	// Bell + attention highlight when status changes to needs_input
	if msg.Status == model.StatusNeedsInput && prevStatus != model.StatusNeedsInput {
		if m.attentionSessions == nil {
			m.attentionSessions = make(map[string]time.Time)
		}
		m.attentionSessions[as.SessionID] = time.Now()
		cmds := []tea.Cmd{bellCmd()}
		if cmd := m.ensureSpinner(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	}
	// Clear attention when status moves away from needs_input
	if prevStatus == model.StatusNeedsInput && msg.Status != model.StatusNeedsInput && msg.Status != "" {
		as.Attention = ""
		m.statusText = ""
		delete(m.attentionSessions, as.SessionID)
	}

	if cmd := m.ensureSpinner(); cmd != nil {
		return m, cmd
	}
	return m, nil
}

func (m Model) handleMonitorStarted(msg MonitorStartedMsg) (Model, tea.Cmd) {
	if msg.Err != nil {
		m.statusText = "Monitor failed: " + msg.Err.Error()
		return m, nil
	}
	as := m.store.GetSession(msg.ProjectDir)
	if as != nil {
		as.MonitorPID = msg.PID
		as.MonitorLog = msg.LogPath
	}
	m.monitorPIDs = append(m.monitorPIDs, msg.PID)
	return m, nil
}

func (m Model) handleScreenRead(msg ScreenReadMsg) (Model, tea.Cmd) {
	if msg.Err != nil {
		return m, nil
	}
	as := m.store.SessionByID(msg.SessionID)
	if as == nil {
		return m, nil
	}
	as.ScreenChecked = true
	// Strip null bytes from screen content (it2 read-screen returns \x00 for spaces)
	content := strings.ReplaceAll(msg.Content, "\x00", " ")
	screenChanged := content != as.LastScreen
	as.LastScreen = content
	status, matchLine := terminal.ClassifyScreen(content)

	if m.debugLog != nil {
		proj := filepath.Base(msg.ProjectDir)
		escaped := strings.ReplaceAll(content, "\n", "\\n")
		m.debugLog.Printf("[screen] %s prev=%s new=%s changed=%v match=%q content=%q",
			proj, as.Status, status, screenChanged, matchLine, escaped)
	}

	if status == "" {
		// Screen changed but no pattern match while in needs_input →
		// the agent moved on, transition to working
		if screenChanged && as.Status == model.StatusNeedsInput {
			status = model.StatusWorking
		} else if !screenChanged && as.Status == model.StatusWorking && isAllBlank(content) {
			// Consecutive blank screen reads while "working" — it2 can't
			// read this session. No evidence the agent is working.
			status = model.StatusIdle
		} else {
			return m, nil
		}
	}

	// Skip stale screen reads (identical content)
	if !screenChanged && status == as.Status {
		return m, nil
	}
	// Screen reads are the sole authority on status. The bottom-region
	// anchoring in ClassifyScreen prevents false matches from scrollback.

	prevStatus := as.Status
	as.Status = status
	as.LastActivity = time.Now()
	if status == model.StatusNeedsInput {
		as.Attention = matchLine
	}

	// Add to chat if viewing this session
	if m.view == viewChat && m.chatSessionID == msg.SessionID && status == model.StatusNeedsInput && as.Attention != "" {
		m.chat.addEntry(model.ChatEntry{
			Timestamp: time.Now(),
			Direction: "received",
			Text:      as.Attention,
		})
	}

	m.rows = buildRows(storeAdapter{m.store})

	// Bell on needs_input transition
	if status == model.StatusNeedsInput && prevStatus != model.StatusNeedsInput {
		if m.attentionSessions == nil {
			m.attentionSessions = make(map[string]time.Time)
		}
		m.attentionSessions[msg.SessionID] = time.Now()
		cmds := []tea.Cmd{bellCmd()}
		if cmd := m.ensureSpinner(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	}
	if prevStatus == model.StatusNeedsInput && status != model.StatusNeedsInput {
		as.Attention = ""
		m.statusText = ""
		delete(m.attentionSessions, msg.SessionID)
	}

	if cmd := m.ensureSpinner(); cmd != nil {
		return m, cmd
	}
	return m, nil
}

func (m Model) handleTick() (Model, tea.Cmd) {
	var cmds []tea.Cmd
	cmds = append(cmds, tickCmd())

	if m.backendOK {
		cmds = append(cmds, refreshSessions(m.backend))

		// Read screen for status detection on all tracked sessions
		for _, s := range m.store.Sessions {
			cmds = append(cmds, readScreen(m.backend, s.SessionID, s.ProjectDir))
		}
	}

	if cmd := m.ensureSpinner(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m Model) updateChat(msg tea.Msg) (Model, tea.Cmd) {
	var vpCmd, taCmd tea.Cmd
	m.chat.viewport, vpCmd = m.chat.viewport.Update(msg)
	m.chat.input, taCmd = m.chat.input.Update(msg)
	return m, tea.Batch(vpCmd, taCmd)
}

func (m Model) updateBatch(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.batchInput, cmd = m.batchInput.Update(msg)
	return m, cmd
}

// Cleanup kills all monitor processes.
func (m Model) Cleanup() {
	for _, pid := range m.monitorPIDs {
		if pid > 0 {
			_ = syscall.Kill(pid, syscall.SIGTERM)
		}
	}
}

func (m Model) hasActiveAnimations() bool {
	for _, s := range m.store.Sessions {
		if s.Status == model.StatusWorking {
			return true
		}
	}
	return len(m.attentionSessions) > 0
}

func (m *Model) ensureSpinner() tea.Cmd {
	if m.spinnerActive || !m.hasActiveAnimations() {
		return nil
	}
	m.spinnerActive = true
	return spinnerTickCmd()
}

// EnableDebugLog sets up debug logging to the given file path.
func (m *Model) EnableDebugLog(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	m.debugLog = log.New(f, "", log.Ltime|log.Lmicroseconds)
	return nil
}

// EnsureMonitorDir creates the monitor directory if needed.
// isAllBlank returns true if content contains only whitespace/newlines.
func isAllBlank(content string) bool {
	return strings.TrimSpace(content) == ""
}

func EnsureMonitorDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}
