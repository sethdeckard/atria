package libatria

import "os"

// Integration names, as they appear in Options.Integrations and Status.Name.
// PTY is the built-in backend and is never listed in Options.Integrations.
const (
	ITerm2     = "iterm2"
	Tmux       = "tmux"
	Kitty      = "kitty"
	WezTerm    = "wezterm"
	DeviceTerm = "deviceterm"
	PTY        = "pty"
)

// names is the precedence order for primary selection, highest first.
var names = []string{DeviceTerm, Tmux, Kitty, WezTerm, ITerm2}

// Names returns the integration names in precedence order, highest first:
// deviceterm, tmux, kitty, wezterm, iterm2. PTY is not listed; it is always
// present and ranks below all of them.
func Names() []string {
	return append([]string(nil), names...)
}

// Source maps an integration name to its composite source label, the value
// of terminal.Session.Source: "iterm2" becomes "iterm", every other name is
// unchanged.
func Source(name string) string {
	if name == ITerm2 {
		return "iterm"
	}
	return name
}

// Prefix returns the session ID prefix a source uses while it is an
// integration rather than the primary: Source(name) followed by a colon.
func Prefix(name string) string {
	return Source(name) + ":"
}

// Rank orders composite sources for primary selection, highest first:
// deviceterm 5, tmux 4, kitty 3, wezterm 2, iterm 1. PTY and unknown
// sources are 0.
func Rank(source string) int {
	switch source {
	case "deviceterm":
		return 5
	case "tmux":
		return 4
	case "kitty":
		return 3
	case "wezterm":
		return 2
	case "iterm":
		return 1
	}
	return 0
}

// EnvMatches reports whether the environment says the process is running
// inside the terminal that name integrates with: TMUX for tmux,
// KITTY_WINDOW_ID for kitty, TERM_PROGRAM=WezTerm or WEZTERM_UNIX_SOCKET for
// wezterm, TERM_PROGRAM=iTerm.app for iterm2, and DEVICETERM_SESSION for
// deviceterm. It is the environment half of primary selection; the other
// half is a passing probe. A nil getenv means os.Getenv. Unknown names never
// match.
func EnvMatches(name string, getenv func(string) string) bool {
	if getenv == nil {
		getenv = os.Getenv
	}
	switch name {
	case Tmux:
		return getenv("TMUX") != ""
	case Kitty:
		return getenv("KITTY_WINDOW_ID") != ""
	case WezTerm:
		return getenv("TERM_PROGRAM") == "WezTerm" || getenv("WEZTERM_UNIX_SOCKET") != ""
	case ITerm2:
		return getenv("TERM_PROGRAM") == "iTerm.app"
	case DeviceTerm:
		return getenv("DEVICETERM_SESSION") != ""
	}
	return false
}
