package tui

import "github.com/charmbracelet/lipgloss"

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#1a1a2e", Dark: "#e0e0e0"}).
			MarginBottom(1)

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
			Foreground(lipgloss.AdaptiveColor{Light: "#666666", Dark: "#888888"})

	statusNeedsInputStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.AdaptiveColor{Light: "#cc6600", Dark: "#ffaa33"})

	statusErrorStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.AdaptiveColor{Light: "#cc0000", Dark: "#ff4444"})

	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#999999", Dark: "#666666"}).
			MarginTop(1)

	chatHeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#1a1a2e", Dark: "#e0e0e0"}).
			BorderStyle(lipgloss.NormalBorder()).
			BorderBottom(true).
			BorderForeground(lipgloss.AdaptiveColor{Light: "#cccccc", Dark: "#444444"})

	chatSentStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#2d5fa1", Dark: "#6699ff"})

	chatReceivedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#333333", Dark: "#cccccc"})

	chatInputBorderStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.NormalBorder()).
				BorderTop(true).
				BorderForeground(lipgloss.AdaptiveColor{Light: "#cccccc", Dark: "#444444"})

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#666666", Dark: "#999999"}).
			Background(lipgloss.AdaptiveColor{Light: "#f0f0f0", Dark: "#2a2a2a"}).
			Padding(1, 2)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#cc6600", Dark: "#ffaa33"})
)
