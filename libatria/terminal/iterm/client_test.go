package iterm

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/sethdeckard/atria/libatria/terminal"
	pb "github.com/sethdeckard/atria/libatria/terminal/iterm/proto"
	"google.golang.org/protobuf/proto"
)

func TestNewClient(t *testing.T) {
	c := NewClient(Options{})
	if c.socketPath != "" {
		t.Errorf("expected empty socketPath, got %q", c.socketPath)
	}
}

func TestNewClientWithSocket(t *testing.T) {
	c := NewClient(Options{SocketPath: "/tmp/test-socket"})
	if c.socketPath != "/tmp/test-socket" {
		t.Errorf("expected socketPath %q, got %q", "/tmp/test-socket", c.socketPath)
	}
}

func TestAvailableErrorBadSocket(t *testing.T) {
	c := NewClient(Options{SocketPath: "/nonexistent/socket/path"})
	err := c.Available()
	if err == nil {
		t.Fatal("expected error when socket does not exist")
	}
	if !errors.Is(err, terminal.ErrUnavailable) {
		t.Fatalf("missing socket = %v, want ErrUnavailable", err)
	}
}

func TestCollectSessionsNil(t *testing.T) {
	sessions := collectSessions(nil)
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions from nil node, got %d", len(sessions))
	}
}

func TestCollectSessionsFlat(t *testing.T) {
	// Single session (no splits)
	node := &pb.SplitTreeNode{
		Links: []*pb.SplitTreeNode_SplitTreeLink{
			{Child: &pb.SplitTreeNode_SplitTreeLink_Session{
				Session: &pb.SessionSummary{
					UniqueIdentifier: proto.String("sess-1"),
					Title:            proto.String("bash"),
				},
			}},
		},
	}
	sessions := collectSessions(node)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].GetUniqueIdentifier() != "sess-1" {
		t.Errorf("expected sess-1, got %s", sessions[0].GetUniqueIdentifier())
	}
}

func TestCollectSessionsSplits(t *testing.T) {
	// Vertical split with 2 panes, one containing a nested horizontal split
	node := &pb.SplitTreeNode{
		Vertical: proto.Bool(true),
		Links: []*pb.SplitTreeNode_SplitTreeLink{
			{Child: &pb.SplitTreeNode_SplitTreeLink_Session{
				Session: &pb.SessionSummary{
					UniqueIdentifier: proto.String("left"),
					Title:            proto.String("left pane"),
				},
			}},
			{Child: &pb.SplitTreeNode_SplitTreeLink_Node{
				Node: &pb.SplitTreeNode{
					Links: []*pb.SplitTreeNode_SplitTreeLink{
						{Child: &pb.SplitTreeNode_SplitTreeLink_Session{
							Session: &pb.SessionSummary{
								UniqueIdentifier: proto.String("top-right"),
								Title:            proto.String("top right"),
							},
						}},
						{Child: &pb.SplitTreeNode_SplitTreeLink_Session{
							Session: &pb.SessionSummary{
								UniqueIdentifier: proto.String("bottom-right"),
								Title:            proto.String("bottom right"),
							},
						}},
					},
				},
			}},
		},
	}
	sessions := collectSessions(node)
	if len(sessions) != 3 {
		t.Fatalf("expected 3 sessions from split tree, got %d", len(sessions))
	}
	ids := make(map[string]bool)
	for _, s := range sessions {
		ids[s.GetUniqueIdentifier()] = true
	}
	for _, want := range []string{"left", "top-right", "bottom-right"} {
		if !ids[want] {
			t.Errorf("missing session %q", want)
		}
	}
}

func TestProtobufRoundTrip(t *testing.T) {
	req := &pb.ClientOriginatedMessage{
		Id: proto.Int64(42),
		Submessage: &pb.ClientOriginatedMessage_SendTextRequest{
			SendTextRequest: &pb.SendTextRequest{
				Session: proto.String("sess-1"),
				Text:    proto.String("hello\n"),
			},
		},
	}
	data, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	decoded := &pb.ClientOriginatedMessage{}
	if err := proto.Unmarshal(data, decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if decoded.GetId() != 42 {
		t.Errorf("expected id 42, got %d", decoded.GetId())
	}
	str := decoded.GetSendTextRequest()
	if str == nil {
		t.Fatal("expected SendTextRequest")
	}
	if str.GetSession() != "sess-1" {
		t.Errorf("expected session sess-1, got %s", str.GetSession())
	}
	if str.GetText() != "hello\n" {
		t.Errorf("expected text hello\\n, got %s", str.GetText())
	}
}

func TestMonitorOutputNotSupported(t *testing.T) {
	c := NewClient(Options{})
	_, err := c.MonitorOutput("sess-1", "/tmp/log", "pattern")
	if err == nil {
		t.Fatal("expected error for unsupported MonitorOutput")
	}
}

func TestDefaultSocketPath(t *testing.T) {
	path := defaultSocketPath()
	if path == "" {
		t.Fatal("expected non-empty default socket path")
	}
}

func TestExtractFocusedWindowID(t *testing.T) {
	tests := []struct {
		name   string
		fr     *pb.FocusResponse
		wantID string
		wantOK bool
	}{
		{
			name:   "nil FocusResponse",
			fr:     nil,
			wantID: "",
			wantOK: false,
		},
		{
			name:   "empty notifications",
			fr:     &pb.FocusResponse{},
			wantID: "",
			wantOK: false,
		},
		{
			name: "window became key",
			fr: &pb.FocusResponse{
				Notifications: []*pb.FocusChangedNotification{
					{Event: &pb.FocusChangedNotification_Window_{
						Window: &pb.FocusChangedNotification_Window{
							WindowStatus: pb.FocusChangedNotification_Window_TERMINAL_WINDOW_BECAME_KEY.Enum(),
							WindowId:     proto.String("win-1"),
						},
					}},
				},
			},
			wantID: "win-1",
			wantOK: true,
		},
		{
			name: "window is current",
			fr: &pb.FocusResponse{
				Notifications: []*pb.FocusChangedNotification{
					{Event: &pb.FocusChangedNotification_Window_{
						Window: &pb.FocusChangedNotification_Window{
							WindowStatus: pb.FocusChangedNotification_Window_TERMINAL_WINDOW_IS_CURRENT.Enum(),
							WindowId:     proto.String("win-2"),
						},
					}},
				},
			},
			wantID: "win-2",
			wantOK: true,
		},
		{
			name: "non-window notifications only",
			fr: &pb.FocusResponse{
				Notifications: []*pb.FocusChangedNotification{
					{Event: &pb.FocusChangedNotification_ApplicationActive{
						ApplicationActive: true,
					}},
					{Event: &pb.FocusChangedNotification_SelectedTab{
						SelectedTab: "tab-1",
					}},
				},
			},
			wantID: "",
			wantOK: false,
		},
		{
			name: "multiple notifications first key wins",
			fr: &pb.FocusResponse{
				Notifications: []*pb.FocusChangedNotification{
					{Event: &pb.FocusChangedNotification_ApplicationActive{
						ApplicationActive: true,
					}},
					{Event: &pb.FocusChangedNotification_Window_{
						Window: &pb.FocusChangedNotification_Window{
							WindowStatus: pb.FocusChangedNotification_Window_TERMINAL_WINDOW_BECAME_KEY.Enum(),
							WindowId:     proto.String("win-first"),
						},
					}},
					{Event: &pb.FocusChangedNotification_Window_{
						Window: &pb.FocusChangedNotification_Window{
							WindowStatus: pb.FocusChangedNotification_Window_TERMINAL_WINDOW_IS_CURRENT.Enum(),
							WindowId:     proto.String("win-second"),
						},
					}},
				},
			},
			wantID: "win-first",
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotOK := extractFocusedWindowID(tt.fr)
			if gotID != tt.wantID || gotOK != tt.wantOK {
				t.Errorf("extractFocusedWindowID() = (%q, %v), want (%q, %v)",
					gotID, gotOK, tt.wantID, tt.wantOK)
			}
		})
	}
}

func TestLineRangeForVisibleScreen(t *testing.T) {
	tests := []struct {
		name      string
		jsonValue string
		wantStart int64
		wantEnd   int64
	}{
		{
			name:      "uses first visible when present",
			jsonValue: `{"overflow":12,"history":40,"grid":25,"first_visible":50}`,
			wantStart: 50,
			wantEnd:   75,
		},
		{
			name:      "falls back to bottom of history",
			jsonValue: `{"overflow":12,"history":40,"grid":25}`,
			wantStart: 52,
			wantEnd:   77,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := lineRangeForVisibleScreen(tt.jsonValue)
			if err != nil {
				t.Fatalf("lineRangeForVisibleScreen() error = %v", err)
			}
			if got.GetWindowedCoordRange().GetCoordRange().GetStart().GetY() != tt.wantStart {
				t.Errorf("start y = %d, want %d",
					got.GetWindowedCoordRange().GetCoordRange().GetStart().GetY(), tt.wantStart)
			}
			if got.GetWindowedCoordRange().GetCoordRange().GetEnd().GetY() != tt.wantEnd {
				t.Errorf("end y = %d, want %d",
					got.GetWindowedCoordRange().GetCoordRange().GetEnd().GetY(), tt.wantEnd)
			}
		})
	}
}

func TestLineRangeForVisibleScreenErrors(t *testing.T) {
	tests := []string{
		`not json`,
		`{"overflow":1,"history":2,"grid":0}`,
	}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			if _, err := lineRangeForVisibleScreen(input); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestScrollbackRange(t *testing.T) {
	tests := []struct {
		name      string
		info      lineInfo
		lines     int
		wantStart int64
		wantEnd   int64
	}{
		{"one grid extra above the requested lines", lineInfo{Overflow: 5, History: 100, Grid: 4}, 3, 102, 109},
		{"history shorter than requested", lineInfo{Overflow: 5, History: 10, Grid: 4}, 50, 5, 19},
		{"no history", lineInfo{Grid: 24}, 100, 0, 24},
		{"lines within the grid", lineInfo{History: 100, Grid: 40}, 25, 75, 140},
		{"ignores a scrolled view", lineInfo{History: 100, Grid: 4, FirstVisible: proto.Int64(10)}, 2, 98, 104},
	}
	for _, tt := range tests {
		r := scrollbackRange(tt.info, tt.lines).GetWindowedCoordRange().GetCoordRange()
		if r.GetStart().GetY() != tt.wantStart || r.GetEnd().GetY() != tt.wantEnd {
			t.Errorf("%s: range = %d..%d, want %d..%d", tt.name, r.GetStart().GetY(), r.GetEnd().GetY(), tt.wantStart, tt.wantEnd)
		}
	}
}

func TestReadScrollback(t *testing.T) {
	var (
		mu       sync.Mutex
		props    []string
		lineRngs []*pb.LineRange
	)
	row := func(text string) *pb.LineContents { return &pb.LineContents{Text: proto.String(text)} }
	bold := &pb.LineContents{
		Text:  proto.String("s2"),
		Style: []*pb.CellStyle{{Bold: proto.Bool(true), Repeats: proto.Uint32(2)}},
	}
	sock, _ := startFakeITermWith(t, "", func(req *pb.ClientOriginatedMessage) *pb.ServerOriginatedMessage {
		mu.Lock()
		defer mu.Unlock()
		if gp := req.GetGetPropertyRequest(); gp != nil {
			props = append(props, gp.GetName())
			return &pb.ServerOriginatedMessage{Submessage: &pb.ServerOriginatedMessage_GetPropertyResponse{
				GetPropertyResponse: &pb.GetPropertyResponse{JsonValue: proto.String(`{"overflow":5,"history":100,"grid":4}`)},
			}}
		}
		lineRngs = append(lineRngs, req.GetGetBufferRequest().GetLineRange())
		return &pb.ServerOriginatedMessage{Submessage: &pb.ServerOriginatedMessage_GetBufferResponse{
			GetBufferResponse: &pb.GetBufferResponse{Contents: []*pb.LineContents{
				row("h1"), row("h2"), row("h3"), row("s1"), bold, row(" "), row("\x00\x00"),
			}},
		}}
	})
	c := NewClient(Options{SocketPath: sock, NoPrompt: true})
	t.Cleanup(func() { _ = c.Close() })

	plain, err := c.ReadScrollback("s", 3, false)
	if err != nil {
		t.Fatal(err)
	}
	if want := "h3\ns1\ns2"; plain != want {
		t.Errorf("plain = %q, want %q", plain, want)
	}
	styled, err := c.ReadScrollback("s", 3, true)
	if err != nil {
		t.Fatal(err)
	}
	if want := "h3\ns1\n\x1b[1ms2\x1b[0m"; styled != want {
		t.Errorf("styled = %q, want %q", styled, want)
	}

	if _, err := c.ReadScrollback("s", 0, false); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ReadScrollback("s", -1, true); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if n := len(lineRngs); n != 4 || !lineRngs[2].GetScreenContentsOnly() || !lineRngs[3].GetScreenContentsOnly() {
		t.Fatalf("buffer requests = %v, want screen reads for lines 0 and -1 after two history reads", lineRngs)
	}
	lineRngs = lineRngs[:2]
	if len(props) != 2 || props[0] != "number_of_lines" {
		t.Errorf("property requests = %q, want number_of_lines per read", props)
	}
	for _, lr := range lineRngs {
		r := lr.GetWindowedCoordRange().GetCoordRange()
		if lr.GetScreenContentsOnly() || r.GetStart().GetY() != 102 || r.GetEnd().GetY() != 109 {
			t.Errorf("buffer range = %v, want rows 102..109", lr)
		}
	}
	if len(lineRngs) != 2 {
		t.Errorf("buffer requests = %d, want one per read", len(lineRngs))
	}
}

func TestNewTabAndSplitPane(t *testing.T) {
	var (
		mu   sync.Mutex
		reqs []*pb.ClientOriginatedMessage
	)
	sock, _ := startFakeITermWith(t, "", func(req *pb.ClientOriginatedMessage) *pb.ServerOriginatedMessage {
		mu.Lock()
		reqs = append(reqs, req)
		mu.Unlock()
		switch {
		case req.GetCreateTabRequest() != nil:
			status := pb.CreateTabResponse_OK
			if req.GetCreateTabRequest().GetWindowId() == "gone" {
				status = pb.CreateTabResponse_INVALID_WINDOW_ID
			}
			return &pb.ServerOriginatedMessage{Submessage: &pb.ServerOriginatedMessage_CreateTabResponse{
				CreateTabResponse: &pb.CreateTabResponse{Status: status.Enum(), SessionId: proto.String("tab-s")},
			}}
		case req.GetSplitPaneRequest() != nil:
			status := pb.SplitPaneResponse_OK
			if req.GetSplitPaneRequest().GetSession() == "full" {
				status = pb.SplitPaneResponse_CANNOT_SPLIT
			}
			return &pb.ServerOriginatedMessage{Submessage: &pb.ServerOriginatedMessage_SplitPaneResponse{
				SplitPaneResponse: &pb.SplitPaneResponse{Status: status.Enum(), SessionId: []string{"split-s"}},
			}}
		case req.GetVariableRequest() != nil:
			return &pb.ServerOriginatedMessage{Submessage: &pb.ServerOriginatedMessage_VariableResponse{
				VariableResponse: &pb.VariableResponse{Values: []string{`"/home/proj"`}},
			}}
		default:
			return &pb.ServerOriginatedMessage{Submessage: &pb.ServerOriginatedMessage_ActivateResponse{
				ActivateResponse: &pb.ActivateResponse{},
			}}
		}
	})
	c := NewClient(Options{SocketPath: sock, NoPrompt: true})
	t.Cleanup(func() { _ = c.Close() })

	if id, err := c.NewTab("w1", "/p"); err != nil || id != "tab-s" {
		t.Fatalf("NewTab = %q, %v", id, err)
	}
	if id, err := c.SplitPane("orig", terminal.SplitRight, ""); err != nil || id != "split-s" {
		t.Fatalf("SplitPane = %q, %v", id, err)
	}
	if id, err := c.SplitPane("orig", terminal.SplitBelow, "/q"); err != nil || id != "split-s" {
		t.Fatalf("SplitPane below = %q, %v", id, err)
	}
	if _, err := c.NewTab("gone", ""); err == nil || !strings.Contains(err.Error(), "INVALID_WINDOW_ID") {
		t.Errorf("NewTab in a missing window = %v, want INVALID_WINDOW_ID", err)
	}
	if _, err := c.SplitPane("full", terminal.SplitBelow, "/q"); err == nil || !strings.Contains(err.Error(), "CANNOT_SPLIT") {
		t.Errorf("SplitPane that can't split = %v, want CANNOT_SPLIT", err)
	}

	mu.Lock()
	defer mu.Unlock()
	dirOf := func(props []*pb.ProfileProperty) string {
		if len(props) != 2 || props[0].GetKey() != "Custom Directory" || props[0].GetJsonValue() != `"Yes"` ||
			props[1].GetKey() != "Working Directory" {
			t.Errorf("profile properties = %v, want Custom Directory then Working Directory", props)
			return ""
		}
		return props[1].GetJsonValue()
	}
	// A failed split sends no reselect, so the failed split is the last request.
	if len(reqs) != 8 {
		t.Fatalf("requests = %d, want 8: %v", len(reqs), reqs)
	}
	tab := reqs[0].GetCreateTabRequest()
	if tab.GetWindowId() != "w1" || tab.SelectTab == nil || tab.GetSelectTab() || dirOf(tab.GetCustomProfileProperties()) != `"/p"` {
		t.Errorf("create tab = %v, want window w1 in the background in /p", tab)
	}
	if v := reqs[1].GetVariableRequest(); v.GetSessionId() != "orig" || len(v.GetGet()) != 1 || v.GetGet()[0] != "path" {
		t.Errorf("an empty dir should read path first, got %v", reqs[1])
	}
	right := reqs[2].GetSplitPaneRequest()
	if right.GetSession() != "orig" || right.GetSplitDirection() != pb.SplitPaneRequest_VERTICAL || dirOf(right.GetCustomProfileProperties()) != `"/home/proj"` {
		t.Errorf("split right = %v, want a vertical split of orig in /home/proj", right)
	}
	for _, i := range []int{3, 5} {
		a := reqs[i].GetActivateRequest()
		if a.GetSessionId() != "orig" || !a.GetSelectSession() || a.OrderWindowFront != nil || a.SelectTab != nil {
			t.Errorf("request %d = %v, want select_session on orig and nothing brought forward", i, reqs[i])
		}
	}
	if below := reqs[4].GetSplitPaneRequest(); below.GetSplitDirection() != pb.SplitPaneRequest_HORIZONTAL || dirOf(below.GetCustomProfileProperties()) != `"/q"` {
		t.Errorf("split below = %v, want a horizontal split in /q", below)
	}
	if reqs[7].GetSplitPaneRequest().GetSession() != "full" {
		t.Errorf("last request = %v, want the failed split with no reselect after it", reqs[7])
	}
}

func TestDirProperties(t *testing.T) {
	if got := dirProperties(""); got != nil {
		t.Errorf("dirProperties(\"\") = %v, want nil", got)
	}
	if got := dirProperties(`/a "b"`); got[1].GetJsonValue() != `"/a \"b\""` {
		t.Errorf("dirProperties quoted %q", got[1].GetJsonValue())
	}
}

func TestJoinBufferLines(t *testing.T) {
	got := joinBufferLines([]*pb.LineContents{
		{Text: proto.String("one")},
		{Text: proto.String("two")},
		{Text: proto.String("three")},
	}, 2)
	if got != "two\nthree" {
		t.Errorf("joinBufferLines() = %q, want %q", got, "two\nthree")
	}
}

func TestJoinBufferLinesAnchorsToLastNonblank(t *testing.T) {
	var lines []*pb.LineContents
	for _, text := range []string{"header", "body", "tail"} {
		lines = append(lines, &pb.LineContents{Text: proto.String(text)})
	}
	for i := 0; i < 5; i++ {
		lines = append(lines, &pb.LineContents{Text: proto.String("")})
	}

	got := joinBufferLines(lines, 4)
	want := "header\nbody\ntail"
	if got != want {
		t.Errorf("joinBufferLines() = %q, want %q", got, want)
	}
}

func TestJoinBufferLinesReplacesNulWithSpace(t *testing.T) {
	lines := []*pb.LineContents{
		{Text: proto.String("a\x00\x00b")},
		{Text: proto.String("tail\x00\x00")},
		{Text: proto.String("\x00\x00\x00")},
		{Text: proto.String("\x00\x00\x00")},
	}
	got := joinBufferLines(lines, 3)
	want := "a  b\ntail  "
	if got != want {
		t.Errorf("joinBufferLines() = %q, want %q", got, want)
	}

	// A zero limit disables trimming; all-NUL rows become spaces.
	got = joinBufferLines(lines[2:], 0)
	if want := "   \n   "; got != want {
		t.Errorf("joinBufferLines() = %q, want %q", got, want)
	}
}

func TestIsSemanticallyBlankIgnoresNulls(t *testing.T) {
	if !isSemanticallyBlank("\x00 \x00\t") {
		t.Fatal("expected NUL-padded whitespace to be blank")
	}
	if isSemanticallyBlank("Claude\x00Code") {
		t.Fatal("expected visible text with NUL separators to be nonblank")
	}
}

func TestUnquoteJSON(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{`"\/Users\/example\/projects"`, "/Users/example/projects"},
		{`"go\/agent-tui"`, "go/agent-tui"},
		{`"plain string"`, "plain string"},
		{`"with \"quotes\""`, `with "quotes"`},
		{"no quotes", "no quotes"},
		{"", ""},
		{`"unterminated`, `"unterminated`},
	}
	for _, tt := range tests {
		got := unquoteJSON(tt.input)
		if got != tt.want {
			t.Errorf("unquoteJSON(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNewClientNamesItselfToITerm(t *testing.T) {
	c := NewClient(Options{})
	if c.clientName != DefaultClientName {
		t.Fatalf("default client name = %q, want %q", c.clientName, DefaultClientName)
	}
	c = NewClient(Options{ClientName: "atria", NoPrompt: true})
	if c.clientName != "atria" || !c.noPrompt {
		t.Fatalf("options not applied: %+v", c)
	}
	cn := &conn{clientName: "atria"}
	h := cn.buildHeaders()
	// The header keys are set as literal lowercase map keys (iTerm2 reads
	// them that way), so look them up case-insensitively rather than with
	// Get, which canonicalizes the key.
	if got := rawHeader(h, "x-iterm2-advisory-name"); got != "atria" {
		t.Fatalf("advisory name = %q, want atria", got)
	}
	if got := rawHeader(h, "x-iterm2-library-version"); got != "go-atria 1.0" {
		t.Fatalf("library version = %q, want go-atria 1.0", got)
	}
	if got := rawHeader((&conn{}).buildHeaders(), "x-iterm2-advisory-name"); got != DefaultClientName {
		t.Fatalf("default advisory name = %q, want %q", got, DefaultClientName)
	}
}

// rawHeader finds a header by case-insensitive key without canonicalizing.
func rawHeader(h http.Header, name string) string {
	for k, v := range h {
		if strings.EqualFold(k, name) && len(v) == 1 {
			return v[0]
		}
	}
	return ""
}

func pane(id, title string) *pb.SplitTreeNode_SplitTreeLink {
	return &pb.SplitTreeNode_SplitTreeLink{Child: &pb.SplitTreeNode_SplitTreeLink_Session{
		Session: &pb.SessionSummary{UniqueIdentifier: proto.String(id), Title: proto.String(title)},
	}}
}

func TestSessionsFromWindowsFillsPlacement(t *testing.T) {
	windows := []*pb.ListSessionsResponse_Window{
		{WindowId: proto.String("w-a"), Tabs: []*pb.ListSessionsResponse_Tab{
			{TabId: proto.String("1"), Root: &pb.SplitTreeNode{Links: []*pb.SplitTreeNode_SplitTreeLink{
				pane("left", "✳ Claude Code"), pane("right", "zsh"),
			}}},
			{TabId: proto.String("2"), Root: &pb.SplitTreeNode{Links: []*pb.SplitTreeNode_SplitTreeLink{
				pane("solo", "htop"),
			}}},
		}},
		{WindowId: proto.String("w-b"), Tabs: []*pb.ListSessionsResponse_Tab{
			{TabId: proto.String("3"), Root: &pb.SplitTreeNode{Links: []*pb.SplitTreeNode_SplitTreeLink{
				pane("other", "vim"),
			}}},
		}},
	}
	titles := map[string]string{"w-a": "example@host:~", "w-b": "example@host:~/src", "1": "✳ Claude Code (claude)", "3": "vim"}
	title := func(scope *pb.VariableRequest) string {
		if id := scope.GetWindowId(); id != "" {
			return titles[id]
		}
		return titles[scope.GetTabId()] // tab 2's request fails: no title
	}
	vars := func(id string) sessionVars {
		v := sessionVars{tty: "/dev/" + id, job: "job-" + id}
		if id == "left" {
			v.cwd = "/Users/example/app" // no shell integration elsewhere
		}
		return v
	}

	got := sessionsFromWindows(windows, vars, title)

	winA := terminal.Place{ID: "w-a", Title: "example@host:~", Index: 0}
	winB := terminal.Place{ID: "w-b", Title: "example@host:~/src", Index: 1}
	split := terminal.Place{ID: "1", Title: "✳ Claude Code (claude)", Index: 0}
	want := []terminal.Session{
		{ID: "left", Name: "✳ Claude Code", TTY: "/dev/left", Job: "job-left", Window: winA, Tab: split, Cwd: "/Users/example/app"},
		{ID: "right", Name: "zsh", TTY: "/dev/right", Job: "job-right", Window: winA, Tab: split},
		{ID: "solo", Name: "htop", TTY: "/dev/solo", Job: "job-solo", Window: winA, Tab: terminal.Place{ID: "2", Index: 1}},
		{ID: "other", Name: "vim", TTY: "/dev/other", Job: "job-other", Window: winB, Tab: terminal.Place{ID: "3", Title: "vim", Index: 0}},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d sessions, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("session %d:\n got %+v\nwant %+v", i, got[i], want[i])
		}
	}
}

func TestTitleValue(t *testing.T) {
	ok := pb.VariableResponse_OK
	missing := pb.VariableResponse_SESSION_NOT_FOUND
	tests := []struct {
		name string
		vr   *pb.VariableResponse
		want string
	}{
		{"quoted", &pb.VariableResponse{Status: &ok, Values: []string{`"~ (-zsh)"`}}, "~ (-zsh)"},
		{"null", &pb.VariableResponse{Status: &ok, Values: []string{"null"}}, ""},
		{"not found", &pb.VariableResponse{Status: &missing, Values: []string{`"x"`}}, ""},
		{"no values", &pb.VariableResponse{Status: &ok}, ""},
		{"no response", nil, ""},
	}
	for _, tt := range tests {
		if got := titleValue(tt.vr); got != tt.want {
			t.Errorf("%s: titleValue = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestSessionVarsFrom(t *testing.T) {
	ok := pb.VariableResponse_OK
	missing := pb.VariableResponse_SESSION_NOT_FOUND
	tests := []struct {
		name string
		vr   *pb.VariableResponse
		want sessionVars
	}{
		{"all three", &pb.VariableResponse{Status: &ok, Values: []string{`"/dev/ttys001"`, `"zsh"`, `"/Users/example/my app"`}},
			sessionVars{tty: "/dev/ttys001", job: "zsh", cwd: "/Users/example/my app"}},
		{"no path without shell integration", &pb.VariableResponse{Status: &ok, Values: []string{`"/dev/ttys001"`, `"zsh"`, "null"}},
			sessionVars{tty: "/dev/ttys001", job: "zsh"}},
		{"fewer values", &pb.VariableResponse{Status: &ok, Values: []string{`"/dev/ttys001"`}},
			sessionVars{tty: "/dev/ttys001"}},
		{"session gone", &pb.VariableResponse{Status: &missing, Values: []string{`"/dev/ttys001"`}}, sessionVars{}},
		{"no response", nil, sessionVars{}},
	}
	for _, tt := range tests {
		if got := sessionVarsFrom(tt.vr); got != tt.want {
			t.Errorf("%s: sessionVarsFrom = %+v, want %+v", tt.name, got, tt.want)
		}
	}
}

func sized(id string, cols, rows int32) *pb.SplitTreeNode_SplitTreeLink {
	return &pb.SplitTreeNode_SplitTreeLink{Child: &pb.SplitTreeNode_SplitTreeLink_Session{
		Session: &pb.SessionSummary{
			UniqueIdentifier: proto.String(id),
			GridSize:         &pb.Size{Width: proto.Int32(cols), Height: proto.Int32(rows)},
		},
	}}
}

func TestLayoutTab(t *testing.T) {
	// Side by side: left, and a stack of top-right over bottom-right.
	root := &pb.SplitTreeNode{
		Vertical: proto.Bool(true),
		Links: []*pb.SplitTreeNode_SplitTreeLink{
			sized("left", 80, 24),
			{Child: &pb.SplitTreeNode_SplitTreeLink_Node{Node: &pb.SplitTreeNode{
				Links: []*pb.SplitTreeNode_SplitTreeLink{sized("top-right", 79, 11), sized("bottom-right", 79, 12)},
			}}},
		},
	}
	want := map[string]terminal.Frame{
		"left":         {Left: 0, Top: 0, Width: 80, Height: 24},
		"top-right":    {Left: 81, Top: 0, Width: 79, Height: 11},
		"bottom-right": {Left: 81, Top: 12, Width: 79, Height: 12},
	}
	got := layoutTab(root)
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("%s = %+v, want %+v", id, got[id], w)
		}
	}

	// One session without a grid size leaves the whole tab unplaced.
	root.Links[1].GetNode().Links[1] = pane("bottom-right", "zsh")
	if got := layoutTab(root); got != nil {
		t.Errorf("with a missing grid size, layoutTab = %+v, want nil", got)
	}
}

func TestSessionsFromWindowsCarriesFrames(t *testing.T) {
	windows := []*pb.ListSessionsResponse_Window{{WindowId: proto.String("w"), Tabs: []*pb.ListSessionsResponse_Tab{
		{TabId: proto.String("1"), Root: &pb.SplitTreeNode{Links: []*pb.SplitTreeNode_SplitTreeLink{
			sized("top", 120, 20), sized("bottom", 120, 19),
		}}},
	}}}
	got := sessionsFromWindows(windows, func(string) sessionVars { return sessionVars{} }, func(*pb.VariableRequest) string { return "" })
	if len(got) != 2 || got[0].Frame != (terminal.Frame{Width: 120, Height: 20}) ||
		got[1].Frame != (terminal.Frame{Top: 21, Width: 120, Height: 19}) {
		t.Fatalf("frames = %+v", got)
	}
}
