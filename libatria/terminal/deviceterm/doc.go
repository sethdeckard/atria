// Package deviceterm implements terminal.Backend on the deviceterm CLI, which
// is spawned as a direct child for every call.
//
// The DeviceTerm daemon authorizes each CLI call by walking the process tree
// back to the tab that started it, and only a human-opened Automation tab
// (Shell > Open Automation Tab) holds the automation grant that capture-text,
// send-input, focus, and tab open require. Available reports the grant state
// with session show and ListSessions re-checks it on every call, so a grant
// lost mid-session (the GUI connection dropped) fails the whole refresh rather
// than returning a partial list. Never run the CLI detached or under tmux
// inside the tab; both break the ancestry and the daemon refuses the call.
//
// DeviceTerm 0.11.0 or later is required for the verbs used here. Each
// terminal pane is a Session with the pane ID (a lowercase UUID) as the
// session ID; a pane's own live title, TTY, and best-effort working directory
// come from pane list --all. Errors from JSON verbs decode into CLIError;
// branch on Code, never on Message. MonitorOutput is unsupported.
package deviceterm
