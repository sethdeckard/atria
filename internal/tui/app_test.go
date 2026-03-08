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
	// Agents dashboard only shows rows with sessions
	for _, p := range store.Projects {
		store.SetSession(&model.AgentSession{
			ProjectDir: p.Dir,
			SessionID:  "sess-" + p.Name,
			Type:       model.AgentClaude,
			Status:     model.StatusIdle,
		})
	}
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
	m.availableAgents = []model.AgentType{model.AgentClaude}
	m.defaultAgent = model.AgentClaude
	// backendOK is false by default

	updated, cmd := m.Update(keyMsg("l"))
	um := modelFrom(updated)
	if cmd != nil {
		t.Error("expected no command when backend unavailable")
	}
	if !strings.Contains(um.statusText, "Backend unavailable") {
		t.Errorf("expected unavailable message, got %q", um.statusText)
	}
}

func TestLaunchWorksWithoutDetectedAgents(t *testing.T) {
	store := makeStore(t)
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 80
	m.backendOK = true
	m.availableAgents = nil
	m.defaultAgent = model.AgentClaude

	_, cmd := m.Update(keyMsg("l"))
	if cmd == nil {
		t.Error("expected listDirs command even without detected agents")
	}
}

func TestLaunchOpensProjectPicker(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	mb := &mockBackend{newSessionID: "sess-1"}
	m := newTestModelWithStore(mb, store)
	m.width = 80
	m.backendOK = true
	m.availableAgents = []model.AgentType{model.AgentClaude}
	m.defaultAgent = model.AgentClaude

	// l key should open the project picker (listDirs command)
	_, cmd := m.Update(keyMsg("l"))
	if cmd == nil {
		t.Fatal("expected a listDirs command")
	}
}

func TestToggleAgent(t *testing.T) {
	store := makeStore(t)
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 80
	m.availableAgents = []model.AgentType{model.AgentClaude, model.AgentCodex}
	m.defaultAgent = model.AgentClaude

	updated, _ := m.Update(keyMsg("t"))
	um := modelFrom(updated)
	if um.defaultAgent != model.AgentCodex {
		t.Errorf("expected codex after toggle, got %q", um.defaultAgent)
	}
	if !strings.Contains(um.statusText, "Default agent: Codex") {
		t.Errorf("expected status message, got %q", um.statusText)
	}

	// Toggle again — should cycle back
	updated, _ = um.Update(keyMsg("t"))
	um = modelFrom(updated)
	if um.defaultAgent != model.AgentClaude {
		t.Errorf("expected claude after second toggle, got %q", um.defaultAgent)
	}
}

func TestToggleHiddenWithOneAgent(t *testing.T) {
	store := makeStore(t)
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 80
	m.availableAgents = []model.AgentType{model.AgentClaude}
	m.defaultAgent = model.AgentClaude

	updated, cmd := m.Update(keyMsg("t"))
	um := modelFrom(updated)
	if um.defaultAgent != model.AgentClaude {
		t.Errorf("expected claude unchanged, got %q", um.defaultAgent)
	}
	if cmd != nil {
		t.Error("expected no command for single agent toggle")
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
	proj := um.store.FindProject("/a/myproject")
	if proj == nil || proj.LastLaunchedAt.IsZero() {
		t.Error("expected LastLaunchedAt to be set on successful launch")
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
	for _, p := range store.Projects {
		store.SetSession(&model.AgentSession{
			ProjectDir: p.Dir,
			SessionID:  "sess-" + p.Name,
			Type:       model.AgentClaude,
			Status:     model.StatusIdle,
		})
	}
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
	for _, p := range store.Projects {
		store.SetSession(&model.AgentSession{
			ProjectDir: p.Dir,
			SessionID:  "sess-" + p.Name,
			Type:       model.AgentClaude,
			Status:     model.StatusIdle,
		})
	}
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 80
	m.cursor = 1 // select last

	updated, _ := m.Update(keyMsg("d"))
	um := modelFrom(updated)
	if um.cursor != 0 {
		t.Errorf("expected cursor to adjust to 0, got %d", um.cursor)
	}
}

func TestDeleteProjectRemovesSessions(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/a/myproject",
		SessionID:  "sess-1",
		Type:       model.AgentClaude,
		Status:     model.StatusWorking,
	})
	store.SetSession(&model.AgentSession{
		ProjectDir: "/a/myproject",
		SessionID:  "sess-2",
		Type:       model.AgentCodex,
		Status:     model.StatusIdle,
	})
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 80

	updated, _ := m.Update(keyMsg("d"))
	um := modelFrom(updated)
	if len(um.store.Sessions) != 0 {
		t.Errorf("expected 0 sessions after delete, got %d", len(um.store.Sessions))
	}
}

func TestOpenChatNoRows(t *testing.T) {
	store := makeStore(t)
	// No projects/sessions — empty dashboard
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 80

	updated, _ := m.Update(keyMsg("s"))
	um := modelFrom(updated)
	if um.view != viewProjectList {
		t.Error("expected to stay on project list when no rows")
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
	if um.chatSessionID != "sess-1" {
		t.Errorf("expected chatSessionID sess-1, got %q", um.chatSessionID)
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

func TestFocusNoRows(t *testing.T) {
	store := makeStore(t)
	// No sessions — empty dashboard
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 80
	m.backendOK = true

	_, cmd := m.Update(keyMsg("f"))
	if cmd != nil {
		t.Error("expected no command when no rows")
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
		{Path: "/watch", Name: "..", IsParent: true},
		{Path: "/watch/sub/alpha", Name: "alpha"},
		{Path: "/watch/sub/beta", Name: "beta"},
	}
	updated, _ := m.Update(DirBrowserMsg{Dirs: dirs, CurrentDir: "/watch/sub"})
	um := modelFrom(updated)
	if um.view != viewDirBrowser {
		t.Errorf("expected dir browser view, got %d", um.view)
	}
	if len(um.browserDirs) != 3 {
		t.Errorf("expected 3 browser dirs, got %d", len(um.browserDirs))
	}
	if um.browserCursor != 0 {
		t.Errorf("expected browser cursor at 0, got %d", um.browserCursor)
	}
	if um.browserPath != "/watch/sub" {
		t.Errorf("expected browserPath /watch/sub, got %q", um.browserPath)
	}
}

func TestDirBrowserEnterOnDirDescends(t *testing.T) {
	store := makeStore(t)
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 80
	m.view = viewDirBrowser
	m.browserPath = "/watch"
	m.browserDirs = []DirBrowserItem{
		{Path: "/", Name: "..", IsParent: true},
		{Path: "/watch/alpha", Name: "alpha"},
	}
	m.browserCursor = 1 // on "alpha"

	// Enter on regular dir should descend, not launch
	_, cmd := m.Update(ctrlKeyMsg(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("expected listDir command for descend")
	}
}

func TestDirBrowserLaunchAction(t *testing.T) {
	store := makeStore(t)
	mb := &mockBackend{newSessionID: "sess-1"}
	m := newTestModelWithStore(mb, store)
	m.width = 80
	m.backendOK = true
	m.availableAgents = []model.AgentType{model.AgentClaude}
	m.defaultAgent = model.AgentClaude
	m.view = viewDirBrowser
	m.browserPath = "/watch/alpha"
	m.browserDirs = []DirBrowserItem{
		{Path: "/watch", Name: "..", IsParent: true},
	}
	// Launch action is last item: 0 dirs entries + 1 = index 1
	m.browserCursor = 1

	// Enter on launch action launches in current browserPath
	updated, cmd := m.Update(ctrlKeyMsg(tea.KeyEnter))
	um := modelFrom(updated)
	if um.view != viewProjectList {
		t.Errorf("expected return to project list, got %d", um.view)
	}
	if um.store.FindProject("/watch/alpha") == nil {
		t.Error("expected alpha project to be added")
	}
	if !strings.Contains(um.statusText, "Launching Claude for alpha") {
		t.Errorf("expected launching message, got %q", um.statusText)
	}
	if cmd == nil {
		t.Error("expected launch command")
	}
	// LastLaunchedAt is not set yet — it's set on AgentLaunchedMsg success
	proj := um.store.FindProject("/watch/alpha")
	if !proj.LastLaunchedAt.IsZero() {
		t.Error("expected LastLaunchedAt to not be set before launch succeeds")
	}
}

func TestDirBrowserEnterOnParentDescends(t *testing.T) {
	store := makeStore(t)
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 80
	m.view = viewDirBrowser
	m.browserPath = "/watch/sub"
	m.browserDirs = []DirBrowserItem{
		{Path: "/watch", Name: "..", IsParent: true},
		{Path: "/watch/sub/alpha", Name: "alpha"},
	}

	// Enter on ".." should navigate up
	_, cmd := m.Update(ctrlKeyMsg(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("expected listDir command for parent navigation")
	}
}

func TestDirBrowserDescendWithL(t *testing.T) {
	store := makeStore(t)
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 80
	m.view = viewDirBrowser
	m.browserPath = "/watch"
	m.browserDirs = []DirBrowserItem{
		{Path: "/", Name: "..", IsParent: true},
		{Path: "/watch/alpha", Name: "alpha"},
	}
	m.browserCursor = 1 // on "alpha"

	// l should descend into alpha
	_, cmd := m.Update(keyMsg("l"))
	if cmd == nil {
		t.Fatal("expected listDir command for descend")
	}
}

func TestDirBrowserBackWithH(t *testing.T) {
	store := makeStore(t)
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 80
	m.view = viewDirBrowser
	m.browserPath = "/watch/sub"
	m.browserDirs = []DirBrowserItem{
		{Path: "/watch", Name: "..", IsParent: true},
	}

	// h should navigate up
	_, cmd := m.Update(keyMsg("h"))
	if cmd == nil {
		t.Fatal("expected listDir command for back navigation")
	}
}

func TestDirBrowserRootBoundary(t *testing.T) {
	store := makeStore(t)
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 80
	m.view = viewDirBrowser
	m.browserPath = "/"
	m.browserDirs = []DirBrowserItem{
		{Path: "/usr", Name: "usr"},
	}

	// h at root should do nothing
	_, cmd := m.Update(keyMsg("h"))
	if cmd != nil {
		t.Error("expected no command at root boundary")
	}
}

func TestDirBrowserRecentProjects(t *testing.T) {
	store := makeStore(t)
	store.Projects = append(store.Projects, &model.Project{
		Name:           "recent-proj",
		Dir:            "/a/recent-proj",
		AddedAt:        time.Now(),
		LastLaunchedAt: time.Now(),
	})
	mb := &mockBackend{newSessionID: "sess-1"}
	m := newTestModelWithStore(mb, store)
	m.width = 80
	m.backendOK = true
	m.availableAgents = []model.AgentType{model.AgentClaude}
	m.defaultAgent = model.AgentClaude
	m.view = viewDirBrowser
	m.browserPath = "/watch"
	m.browserDirs = []DirBrowserItem{
		{Path: "/", Name: "..", IsParent: true},
		{Path: "/watch/alpha", Name: "alpha"},
	}

	// Cursor 0 is the recent project. Enter should launch it
	updated, cmd := m.Update(ctrlKeyMsg(tea.KeyEnter))
	um := modelFrom(updated)
	if um.view != viewProjectList {
		t.Errorf("expected return to project list, got %d", um.view)
	}
	if !strings.Contains(um.statusText, "Launching Claude for recent-proj") {
		t.Errorf("expected launching message, got %q", um.statusText)
	}
	if cmd == nil {
		t.Error("expected launch command")
	}
}

func TestDirBrowserToggleAgent(t *testing.T) {
	store := makeStore(t)
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 80
	m.view = viewDirBrowser
	m.browserPath = "/watch"
	m.browserDirs = []DirBrowserItem{
		{Path: "/", Name: "..", IsParent: true},
	}
	m.availableAgents = []model.AgentType{model.AgentClaude, model.AgentCodex}
	m.defaultAgent = model.AgentClaude

	updated, _ := m.Update(keyMsg("t"))
	um := modelFrom(updated)
	if um.defaultAgent != model.AgentCodex {
		t.Errorf("expected codex after toggle, got %q", um.defaultAgent)
	}
	if um.view != viewDirBrowser {
		t.Error("expected to stay in browser after toggle")
	}
}

func TestDirBrowserScroll(t *testing.T) {
	store := makeStore(t)
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 80
	m.height = 12 // small terminal
	m.view = viewDirBrowser
	m.browserPath = "/watch"

	// Create many directories to exceed visible area
	var dirs []DirBrowserItem
	dirs = append(dirs, DirBrowserItem{Path: "/", Name: "..", IsParent: true})
	for i := 0; i < 20; i++ {
		dirs = append(dirs, DirBrowserItem{
			Path: fmt.Sprintf("/watch/dir%02d", i),
			Name: fmt.Sprintf("dir%02d", i),
		})
	}
	m.browserDirs = dirs

	if m.browserScroll != 0 {
		t.Fatalf("expected initial scroll 0, got %d", m.browserScroll)
	}

	// Navigate to the bottom
	var updated tea.Model
	for i := 0; i < 21; i++ { // 21 dirs + 1 launch = 22 items
		updated, _ = m.Update(keyMsg("j"))
		m = modelFrom(updated)
	}
	if m.browserScroll == 0 {
		t.Error("expected scroll to increase when cursor passes visible area")
	}

	// Navigate back to top
	for m.browserCursor > 0 {
		updated, _ = m.Update(keyMsg("k"))
		m = modelFrom(updated)
	}
	if m.browserScroll != 0 {
		t.Errorf("expected scroll 0 at top, got %d", m.browserScroll)
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

func TestEnterOpensChat(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/a/myproject",
		SessionID:  "sess-1",
		Type:       model.AgentClaude,
		Status:     model.StatusIdle,
	})
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 80
	m.height = 40

	updated, _ := m.Update(ctrlKeyMsg(tea.KeyEnter))
	um := modelFrom(updated)
	if um.view != viewChat {
		t.Errorf("expected chat view, got %d", um.view)
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

func TestSessionsRefreshedRemovesExitedAgent(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/a/myproject",
		SessionID:  "sess-exited",
		Type:       model.AgentCodex,
		Status:     model.StatusIdle, // screen reads transitioned to idle
	})
	m := newTestModelWithStore(&mockBackend{}, store)

	// iTerm pane still alive but name no longer matches an agent (plain zsh)
	updated, _ := m.Update(SessionsRefreshedMsg{
		Sessions: []terminal.Session{
			{ID: "sess-exited", Name: "zsh"},
		},
	})
	um := modelFrom(updated)
	if um.store.SessionByID("sess-exited") != nil {
		t.Error("expected exited agent session to be removed")
	}
}

func TestSessionsRefreshedKeepsActiveAgentWithChangedName(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/a/myproject",
		SessionID:  "sess-working",
		Type:       model.AgentCodex,
		Status:     model.StatusWorking, // still working
	})
	m := newTestModelWithStore(&mockBackend{}, store)

	// Even if name doesn't match agent, keep it while still working
	updated, _ := m.Update(SessionsRefreshedMsg{
		Sessions: []terminal.Session{
			{ID: "sess-working", Name: "zsh"},
		},
	})
	um := modelFrom(updated)
	if um.store.SessionByID("sess-working") == nil {
		t.Error("expected working session to be kept even with non-agent name")
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
	m.chatSessionID = "sess-1"
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
		SessionID:  "sess-1",
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

func TestScreenReadIdleTransitionWithRecentActivity(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	store.SetSession(&model.AgentSession{
		ProjectDir:   "/a/myproject",
		SessionID:    "sess-1",
		Type:         model.AgentClaude,
		Status:       model.StatusWorking,
		LastActivity: time.Now(), // recent activity from session name
	})
	m := newTestModelWithStore(&mockBackend{}, store)

	// Screen changes to show idle prompt — should transition despite recent activity
	updated, _ := m.Update(ScreenReadMsg{
		SessionID:  "sess-1",
		ProjectDir: "/a/myproject",
		Content:    "some output\n\u276f",
	})
	um := modelFrom(updated)
	session := um.store.GetSession("/a/myproject")
	if session.Status != model.StatusIdle {
		t.Errorf("expected idle when screen shows prompt, got %q", session.Status)
	}
}

func TestScreenReadIdleUnchangedStillTransitions(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	store.SetSession(&model.AgentSession{
		ProjectDir:   "/a/myproject",
		SessionID:    "sess-1",
		Type:         model.AgentClaude,
		Status:       model.StatusWorking,
		LastActivity: time.Now(),
		LastScreen:   "some output\n\u276f", // same content as incoming
	})
	m := newTestModelWithStore(&mockBackend{}, store)

	// Same screen content showing idle — screen reads are authoritative,
	// should still transition even if content unchanged
	updated, _ := m.Update(ScreenReadMsg{
		SessionID:  "sess-1",
		ProjectDir: "/a/myproject",
		Content:    "some output\n\u276f",
	})
	um := modelFrom(updated)
	session := um.store.GetSession("/a/myproject")
	if session.Status != model.StatusIdle {
		t.Errorf("expected idle when screen shows prompt, got %q", session.Status)
	}
}

func TestScreenReadBlankTransitionsToIdle(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/a/myproject",
		SessionID:  "sess-1",
		Type:       model.AgentClaude,
		Status:     model.StatusWorking,
		LastScreen: "\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n", // previous blank
	})
	m := newTestModelWithStore(&mockBackend{}, store)

	// Second consecutive blank read — should transition to idle
	updated, _ := m.Update(ScreenReadMsg{
		SessionID:  "sess-1",
		ProjectDir: "/a/myproject",
		Content:    "\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n",
	})
	um := modelFrom(updated)
	session := um.store.GetSession("/a/myproject")
	if session.Status != model.StatusIdle {
		t.Errorf("expected idle after consecutive blank reads, got %q", session.Status)
	}
}

func TestScreenReadAgentExitedTransitionsToIdle(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/a/myproject",
		SessionID:  "sess-1",
		Type:       model.AgentCodex,
		Status:     model.StatusWorking,
		LastScreen: "› some codex prompt\n",
	})
	m := newTestModelWithStore(&mockBackend{}, store)

	shellContent := "myhost% \n"

	// Read 1: screen changed (from codex prompt to shell) — counter resets,
	// stays working because changing content means something is happening
	updated, _ := m.Update(ScreenReadMsg{
		SessionID:  "sess-1",
		ProjectDir: "/a/myproject",
		Content:    shellContent,
	})
	m = modelFrom(updated)
	session := m.store.SessionByID("sess-1")
	if session.Status != model.StatusWorking {
		t.Errorf("expected working after changed unmatched read, got %q", session.Status)
	}
	if session.UnmatchedReads != 0 {
		t.Errorf("expected counter=0 after changed read, got %d", session.UnmatchedReads)
	}

	// Reads 2-4: stable (same content), counter increments to 1, 2, 3
	for i := 1; i <= 3; i++ {
		updated, _ = m.Update(ScreenReadMsg{
			SessionID:  "sess-1",
			ProjectDir: "/a/myproject",
			Content:    shellContent,
		})
		m = modelFrom(updated)
		session = m.store.SessionByID("sess-1")
		if i < 3 && session.Status != model.StatusWorking {
			t.Errorf("read %d: expected working, got %q", i+1, session.Status)
		}
	}
	if session.Status != model.StatusIdle {
		t.Errorf("expected idle after 3 stable unmatched reads, got %q", session.Status)
	}
}

func TestScreenReadChangingOutputStaysWorking(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/a/myproject",
		SessionID:  "sess-1",
		Type:       model.AgentClaude,
		Status:     model.StatusWorking,
		LastScreen: "initial\n",
	})
	m := newTestModelWithStore(&mockBackend{}, store)

	// Many reads with changing content but no agent patterns — agent is
	// producing output, should stay working (counter resets each time)
	for i := 0; i < 10; i++ {
		updated, _ := m.Update(ScreenReadMsg{
			SessionID:  "sess-1",
			ProjectDir: "/a/myproject",
			Content:    fmt.Sprintf("plain output line %d\n", i),
		})
		m = modelFrom(updated)
	}
	session := m.store.SessionByID("sess-1")
	if session.Status != model.StatusWorking {
		t.Errorf("expected working with changing output, got %q", session.Status)
	}
	if session.UnmatchedReads != 0 {
		t.Errorf("expected counter=0 with changing output, got %d", session.UnmatchedReads)
	}
}

func TestScreenReadUnmatchedCounterResetsOnMatch(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/a/myproject",
		SessionID:  "sess-1",
		Type:       model.AgentClaude,
		Status:     model.StatusWorking,
		LastScreen: "✻ Reading…\n",
	})
	m := newTestModelWithStore(&mockBackend{}, store)

	// Read 1: changed content (from initial), no pattern — counter stays 0
	updated, _ := m.Update(ScreenReadMsg{
		SessionID:  "sess-1",
		ProjectDir: "/a/myproject",
		Content:    "some plain output\n",
	})
	m = modelFrom(updated)

	// Reads 2-3: stable, counter goes to 1 then 2
	for i := 0; i < 2; i++ {
		updated, _ = m.Update(ScreenReadMsg{
			SessionID:  "sess-1",
			ProjectDir: "/a/myproject",
			Content:    "some plain output\n",
		})
		m = modelFrom(updated)
	}
	session := m.store.SessionByID("sess-1")
	if session.UnmatchedReads != 2 {
		t.Fatalf("expected 2 unmatched reads, got %d", session.UnmatchedReads)
	}

	// Matched read resets counter
	updated, _ = m.Update(ScreenReadMsg{
		SessionID:  "sess-1",
		ProjectDir: "/a/myproject",
		Content:    "✻ Editing…\n",
	})
	m = modelFrom(updated)
	session = m.store.SessionByID("sess-1")
	if session.UnmatchedReads != 0 {
		t.Errorf("expected unmatched counter reset to 0, got %d", session.UnmatchedReads)
	}
	if session.Status != model.StatusWorking {
		t.Errorf("expected still working after matched read, got %q", session.Status)
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
	m.availableAgents = []model.AgentType{model.AgentClaude, model.AgentCodex}
	m.defaultAgent = model.AgentClaude
	v := m.View()
	if !strings.Contains(v, "agents") {
		t.Error("expected agents title")
	}
	if !strings.Contains(v, "Agent orchestration") {
		t.Error("expected tagline in empty state")
	}
	if !strings.Contains(v, "Launch an agent") {
		t.Error("expected launch hint in empty state")
	}
	if !strings.Contains(v, "Toggle agent type") {
		t.Error("expected toggle hint in empty state")
	}
}

func TestViewProjectListEmptySingleAgent(t *testing.T) {
	m := newTestModelWithStore(&mockBackend{}, makeStore(t))
	m.width = 80
	m.height = 40
	m.availableAgents = []model.AgentType{model.AgentClaude}
	m.defaultAgent = model.AgentClaude
	v := m.View()
	if !strings.Contains(v, "Launch an agent") {
		t.Error("expected launch hint in empty state")
	}
	if strings.Contains(v, "Toggle agent type") {
		t.Error("should not show toggle hint with single agent")
	}
}

func TestViewProjectListWithAgents(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/alpha", "/a/beta")
	for _, p := range store.Projects {
		store.SetSession(&model.AgentSession{
			ProjectDir: p.Dir,
			SessionID:  "sess-" + p.Name,
			Type:       model.AgentClaude,
			Status:     model.StatusIdle,
		})
	}
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 80
	m.height = 40
	m.availableAgents = []model.AgentType{model.AgentClaude}
	m.defaultAgent = model.AgentClaude
	v := m.View()
	if !strings.Contains(v, "alpha") {
		t.Error("expected alpha in view")
	}
	if !strings.Contains(v, "beta") {
		t.Error("expected beta in view")
	}
	if !strings.Contains(v, "2 agents") {
		t.Error("expected agent count")
	}
	if !strings.Contains(v, "l:launch (Claude)") {
		t.Error("expected launch hint with agent name")
	}
	// Directory should be inline in the row
	if !strings.Contains(v, "/a/alpha") {
		t.Error("expected path for alpha")
	}
	// Column headers
	if !strings.Contains(v, "agent") {
		t.Error("expected column header 'agent'")
	}
	if !strings.Contains(v, "harness") {
		t.Error("expected column header 'harness'")
	}
	// Branding
	if !strings.Contains(v, "atria") {
		t.Error("expected atria branding")
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
		{project: &model.Project{Name: "delta"}, session: &model.AgentSession{Status: model.StatusIdle}},
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
		{model.StatusIdle, "Atria Agent Orchestration", "", "Atria Agent Orchestration"},
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

func TestScrollOffset(t *testing.T) {
	store := makeStore(t)
	// Create enough projects to exceed a small terminal
	dirs := []string{"/a/p1", "/a/p2", "/a/p3", "/a/p4", "/a/p5", "/a/p6", "/a/p7", "/a/p8", "/a/p9", "/a/p10"}
	store.Projects = makeProjects(dirs...)
	for _, p := range store.Projects {
		store.SetSession(&model.AgentSession{
			ProjectDir: p.Dir,
			SessionID:  "sess-" + p.Name,
			Type:       model.AgentClaude,
			Status:     model.StatusIdle,
		})
	}
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 100
	m.height = 12 // small: header(4) + footer(2) = 6 overhead, so ~6 visible rows

	if m.scrollOffset != 0 {
		t.Fatalf("expected initial scrollOffset 0, got %d", m.scrollOffset)
	}

	// Navigate down past visible area
	var updated tea.Model
	for i := 0; i < 9; i++ {
		updated, _ = m.Update(keyMsg("j"))
		m = modelFrom(updated)
	}
	if m.cursor != 9 {
		t.Errorf("expected cursor at 9, got %d", m.cursor)
	}
	if m.scrollOffset == 0 {
		t.Error("expected scrollOffset to increase when cursor moves past visible area")
	}

	// Navigate back up
	for i := 0; i < 9; i++ {
		updated, _ = m.Update(keyMsg("k"))
		m = modelFrom(updated)
	}
	if m.cursor != 0 {
		t.Errorf("expected cursor at 0, got %d", m.cursor)
	}
	if m.scrollOffset != 0 {
		t.Errorf("expected scrollOffset 0 after scrolling back up, got %d", m.scrollOffset)
	}
}

func TestViewHeaderBranding(t *testing.T) {
	m := newTestModelWithStore(&mockBackend{}, makeStore(t))
	m.width = 80
	m.height = 40
	m.availableAgents = []model.AgentType{model.AgentClaude}
	m.defaultAgent = model.AgentClaude
	v := m.View()
	if !strings.Contains(v, "atria") {
		t.Error("expected atria branding in header")
	}
	if !strings.Contains(v, "agents") {
		t.Error("expected agents title in header")
	}
	// Separator
	if !strings.Contains(v, "\u2500") {
		t.Error("expected horizontal separator")
	}
}

func TestStreamToggle(t *testing.T) {
	store := makeStore(t)
	m := newTestModelWithStore(&mockBackend{}, store)
	m.backendOK = true

	if m.streamOpen {
		t.Fatal("streamOpen should default to false")
	}

	// Press v to open
	updated, _ := m.Update(keyMsg("v"))
	m = modelFrom(updated)
	if !m.streamOpen {
		t.Error("expected streamOpen=true after pressing v")
	}

	// Press v again to close
	updated, _ = m.Update(keyMsg("v"))
	m = modelFrom(updated)
	if m.streamOpen {
		t.Error("expected streamOpen=false after pressing v again")
	}
}

func TestMaxVisibleRowsShrinksWithStream(t *testing.T) {
	store := makeStore(t)
	m := newTestModelWithStore(&mockBackend{}, store)
	m.backendOK = true

	without := m.maxVisibleRows()

	m.streamOpen = true
	with := m.maxVisibleRows()

	if with >= without {
		t.Errorf("maxVisibleRows with stream (%d) should be less than without (%d)", with, without)
	}

	expected := streamPanelHeight(m.height) + 1 // +1 for spacer line
	if without-with != expected {
		t.Errorf("difference should be streamPanelHeight+1 (%d), got %d", expected, without-with)
	}
}

func TestStreamPanelShowsSelectedScreen(t *testing.T) {
	store := makeStore(t)
	store.AddProject("/proj/alpha")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/proj/alpha",
		SessionID:  "s1",
		Type:       model.AgentClaude,
		Status:     model.StatusWorking,
		LastScreen: "Reading file...\n\n❯\n",
	})
	m := newTestModelWithStore(&mockBackend{}, store)
	m.backendOK = true
	m.streamOpen = true

	v := m.View()
	if !strings.Contains(v, "Reading file...") {
		t.Error("expected stream panel to contain LastScreen content")
	}
}

func TestStreamPanelNoOutput(t *testing.T) {
	store := makeStore(t)
	store.AddProject("/proj/alpha")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/proj/alpha",
		SessionID:  "s1",
		Type:       model.AgentClaude,
		Status:     model.StatusIdle,
		LastScreen: "",
	})
	m := newTestModelWithStore(&mockBackend{}, store)
	m.backendOK = true
	m.streamOpen = true

	v := m.View()
	if !strings.Contains(v, "no output") {
		t.Error("expected 'no output' placeholder when LastScreen is empty")
	}
}

func TestStreamPanelUpdatesWithCursor(t *testing.T) {
	store := makeStore(t)
	store.AddProject("/proj/alpha")
	store.AddProject("/proj/beta")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/proj/alpha",
		SessionID:  "s1",
		Type:       model.AgentClaude,
		Status:     model.StatusWorking,
		LastScreen: "alpha screen content",
	})
	store.SetSession(&model.AgentSession{
		ProjectDir: "/proj/beta",
		SessionID:  "s2",
		Type:       model.AgentClaude,
		Status:     model.StatusIdle,
		LastScreen: "beta screen content",
	})
	m := newTestModelWithStore(&mockBackend{}, store)
	m.backendOK = true
	m.streamOpen = true

	// First row selected
	v := m.View()
	// Move cursor down
	updated, _ := m.Update(keyMsg("j"))
	m = modelFrom(updated)
	v = m.View()

	// The second session's screen should now be visible
	// (both sessions are in the list; exact order depends on sort)
	if !strings.Contains(v, "alpha screen content") && !strings.Contains(v, "beta screen content") {
		t.Error("expected stream panel to show some session's screen content after cursor move")
	}
}

func TestFooterShowsStreamHint(t *testing.T) {
	store := makeStore(t)
	store.AddProject("/proj/alpha")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/proj/alpha",
		SessionID:  "s1",
		Type:       model.AgentClaude,
		Status:     model.StatusIdle,
	})
	m := newTestModelWithStore(&mockBackend{}, store)
	m.backendOK = true

	v := m.View()
	if !strings.Contains(v, "v:stream") {
		t.Error("expected footer to contain v:stream hint")
	}

	m.streamOpen = true
	v = m.View()
	if !strings.Contains(v, "v:close") {
		t.Error("expected footer to contain v:close when stream is open")
	}
}

func TestStreamPanelTruncatesLongLines(t *testing.T) {
	longLine := strings.Repeat("x", 200)
	store := makeStore(t)
	store.AddProject("/proj/alpha")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/proj/alpha",
		SessionID:  "s1",
		Type:       model.AgentClaude,
		Status:     model.StatusWorking,
		LastScreen: longLine,
	})
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 80
	m.backendOK = true
	m.streamOpen = true

	v := m.View()
	// The full 200-char line should not appear; it should be truncated
	if strings.Contains(v, longLine) {
		t.Error("expected long line to be truncated, but it appeared in full")
	}
	if !strings.Contains(v, "\u2026") {
		t.Error("expected truncation ellipsis in stream panel")
	}
}

func TestStreamPanelHeight(t *testing.T) {
	// Small terminal
	h := streamPanelHeight(10)
	if h < 5 {
		t.Errorf("streamPanelHeight(10) = %d, want >= 5", h)
	}

	// Normal terminal
	h = streamPanelHeight(40)
	if h != 16 {
		t.Errorf("streamPanelHeight(40) = %d, want 16", h)
	}

	// Large terminal — capped at 25
	h = streamPanelHeight(80)
	if h != 25 {
		t.Errorf("streamPanelHeight(80) = %d, want 25 (capped)", h)
	}
}
