// Package agent identifies AI coding agents (Claude Code, Codex, OpenCode,
// GitHub Copilot) from what a terminal shows and classifies what they are
// doing.
//
// The package has four layers. The vocabulary is [Type] and [Status]: a
// Type's string value is the agent's CLI binary name, and a Status is one of
// working, idle, needs_input, or error. Detection reads a session title
// ([Detect], [ExtractActivity]) or screen text ([InferFromScreen],
// [HasScreen]). Classification turns screen text into a Status
// ([ClassifyScreen], [ClassifyOutput]) using a per-agent pattern registry
// ([PatternsFor]). Driving starts an agent in a terminal session ([Launch],
// [LaunchCommand]) and submits prompts to it ([SendPrompt]) with the timing
// these raw-mode TUIs need.
//
// Classification reads the bottom of the screen. Active statuses
// (needs_input, error, working) are trusted only in the last few lines above
// the final non-blank line, because conversation history higher up quotes
// prompt text and spinner glyphs. Idle patterns match anywhere. Each agent has
// its own patterns so one agent's prompt glyph cannot fire on another agent's
// screen; a bell character, "Error:", and a shell prompt are shared fallbacks.
//
// Detection and classification are pure string processing and safe for
// concurrent use. [Installed] consults PATH. [Launch] waits after focusing
// the session; [SendPrompt] spaces input writes. Both call the backend.
package agent
