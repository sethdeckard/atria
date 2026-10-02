package agent

import (
	"regexp"
)

// Patterns holds the compiled regular expressions that classify one agent's
// screen lines. Each slice is tried in order; the first match decides.
type Patterns struct {
	NeedsInput     []*regexp.Regexp // the agent is waiting on a prompt, permission, or question
	Working        []*regexp.Regexp // spinner, "esc to interrupt", and similar activity markers
	WorkingExclude []*regexp.Regexp // lines matching these are never counted as working
	Idle           []*regexp.Regexp // the agent's input prompt or help footer
}

// PatternsFor returns the registry's patterns for t, or nil for an unknown
// type. The returned value is the registry's own entry: don't modify the
// struct, its slices, or the regular expressions.
func PatternsFor(t Type) *Patterns {
	return registry[t]
}

// Per-agent pattern definitions. Each comment names the screen the pattern
// matches; classify_test.go has a sample line for most of them.

var claudePatterns = &Patterns{
	NeedsInput: []*regexp.Regexp{
		regexp.MustCompile(`Do you want to proceed`),    // permission dialog question
		regexp.MustCompile(`Would you like to proceed`), // plan-mode approval; its ❯ cursor also matches idle
		regexp.MustCompile(`Allow .+\?`),                // "Allow file edit?" and similar
		regexp.MustCompile(`Esc to cancel`),             // permission dialog footer
	},
	Working: []*regexp.Regexp{
		// Spinner glyph, then the activity word ("✶ Doodling…", "* Germinating...").
		// A bare ✻ with no activity means the turn is done.
		regexp.MustCompile(`(?:[✻✶✽✢·]|\*)\s+\S+(?:…|\.{3})`),
		// Status bar while a background shell task runs.
		regexp.MustCompile(`⏵⏵.*esc\s+to\s+interrupt`),
		// Waiting on a background task's output.
		regexp.MustCompile(`Waiting for task \(esc to give additional instructions\)`),
	},
	WorkingExclude: []*regexp.Regexp{
		// A "(running)" task in the status bar shows even while Claude is idle.
		regexp.MustCompile(`⏵⏵.*\(running\).*esc\s+to\s+interrupt`),
	},
	Idle: []*regexp.Regexp{
		regexp.MustCompile(`❯`),                // input prompt
		regexp.MustCompile(`\? for shortcuts`), // help hint under the prompt
	},
}

var codexPatterns = &Patterns{
	NeedsInput: []*regexp.Regexp{
		regexp.MustCompile(`Waiting for .+ input`),   // "Waiting for user input"
		regexp.MustCompile(`Would you like to run`),  // command approval
		regexp.MustCompile(`Press enter to confirm`), // command approval footer
		regexp.MustCompile(`Question \d+/\d+`),       // plan-question banner
		regexp.MustCompile(`None of the above`),      // plan-question fallback option
		regexp.MustCompile(`Trust this folder\?`),    // folder-trust screen question
		// Folder-trust footer. On a narrow pane the explanation wraps and
		// pushes the question out of the bottom region; this still matches.
		regexp.MustCompile(`enter continue · esc back`),
	},
	Working: []*regexp.Regexp{
		regexp.MustCompile(`[•●] Working`),         // "• Working (30s • esc to interrupt)"
		regexp.MustCompile(`esc\s+to\s+interrupt`), // the same line's interrupt hint
	},
	WorkingExclude: []*regexp.Regexp{
		// Keeps the "esc to interrupt" pattern above from matching a ⏵ status
		// bar such as Claude's background-task bar.
		regexp.MustCompile(`⏵`),
	},
	Idle: []*regexp.Regexp{
		regexp.MustCompile(`›`),             // input prompt and option cursor
		regexp.MustCompile(`gpt-\S+-codex`), // status line ("gpt-5.3-codex default · 73% left")
	},
}

var openCodePatterns = &Patterns{
	NeedsInput: []*regexp.Regexp{
		regexp.MustCompile(`Permission required`), // "△ Permission required" dialog
		regexp.MustCompile(`Allow once`),          // the dialog's button row
	},
	Working: []*regexp.Regexp{
		regexp.MustCompile(`esc\s+interrupt`), // "esc interrupt", with no "to"
	},
	WorkingExclude: []*regexp.Regexp{
		// Matches no known OpenCode line; OpenCode's working pattern doesn't
		// match a ⏵⏵ … esc to interrupt bar either way.
		regexp.MustCompile(`⏵`),
	},
	Idle: []*regexp.Regexp{
		regexp.MustCompile(`ctrl\+p commands`), // footer hint
	},
}

var copilotPatterns = &Patterns{
	NeedsInput: []*regexp.Regexp{
		regexp.MustCompile(`Do you trust the files in this folder`), // folder-trust prompt
		regexp.MustCompile(`Permission request`),                    // permission dialog
		regexp.MustCompile(`Enter to (?:confirm|select)`),           // selection footer
	},
	Working: []*regexp.Regexp{
		regexp.MustCompile(`[○◎●] Thinking`), // spinner glyph and "Thinking"
		regexp.MustCompile(`Esc to cancel`),  // cancel hint (on Claude this is a dialog footer)
	},
	WorkingExclude: []*regexp.Regexp{},
	Idle: []*regexp.Regexp{
		regexp.MustCompile(`❯`),                // input prompt
		regexp.MustCompile(`\? for shortcuts`), // help hint under the prompt
	},
}

// registry maps each supported agent to its patterns.
var registry = map[Type]*Patterns{
	Claude:   claudePatterns,
	Codex:    codexPatterns,
	OpenCode: openCodePatterns,
	Copilot:  copilotPatterns,
}

// Shared patterns that apply to all agent types. ClassifyOutput's doc
// comment gives where each one sits in the classification order.
var (
	sharedBellPattern      = regexp.MustCompile("\x07")
	sharedErrorPattern     = regexp.MustCompile(`Error:`)
	sharedCompletedPattern = regexp.MustCompile(`✓|(?:^|[[:space:]])completed(?: successfully)?(?:$|[[:space:].!])|No findings`)
	sharedShellPrompt      = regexp.MustCompile(`\$ $`)
)
