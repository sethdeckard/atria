package agent

import (
	"strings"
	"testing"
)

func TestClassifyOutput(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		agentType Type
		expected  Status
	}{
		// Claude patterns
		{"claude allow prompt", "Allow this action?", Claude, StatusNeedsInput},
		{"claude allow edit", "Allow file edit?", Claude, StatusNeedsInput},
		{"claude proceed prompt", "Do you want to proceed?", Claude, StatusNeedsInput},
		{"claude esc to cancel", "Esc to cancel · Tab to amend", Claude, StatusNeedsInput},
		{"claude plan mode prompt", "Would you like to proceed?", Claude, StatusNeedsInput},
		{"claude working spinner", "✻ Reading…", Claude, StatusWorking},
		{"claude thinking", "✶ Doodling… (thought for 6s)", Claude, StatusWorking},
		{"claude dot spinner", "· Doodling… (48s)", Claude, StatusWorking},
		{"claude background task not working", "⏵⏵ accept edits on · tail -f log (running) · esc to interrupt", Claude, ""},
		{"claude shell background task working", "shell · ⏵⏵ accept edits on · esc to interrupt · ↓ to manage", Claude, StatusWorking},
		{"claude idle prompt", "❯ ", Claude, StatusIdle},
		{"claude idle prompt with path", "~/projects ❯", Claude, StatusIdle},
		{"claude shortcuts", "? for shortcuts", Claude, StatusIdle},
		{"claude done static", "✻", Claude, ""},

		// Codex patterns
		{"codex working bullet", "• Working (30s • esc to interrupt)", Codex, StatusWorking},
		{"codex working simple", "• Working", Codex, StatusWorking},
		{"codex waiting for input", "Waiting for user input", Codex, StatusNeedsInput},
		{"codex run command prompt", "Would you like to run the following command?", Codex, StatusNeedsInput},
		{"codex confirm prompt", "Press enter to confirm or esc to cancel", Codex, StatusNeedsInput},
		{"codex question banner", "Question 1/1 (1 unanswered)", Codex, StatusNeedsInput},
		{"codex none of the above option", "4. None of the above", Codex, StatusNeedsInput},
		{"codex prompt", "› Write tests for @filename", Codex, StatusIdle},
		{"codex status bar idle", "gpt-5.3-codex default · 73% left · ~/projects/foo", Codex, StatusIdle},

		// OpenCode patterns
		{"opencode permission required", "△ Permission required", OpenCode, StatusNeedsInput},
		{"opencode allow once button", "Allow once   Allow always   Reject", OpenCode, StatusNeedsInput},
		{"opencode working", "■ ..... esc interrupt", OpenCode, StatusWorking},
		{"opencode idle footer", "ctrl+t variants  tab agents  ctrl+p commands", OpenCode, StatusIdle},

		// Shared patterns (work for any agent type)
		{"shared bell character", "\x07", Claude, StatusNeedsInput},
		{"shared bell with text", "prompt\x07here", Codex, StatusNeedsInput},
		{"shared error message", "Error: file not found", Claude, StatusError},
		{"shared error with context", "compilation Error: syntax", OpenCode, StatusError},
		{"shared completed check", "✓ All tests passed", Claude, StatusIdle},
		{"shared completed text", "Task completed successfully", Codex, StatusIdle},
		{"shared completed metric not idle", "checkpoint_loaded completed=1 path=/tmp/checkpoint.json", Claude, ""},
		{"shared no findings", "No findings reported", OpenCode, StatusIdle},
		{"shared shell prompt", "user@host $ ", Claude, StatusIdle},

		// Cross-agent isolation: agent-specific patterns must NOT match other agents
		{"claude prompt not codex", "❯ ", Codex, ""},
		{"claude prompt not opencode", "❯ ", OpenCode, ""},
		{"claude shortcuts not codex", "? for shortcuts", Codex, ""},
		{"codex prompt not claude", "› Write tests", Claude, ""},
		{"codex prompt not opencode", "› Write tests", OpenCode, ""},
		{"codex working not claude", "• Working", Claude, ""},
		{"codex working not opencode", "• Working", OpenCode, ""},
		{"opencode idle not claude", "ctrl+p commands", Claude, ""},
		{"opencode idle not codex", "ctrl+p commands", Codex, ""},
		{"claude proceed not codex", "Do you want to proceed?", Codex, ""},
		{"opencode permission not claude", "Permission required", Claude, ""},

		// Unknown agent type: only shared patterns match
		{"unknown bell", "\x07", "unknown", StatusNeedsInput},
		{"unknown error", "Error: oops", "unknown", StatusError},
		{"unknown completed", "✓ done", "unknown", StatusIdle},
		{"unknown shell prompt", "user@host $ ", "unknown", StatusIdle},
		{"unknown claude spinner", "✻ Reading…", "unknown", ""},
		{"unknown codex working", "• Working", "unknown", ""},
		{"unknown no match", "Hello world", "unknown", ""},

		// Misc
		{"generic question no match", "Do you want to continue?", Claude, ""},
		{"bare continue no match", "Press Continue to proceed", Claude, ""},
		{"generic working output", "Compiling main.go...", Claude, ""},
		{"empty string", "", Claude, ""},
		{"random text", "Hello world", Claude, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyOutput(tt.input, tt.agentType)
			if got != tt.expected {
				t.Errorf("ClassifyOutput(%q, %q) = %q, want %q", tt.input, tt.agentType, got, tt.expected)
			}
		})
	}
}

func TestBottomRegion(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  int
	}{
		{"all blank", []string{"", "", ""}, 0},
		{"fewer than 8 lines", []string{"a", "b", "c"}, 0},
		{"exactly 8 lines", []string{"a", "b", "c", "d", "e", "f", "g", "h"}, 0},
		{"normal case", []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9"}, 2},
		{
			"trailing blanks anchored from last non-blank",
			[]string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "", "", ""},
			2,
		},
		{"single line", []string{"only"}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := bottomRegion(tt.lines, Claude)
			if got != tt.want {
				t.Errorf("bottomRegion() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestClassifyScreen(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		agentType  Type
		wantStatus Status
		wantLine   string
	}{
		{
			"claude needs_input wins over idle",
			"Do you want to proceed?\n❯ prompt here\n? for shortcuts",
			Claude,
			StatusNeedsInput,
			"Do you want to proceed?",
		},
		{
			"claude working wins over idle",
			"✻ Reading…\n❯ \n? for shortcuts",
			Claude,
			StatusWorking,
			"✻ Reading…",
		},
		{
			"claude idle only",
			"some output\n❯ \n? for shortcuts",
			Claude,
			StatusIdle,
			"❯",
		},
		{
			"shared error wins over idle",
			"Error: something broke\n❯ prompt",
			Claude,
			StatusError,
			"Error: something broke",
		},
		{
			"empty content",
			"",
			Claude,
			"",
			"",
		},
		{
			"no match",
			"just some random text\nnothing special",
			Claude,
			"",
			"",
		},
		{
			"claude needs_input in scrollback ignored",
			"line1\nline2\nDo you want to proceed?\nline4\nline5\nline6\nline7\nline8\nline9\nline10\n❯ \n? for shortcuts",
			Claude,
			StatusIdle,
			"❯",
		},
		{
			"claude needs_input near bottom detected",
			"line1\nline2\nline3\nline4\n❯ \nDo you want to proceed?\n1. Yes\n2. No\nEsc to cancel",
			Claude,
			StatusNeedsInput,
			"Do you want to proceed?",
		},
		{
			// Claude renders a persistent todo footer below the prompt; the
			// anchor must skip it so the prompt stays in the bottom region.
			"claude prompt above todo footer detected",
			"earlier output\n\n Do you want to proceed?\n ❯ 1. Yes\n   2. Yes, and don’t ask again\n   3. No\n\n Esc to cancel · Tab to amend · ctrl+e to explain\n\n  5 tasks (2 done, 1 in progress, 2 open)\n  ◼ Task alpha\n  ◻ Task beta\n  ◻ Task gamma\n  ◻ Task delta\n   … +1 completed",
			Claude,
			StatusNeedsInput,
			"Do you want to proceed?",
		},
		{
			"opencode permission prompt layout",
			"Build · big-pickle · 9.2s\n\nread the file ../RESEARCH.md\n\nThinking: user wants to read a file\n\nRead /Users/example/projects/go/RESEARCH.md\n\nBuild · big-pickle\n\n△ Permission required\nAccess external directory ~/projects/go\n\nPatterns\n\n- /Users/example/projects/go/*\n\n\nAllow once   Allow always   Reject   ctrl+f fullscreen  enter confirm\n• OpenCode 1.2.21\n",
			OpenCode,
			StatusNeedsInput,
			"Allow once",
		},
		{
			"claude plan mode prompt with selection cursor",
			"Would you like to proceed?\n\n ❯ 1. Yes, clear context (62% used) and auto-accept edits (shift+tab)\n   2. Yes, auto-accept edits\n   3. Yes, manually approve edits\n   4. Type here to tell Claude what to change\n\n ctrl-g to edit in Mvim · ~/.claude/plans/...",
			Claude,
			StatusNeedsInput,
			"Would you like to proceed?",
		},
		{
			"claude working in scrollback ignored",
			"line1\n✻ Reading…\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\n❯ \n? for shortcuts",
			Claude,
			StatusIdle,
			"❯",
		},
		{
			"codex screen with working",
			"some output\n• Working (30s • esc to interrupt)\n",
			Codex,
			StatusWorking,
			"• Working",
		},
		{
			// The todo-footer anchor adjustment is Claude-only: a coincidental
			// "N tasks (...)" line in Codex output must NOT pull the anchor up
			// and admit the stale "Working" line from scrollback above it.
			"codex tasks line does not shift anchor",
			"out a\nout b\nout c\n• Working (30s • esc to interrupt)\nout e\nout f\nout g\nout h\nout i\nout j\n5 tasks (2 done, 3 open)\n› type a message",
			Codex,
			StatusIdle,
			"› type a message",
		},
		{
			"codex plan question screen detected",
			"• Placeholder line one.\n\n• Placeholder line two.\n\nQuestion 1/1 (1 unanswered)\nChoose one option.\n\n› 1. Option A (Recommended)  Placeholder detail.\n  2. Option B                Placeholder detail.\n  3. Option C                Placeholder detail.\n  4. None of the above       Placeholder detail.\n",
			Codex,
			StatusNeedsInput,
			"Question 1/1",
		},
		{
			"codex numbered prose without ui marker stays unmatched",
			"1. Placeholder item\n2. Placeholder item\n3. Placeholder item\n",
			Codex,
			"",
			"",
		},
		{
			"cross-agent: claude spinner not detected for codex",
			"✻ Reading…\n",
			Codex,
			"",
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, line := ClassifyScreen(tt.content, tt.agentType)
			if status != tt.wantStatus {
				t.Errorf("ClassifyScreen() status = %q, want %q", status, tt.wantStatus)
			}
			if tt.wantLine != "" && !strings.Contains(line, tt.wantLine) {
				t.Errorf("ClassifyScreen() line = %q, want containing %q", line, tt.wantLine)
			}
		})
	}
}

func TestHasScreen(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		agentType Type
		expected  bool
	}{
		{
			"claude idle prompt in bottom",
			"some output\n❯ ",
			Claude,
			true,
		},
		{
			"claude working spinner",
			"some output\n✻ Reading…",
			Claude,
			true,
		},
		{
			"claude ascii working spinner",
			"some output\n* Germinating…",
			Claude,
			true,
		},
		{
			"claude ascii three-dot working spinner",
			"some output\n* Germinating...",
			Claude,
			true,
		},
		{
			"claude eight spoke working spinner",
			"some output\n✽ Frosting…",
			Claude,
			true,
		},
		{
			"claude four teardrop working spinner",
			"some output\n✢ Frosting…",
			Claude,
			true,
		},
		{
			"claude waiting for task",
			"Task Output worker123\nWaiting for task (esc to give additional instructions)",
			Claude,
			true,
		},
		{
			"claude shell background task",
			"some output\nshell · ⏵⏵ accept edits on · esc to interrupt · ↓ to manage",
			Claude,
			true,
		},
		{
			"claude shell background task with nulls",
			"some output\n\x001\x00shell ·\x00⏵⏵\x00accept\x00edits\x00on ·\x00esc\x00to\x00interrupt\x00·\x00↓\x00to\x00manage",
			Claude,
			true,
		},
		{
			"claude spinner token with null separator",
			"some output\n✻\x00Reading…",
			Claude,
			true,
		},
		{
			"claude needs_input",
			"some output\nAllow file edit?",
			Claude,
			true,
		},
		{
			"codex idle prompt",
			"some output\n› Write tests",
			Codex,
			true,
		},
		{
			"codex working",
			"some output\n• Working (30s • esc to interrupt)",
			Codex,
			true,
		},
		{
			"opencode idle",
			"some output\nctrl+p commands",
			OpenCode,
			true,
		},
		{
			"opencode needs_input",
			"some output\n△ Permission required",
			OpenCode,
			true,
		},
		{
			"cross-agent isolation: claude prompt on codex screen",
			"some output\n❯ ",
			Codex,
			false,
		},
		{
			"cross-agent isolation: codex prompt on claude screen",
			"some output\n› Write tests",
			Claude,
			false,
		},
		{
			"pattern in scrollback only above bottom region",
			"❯ \nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\nline11\nline12",
			Claude,
			false,
		},
		{
			"empty content",
			"",
			Claude,
			false,
		},
		{
			"unknown agent type",
			"❯ ",
			"unknown",
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasScreen(tt.content, tt.agentType)
			if got != tt.expected {
				t.Errorf("HasScreen(%q, %q) = %v, want %v", tt.content, tt.agentType, got, tt.expected)
			}
		})
	}
}

func TestInferFromScreen(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    Type
	}{
		{
			name: "claude explicit product text",
			content: "Claude Code\n\n" +
				"* Germinating…\n" +
				"❯ Try \"fix tests\"",
			want: Claude,
		},
		{
			name:    "claude working pattern",
			content: "some output\n✻ Reading…",
			want:    Claude,
		},
		{
			name:    "claude ascii working pattern",
			content: "some output\n* Germinating…",
			want:    Claude,
		},
		{
			name:    "claude ascii three-dot working pattern",
			content: "some output\n* Germinating...",
			want:    Claude,
		},
		{
			name:    "claude eight spoke working pattern",
			content: "some output\n✽ Frosting…",
			want:    Claude,
		},
		{
			name:    "claude four teardrop working pattern",
			content: "some output\n✢ Frosting…",
			want:    Claude,
		},
		{
			name:    "claude waiting for task pattern",
			content: "Task Output worker123\nWaiting for task (esc to give additional instructions)",
			want:    Claude,
		},
		{
			name:    "claude shell background task pattern",
			content: "some output\nshell · ⏵⏵ accept edits on · esc to interrupt · ↓ to manage",
			want:    Claude,
		},
		{
			name:    "claude shell background task pattern with nulls",
			content: "some output\n\x001\x00shell ·\x00⏵⏵\x00accept\x00edits\x00on ·\x00esc\x00to\x00interrupt\x00·\x00↓\x00to\x00manage",
			want:    Claude,
		},
		{
			name:    "claude spinner token with null separator",
			content: "some output\n✻\x00Reading…",
			want:    Claude,
		},
		{
			name: "claude active signal beats ambiguous idle prompt",
			content: "some output\n" +
				"\x001\x00shell ·\x00⏵⏵\x00accept\x00edits\x00on ·\x00esc\x00to\x00interrupt\x00·\x00↓\x00to\x00manage\n" +
				"❯ ",
			want: Claude,
		},
		{
			name: "codex product text",
			content: "gpt-5.4-codex default · 90% left\n\n" +
				"• Working (30s • esc to interrupt)",
			want: Codex,
		},
		{
			name:    "opencode product text",
			content: "OC | Editing file (opencode)\nctrl+p commands",
			want:    OpenCode,
		},
		{
			name:    "plain shell stays unknown",
			content: "user@host project % ls",
			want:    "",
		},
		{
			name:    "ambiguous shared idle prompt stays unknown",
			content: "❯ \n? for shortcuts",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := InferFromScreen(tt.content); got != tt.want {
				t.Errorf("InferFromScreen() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClassifyScreenBellAnywhereIsNeedsInput(t *testing.T) {
	// A bell prefixed to the first line of a screen taller than the bottom
	// region is still needs_input: the bell is an event, not text.
	body := strings.Repeat("some conversation output\n", 20)
	status, line := ClassifyScreen("\x07"+body+"❯ ", Claude)
	if status != StatusNeedsInput {
		t.Fatalf("status = %q, want needs_input", status)
	}
	if !strings.Contains(line, "\x07") {
		t.Fatalf("matched line should be the one carrying the bell, got %q", line)
	}
	// Without the bell the same screen is idle.
	if status, _ := ClassifyScreen(body+"❯ ", Claude); status != StatusIdle {
		t.Fatalf("status without bell = %q, want idle", status)
	}
}
