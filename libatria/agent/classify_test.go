package agent

import (
	"os"
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
		{"claude spinner frame ✳", "✳ Beaming… (4m 22s · ↓ 5.2k tokens)", Claude, StatusWorking},
		{"claude ✳ separator mid-line", "~/projects/app ✳  feature/very-lo…", Claude, ""},
		{"claude · separator mid-line", "foo · Reading…", Claude, ""},

		// Codex patterns
		{"codex working bullet", "• Working (30s • esc to interrupt)", Codex, StatusWorking},
		{"codex working simple", "• Working", Codex, StatusWorking},
		{"codex waiting for input", "Waiting for user input", Codex, StatusNeedsInput},
		{"codex run command prompt", "Would you like to run the following command?", Codex, StatusNeedsInput},
		{"codex confirm prompt", "Press enter to confirm or esc to cancel", Codex, StatusNeedsInput},
		{"codex question banner", "Question 1/1 (1 unanswered)", Codex, StatusNeedsInput},
		{"codex none of the above option", "4. None of the above", Codex, StatusNeedsInput},
		{"codex folder trust question", "Trust this folder? Codex can read, edit, and run files here", Codex, StatusNeedsInput},
		{"codex folder trust footer", "enter continue · esc back", Codex, StatusNeedsInput},
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
		{"codex folder trust not claude", "Trust this folder? Codex can read, edit, and run files here", Claude, ""},

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
			"codex folder trust screen",
			"Folder access\n/private/tmp/example\nTrust this folder? Codex can read, edit, and run files here.\n› 1. Trust and continue\n  2. Back to Agent Command Center\nenter continue · esc back\n",
			Codex,
			StatusNeedsInput,
			"Trust this folder?",
		},
		{
			// On a narrow pane the explanation wraps and pushes the question
			// out of the bottom region; the footer still marks the prompt.
			"codex folder trust screen wrapped on narrow pane",
			"Folder access\n/private/tmp/example\nTrust this folder? Codex can\nread, edit, and run files\nhere. Placeholder wrapped\nline one. Placeholder\nwrapped line two.\n\n› 1. Trust and continue\n  2. Back to Agent Command\n     Center\nenter continue · esc back\n",
			Codex,
			StatusNeedsInput,
			"enter continue · esc back",
		},
		{
			"codex numbered prose without ui marker stays unmatched",
			"1. Placeholder item\n2. Placeholder item\n3. Placeholder item\n",
			Codex,
			"",
			"",
		},
		{
			// A ✳ spinner frame with the prompt box and a status line that
			// uses ✳ as its separator below it.
			"claude ✳ spinner frame is working",
			claudeStatusLineScreen("✳ Beaming… (4m 22s · ↓ 5.2k tokens)", "main"),
			Claude,
			StatusWorking,
			"✳ Beaming…",
		},
		{
			"claude ✳ status line separator is not a spinner",
			claudeStatusLineScreen("⏺ Done.", "feature/very-lo…"),
			Claude,
			StatusIdle,
			"❯",
		},
		{
			"claude · footer separator is not a spinner",
			"⏺ Done.\n\n" + strings.Repeat("─", 40) + "\n❯ \n" + strings.Repeat("─", 40) +
				"\n  -- INSERT -- ⏵⏵ auto mode on (shift+tab to cycle) · ← for agents · fix-the-very-lo…",
			Claude,
			StatusIdle,
			"❯",
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

// claudeStatusLineScreen is the bottom of a Claude Code screen: top (a
// spinner row or a finished reply), the prompt box, and a status line that
// separates its segments with ✳, showing branch.
func claudeStatusLineScreen(top, branch string) string {
	rule := strings.Repeat("─", 40)
	return top + "\n\n" + rule + " app-plan ─\n❯ \n" + rule +
		"\n   ~/projects/app ✳  " + branch + " ✳ Opus 5.5 ✳ ctx 87% ✳ 5h:89% 7d:88%" +
		"\n  -- INSERT -- ⏵⏵ auto mode on (shift+tab to cycle) · ← for agents"
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

// Dialog screens for PromptText. The fixture text is generic; only the
// layout follows what each agent draws.
const (
	claudeQuestionDialog = "earlier output\n" +
		"←  ☐ First topic  ☐ Second topic  ✔ Submit  →\n" +
		"│ A refused request returns an empty result. How should the plan handle it?\n" +
		"❯ 1. Fix it in code (Recommended)\n" +
		"     The first unit also makes a refused request fail loudly.\n" +
		"  2. Narrow the prose only\n" +
		"     Leave the code alone and document the behaviour.\n" +
		"  3. Type something.\n" +
		"────────────────────────────────\n" +
		"  4. Chat about this\n" +
		"Enter to select · Tab/Arrow keys to navigate · Esc to cancel\n"
	codexApprovalWithQuestion = "  Would you like to run the following command?\n" +
		"  Environment: local\n" +
		"  Reason: Allow creating /Users/example/notes.txt outside the workspace?\n" +
		"  $ touch /Users/example/notes.txt\n" +
		"› 1. Yes, proceed (y)\n" +
		"  2. Yes, and don't ask again for this command (p)\n" +
		"  3. No, and tell Codex what to do differently (esc)\n" +
		"  Press enter to confirm or esc to cancel\n"
)

func TestPromptText(t *testing.T) {
	tests := []struct {
		name   string
		screen string
		agent  Type
		want   string
	}{
		{"claude question above a footer", claudeQuestionDialog, Claude,
			"A refused request returns an empty result. How should the plan handle it?"},
		{"codex question pattern in view", codexApprovalWithQuestion, Codex,
			"Would you like to run the following command?"},
		{"single blank lines inside a dialog don't stop the search",
			"  Reason: Allow creating /Users/example/notes.txt outside the workspace?\n\n" +
				"  $ touch /Users/example/notes.txt\n\n› 1. Yes, proceed (y)\n  2. No (esc)\n\n" +
				"  Press enter to confirm or esc to cancel\n", Codex,
			"Reason: Allow creating /Users/example/notes.txt outside the workspace?"},
		{"no question falls back to the footer",
			" Select a model\n ❯ 1. Default\n   2. Fast\n Enter to select · Esc to cancel\n", Claude,
			"Enter to select · Esc to cancel"},
		{"a dialog's title stops the search before the conversation",
			"> Can you check the parser?\n\n\n Select a model\n ❯ 1. Default\n Enter to select · Esc to cancel\n", Claude,
			"Enter to select · Esc to cancel"},
		{"a question more than 25 rows up isn't taken",
			"> Can you check the parser?\n" + strings.Repeat("output\n", 24) + " Select a model\n ❯ 1. Default\n Enter to select · Esc to cancel\n", Claude,
			"Enter to select · Esc to cancel"},
		{"boxed question loses its borders",
			"╭──────────────────────────────────────────────╮\n" +
				"│ Do you want to make this edit to parser.go? │\n" +
				"│ ❯ 1. Yes                                     │\n" +
				"│   2. No                                      │\n" +
				"╰──────────────────────────────────────────────╯\n" +
				"   Esc to cancel\n", Claude,
			"Do you want to make this edit to parser.go?"},
		{"a question pattern match is kept",
			"Do you want to proceed?\n ❯ 1. Yes\n   2. No\n Esc to cancel · Tab to amend\n", Claude,
			"Do you want to proceed?"},
		{"not needs_input", "some output\n✻ Reading…\n", Claude, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PromptText(tt.screen, tt.agent); got != tt.want {
				t.Errorf("PromptText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPromptTextLeavesClassificationAlone(t *testing.T) {
	tests := []struct {
		name     string
		screen   string
		agent    Type
		wantLine string
	}{
		{"claude dialog", claudeQuestionDialog, Claude, "Enter to select · Tab/Arrow keys to navigate · Esc to cancel"},
		{"codex approval", codexApprovalWithoutReasonRows(t), Codex, "Press enter to confirm or esc to cancel"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, line := ClassifyScreen(tt.screen, tt.agent)
			if status != StatusNeedsInput || line != tt.wantLine {
				t.Errorf("ClassifyScreen() = %q, %q, want needs_input, %q", status, line, tt.wantLine)
			}
		})
	}
}

// TestPromptTextCaptured runs PromptText on real captures from a 60-column
// tmux pane (tmux capture-pane -p, without -J), cropped at the top only.
// Claude Code 2.1.289 repeats its │ border on each wrapped row; Codex 0.160.0
// wraps at 58 columns, and its Reason row stops short before a long path.
func TestPromptTextCaptured(t *testing.T) {
	tests := []struct {
		file  string
		agent Type
		want  string
	}{
		{"claude-question-60col.txt", Claude,
			"For the small inventory service we're building, which tracks products, stock levels, and warehouse locations with modest traffic and a small team maintaining it, which database would you prefer to use as the primary data store going forward?"},
		{"codex-approval-60col.txt", Codex,
			"Reason: May I create /Users/Shared/atria-capture-test.txt outside the workspace as requested so the inventory export check has the marker file it needs to verify that its shared output location is available?"},
		// No row ends in "?": the question runs on into "(Like your own code…".
		{"claude-trust-60col.txt", Claude, "Enter to confirm · Esc to cancel"},
		// /model: the search stops at the subtitle above option 1, so the
		// conversation's "❯ /model" never gets a look. The footer wraps, and
		// the matched row is its second line.
		{"claude-model-60col.txt", Claude, "Esc to cancel"},
		// /model after Claude replied with a question: the reply (⏺) and the
		// wrapped prompt above it stay out of the dialog's text.
		{"claude-model-after-reply-60col.txt", Claude, "Esc to cancel"},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			screen, err := os.ReadFile("testdata/" + tt.file)
			if err != nil {
				t.Fatal(err)
			}
			if got := PromptText(string(screen), tt.agent); got != tt.want {
				t.Errorf("PromptText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPromptTextJoinsWrappedRows(t *testing.T) {
	// A 30-column rule sets the width the rows are measured against.
	const rule = "──────────────────────────────\n"
	tests := []struct {
		name, screen, want string
	}{
		{"wrap before a capitalized word",
			rule + "Which backend should we use for\nTypeScript?\n❯ 1. Node\nEnter to select · Esc to cancel\n",
			"Which backend should we use for TypeScript?"},
		{"wrap before a path",
			rule + "Can I write to\n/Users/Shared/some-long-name.txt?\n❯ 1. Yes\nEnter to select · Esc to cancel\n",
			"Can I write to /Users/Shared/some-long-name.txt?"},
		{"a short row above is its own line",
			rule + "Pick a backend.\nWhich one should we use?\n❯ 1. Node\nEnter to select · Esc to cancel\n",
			"Which one should we use?"},
		{"a long row ending a sentence isn't a wrap into a capital",
			rule + "This sentence runs to the edge.\nWhich one should we use?\n❯ 1. Node\nEnter to select · Esc to cancel\n",
			"Which one should we use?"},
		{"a tab bar above is never joined",
			rule + "← ☐ Backend ☐ Database ✔ Submit\nwhich one should we use?\n❯ 1. Node\nEnter to select · Esc to cancel\n",
			"which one should we use?"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PromptText(tt.screen, Claude); got != tt.want {
				t.Errorf("PromptText() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestPromptTextNoQuestionInCodexApproval pins what a dialog with no question
// shows: the real Codex capture with its "Would you like to run…?" row and
// its wrapped "Reason: …?" rows taken out. Nothing within questionGap rows
// above option 1 ends in "?", so it keeps the footer rather than reaching
// into the conversation.
func TestPromptTextNoQuestionInCodexApproval(t *testing.T) {
	screen := codexApprovalWithoutReasonRows(t)
	if got, want := PromptText(screen, Codex), "Press enter to confirm or esc to cancel"; got != want {
		t.Errorf("PromptText() = %q, want %q", got, want)
	}
}

// codexApprovalWithoutReasonRows is the real Codex approval capture with its
// "Would you like to run…?" row and its wrapped "Reason: …?" rows removed.
func codexApprovalWithoutReasonRows(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/codex-approval-60col.txt")
	if err != nil {
		t.Fatal(err)
	}
	var kept []string
	inReason := false
	for _, row := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(row)
		switch {
		case strings.HasPrefix(trimmed, "Would you like to run"):
			continue
		case strings.HasPrefix(trimmed, "Reason:"):
			inReason = true
		}
		if inReason {
			inReason = !strings.HasSuffix(trimmed, "?")
			continue
		}
		kept = append(kept, row)
	}
	screen := strings.Join(kept, "\n")
	if strings.Contains(screen, "Reason:") || strings.Contains(screen, "available?") {
		t.Fatalf("the question rows weren't all removed:\n%s", screen)
	}
	return screen
}

// TestPromptTextJoinStaysInWindow checks that joining a wrapped question
// reads no row above the 25-row window the search is bounded by. The
// options aren't numbered, so the search isn't anchored on them.
func TestPromptTextJoinStaysInWindow(t *testing.T) {
	// The question's last row sits exactly 25 rows above the footer, at the
	// top of the window; the row it wrapped from is one row above that.
	screen := "──────────────────────────────\n" +
		"Which backend should we use for\n" +
		"the new service?\n" +
		strings.Repeat("  detail\n", 23) +
		"❯ Node\n" +
		"Enter to select · Esc to cancel\n"
	if got, want := PromptText(screen, Claude), "the new service?"; got != want {
		t.Errorf("PromptText() = %q, want %q", got, want)
	}
}

func TestPromptTextSkipsQuestionsInOptions(t *testing.T) {
	tests := []struct {
		name, screen, want string
		agent              Type // Claude when empty
	}{
		{"an option label and a description end in ?",
			"←  ☐ Cache  ✔ Submit  →\n" +
				"│ Where should the cache live?\n" +
				"❯ 1. Keep it in memory?\n" +
				"     Fastest, lost on restart.\n" +
				"  2. Put it on disk\n" +
				"     Survives restarts. Should we encrypt it?\n" +
				"  3. Type something.\n" +
				"────────────────────────────────\n" +
				"  4. Chat about this\n" +
				"Enter to select · Tab/Arrow keys to navigate · Esc to cancel\n",
			"Where should the cache live?", ""},
		{"options with only questions fall back to the footer",
			"│ Pick one.\n" +
				"❯ 1. Keep it in memory?\n" +
				"  2. Put it on disk?\n" +
				"Enter to select · Esc to cancel\n",
			"Enter to select · Esc to cancel", ""},
		{"a wrapped command between the question and the options",
			"──────────────────────────────\n" +
				"  Reason: May I write the file?\n" +
				"\n" +
				"  $ touch /Users/Shared/some/long\n" +
				"  /path/name.txt\n" +
				"\n" +
				"› 1. Yes, proceed (y)\n" +
				"  Press enter to confirm or esc to cancel\n",
			"Reason: May I write the file?", Codex},
		{"a title and a blank row above the options, under a Claude reply",
			"⏺ Can you check the parser?\n" +
				"\n" +
				" Select a model\n" +
				"\n" +
				" ❯ 1. Default\n" +
				" Enter to select · Esc to cancel\n",
			"Enter to select · Esc to cancel", ""},
		{"a title and a blank row above the options, under a Codex prompt",
			"› Can you check the parser?\n" +
				"\n" +
				"  Pick one\n" +
				"\n" +
				"› 1. Yes, proceed (y)\n" +
				"  Press enter to confirm or esc to cancel\n",
			"Press enter to confirm or esc to cancel", Codex},
		{"a reply without a question stops the search before older output",
			"  ⎿  Is the cache warm?\n" +
				"\n" +
				"⏺ Done.\n" +
				"\n" +
				" Select a model\n" +
				"\n" +
				" ❯ 1. Default\n" +
				" Enter to select · Esc to cancel\n",
			"Enter to select · Esc to cancel", ""},
		{"a wrapped prompt whose question is on an indented row",
			"────────────────────────────────────────────────────────────\n" +
				"❯ Reply with exactly this one line and nothing else: Can\n" +
				"  you check the parser?\n" +
				"\n" +
				" Select a model\n" +
				"\n" +
				" ❯ 1. Default\n" +
				" Enter to select · Esc to cancel\n",
			"Enter to select · Esc to cancel", ""},
		{"a numbered list in the conversation above doesn't hide the question",
			"Two ways to do it:\n" +
				"1. Keep the cache in memory\n" +
				"2. Put it on disk\n" +
				"\n" +
				"│ Where should the cache live?\n" +
				"\n" +
				"❯ 1. Memory\n" +
				"  2. Disk\n" +
				"Enter to select · Esc to cancel\n",
			"Where should the cache live?", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := tt.agent
			if agent == "" {
				agent = Claude
			}
			if got := PromptText(tt.screen, agent); got != tt.want {
				t.Errorf("PromptText() = %q, want %q", got, tt.want)
			}
		})
	}
}
