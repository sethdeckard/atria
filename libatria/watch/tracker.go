package watch

import (
	"strings"
	"time"

	"github.com/sethdeckard/atria/libatria/agent"
	"github.com/sethdeckard/atria/libatria/terminal"
)

const (
	// StableUnmatchedReads is how many consecutive identical screen reads
	// with no agent pattern a working session tolerates before it is deemed
	// idle (the agent has probably exited and the pane shows a shell).
	StableUnmatchedReads = 3
	// BlankReads is how many consecutive identical all-blank reads a working
	// session tolerates before it is deemed idle (the backend can't read it).
	BlankReads = 2
	// OrphanThreshold is how many consecutive discovery refreshes a session
	// must look orphaned before Refresh reports it for removal.
	OrphanThreshold = 2
)

// Tracker is the observable state of one agent session and the rules that
// update it. Fields are exported so a caller can render them and a test can
// seed them; change them only through Observe and Refresh.
//
// Status comes from screen reads alone. The session title feeds Activity and
// can retype the agent, but never changes Status, because Claude Code updates
// its title while idle.
type Tracker struct {
	Type   agent.Type
	Source string // composite source label: "pty", "iterm", "tmux", ...

	Status       agent.Status
	Activity     string    // from the title; informational only
	Attention    string    // the matched prompt line while Status is needs_input
	LastActivity time.Time // last status assignment or non-empty activity change

	ScreenChecked  bool      // at least one successful Observe
	LastScreen     string    // last plain screen, NUL bytes replaced by spaces and bell bytes removed
	LastRead       time.Time // time of the last Observe
	UnmatchedReads int       // consecutive identical reads with no agent pattern
	OrphanTicks    int       // consecutive refreshes that looked orphaned
}

// Reason says how a Transition chose its To status.
type Reason int

// Reasons for a transition.
const (
	ReasonNone            Reason = iota // no status decision was made
	ReasonMatched                       // ClassifyScreen matched a pattern
	ReasonMovedOn                       // needs_input plus a changed, unmatched screen: the agent moved on
	ReasonStableUnmatched               // working plus StableUnmatchedReads identical unmatched reads
	ReasonBlank                         // working plus BlankReads identical blank reads
)

func (r Reason) String() string {
	switch r {
	case ReasonMatched:
		return "matched"
	case ReasonMovedOn:
		return "moved-on"
	case ReasonStableUnmatched:
		return "stable-unmatched"
	case ReasonBlank:
		return "blank"
	default:
		return "none"
	}
}

// Transition is the outcome of one Observe.
type Transition struct {
	From, To  agent.Status // To equals From unless Applied and the status changed
	Applied   bool         // Status was assigned and LastActivity stamped
	Changed   bool         // the screen differed from the previous read
	Matched   agent.Status // the raw ClassifyScreen result, "" when nothing matched
	MatchLine string       // the raw ClassifyScreen line
	Reason    Reason
}

// StatusChanged reports whether the status is different after this read.
func (t Transition) StatusChanged() bool { return t.Applied && t.From != t.To }

// EnteredNeedsInput reports the edge a caller rings a bell on.
func (t Transition) EnteredNeedsInput() bool {
	return t.Applied && t.To == agent.StatusNeedsInput && t.From != agent.StatusNeedsInput
}

// LeftNeedsInput reports the edge that clears attention state.
func (t Transition) LeftNeedsInput() bool {
	return t.Applied && t.From == agent.StatusNeedsInput && t.To != agent.StatusNeedsInput
}

// RefreshResult is the outcome of one Refresh.
type RefreshResult struct {
	ActivityChanged bool
	Retyped         bool
	PrevType        agent.Type // the type before a retype
	Orphan          bool       // OrphanTicks reached OrphanThreshold; drop the session
}

// Observe applies one screen read taken at now and returns what changed.
//
// The rules: a matched status is applied unless the screen is unchanged and
// the status is already current (a stale read). An unmatched read resets the
// unmatched counter when the screen changed and increments it otherwise; a
// changed unmatched screen while in needs_input means the agent moved on and
// becomes working, and an unchanged unmatched screen while working becomes
// idle after StableUnmatchedReads reads, or after BlankReads reads when the
// screen is blank. Anything else changes nothing. When a status is applied,
// LastActivity is stamped, Attention is set to the matched line on entering
// or staying in needs_input, and cleared on leaving it.
func (t *Tracker) Observe(screen string, now time.Time) Transition {
	t.ScreenChecked = true
	t.LastRead = now
	screen = strings.ReplaceAll(screen, "\x00", " ")
	// A bell is an event the backend attached to this read, not part of the
	// picture. Classify with it, but compare and remember the screen without
	// it, or the read after a bell would look like the agent moved on.
	visible := strings.ReplaceAll(screen, "\x07", "")
	changed := visible != t.LastScreen
	t.LastScreen = visible

	matched, line := agent.ClassifyScreen(screen, t.Type)
	tr := Transition{From: t.Status, To: t.Status, Changed: changed, Matched: matched, MatchLine: line}

	status := matched
	if status == "" {
		// Only stable (unchanged) unmatched reads count. If the screen is
		// still changing, the agent is producing output the patterns don't
		// know, and that is activity.
		if changed {
			t.UnmatchedReads = 0
		} else {
			t.UnmatchedReads++
		}
		switch {
		case changed && t.Status == agent.StatusNeedsInput:
			status = agent.StatusWorking
			tr.Reason = ReasonMovedOn
		case t.Status == agent.StatusWorking && !changed && t.UnmatchedReads >= StableUnmatchedReads:
			status = agent.StatusIdle
			tr.Reason = ReasonStableUnmatched
		case !changed && t.Status == agent.StatusWorking && isAllBlank(visible) && t.UnmatchedReads >= BlankReads:
			status = agent.StatusIdle
			tr.Reason = ReasonBlank
		default:
			return tr
		}
	} else {
		t.UnmatchedReads = 0
		tr.Reason = ReasonMatched
	}

	// A stale read: identical screen, same status. Nothing to apply.
	if !changed && status == t.Status {
		return tr
	}

	tr.Applied = true
	tr.To = status
	t.Status = status
	t.LastActivity = now
	if status == agent.StatusNeedsInput {
		t.Attention = line
	}
	if tr.From == agent.StatusNeedsInput && status != agent.StatusNeedsInput {
		t.Attention = ""
	}
	return tr
}

// Refresh applies one discovery listing of the session, taken at now.
//
// Activity follows the title (LastActivity is stamped only when the new
// activity is non-empty). The tracker is retyped when the title names a
// different agent, which handles a pane reused by another agent. Source is
// taken from the session. Then an orphan tick is counted when the session is
// idle, has been screen-checked, and either the title no longer names an
// agent while the last screen shows no agent UI, or (iTerm2 only) the
// foreground job is a shell. All three conditions are needed: Claude Code
// drops the agent glyph from its title while idle, and scrollback from an
// exited agent still contains idle patterns higher up the screen.
func (t *Tracker) Refresh(sess terminal.Session, now time.Time) RefreshResult {
	var r RefreshResult
	if activity := agent.ExtractActivity(sess.Name); activity != t.Activity {
		t.Activity = activity
		if activity != "" {
			t.LastActivity = now
		}
		r.ActivityChanged = true
	}
	detected := agent.Detect(sess.Name)
	if detected != "" && detected != t.Type {
		r.PrevType = t.Type
		t.Type = detected
		r.Retyped = true
	}
	if sess.Source != "" {
		t.Source = sess.Source
	}

	shellFallback := t.Source == "iterm" && isShellJob(sess.Job)
	if t.Status == agent.StatusIdle && t.ScreenChecked &&
		((detected == "" && !agent.HasScreen(t.LastScreen, t.Type)) || shellFallback) {
		t.OrphanTicks++
	} else {
		t.OrphanTicks = 0
	}
	r.Orphan = t.OrphanTicks >= OrphanThreshold
	return r
}

// PollInterval returns how long to wait before reading a session's screen
// again: active for working, needs_input, and error, idle otherwise.
func PollInterval(s agent.Status, active, idle time.Duration) time.Duration {
	switch s {
	case agent.StatusWorking, agent.StatusNeedsInput, agent.StatusError:
		return active
	default:
		return idle
	}
}

func isAllBlank(content string) bool {
	return strings.TrimSpace(content) == ""
}

// isShellJob reports whether an iTerm2 foreground job name is a shell, which
// on an idle session means the agent exited and the pane fell back to it.
func isShellJob(job string) bool {
	switch strings.ToLower(strings.TrimSpace(job)) {
	case "sh", "bash", "zsh", "fish", "ksh", "dash", "tcsh", "csh":
		return true
	default:
		return false
	}
}
