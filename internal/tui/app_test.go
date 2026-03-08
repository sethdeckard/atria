package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sethdeckard/atria/internal/model"
	"github.com/sethdeckard/atria/internal/terminal"
)

// --- mock backend ---

type mockBackend struct {
	available    error
	sessions     []terminal.Session
	newSessionID string
	newSessionErr error
	sendTextLog  []string
	runCmdLog    []string
	focusLog     []string
	screenContent string
	getVarVal    string
	monitorPID   int
}

func (m *mockBackend) Available() error                        { return m.available }
func (m *mockBackend) ListSessions() ([]terminal.Session, error) { return m.sessions, nil }
func (m *mockBackend) NewSession() (string, error) {
	return m.newSessionID, m.newSessionErr
}
func (m *mockBackend) SendText(sid, text string) error {
	m.sendTextLog = append(m.sendTextLog, fmt.Sprintf("%s:%s", sid, text))
	return nil
}
func (m *mockBackend) RunCommand(sid, cmd string) error {
	m.runCmdLog = append(m.runCmdLog, fmt.Sprintf("%s:%s", sid, cmd))
	return nil
}
func (m *mockBackend) FocusSession(sid string) error {
	m.focusLog = append(m.focusLog, sid)
	return nil
}
func (m *mockBackend) ReadScreen(sid string, lines int) (string, error) {
	return m.screenContent, nil
}
func (m *mockBackend) GetVar(sid, name string) (string, error) {
	return m.getVarVal, nil
}
func (m *mockBackend) MonitorOutput(sid, logPath, patterns string) (int, error) {
	return m.monitorPID, nil
}

// --- helpers ---

func newTestModelWithStore(backend *mockBackend, store *model.Store) Model {
	m := NewModel(backend, store, []string{"/watch"}, "/tmp/monitors")
	m.width = 120
	m.height = 40
	return m
}

func keyMsg(k string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

func ctrlKeyMsg(k tea.KeyType) tea.KeyMsg {
	return tea.KeyMsg{Type: k}
}

func modelFrom(tm tea.Model) Model {
	return tm.(Model)
}

func makeStore(t *testing.T) *model.Store {
	return model.NewStore(t.TempDir())
}

func makeProjects(dirs ...string) []*model.Project {
	var ps []*model.Project
	for _, d := range dirs {
		ps = append(ps, &model.Project{
			Name:    d[strings.LastIndex(d, "/")+1:],
			Dir:     d,
			AddedAt: time.Now(),
		})
	}
	return ps
}

// --- tests ---

func TestWindowSizeMsg(t *testing.T) {
	m := newTestModelWithStore(&mockBackend{}, makeStore(t))
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	um := modelFrom(updated)
	if um.width != 200 || um.height != 50 {
		t.Errorf("expected 200x50, got %dx%d", um.width, um.height)
	}
}

func TestQuitKey(t *testing.T) {
	m := newTestModelWithStore(&mockBackend{}, makeStore(t))
	m.width = 80
	_, cmd := m.Update(keyMsg("q"))
	if cmd == nil {
		t.Fatal("expected quit command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg, got %T", msg)
	}
}

func TestHelpToggle(t *testing.T) {
	m := newTestModelWithStore(&mockBackend{}, makeStore(t))
	m.width = 80
	if m.showHelp {
		t.Fatal("help should be hidden initially")
	}
	updated, _ := m.Update(keyMsg("?"))
	um := modelFrom(updated)
	if !um.showHelp {
		t.Error("help should be shown after ?")
	}
	updated, _ = um.Update(keyMsg("?"))
	um = modelFrom(updated)
	if um.showHelp {
		t.Error("help should be hidden after second ?")
	}
}

func TestCursorNavigation(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/alpha", "/a/beta", "/a/gamma")
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 80

	if m.cursor != 0 {
		t.Fatalf("expected cursor at 0, got %d", m.cursor)
	}

	// Move down
	updated, _ := m.Update(keyMsg("j"))
	um := modelFrom(updated)
	if um.cursor != 1 {
		t.Errorf("expected cursor at 1 after j, got %d", um.cursor)
	}

	// Move down again
	updated, _ = um.Update(keyMsg("j"))
	um = modelFrom(updated)
	if um.cursor != 2 {
		t.Errorf("expected cursor at 2 after second j, got %d", um.cursor)
	}

	// Can't go past end
	updated, _ = um.Update(keyMsg("j"))
	um = modelFrom(updated)
	if um.cursor != 2 {
		t.Errorf("expected cursor clamped at 2, got %d", um.cursor)
	}

	// Move up
	updated, _ = um.Update(keyMsg("k"))
	um = modelFrom(updated)
	if um.cursor != 1 {
		t.Errorf("expected cursor at 1 after k, got %d", um.cursor)
	}

	// Up to 0
	updated, _ = um.Update(keyMsg("k"))
	um = modelFrom(updated)
	if um.cursor != 0 {
		t.Errorf("expected cursor at 0, got %d", um.cursor)
	}

	// Can't go below 0
	updated, _ = um.Update(keyMsg("k"))
	um = modelFrom(updated)
	if um.cursor != 0 {
		t.Errorf("expected cursor clamped at 0, got %d", um.cursor)
	}
}

func TestBackendAvailableSuccess(t *testing.T) {
	m := newTestModelWithStore(&mockBackend{}, makeStore(t))
	updated, cmd := m.Update(BackendAvailableMsg{Err: nil})
	um := modelFrom(updated)
	if !um.backendOK {
		t.Error("expected backendOK to be true")
	}
	if cmd == nil {
		t.Error("expected a refresh sessions command")
	}
}

func TestBackendAvailableFailure(t *testing.T) {
	m := newTestModelWithStore(&mockBackend{}, makeStore(t))
	updated, cmd := m.Update(BackendAvailableMsg{Err: errors.New("it2 not found")})
	um := modelFrom(updated)
	if um.backendOK {
		t.Error("expected backendOK to be false")
	}
	if !strings.Contains(um.statusText, "it2 not found") {
		t.Errorf("expected status to mention error, got %q", um.statusText)
	}
	if cmd != nil {
		t.Error("expected no command on backend failure")
	}
}

func TestLaunchBlockedWithoutBackend(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 80
	// backendOK is false by default

	updated, cmd := m.Update(keyMsg("c"))
	um := modelFrom(updated)
	if cmd != nil {
		t.Error("expected no command when backend unavailable")
	}
	if !strings.Contains(um.statusText, "Backend unavailable") {
		t.Errorf("expected unavailable message, got %q", um.statusText)
	}
}

func TestLaunchWithBackend(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	mb := &mockBackend{newSessionID: "sess-1"}
	m := newTestModelWithStore(mb, store)
	m.width = 80
	m.backendOK = true

	_, cmd := m.Update(keyMsg("c"))
	if cmd == nil {
		t.Fatal("expected a launch command")
	}
}

func TestLaunchBlockedWhenAgentExists(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/a/myproject",
		SessionID:  "existing",
		Type:       model.AgentClaude,
		Status:     model.StatusWorking,
	})
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 80
	m.backendOK = true

	updated, cmd := m.Update(keyMsg("c"))
	um := modelFrom(updated)
	if cmd != nil {
		t.Error("expected no command when agent already exists")
	}
	if !strings.Contains(um.statusText, "already has an agent") {
		t.Errorf("expected already-has-agent message, got %q", um.statusText)
	}
}

func TestAgentLaunchedMsg(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	m := newTestModelWithStore(&mockBackend{monitorPID: 42}, store)
	m.width = 80
	m.backendOK = true

	updated, cmd := m.Update(AgentLaunchedMsg{
		ProjectDir: "/a/myproject",
		SessionID:  "sess-1",
		AgentType:  model.AgentClaude,
	})
	um := modelFrom(updated)

	session := um.store.GetSession("/a/myproject")
	if session == nil {
		t.Fatal("expected session to be created")
	}
	if session.SessionID != "sess-1" {
		t.Errorf("expected session ID sess-1, got %q", session.SessionID)
	}
	if session.Type != model.AgentClaude {
		t.Errorf("expected agent type claude, got %q", session.Type)
	}
	if session.Status != model.StatusWorking {
		t.Errorf("expected status working, got %q", session.Status)
	}
	if !strings.Contains(um.statusText, "Launched") {
		t.Errorf("expected launched message, got %q", um.statusText)
	}
	if cmd == nil {
		t.Error("expected monitor start command")
	}
}

func TestAgentLaunchedMsgError(t *testing.T) {
	store := makeStore(t)
	m := newTestModelWithStore(&mockBackend{}, store)

	updated, _ := m.Update(AgentLaunchedMsg{
		ProjectDir: "/a/myproject",
		Err:        errors.New("tab creation failed"),
	})
	um := modelFrom(updated)
	if !strings.Contains(um.statusText, "Launch failed") {
		t.Errorf("expected launch failed message, got %q", um.statusText)
	}
}

func TestDeleteProject(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/alpha", "/a/beta")
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 80

	updated, _ := m.Update(keyMsg("d"))
	um := modelFrom(updated)
	if len(um.store.Projects) != 1 {
		t.Fatalf("expected 1 project after delete, got %d", len(um.store.Projects))
	}
	if !strings.Contains(um.statusText, "Removed") {
		t.Errorf("expected removed message, got %q", um.statusText)
	}
}

func TestDeleteLastProjectAdjustsCursor(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/alpha", "/a/beta")
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 80
	m.cursor = 1 // select last

	updated, _ := m.Update(keyMsg("d"))
	um := modelFrom(updated)
	if um.cursor != 0 {
		t.Errorf("expected cursor to adjust to 0, got %d", um.cursor)
	}
}

func TestOpenChatNoAgent(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 80

	updated, _ := m.Update(keyMsg("s"))
	um := modelFrom(updated)
	if um.view != viewProjectList {
		t.Error("expected to stay on project list when no agent")
	}
	if !strings.Contains(um.statusText, "No agent running") {
		t.Errorf("expected no agent message, got %q", um.statusText)
	}
}

func TestOpenChatWithAgent(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/a/myproject",
		SessionID:  "sess-1",
		Type:       model.AgentClaude,
		Status:     model.StatusWorking,
	})
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 80
	m.height = 40

	updated, _ := m.Update(keyMsg("s"))
	um := modelFrom(updated)
	if um.view != viewChat {
		t.Errorf("expected chat view, got %d", um.view)
	}
	if um.chatProject != "/a/myproject" {
		t.Errorf("expected chatProject /a/myproject, got %q", um.chatProject)
	}
}

func TestChatEscapeReturnsToProjectList(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/a/myproject",
		SessionID:  "sess-1",
		Type:       model.AgentClaude,
		Status:     model.StatusWorking,
	})
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 80
	m.height = 40

	// Enter chat
	updated, _ := m.Update(keyMsg("s"))
	um := modelFrom(updated)
	if um.view != viewChat {
		t.Fatal("expected chat view")
	}

	// Escape back
	updated, _ = um.Update(ctrlKeyMsg(tea.KeyEscape))
	um = modelFrom(updated)
	if um.view != viewProjectList {
		t.Errorf("expected project list after esc, got %d", um.view)
	}
}

func TestFocusNoAgent(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 80
	m.backendOK = true

	updated, cmd := m.Update(keyMsg("f"))
	um := modelFrom(updated)
	if cmd != nil {
		t.Error("expected no command when no agent")
	}
	if !strings.Contains(um.statusText, "No agent running") {
		t.Errorf("expected no agent message, got %q", um.statusText)
	}
}

func TestFocusWithAgent(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/a/myproject",
		SessionID:  "sess-1",
		Type:       model.AgentClaude,
		Status:     model.StatusWorking,
	})
	mb := &mockBackend{}
	m := newTestModelWithStore(mb, store)
	m.width = 80
	m.backendOK = true

	_, cmd := m.Update(keyMsg("f"))
	if cmd == nil {
		t.Fatal("expected focus command")
	}
}

func TestFocusBlockedWithoutBackend(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/a/myproject",
		SessionID:  "sess-1",
		Type:       model.AgentClaude,
	})
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 80
	// backendOK = false

	updated, cmd := m.Update(keyMsg("f"))
	um := modelFrom(updated)
	if cmd != nil {
		t.Error("expected no command without backend")
	}
	if !strings.Contains(um.statusText, "Backend unavailable") {
		t.Errorf("expected unavailable message, got %q", um.statusText)
	}
}

func TestDirBrowserMsg(t *testing.T) {
	m := newTestModelWithStore(&mockBackend{}, makeStore(t))

	dirs := []DirBrowserItem{
		{Path: "/watch/alpha", Name: "alpha"},
		{Path: "/watch/beta", Name: "beta"},
	}
	updated, _ := m.Update(DirBrowserMsg{Dirs: dirs})
	um := modelFrom(updated)
	if um.view != viewDirBrowser {
		t.Errorf("expected dir browser view, got %d", um.view)
	}
	if len(um.browserDirs) != 2 {
		t.Errorf("expected 2 browser dirs, got %d", len(um.browserDirs))
	}
	if um.browserCursor != 0 {
		t.Errorf("expected browser cursor at 0, got %d", um.browserCursor)
	}
}

func TestDirBrowserNavAndSelect(t *testing.T) {
	store := makeStore(t)
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 80
	m.view = viewDirBrowser
	m.browserDirs = []DirBrowserItem{
		{Path: "/watch/alpha", Name: "alpha"},
		{Path: "/watch/beta", Name: "beta"},
	}

	// Navigate down
	updated, _ := m.Update(keyMsg("j"))
	um := modelFrom(updated)
	if um.browserCursor != 1 {
		t.Errorf("expected browser cursor at 1, got %d", um.browserCursor)
	}

	// Select with Enter
	updated, _ = um.Update(ctrlKeyMsg(tea.KeyEnter))
	um = modelFrom(updated)
	if um.view != viewProjectList {
		t.Errorf("expected return to project list, got %d", um.view)
	}
	if um.store.FindProject("/watch/beta") == nil {
		t.Error("expected beta project to be added")
	}
	if !strings.Contains(um.statusText, "Added beta") {
		t.Errorf("expected added message, got %q", um.statusText)
	}
}

func TestDirBrowserEscape(t *testing.T) {
	m := newTestModelWithStore(&mockBackend{}, makeStore(t))
	m.width = 80
	m.view = viewDirBrowser

	updated, _ := m.Update(ctrlKeyMsg(tea.KeyEscape))
	um := modelFrom(updated)
	if um.view != viewProjectList {
		t.Errorf("expected project list after esc, got %d", um.view)
	}
}

func TestBatchBlockedWithoutBackend(t *testing.T) {
	m := newTestModelWithStore(&mockBackend{}, makeStore(t))
	m.width = 80

	updated, cmd := m.Update(keyMsg("B"))
	um := modelFrom(updated)
	if cmd != nil {
		t.Error("expected no command without backend")
	}
	if !strings.Contains(um.statusText, "Backend unavailable") {
		t.Errorf("expected unavailable message, got %q", um.statusText)
	}
}

func TestBatchOpenAndEscape(t *testing.T) {
	m := newTestModelWithStore(&mockBackend{}, makeStore(t))
	m.width = 80
	m.backendOK = true

	updated, _ := m.Update(keyMsg("B"))
	um := modelFrom(updated)
	if um.view != viewBatchPrompt {
		t.Errorf("expected batch view, got %d", um.view)
	}

	updated, _ = um.Update(ctrlKeyMsg(tea.KeyEscape))
	um = modelFrom(updated)
	if um.view != viewProjectList {
		t.Errorf("expected project list after esc, got %d", um.view)
	}
}

func TestEnterShowsPath(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 80

	updated, _ := m.Update(ctrlKeyMsg(tea.KeyEnter))
	um := modelFrom(updated)
	if um.statusText != "/a/myproject" {
		t.Errorf("expected path in status, got %q", um.statusText)
	}
}

func TestStatusMsg(t *testing.T) {
	m := newTestModelWithStore(&mockBackend{}, makeStore(t))
	updated, _ := m.Update(StatusMsg{Text: "hello"})
	um := modelFrom(updated)
	if um.statusText != "hello" {
		t.Errorf("expected status 'hello', got %q", um.statusText)
	}
}

func TestPromptSentError(t *testing.T) {
	m := newTestModelWithStore(&mockBackend{}, makeStore(t))
	updated, _ := m.Update(PromptSentMsg{Err: errors.New("send failed")})
	um := modelFrom(updated)
	if !strings.Contains(um.statusText, "Send failed") {
		t.Errorf("expected send failed message, got %q", um.statusText)
	}
}

func TestFocusedError(t *testing.T) {
	m := newTestModelWithStore(&mockBackend{}, makeStore(t))
	updated, _ := m.Update(FocusedMsg{Err: errors.New("bad session")})
	um := modelFrom(updated)
	if !strings.Contains(um.statusText, "Focus failed") {
		t.Errorf("expected focus failed message, got %q", um.statusText)
	}
}

func TestSessionsRefreshedError(t *testing.T) {
	m := newTestModelWithStore(&mockBackend{}, makeStore(t))
	updated, _ := m.Update(SessionsRefreshedMsg{Err: errors.New("timeout")})
	um := modelFrom(updated)
	if !strings.Contains(um.statusText, "Session refresh failed") {
		t.Errorf("expected refresh failed message, got %q", um.statusText)
	}
}

func TestSessionsRefreshedUpdatesActivity(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/a/myproject",
		SessionID:  "sess-1",
		Type:       model.AgentClaude,
		Status:     model.StatusWorking,
	})
	m := newTestModelWithStore(&mockBackend{}, store)

	updated, _ := m.Update(SessionsRefreshedMsg{
		Sessions: []terminal.Session{
			{ID: "sess-1", Name: "\u2733 Editing main.go (sourcekit-lsp)"},
		},
	})
	um := modelFrom(updated)
	session := um.store.GetSession("/a/myproject")
	if session.Activity != "Editing main.go" {
		t.Errorf("expected activity 'Editing main.go', got %q", session.Activity)
	}
}

func TestSessionsRefreshedRemovesDeadSessions(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/a/myproject",
		SessionID:  "sess-dead",
		Type:       model.AgentClaude,
		Status:     model.StatusWorking,
	})
	m := newTestModelWithStore(&mockBackend{}, store)

	// Refresh with no sessions — sess-dead should be removed
	updated, _ := m.Update(SessionsRefreshedMsg{Sessions: nil})
	um := modelFrom(updated)
	if um.store.GetSession("/a/myproject") != nil {
		t.Error("expected dead session to be removed")
	}
}

func TestSessionsRefreshedDispatchesDiscovery(t *testing.T) {
	store := makeStore(t)
	// No existing projects — the agent should be discovered via CWD
	m := newTestModelWithStore(&mockBackend{}, store)

	updated, cmd := m.Update(SessionsRefreshedMsg{
		Sessions: []terminal.Session{
			{ID: "new-sess", Name: "\u2733 Claude Code (claude)", TTY: "/dev/ttys005"},
		},
	})
	_ = modelFrom(updated)
	if cmd == nil {
		t.Fatal("expected discovery command for untracked agent session")
	}
}

func TestAgentDiscoveredMsg(t *testing.T) {
	store := makeStore(t)
	m := newTestModelWithStore(&mockBackend{monitorPID: 99}, store)
	m.width = 80

	updated, cmd := m.Update(AgentDiscoveredMsg{
		SessionID: "sess-new",
		AgentType: model.AgentClaude,
		Dir:       "/a/discovered",
	})
	um := modelFrom(updated)

	proj := um.store.FindProject("/a/discovered")
	if proj == nil {
		t.Fatal("expected project to be auto-added")
	}
	session := um.store.GetSession("/a/discovered")
	if session == nil {
		t.Fatal("expected session to be created")
	}
	if session.SessionID != "sess-new" {
		t.Errorf("expected session ID sess-new, got %q", session.SessionID)
	}
	if session.Type != model.AgentClaude {
		t.Errorf("expected claude, got %q", session.Type)
	}
	if cmd == nil {
		t.Error("expected monitor start command")
	}
}

func TestAgentDiscoveredEmptyDir(t *testing.T) {
	m := newTestModelWithStore(&mockBackend{}, makeStore(t))
	updated, cmd := m.Update(AgentDiscoveredMsg{
		SessionID: "sess-1",
		AgentType: model.AgentCodex,
		Dir:       "",
	})
	um := modelFrom(updated)
	if len(um.store.Sessions) != 0 {
		t.Error("expected no session for empty dir")
	}
	if cmd != nil {
		t.Error("expected no command for empty dir")
	}
}

func TestAgentDiscoveredDuplicate(t *testing.T) {
	store := makeStore(t)
	store.SetSession(&model.AgentSession{
		ProjectDir: "/a/existing",
		SessionID:  "sess-1",
		Type:       model.AgentClaude,
	})
	m := newTestModelWithStore(&mockBackend{}, store)

	// Same session ID discovered again — should be skipped
	updated, cmd := m.Update(AgentDiscoveredMsg{
		SessionID: "sess-1",
		AgentType: model.AgentClaude,
		Dir:       "/a/existing",
	})
	um := modelFrom(updated)
	if len(um.store.Sessions) != 1 {
		t.Errorf("expected 1 session, got %d", len(um.store.Sessions))
	}
	if cmd != nil {
		t.Error("expected no command for duplicate")
	}
}

func TestStatusUpdatedMsg(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/a/myproject",
		SessionID:  "sess-1",
		Type:       model.AgentClaude,
		Status:     model.StatusWorking,
	})
	m := newTestModelWithStore(&mockBackend{}, store)

	updated, _ := m.Update(StatusUpdatedMsg{
		ProjectDir: "/a/myproject",
		Status:     model.StatusNeedsInput,
		Attention:  "Allow file edit? [y/n]",
	})
	um := modelFrom(updated)
	session := um.store.GetSession("/a/myproject")
	if session.Status != model.StatusNeedsInput {
		t.Errorf("expected needs_input, got %q", session.Status)
	}
	if session.Attention != "Allow file edit? [y/n]" {
		t.Errorf("expected attention text, got %q", session.Attention)
	}
}

func TestStatusUpdatedAddsToChat(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/a/myproject",
		SessionID:  "sess-1",
		Type:       model.AgentClaude,
		Status:     model.StatusWorking,
	})
	m := newTestModelWithStore(&mockBackend{}, store)
	m.view = viewChat
	m.chatProject = "/a/myproject"
	m.chat.setSize(80, 40)

	updated, _ := m.Update(StatusUpdatedMsg{
		ProjectDir: "/a/myproject",
		Status:     model.StatusNeedsInput,
		Attention:  "Continue?",
	})
	um := modelFrom(updated)
	if len(um.chat.entries) != 1 {
		t.Fatalf("expected 1 chat entry, got %d", len(um.chat.entries))
	}
	if um.chat.entries[0].Direction != "received" {
		t.Errorf("expected received direction, got %q", um.chat.entries[0].Direction)
	}
	if um.chat.entries[0].Text != "Continue?" {
		t.Errorf("expected 'Continue?', got %q", um.chat.entries[0].Text)
	}
}

func TestMonitorStartedMsg(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/a/myproject",
		SessionID:  "sess-1",
		Type:       model.AgentClaude,
	})
	m := newTestModelWithStore(&mockBackend{}, store)

	updated, _ := m.Update(MonitorStartedMsg{
		ProjectDir: "/a/myproject",
		PID:        1234,
		LogPath:    "/tmp/monitors/myproject.log",
	})
	um := modelFrom(updated)
	session := um.store.GetSession("/a/myproject")
	if session.MonitorPID != 1234 {
		t.Errorf("expected PID 1234, got %d", session.MonitorPID)
	}
	if session.MonitorLog != "/tmp/monitors/myproject.log" {
		t.Errorf("expected log path, got %q", session.MonitorLog)
	}
	if len(um.monitorPIDs) != 1 || um.monitorPIDs[0] != 1234 {
		t.Errorf("expected monitorPIDs [1234], got %v", um.monitorPIDs)
	}
}

func TestMonitorStartedError(t *testing.T) {
	m := newTestModelWithStore(&mockBackend{}, makeStore(t))
	updated, _ := m.Update(MonitorStartedMsg{Err: errors.New("spawn failed")})
	um := modelFrom(updated)
	if !strings.Contains(um.statusText, "Monitor failed") {
		t.Errorf("expected monitor failed message, got %q", um.statusText)
	}
}

func TestScreenReadUpdatesStatus(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/a/myproject",
		SessionID:  "sess-1",
		Type:       model.AgentClaude,
		Status:     model.StatusWorking,
	})
	m := newTestModelWithStore(&mockBackend{}, store)

	updated, _ := m.Update(ScreenReadMsg{
		ProjectDir: "/a/myproject",
		Content:    "some output\n\u276f",
	})
	um := modelFrom(updated)
	session := um.store.GetSession("/a/myproject")
	if !session.ScreenChecked {
		t.Error("expected ScreenChecked to be true")
	}
	if session.Status != model.StatusIdle {
		t.Errorf("expected idle status, got %q", session.Status)
	}
}

func TestScreenReadErrorIgnored(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/a/myproject",
		SessionID:  "sess-1",
		Type:       model.AgentClaude,
		Status:     model.StatusWorking,
	})
	m := newTestModelWithStore(&mockBackend{}, store)

	updated, _ := m.Update(ScreenReadMsg{
		ProjectDir: "/a/myproject",
		Err:        errors.New("read failed"),
	})
	um := modelFrom(updated)
	session := um.store.GetSession("/a/myproject")
	if session.Status != model.StatusWorking {
		t.Errorf("expected status unchanged, got %q", session.Status)
	}
}

func TestTickProducesCommands(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/a/myproject",
		SessionID:  "sess-1",
		Type:       model.AgentClaude,
		MonitorLog: "/tmp/monitors/myproject.log",
	})
	m := newTestModelWithStore(&mockBackend{}, store)
	m.backendOK = true

	_, cmd := m.Update(TickMsg{})
	if cmd == nil {
		t.Fatal("expected commands from tick")
	}
}

func TestTickNoCommandsWithoutBackend(t *testing.T) {
	m := newTestModelWithStore(&mockBackend{}, makeStore(t))
	// backendOK = false

	_, cmd := m.Update(TickMsg{})
	// Should still have tick rescheduled but no backend commands
	if cmd == nil {
		t.Fatal("expected at least tick reschedule")
	}
}

func TestViewBeforeWindowSize(t *testing.T) {
	store := makeStore(t)
	m := NewModel(&mockBackend{}, store, nil, "/tmp/monitors")
	// width/height are 0 — no WindowSizeMsg received yet
	v := m.View()
	if v != "Loading..." {
		t.Errorf("expected Loading..., got %q", v)
	}
}

func TestViewProjectListEmpty(t *testing.T) {
	m := newTestModelWithStore(&mockBackend{}, makeStore(t))
	m.width = 80
	m.height = 40
	v := m.View()
	if !strings.Contains(v, "Projects") {
		t.Error("expected Projects title")
	}
	if !strings.Contains(v, "Agent orchestration") {
		t.Error("expected tagline in empty state")
	}
	if !strings.Contains(v, "Add a project") {
		t.Error("expected hint in empty state")
	}
}

func TestViewProjectListWithProjects(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/alpha", "/a/beta")
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 80
	m.height = 40
	v := m.View()
	if !strings.Contains(v, "alpha") {
		t.Error("expected alpha in view")
	}
	if !strings.Contains(v, "beta") {
		t.Error("expected beta in view")
	}
	if !strings.Contains(v, "2 projects") {
		t.Error("expected project count")
	}
}

func TestViewHelp(t *testing.T) {
	m := newTestModelWithStore(&mockBackend{}, makeStore(t))
	m.width = 80
	m.height = 40
	m.showHelp = true
	v := m.View()
	if !strings.Contains(v, "Key Bindings") {
		t.Error("expected help text in view")
	}
}

// --- projectlist unit tests ---

func TestSortRows(t *testing.T) {
	rows := []projectRow{
		{project: &model.Project{Name: "delta"}, session: nil},
		{project: &model.Project{Name: "alpha"}, session: &model.AgentSession{Status: model.StatusIdle}},
		{project: &model.Project{Name: "beta"}, session: &model.AgentSession{Status: model.StatusNeedsInput}},
		{project: &model.Project{Name: "gamma"}, session: &model.AgentSession{Status: model.StatusWorking}},
	}
	sortRows(rows)

	expected := []string{"beta", "gamma", "alpha", "delta"}
	for i, name := range expected {
		if rows[i].project.Name != name {
			t.Errorf("position %d: expected %q, got %q", i, name, rows[i].project.Name)
		}
	}
}

func TestStatusPriority(t *testing.T) {
	tests := []struct {
		session  *model.AgentSession
		expected int
	}{
		{nil, 4},
		{&model.AgentSession{Status: model.StatusNeedsInput}, 0},
		{&model.AgentSession{Status: model.StatusWorking}, 1},
		{&model.AgentSession{Status: model.StatusError}, 2},
		{&model.AgentSession{Status: model.StatusIdle}, 3},
		{&model.AgentSession{Status: ""}, 1}, // default
	}
	for _, tc := range tests {
		got := statusPriority(tc.session)
		if got != tc.expected {
			t.Errorf("statusPriority(%v) = %d, want %d", tc.session, got, tc.expected)
		}
	}
}

func TestFormatStatus(t *testing.T) {
	tests := []struct {
		status    model.AgentStatus
		activity  string
		attention string
		contains  string
	}{
		{model.StatusWorking, "", "", "Working..."},
		{model.StatusWorking, "Running tests", "", "Running tests"},
		{model.StatusIdle, "", "", "idle"},
		{model.StatusNeedsInput, "", "Allow edit?", "Allow edit?"},
		{model.StatusNeedsInput, "", "", "Needs input"},
		{model.StatusError, "", "", "error"},
		{model.StatusError, "", "Crash!", "Crash!"},
	}
	for _, tc := range tests {
		session := &model.AgentSession{
			Status:    tc.status,
			Activity:  tc.activity,
			Attention: tc.attention,
		}
		text, _ := formatStatus(session, 0)
		if !strings.Contains(text, tc.contains) {
			t.Errorf("formatStatus(%q, %q, %q) = %q, want containing %q",
				tc.status, tc.activity, tc.attention, text, tc.contains)
		}
	}
}

func TestRelativeTime(t *testing.T) {
	tests := []struct {
		ago      time.Duration
		expected string
	}{
		{10 * time.Second, "just now"},
		{5 * time.Minute, "5m ago"},
		{3 * time.Hour, "3h ago"},
		{48 * time.Hour, "2d ago"},
	}
	for _, tc := range tests {
		got := relativeTime(time.Now().Add(-tc.ago))
		if got != tc.expected {
			t.Errorf("relativeTime(-%v) = %q, want %q", tc.ago, got, tc.expected)
		}
	}
}

func TestMatchProjectDir(t *testing.T) {
	projects := makeProjects("/a/myapp", "/b/other")

	tests := []struct {
		name     string
		expected string
	}{
		{"\u2733 Claude Code (myapp)", "/a/myapp"},
		{"codex (other)", "/b/other"},
		{"unrelated session", ""},
	}
	for _, tc := range tests {
		got := matchProjectDir(tc.name, projects)
		if got != tc.expected {
			t.Errorf("matchProjectDir(%q) = %q, want %q", tc.name, got, tc.expected)
		}
	}
}

func TestShellEscape(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "'simple'"},
		{"has space", "'has space'"},
		{"it's", "'it'\"'\"'s'"},
	}
	for _, tc := range tests {
		got := shellEscape(tc.input)
		if got != tc.expected {
			t.Errorf("shellEscape(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}
