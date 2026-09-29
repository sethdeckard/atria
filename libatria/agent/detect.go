package agent

import (
	"path"
	"strings"
)

// Detect returns the agent type a session title names, or "" when the title
// isn't an agent's. Matching is case-insensitive on the product name, with
// two glyph shortcuts: Claude Code titles start with one of ✳ ✻ ✶ ✽ ✢ and
// Copilot titles start with 🤖. Claude Code drops its glyph while idle, so a
// "" result on a session you already track is not evidence the agent exited;
// see HasScreen for that.
func Detect(name string) Type {
	lower := strings.ToLower(name)

	if hasClaudePrefix(name) || strings.Contains(lower, "claude") {
		return Claude
	}

	if strings.Contains(lower, "opencode") {
		return OpenCode
	}

	if strings.HasPrefix(name, "\U0001F916") || strings.Contains(lower, "copilot") {
		return Copilot
	}

	if strings.Contains(lower, "codex") {
		return Codex
	}

	return ""
}

// ExtractActivity returns the activity text an agent put in its session title,
// or "" when the title is only a product name. It strips the agent glyph, the
// "OC | " and "🤖 " prefixes, and a trailing parenthesized job name. Some
// terminals, iTerm2 among them, append the foreground job to the title, so
// "✳ Editing src/game.go (sourcekit-lsp)" with job "sourcekit-lsp" becomes
// "Editing src/game.go". The suffix is stripped only when it names job (or
// its base name, or a login shell's "-zsh" form) or an agent binary from
// Types; any other trailing parenthetical belongs to the title and is kept.
// job may be "" when the terminal doesn't report one. Activity is
// informational; Claude Code updates its title while idle, so a change here
// says nothing about status.
func ExtractActivity(name, job string) string {
	s := name

	// Strip the Claude prefix glyph (with optional trailing space).
	if prefix := claudePrefix(s); prefix != "" {
		s = strings.TrimPrefix(s, prefix)
		s = strings.TrimLeft(s, " ")
	}

	// Strip "OC | " prefix for OpenCode sessions.
	s = strings.TrimPrefix(s, "OC | ")

	// Strip "🤖 " prefix for Copilot sessions.
	s = strings.TrimPrefix(s, "\U0001F916 ")

	if idx := strings.LastIndex(s, "("); idx > 0 && strings.HasSuffix(s, ")") {
		if namesJob(s[idx+1:len(s)-1], job) {
			s = s[:idx]
		}
	}

	s = strings.TrimSpace(s)

	// If the result is just a known product name, return "" so the UI
	// shows "idle" instead of the product name as activity.
	switch strings.ToLower(s) {
	case "claude code", "claude", "codex", "openai codex", "opencode", "github copilot", "copilot", "cd":
		return ""
	}

	return s
}

// namesJob reports whether the suffix matches the session job or a supported
// agent binary.
func namesJob(suffix, job string) bool {
	suffix = strings.TrimPrefix(strings.TrimSpace(suffix), "-")
	if suffix == "" {
		return false
	}
	if job = strings.TrimPrefix(strings.TrimSpace(job), "-"); job != "" {
		if strings.EqualFold(suffix, job) || strings.EqualFold(suffix, path.Base(job)) {
			return true
		}
	}
	for _, t := range Types() {
		if strings.EqualFold(suffix, string(t)) {
			return true
		}
	}
	return false
}

func hasClaudePrefix(name string) bool {
	return claudePrefix(name) != ""
}

func claudePrefix(name string) string {
	for _, prefix := range []string{"\u2733", "✻", "✶", "✽", "✢"} {
		if strings.HasPrefix(name, prefix) {
			return prefix
		}
	}
	return ""
}
