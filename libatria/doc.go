// Package libatria assembles atria's terminal backends into one
// terminal.Backend and keeps their roles straight as the set changes at
// runtime.
//
// The library has four main packages, with the terminal clients in
// subpackages. terminal defines the Backend interface,
// the six terminal clients (iTerm2, tmux, kitty, WezTerm, DeviceTerm, and a
// built-in PTY multiplexer), and the composite and cache that combine them.
// agent identifies Claude Code, Codex, OpenCode, and GitHub Copilot from a
// title or a screen and classifies what they are doing. watch turns screen
// reads into status transitions and runs a poller that emits events. This
// package sits on top: [Open] probes the terminals you name, picks the one
// you are running inside as the launch primary, and hands back a [Stack]
// whose Backend lists every agent session it can see.
//
// The primary is chosen by rank and environment: the highest-ranked
// integration (see [Names]) whose probe passed and whose environment matches
// ([EnvMatches]) launches new sessions with unprefixed IDs; every other
// integration contributes sessions under a "source:" prefix. Enable,
// Disable, and Reprobe change the set while the stack is in use and report a
// [RoleChange] when a backend swaps roles, because its session IDs gain or
// lose their prefix and anything held by ID has to follow.
//
// DeviceTerm is the exception to the discovery model: it is the primary or
// absent, never a discovery integration, because a granted Automation tab is
// never inside another terminal and outside one its CLI can't capture, send,
// or focus.
//
// All Stack methods are safe for concurrent use, including Enable, Disable,
// and Reprobe while a watch.Watcher polls the stack's Backend.
// Options.CommandTimeout bounds each CLI invocation and each iTerm2
// request-response exchange (not a whole backend call), and a lost terminal
// is reported as terminal.ErrUnavailable.
package libatria
