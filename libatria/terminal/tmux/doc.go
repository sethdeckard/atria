// Package tmux implements terminal.Backend on the tmux command-line client.
// Sessions are discovered across every tmux session with list-panes -a, and
// the pane ID ("%3") is the session ID.
//
// Agent detection relies on pane titles, so tmux's allow-rename option (on by
// default) must stay on for Claude Code's title escapes to reach pane_title.
// NewSession opens a window in the configured launch session, otherwise in
// the current tmux session when running inside tmux, otherwise in a detached
// session named by Options.FallbackSession. FocusSession selects the window and, when running
// inside tmux, also attempts to switch the client to the owning session.
//
// SendText uses send-keys -l, with a carriage return or newline mapped to the
// Enter key name. Screen reads use capture-pane. MonitorOutput is unsupported.
package tmux
