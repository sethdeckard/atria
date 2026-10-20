package watch

import (
	"time"

	"github.com/sethdeckard/atria/libatria/agent"
	"github.com/sethdeckard/atria/libatria/terminal"
)

// EventKind says what an Event reports.
type EventKind int

// Event kinds.
const (
	SessionAdded    EventKind = iota // a new agent session is tracked; Session holds its initial state
	SessionRemoved                   // a tracked session is gone; Reason says why
	StatusChanged                    // Session.Status changed from From to To
	ActivityChanged                  // the title's activity text changed
	TypeChanged                      // the pane now runs a different agent; FromType and ToType say which
	ScreenRead                       // a screen was read (only with Options.EmitScreenReads)
	Error                            // a backend call failed; see Event.Err and Event.Session
	// StatusKnown reports a session's first status read from its screen,
	// once per session and before any StatusChanged for it. It follows
	// SessionAdded directly when identification already read the screen,
	// and otherwise comes from the first successful poll; that first read
	// is not also reported as a StatusChanged from the working placeholder.
	StatusKnown
	// PanesChanged reports that Watcher.Panes differs from the last pass:
	// a pane was added or removed, or one's title, job, TTY, working
	// directory, window, or tab changed. It is the last event of its
	// discovery pass and carries only Time; read Panes for the listing. A
	// title that changes only its agent spinner glyph doesn't count, and
	// window titles aren't compared, because most terminals report the
	// selected tab's title there. A pane's frame counts by its tab's split
	// layout, so a swap, a new layout, or a zoom fires it, and a window
	// resize or a divider drag doesn't, unless the drag moves a divider into
	// or out of line with another (see splitLayout).
	PanesChanged
	// AttentionChanged reports that a session in needs_input now shows a
	// different prompt, one dialog having replaced another with no read in
	// between that left needs_input. Session carries the new Attention, and
	// MatchLine holds it too. A new prompt whose question matches the last
	// one's, or contains it or is contained in it once whitespace is
	// removed, doesn't count, so a resize that rewraps the question doesn't
	// fire it. After a session's StatusKnown, entering needs_input is a
	// StatusChanged, not this.
	AttentionChanged
)

func (k EventKind) String() string {
	switch k {
	case SessionAdded:
		return "added"
	case SessionRemoved:
		return "removed"
	case StatusChanged:
		return "status"
	case ActivityChanged:
		return "activity"
	case TypeChanged:
		return "type"
	case ScreenRead:
		return "screen"
	case Error:
		return "error"
	case StatusKnown:
		return "known"
	case PanesChanged:
		return "panes"
	case AttentionChanged:
		return "attention"
	default:
		return "unknown"
	}
}

// RemoveReason says why a session was removed.
type RemoveReason int

// Remove reasons.
const (
	RemovedGone       RemoveReason = iota // absent from the filtered listing, unless its source failed
	RemovedOrphan                         // the agent is gone: Tracker.Refresh reached the orphan threshold (see Refresh), or the TTY no longer runs an agent process
	RemovedOutOfScope                     // gated mode: the pane's replacement agent runs outside the watch directories
)

func (r RemoveReason) String() string {
	switch r {
	case RemovedOrphan:
		return "orphan"
	case RemovedOutOfScope:
		return "out-of-scope"
	default:
		return "gone"
	}
}

// Snapshot is a session's state at one moment, as the Watcher saw it.
type Snapshot struct {
	terminal.Session // ID, Name, TTY, Job, Source as last listed

	Dir          string
	Type         agent.Type
	Process      terminal.Process // zero until resolved; re-resolved on retype and when the PID dies
	Status       agent.Status
	StatusKnown  bool // Status was read from the screen at least once; until then it is the working placeholder
	Preexisting  bool // already listed when its source started answering, rather than appearing while watched
	Activity     string
	Attention    string
	LastActivity time.Time
	LastRead     time.Time
	Screen       string // last capture, styled when read through StyledReader; identification captures are plain; bell bytes removed
}

// Event is one thing the Watcher observed. Kind says which fields are set.
type Event struct {
	Kind    EventKind
	Time    time.Time
	Session Snapshot // state after the change; for Error, set when one session's read failed and zero when the listing failed

	From, To         agent.Status // StatusChanged
	FromType, ToType agent.Type   // TypeChanged
	MatchLine        string       // StatusChanged and StatusKnown: the classifier's line, the attention text when the status is needs_input; AttentionChanged: the new attention text
	Reason           RemoveReason // SessionRemoved
	Screen           string       // ScreenRead; never carries a bell byte
	Err              error        // Error; errors.Is(Err, terminal.ErrUnavailable) for a lost terminal, errors.As into a *terminal.SourceError names the source of a listing failure
}
