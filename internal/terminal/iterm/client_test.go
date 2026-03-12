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

func TestUnquoteJSON(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{`"\/Users\/seth\/projects"`, "/Users/seth/projects"},
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
