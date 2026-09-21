// Package iterm implements terminal.Backend for iTerm2 over its native
// protobuf-over-WebSocket API on a Unix socket. No Python runtime is
// required; interactive authentication uses osascript.
//
// iTerm2 must have the Python API enabled (Settings > General > Magic). Inside
// iTerm2 the ITERM2_COOKIE and ITERM2_KEY variables authenticate the
// connection and are removed from this process's environment once read so
// child processes don't inherit them. The client first connects with any
// credentials it has; if the handshake returns 401, it requests credentials
// through an AppleScript dialog unless SetNoPrompt(true) was called. Suppress
// the dialog whenever the caller is a TUI or has no user at the keyboard.
// With prompting disabled and no valid credentials, connecting requires
// iTerm2's automation auth to be disabled by creating
// ~/.config/iterm2/disable-automation-auth.
//
// Each iTerm2 session is a Session; its iTerm2 session ID is the ID. Screen
// reads use GetBufferRequest and are the primary status mechanism.
// MonitorOutput is unsupported.
package iterm
