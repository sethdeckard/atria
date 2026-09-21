// Package kitty implements terminal.Backend on the kitten @ remote-control
// CLI over a Unix socket, so nothing is written to the controlling TTY.
//
// Kitty must be configured with allow_remote_control yes and a listen_on
// socket; KITTY_LISTEN_ON must be set in the environment (Kitty sets it when
// listen_on is configured). Each Kitty window is a Session and the window ID is
// the session ID. Titles come from the window title, the working directory
// from Kitty's window metadata, and the TTY from the window's reported PID.
//
// Screen reads use get-text; MonitorOutput is unsupported.
package kitty
