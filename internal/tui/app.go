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
	"github.com/sethdeckard/atria/internal/config"
	"github.com/sethdeckard/atria/internal/model"
	"github.com/sethdeckard/atria/internal/terminal"
)

// monitorPatterns are the regex patterns for monitor output.
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
	viewTerminal
	viewSettings
	viewSetup
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
	sortCol      sortColumn
	sortDesc     bool

	// Chat view
	chat          chatView
	chatSessionID string // session ID for active chat

	// Stream panel
	streamOpen bool

	// Spinner & attention
	spinnerFrame      int
	spinnerActive     bool
	attentionSessions map[string]time.Time // session IDs needing attention, with timestamp

	// Agent type
	availableAgents []model.AgentType
	defaultAgent    model.AgentType

	// Settings
	statusInfo      StatusInfo
	settingsItems   []settingsItem
	settingsCursor  int
	settingsEditing bool
	settingsEditBuf string
	cfg             *config.Config
	configPath      string
	ptyClient       terminal.Backend
	settingsDirPick bool // true when dir browser is for settings (add watch dir)

	// Setup wizard
	setupItems      []settingsItem
	setupCursor     int
	setupStep       int
	setupEditing    bool
	setupEditBuf    string
	setupDirPick    bool      // true when dir browser is for setup (add watch dir)
	setupReturnView viewState // view to return to when wizard exits

	// Directory browser
	browserDirs   []DirBrowserItem
	browserCursor int
	browserScroll int
	browserPath   string

	// Batch prompt
	batchInput textarea.Model

	// Terminal view (PTY backend)
	termView      termView
	termSessionID string

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

	rows := buildRows(storeAdapter{store})
	sortRows(rows, sortByAgent, false)

	return Model{
		watchDirs:       watchDirs,
		monitorDir:      monitorDir,
		launchDir:       launchDir,
		backend:         backend,
		store:           store,
		rows:            rows,
		chat:            newChatView(),
		batchInput:      ba,
		availableAgents: available,
		defaultAgent:    defaultAgent,
	}
}

// SetStatusInfo sets the backend status info for the settings screen.
func (m *Model) SetStatusInfo(info StatusInfo) {
	m.statusInfo = info
}

// SetConfig sets the config and config path for the settings screen.
func (m *Model) SetConfig(cfg *config.Config, path string) {
	m.cfg = cfg
	m.configPath = path
}

// SetPTYClient sets the PTY client reference for integration toggling.
func (m *Model) SetPTYClient(pty terminal.Backend) {
	m.ptyClient = pty
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
		if r, ok := m.backend.(interface{ Resize(int, int) }); ok {
			r.Resize(msg.Width, msg.Height)
		}
		m.termView.width = msg.Width
		m.termView.height = msg.Height
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

	case IntegrationToggledMsg:
		if msg.Err != nil {
			m.statusText = "Toggle failed: " + msg.Err.Error()
		} else {
			// Remap session IDs if PTY was demoted from primary to integration.
			// This keeps store, attention map, and chat/term references consistent.
			if len(msg.RemappedIDs) > 0 {
				for _, s := range m.store.Sessions {
					if newID, ok := msg.RemappedIDs[s.SessionID]; ok {
						oldID := s.SessionID
						s.SessionID = newID
						if t, has := m.attentionSessions[oldID]; has {
							delete(m.attentionSessions, oldID)
							m.attentionSessions[newID] = t
						}
						if m.chatSessionID == oldID {
							m.chatSessionID = newID
						}
						if m.termSessionID == oldID {
							m.termSessionID = newID
						}
					}
				}
			}
			// Update statusInfo.
			for i, bs := range m.statusInfo.Backends {
				if bs.Name == msg.Name {
					m.statusInfo.Backends[i] = msg.Status
					break
				}
			}
			// Invalidate cache so next tick picks up new sessions.
			if cb, ok := m.backend.(interface{ Invalidate() }); ok {
				cb.Invalidate()
			}
			// Rebuild settings/setup items.
			if m.cfg != nil {
				m.settingsItems = buildSettingsItems(m.statusInfo, m.cfg, m.availableAgents)
				if m.view == viewSetup {
					m.rebuildSetupItems()
				}
			}
		}
		return m, nil

	case ConfigSavedMsg:
		if msg.Err != nil {
			m.statusText = "Config save failed: " + msg.Err.Error()
			if msg.Rollback != nil {
				msg.Rollback(&m)
				m.settingsItems = buildSettingsItems(m.statusInfo, m.cfg, m.availableAgents)
				if m.view == viewSetup {
					m.rebuildSetupItems()
				}
			}
		}
		return m, nil

	case termRefreshMsg:
		if m.view != viewTerminal {
			return m, nil
		}
		content, err := m.backend.ReadScreen(m.termSessionID, m.height)
		if err == nil {
			m.termView.content = content
		}
		return m, termRefreshCmd()
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
	case viewTerminal:
		content = m.termView.render()
	case viewSettings:
		content = renderSettings(m.settingsItems, m.settingsCursor, m.settingsEditing, m.settingsEditBuf, m.width, m.height)
	case viewSetup:
		content = renderSetupWithDescription(m.setupStep, m.setupItems, m.setupCursor, m.setupEditing, m.setupEditBuf, m.statusInfo, m.cfg, m.width, m.height)
	}

	if m.showHelp {
		content = m.viewHelp() + "\n\n" + content
	}

	// Pad output to exactly m.height lines so Bubble Tea's alternate screen
	// doesn't leave ghost lines when the output shrinks between frames
	// (e.g. statusText appearing/disappearing, status transitions).
	lines := strings.Count(content, "\n")
	if lines < m.height-1 {
		content += strings.Repeat("\n", m.height-1-lines)
	}

	return content
}

func (m Model) viewProjectList() string {
	var sb strings.Builder

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

	canSetup := m.canSetup()
	list := renderProjectList(m.rows, m.cursor, m.width, m.spinnerFrame, m.attentionSessions, m.defaultAgent, m.availableAgents, maxRows, scrollOffset, m.sortCol, m.sortDesc, canSetup)
	sb.WriteString(list)

	if m.streamOpen {
		var session *model.AgentSession
		var projectName, projectDir string
		if m.cursor >= 0 && m.cursor < len(m.rows) {
			session = m.rows[m.cursor].session
			projectName = m.rows[m.cursor].displayName
			projectDir = contractHome(m.rows[m.cursor].project.Dir)
		}
		panelH := streamPanelHeight(m.height)
		sb.WriteString(renderStreamPanel(session, projectName, projectDir, m.width, panelH))
	}

	var selected *projectRow
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		selected = &m.rows[m.cursor]
	}
	sb.WriteString("\n")
	if !m.streamOpen && selected != nil {
		dirLine := " " + contractHome(selected.project.Dir)
		if lipgloss.Width(dirLine) > m.width && m.width > 2 {
			// Truncate by runes to handle non-ASCII correctly
			runes := []rune(dirLine)
			for lipgloss.Width(string(runes)) > m.width-1 {
				runes = runes[:len(runes)-1]
			}
			dirLine = string(runes) + "\u2026"
		}
		sb.WriteString(dimStyle.Render(dirLine))
		sb.WriteString("\n")
	}
	sb.WriteString(renderFooter(len(m.rows), selected, m.defaultAgent, len(m.availableAgents) > 1, m.streamOpen, m.width))

	if m.statusText != "" {
		sb.WriteString("\n")
		sb.WriteString(statusBarStyle.Render(m.statusText))
	}

	return sb.String()
}

// renderStreamPanel renders the live screen output panel for the selected agent.
func renderStreamPanel(session *model.AgentSession, projectName, projectDir string, width, height int) string {
	var sb strings.Builder

	// Box dimensions: 1 char indent, box is width-1 chars wide
	boxWidth := width - 1
	if boxWidth < 6 {
		boxWidth = 6
	}
	innerWidth := boxWidth - 4 // "│ " + content + " │"
	if innerWidth < 1 {
		innerWidth = 1
	}

	sb.WriteString("\n")

	// Top border with header
	if session != nil && projectName != "" {
		agentType := strings.ToUpper(string(session.Type)[:1]) + string(session.Type)[1:]
		leftText := " " + projectName + " \u00b7 " + agentType + " \u00b7 " + projectDir + " "
		rightText := " v:close "
		maxLeftWidth := boxWidth - 2 - lipgloss.Width(rightText) - 1 // -2 for ┌┐, -1 for min fill
		if leftWidth := lipgloss.Width(leftText); leftWidth > maxLeftWidth {
			// Truncate path first, then project name if still too wide
			prefix := " " + projectName + " \u00b7 " + agentType + " \u00b7 "
			availForDir := maxLeftWidth - lipgloss.Width(prefix) - 1 // -1 for trailing space
			if availForDir >= 4 {
				// Truncate dir from the left: …/tail (rune-safe)
				dirRunes := []rune(projectDir)
				truncDir := ""
				for i := len(dirRunes) - 1; i >= 0; i-- {
					candidate := "\u2026" + string(dirRunes[i]) + truncDir
					if lipgloss.Width(candidate) > availForDir {
						break
					}
					truncDir = string(dirRunes[i]) + truncDir
				}
				leftText = prefix + "\u2026" + truncDir + " "
			}
			// If path truncation wasn't possible or leftText is still too wide,
			// drop path and truncate name to guarantee fit.
			if lipgloss.Width(leftText) > maxLeftWidth {
				truncName := ""
				for _, r := range projectName {
					candidate := " " + truncName + string(r) + "\u2026 \u00b7 " + agentType + " "
					if lipgloss.Width(candidate) > maxLeftWidth {
						break
					}
					truncName += string(r)
				}
				leftText = " " + truncName + "\u2026 \u00b7 " + agentType + " "
				// Final clamp: if even "…· Type " overflows, hard-truncate
				if lipgloss.Width(leftText) > maxLeftWidth {
					leftText = " "
				}
			}
		}
		fillLen := boxWidth - 2 - lipgloss.Width(leftText) - lipgloss.Width(rightText)
		if fillLen < 0 {
			fillLen = 0
		}
		topBorder := "\u250c" + leftText + strings.Repeat("\u2500", fillLen) + rightText + "\u2510"
		sb.WriteString(dimStyle.Render(" " + topBorder))
	} else {
		topBorder := "\u250c" + strings.Repeat("\u2500", boxWidth-2) + "\u2510"
		sb.WriteString(dimStyle.Render(" " + topBorder))
	}
	sb.WriteString("\n")

	contentLines := height - 2 // account for top/bottom borders
	if contentLines < 1 {
		contentLines = 1
	}

	if session == nil || strings.TrimSpace(session.LastScreen) == "" {
		// Placeholder
		placeholder := "no output"
		pad := innerWidth - lipgloss.Width(placeholder)
		if pad < 0 {
			pad = 0
		}
		sb.WriteString(dimStyle.Render(" \u2502") + " " + dimStyle.Render(placeholder) + strings.Repeat(" ", pad) + " " + dimStyle.Render("\u2502"))
		sb.WriteString("\n")
		for i := 1; i < contentLines; i++ {
			sb.WriteString(dimStyle.Render(" \u2502") + strings.Repeat(" ", innerWidth+2) + dimStyle.Render("\u2502"))
			sb.WriteString("\n")
		}
	} else {
		// Split screen content, trim trailing blank lines, take from bottom
		lines := strings.Split(session.LastScreen, "\n")
		for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
			lines = lines[:len(lines)-1]
		}
		if len(lines) > contentLines {
			lines = lines[len(lines)-contentLines:]
		}
		for _, line := range lines {
			lineWidth := lipgloss.Width(line)
			if lineWidth > innerWidth {
				// Truncate to fit (rune-safe)
				truncated := ""
				for _, r := range line {
					if lipgloss.Width(truncated+string(r)) > innerWidth-1 {
						truncated += "\u2026"
						break
					}
					truncated += string(r)
				}
				line = truncated
				lineWidth = lipgloss.Width(line)
			}
			pad := innerWidth - lineWidth
			if pad < 0 {
				pad = 0
			}
			sb.WriteString(dimStyle.Render(" \u2502") + " " + line + strings.Repeat(" ", pad) + " " + dimStyle.Render("\u2502"))
			sb.WriteString("\n")
		}
		// Pad remaining lines
		for i := len(lines); i < contentLines; i++ {
			sb.WriteString(dimStyle.Render(" \u2502") + strings.Repeat(" ", innerWidth+2) + dimStyle.Render("\u2502"))
			sb.WriteString("\n")
		}
	}

	// Bottom border
	bottomBorder := "\u2514" + strings.Repeat("\u2500", boxWidth-2) + "\u2518"
	sb.WriteString(dimStyle.Render(" " + bottomBorder))

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
	if m.streamOpen {
		overhead += streamPanelHeight(m.height) + 1 // +1 for spacer line above top separator
	} else if len(m.rows) > 0 {
		overhead++ // directory path line above footer
	}
	max := m.height - overhead
	if max < 1 {
		max = 1
	}
	return max
}

// streamPanelHeight returns the height of the stream panel in lines.
func streamPanelHeight(termHeight int) int {
	h := termHeight * 2 / 5
	if h < 5 {
		h = 5
	}
	if h > 25 {
		h = 25
	}
	return h
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
	return m.chat.render(session, project, m.width, m.spinnerFrame)
}

func (m Model) viewDirBrowser() string {
	var sb strings.Builder
	agentName := agentDisplayName(m.defaultAgent)

	// Header
	headerText := "launch " + agentName
	var right string
	if m.settingsDirPick || m.setupDirPick {
		headerText = "select watch directory"
		right = brandingStyle.Render("atria  ")
	} else {
		right = dimStyle.Render(contractHome(m.browserPath) + "  ")
	}
	left := titleStyle.Render("  " + headerText)
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
	if m.settingsDirPick || m.setupDirPick {
		recent = nil
	}
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
			// Action button
			launchLabel := "\u25b6 launch " + agentName + " here"
			if m.settingsDirPick || m.setupDirPick {
				launchLabel = "\u25b6 add this directory"
			}
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
	hints = append(hints, "enter:select")
	if !m.settingsDirPick && !m.setupDirPick {
		hints = append(hints, "l/\u2192:open")
	}
	hints = append(hints, "h/\u2190:back")
	if !m.settingsDirPick && !m.setupDirPick && len(m.availableAgents) > 1 {
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
  v              Toggle agent screen stream
  l              Launch agent in a project
  t              Toggle agent type (Claude Code/Codex/OpenCode)
  s              Cycle sort column
  S              Reverse sort direction
  f              Focus agent's terminal tab
  d              Remove project from list
  B              Batch send to multiple agents
  Enter          Open chat view
  Ctrl+\         Return from terminal view
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
	case viewTerminal:
		return m.handleTerminalKey(msg)
	case viewSettings:
		return m.handleSettingsKey(msg)
	case viewSetup:
		return m.handleSetupKey(msg)
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
		m.statusText = "Default agent: " + agentDisplayName(m.defaultAgent)
		return m, nil

	case key.Matches(msg, keys.Sort):
		m.sortCol = (m.sortCol + 1) % sortColumnCount
		sortRows(m.rows, m.sortCol, m.sortDesc)
		m.cursor = 0
		m.scrollOffset = 0
		return m, nil

	case key.Matches(msg, keys.SortReverse):
		if len(m.rows) == 0 && m.canSetup() {
			m.setupStep = 0
			m.setupEditing = false
			m.setupEditBuf = ""
			m.setupReturnView = viewProjectList
			m.setupItems = buildSetupStepItems(0, m.statusInfo, m.cfg, m.availableAgents)
			m.setupCursor = firstSelectableItem(m.setupItems)
			m.view = viewSetup
			m.showHelp = false
			return m, nil
		}
		m.sortDesc = !m.sortDesc
		sortRows(m.rows, m.sortCol, m.sortDesc)
		m.cursor = 0
		m.scrollOffset = 0
		return m, nil

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

	case key.Matches(msg, keys.Stream):
		m.streamOpen = !m.streamOpen
		m.adjustScroll()
		return m, nil

	case key.Matches(msg, keys.Enter):
		return m.openChat()

	case key.Matches(msg, keys.Info):
		if m.cfg == nil {
			m.statusText = "Config not available"
			return m, nil
		}
		m.settingsItems = buildSettingsItems(m.statusInfo, m.cfg, m.availableAgents)
		m.settingsCursor = m.firstEditableSettingsItem()
		m.settingsEditing = false
		m.settingsEditBuf = ""
		m.view = viewSettings
		m.showHelp = false
		return m, nil
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
		if m.setupDirPick {
			m.setupDirPick = false
			m.setupItems = buildSetupStepItems(m.setupStep, m.statusInfo, m.cfg, m.availableAgents)
			m.view = viewSetup
		} else if m.settingsDirPick {
			m.settingsDirPick = false
			m.settingsItems = buildSettingsItems(m.statusInfo, m.cfg, m.availableAgents)
			m.view = viewSettings
		} else {
			m.view = viewProjectList
		}
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
		// Setup: add watch dir
		if m.setupDirPick {
			if m.browserCursor == launchIdx {
				return m.addSetupWatchDirFromBrowser(m.browserPath)
			}
			if m.browserCursor < recentCount {
				recent := m.browserRecentProjects()
				p := recent[m.browserCursor]
				return m.addSetupWatchDirFromBrowser(p.Dir)
			}
			dirIdx := m.browserCursor - recentCount
			if dirIdx >= 0 && dirIdx < len(m.browserDirs) {
				dir := m.browserDirs[dirIdx]
				return m, listDir(dir.Path)
			}
			return m, nil
		}

		// Settings: add watch dir
		if m.settingsDirPick {
			if m.browserCursor == launchIdx {
				return m.addWatchDirFromBrowser(m.browserPath)
			}
			if m.browserCursor < recentCount {
				recent := m.browserRecentProjects()
				p := recent[m.browserCursor]
				return m.addWatchDirFromBrowser(p.Dir)
			}
			dirIdx := m.browserCursor - recentCount
			if dirIdx >= 0 && dirIdx < len(m.browserDirs) {
				dir := m.browserDirs[dirIdx]
				return m, listDir(dir.Path)
			}
			return m, nil
		}

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

func (m Model) addWatchDirFromBrowser(dirPath string) (Model, tea.Cmd) {
	m.settingsDirPick = false
	// Add if not already present.
	prevWatchDirs := make([]string, len(m.cfg.WatchDirs))
	copy(prevWatchDirs, m.cfg.WatchDirs)
	if !containsString(m.cfg.WatchDirs, dirPath) {
		m.cfg.WatchDirs = append(m.cfg.WatchDirs, dirPath)
		m.watchDirs = m.cfg.WatchDirs
	}
	m.settingsItems = buildSettingsItems(m.statusInfo, m.cfg, m.availableAgents)
	m.view = viewSettings
	return m, saveConfig(m.cfg, m.configPath, func(rm *Model) {
		rm.cfg.WatchDirs = prevWatchDirs
		rm.watchDirs = prevWatchDirs
	})
}

func (m Model) addSetupWatchDirFromBrowser(dirPath string) (Model, tea.Cmd) {
	m.setupDirPick = false
	prevWatchDirs := make([]string, len(m.cfg.WatchDirs))
	copy(prevWatchDirs, m.cfg.WatchDirs)
	if !containsString(m.cfg.WatchDirs, dirPath) {
		m.cfg.WatchDirs = append(m.cfg.WatchDirs, dirPath)
		m.watchDirs = m.cfg.WatchDirs
	}
	m.setupItems = buildSetupStepItems(m.setupStep, m.statusInfo, m.cfg, m.availableAgents)
	m.view = viewSetup
	return m, saveConfig(m.cfg, m.configPath, func(rm *Model) {
		rm.cfg.WatchDirs = prevWatchDirs
		rm.watchDirs = prevWatchDirs
	})
}

func (m Model) launchFromBrowser(dirPath, name string) (Model, tea.Cmd) {
	m.store.AddProject(dirPath)
	_ = m.store.SaveProjects()
	m.rows = buildRows(storeAdapter{m.store})
	sortRows(m.rows, m.sortCol, m.sortDesc)
	m.view = viewProjectList
	m.statusText = fmt.Sprintf("Launching %s for %s...", agentDisplayName(m.defaultAgent), name)
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
	if m.settingsDirPick || m.setupDirPick {
		return 0
	}
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

func (m Model) handleTerminalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Ctrl+\ returns to the project list
	if key.Matches(msg, keys.CtrlBackslash) {
		m.view = viewProjectList
		m.termSessionID = ""
		return m, nil
	}

	// Forward all other keystrokes to the PTY
	b := keyToBytes(msg)
	if len(b) > 0 {
		m.backend.SendText(m.termSessionID, string(b))
	}
	return m, nil
}

func (m Model) handleSettingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.settingsEditing {
		return m.handleSettingsEditKey(msg)
	}

	switch {
	case key.Matches(msg, keys.Escape):
		m.view = viewProjectList
		return m, nil

	case key.Matches(msg, keys.Quit):
		m.view = viewProjectList
		return m, nil

	case key.Matches(msg, keys.Down):
		m.settingsCursor = m.nextSettingsItem(m.settingsCursor, 1)
		return m, nil

	case key.Matches(msg, keys.Up):
		m.settingsCursor = m.nextSettingsItem(m.settingsCursor, -1)
		return m, nil

	case key.Matches(msg, keys.Enter):
		if m.settingsCursor < 0 || m.settingsCursor >= len(m.settingsItems) {
			return m, nil
		}
		item := m.settingsItems[m.settingsCursor]
		switch item.itemType {
		case "toggle":
			return m.toggleSettingsIntegration(item)
		case "choice":
			return m.cycleSettingsChoice(item)
		case "string", "number":
			m.settingsEditing = true
			m.settingsEditBuf = item.value
			return m, nil
		case "action":
			if item.key == "add_watch_dir" {
				return m.openSettingsDirPicker()
			}
		}
		return m, nil

	case key.Matches(msg, keys.Add):
		return m.openSettingsDirPicker()

	case key.Matches(msg, keys.SortReverse):
		m.setupStep = 0
		m.setupEditing = false
		m.setupEditBuf = ""
		m.setupReturnView = viewSettings
		m.setupItems = buildSetupStepItems(0, m.statusInfo, m.cfg, m.availableAgents)
		m.setupCursor = firstSelectableItem(m.setupItems)
		m.view = viewSetup
		return m, nil

	case key.Matches(msg, keys.Delete):
		if m.settingsCursor < 0 || m.settingsCursor >= len(m.settingsItems) {
			return m, nil
		}
		item := m.settingsItems[m.settingsCursor]
		if item.itemType == "list-entry" && item.key == "watch_dirs" {
			// Remove this watch dir from config
			prevWatchDirs := make([]string, len(m.cfg.WatchDirs))
			copy(prevWatchDirs, m.cfg.WatchDirs)
			dir := item.value
			filtered := make([]string, 0, len(m.cfg.WatchDirs))
			for _, d := range m.cfg.WatchDirs {
				if d != dir {
					filtered = append(filtered, d)
				}
			}
			m.cfg.WatchDirs = filtered
			m.watchDirs = m.cfg.WatchDirs
			m.settingsItems = buildSettingsItems(m.statusInfo, m.cfg, m.availableAgents)
			if m.settingsCursor >= len(m.settingsItems) {
				m.settingsCursor = len(m.settingsItems) - 1
			}
			// Skip to non-header
			for m.settingsCursor < len(m.settingsItems) && m.settingsItems[m.settingsCursor].itemType == "header" {
				m.settingsCursor++
			}
			return m, saveConfig(m.cfg, m.configPath, func(rm *Model) {
				rm.cfg.WatchDirs = prevWatchDirs
				rm.watchDirs = prevWatchDirs
			})
		}
		return m, nil
	}
	return m, nil
}

func (m Model) handleSettingsEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEscape:
		m.settingsEditing = false
		m.settingsEditBuf = ""
		return m, nil

	case tea.KeyEnter:
		item := m.settingsItems[m.settingsCursor]
		m.settingsEditing = false
		val := m.settingsEditBuf
		m.settingsEditBuf = ""

		prevCols := m.cfg.PtyCols
		prevRows := m.cfg.PtyRows
		prevTmuxSession := m.cfg.TmuxSession

		switch item.key {
		case "pty_cols":
			n := parsePositiveInt(val, 120)
			m.cfg.PtyCols = n
		case "pty_rows":
			n := parsePositiveInt(val, 40)
			m.cfg.PtyRows = n
		case "tmux_session":
			if val != "" {
				m.cfg.TmuxSession = val
			}
		}

		// Apply PTY dimension changes to the live backend.
		if m.cfg.PtyCols != prevCols || m.cfg.PtyRows != prevRows {
			if r, ok := m.ptyClient.(interface{ Resize(int, int) }); ok {
				r.Resize(m.cfg.PtyCols, m.cfg.PtyRows)
			}
		}

		m.settingsItems = buildSettingsItems(m.statusInfo, m.cfg, m.availableAgents)
		return m, saveConfig(m.cfg, m.configPath, func(rm *Model) {
			rm.cfg.PtyCols = prevCols
			rm.cfg.PtyRows = prevRows
			rm.cfg.TmuxSession = prevTmuxSession
			// Revert live PTY dimensions on save failure.
			if r, ok := rm.ptyClient.(interface{ Resize(int, int) }); ok {
				r.Resize(prevCols, prevRows)
			}
		})

	case tea.KeyBackspace:
		if len(m.settingsEditBuf) > 0 {
			m.settingsEditBuf = m.settingsEditBuf[:len(m.settingsEditBuf)-1]
		}
		return m, nil

	default:
		if len(msg.Runes) > 0 {
			m.settingsEditBuf += string(msg.Runes)
		}
		return m, nil
	}
}

// canSetup returns true when no integrations are enabled and at least one
// integration environment is detected (TMUX set or TERM_PROGRAM == iTerm.app).
// rebuildSetupItems rebuilds the setup item list for the current step,
// cancels any in-progress edit, and clamps the cursor.
func (m *Model) rebuildSetupItems() {
	m.setupItems = buildSetupStepItems(m.setupStep, m.statusInfo, m.cfg, m.availableAgents)
	m.setupEditing = false
	m.setupEditBuf = ""
	if m.setupCursor >= len(m.setupItems) {
		m.setupCursor = len(m.setupItems) - 1
	}
	if m.setupCursor < 0 {
		m.setupCursor = 0
	}
}

// canSetup returns true when the empty state should offer the setup wizard.
func (m *Model) canSetup() bool {
	return m.cfg != nil
}

func (m *Model) firstEditableSettingsItem() int {
	for i, item := range m.settingsItems {
		if item.itemType != "header" {
			return i
		}
	}
	return 0
}

func (m *Model) nextSettingsItem(cur, dir int) int {
	n := len(m.settingsItems)
	if n == 0 {
		return 0
	}
	next := cur + dir
	for next >= 0 && next < n {
		if m.settingsItems[next].itemType != "header" {
			return next
		}
		next += dir
	}
	return cur
}

func (m Model) toggleSettingsIntegration(item settingsItem) (Model, tea.Cmd) {
	// Find the backend status.
	var bs BackendStatus
	for _, b := range m.statusInfo.Backends {
		if b.Name == item.key {
			bs = b
			break
		}
	}
	enable := !bs.Enabled

	// Get the composite backend.
	var composite *terminal.CompositeBackend
	if cb, ok := m.backend.(*terminal.CachedBackend); ok {
		composite, _ = cb.Inner().(*terminal.CompositeBackend)
	}
	if composite == nil {
		m.statusText = "Cannot modify backend"
		return m, nil
	}

	return m, toggleIntegration(item.key, enable, m.cfg, m.configPath, composite, m.ptyClient)
}

func (m Model) openSettingsDirPicker() (Model, tea.Cmd) {
	startDir := ""
	if len(m.watchDirs) > 0 {
		startDir = m.watchDirs[0]
	}
	if startDir == "" {
		home, _ := os.UserHomeDir()
		startDir = home
	}
	m.settingsEditing = false
	m.settingsDirPick = true
	return m, listDir(startDir)
}

func (m Model) cycleSettingsChoice(item settingsItem) (Model, tea.Cmd) {
	if item.key != "default_agent" {
		return m, nil
	}
	agents := m.availableAgents
	if len(agents) == 0 {
		return m, nil
	}
	cur := item.value
	nextAgent := agents[0]
	for i, a := range agents {
		if agentDisplayName(a) == cur {
			nextAgent = agents[(i+1)%len(agents)]
			break
		}
	}
	prevAgent := m.defaultAgent
	prevCfgAgent := m.cfg.DefaultAgent
	m.defaultAgent = nextAgent
	m.cfg.DefaultAgent = string(nextAgent)
	m.settingsItems = buildSettingsItems(m.statusInfo, m.cfg, m.availableAgents)
	return m, saveConfig(m.cfg, m.configPath, func(rm *Model) {
		rm.defaultAgent = prevAgent
		rm.cfg.DefaultAgent = prevCfgAgent
	})
}

func parsePositiveInt(s string, fallback int) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return fallback
		}
		n = n*10 + int(c-'0')
	}
	if n <= 0 {
		return fallback
	}
	return n
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

	// PTY sessions use the embedded terminal view; integration sessions
	// delegate to their native focus mechanism (iTerm tab, tmux window).
	// When Source is empty (persisted session before first refresh), derive
	// it: prefixed IDs (e.g. "iterm:", "tmux:") are integrations; unprefixed
	// IDs belong to the primary, so check the composite's primary source.
	source := r.session.Source
	if source == "" {
		if strings.Contains(r.session.SessionID, ":") {
			source = "" // integration — will skip embedded view
		} else if cb, ok := m.backend.(*terminal.CachedBackend); ok {
			if comp, ok := cb.Inner().(*terminal.CompositeBackend); ok {
				source = comp.PrimarySource()
			}
		}
	}
	if source == "pty" {
		m.termSessionID = r.session.SessionID
		m.termView = newTermView(r.session.SessionID, m.backend)
		m.termView.width = m.width
		m.termView.height = m.height
		m.view = viewTerminal
		return m, termRefreshCmd()
	}

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
	m.store.RemoveProject(r.project.Dir)
	_ = m.store.SaveProjects()
	m.rows = buildRows(storeAdapter{m.store})
	sortRows(m.rows, m.sortCol, m.sortDesc)
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

	// Update activity text and agent type from session names for tracked sessions.
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
			// Re-type if the session name now indicates a different agent.
			// This handles pane reuse (e.g. Claude exits, Codex starts in same pane).
			// Only update when DetectAgent returns a valid agent type — a non-agent
			// name (e.g. "zsh") is handled by orphan removal, not re-typing.
			if detected := terminal.DetectAgent(sess.Name); detected != "" && detected != as.Type {
				as.Type = detected
			}
			// Populate source from composite backend.
			if sess.Source != "" {
				as.Source = sess.Source
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

	// Track orphan ticks: when a session is idle, the terminal name no
	// longer matches an agent pattern, AND the bottom screen region shows
	// no agent UI, the agent likely exited and the pane fell back to a shell.
	//
	// All three conditions are needed:
	// - Name check alone is insufficient: Claude drops ✳/agent keywords
	//   from its title while idle but its screen still shows ❯.
	// - UnmatchedReads alone is insufficient: idle patterns from agent
	//   scrollback (e.g. Codex's › still visible) keep resetting it.
	// - HasAgentScreen restricts pattern matching to the bottom region,
	//   so scrollback from exited agents doesn't prevent orphan cleanup.
	liveNames := make(map[string]string)
	for _, sess := range msg.Sessions {
		liveNames[sess.ID] = sess.Name
	}
	for _, s := range m.store.Sessions {
		name, alive := liveNames[s.SessionID]
		if !alive {
			continue
		}
		if s.Status == model.StatusIdle && s.ScreenChecked && terminal.DetectAgent(name) == "" && !terminal.HasAgentScreen(s.LastScreen, s.Type) {
			s.OrphanTicks++
		} else {
			s.OrphanTicks = 0
		}
		if s.OrphanTicks >= 2 {
			liveIDs[s.SessionID] = false // mark for removal below
		}
	}

	// Remove sessions for dead iTerm sessions and exited agents.
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
	sortRows(m.rows, m.sortCol, m.sortDesc)
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

	m.rows = buildRows(storeAdapter{m.store})
	sortRows(m.rows, m.sortCol, m.sortDesc)

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

	// Determine source from the composite's primary backend.
	source := "pty"
	if cb, ok := m.backend.(*terminal.CachedBackend); ok {
		if comp, ok := cb.Inner().(*terminal.CompositeBackend); ok {
			source = comp.PrimarySource()
		}
	}

	as := &model.AgentSession{
		ProjectDir: msg.ProjectDir,
		SessionID:  msg.SessionID,
		Type:       msg.AgentType,
		Status:     model.StatusWorking,
		LastSent:   time.Now(),
		Source:     source,
	}
	m.store.SetSession(as)

	m.rows = buildRows(storeAdapter{m.store})
	sortRows(m.rows, m.sortCol, m.sortDesc)
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
	sortRows(m.rows, m.sortCol, m.sortDesc)

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
		if !strings.Contains(msg.Err.Error(), "does not support") {
			m.statusText = "Monitor failed: " + msg.Err.Error()
		}
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
	// Strip null bytes from screen content (some backends return \x00 for spaces)
	content := strings.ReplaceAll(msg.Content, "\x00", " ")
	screenChanged := content != as.LastScreen
	as.LastScreen = content
	status, matchLine := terminal.ClassifyScreen(content, as.Type)

	if m.debugLog != nil {
		proj := filepath.Base(msg.ProjectDir)
		escaped := strings.ReplaceAll(content, "\n", "\\n")
		m.debugLog.Printf("[screen] %s prev=%s new=%s changed=%v match=%q content=%q",
			proj, as.Status, status, screenChanged, matchLine, escaped)
	}

	if status == "" {
		// Only count stable (unchanged) unmatched reads. If content is
		// still changing, the agent is active — just producing output
		// that doesn't match our patterns.
		if screenChanged {
			as.UnmatchedReads = 0
		} else {
			as.UnmatchedReads++
		}
		// Screen changed but no pattern match while in needs_input →
		// the agent moved on, transition to working
		if screenChanged && as.Status == model.StatusNeedsInput {
			status = model.StatusWorking
		} else if as.Status == model.StatusWorking && !screenChanged && as.UnmatchedReads >= 3 {
			// Multiple consecutive stable reads with no agent patterns —
			// the agent likely exited and the pane shows a shell.
			status = model.StatusIdle
		} else if !screenChanged && as.Status == model.StatusWorking && isAllBlank(content) {
			// Consecutive blank screen reads while "working" — backend can't
			// read this session. No evidence the agent is working.
			status = model.StatusIdle
		} else {
			return m, nil
		}
	} else {
		as.UnmatchedReads = 0
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
	sortRows(m.rows, m.sortCol, m.sortDesc)

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
	var cmd tea.Cmd
	m.chat.input, cmd = m.chat.input.Update(msg)
	return m, cmd
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
	if cl, ok := m.backend.(interface{ Close() }); ok {
		cl.Close()
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
