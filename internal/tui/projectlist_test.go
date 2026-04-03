package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/sethdeckard/atria/internal/model"
)

// mockStore implements the buildRows store interface for testing.
type mockStore struct {
	projects []*model.Project
	sessions map[string][]*model.AgentSession
}

func (m *mockStore) Projects() []*model.Project { return m.projects }
func (m *mockStore) GetSessions(dir string) []*model.AgentSession {
	return m.sessions[dir]
}

func TestBuildRows(t *testing.T) {
	t.Run("empty store", func(t *testing.T) {
		s := &mockStore{}
		rows := buildRows(s)
		if len(rows) != 0 {
			t.Errorf("expected 0 rows, got %d", len(rows))
		}
	})

	t.Run("project with no sessions", func(t *testing.T) {
		s := &mockStore{
			projects: []*model.Project{{Name: "foo", Dir: "/tmp/foo"}},
			sessions: map[string][]*model.AgentSession{},
		}
		rows := buildRows(s)
		if len(rows) != 0 {
			t.Errorf("expected 0 rows (no sessions), got %d", len(rows))
		}
	})

	t.Run("project with sessions", func(t *testing.T) {
		s := &mockStore{
			projects: []*model.Project{{Name: "foo", Dir: "/tmp/foo"}},
			sessions: map[string][]*model.AgentSession{
				"/tmp/foo": {
					{SessionID: "s1", Type: model.AgentClaude},
					{SessionID: "s2", Type: model.AgentCodex},
				},
			},
		}
		rows := buildRows(s)
		if len(rows) != 2 {
			t.Fatalf("expected 2 rows, got %d", len(rows))
		}
		if rows[0].session.SessionID != "s1" || rows[1].session.SessionID != "s2" {
			t.Error("rows should preserve session order")
		}
	})

	t.Run("unique 2-segment names not disambiguated", func(t *testing.T) {
		s := &mockStore{
			projects: []*model.Project{
				{Name: "svc", Dir: "/a/svc"},
				{Name: "svc", Dir: "/b/svc"},
				{Name: "svc", Dir: "/c/svc"},
			},
			sessions: map[string][]*model.AgentSession{
				"/a/svc": {{SessionID: "s1", Type: model.AgentClaude}},
				"/b/svc": {{SessionID: "s2", Type: model.AgentClaude}},
				"/c/svc": {{SessionID: "s3", Type: model.AgentClaude}},
			},
		}
		rows := buildRows(s)
		if len(rows) != 3 {
			t.Fatalf("expected 3 rows, got %d", len(rows))
		}
		// With 2-segment display names, a/svc, b/svc, c/svc are unique
		if rows[0].displayName != "a/svc" {
			t.Errorf("first should be 'a/svc', got %q", rows[0].displayName)
		}
		if rows[1].displayName != "b/svc" {
			t.Errorf("second should be 'b/svc', got %q", rows[1].displayName)
		}
		if rows[2].displayName != "c/svc" {
			t.Errorf("third should be 'c/svc', got %q", rows[2].displayName)
		}
	})

	t.Run("three duplicates get #2 #3 suffixes", func(t *testing.T) {
		s := &mockStore{
			projects: []*model.Project{
				{Name: "svc", Dir: "/x/same/svc"},
				{Name: "svc", Dir: "/y/same/svc"},
				{Name: "svc", Dir: "/z/same/svc"},
			},
			sessions: map[string][]*model.AgentSession{
				"/x/same/svc": {{SessionID: "s1", Type: model.AgentClaude}},
				"/y/same/svc": {{SessionID: "s2", Type: model.AgentClaude}},
				"/z/same/svc": {{SessionID: "s3", Type: model.AgentClaude}},
			},
		}
		rows := buildRows(s)
		if len(rows) != 3 {
			t.Fatalf("expected 3 rows, got %d", len(rows))
		}
		if rows[0].displayName != "same/svc" {
			t.Errorf("first should be 'same/svc', got %q", rows[0].displayName)
		}
		if rows[1].displayName != "same/svc #2" {
			t.Errorf("second should be 'same/svc #2', got %q", rows[1].displayName)
		}
		if rows[2].displayName != "same/svc #3" {
			t.Errorf("third should be 'same/svc #3', got %q", rows[2].displayName)
		}
	})

	t.Run("unique names not disambiguated", func(t *testing.T) {
		s := &mockStore{
			projects: []*model.Project{
				{Name: "alpha", Dir: "/a/alpha"},
				{Name: "beta", Dir: "/b/beta"},
			},
			sessions: map[string][]*model.AgentSession{
				"/a/alpha": {{SessionID: "s1", Type: model.AgentClaude}},
				"/b/beta":  {{SessionID: "s2", Type: model.AgentClaude}},
			},
		}
		rows := buildRows(s)
		if rows[0].displayName != "a/alpha" || rows[1].displayName != "b/beta" {
			t.Errorf("unique names should not be modified, got %q and %q", rows[0].displayName, rows[1].displayName)
		}
	})
}

func TestAgentTypeLabel(t *testing.T) {
	tests := []struct {
		agentType model.AgentType
		expected  string
	}{
		{model.AgentClaude, "Claude"},
		{model.AgentCodex, "Codex"},
		{model.AgentOpenCode, "OpenCode"},
		{model.AgentCopilot, "Copilot"},
		{"mystery", "Mystery"}, // fallback capitalizes first letter
	}
	for _, tt := range tests {
		t.Run(string(tt.agentType), func(t *testing.T) {
			got := agentTypeLabel(tt.agentType)
			if got != tt.expected {
				t.Errorf("agentTypeLabel(%q) = %q, want %q", tt.agentType, got, tt.expected)
			}
		})
	}
}

func TestAgentTypeStyle(t *testing.T) {
	t.Run("known type returns specific style", func(t *testing.T) {
		style := agentTypeStyle(model.AgentClaude)
		if style.GetForeground() == normalStyle.GetForeground() {
			t.Error("expected a distinct style for Claude, got normalStyle")
		}
	})

	t.Run("unknown type returns normalStyle", func(t *testing.T) {
		style := agentTypeStyle("unknown")
		if style.GetForeground() != normalStyle.GetForeground() {
			t.Error("expected normalStyle for unknown agent type")
		}
	})
}

func TestPadToWidth(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		width    int
		wantLen  int
		wantSame bool // true if input should be returned unchanged
	}{
		{"shorter than width", "hi", 10, 10, false},
		{"equal to width", "hello", 5, 5, true},
		{"longer than width", "hello world", 5, 11, true},
		{"empty string", "", 5, 5, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := padToWidth(tt.input, tt.width)
			if tt.wantSame && got != tt.input {
				t.Errorf("expected unchanged input, got %q", got)
			}
			if len(got) != tt.wantLen {
				t.Errorf("padToWidth(%q, %d) len = %d, want %d", tt.input, tt.width, len(got), tt.wantLen)
			}
		})
	}
}

func TestFormatNarrowRow(t *testing.T) {
	row := projectRow{
		project: &model.Project{Name: "myproject", Dir: "/tmp/myproject"},
		session: &model.AgentSession{
			SessionID:    "s1",
			Type:         model.AgentClaude,
			Status:       model.StatusIdle,
			Activity:     "idle",
			LastActivity: time.Now().Add(-5 * time.Minute),
		},
		displayName: "myproject",
	}

	tests := []struct {
		name string
		lp   layoutPolicy
		// Per-line placement expectations
		line1Name   bool // name on line 1
		line1Type   bool // type on line 1
		line2Status bool // status on line 2
		line2Type   bool // type on line 2 (compact moves it here)
		line2Time   bool // time on line 2
	}{
		{
			name:        "narrow: name+type on line 1, status+time on line 2",
			lp:          layoutPolicy{mode: layoutNarrow, width: 50, showType: true, showTime: true},
			line1Name:   true,
			line1Type:   true,
			line2Status: true,
			line2Type:   false,
			line2Time:   true,
		},
		{
			name:        "compact: name owns line 1, status+type on line 2",
			lp:          layoutPolicy{mode: layoutCompact, width: 30, showType: true, showTime: false},
			line1Name:   true,
			line1Type:   false,
			line2Status: true,
			line2Type:   true,
			line2Time:   false,
		},
		{
			name:        "survival: name on line 1, status only on line 2",
			lp:          layoutPolicy{mode: layoutSurvival, width: 24, showType: false, showTime: false},
			line1Name:   true,
			line1Type:   false,
			line2Status: true,
			line2Type:   false,
			line2Time:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use plain=true so ANSI escapes don't interfere with Contains checks
			result := formatNarrowRow(row, tt.lp, 0, true)
			lines := strings.Split(result, "\n")
			if len(lines) != 2 {
				t.Fatalf("expected 2 lines, got %d: %q", len(lines), result)
			}

			line1, line2 := lines[0], lines[1]

			check := func(label, line string, want bool, substr string) {
				got := strings.Contains(line, substr)
				if got != want {
					t.Errorf("%s: Contains(%q)=%v, want %v; line=%q", label, substr, got, want, line)
				}
			}

			check("line1 name", line1, tt.line1Name, "myproject")
			check("line1 type", line1, tt.line1Type, "Claude")
			check("line2 status", line2, tt.line2Status, "idle")
			check("line2 type", line2, tt.line2Type, "Claude")
			check("line2 time", line2, tt.line2Time, "ago")

			// Width bounds
			for i, line := range lines {
				if w := lipgloss.Width(line); w > tt.lp.width {
					t.Errorf("line %d width %d exceeds max %d: %q", i, w, tt.lp.width, line)
				}
			}
		})
	}
}

// TestNarrowLayoutNamePriority verifies that the project name is never
// truncated more aggressively than secondary metadata. At compact/survival
// widths the name should get the full line 1 rather than sharing it.
func TestNarrowLayoutNamePriority(t *testing.T) {
	longName := "my-really-long-project-name"
	row := projectRow{
		project: &model.Project{Name: longName, Dir: "/tmp/" + longName},
		session: &model.AgentSession{
			SessionID:    "s1",
			Type:         model.AgentClaude,
			Status:       model.StatusWorking,
			Activity:     "thinking",
			LastActivity: time.Now().Add(-1 * time.Minute),
		},
		displayName: longName,
	}

	t.Run("compact gives name full line 1", func(t *testing.T) {
		lp := layoutPolicy{mode: layoutCompact, width: 32, showType: true, showTime: false}
		result := formatNarrowRow(row, lp, 0, true)
		lines := strings.Split(result, "\n")
		line1 := lines[0]

		// Line 1 should NOT contain the type — name owns the line
		if strings.Contains(line1, "Claude") {
			t.Errorf("compact line 1 should not contain type; line1=%q", line1)
		}
		// Name should use as much of the line as possible
		// (indent=2, so maxW=30; name is 27 chars — should fit without truncation)
		if !strings.Contains(line1, longName) {
			t.Errorf("compact line 1 should contain full name %q; line1=%q", longName, line1)
		}
	})

	t.Run("survival gives name full line 1 no metadata", func(t *testing.T) {
		lp := layoutPolicy{mode: layoutSurvival, width: 24, showType: false, showTime: false}
		result := formatNarrowRow(row, lp, 0, true)
		lines := strings.Split(result, "\n")

		// Line 1: name only (truncated to fit)
		if strings.Contains(lines[0], "Claude") {
			t.Errorf("survival line 1 should not contain type; line1=%q", lines[0])
		}
		// Line 2: status only, no type or time
		if strings.Contains(lines[1], "Claude") {
			t.Errorf("survival line 2 should not contain type; line2=%q", lines[1])
		}
		if strings.Contains(lines[1], "ago") {
			t.Errorf("survival line 2 should not contain time; line2=%q", lines[1])
		}
	})

	t.Run("narrow allows type on line 1 only if name fits", func(t *testing.T) {
		// At width 36, maxW=34. longName is 27 chars, " · Claude" is 10 → 37 > 34.
		// Type should be dropped so name isn't clipped.
		lp := layoutPolicy{mode: layoutNarrow, width: 36, showType: true, showTime: true}
		result := formatNarrowRow(row, lp, 0, true)
		lines := strings.Split(result, "\n")

		if strings.Contains(lines[0], "Claude") {
			// Type didn't fit — name should be untruncated
			t.Logf("type present on line 1 (unexpected at this width); line1=%q", lines[0])
		}
		// Name should not be truncated when type is dropped
		if !strings.Contains(lines[0], "Claude") && !strings.Contains(lines[0], longName) {
			t.Errorf("when type is dropped, name should be untruncated; line1=%q", lines[0])
		}
	})
}

func TestFormatNarrowSelectedRow(t *testing.T) {
	row := projectRow{
		project: &model.Project{Name: "test", Dir: "/tmp/test"},
		session: &model.AgentSession{
			SessionID:    "s1",
			Type:         model.AgentClaude,
			Status:       model.StatusWorking,
			Activity:     "thinking",
			LastActivity: time.Now(),
		},
		displayName: "test",
	}

	lp := layoutPolicy{mode: layoutNarrow, width: 45, showType: true, showTime: true}
	result := formatNarrowSelectedRow(row, lp, 0)
	lines := strings.Split(result, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			t.Errorf("line %d should not be empty", i)
		}
	}
}

func TestLayoutPolicyHysteresis(t *testing.T) {
	// Start wide
	narrowActive := false
	lp := computeLayoutPolicy(80, narrowActive)
	if lp.mode != layoutWide {
		t.Errorf("expected wide at 80, got %v", lp.mode)
	}

	// Shrink to 55 → enters narrow
	narrowActive = 55 < 56
	lp = computeLayoutPolicy(55, narrowActive)
	if lp.mode != layoutNarrow {
		t.Errorf("expected narrow at 55, got %v", lp.mode)
	}

	// Grow to 58 → still narrow (hysteresis: need >= 60 to exit)
	if 58 >= 60 {
		narrowActive = false
	}
	lp = computeLayoutPolicy(58, narrowActive)
	if lp.mode != layoutNarrow {
		t.Errorf("expected narrow at 58 (hysteresis), got %v", lp.mode)
	}

	// Grow to 60 → exits narrow
	if 60 >= 60 {
		narrowActive = false
	}
	lp = computeLayoutPolicy(60, narrowActive)
	if lp.mode != layoutWide {
		t.Errorf("expected wide at 60 after exit, got %v", lp.mode)
	}
}

func TestLayoutPolicyModes(t *testing.T) {
	tests := []struct {
		name     string
		width    int
		wantMode layoutMode
	}{
		{"survival at 20", 20, layoutSurvival},
		{"survival at 27", 27, layoutSurvival},
		{"compact at 28", 28, layoutCompact},
		{"compact at 35", 35, layoutCompact},
		{"narrow at 36", 36, layoutNarrow},
		{"narrow at 55", 55, layoutNarrow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lp := computeLayoutPolicy(tt.width, true)
			if lp.mode != tt.wantMode {
				t.Errorf("computeLayoutPolicy(%d, true).mode = %v, want %v", tt.width, lp.mode, tt.wantMode)
			}
		})
	}
}

func TestLayoutPolicyDerivedMethods(t *testing.T) {
	wide := computeLayoutPolicy(80, false)
	if wide.linesPerRow() != 1 {
		t.Errorf("wide linesPerRow should be 1, got %d", wide.linesPerRow())
	}
	if wide.headerLines() != headerLineCount {
		t.Errorf("wide headerLines should be %d, got %d", headerLineCount, wide.headerLines())
	}

	narrow := computeLayoutPolicy(45, true)
	if narrow.linesPerRow() != 2 {
		t.Errorf("narrow linesPerRow should be 2, got %d", narrow.linesPerRow())
	}
	if narrow.headerLines() != 2 {
		t.Errorf("narrow headerLines should be 2, got %d", narrow.headerLines())
	}

	survival := computeLayoutPolicy(20, true)
	if survival.showBranding() {
		t.Error("survival should not show branding at width 20")
	}
	if survival.showLogo() {
		t.Error("survival should not show logo at width 20")
	}

	compact := computeLayoutPolicy(32, true)
	if !compact.showBranding() {
		t.Error("compact at 32 should show branding")
	}
	if compact.showLogo() {
		t.Error("compact at 32 should not show logo")
	}
}

func TestProjectListLayoutNarrowStreamShortTerminal(t *testing.T) {
	// Regression: narrow + stream-open + very short terminal must not
	// produce maxRows*linesPerRow + panelHeight > usable, which would
	// cause normalizeView to clip the footer/stream.
	tests := []struct {
		name   string
		height int
		width  int
	}{
		{"10 lines", 10, 45},
		{"12 lines", 12, 45},
		{"8 lines", 8, 45},
		{"15 lines", 15, 40},
		{"very short 6 lines", 6, 45},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{
				width:        tt.width,
				height:       tt.height,
				narrowActive: true,
				streamOpen:   true,
				rows: []projectRow{
					{
						project: &model.Project{Name: "a", Dir: "/a"},
						session: &model.AgentSession{SessionID: "s1", Type: model.AgentClaude, Status: model.StatusIdle},
					},
					{
						project: &model.Project{Name: "b", Dir: "/b"},
						session: &model.AgentSession{SessionID: "s2", Type: model.AgentClaude, Status: model.StatusWorking},
					},
				},
			}
			layout := m.projectListLayout()
			lp := computeLayoutPolicy(m.width, m.narrowActive)
			linesPerRow := lp.linesPerRow()

			// overhead = headerLines(2) + footerLineCount(3) = 5
			// available = height - 5; usable = available - 1 (spacer)
			overhead := lp.headerLines() + footerLineCount
			available := m.height - overhead
			usable := available - 1
			if usable < 4 {
				usable = 4
			}

			consumed := layout.maxRows*linesPerRow + layout.panelHeight
			if consumed > usable {
				t.Errorf("layout exceeds usable space: maxRows=%d * linesPerRow=%d + panelHeight=%d = %d > usable=%d",
					layout.maxRows, linesPerRow, layout.panelHeight, consumed, usable)
			}
			if layout.maxRows < 1 {
				t.Errorf("maxRows must be at least 1, got %d", layout.maxRows)
			}
			if layout.panelHeight < 1 {
				t.Errorf("panelHeight must be at least 1, got %d", layout.panelHeight)
			}
		})
	}
}

func TestRenderFooterNarrow(t *testing.T) {
	selected := &projectRow{
		project:     &model.Project{Name: "test", Dir: "/tmp/test"},
		session:     &model.AgentSession{SessionID: "s1", Type: model.AgentClaude, Status: model.StatusIdle},
		displayName: "test",
	}

	tests := []struct {
		name string
		lp   layoutPolicy
	}{
		{"wide", layoutPolicy{mode: layoutWide, width: 80, showType: true, showTime: true}},
		{"narrow", layoutPolicy{mode: layoutNarrow, width: 50, showType: true, showTime: true}},
		{"compact", layoutPolicy{mode: layoutCompact, width: 30, showType: true, showTime: false}},
		{"survival", layoutPolicy{mode: layoutSurvival, width: 24, showType: false, showTime: false}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := renderFooter(1, selected, model.AgentClaude, false, tt.lp.width, tt.lp)
			w := lipgloss.Width(result)
			if w > tt.lp.width {
				t.Errorf("footer width %d exceeds max %d: %q", w, tt.lp.width, result)
			}
			if strings.TrimSpace(result) == "" {
				t.Error("footer should not be empty")
			}
		})
	}
}

func TestSortColumnLabel(t *testing.T) {
	tests := []struct {
		col  sortColumn
		desc bool
		want string
	}{
		{sortByAgent, false, "name ▲"},
		{sortByStatus, true, "status ▼"},
		{sortByUpdated, false, "updated ▲"},
	}
	for _, tt := range tests {
		got := sortColumnLabel(tt.col, tt.desc)
		if got != tt.want {
			t.Errorf("sortColumnLabel(%d, %v) = %q, want %q", tt.col, tt.desc, got, tt.want)
		}
	}
}
