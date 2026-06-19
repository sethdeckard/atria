package iterm

import (
	"testing"

	pb "github.com/sethdeckard/atria/internal/terminal/iterm/proto"
	"google.golang.org/protobuf/proto"
)

func TestNewClient(t *testing.T) {
	c := NewClient()
	if c.socketPath != "" {
		t.Errorf("expected empty socketPath, got %q", c.socketPath)
	}
}

func TestNewClientWithSocket(t *testing.T) {
	c := NewClient("/tmp/test-socket")
	if c.socketPath != "/tmp/test-socket" {
		t.Errorf("expected socketPath %q, got %q", "/tmp/test-socket", c.socketPath)
	}
}

func TestAvailableErrorBadSocket(t *testing.T) {
	c := NewClient("/nonexistent/socket/path")
	err := c.Available()
	if err == nil {
		t.Fatal("expected error when socket does not exist")
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
	c := NewClient()
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
