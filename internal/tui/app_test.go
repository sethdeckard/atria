package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sethdeckard/atria/internal/config"
	"github.com/sethdeckard/atria/internal/model"
	"github.com/sethdeckard/atria/internal/terminal"
)

// --- mock backend ---

type mockBackend struct {
	available     error
	sessions      []terminal.Session
	listErr       error
	newSessionID  string
	newSessionErr error
	sendTextLog   []string
	runCmdLog     []string
	focusLog      []string
	readScreenLog []string
	screenContent string
	screenByID    map[string]string
	getVarVal     string
	monitorPID    int
}

func (m *mockBackend) Available() error                          { return m.available }
func (m *mockBackend) ListSessions() ([]terminal.Session, error) { return m.sessions, m.listErr }
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
	m.readScreenLog = append(m.readScreenLog, fmt.Sprintf("%s:%d", sid, lines))
	if m.screenByID != nil {
		if content, ok := m.screenByID[sid]; ok {
			return content, nil
		}
	}
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

func collectMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	switch msg := msg.(type) {
	case tea.BatchMsg:
		var out []tea.Msg
		for _, nested := range msg {
			out = append(out, collectMsgs(nested)...)
		}
		return out
	default:
		return []tea.Msg{msg}
	}
}

func drainCmd(t *testing.T, m Model, cmd tea.Cmd, maxMsgs int) Model {
	t.Helper()
	queue := collectMsgs(cmd)
	for len(queue) > 0 && maxMsgs > 0 {
		msg := queue[0]
		queue = queue[1:]
		updated, next := m.Update(msg)
		m = modelFrom(updated)
		queue = append(queue, collectMsgs(next)...)
		maxMsgs--
	}
	return m
}

func makeStore(t *testing.T) *model.Store {
	return model.NewStore(t.TempDir())
}

func assertLinesWithinWidth(t *testing.T, content string, width int) {
	t.Helper()
	for i, line := range strings.Split(content, "\n") {
		if got := lipgloss.Width(line); got > width {
			t.Fatalf("line %d width = %d, want <= %d: %q", i, got, width, line)
		}
	}
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
	updated, cmd := m.Update(BackendAvailableMsg{Err: errors.New("cannot connect to iTerm2")})
	um := modelFrom(updated)
	if um.backendOK {
		t.Error("expected backendOK to be false")
	}
	if !strings.Contains(um.statusText, "cannot connect to iTerm2") {
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

	updated, cmd := m.Update(keyMsg("n"))
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

	_, cmd := m.Update(keyMsg("n"))
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
	_, cmd := m.Update(keyMsg("n"))
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

	session := um.store.FirstSession("/a/myproject")
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

	// Cursor should move to the newly launched agent.
	found := false
	for i, r := range um.rows {
		if r.session != nil && r.session.SessionID == "sess-1" {
			if um.cursor != i {
				t.Errorf("expected cursor at %d (new agent row), got %d", i, um.cursor)
			}
			found = true
			break
		}
	}
	if !found {
		t.Error("expected new agent to appear in rows")
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

func TestOpenChatNoRows(t *testing.T) {
	store := makeStore(t)
	// No projects/sessions — empty dashboard
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 80

	updated, _ := m.Update(keyMsg("enter"))
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

	updated, _ := m.Update(keyMsg("enter"))
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
	updated, _ := m.Update(keyMsg("enter"))
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

func TestDirBrowserDescendWithN(t *testing.T) {
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

	// n should descend into alpha
	_, cmd := m.Update(keyMsg("n"))
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

func TestDirBrowserRecentEnterNavigates(t *testing.T) {
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

	// Cursor 0 is the recent project. Enter should navigate, not launch
	updated, cmd := m.Update(ctrlKeyMsg(tea.KeyEnter))
	um := modelFrom(updated)
	if um.view != viewDirBrowser {
		t.Errorf("expected to stay in dir browser, got %d", um.view)
	}
	if um.browserSelectLaunchPath != "/a/recent-proj" {
		t.Errorf("expected browserSelectLaunchPath to be /a/recent-proj, got %q", um.browserSelectLaunchPath)
	}
	if cmd == nil {
		t.Error("expected listDir command")
	}
	if um.statusText != "" {
		t.Errorf("expected no status text, got %q", um.statusText)
	}
}

func TestDirBrowserRecentNavigateCursorOnLaunch(t *testing.T) {
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
	m.browserSelectLaunchPath = "/a/recent-proj"

	// Simulate DirBrowserMsg arriving after recent selection
	dirs := []DirBrowserItem{
		{Path: "/a", Name: "..", IsParent: true},
		{Path: "/a/recent-proj/sub", Name: "sub"},
	}
	updated, _ := m.Update(DirBrowserMsg{Dirs: dirs, CurrentDir: "/a/recent-proj"})
	um := modelFrom(updated)
	if um.browserSelectLaunchPath != "" {
		t.Error("expected browserSelectLaunchPath to be cleared")
	}
	// Cursor should be on the launch action: recentCount + len(dirs)
	recentCount := um.browserRecentCount()
	expectedCursor := recentCount + len(dirs)
	if um.browserCursor != expectedCursor {
		t.Errorf("expected cursor at launch action %d, got %d", expectedCursor, um.browserCursor)
	}
	if um.browserPath != "/a/recent-proj" {
		t.Errorf("expected browserPath /a/recent-proj, got %q", um.browserPath)
	}
}

func TestDirBrowserRecentSelectIgnoresMismatchedPath(t *testing.T) {
	store := makeStore(t)
	mb := &mockBackend{newSessionID: "sess-1"}
	m := newTestModelWithStore(mb, store)
	m.width = 80
	m.backendOK = true
	m.availableAgents = []model.AgentType{model.AgentClaude}
	m.defaultAgent = model.AgentClaude
	m.view = viewDirBrowser
	// Flag was set for /a/recent-proj, but user navigated elsewhere before it arrived
	m.browserSelectLaunchPath = "/a/recent-proj"

	// DirBrowserMsg arrives for a different path
	dirs := []DirBrowserItem{
		{Path: "/b", Name: "..", IsParent: true},
		{Path: "/b/other/sub", Name: "sub"},
	}
	updated, _ := m.Update(DirBrowserMsg{Dirs: dirs, CurrentDir: "/b/other"})
	um := modelFrom(updated)
	if um.browserSelectLaunchPath != "" {
		t.Error("expected browserSelectLaunchPath to be cleared even on mismatch")
	}
	// Cursor should NOT jump to launch action — should stay at default 0
	if um.browserCursor != 0 {
		t.Errorf("expected cursor at 0 for mismatched path, got %d", um.browserCursor)
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

func TestDirBrowserLaunchChoiceRendering(t *testing.T) {
	store := makeStore(t)
	mb := &mockBackend{newSessionID: "sess-1"}
	m := newTestModelWithStore(mb, store)
	m.width = 80
	m.height = 40
	m.view = viewDirBrowser
	m.browserPath = "/watch/alpha"
	m.browserDirs = []DirBrowserItem{
		{Path: "/watch", Name: "..", IsParent: true},
	}
	m.availableAgents = []model.AgentType{model.AgentClaude}
	m.defaultAgent = model.AgentClaude

	// Set up composite with tmux as primary and PTY as integration.
	ptyBackend := &mockBackend{newSessionID: "pty-0"}
	tmuxBackend := &mockBackend{newSessionID: "tmux-0"}
	comp := terminal.NewCompositeBackend(tmuxBackend, "tmux", []terminal.Integration{
		{Prefix: "pty:", Source: "pty", Backend: ptyBackend},
	})
	m.backend = terminal.NewCachedBackend(comp, 5)

	v := m.viewDirBrowser()
	if !strings.Contains(v, "(tmux)") {
		t.Errorf("expected '(tmux)' in browser, got:\n%s", v)
	}
	if !strings.Contains(v, "(embedded)") {
		t.Errorf("expected '(embedded)' in browser, got:\n%s", v)
	}
}

func TestDirBrowserHeaderAgentHint(t *testing.T) {
	store := makeStore(t)
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 80
	m.height = 40
	m.view = viewDirBrowser
	m.browserPath = "/watch"
	m.browserDirs = []DirBrowserItem{
		{Path: "/", Name: "..", IsParent: true},
	}

	// Multiple agents — header should show cycle hint near agent name.
	m.availableAgents = []model.AgentType{model.AgentClaude, model.AgentCodex}
	m.defaultAgent = model.AgentClaude
	v := m.viewDirBrowser()
	if !strings.Contains(v, "t:cycle") {
		t.Errorf("expected 't:cycle' in header with multiple agents, got:\n%s", v)
	}

	// Single agent — no cycle hint.
	m.availableAgents = []model.AgentType{model.AgentClaude}
	v = m.viewDirBrowser()
	if strings.Contains(v, "t:cycle") {
		t.Errorf("should not show 't:cycle' with single agent, got:\n%s", v)
	}
}

func TestDirBrowserPathNearLaunchAction(t *testing.T) {
	store := makeStore(t)
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 80
	m.height = 40
	m.view = viewDirBrowser
	m.browserPath = "/watch/myproject"
	m.browserDirs = []DirBrowserItem{
		{Path: "/watch", Name: "..", IsParent: true},
	}
	m.availableAgents = []model.AgentType{model.AgentClaude}
	m.defaultAgent = model.AgentClaude

	v := m.viewDirBrowser()
	// Path should appear near the launch action, not just in header
	launchIdx := strings.Index(v, "launch Claude here")
	pathIdx := strings.LastIndex(v[:launchIdx], "/watch/myproject")
	if pathIdx == -1 {
		t.Errorf("expected path label near launch action, got:\n%s", v)
	}
}

func TestDirBrowserNoChoiceRendering(t *testing.T) {
	store := makeStore(t)
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 80
	m.height = 40
	m.view = viewDirBrowser
	m.browserPath = "/watch/alpha"
	m.browserDirs = []DirBrowserItem{
		{Path: "/watch", Name: "..", IsParent: true},
	}
	m.availableAgents = []model.AgentType{model.AgentClaude}
	m.defaultAgent = model.AgentClaude

	v := m.viewDirBrowser()
	if strings.Contains(v, "(embedded)") {
		t.Errorf("should not show (embedded) without launch choice, got:\n%s", v)
	}
	if strings.Contains(v, "(pty)") {
		t.Errorf("should not show (pty) suffix without launch choice, got:\n%s", v)
	}
	if !strings.Contains(v, "launch Claude here") {
		t.Errorf("expected 'launch Claude here' without suffix, got:\n%s", v)
	}
}

func TestDirBrowserLaunchEmbedded(t *testing.T) {
	store := makeStore(t)
	mb := &mockBackend{newSessionID: "pty-0"}
	tmuxBackend := &mockBackend{newSessionID: "tmux-0"}
	comp := terminal.NewCompositeBackend(tmuxBackend, "tmux", []terminal.Integration{
		{Prefix: "pty:", Source: "pty", Backend: mb},
	})
	cached := terminal.NewCachedBackend(comp, 5)

	m := newTestModelWithStore(&mockBackend{}, store)
	m.backend = cached
	m.width = 80
	m.height = 40
	m.backendOK = true
	m.availableAgents = []model.AgentType{model.AgentClaude}
	m.defaultAgent = model.AgentClaude
	m.view = viewDirBrowser
	m.browserPath = "/watch/alpha"
	m.browserDirs = []DirBrowserItem{
		{Path: "/watch", Name: "..", IsParent: true},
	}
	// Cursor on embedded action (dirs=1, launchIdx=1, embeddedIdx=2)
	m.browserCursor = 2

	updated, cmd := m.Update(ctrlKeyMsg(tea.KeyEnter))
	um := modelFrom(updated)
	if um.view != viewProjectList {
		t.Errorf("expected return to project list, got %d", um.view)
	}
	if !strings.Contains(um.statusText, "Launching") {
		t.Errorf("expected launching message, got %q", um.statusText)
	}
	if cmd == nil {
		t.Error("expected launch command")
	}
}

func TestDirBrowserLaunchIntegration(t *testing.T) {
	store := makeStore(t)
	mb := &mockBackend{newSessionID: "pty-0"}
	tmuxBackend := &mockBackend{newSessionID: "tmux-0"}
	comp := terminal.NewCompositeBackend(tmuxBackend, "tmux", []terminal.Integration{
		{Prefix: "pty:", Source: "pty", Backend: mb},
	})
	cached := terminal.NewCachedBackend(comp, 5)

	m := newTestModelWithStore(&mockBackend{}, store)
	m.backend = cached
	m.width = 80
	m.height = 40
	m.backendOK = true
	m.availableAgents = []model.AgentType{model.AgentClaude}
	m.defaultAgent = model.AgentClaude
	m.view = viewDirBrowser
	m.browserPath = "/watch/alpha"
	m.browserDirs = []DirBrowserItem{
		{Path: "/watch", Name: "..", IsParent: true},
	}
	// Cursor on primary launch action (launchIdx=1)
	m.browserCursor = 1

	updated, cmd := m.Update(ctrlKeyMsg(tea.KeyEnter))
	um := modelFrom(updated)
	if um.view != viewProjectList {
		t.Errorf("expected return to project list, got %d", um.view)
	}
	if !strings.Contains(um.statusText, "Launching") {
		t.Errorf("expected launching message, got %q", um.statusText)
	}
	if cmd == nil {
		t.Error("expected launch command")
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

func TestDirBrowserEscapeClearsPendingRecentSelection(t *testing.T) {
	m := newTestModelWithStore(&mockBackend{}, makeStore(t))
	m.width = 80
	m.view = viewDirBrowser
	m.browserSelectLaunchPath = "/some/path"

	updated, _ := m.Update(ctrlKeyMsg(tea.KeyEscape))
	um := modelFrom(updated)
	if um.browserSelectLaunchPath != "" {
		t.Error("expected browserSelectLaunchPath to be cleared on Escape")
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
	session := um.store.FirstSession("/a/myproject")
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
	if um.store.FirstSession("/a/myproject") != nil {
		t.Error("expected dead session to be removed")
	}
}

func TestSessionsRefreshedKeepsSessionFromFailedIntegration(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/a/myproject",
		SessionID:  "iterm:sess-abc",
		Type:       model.AgentClaude,
		Source:     "iterm",
		Status:     model.StatusWorking,
	})

	// Set up composite with a failing iterm integration.
	ptyBackend := &mockBackend{sessions: []terminal.Session{}}
	itermBackend := &mockBackend{listErr: errors.New("connection refused")}
	comp := terminal.NewCompositeBackend(ptyBackend, "pty", []terminal.Integration{
		{Prefix: "iterm:", Source: "iterm", Backend: itermBackend},
	})
	m := newTestModelWithStore(&mockBackend{}, store)
	cached := terminal.NewCachedBackend(comp, 5)
	m.backend = cached

	// Trigger ListSessions on the composite to populate failedSources
	// (mirrors what refreshSessions does before returning SessionsRefreshedMsg).
	sessions, _ := cached.ListSessions()

	// Refresh returns only primary sessions (iterm failed).
	// The iterm session should survive because iterm is in failedSources.
	updated, _ := m.Update(SessionsRefreshedMsg{Sessions: sessions})
	um := modelFrom(updated)
	if um.store.SessionByID("iterm:sess-abc") == nil {
		t.Error("expected iterm session to survive when integration failed")
	}
}

func TestSessionsRefreshedRemovesExitedAgent(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	store.SetSession(&model.AgentSession{
		ProjectDir:    "/a/myproject",
		SessionID:     "sess-exited",
		Type:          model.AgentCodex,
		Status:        model.StatusIdle,
		OrphanTicks:   1,               // one tick already accumulated, next refresh triggers removal
		LastScreen:    "user@host ~ %", // shell prompt, no agent UI
		ScreenChecked: true,
	})
	m := newTestModelWithStore(&mockBackend{}, store)

	// Pane still alive but name no longer matches an agent (plain zsh)
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

func TestSessionsRefreshedOrphanFullCycle(t *testing.T) {
	// End-to-end: idle + generic title + no agent screen => removed after 2 refreshes.
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	store.SetSession(&model.AgentSession{
		ProjectDir:    "/a/myproject",
		SessionID:     "sess-orphan",
		Type:          model.AgentCodex,
		Status:        model.StatusIdle,
		LastScreen:    "user@host ~ %",
		ScreenChecked: true,
	})
	m := newTestModelWithStore(&mockBackend{}, store)

	refreshMsg := SessionsRefreshedMsg{
		Sessions: []terminal.Session{
			{ID: "sess-orphan", Name: "~/projects/myproject"},
		},
	}

	// First refresh: OrphanTicks goes from 0 → 1, session kept.
	updated, _ := m.Update(refreshMsg)
	um := modelFrom(updated)
	sess := um.store.SessionByID("sess-orphan")
	if sess == nil {
		t.Fatal("expected session to survive first refresh")
	}
	if sess.OrphanTicks != 1 {
		t.Fatalf("expected OrphanTicks=1 after first refresh, got %d", sess.OrphanTicks)
	}

	// Second refresh: OrphanTicks goes from 1 → 2, session removed.
	updated2, _ := um.Update(refreshMsg)
	um2 := modelFrom(updated2)
	if um2.store.SessionByID("sess-orphan") != nil {
		t.Error("expected orphan session to be removed after second refresh")
	}
}

func TestSessionsRefreshedKeepsIdleAgentWithScreenUI(t *testing.T) {
	// idle + generic title + agent prompt still visible => retained across refreshes.
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	store.SetSession(&model.AgentSession{
		ProjectDir:    "/a/myproject",
		SessionID:     "sess-idle",
		Type:          model.AgentClaude,
		Status:        model.StatusIdle,
		ScreenChecked: true,
		LastScreen:    "some conversation output\n\n❯ ", // Claude prompt in bottom region
	})
	m := newTestModelWithStore(&mockBackend{}, store)

	refreshMsg := SessionsRefreshedMsg{
		Sessions: []terminal.Session{
			{ID: "sess-idle", Name: "Editing foo.go"},
		},
	}

	// First refresh: screen shows agent UI, OrphanTicks stays 0.
	updated, _ := m.Update(refreshMsg)
	um := modelFrom(updated)
	sess := um.store.SessionByID("sess-idle")
	if sess == nil {
		t.Fatal("expected idle Claude session to survive first refresh")
	}
	if sess.OrphanTicks != 0 {
		t.Errorf("expected OrphanTicks=0 when screen shows agent UI, got %d", sess.OrphanTicks)
	}

	// Second refresh: still retained, OrphanTicks still 0.
	updated2, _ := um.Update(refreshMsg)
	um2 := modelFrom(updated2)
	sess2 := um2.store.SessionByID("sess-idle")
	if sess2 == nil {
		t.Fatal("expected idle Claude session to survive second refresh")
	}
	if sess2.OrphanTicks != 0 {
		t.Errorf("expected OrphanTicks=0 after second refresh, got %d", sess2.OrphanTicks)
	}
}

func TestSessionsRefreshedRemovesIdleITermShellFallback(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	store.SetSession(&model.AgentSession{
		ProjectDir:    "/a/myproject",
		SessionID:     "iterm:sess-shell",
		Type:          model.AgentClaude,
		Source:        "iterm",
		Status:        model.StatusIdle,
		ScreenChecked: true,
		LastScreen:    "old Claude output\n\n❯ ",
	})
	m := newTestModelWithStore(&mockBackend{}, store)

	refreshMsg := SessionsRefreshedMsg{
		Sessions: []terminal.Session{
			{ID: "iterm:sess-shell", Name: "..ts/go/loadout (-zsh)", Source: "iterm", Job: "zsh"},
		},
	}

	updated, _ := m.Update(refreshMsg)
	um := modelFrom(updated)
	sess := um.store.SessionByID("iterm:sess-shell")
	if sess == nil {
		t.Fatal("expected session to survive first refresh")
	}
	if sess.OrphanTicks != 1 {
		t.Fatalf("expected OrphanTicks=1 after shell fallback, got %d", sess.OrphanTicks)
	}

	updated2, _ := um.Update(refreshMsg)
	um2 := modelFrom(updated2)
	if um2.store.SessionByID("iterm:sess-shell") != nil {
		t.Error("expected idle iTerm shell fallback session to be removed after second refresh")
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

func TestSessionsRefreshedRetypesAgent(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/a/myproject",
		SessionID:  "sess-retyped",
		Type:       model.AgentClaude,
		Status:     model.StatusIdle,
	})
	m := newTestModelWithStore(&mockBackend{}, store)

	// Session name now indicates codex instead of claude
	updated, _ := m.Update(SessionsRefreshedMsg{
		Sessions: []terminal.Session{
			{ID: "sess-retyped", Name: "codex"},
		},
	})
	um := modelFrom(updated)
	as := um.store.SessionByID("sess-retyped")
	if as == nil {
		t.Fatal("expected session to still exist")
	}
	if as.Type != model.AgentCodex {
		t.Errorf("expected type %q after retype, got %q", model.AgentCodex, as.Type)
	}
}

func TestSessionsRefreshedRetypeKeepsTypeWhenNameUnchanged(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/a/myproject",
		SessionID:  "sess-stable",
		Type:       model.AgentClaude,
		Status:     model.StatusWorking,
	})
	m := newTestModelWithStore(&mockBackend{}, store)

	// Session name still indicates claude — type should not change
	updated, _ := m.Update(SessionsRefreshedMsg{
		Sessions: []terminal.Session{
			{ID: "sess-stable", Name: "✳ Editing main.go (claude)"},
		},
	})
	um := modelFrom(updated)
	as := um.store.SessionByID("sess-stable")
	if as == nil {
		t.Fatal("expected session to still exist")
	}
	if as.Type != model.AgentClaude {
		t.Errorf("expected type to remain %q, got %q", model.AgentClaude, as.Type)
	}
}

func TestSessionsRefreshedRetypeIgnoresNonAgentName(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/a/myproject",
		SessionID:  "sess-noagent",
		Type:       model.AgentClaude,
		Status:     model.StatusIdle,
	})
	m := newTestModelWithStore(&mockBackend{}, store)

	// Session name is now "zsh" — not an agent, so type should NOT change
	// (orphan removal handles this separately)
	updated, _ := m.Update(SessionsRefreshedMsg{
		Sessions: []terminal.Session{
			{ID: "sess-noagent", Name: "zsh"},
		},
	})
	um := modelFrom(updated)
	as := um.store.SessionByID("sess-noagent")
	if as == nil {
		t.Fatal("expected session to still exist")
	}
	if as.Type != model.AgentClaude {
		t.Errorf("expected type to remain %q when name is non-agent, got %q", model.AgentClaude, as.Type)
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
	session := um.store.FirstSession("/a/discovered")
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

func TestSessionsRefreshedDiscoversUnknownTitleByScreen(t *testing.T) {
	store := makeStore(t)
	backend := &mockBackend{
		getVarVal: "/watch/project",
		screenByID: map[string]string{
			"sess-claude-title":   "Claude Code\n❯ \n? for shortcuts",
			"sess-codex":          "gpt-5.4-codex default · 92% left\n› Improve docs",
			"sess-neutral-claude": "Claude Code\n* Germinating…\n❯ Try \"fix tests\"",
		},
	}
	m := newTestModelWithStore(backend, store)

	updated, cmd := m.Update(SessionsRefreshedMsg{
		Sessions: []terminal.Session{
			{ID: "sess-claude-title", Name: "claude"},
			{ID: "sess-codex", Name: "codex"},
			{ID: "sess-neutral-claude", Name: "Research data pipeline (caffeinate)"},
		},
	})
	um := drainCmd(t, modelFrom(updated), cmd, 20)

	if got := len(um.store.Sessions); got != 3 {
		t.Fatalf("expected 3 discovered sessions, got %d", got)
	}
	if got := len(um.rows); got != 3 {
		t.Fatalf("expected 3 rows, got %d", got)
	}

	var claudeCount, codexCount int
	for _, sess := range um.store.Sessions {
		switch sess.Type {
		case model.AgentClaude:
			claudeCount++
		case model.AgentCodex:
			codexCount++
		}
	}
	if claudeCount != 2 {
		t.Errorf("expected 2 Claude sessions, got %d", claudeCount)
	}
	if codexCount != 1 {
		t.Errorf("expected 1 Codex session, got %d", codexCount)
	}
	if got := len(backend.readScreenLog); got != 1 {
		t.Fatalf("expected one screen read for unknown-title session, got %d", got)
	}
	if backend.readScreenLog[0] != "sess-neutral-claude:40" {
		t.Errorf("expected screen read for neutral title, got %q", backend.readScreenLog[0])
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
		SessionID:  "sess-1",
		ProjectDir: "/a/myproject",
		Status:     model.StatusNeedsInput,
		Attention:  "Allow file edit? [y/n]",
	})
	um := modelFrom(updated)
	session := um.store.SessionByID("sess-1")
	if session.Status != model.StatusNeedsInput {
		t.Errorf("expected needs_input, got %q", session.Status)
	}
	if session.Attention != "Allow file edit? [y/n]" {
		t.Errorf("expected attention text, got %q", session.Attention)
	}
}

func TestStatusUpdatedMultipleAgentsSameDir(t *testing.T) {
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
		Type:       model.AgentClaude,
		Status:     model.StatusWorking,
	})
	m := newTestModelWithStore(&mockBackend{}, store)

	// Update only sess-2 — sess-1 should remain unchanged.
	updated, _ := m.Update(StatusUpdatedMsg{
		SessionID:  "sess-2",
		ProjectDir: "/a/myproject",
		Status:     model.StatusNeedsInput,
		Attention:  "Allow?",
	})
	um := modelFrom(updated)
	s1 := um.store.SessionByID("sess-1")
	s2 := um.store.SessionByID("sess-2")
	if s1.Status != model.StatusWorking {
		t.Errorf("sess-1 should stay working, got %q", s1.Status)
	}
	if s2.Status != model.StatusNeedsInput {
		t.Errorf("sess-2 should be needs_input, got %q", s2.Status)
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
		SessionID:  "sess-1",
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
		SessionID:  "sess-1",
		ProjectDir: "/a/myproject",
		PID:        1234,
		LogPath:    "/tmp/monitors/myproject.log",
	})
	um := modelFrom(updated)
	session := um.store.SessionByID("sess-1")
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

func TestMonitorStartedFallsBackAfterRemap(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	// Session was remapped from "pty-0" to "pty:pty-0" by integration toggle.
	store.SetSession(&model.AgentSession{
		ProjectDir: "/a/myproject",
		SessionID:  "pty:pty-0",
		Type:       model.AgentClaude,
	})
	m := newTestModelWithStore(&mockBackend{}, store)

	// MonitorStartedMsg arrives with the OLD session ID (in-flight before remap).
	updated, _ := m.Update(MonitorStartedMsg{
		SessionID:  "pty-0",
		ProjectDir: "/a/myproject",
		PID:        9999,
		LogPath:    "/tmp/monitors/pty-0.log",
	})
	um := modelFrom(updated)

	// Should fall back to FirstSession and still record the monitor.
	session := um.store.SessionByID("pty:pty-0")
	if session.MonitorPID != 9999 {
		t.Errorf("expected PID 9999 after remap fallback, got %d", session.MonitorPID)
	}
	if session.MonitorLog != "/tmp/monitors/pty-0.log" {
		t.Errorf("expected log path after remap fallback, got %q", session.MonitorLog)
	}
}

func TestMonitorStartedMultipleAgentsSameDir(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/a/myproject",
		SessionID:  "sess-1",
		Type:       model.AgentClaude,
	})
	store.SetSession(&model.AgentSession{
		ProjectDir: "/a/myproject",
		SessionID:  "sess-2",
		Type:       model.AgentClaude,
	})
	m := newTestModelWithStore(&mockBackend{}, store)

	// Send monitor started for sess-2 specifically.
	updated, _ := m.Update(MonitorStartedMsg{
		SessionID:  "sess-2",
		ProjectDir: "/a/myproject",
		PID:        5678,
		LogPath:    "/tmp/monitors/sess-2.log",
	})
	um := modelFrom(updated)

	// sess-2 should get the PID, not sess-1.
	s1 := um.store.SessionByID("sess-1")
	s2 := um.store.SessionByID("sess-2")
	if s1.MonitorPID != 0 {
		t.Errorf("sess-1 should not have monitor PID, got %d", s1.MonitorPID)
	}
	if s2.MonitorPID != 5678 {
		t.Errorf("sess-2 expected PID 5678, got %d", s2.MonitorPID)
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
	session := um.store.FirstSession("/a/myproject")
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
	session := um.store.FirstSession("/a/myproject")
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
	session := um.store.FirstSession("/a/myproject")
	if session.Status != model.StatusIdle {
		t.Errorf("expected idle when screen shows prompt, got %q", session.Status)
	}
}

func TestScreenReadBlankTransitionsToIdle(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/myproject")
	store.SetSession(&model.AgentSession{
		ProjectDir:     "/a/myproject",
		SessionID:      "sess-1",
		Type:           model.AgentClaude,
		Status:         model.StatusWorking,
		LastScreen:     "\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n", // previous blank
		UnmatchedReads: 1,                                                    // one prior blank read
	})
	m := newTestModelWithStore(&mockBackend{}, store)

	// Third consecutive blank read — should transition to idle
	updated, _ := m.Update(ScreenReadMsg{
		SessionID:  "sess-1",
		ProjectDir: "/a/myproject",
		Content:    "\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n",
	})
	um := modelFrom(updated)
	session := um.store.FirstSession("/a/myproject")
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
	session := um.store.FirstSession("/a/myproject")
	if session.Status != model.StatusWorking {
		t.Errorf("expected status unchanged, got %q", session.Status)
	}
}

func TestDiscoveryTickProducesCommands(t *testing.T) {
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

	_, cmd := m.Update(DiscoveryTickMsg{})
	if cmd == nil {
		t.Fatal("expected commands from tick")
	}
}

func TestStatusTickNoCommandsWithoutBackend(t *testing.T) {
	m := newTestModelWithStore(&mockBackend{}, makeStore(t))
	// backendOK = false

	_, cmd := m.Update(StatusTickMsg{})
	// Should still have tick rescheduled but no backend commands
	if cmd == nil {
		t.Fatal("expected at least tick reschedule")
	}
}

func TestStatusTickSkipsVisibleSession(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/proj/alpha", "/proj/beta")
	store.SetSession(&model.AgentSession{
		ProjectDir:     "/proj/alpha",
		SessionID:      "sid-1",
		Type:           model.AgentClaude,
		Status:         model.StatusWorking,
		LastScreenRead: time.Now().Add(-2 * time.Second),
	})
	store.SetSession(&model.AgentSession{
		ProjectDir:     "/proj/beta",
		SessionID:      "sid-2",
		Type:           model.AgentClaude,
		Status:         model.StatusWorking,
		LastScreenRead: time.Now().Add(-2 * time.Second),
	})
	backend := &mockBackend{}
	m := newTestModelWithStore(backend, store)
	m.backendOK = true
	m.streamOpen = true
	updated, cmd := m.Update(StatusTickMsg{})
	if cmd == nil {
		t.Fatal("expected status tick command")
	}
	_ = drainCmd(t, modelFrom(updated), cmd, 2)
	if len(backend.readScreenLog) == 0 {
		t.Fatalf("expected at least 1 background read, got %v", backend.readScreenLog)
	}
	for _, got := range backend.readScreenLog {
		if got != "sid-2:40" {
			t.Fatalf("expected visible session sid-1 to be skipped, got %v", backend.readScreenLog)
		}
	}
}

func TestStreamOpenStartsVisibleRefresh(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/proj/alpha")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/proj/alpha",
		SessionID:  "sid-1",
		Type:       model.AgentClaude,
		Status:     model.StatusWorking,
	})
	backend := &mockBackend{screenByID: map[string]string{"sid-1": "live output"}}
	m := newTestModelWithStore(backend, store)
	m.backendOK = true

	updated, cmd := m.Update(keyMsg("v"))
	if cmd == nil {
		t.Fatal("expected visible refresh to start")
	}
	_ = drainCmd(t, modelFrom(updated), cmd, 2)

	if len(backend.readScreenLog) == 0 || backend.readScreenLog[0] != "sid-1:40" {
		t.Fatalf("expected visible stream read for sid-1, got %v", backend.readScreenLog)
	}
}

func TestChatOpenStartsVisibleRefresh(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/proj/alpha")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/proj/alpha",
		SessionID:  "sid-1",
		Type:       model.AgentClaude,
		Status:     model.StatusWorking,
	})
	backend := &mockBackend{screenByID: map[string]string{"sid-1": "chat output"}}
	m := newTestModelWithStore(backend, store)
	m.backendOK = true

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected visible refresh to start")
	}
	_ = drainCmd(t, modelFrom(updated), cmd, 2)

	if len(backend.readScreenLog) == 0 || backend.readScreenLog[0] != "sid-1:40" {
		t.Fatalf("expected visible chat read for sid-1, got %v", backend.readScreenLog)
	}
}

func TestStreamCursorMoveRetargetsVisibleRefresh(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/proj/alpha", "/proj/beta")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/proj/alpha",
		SessionID:  "sid-1",
		Type:       model.AgentClaude,
		Status:     model.StatusWorking,
	})
	store.SetSession(&model.AgentSession{
		ProjectDir: "/proj/beta",
		SessionID:  "sid-2",
		Type:       model.AgentClaude,
		Status:     model.StatusWorking,
	})
	m := newTestModelWithStore(&mockBackend{}, store)
	m.backendOK = true
	m.streamOpen = true

	updated, cmd := m.Update(keyMsg("j"))
	um := modelFrom(updated)
	if um.cursor != 1 {
		t.Fatalf("expected cursor 1, got %d", um.cursor)
	}
	if cmd == nil {
		t.Fatal("expected visible refresh to restart on cursor move")
	}
	um = drainCmd(t, um, cmd, 1)
	if um.visibleRefreshID != "sid-2" {
		t.Fatalf("expected visible refresh to retarget sid-2, got %q", um.visibleRefreshID)
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
	if !strings.Contains(v, "Agent multiplexer") {
		t.Error("expected tagline in empty state")
	}
	if !strings.Contains(v, "new agent in a directory") {
		t.Error("expected new agent hint in empty state")
	}
	if !strings.Contains(v, "cycle agent") {
		t.Error("expected cycle agent hint in empty state")
	}
}

func TestViewProjectListEmptySingleAgent(t *testing.T) {
	m := newTestModelWithStore(&mockBackend{}, makeStore(t))
	m.width = 80
	m.height = 40
	m.availableAgents = []model.AgentType{model.AgentClaude}
	m.defaultAgent = model.AgentClaude
	v := m.View()
	if !strings.Contains(v, "new agent in a directory") {
		t.Error("expected new agent hint in empty state")
	}
	if strings.Contains(v, "cycle agent") {
		t.Error("should not show cycle agent hint with single agent")
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
	if !strings.Contains(v, "n:new") {
		t.Error("expected new agent hint")
	}
	// Directory should be inline in the row
	if !strings.Contains(v, "/a/alpha") {
		t.Error("expected path for alpha")
	}
	// Column headers
	if !strings.Contains(v, "agent") {
		t.Error("expected column header 'agent'")
	}
	if !strings.Contains(v, "type") {
		t.Error("expected column header 'type'")
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
	t.Run("by status ascending", func(t *testing.T) {
		rows := []projectRow{
			{project: &model.Project{Name: "delta"}, session: &model.AgentSession{Status: model.StatusIdle}, displayName: "delta"},
			{project: &model.Project{Name: "alpha"}, session: &model.AgentSession{Status: model.StatusIdle}, displayName: "alpha"},
			{project: &model.Project{Name: "beta"}, session: &model.AgentSession{Status: model.StatusNeedsInput}, displayName: "beta"},
			{project: &model.Project{Name: "gamma"}, session: &model.AgentSession{Status: model.StatusWorking}, displayName: "gamma"},
		}
		sortRows(rows, sortByStatus, false)

		expected := []string{"beta", "gamma", "alpha", "delta"}
		for i, name := range expected {
			if rows[i].project.Name != name {
				t.Errorf("position %d: expected %q, got %q", i, name, rows[i].project.Name)
			}
		}
	})

	t.Run("by agent ascending", func(t *testing.T) {
		rows := []projectRow{
			{project: &model.Project{Name: "delta"}, session: &model.AgentSession{}, displayName: "delta"},
			{project: &model.Project{Name: "alpha"}, session: &model.AgentSession{}, displayName: "alpha"},
			{project: &model.Project{Name: "gamma"}, session: &model.AgentSession{}, displayName: "gamma"},
		}
		sortRows(rows, sortByAgent, false)

		expected := []string{"alpha", "delta", "gamma"}
		for i, name := range expected {
			if rows[i].displayName != name {
				t.Errorf("position %d: expected %q, got %q", i, name, rows[i].displayName)
			}
		}
	})

	t.Run("by agent descending", func(t *testing.T) {
		rows := []projectRow{
			{project: &model.Project{Name: "delta"}, session: &model.AgentSession{}, displayName: "delta"},
			{project: &model.Project{Name: "alpha"}, session: &model.AgentSession{}, displayName: "alpha"},
			{project: &model.Project{Name: "gamma"}, session: &model.AgentSession{}, displayName: "gamma"},
		}
		sortRows(rows, sortByAgent, true)

		expected := []string{"gamma", "delta", "alpha"}
		for i, name := range expected {
			if rows[i].displayName != name {
				t.Errorf("position %d: expected %q, got %q", i, name, rows[i].displayName)
			}
		}
	})

	t.Run("by type", func(t *testing.T) {
		rows := []projectRow{
			{project: &model.Project{Name: "a"}, session: &model.AgentSession{Type: model.AgentCodex}, displayName: "a"},
			{project: &model.Project{Name: "b"}, session: &model.AgentSession{Type: model.AgentClaude}, displayName: "b"},
			{project: &model.Project{Name: "c"}, session: &model.AgentSession{Type: model.AgentClaude}, displayName: "c"},
		}
		sortRows(rows, sortByHarness, false)

		expected := []string{"b", "c", "a"} // claude < codex, then by name
		for i, name := range expected {
			if rows[i].displayName != name {
				t.Errorf("position %d: expected %q, got %q", i, name, rows[i].displayName)
			}
		}
	})

	t.Run("by updated", func(t *testing.T) {
		now := time.Now()
		rows := []projectRow{
			{project: &model.Project{Name: "old"}, session: &model.AgentSession{LastActivity: now.Add(-5 * time.Minute)}, displayName: "old"},
			{project: &model.Project{Name: "new"}, session: &model.AgentSession{LastActivity: now}, displayName: "new"},
		}
		sortRows(rows, sortByUpdated, false)

		if rows[0].displayName != "new" {
			t.Errorf("expected 'new' first (most recent), got %q", rows[0].displayName)
		}
	})
}

func TestDuplicateDisplayNames(t *testing.T) {
	// Different parent dirs produce unique 2-segment names — no disambiguation
	store := &model.Store{
		Projects: []*model.Project{
			{Name: "myapp", Dir: "/go/myapp"},
			{Name: "myapp", Dir: "/rb/myapp"},
			{Name: "other", Dir: "/home/other"},
		},
		Sessions: []*model.AgentSession{
			{ProjectDir: "/go/myapp", SessionID: "s1", Type: model.AgentClaude, Status: model.StatusIdle},
			{ProjectDir: "/rb/myapp", SessionID: "s2", Type: model.AgentClaude, Status: model.StatusIdle},
			{ProjectDir: "/home/other", SessionID: "s3", Type: model.AgentClaude, Status: model.StatusIdle},
		},
	}
	rows := buildRows(storeAdapter{store})
	sortRows(rows, sortByAgent, false)

	var myappNames []string
	for _, r := range rows {
		if r.project.Name == "myapp" {
			myappNames = append(myappNames, r.displayName)
		}
	}
	if len(myappNames) != 2 {
		t.Fatalf("expected 2 myapp rows, got %d", len(myappNames))
	}
	if myappNames[0] != "go/myapp" {
		t.Errorf("first should be 'go/myapp', got %q", myappNames[0])
	}
	if myappNames[1] != "rb/myapp" {
		t.Errorf("second should be 'rb/myapp', got %q", myappNames[1])
	}

	// Same parent dir produces duplicate 2-segment names — disambiguated with #2
	store2 := &model.Store{
		Projects: []*model.Project{
			{Name: "myapp", Dir: "/work/myapp"},
		},
		Sessions: []*model.AgentSession{
			{ProjectDir: "/work/myapp", SessionID: "s1", Type: model.AgentClaude, Status: model.StatusIdle},
			{ProjectDir: "/work/myapp", SessionID: "s2", Type: model.AgentCodex, Status: model.StatusIdle},
		},
	}
	rows2 := buildRows(storeAdapter{store2})

	if len(rows2) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows2))
	}
	if rows2[0].displayName != "work/myapp" {
		t.Errorf("first should be 'work/myapp', got %q", rows2[0].displayName)
	}
	if rows2[1].displayName != "work/myapp #2" {
		t.Errorf("second should be 'work/myapp #2', got %q", rows2[1].displayName)
	}
}

func TestSortKeyCyclesColumn(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/proj")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/a/proj",
		SessionID:  "s1",
		Type:       model.AgentClaude,
		Status:     model.StatusIdle,
	})
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 120
	m.height = 40

	if m.sortCol != sortByAgent {
		t.Fatalf("expected default sort column agent, got %d", m.sortCol)
	}

	// Press s to cycle to type
	updated, _ := m.Update(keyMsg("s"))
	um := modelFrom(updated)
	if um.sortCol != sortByHarness {
		t.Errorf("expected sortByHarness after one s press, got %d", um.sortCol)
	}

	// Press S to reverse direction
	updated, _ = um.Update(keyMsg("S"))
	um = modelFrom(updated)
	if !um.sortDesc {
		t.Error("expected sortDesc=true after S press")
	}
	if um.sortCol != sortByHarness {
		t.Errorf("expected sortByHarness unchanged, got %d", um.sortCol)
	}
}

func TestColumnHeaderSortIndicator(t *testing.T) {
	header := renderColumnHeaders(20, 10, 100, sortByAgent, false, false, 0)
	if !strings.Contains(header, "agent▲") {
		t.Errorf("expected agent▲ in header, got %q", header)
	}
	if strings.Contains(header, "type▲") || strings.Contains(header, "type▼") {
		t.Error("non-active column should not have sort indicator")
	}

	header = renderColumnHeaders(20, 10, 100, sortByAgent, true, false, 0)
	if !strings.Contains(header, "agent▼") {
		t.Errorf("expected agent▼ in header for descending, got %q", header)
	}

	header = renderColumnHeaders(20, 10, 100, sortByStatus, false, false, 0)
	if !strings.Contains(header, "status▲") {
		t.Errorf("expected status▲ in header, got %q", header)
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
		{model.StatusWorking, "", "", "working..."},
		{model.StatusWorking, "Running tests", "", "Running tests"},
		{model.StatusIdle, "", "", "idle"},
		{model.StatusIdle, "Atria Agent Orchestration", "", "Atria Agent Orchestration"},
		{model.StatusNeedsInput, "", "Allow edit?", "Allow edit?"},
		{model.StatusNeedsInput, "", "", "needs input"},
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
		{10 * time.Second, "now"},
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
		{"", "''"},
		{"a'b'c", "'a'\"'\"'b'\"'\"'c'"},
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
	for i := 0; i < 50; i++ {
		dir := fmt.Sprintf("/proj/agent-%02d", i)
		store.AddProject(dir)
		store.SetSession(&model.AgentSession{
			ProjectDir: dir,
			SessionID:  fmt.Sprintf("s%d", i),
			Type:       model.AgentClaude,
			Status:     model.StatusIdle,
		})
	}
	m := newTestModelWithStore(&mockBackend{}, store)
	m.backendOK = true

	without := m.maxVisibleRows()

	m.streamOpen = true
	layout := m.projectListLayout()
	with := m.maxVisibleRows()

	if with >= without {
		t.Errorf("maxVisibleRows with stream (%d) should be less than without (%d)", with, without)
	}
	if with != layout.maxRows {
		t.Errorf("maxVisibleRows() = %d, want layout maxRows %d", with, layout.maxRows)
	}
	if without-with != layout.panelHeight {
		t.Errorf("difference should be panelHeight (%d), got %d", layout.panelHeight, without-with)
	}
}

func TestProjectListLayoutUsesMinimumPanelForLongLists(t *testing.T) {
	store := makeStore(t)
	for i := 0; i < 50; i++ {
		dir := fmt.Sprintf("/proj/agent-%02d", i)
		store.AddProject(dir)
		store.SetSession(&model.AgentSession{
			ProjectDir: dir,
			SessionID:  fmt.Sprintf("s%d", i),
			Type:       model.AgentClaude,
			Status:     model.StatusIdle,
		})
	}

	m := newTestModelWithStore(&mockBackend{}, store)
	m.streamOpen = true

	layout := m.projectListLayout()
	usable := m.height - headerLineCount - footerLineCount - 1
	wantPanel := (usable + 1) / 2

	if layout.panelHeight != wantPanel {
		t.Fatalf("panelHeight = %d, want %d", layout.panelHeight, wantPanel)
	}
	if layout.maxRows != usable-wantPanel {
		t.Fatalf("maxRows = %d, want %d", layout.maxRows, usable-wantPanel)
	}
}

func TestProjectListLayoutExpandsPanelForShortLists(t *testing.T) {
	store := makeStore(t)
	for i := 0; i < 3; i++ {
		dir := fmt.Sprintf("/proj/agent-%02d", i)
		store.AddProject(dir)
		store.SetSession(&model.AgentSession{
			ProjectDir: dir,
			SessionID:  fmt.Sprintf("s%d", i),
			Type:       model.AgentClaude,
			Status:     model.StatusIdle,
		})
	}

	m := newTestModelWithStore(&mockBackend{}, store)
	m.streamOpen = true

	layout := m.projectListLayout()
	usable := m.height - headerLineCount - footerLineCount - 1

	if layout.maxRows != len(m.rows) {
		t.Fatalf("maxRows = %d, want %d", layout.maxRows, len(m.rows))
	}
	if layout.panelHeight != usable-len(m.rows) {
		t.Fatalf("panelHeight = %d, want %d", layout.panelHeight, usable-len(m.rows))
	}
	if layout.panelHeight < (usable+1)/2 {
		t.Fatalf("panelHeight = %d, want >= %d", layout.panelHeight, (usable+1)/2)
	}
}

func TestProjectListLayoutHandlesShortTerminal(t *testing.T) {
	store := makeStore(t)
	store.AddProject("/proj/alpha")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/proj/alpha",
		SessionID:  "s1",
		Type:       model.AgentClaude,
		Status:     model.StatusIdle,
		LastScreen: "hello",
	})

	m := newTestModelWithStore(&mockBackend{}, store)
	m.height = 12
	m.streamOpen = true

	layout := m.projectListLayout()
	if layout.maxRows < 1 {
		t.Fatalf("maxRows = %d, want >= 1", layout.maxRows)
	}
	if layout.panelHeight < 3 {
		t.Fatalf("panelHeight = %d, want >= 3", layout.panelHeight)
	}

	view := m.View()
	assertLinesWithinWidth(t, view, m.width)
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
	if !strings.Contains(v, "alpha") {
		t.Error("expected stream panel header to contain project name")
	}
	if !strings.Contains(v, "Claude") {
		t.Error("expected stream panel header to contain agent type")
	}
	if !strings.Contains(v, "v:close") {
		t.Error("expected stream panel header to contain v:close hint")
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
	_ = m.View()
	// Move cursor down
	updated, _ := m.Update(keyMsg("j"))
	m = modelFrom(updated)
	v := m.View()

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

func TestStreamPanelHeaderTruncatesLongName(t *testing.T) {
	longName := strings.Repeat("a", 200)
	session := &model.AgentSession{
		ProjectDir: "/proj/" + longName,
		SessionID:  "s1",
		Type:       model.AgentClaude,
		Status:     model.StatusWorking,
		LastScreen: "some content",
	}
	width := 40
	panel := renderStreamPanel(session, longName, "/proj/"+longName, width, 8, false)
	if !strings.Contains(panel, "\u2026") {
		t.Error("expected truncated project name with ellipsis")
	}
	// The full long name should NOT appear — it must be truncated
	if strings.Contains(panel, longName) {
		t.Error("expected long project name to be truncated in header")
	}
}

func TestStreamPanelKeepsSafetyMarginWithWideGlyphs(t *testing.T) {
	session := &model.AgentSession{
		ProjectDir: "/proj/alpha",
		SessionID:  "s1",
		Type:       model.AgentCopilot,
		Status:     model.StatusIdle,
		LastScreen: strings.Repeat("💡", 20) + " status line",
	}

	panel := renderStreamPanel(session, "alpha", "/proj/alpha", 40, 8, false)
	assertLinesWithinWidth(t, panel, 39)
}

func TestStreamPanelSanitizesLayoutHostileContent(t *testing.T) {
	session := &model.AgentSession{
		ProjectDir: "/proj/alpha",
		SessionID:  "s1",
		Type:       model.AgentClaude,
		Status:     model.StatusNeedsInput,
		LastScreen: "\x1b[33mEnter\tto\tselect\x1b[0m\r" +
			"↑/↓ to navigate\rEsc to cancel\x00",
	}

	panel := renderStreamPanel(session, "alpha", "/proj/alpha", 70, 8, false)
	if !strings.Contains(panel, "v:close") {
		t.Fatal("expected stream panel header to contain v:close")
	}
	assertLinesWithinWidth(t, panel, 70)
}

func TestProjectListSelectedRowKeepsSafetyMarginWhenStreamOpen(t *testing.T) {
	rows := []projectRow{
		{
			project: &model.Project{Dir: "/proj/alpha", Name: "alpha"},
			session: &model.AgentSession{
				ProjectDir:   "/proj/alpha",
				SessionID:    "s1",
				Type:         model.AgentCopilot,
				Status:       model.StatusWorking,
				Activity:     strings.Repeat("wide 💡 activity ", 6),
				LastActivity: time.Now(),
			},
			displayName: "alpha-agent-with-long-name",
		},
	}

	view := renderProjectList(rows, 0, 80, 0, map[string]time.Time{}, model.AgentClaude, nil, 5, 0, sortByAgent, false, false, true)
	var selectedLine string
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "alpha-agent-with-long-name") {
			selectedLine = line
			break
		}
	}
	if selectedLine == "" {
		t.Fatal("expected selected row in rendered project list")
	}
	if got := lipgloss.Width(selectedLine); got > 79 {
		t.Fatalf("selected row width = %d, want <= 79", got)
	}
}

func TestStreamOpenLastSelectionKeepsAgentsHeaderVisible(t *testing.T) {
	store := makeStore(t)
	for i := 0; i < 25; i++ {
		dir := fmt.Sprintf("/proj/agent-%02d", i)
		store.AddProject(dir)
		screen := "steady output"
		if i == 24 {
			screen = strings.Repeat("💡", 24) + " Copilot needs review"
		}
		store.SetSession(&model.AgentSession{
			ProjectDir: dir,
			SessionID:  fmt.Sprintf("s%d", i),
			Type:       model.AgentCopilot,
			Status:     model.StatusWorking,
			Activity:   "reviewing changes",
			LastScreen: screen,
		})
	}

	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 166
	m.height = 39
	m.backendOK = true
	m.streamOpen = true
	m.rebuildRows()

	first := m.View()
	if !strings.Contains(first, "agents") {
		t.Fatal("expected agents header with first row selected")
	}

	for i := 0; i < len(m.rows)-1; i++ {
		updated, _ := m.Update(keyMsg("j"))
		m = modelFrom(updated)
	}

	last := m.View()
	if !strings.Contains(last, "agents") {
		t.Fatal("expected agents header with last row selected")
	}
	assertLinesWithinWidth(t, last, 166)
}

func TestStreamOpenLayoutHostileScreenKeepsHeaderVisible(t *testing.T) {
	store := makeStore(t)
	store.AddProject("/proj/alpha")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/proj/alpha",
		SessionID:  "s1",
		Type:       model.AgentClaude,
		Status:     model.StatusNeedsInput,
		Attention:  "Enter to select",
		LastScreen: "299 +nds if strings.Contains(list, \"a n/a\") {\n" +
			"6.\tUpdate key handling\n" +
			"\x1b[33mEnter to select\x1b[0m\r" +
			"↑/↓ to navigate\rEsc to cancel",
	})

	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 120
	m.height = 24
	m.backendOK = true
	m.streamOpen = true
	m.rebuildRows()

	view := m.View()
	if !strings.Contains(view, "agents") {
		t.Fatal("expected agents header to remain visible")
	}
	if !strings.Contains(view, "v:close") {
		t.Fatal("expected stream panel header to remain visible")
	}
	assertLinesWithinWidth(t, view, 120)
}

func TestShortListKeepsSingleSpacerAbovePanel(t *testing.T) {
	store := makeStore(t)
	for i := 0; i < 2; i++ {
		dir := fmt.Sprintf("/proj/agent-%02d", i)
		store.AddProject(dir)
		store.SetSession(&model.AgentSession{
			ProjectDir: dir,
			SessionID:  fmt.Sprintf("s%d", i),
			Type:       model.AgentClaude,
			Status:     model.StatusIdle,
			LastScreen: "steady output",
		})
	}

	m := newTestModelWithStore(&mockBackend{}, store)
	m.streamOpen = true
	m.rebuildRows()

	lines := strings.Split(m.View(), "\n")
	panelTop := -1
	for i, line := range lines {
		if panelTop == -1 && strings.Contains(line, "┌") && strings.Contains(line, "v:close") {
			panelTop = i
		}
	}
	if panelTop < 2 {
		t.Fatalf("failed to locate panel top with preceding spacer and row")
	}
	if strings.TrimSpace(lines[panelTop-1]) != "" {
		t.Fatalf("expected spacer line before panel, got %q", lines[panelTop-1])
	}
	if !strings.Contains(lines[panelTop-2], "agent-") {
		t.Fatalf("expected last rendered row above spacer, got %q", lines[panelTop-2])
	}
}

// --- Settings screen tests ---

func settingsModel(t *testing.T) Model {
	t.Helper()
	store := makeStore(t)
	m := newTestModelWithStore(&mockBackend{}, store)
	m.cfg = &config.Config{
		WatchDirs: []string{"/a", "/b"},
		PtyCols:   120,
		PtyRows:   40,
	}
	m.availableAgents = []model.AgentType{model.AgentClaude, model.AgentCodex}
	m.defaultAgent = model.AgentClaude
	m.statusInfo = StatusInfo{
		Backends: []BackendStatus{
			{Name: "pty", Enabled: true, Active: true},
			{Name: "tmux", Enabled: false},
			{Name: "iterm2", Enabled: false},
		},
	}
	m.view = viewSettings
	m.settingsItems = buildSettingsItems(m.statusInfo, m.cfg, m.availableAgents)
	m.settingsCursor = m.firstEditableSettingsItem()
	return m
}

func TestSettingsNavigation(t *testing.T) {
	m := settingsModel(t)
	startCursor := m.settingsCursor

	// Move down
	updated, _ := m.Update(keyMsg("j"))
	um := modelFrom(updated)
	if um.settingsCursor <= startCursor {
		t.Errorf("expected cursor to advance past %d, got %d", startCursor, um.settingsCursor)
	}
	downCursor := um.settingsCursor

	// Move up — should return to start
	updated, _ = um.Update(keyMsg("k"))
	um = modelFrom(updated)
	if um.settingsCursor != startCursor {
		t.Errorf("expected cursor back at %d, got %d", startCursor, um.settingsCursor)
	}

	// Move down past last item — cursor stays
	um.settingsCursor = len(um.settingsItems) - 1
	// Make sure we're on a non-header
	for um.settingsItems[um.settingsCursor].itemType == "header" {
		um.settingsCursor--
	}
	lastCursor := um.settingsCursor
	updated, _ = um.Update(keyMsg("j"))
	um = modelFrom(updated)
	if um.settingsCursor != lastCursor {
		t.Errorf("expected cursor clamped at %d, got %d", lastCursor, um.settingsCursor)
	}
	_ = downCursor
}

func TestSettingsEscapeReturns(t *testing.T) {
	m := settingsModel(t)
	updated, _ := m.Update(ctrlKeyMsg(tea.KeyEscape))
	um := modelFrom(updated)
	if um.view != viewProjectList {
		t.Errorf("expected viewProjectList after Esc, got %d", um.view)
	}
}

func TestSettingsQuitReturns(t *testing.T) {
	m := settingsModel(t)
	updated, _ := m.Update(keyMsg("q"))
	um := modelFrom(updated)
	if um.view != viewProjectList {
		t.Errorf("expected viewProjectList after q, got %d", um.view)
	}
}

func TestSettingsEnterOnToggle(t *testing.T) {
	m := settingsModel(t)
	// Find the tmux toggle item
	for i, item := range m.settingsItems {
		if item.itemType == "toggle" && item.key == "tmux" {
			m.settingsCursor = i
			break
		}
	}
	updated, _ := m.Update(ctrlKeyMsg(tea.KeyEnter))
	um := modelFrom(updated)
	if um.statusText != "Cannot modify backend" {
		t.Errorf("expected 'Cannot modify backend', got %q", um.statusText)
	}
}

func TestSettingsEnterOnChoice(t *testing.T) {
	m := settingsModel(t)
	// Find the default_agent choice item
	for i, item := range m.settingsItems {
		if item.itemType == "choice" && item.key == "default_agent" {
			m.settingsCursor = i
			break
		}
	}
	if m.defaultAgent != model.AgentClaude {
		t.Fatalf("expected initial agent Claude, got %s", m.defaultAgent)
	}
	updated, _ := m.Update(ctrlKeyMsg(tea.KeyEnter))
	um := modelFrom(updated)
	if um.defaultAgent != model.AgentCodex {
		t.Errorf("expected agent cycled to Codex, got %s", um.defaultAgent)
	}
	if um.cfg.DefaultAgent != string(model.AgentCodex) {
		t.Errorf("expected cfg.DefaultAgent 'codex', got %q", um.cfg.DefaultAgent)
	}
}

func TestSettingsEnterOnStringOpensEdit(t *testing.T) {
	m := settingsModel(t)
	// Find the tmux_session string item
	for i, item := range m.settingsItems {
		if item.itemType == "string" && item.key == "tmux_session" {
			m.settingsCursor = i
			break
		}
	}
	updated, _ := m.Update(ctrlKeyMsg(tea.KeyEnter))
	um := modelFrom(updated)
	if !um.settingsEditing {
		t.Error("expected settingsEditing to be true")
	}
	if um.settingsEditBuf != "" {
		t.Errorf("expected empty edit buf for auto-detect, got %q", um.settingsEditBuf)
	}
}

func TestSettingsEditTypeAndSave(t *testing.T) {
	m := settingsModel(t)
	// Position cursor on pty_cols item
	for i, item := range m.settingsItems {
		if item.key == "pty_cols" {
			m.settingsCursor = i
			break
		}
	}
	m.settingsEditing = true
	m.settingsEditBuf = "12"

	// Type '0'
	updated, _ := m.Update(keyMsg("0"))
	um := modelFrom(updated)
	if um.settingsEditBuf != "120" {
		t.Errorf("expected edit buf '120', got %q", um.settingsEditBuf)
	}

	// Press Enter to commit
	updated, cmd := um.Update(ctrlKeyMsg(tea.KeyEnter))
	um = modelFrom(updated)
	if um.settingsEditing {
		t.Error("expected settingsEditing to be false after Enter")
	}
	if um.cfg.PtyCols != 120 {
		t.Errorf("expected PtyCols=120, got %d", um.cfg.PtyCols)
	}
	if cmd == nil {
		t.Error("expected saveConfig command")
	}
}

func TestSettingsCanClearTmuxLaunchSession(t *testing.T) {
	m := settingsModel(t)
	m.cfg.TmuxSession = "override"
	m.settingsItems = buildSettingsItems(m.statusInfo, m.cfg, m.availableAgents)
	for i, item := range m.settingsItems {
		if item.key == "tmux_session" {
			m.settingsCursor = i
			break
		}
	}
	m.settingsEditing = true
	m.settingsEditBuf = ""

	updated, cmd := m.Update(ctrlKeyMsg(tea.KeyEnter))
	um := modelFrom(updated)
	if um.cfg.TmuxSession != "" {
		t.Fatalf("expected tmux session override to clear, got %q", um.cfg.TmuxSession)
	}
	if cmd == nil {
		t.Fatal("expected saveConfig command")
	}
}

func TestSettingsEditBackspace(t *testing.T) {
	m := settingsModel(t)
	m.settingsEditing = true
	m.settingsEditBuf = "abc"
	// Need cursor on any editable item so handleSettingsEditKey runs
	for i, item := range m.settingsItems {
		if item.itemType == "string" || item.itemType == "number" {
			m.settingsCursor = i
			break
		}
	}

	updated, _ := m.Update(ctrlKeyMsg(tea.KeyBackspace))
	um := modelFrom(updated)
	if um.settingsEditBuf != "ab" {
		t.Errorf("expected 'ab' after backspace, got %q", um.settingsEditBuf)
	}
}

func TestSettingsEditEscapeCancels(t *testing.T) {
	m := settingsModel(t)
	m.settingsEditing = true
	m.settingsEditBuf = "changed"
	for i, item := range m.settingsItems {
		if item.itemType == "string" || item.itemType == "number" {
			m.settingsCursor = i
			break
		}
	}

	updated, _ := m.Update(ctrlKeyMsg(tea.KeyEscape))
	um := modelFrom(updated)
	if um.settingsEditing {
		t.Error("expected settingsEditing to be false after Esc")
	}
	if um.settingsEditBuf != "" {
		t.Errorf("expected empty edit buf, got %q", um.settingsEditBuf)
	}
}

func TestSettingsDeleteWatchDir(t *testing.T) {
	m := settingsModel(t)
	// Find the list-entry for "/a"
	found := false
	for i, item := range m.settingsItems {
		if item.itemType == "list-entry" && item.key == "watch_dirs" && item.value == "/a" {
			m.settingsCursor = i
			found = true
			break
		}
	}
	if !found {
		t.Fatal("could not find watch_dirs list-entry for /a")
	}

	updated, cmd := m.Update(keyMsg("d"))
	um := modelFrom(updated)
	if len(um.cfg.WatchDirs) != 1 || um.cfg.WatchDirs[0] != "/b" {
		t.Errorf("expected WatchDirs=[/b], got %v", um.cfg.WatchDirs)
	}
	if len(um.watchDirs) != 1 || um.watchDirs[0] != "/b" {
		t.Errorf("expected watchDirs=[/b], got %v", um.watchDirs)
	}
	if cmd == nil {
		t.Error("expected saveConfig command")
	}
}

func TestIntegrationToggledMsgRemapsIDs(t *testing.T) {
	store := makeStore(t)
	store.SetSession(&model.AgentSession{
		SessionID:  "old-1",
		ProjectDir: "/proj",
		Type:       model.AgentClaude,
		Status:     model.StatusIdle,
	})
	m := newTestModelWithStore(&mockBackend{}, store)
	m.cfg = &config.Config{}
	m.availableAgents = []model.AgentType{model.AgentClaude}
	m.statusInfo = StatusInfo{
		Backends: []BackendStatus{
			{Name: "tmux", Enabled: true, Active: true},
		},
	}
	m.attentionSessions = map[string]time.Time{"old-1": time.Now()}
	m.chatSessionID = "old-1"
	m.termSessionID = "old-1"

	msg := IntegrationToggledMsg{
		Name:        "tmux",
		Status:      BackendStatus{Name: "tmux", Enabled: true, Active: true},
		RemappedIDs: map[string]string{"old-1": "pty:old-1"},
	}
	updated, _ := m.Update(msg)
	um := modelFrom(updated)

	// Session ID should be remapped
	sess := um.store.Sessions[0]
	if sess.SessionID != "pty:old-1" {
		t.Errorf("expected session ID 'pty:old-1', got %q", sess.SessionID)
	}
	// Attention map should be remapped
	if _, ok := um.attentionSessions["old-1"]; ok {
		t.Error("old attention key should be removed")
	}
	if _, ok := um.attentionSessions["pty:old-1"]; !ok {
		t.Error("new attention key should be set")
	}
	if um.chatSessionID != "pty:old-1" {
		t.Errorf("expected chatSessionID 'pty:old-1', got %q", um.chatSessionID)
	}
	if um.termSessionID != "pty:old-1" {
		t.Errorf("expected termSessionID 'pty:old-1', got %q", um.termSessionID)
	}
	// Backend status should be updated
	for _, bs := range um.statusInfo.Backends {
		if bs.Name == "tmux" && !bs.Active {
			t.Error("expected tmux backend to be active after toggle msg")
		}
	}
}

func integrationToggleModel(t *testing.T, backends []BackendStatus) Model {
	t.Helper()
	store := makeStore(t)
	m := newTestModelWithStore(&mockBackend{}, store)
	m.cfg = &config.Config{}
	m.availableAgents = []model.AgentType{model.AgentClaude}
	m.statusInfo = StatusInfo{Backends: backends}
	return m
}

func TestIntegrationToggledClearsOldLaunchFlag(t *testing.T) {
	m := integrationToggleModel(t, []BackendStatus{
		{Name: "pty", Enabled: true, Active: true, Launch: true},
		{Name: "tmux", Enabled: false},
	})

	// Toggle tmux on — it becomes the new launch target.
	msg := IntegrationToggledMsg{
		Name:       "tmux",
		Status:     BackendStatus{Name: "tmux", Enabled: true, Active: true, Launch: true},
		NewPrimary: "tmux",
	}
	updated, _ := m.Update(msg)
	um := modelFrom(updated)

	for _, bs := range um.statusInfo.Backends {
		if bs.Name == "pty" && bs.Launch {
			t.Error("expected PTY Launch to be cleared after tmux became launch target")
		}
		if bs.Name == "tmux" && !bs.Launch {
			t.Error("expected tmux Launch to be true")
		}
	}
}

func TestIntegrationToggledRestoresLaunchOnDisable(t *testing.T) {
	m := integrationToggleModel(t, []BackendStatus{
		{Name: "pty", Enabled: true, Active: true, Launch: false},
		{Name: "tmux", Enabled: true, Active: true, Launch: true},
	})

	// Disable tmux — PTY should become the launch target.
	msg := IntegrationToggledMsg{
		Name:       "tmux",
		Status:     BackendStatus{Name: "tmux", Enabled: false},
		NewPrimary: "pty",
	}
	updated, _ := m.Update(msg)
	um := modelFrom(updated)

	for _, bs := range um.statusInfo.Backends {
		if bs.Name == "pty" && !bs.Launch {
			t.Error("expected PTY Launch to be restored after tmux disabled")
		}
		if bs.Name == "tmux" && bs.Launch {
			t.Error("expected tmux Launch to be cleared after disable")
		}
	}
}

func TestIntegrationToggledPreservesLaunchOnProbeFail(t *testing.T) {
	m := integrationToggleModel(t, []BackendStatus{
		{Name: "pty", Enabled: true, Active: true, Launch: true},
		{Name: "tmux", Enabled: false},
	})

	// Toggle tmux on but probe fails — NewPrimary is empty.
	msg := IntegrationToggledMsg{
		Name:   "tmux",
		Status: BackendStatus{Name: "tmux", Enabled: true, Reason: "tmux not found"},
	}
	updated, _ := m.Update(msg)
	um := modelFrom(updated)

	for _, bs := range um.statusInfo.Backends {
		if bs.Name == "pty" && !bs.Launch {
			t.Error("expected PTY Launch to be preserved after failed tmux probe")
		}
		if bs.Name == "tmux" && bs.Launch {
			t.Error("expected tmux Launch to remain false after failed probe")
		}
	}
}

func TestConfigSavedMsgRollback(t *testing.T) {
	m := settingsModel(t)
	m.cfg.PtyCols = 200

	msg := ConfigSavedMsg{
		Err: errors.New("disk full"),
		Rollback: func(rm *Model) {
			rm.cfg.PtyCols = 120
		},
	}
	updated, _ := m.Update(msg)
	um := modelFrom(updated)
	if !strings.Contains(um.statusText, "Config save failed") {
		t.Errorf("expected status to contain 'Config save failed', got %q", um.statusText)
	}
	if um.cfg.PtyCols != 120 {
		t.Errorf("expected PtyCols rolled back to 120, got %d", um.cfg.PtyCols)
	}
}

func TestEnvColumnShownWithIntegration(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/alpha", "/a/beta")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/a/alpha",
		SessionID:  "tmux:sess-alpha",
		Type:       model.AgentClaude,
		Status:     model.StatusIdle,
		Source:     "tmux",
	})
	store.SetSession(&model.AgentSession{
		ProjectDir: "/a/beta",
		SessionID:  "pty-1",
		Type:       model.AgentClaude,
		Status:     model.StatusIdle,
		Source:     "pty",
	})
	// Add a session with empty Source to verify it shows blank (not "embedded")
	store.Projects = append(store.Projects, &model.Project{Dir: "/a/gamma", Name: "gamma"})
	store.SetSession(&model.AgentSession{
		ProjectDir: "/a/gamma",
		SessionID:  "unknown-1",
		Type:       model.AgentClaude,
		Status:     model.StatusIdle,
		Source:     "",
	})
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 120
	m.height = 40
	m.availableAgents = []model.AgentType{model.AgentClaude}
	m.defaultAgent = model.AgentClaude
	v := m.View()
	if !strings.Contains(v, "env") {
		t.Error("expected 'env' column header when integration sessions exist")
	}
	if !strings.Contains(v, "tmux") {
		t.Error("expected 'tmux' label in env column")
	}
	if !strings.Contains(v, "embedded") {
		t.Error("expected 'embedded' label for PTY session")
	}
	// Empty Source should show blank, not "embedded"
	lines := strings.Split(v, "\n")
	for _, line := range lines {
		if strings.Contains(line, "gamma") {
			if strings.Contains(line, "embedded") {
				t.Error("empty Source should show blank in env column, not 'embedded'")
			}
			break
		}
	}
}

func TestEnvColumnHiddenPtyOnly(t *testing.T) {
	store := makeStore(t)
	store.Projects = makeProjects("/a/alpha", "/a/beta")
	for _, p := range store.Projects {
		store.SetSession(&model.AgentSession{
			ProjectDir: p.Dir,
			SessionID:  "pty-" + p.Name,
			Type:       model.AgentClaude,
			Status:     model.StatusIdle,
			Source:     "pty",
		})
	}
	m := newTestModelWithStore(&mockBackend{}, store)
	m.width = 120
	m.height = 40
	m.availableAgents = []model.AgentType{model.AgentClaude}
	m.defaultAgent = model.AgentClaude
	v := m.View()
	// "env" should not appear as a column header; note "agent" contains "en" but not "env" as a standalone word
	lines := strings.Split(v, "\n")
	for _, line := range lines {
		// Check the header line (contains "agent" and "type")
		if strings.Contains(line, "agent") && strings.Contains(line, "type") {
			if strings.Contains(line, "env") {
				t.Error("expected no 'env' column header when all sessions are PTY-only")
			}
			break
		}
	}
}

func TestNormalizeView(t *testing.T) {
	t.Run("height clamped", func(t *testing.T) {
		// Too few lines — should pad
		out := normalizeView("a\nb", 10, 5)
		lines := strings.Split(out, "\n")
		if len(lines) != 5 {
			t.Errorf("got %d lines, want 5", len(lines))
		}

		// Too many lines — should truncate
		out = normalizeView("1\n2\n3\n4\n5\n6\n7", 10, 3)
		lines = strings.Split(out, "\n")
		if len(lines) != 3 {
			t.Errorf("got %d lines, want 3", len(lines))
		}
	})

	t.Run("width normalized", func(t *testing.T) {
		out := normalizeView("abc\nx", 5, 2)
		lines := strings.Split(out, "\n")
		if len(lines) != 2 {
			t.Fatalf("got %d lines, want 2", len(lines))
		}
		for i, line := range lines {
			if got := lipgloss.Width(line); got != 5 {
				t.Fatalf("line %d width = %d, want 5: %q", i, got, line)
			}
		}
	})
}

// --- Quick-response arm-mode tests ---

func setupQuickResponseModel(t *testing.T, status model.AgentStatus, streamOpen bool) (Model, *mockBackend) {
	t.Helper()
	store := makeStore(t)
	store.AddProject("/proj/test")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/proj/test",
		SessionID:  "sid",
		Type:       model.AgentClaude,
		Status:     status,
	})
	backend := &mockBackend{sessions: []terminal.Session{{ID: "sid", Name: "claude"}}}
	m := newTestModelWithStore(backend, store)
	m.backendOK = true
	m.streamOpen = streamOpen
	// Rebuild rows so cursor 0 maps to the session.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = modelFrom(updated)
	return m, backend
}

func TestQuickResponseArm(t *testing.T) {
	m, backend := setupQuickResponseModel(t, model.StatusNeedsInput, true)
	updated, cmd := m.Update(ctrlKeyMsg(tea.KeyCtrlR))
	um := modelFrom(updated)
	if !um.quickResponseArmed {
		t.Fatal("expected quick response to be armed")
	}
	if um.quickResponseSessionID != "sid" {
		t.Fatalf("expected armed session sid, got %q", um.quickResponseSessionID)
	}
	if !strings.Contains(um.statusText, "Quick response armed:") {
		t.Errorf("expected armed status text, got %q", um.statusText)
	}
	if cmd == nil {
		t.Fatal("expected timeout command")
	}
	if len(backend.sendTextLog) != 0 {
		t.Fatalf("expected no sends while arming, got %v", backend.sendTextLog)
	}
}

func TestQuickResponseAcceptAfterArming(t *testing.T) {
	m, backend := setupQuickResponseModel(t, model.StatusNeedsInput, true)
	updated, _ := m.Update(ctrlKeyMsg(tea.KeyCtrlR))
	m = modelFrom(updated)

	updated, cmd := m.Update(keyMsg("y"))
	um := modelFrom(updated)
	if um.quickResponseArmed {
		t.Fatal("expected quick response to clear after sending")
	}
	if !strings.Contains(um.statusText, `Sent "y"`) {
		t.Errorf("expected sent status, got %q", um.statusText)
	}
	if cmd == nil {
		t.Fatal("expected a command")
	}
	cmd()
	if len(backend.sendTextLog) < 2 {
		t.Fatalf("expected 2 sends, got %d: %v", len(backend.sendTextLog), backend.sendTextLog)
	}
	if backend.sendTextLog[0] != "sid:y" {
		t.Errorf("expected first send 'sid:y', got %q", backend.sendTextLog[0])
	}
	if backend.sendTextLog[1] != "sid:\r" {
		t.Errorf("expected second send 'sid:\\r', got %q", backend.sendTextLog[1])
	}
}

func TestQuickResponseRejectAfterArming(t *testing.T) {
	m, backend := setupQuickResponseModel(t, model.StatusNeedsInput, true)
	updated, _ := m.Update(ctrlKeyMsg(tea.KeyCtrlR))
	m = modelFrom(updated)

	updated, cmd := m.Update(ctrlKeyMsg(tea.KeyEsc))
	um := modelFrom(updated)
	if um.quickResponseArmed {
		t.Fatal("expected quick response to clear after reject")
	}
	if !strings.Contains(um.statusText, "Rejected") {
		t.Errorf("expected rejected status, got %q", um.statusText)
	}
	if cmd == nil {
		t.Fatal("expected a command")
	}
	cmd()
	if len(backend.sendTextLog) != 1 {
		t.Fatalf("expected 1 send, got %d: %v", len(backend.sendTextLog), backend.sendTextLog)
	}
	if backend.sendTextLog[0] != "sid:\x1b" {
		t.Errorf("expected first send 'sid:\\x1b', got %q", backend.sendTextLog[0])
	}
}

func TestQuickResponseDigitAfterArming(t *testing.T) {
	m, backend := setupQuickResponseModel(t, model.StatusNeedsInput, true)
	updated, _ := m.Update(ctrlKeyMsg(tea.KeyCtrlR))
	m = modelFrom(updated)

	_, cmd := m.Update(keyMsg("5"))
	if cmd == nil {
		t.Fatal("expected a command")
	}
	cmd()
	if len(backend.sendTextLog) < 2 {
		t.Fatalf("expected 2 sends, got %d: %v", len(backend.sendTextLog), backend.sendTextLog)
	}
	if backend.sendTextLog[0] != "sid:5" {
		t.Errorf("expected first send 'sid:5', got %q", backend.sendTextLog[0])
	}
}

func TestQuickResponseArmBlockedStreamClosed(t *testing.T) {
	m, backend := setupQuickResponseModel(t, model.StatusNeedsInput, false)
	updated, cmd := m.Update(ctrlKeyMsg(tea.KeyCtrlR))
	um := modelFrom(updated)
	if cmd != nil {
		t.Error("expected nil command when stream is closed")
	}
	if um.quickResponseArmed {
		t.Error("expected quick response to remain unarmed")
	}
	if len(backend.sendTextLog) != 0 {
		t.Errorf("expected no sends, got %v", backend.sendTextLog)
	}
}

func TestQuickResponseArmBlockedNotNeedsInput(t *testing.T) {
	m, backend := setupQuickResponseModel(t, model.StatusIdle, true)
	updated, cmd := m.Update(ctrlKeyMsg(tea.KeyCtrlR))
	um := modelFrom(updated)
	if cmd != nil {
		t.Error("expected nil command when status is idle")
	}
	if um.quickResponseArmed {
		t.Error("expected quick response to remain unarmed")
	}
	if len(backend.sendTextLog) != 0 {
		t.Errorf("expected no sends, got %v", backend.sendTextLog)
	}
}

func TestQuickResponseArmBlockedNoSession(t *testing.T) {
	store := makeStore(t)
	backend := &mockBackend{}
	m := newTestModelWithStore(backend, store)
	m.backendOK = true
	m.streamOpen = true
	updated, cmd := m.Update(ctrlKeyMsg(tea.KeyCtrlR))
	um := modelFrom(updated)
	if cmd != nil {
		t.Error("expected nil command when no sessions")
	}
	if um.quickResponseArmed {
		t.Error("expected quick response to remain unarmed")
	}
}

func TestQuickResponseArmClearsOnUnrelatedKeyAndFallsThrough(t *testing.T) {
	store := makeStore(t)
	store.AddProject("/proj/alpha")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/proj/alpha",
		SessionID:  "sid-1",
		Type:       model.AgentClaude,
		Status:     model.StatusNeedsInput,
	})
	store.AddProject("/proj/beta")
	store.SetSession(&model.AgentSession{
		ProjectDir: "/proj/beta",
		SessionID:  "sid-2",
		Type:       model.AgentClaude,
		Status:     model.StatusIdle,
	})
	m := newTestModelWithStore(&mockBackend{}, store)
	m.backendOK = true
	m.streamOpen = true
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = modelFrom(updated)

	updated, _ = m.Update(ctrlKeyMsg(tea.KeyCtrlR))
	m = modelFrom(updated)

	updated, _ = m.Update(keyMsg("j"))
	um := modelFrom(updated)
	if um.quickResponseArmed {
		t.Fatal("expected unrelated key to clear armed state")
	}
	if um.cursor != 1 {
		t.Errorf("expected unrelated key to continue normal navigation, got cursor %d", um.cursor)
	}
}

func TestQuickResponseArmExpires(t *testing.T) {
	m, _ := setupQuickResponseModel(t, model.StatusNeedsInput, true)
	updated, _ := m.Update(ctrlKeyMsg(tea.KeyCtrlR))
	m = modelFrom(updated)

	updated, _ = m.Update(QuickResponseArmExpiredMsg{SessionID: "sid"})
	um := modelFrom(updated)
	if um.quickResponseArmed {
		t.Fatal("expected timeout to clear armed state")
	}
	if um.statusText != "" {
		t.Errorf("expected timeout to clear armed status text, got %q", um.statusText)
	}
}

func TestQuickResponseArmClearsWhenStatusLeavesNeedsInput(t *testing.T) {
	m, _ := setupQuickResponseModel(t, model.StatusNeedsInput, true)
	updated, _ := m.Update(ctrlKeyMsg(tea.KeyCtrlR))
	m = modelFrom(updated)

	updated, _ = m.Update(StatusUpdatedMsg{
		SessionID:  "sid",
		ProjectDir: "/proj/test",
		Status:     model.StatusIdle,
	})
	um := modelFrom(updated)
	if um.quickResponseArmed {
		t.Fatal("expected status change to clear armed state")
	}
}

// --- Stream panel hint tests ---

func TestStreamPanelHintNeedsInput(t *testing.T) {
	session := &model.AgentSession{
		SessionID: "sid",
		Status:    model.StatusNeedsInput,
		Type:      model.AgentClaude,
	}
	out := renderStreamPanel(session, "test", "/proj/test", 120, 10, false)
	if !strings.Contains(out, "ctrl+r") {
		t.Error("expected hint text 'ctrl+r' in stream panel")
	}
}

func TestStreamPanelHintArmedNeedsInput(t *testing.T) {
	session := &model.AgentSession{
		SessionID: "sid",
		Status:    model.StatusNeedsInput,
		Type:      model.AgentClaude,
	}
	out := renderStreamPanel(session, "test", "/proj/test", 120, 10, true)
	if !strings.Contains(out, "esc:reject") {
		t.Error("expected armed hint text in stream panel")
	}
}

func TestStreamPanelHintNarrowWidth(t *testing.T) {
	session := &model.AgentSession{
		SessionID: "sid",
		Status:    model.StatusNeedsInput,
		Type:      model.AgentClaude,
	}
	// Should not panic at narrow width
	out := renderStreamPanel(session, "test", "/proj/test", 12, 10, false)
	if strings.Contains(out, "ctrl+r:respond") {
		t.Error("should not show full hint at narrow width")
	}
}

func TestStreamPanelNoHintWhenIdle(t *testing.T) {
	session := &model.AgentSession{
		SessionID: "sid",
		Status:    model.StatusIdle,
		Type:      model.AgentClaude,
	}
	out := renderStreamPanel(session, "test", "/proj/test", 120, 10, false)
	if strings.Contains(out, "ctrl+r") || strings.Contains(out, "esc:reject") {
		t.Error("should not show hints when idle")
	}
}
