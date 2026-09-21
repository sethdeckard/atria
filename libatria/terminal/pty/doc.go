// Package pty implements terminal.Backend as a built-in multiplexer: each
// session is a child process on its own pseudo-terminal, fed into a vt10x
// emulator. No terminal multiplexer is required. Screen reads come from the
// in-memory buffer without a subprocess; working-directory lookup tries
// /proc first, then falls back to lsof.
//
// Sessions are created by NewSession, which starts the user's $SHELL (or
// /bin/sh) with TERM=xterm-256color; RunCommand then types the command. The
// reader goroutine watches raw output for bell characters, which ReadScreen
// reports by prefixing "\x07" to the next read, and for OSC title changes,
// which become the session name. A process that exits is dropped from
// ListSessions.
//
// The Client also implements Resize and Close. Close closes each PTY and
// sends SIGTERM, escalating to SIGKILL if the session's reader has not
// finished within two seconds. FocusSession is a no-op because
// there is nothing to focus; embedding callers render the screen themselves.
// MonitorOutput is unsupported.
package pty
