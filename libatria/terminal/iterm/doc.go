// Package iterm implements terminal.Backend for iTerm2 over its native
// protobuf-over-WebSocket API on a Unix socket. No Python runtime is
// required; interactive authentication uses osascript.
//
// iTerm2 must have the Python API enabled (Settings > General > Magic). Inside
// iTerm2 the ITERM2_COOKIE and ITERM2_KEY variables authenticate the
// connection and are removed from this process's environment once read so
// child processes don't inherit them. The client first connects with any
// credentials it has; if the handshake returns 401, it requests credentials
// through an AppleScript dialog unless Options.NoPrompt is set. Set it
// whenever the caller is a TUI or has no user at the keyboard. The request
// works from any process allowed to send Apple events, inside iTerm2 or not,
// so a background process can prompt too. The first request triggers macOS's
// Automation consent, which macOS attributes to the app the process was
// started from (Terminal.app, say), not to the binary. The wait for an
// answer is bounded by Options.AuthTimeout, and a request that is declined
// or times out disarms the client's AuthGate: later 401s, reconnects
// included, fail with ErrAuthFailed and show no dialog until the gate is
// re-armed. With Options.NoPrompt set, a 401 handshake response returns an
// error wrapping ErrAuthRequired. To connect without credentials, disable
// iTerm2's automation auth by creating
// ~/.config/iterm2/disable-automation-auth.
//
// Each iTerm2 session is a Session; its iTerm2 session ID is the ID. Screen
// reads use GetBufferRequest and are the primary status mechanism.
// MonitorOutput is unsupported.
package iterm
