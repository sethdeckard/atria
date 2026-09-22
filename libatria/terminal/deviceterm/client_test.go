package deviceterm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sethdeckard/atria/libatria/terminal"
)

func TestNewClientDefaults(t *testing.T) {
	c := NewClient(Options{})
	if c.devicetermPath != "deviceterm" {
		t.Errorf("devicetermPath = %q, want %q", c.devicetermPath, "deviceterm")
	}
}

func TestNewClientCustomPath(t *testing.T) {
	c := NewClient(Options{Path: "/opt/bin/deviceterm"})
	if c.devicetermPath != "/opt/bin/deviceterm" {
		t.Errorf("devicetermPath = %q, want %q", c.devicetermPath, "/opt/bin/deviceterm")
	}
}

func TestParseSessionReport(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantID    string
		wantRole  string
		wantGrant bool
	}{
		{
			"granted automation tab",
			`{"automationGrant":true,"id":"550e8400-e29b-41d4-a716-446655440000","role":"automation"}` + "\n",
			"550e8400-e29b-41d4-a716-446655440000", "automation", true,
		},
		{
			"ungranted agent tab",
			`{"automationGrant":false,"id":"550e8400-e29b-41d4-a716-446655440000","role":"agent"}`,
			"550e8400-e29b-41d4-a716-446655440000", "agent", false,
		},
		{"out of tab omits id and role", `{"automationGrant":false}`, "", "", false},
		{"unknown keys ignored", `{"automationGrant":true,"id":"x","role":"automation","future":{"y":1}}`, "x", "automation", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := parseSessionReport([]byte(tt.input))
			if err != nil {
				t.Fatalf("parseSessionReport() error = %v", err)
			}
			if r.ID != tt.wantID || r.Role != tt.wantRole || r.AutomationGrant != tt.wantGrant {
				t.Errorf("got %+v, want id=%q role=%q grant=%v", r, tt.wantID, tt.wantRole, tt.wantGrant)
			}
		})
	}
	if _, err := parseSessionReport([]byte(`{not json`)); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestGrantState(t *testing.T) {
	tests := []struct {
		name   string
		report sessionReport
		err    error
		want   string
	}{
		{"granted", sessionReport{ID: "s1", Role: "automation", AutomationGrant: true}, nil, ""},
		{"ungranted in tab", sessionReport{ID: "s1", Role: "agent"}, nil, defaultAutomationTabReason},
		{"automation role without grant is still ungranted", sessionReport{ID: "s1", Role: "automation"}, nil, defaultAutomationTabReason},
		{"out of tab", sessionReport{}, nil, defaultUnrecognizedReason},
		{"daemon unreachable", sessionReport{}, &CLIError{Code: "transport.unavailable", Message: "no daemon"}, "DeviceTerm unreachable: transport.unavailable"},
		{"daemon timeout", sessionReport{}, &CLIError{Code: "transport.timeout"}, "DeviceTerm unreachable: transport.timeout"},
		{"pre-0.11.0 CLI has no session verb", sessionReport{}, &CLIError{Code: "cli.invalidUsage", Message: "unknown verb"}, tooOldReason},
		{"typed unauthorized maps to the Automation-tab reason", sessionReport{}, &CLIError{Code: "session.unauthorized"}, defaultAutomationTabReason},
		{"untyped error surfaces as is", sessionReport{}, errors.New("deviceterm session show failed: boom"), "deviceterm session show failed: boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewClient(Options{}).grantState(tt.report, tt.err); got != tt.want {
				t.Errorf("grantState() = %q, want %q", got, tt.want)
			}
		})
	}
}

// paneListMixed is a `pane list --all --json` payload: a split tab holding
// Claude and Codex, a tab whose terminal has no tty yet, a simulator, a
// device, and an unknown kind. Row context fields are present and ignored.
const paneListMixed = `[
  {"id":"p-claude","shortId":"a1b2c3","kind":"terminal","tabId":"t1","tabTitle":"✳ Claude Code","windowId":"w1",
   "current":false,"focused":true,"capabilities":["sendInput","captureText"],
   "terminal":{"sessionId":"p-claude","title":"✳ Claude Code","tty":"/dev/ttys004","cwd":"/Users/test/projects/atria"}},
  {"id":"p-codex","shortId":"d4e5f6","kind":"terminal","tabId":"t1","tabTitle":"✳ Claude Code","windowId":"w1",
   "current":false,"focused":false,"capabilities":["sendInput","captureText"],
   "terminal":{"sessionId":"p-codex","title":"codex","tty":"/dev/ttys005","cwd":"/Users/test/projects/atria"}},
  {"id":"p-fresh","kind":"terminal","tabId":"t2","tabTitle":"shell","windowId":"w1","name":"scratch",
   "terminal":{"sessionId":"p-fresh","title":"scratch"}},
  {"id":"p-sim","kind":"simulator","tabId":"t3","tabTitle":"iPhone 17","windowId":"w2","simulator":{"udid":"abc"}},
  {"id":"p-dev","kind":"device","tabId":"t3","tabTitle":"iPhone 17","windowId":"w2","device":{"udid":"def"}},
  {"id":"p-odd","kind":"hologram","tabId":"t3","tabTitle":"?","windowId":"w2"}
]`

func TestParsePaneList(t *testing.T) {
	panes, err := parsePaneList([]byte(paneListMixed))
	if err != nil {
		t.Fatalf("parsePaneList() error = %v", err)
	}
	if len(panes) != 6 {
		t.Fatalf("len = %d, want 6", len(panes))
	}
	if panes[0].Terminal == nil || panes[0].Terminal.Title != "✳ Claude Code" || panes[0].Terminal.TTY != "/dev/ttys004" {
		t.Errorf("panes[0].terminal = %+v", panes[0].Terminal)
	}
	if panes[2].Terminal == nil || panes[2].Terminal.TTY != "" || panes[2].Terminal.CWD != "" {
		t.Errorf("panes[2].terminal = %+v, want tty and cwd absent", panes[2].Terminal)
	}
	if panes[3].Terminal != nil || panes[4].Terminal != nil {
		t.Errorf("non-terminal panes should have no terminal object")
	}
	if panes[5].Kind != "hologram" {
		t.Errorf("unknown kind should decode as-is, got %q", panes[5].Kind)
	}
	if _, err := parsePaneList([]byte(`{"tab":{}}`)); err == nil {
		t.Error("expected error for wrong JSON shape")
	}
	if _, err := parsePaneList([]byte(`nope`)); err == nil {
		t.Error("expected error for invalid JSON")
	}
	empty, err := parsePaneList([]byte(`[]`))
	if err != nil || len(empty) != 0 {
		t.Errorf("empty array: got %v, %v", empty, err)
	}
}

func TestSessionsFromPanes(t *testing.T) {
	panes, err := parsePaneList([]byte(paneListMixed))
	if err != nil {
		t.Fatal(err)
	}
	sessions, cwd := sessionsFromPanes(panes)

	if len(sessions) != 3 {
		t.Fatalf("len(sessions) = %d, want 3 (terminal panes only): %+v", len(sessions), sessions)
	}
	// Two terminal panes sharing a tab retain independent titles.
	if sessions[0].ID != "p-claude" || sessions[0].Name != "✳ Claude Code" || sessions[0].TTY != "/dev/ttys004" {
		t.Errorf("sessions[0] = %+v", sessions[0])
	}
	if sessions[1].ID != "p-codex" || sessions[1].Name != "codex" || sessions[1].TTY != "/dev/ttys005" {
		t.Errorf("sessions[1] = %+v", sessions[1])
	}
	// A terminal whose shell has not attached yet has no tty; the composite
	// filters and dedups by TTY only when it is non-empty.
	if sessions[2].ID != "p-fresh" || sessions[2].Name != "scratch" || sessions[2].TTY != "" {
		t.Errorf("sessions[2] = %+v", sessions[2])
	}
	if cwd["p-claude"] != "/Users/test/projects/atria" {
		t.Errorf("cwd[p-claude] = %q", cwd["p-claude"])
	}
	if v, ok := cwd["p-fresh"]; !ok || v != "" {
		t.Errorf("cwd[p-fresh] = %q, %v; want present and empty", v, ok)
	}
	if _, ok := cwd["p-sim"]; ok {
		t.Errorf("simulator pane should not be in cwd map")
	}
}

func TestParseCapture(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			"plain",
			`{"pane":{"id":"p1","kind":"terminal"},"text":"❯ \n\n? for shortcuts"}`,
			"❯ \n\n? for shortcuts",
		},
		{
			"styled truecolor and indexed SGR round-trip",
			`{"pane":{"id":"p1"},"text":"\u001b[38;2;255;0;0m✻ Reading…\u001b[0m\n\u001b[38;5;33m❯\u001b[0m "}`,
			"\x1b[38;2;255;0;0m✻ Reading…\x1b[0m\n\x1b[38;5;33m❯\x1b[0m ",
		},
		{"empty text", `{"pane":{"id":"p1"},"text":""}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCapture([]byte(tt.input))
			if err != nil {
				t.Fatalf("parseCapture() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("text = %q, want %q", got, tt.want)
			}
		})
	}
	if _, err := parseCapture([]byte(`nope`)); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseMutationReceipt(t *testing.T) {
	id, err := parseMutationReceipt([]byte(`{"ok":true,"window":{"id":"w1"},"tab":{"id":"t9","title":"shell"},` +
		`"pane":{"id":"p9","kind":"terminal","tabId":"t9","terminal":{"sessionId":"p9","title":"shell"}}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "p9" {
		t.Errorf("id = %q, want p9", id)
	}

	for name, input := range map[string]string{
		"missing pane":      `{"ok":true,"tab":{"id":"t9"}}`,
		"empty pane id":     `{"ok":true,"pane":{"id":""}}`,
		"mutation failed":   `{"error":{"code":"intent.mutationFailed","message":"terminal failed","details":{"committed":{"tab":{"id":"t9"}}}}}`,
		"invalid json":      `{`,
		"empty stdout":      ``,
		"wrong shape array": `[]`,
	} {
		t.Run(name, func(t *testing.T) {
			id, err := parseMutationReceipt([]byte(input))
			if err == nil {
				t.Fatalf("expected error, got id %q", id)
			}
			if id != "" {
				t.Errorf("id = %q, want empty on error", id)
			}
		})
	}

	_, err = parseMutationReceipt([]byte(`{"error":{"code":"intent.mutationFailed","message":"x"}}`))
	var ce *CLIError
	if !errors.As(err, &ce) || ce.Code != "intent.mutationFailed" {
		t.Errorf("expected CLIError intent.mutationFailed, got %v", err)
	}
}

func TestParseErrorEnvelope(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantCode string
		wantMsg  string
		wantOK   bool
	}{
		{"typed", `{"error":{"code":"session.unauthorized","message":"automation grant required","details":{"rpcCode":-32011}}}` + "\n",
			"session.unauthorized", "automation grant required", true},
		{"code only", `{"error":{"code":"pane.notFound"}}`, "pane.notFound", "", true},
		{"no code", `{"error":{"message":"x"}}`, "", "", false},
		{"not an envelope", `{"pane":{"id":"p1"},"text":""}`, "", "", false},
		{"empty", ``, "", "", false},
		{"garbage", `deviceterm: boom`, "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, msg, ok := parseErrorEnvelope([]byte(tt.input))
			if ok != tt.wantOK || code != tt.wantCode || msg != tt.wantMsg {
				t.Errorf("got (%q, %q, %v), want (%q, %q, %v)", code, msg, ok, tt.wantCode, tt.wantMsg, tt.wantOK)
			}
		})
	}
}

func TestCLIErrorString(t *testing.T) {
	e := &CLIError{Code: "pane.notFound", Message: "no such pane"}
	if !strings.Contains(e.Error(), "pane.notFound") || !strings.Contains(e.Error(), "no such pane") {
		t.Errorf("Error() = %q", e.Error())
	}
	if got := (&CLIError{Code: "x"}).Error(); got != "deviceterm: x" {
		t.Errorf("Error() = %q", got)
	}
}

func TestIsUngranted(t *testing.T) {
	tests := map[string]bool{
		"session.unauthorized":       true,
		"intent.automationRequired":  true,
		"pane.notFound":              false,
		"transport.unavailable":      false,
		"":                           false,
		"session.unauthorized.extra": false,
	}
	for code, want := range tests {
		if got := isUngranted(code); got != want {
			t.Errorf("isUngranted(%q) = %v, want %v", code, got, want)
		}
	}
}

// fakeRun scripts CLI responses by the joined argument string and records
// every call. Unscripted calls fail the test.
type fakeRun struct {
	t         *testing.T
	responses map[string]func() ([]byte, error)
	calls     []string
}

func (f *fakeRun) run(args ...string) ([]byte, error) {
	key := strings.Join(args, " ")
	f.calls = append(f.calls, key)
	resp, ok := f.responses[key]
	if !ok {
		f.t.Fatalf("unexpected deviceterm call: %s", key)
	}
	return resp()
}

func ok(body string) func() ([]byte, error) {
	return func() ([]byte, error) { return []byte(body), nil }
}

func typed(code string) func() ([]byte, error) {
	return func() ([]byte, error) { return nil, &CLIError{Code: code, Message: code} }
}

func untyped(msg string) func() ([]byte, error) {
	return func() ([]byte, error) { return nil, errors.New(msg) }
}

const (
	sessionKey  = "session show --json"
	listKey     = "pane list --all --json"
	granted     = `{"automationGrant":true,"id":"self","role":"automation"}`
	ungranted   = `{"automationGrant":false,"id":"self","role":"agent"}`
	outOfTab    = `{"automationGrant":false}`
	twoTerminal = `[{"id":"p1","kind":"terminal","tabId":"t1","tabTitle":"one","windowId":"w1","terminal":{"sessionId":"p1","title":"✳ one","tty":"/dev/ttys001","cwd":"/w/one"}},
		{"id":"p2","kind":"terminal","tabId":"t2","tabTitle":"two","windowId":"w1","terminal":{"sessionId":"p2","title":"✳ two","tty":"/dev/ttys002","cwd":"/w/two"}}]`
)

func newFakeClient(t *testing.T, responses map[string]func() ([]byte, error)) (*Client, *fakeRun) {
	f := &fakeRun{t: t, responses: responses}
	c := NewClient(Options{})
	c.selfSession = "self"
	c.runFn = f.run
	return c, f
}

func TestListSessionsHappyPath(t *testing.T) {
	c, f := newFakeClient(t, map[string]func() ([]byte, error){
		sessionKey: ok(granted),
		listKey:    ok(twoTerminal),
	})
	sessions, err := c.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 2 || sessions[0].ID != "p1" || sessions[1].ID != "p2" {
		t.Errorf("sessions = %+v", sessions)
	}
	if sessions[0].Name != "✳ one" || sessions[0].TTY != "/dev/ttys001" {
		t.Errorf("sessions[0] = %+v", sessions[0])
	}
	if len(f.calls) != 2 || f.calls[0] != sessionKey || f.calls[1] != listKey {
		t.Errorf("calls = %v, want the grant check then one pane list", f.calls)
	}
	if got, _ := c.GetVar("p2", "path"); got != "/w/two" {
		t.Errorf("GetVar(p2) = %q", got)
	}
}

func TestListSessionsFailsWhenGrantLost(t *testing.T) {
	cases := []struct {
		name       string
		session    func() ([]byte, error)
		wantReason string
	}{
		{"grant revoked", ok(ungranted), defaultAutomationTabReason},
		{"ancestry broken", ok(outOfTab), defaultUnrecognizedReason},
		{"daemon unreachable", typed("transport.unavailable"), "DeviceTerm unreachable: transport.unavailable"},
		{"old CLI", typed("cli.invalidUsage"), tooOldReason},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			c, f := newFakeClient(t, map[string]func() ([]byte, error){
				sessionKey: tt.session,
			})
			c.cwd = map[string]string{"p1": "/kept"}

			sessions, err := c.ListSessions()
			if err == nil {
				t.Fatalf("expected error, got sessions %+v", sessions)
			}
			if err.Error() != tt.wantReason {
				t.Errorf("error = %q, want %q", err, tt.wantReason)
			}
			if len(f.calls) != 1 {
				t.Errorf("calls = %v, want only the grant check before failing", f.calls)
			}
			// A failed refresh must not clobber the cwd cache.
			if got, _ := c.GetVar("p1", "path"); got != "/kept" {
				t.Errorf("cwd cache = %q, want preserved", got)
			}
		})
	}
}

func TestListSessionsFailsOnPaneListError(t *testing.T) {
	cases := map[string]func() ([]byte, error){
		"typed error":    typed("transport.timeout"),
		"untyped error":  untyped("deviceterm pane list failed: boom"),
		"malformed json": ok(`[{"id":`),
	}
	for name, failure := range cases {
		t.Run(name, func(t *testing.T) {
			c, _ := newFakeClient(t, map[string]func() ([]byte, error){
				sessionKey: ok(granted),
				listKey:    failure,
			})
			c.cwd = map[string]string{"p1": "/kept"}

			sessions, err := c.ListSessions()
			if err == nil {
				t.Fatalf("expected error, got sessions %+v", sessions)
			}
			if sessions != nil {
				t.Errorf("sessions = %+v, want nil on failure", sessions)
			}
			if got, _ := c.GetVar("p1", "path"); got != "/kept" {
				t.Errorf("cwd cache = %q, want preserved", got)
			}
		})
	}
}

func TestListSessionsSkipsGrantCheckWithoutSelfSession(t *testing.T) {
	c, f := newFakeClient(t, map[string]func() ([]byte, error){
		listKey: ok(`[]`),
	})
	c.selfSession = ""
	if _, err := c.ListSessions(); err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(f.calls) != 1 || f.calls[0] != listKey {
		t.Errorf("calls = %v, want only pane list", f.calls)
	}
}

func TestCheckGrantClassifiesStates(t *testing.T) {
	cases := []struct {
		name    string
		session func() ([]byte, error)
		want    string // "" means granted
	}{
		{"granted", ok(granted), ""},
		{"ungranted", ok(ungranted), defaultAutomationTabReason},
		{"out of tab", ok(outOfTab), defaultUnrecognizedReason},
		{"unreachable", typed("transport.unavailable"), "DeviceTerm unreachable: transport.unavailable"},
		{"malformed report", ok(`{"automationGrant":`), "parse deviceterm session show: unexpected end of JSON input"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newFakeClient(t, map[string]func() ([]byte, error){sessionKey: tt.session})
			err := c.checkGrant()
			switch {
			case tt.want == "" && err != nil:
				t.Errorf("checkGrant() = %v, want nil", err)
			case tt.want != "" && (err == nil || err.Error() != tt.want):
				t.Errorf("checkGrant() = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestSendTextUsesRaw(t *testing.T) {
	text := `C:\path\n with "quotes" and --json-looking text` + "\r"
	c, f := newFakeClient(t, map[string]func() ([]byte, error){
		"pane send-input --json --raw p1 -- " + text: ok(`{"ok":true,"pane":{"id":"p1"},"bytes":48}`),
	})
	if err := c.SendText("p1", text); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	want := []string{"pane", "send-input", "--json", "--raw", "p1", "--", text}
	if got := f.calls[0]; got != strings.Join(want, " ") {
		t.Errorf("argv = %q, want %q", got, strings.Join(want, " "))
	}
}

func TestRunCommandSendsTextThenEnter(t *testing.T) {
	c, f := newFakeClient(t, map[string]func() ([]byte, error){
		"pane send-input --json --raw p1 -- make test": ok(`{"ok":true}`),
		"pane send-input --json --raw p1 -- \r":        ok(`{"ok":true}`),
	})
	if err := c.RunCommand("p1", "make test"); err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	if len(f.calls) != 2 || !strings.HasSuffix(f.calls[1], "-- \r") {
		t.Errorf("calls = %q, want the command then a bare CR", f.calls)
	}
}

func TestVerb(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"pane", "list", "--all", "--json"}, "pane list"},
		{[]string{"pane", "capture-text", "p1", "--json"}, "pane capture-text"},
		{[]string{"session", "show", "--json"}, "session show"},
		{[]string{"pane", "send-input", "--json", "--raw", "p1", "--", "x"}, "pane send-input"},
		{nil, ""},
	}
	for _, tt := range tests {
		if got := verb(tt.args); got != tt.want {
			t.Errorf("verb(%v) = %q, want %q", tt.args, got, tt.want)
		}
	}
}

func TestGetVarPath(t *testing.T) {
	c := NewClient(Options{})
	c.cwd = map[string]string{"p1": "/Users/test/projects/atria", "p2": ""}

	got, err := c.GetVar("p1", "path")
	if err != nil || got != "/Users/test/projects/atria" {
		t.Errorf("GetVar(p1) = %q, %v", got, err)
	}
	// Known session with unresolved cwd and unknown session both yield
	// empty without error so CWD discovery falls through.
	if got, err := c.GetVar("p2", "path"); err != nil || got != "" {
		t.Errorf("GetVar(p2) = %q, %v; want empty, nil", got, err)
	}
	if got, err := c.GetVar("nope", "path"); err != nil || got != "" {
		t.Errorf("GetVar(nope) = %q, %v; want empty, nil", got, err)
	}
}

func TestGetVarUnsupported(t *testing.T) {
	c := NewClient(Options{})
	if _, err := c.GetVar("p1", "title"); err == nil {
		t.Error("expected error for unsupported variable")
	}
}

func TestMonitorOutputUnsupported(t *testing.T) {
	c := NewClient(Options{})
	pid, err := c.MonitorOutput("p1", "/tmp/log", "pattern")
	if err == nil {
		t.Error("expected error for unsupported MonitorOutput")
	}
	if pid != 0 {
		t.Errorf("pid = %d, want 0", pid)
	}
}

var (
	defaultAutomationTabReason = NewClient(Options{}).automationTabReason()
	defaultUnrecognizedReason  = NewClient(Options{}).unrecognizedReason()
)

func TestReasonsNameTheProgram(t *testing.T) {
	c := NewClient(Options{ProgramName: "atria"})
	if got := c.automationTabReason(); got != "open an Automation tab (Shell ▸ Open Automation Tab, ⇧⌘T) and run atria there" {
		t.Fatalf("automationTabReason = %q", got)
	}
	if got := c.unrecognizedReason(); !strings.Contains(got, "run atria directly") {
		t.Fatalf("unrecognizedReason = %q", got)
	}
	if got := NewClient(Options{}).automationTabReason(); !strings.Contains(got, "run this program there") {
		t.Fatalf("default reason = %q", got)
	}
}

func TestTransportErrorsAreUnavailable(t *testing.T) {
	c, _ := newFakeClient(t, map[string]func() ([]byte, error){
		sessionKey: typed("transport.unavailable"),
	})
	_, err := c.ListSessions()
	if !errors.Is(err, terminal.ErrUnavailable) {
		t.Fatalf("transport error = %v, want ErrUnavailable", err)
	}
	var ce *CLIError
	if !errors.As(err, &ce) || ce.Code != "transport.unavailable" {
		t.Fatalf("typed error must still be recoverable, got %v", err)
	}

	// A grant problem is not unavailability.
	c, _ = newFakeClient(t, map[string]func() ([]byte, error){
		sessionKey: typed("session.unauthorized"),
	})
	if _, err := c.ListSessions(); err == nil || errors.Is(err, terminal.ErrUnavailable) {
		t.Fatalf("unauthorized = %v, want a plain grant error", err)
	}
}

func TestRunTimesOutAsUnavailable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deviceterm")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nsleep 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := NewClient(Options{Path: path, CommandTimeout: time.Second})
	start := time.Now()
	_, err := c.ListSessions() // no selfSession: goes straight to pane list
	if !errors.Is(err, terminal.ErrUnavailable) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout = %v, want ErrUnavailable and DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("timeout took %s; the pipe wait was not bounded", elapsed)
	}
}
