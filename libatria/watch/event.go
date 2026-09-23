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
	Error                            // a backend call failed; Err is set. Session is set for a per-session read error and zero for a listing error
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
	default:
		return "unknown"
	}
}

// RemoveReason says why a session was removed.
type RemoveReason int

// Remove reasons.
const (
	RemovedGone       RemoveReason = iota // absent from the filtered listing, unless its source failed
	RemovedOrphan                         // Tracker.Refresh reached the orphan threshold; see Refresh for the detection rules
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
	MatchLine        string       // StatusChanged: the classifier's line, the attention text when To is needs_input
	Reason           RemoveReason // SessionRemoved
	Screen           string       // ScreenRead; never carries a bell byte
	Err              error        // Error; errors.Is(Err, terminal.ErrUnavailable) for a lost terminal
}
