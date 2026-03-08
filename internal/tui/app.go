package tui

import (
	"fmt"
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
	chat         chatView
	chatProject  string // project dir for active chat

	// Directory browser
	browserDirs   []DirBrowserItem
	browserCursor int

	// Batch prompt
	batchInput textarea.Model

	// Monitor PIDs to clean up
	monitorPIDs []int
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

	case ScreenReadMsg:
		return m.handleScreenRead(msg)

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
	list := renderProjectList(m.rows, m.cursor, allProjects, m.width)
	sb.WriteString(list)

	activeCount := 0
	for _, r := range m.rows {
		if r.session != nil {
			activeCount++
		}
	}
	sb.WriteString("\n")
	sb.WriteString(renderFooter(len(m.rows), activeCount))

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
		// Toggle expand — for now just show path in status
		if m.cursor >= 0 && m.cursor < len(m.rows) {
			r := m.rows[m.cursor]
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
	m.chatProject = r.project.Dir
	m.chat = newChatView()
	m.chat.setSize(m.width, m.height)
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
			if activity != "" {
				as.Activity = activity
				as.LastActivity = time.Now()
			}
		}
	}

	// Auto-discover untracked agent sessions
	var cmds []tea.Cmd
	liveIDs := make(map[string]bool)
	for _, sess := range msg.Sessions {
		liveIDs[sess.ID] = true
		if trackedIDs[sess.ID] {
			continue
		}
		agentType := terminal.DetectAgent(sess.Name)
		if agentType == "" {
			continue
		}
		// Try to discover CWD — we'll do a simple name match for now
		// Full CWD discovery (lsof etc.) happens in iterm package
		dir := matchProjectDir(sess.Name, m.store.Projects)
		if dir == "" {
			continue
		}
		p := m.store.AddProject(dir)
		if p != nil {
			_ = m.store.SaveProjects()
		}
		as := &model.AgentSession{
			ProjectDir: dir,
			SessionID:  sess.ID,
			Type:       agentType,
			Status:     model.StatusWorking,
		}
		m.store.SetSession(as)
		_ = m.store.SaveSessions()

		// Start monitoring
		logPath := filepath.Join(m.monitorDir, filepath.Base(dir)+".log")
		cmds = append(cmds, startMonitor(m.backend, sess.ID, logPath,
			`Allow|Permission|Error:|\\?$|Waiting for|\u276f`, dir))
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
		`Allow|Permission|Error:|\\?$|Waiting for|\u276f`, msg.ProjectDir))
	cmds = append(cmds, func() tea.Msg {
		if cb, ok := m.backend.(interface{ Invalidate() }); ok {
			cb.Invalidate()
		}
		return nil
	})
	return m, tea.Batch(cmds...)
}

func (m Model) handleStatusUpdated(msg StatusUpdatedMsg) (Model, tea.Cmd) {
	as := m.store.GetSession(msg.ProjectDir)
	if as == nil {
		return m, nil
	}
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
	status := terminal.ClassifyOutput(msg.Content)
	if status != "" {
		as.InitialStatus = status
		as.Status = status
		as.LastActivity = time.Now()
		m.rows = buildRows(storeAdapter{m.store})
	}
	return m, nil
}

func (m Model) handleTick() (Model, tea.Cmd) {
	var cmds []tea.Cmd
	cmds = append(cmds, tickCmd())

	if m.backendOK {
		cmds = append(cmds, refreshSessions(m.backend))

		// Check status for all monitored sessions
		for _, s := range m.store.Sessions {
			if s.MonitorLog != "" {
				cmds = append(cmds, checkStatus(s.ProjectDir, s.MonitorLog))
			} else if !s.ScreenChecked {
				cmds = append(cmds, readScreen(m.backend, s.SessionID, s.ProjectDir))
			}
		}
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

// EnsureMonitorDir creates the monitor directory if needed.
func EnsureMonitorDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}
