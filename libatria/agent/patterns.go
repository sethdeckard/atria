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

// Per-agent pattern definitions.

var claudePatterns = &Patterns{
	NeedsInput: []*regexp.Regexp{
		regexp.MustCompile(`Do you want to proceed`),
		regexp.MustCompile(`Would you like to proceed`),
		regexp.MustCompile(`Allow .+\?`),
		regexp.MustCompile(`Esc to cancel`),
	},
	Working: []*regexp.Regexp{
		regexp.MustCompile(`(?:[✻✶✽✢·]|\*)\s+\S+(?:…|\.{3})`),
		regexp.MustCompile(`⏵⏵.*esc\s+to\s+interrupt`),
		regexp.MustCompile(`Waiting for task \(esc to give additional instructions\)`),
	},
	WorkingExclude: []*regexp.Regexp{
		regexp.MustCompile(`⏵⏵.*\(running\).*esc\s+to\s+interrupt`),
	},
	Idle: []*regexp.Regexp{
		regexp.MustCompile(`❯`),
		regexp.MustCompile(`\? for shortcuts`),
	},
}

var codexPatterns = &Patterns{
	NeedsInput: []*regexp.Regexp{
		regexp.MustCompile(`Waiting for .+ input`),
		regexp.MustCompile(`Would you like to run`),
		regexp.MustCompile(`Press enter to confirm`),
		regexp.MustCompile(`Question \d+/\d+`),
		regexp.MustCompile(`None of the above`),
	},
	Working: []*regexp.Regexp{
		regexp.MustCompile(`[•●] Working`),
		regexp.MustCompile(`esc\s+to\s+interrupt`),
	},
	WorkingExclude: []*regexp.Regexp{
		regexp.MustCompile(`⏵`),
	},
	Idle: []*regexp.Regexp{
		regexp.MustCompile(`›`),
		regexp.MustCompile(`gpt-\S+-codex`),
	},
}

var openCodePatterns = &Patterns{
	NeedsInput: []*regexp.Regexp{
		regexp.MustCompile(`Permission required`),
		regexp.MustCompile(`Allow once`),
	},
	Working: []*regexp.Regexp{
		regexp.MustCompile(`esc\s+interrupt`),
	},
	WorkingExclude: []*regexp.Regexp{
		regexp.MustCompile(`⏵`),
	},
	Idle: []*regexp.Regexp{
		regexp.MustCompile(`ctrl\+p commands`),
	},
}

var copilotPatterns = &Patterns{
	NeedsInput: []*regexp.Regexp{
		regexp.MustCompile(`Do you trust the files in this folder`),
		regexp.MustCompile(`Permission request`),
		regexp.MustCompile(`Enter to (?:confirm|select)`),
	},
	Working: []*regexp.Regexp{
		regexp.MustCompile(`[○◎●] Thinking`),
		regexp.MustCompile(`Esc to cancel`),
	},
	WorkingExclude: []*regexp.Regexp{},
	Idle: []*regexp.Regexp{
		regexp.MustCompile(`❯`),
		regexp.MustCompile(`\? for shortcuts`),
	},
}

// registry maps each supported agent to its patterns.
var registry = map[Type]*Patterns{
	Claude:   claudePatterns,
	Codex:    codexPatterns,
	OpenCode: openCodePatterns,
	Copilot:  copilotPatterns,
}

// Shared patterns that apply to all agent types.
var (
	sharedBellPattern      = regexp.MustCompile("\x07")
	sharedErrorPattern     = regexp.MustCompile(`Error:`)
	sharedCompletedPattern = regexp.MustCompile(`✓|(?:^|[[:space:]])completed(?: successfully)?(?:$|[[:space:].!])|No findings`)
	sharedShellPrompt      = regexp.MustCompile(`\$ $`)
)
