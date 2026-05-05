package tui

import "github.com/charmbracelet/lipgloss"

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#1a1a2e", Dark: "#e0e0e0"})

	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#1a1a2e", Dark: "#ffffff"}).
			Background(lipgloss.AdaptiveColor{Light: "#d0d0ff", Dark: "#3a3a5c"})

	normalStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#333333", Dark: "#cccccc"})

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#999999", Dark: "#666666"})

	statusWorkingStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#2d7d46", Dark: "#4ec969"})

	statusIdleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#b8860b", Dark: "#f0c040"})

	attentionRowStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.AdaptiveColor{Light: "#993300", Dark: "#ffcc00"}).
				Background(lipgloss.AdaptiveColor{Light: "#ffe0a0", Dark: "#4a3510"})

	attentionSelectedStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.AdaptiveColor{Light: "#993300", Dark: "#ffffff"}).
				Background(lipgloss.AdaptiveColor{Light: "#ffd080", Dark: "#6a4a10"})

	statusNeedsInputStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.AdaptiveColor{Light: "#cc6600", Dark: "#ffaa33"})

	statusErrorStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.AdaptiveColor{Light: "#cc0000", Dark: "#ff4444"})

	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#999999", Dark: "#666666"}).
			MarginTop(1)

	chatSentStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#2d5fa1", Dark: "#6699ff"})

	chatReceivedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#333333", Dark: "#cccccc"})

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#666666", Dark: "#999999"}).
			Background(lipgloss.AdaptiveColor{Light: "#f0f0f0", Dark: "#2a2a2a"}).
			Padding(1, 2)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#cc6600", Dark: "#ffaa33"})

	logoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#5555aa", Dark: "#7777cc"})

	emptyHintStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#666666", Dark: "#888888"})

	emptyKeyStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#4444aa", Dark: "#9999dd"})

	selectedTextStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.AdaptiveColor{Light: "#1a1a2e", Dark: "#ffffff"}).
				Background(lipgloss.AdaptiveColor{Light: "#d0d0ff", Dark: "#3a3a5c"})

	brandingStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#999999", Dark: "#555555"})

	agentClaudeStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#b07020", Dark: "#e0a050"})

	agentCodexStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#2d7d46", Dark: "#6bc98a"})

	agentOpenCodeStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#2d5fa1", Dark: "#6ca6f0"})

	agentCopilotStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#b04080", Dark: "#e070b0"})
)

var selectedBg lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#d0d0ff", Dark: "#3a3a5c"}

// useReverseSelection flips selection rendering to reverse video. Only set by
// ApplyANSITheme — the ANSI-16 palette has no neutral mid-tone bg that plays
// well with every foreground color, so we let the terminal invert its own
// colors for selection. Built-in theme keeps the pale-indigo background.
var useReverseSelection bool

func withSelectedBg(s lipgloss.Style) lipgloss.Style {
	if useReverseSelection {
		return s.Reverse(true)
	}
	return s.Background(selectedBg)
}

// ApplyANSITheme switches all styles to use the terminal's ANSI base-16 palette.
func ApplyANSITheme() {
	titleStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("15"))

	selectedStyle = lipgloss.NewStyle().
		Bold(true).
		Reverse(true)

	normalStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("7"))

	dimStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("8"))

	statusWorkingStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("2"))

	statusIdleStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("3"))

	attentionRowStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("3"))

	attentionSelectedStyle = lipgloss.NewStyle().
		Bold(true).
		Reverse(true).
		Foreground(lipgloss.Color("3"))

	statusNeedsInputStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("3"))

	statusErrorStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("1"))

	footerStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("8")).
		MarginTop(1)

	chatSentStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("4"))

	chatReceivedStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("7"))

	helpStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("7")).
		Background(lipgloss.Color("0")).
		Padding(1, 2)

	statusBarStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("3"))

	logoStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("4"))

	emptyHintStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("8"))

	emptyKeyStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("4"))

	selectedTextStyle = lipgloss.NewStyle().
		Bold(true).
		Reverse(true)

	brandingStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("8"))

	agentClaudeStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("3"))

	agentCodexStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("2"))

	agentOpenCodeStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("4"))

	agentCopilotStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("5"))

	selectedBg = lipgloss.Color("4")
	useReverseSelection = true
}

// ApplyDefaultTheme restores the default hardcoded color palette.
func ApplyDefaultTheme() {
	titleStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#1a1a2e", Dark: "#e0e0e0"})

	selectedStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#1a1a2e", Dark: "#ffffff"}).
		Background(lipgloss.AdaptiveColor{Light: "#d0d0ff", Dark: "#3a3a5c"})

	normalStyle = lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#333333", Dark: "#cccccc"})

	dimStyle = lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#999999", Dark: "#666666"})

	statusWorkingStyle = lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#2d7d46", Dark: "#4ec969"})

	statusIdleStyle = lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#b8860b", Dark: "#f0c040"})

	attentionRowStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#993300", Dark: "#ffcc00"}).
		Background(lipgloss.AdaptiveColor{Light: "#ffe0a0", Dark: "#4a3510"})

	attentionSelectedStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#993300", Dark: "#ffffff"}).
		Background(lipgloss.AdaptiveColor{Light: "#ffd080", Dark: "#6a4a10"})

	statusNeedsInputStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#cc6600", Dark: "#ffaa33"})

	statusErrorStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#cc0000", Dark: "#ff4444"})

	footerStyle = lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#999999", Dark: "#666666"}).
		MarginTop(1)

	chatSentStyle = lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#2d5fa1", Dark: "#6699ff"})

	chatReceivedStyle = lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#333333", Dark: "#cccccc"})

	helpStyle = lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#666666", Dark: "#999999"}).
		Background(lipgloss.AdaptiveColor{Light: "#f0f0f0", Dark: "#2a2a2a"}).
		Padding(1, 2)

	statusBarStyle = lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#cc6600", Dark: "#ffaa33"})

	logoStyle = lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#5555aa", Dark: "#7777cc"})

	emptyHintStyle = lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#666666", Dark: "#888888"})

	emptyKeyStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#4444aa", Dark: "#9999dd"})

	selectedTextStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#1a1a2e", Dark: "#ffffff"}).
		Background(lipgloss.AdaptiveColor{Light: "#d0d0ff", Dark: "#3a3a5c"})

	brandingStyle = lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#999999", Dark: "#555555"})

	agentClaudeStyle = lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#b07020", Dark: "#e0a050"})

	agentCodexStyle = lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#2d7d46", Dark: "#6bc98a"})

	agentOpenCodeStyle = lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#2d5fa1", Dark: "#6ca6f0"})

	agentCopilotStyle = lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#b04080", Dark: "#e070b0"})

	selectedBg = lipgloss.AdaptiveColor{Light: "#d0d0ff", Dark: "#3a3a5c"}
	useReverseSelection = false
}
