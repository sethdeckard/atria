// Package agent identifies AI coding agents (Claude Code, Codex, OpenCode,
// GitHub Copilot) from what a terminal shows and classifies what they are
// doing.
//
// The package has three layers. The vocabulary is [Type] and [Status]: a
// Type's string value is the agent's CLI binary name, and a Status is one of
// working, idle, needs_input, or error. Detection reads a session title
// ([Detect], [ExtractActivity]) or screen text ([InferFromScreen],
// [HasScreen]). Classification turns screen text into a Status
// ([ClassifyScreen], [ClassifyOutput]) using a per-agent pattern registry
// ([PatternsFor]).
//
// Classification reads the bottom of the screen. Active statuses
// (needs_input, error, working) are trusted only in the last few lines above
// the final non-blank line, because conversation history higher up quotes
// prompt text and spinner glyphs. Idle patterns match anywhere. Each agent has
// its own patterns so one agent's prompt glyph cannot fire on another agent's
// screen; a bell character, "Error:", and a shell prompt are shared fallbacks.
//
// Everything here is pure string processing and safe for concurrent use.
// [Installed] is the exception: it consults PATH.
package agent
