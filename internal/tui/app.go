package tui

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
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

	// Backend
	backend    terminal.Backend
	backendOK  bool
	backendErr string

	// Data
	store *model.Store
	rows  []projectRow

	// UI state
	view       viewState
	cursor     int
	width      int
	height     int
	statusText string
	showHelp   bool

	// Chat view
	chat        chatView
	chatProject string // project dir for active chat

	// Spinner & attention
	spinnerFrame  int
	spinnerActive bool
	attentionDirs map[string]time.Time // projects needing attention, with timestamp

	// Directory browser
	browserDirs   []DirBrowserItem
	browserCursor int

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

func (a storeAdapter) GetSession(dir string) *model.AgentSession {
	return a.s.GetSession(dir)
}

func NewModel(backend terminal.Backend, store *model.Store, watchDirs []string, monitorDir string) Model {
	ba := textarea.New()
	ba.Placeholder = "Batch prompt ({name} for project name)..."
	ba.ShowLineNumbers = false
	ba.SetHeight(3)
	ba.CharLimit = 0

	return Model{
		watchDirs:  watchDirs,
		monitorDir: monitorDir,
		backend:    backend,
		store:      store,
		rows:       buildRows(storeAdapter{store}),
		chat:       newChatView(),
		batchInput: ba,
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
	list := renderProjectList(m.rows, m.cursor, allProjects, m.width, m.spinnerFrame, m.attentionDirs)
	sb.WriteString(list)

	activeCount := 0
	for _, r := range m.rows {
		if r.session != nil {
			activeCount++
		}
	}
	var selected *projectRow
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		selected = &m.rows[m.cursor]
	}
	sb.WriteString("\n")
	sb.WriteString(renderFooter(len(m.rows), activeCount, selected))

	if m.statusText != "" {
		sb.WriteString("\n")
		sb.WriteString(statusBarStyle.Render(m.statusText))
	}

	return sb.String()
}

func (m Model) viewChat() string {
	var session *model.AgentSession
	var project *model.Project
	if m.chatProject != "" {
		session = m.store.GetSession(m.chatProject)
		project = m.store.FindProject(m.chatProject)
	}
	return m.chat.render(session, project, m.width)
}

func (m Model) viewDirBrowser() string {
	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Add Project"))
	sb.WriteString("\n\n")
	if len(m.browserDirs) == 0 {
		sb.WriteString(dimStyle.Render("  No directories found in watch_dirs."))
	}
	for i, d := range m.browserDirs {
		prefix := "  "
		if i == m.browserCursor {
			prefix = selectedStyle.Render("> ")
		}
		name := d.Name
		path := dimStyle.Render(" " + d.Path)
		if i == m.browserCursor {
			sb.WriteString(selectedStyle.Render(prefix + name))
		} else {
			sb.WriteString(prefix + name)
		}
		sb.WriteString(path + "\n")
	}
	sb.WriteString("\n")
	sb.WriteString(footerStyle.Render(" Enter: add  Esc: cancel"))
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
  c              Launch Claude for selected project
  x              Launch Codex for selected project
  s              Send prompt (opens chat view)
  f              Focus agent's terminal tab
  a              Add project (directory browser)
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
		return m, nil

	case key.Matches(msg, keys.Down):
		if m.cursor < len(m.rows)-1 {
			m.cursor++
		}
		return m, nil

	case key.Matches(msg, keys.Up):
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil

	case key.Matches(msg, keys.LaunchClaude):
		return m.launchForSelected(model.AgentClaude)

	case key.Matches(msg, keys.LaunchCodex):
		return m.launchForSelected(model.AgentCodex)

	case key.Matches(msg, keys.Send):
		return m.openChat()

	case key.Matches(msg, keys.Focus):
		return m.focusSelected()

	case key.Matches(msg, keys.Add):
		return m, listDirs(m.watchDirs)

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
		if m.cursor >= 0 && m.cursor < len(m.rows) {
			r := m.rows[m.cursor]
			if r.session != nil {
				return m.openChat()
			}
			m.statusText = r.project.Dir
		}
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

		session := m.store.GetSession(m.chatProject)
		if session == nil {
			m.statusText = "No agent session"
			return m, nil
		}
		session.LastSent = time.Now()
		return m, sendPrompt(m.backend, session.SessionID, text, m.chatProject)
	}

	// Pass to textarea
	var cmd tea.Cmd
	m.chat.input, cmd = m.chat.input.Update(msg)
	return m, cmd
}

func (m Model) handleDirBrowserKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Escape):
		m.view = viewProjectList
		return m, nil

	case key.Matches(msg, keys.Down):
		if m.browserCursor < len(m.browserDirs)-1 {
			m.browserCursor++
		}
		return m, nil

	case key.Matches(msg, keys.Up):
		if m.browserCursor > 0 {
			m.browserCursor--
		}
		return m, nil

	case key.Matches(msg, keys.Enter):
		if m.browserCursor >= 0 && m.browserCursor < len(m.browserDirs) {
			dir := m.browserDirs[m.browserCursor]
			m.store.AddProject(dir.Path)
			_ = m.store.SaveProjects()
			m.rows = buildRows(storeAdapter{m.store})
			m.statusText = fmt.Sprintf("Added %s", dir.Name)
		}
		m.view = viewProjectList
		return m, nil
	}
	return m, nil
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

func (m Model) launchForSelected(agentType model.AgentType) (Model, tea.Cmd) {
	if !m.backendOK {
		m.statusText = "Backend unavailable"
		return m, nil
	}
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return m, nil
	}
	r := m.rows[m.cursor]
	if r.session != nil {
		m.statusText = fmt.Sprintf("%s already has an agent", r.project.Name)
		return m, nil
	}
	m.statusText = fmt.Sprintf("Launching %s for %s...", agentType, r.project.Name)
	return m, launchAgent(m.backend, r.project.Dir, agentType)
}

func (m Model) openChat() (Model, tea.Cmd) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return m, nil
	}
	r := m.rows[m.cursor]
	if r.session == nil {
		m.statusText = "No agent running for this project"
		return m, nil
	}
	if m.chatProject != r.project.Dir {
		m.chatProject = r.project.Dir
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
	if r.session == nil {
		m.statusText = "No agent running for this project"
		return m, nil
	}
	return m, focusSession(m.backend, r.session.SessionID)
}

func (m Model) deleteSelected() (Model, tea.Cmd) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return m, nil
	}
	r := m.rows[m.cursor]
	m.store.RemoveProject(r.project.Dir)
	_ = m.store.SaveProjects()
	m.rows = buildRows(storeAdapter{m.store})
	if m.cursor >= len(m.rows) && m.cursor > 0 {
		m.cursor--
	}
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

	// Update activity from session names for tracked sessions
	for _, sess := range msg.Sessions {
		if as := m.store.SessionByID(sess.ID); as != nil {
			activity := terminal.ExtractActivity(sess.Name)
			if activity != "" && activity != as.Activity {
				as.Activity = activity
				as.LastActivity = time.Now()
				as.Status = model.StatusWorking
				// Clear stale attention from previous needs_input
				as.Attention = ""
				delete(m.attentionDirs, as.ProjectDir)
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

	// Remove sessions for dead iTerm sessions
	for _, s := range m.store.Sessions {
		if !liveIDs[s.SessionID] {
			m.store.RemoveSession(s.ProjectDir)
		}
	}

	m.rows = buildRows(storeAdapter{m.store})
	if m.cursor >= len(m.rows) && m.cursor > 0 {
		m.cursor = len(m.rows) - 1
	}

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

	p := m.store.AddProject(msg.ProjectDir)
	if p != nil {
		_ = m.store.SaveProjects()
	}

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

	// Add received text to chat if viewing this project
	if m.view == viewChat && m.chatProject == msg.ProjectDir && msg.Attention != "" {
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
		if m.attentionDirs == nil {
			m.attentionDirs = make(map[string]time.Time)
		}
		m.attentionDirs[msg.ProjectDir] = time.Now()
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
		delete(m.attentionDirs, msg.ProjectDir)
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
	as := m.store.GetSession(msg.ProjectDir)
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
		} else {
			return m, nil
		}
	}

	// Skip stale screen reads (identical content)
	if !screenChanged && status == as.Status {
		return m, nil
	}
	// Screen reads reliably detect needs_input and error.
	// For working/idle, session name activity is the primary signal since
	// Claude's bottom screen lines are often blank padding.
	if status == model.StatusIdle || status == model.StatusWorking {
		// Only transition to idle if not recently active (session name changes)
		if status == model.StatusIdle && as.Status == model.StatusWorking {
			if !as.LastActivity.IsZero() && time.Since(as.LastActivity) < 10*time.Second {
				return m, nil
			}
		}
	}

	prevStatus := as.Status
	as.Status = status
	as.LastActivity = time.Now()
	if status == model.StatusNeedsInput {
		as.Attention = matchLine
	}

	// Add to chat if viewing this project
	if m.view == viewChat && m.chatProject == msg.ProjectDir && status == model.StatusNeedsInput && as.Attention != "" {
		m.chat.addEntry(model.ChatEntry{
			Timestamp: time.Now(),
			Direction: "received",
			Text:      as.Attention,
		})
	}

	m.rows = buildRows(storeAdapter{m.store})

	// Bell on needs_input transition
	if status == model.StatusNeedsInput && prevStatus != model.StatusNeedsInput {
		if m.attentionDirs == nil {
			m.attentionDirs = make(map[string]time.Time)
		}
		m.attentionDirs[msg.ProjectDir] = time.Now()
		cmds := []tea.Cmd{bellCmd()}
		if cmd := m.ensureSpinner(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	}
	if prevStatus == model.StatusNeedsInput && status != model.StatusNeedsInput {
		as.Attention = ""
		m.statusText = ""
		delete(m.attentionDirs, msg.ProjectDir)
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
	return len(m.attentionDirs) > 0
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
func EnsureMonitorDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}
