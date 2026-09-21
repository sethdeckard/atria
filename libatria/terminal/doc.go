// Package terminal defines the Backend interface that every terminal
// integration implements, and the two backends that compose others: a
// CompositeBackend that merges a launch-capable primary with discovery-only
// integrations, and a CachedBackend that memoizes ListSessions for a TTL.
//
// A Backend lists sessions, reads their screens, sends text, focuses them, and
// launches new ones. Six implementations live in subpackages: iterm, tmux,
// kitty, wezterm, deviceterm, and pty. Optional behaviour (styled screen
// reads, resizing, closing) is discovered by type assertion; see StyledReader.
//
// The composite prefixes each integration's session IDs with its source and a
// colon ("tmux:%3") and leaves the primary's unprefixed, then routes every
// per-session call back to the owning backend by prefix. Sessions sharing a
// TTY are deduplicated with the primary winning, and the caller's own TTY can
// be filtered out with SetSelfTTY so a dashboard doesn't discover itself.
//
// This package knows nothing about agents. Agent detection and status
// classification are in the sibling package agent.
package terminal
