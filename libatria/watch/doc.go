// Package watch follows agent sessions over time: which sessions exist, which
// agent runs in each, and what that agent is doing.
//
// Tracker is the state machine for one session. It is pure: feed it screen
// reads with Observe and listing results with Refresh, and it applies the
// debounce and orphan rules that decide when a status change is real. Callers
// can drive one Tracker per session from their own event loop.
//
// Identify decides whether a listed session is an agent session, in which
// directory, and behind which process. With watch directories it admits only
// sessions under them; with none, every agent session the backend lists.
//
// Watcher runs the whole loop in a goroutine for programs without an event
// loop of their own: it lists sessions on one cadence, reads screens on
// another (faster for active agents), and delivers Event values on a channel.
// Sends block when the channel is full rather than dropping, because a lost
// needs_input event is an agent waiting for someone who never comes; every
// send also selects on the context, so cancelling Run always returns and any
// events still pending are abandoned. A failed listing or a source reported
// by terminal.FailureReporter keeps its sessions rather than removing them,
// and errors.Is(ev.Err, terminal.ErrUnavailable) tells a lost terminal from
// an ordinary read failure.
//
// Watcher's accessors are safe for concurrent use with Run, and the backend
// may change underneath it (integrations enabled, disabled, or re-probed)
// between ticks.
//
// Known limitations. Process liveness is checked with kill(pid, 0), which
// reports a zombie or a reused PID as alive; a batched ps per tick would be
// more accurate. A pane whose scrollback still shows an agent banner can be
// identified again a tick after it was removed as an orphan and removed again
// later (atria's dashboard has the same churn); a cooldown after orphan
// removal would stop it. A screen-read failure during Identify is logged, not
// emitted as an Error event.
package watch
