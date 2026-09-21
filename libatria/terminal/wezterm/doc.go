// Package wezterm implements terminal.Backend on the wezterm CLI, which finds
// the running instance's Unix socket through WEZTERM_UNIX_SOCKET on its own.
//
// Each WezTerm pane is a Session and the pane ID is the session ID. The list
// output carries the title, working directory, and TTY directly, so no
// process lookups are needed. SendText writes through send-text --no-paste on
// stdin, so control bytes arrive as typed. Screen reads use get-text;
// MonitorOutput is unsupported.
package wezterm
