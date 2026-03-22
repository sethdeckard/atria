package terminal

import (
	"regexp"

	"github.com/sethdeckard/atria/internal/model"
)

// AgentPatterns holds compiled regexes for a specific agent type.
type AgentPatterns struct {
	NeedsInput     []*regexp.Regexp
	Working        []*regexp.Regexp
	WorkingExclude []*regexp.Regexp // lines matching these skip working detection
	Idle           []*regexp.Regexp
}

// Per-agent pattern definitions.

var claudePatterns = &AgentPatterns{
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

var codexPatterns = &AgentPatterns{
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

var openCodePatterns = &AgentPatterns{
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

var copilotPatterns = &AgentPatterns{
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

var agentPatternRegistry = map[model.AgentType]*AgentPatterns{
	model.AgentClaude:   claudePatterns,
	model.AgentCodex:    codexPatterns,
	model.AgentOpenCode: openCodePatterns,
	model.AgentCopilot:  copilotPatterns,
}

// Shared patterns that apply to all agent types.
var (
	sharedBellPattern      = regexp.MustCompile("\x07")
	sharedErrorPattern     = regexp.MustCompile(`Error:`)
	sharedCompletedPattern = regexp.MustCompile(`✓|(?:^|[[:space:]])completed(?: successfully)?(?:$|[[:space:].!])|No findings`)
	sharedShellPrompt      = regexp.MustCompile(`\$ $`)
)
