package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/sethdeckard/atria/internal/config"
	"github.com/sethdeckard/atria/internal/model"
)

// BackendStatus describes the state of a backend for the settings screen.
type BackendStatus struct {
	Name    string // "pty", "iterm2", "tmux"
	Enabled bool   // in integrations list
	Active  bool   // enabled + probe OK + environment matches
	Launch  bool   // is the launch target
	Reason  string // why unavailable or not active
}

// StatusInfo holds backend status information built during startup.
type StatusInfo struct {
	Backends   []BackendStatus
	ConfigPath string
}

type settingsItem struct {
	section  string // "backends", "watch_dirs", "config"
	label    string
	itemType string // "toggle", "choice", "string", "number", "list-entry", "readonly", "header"
	value    string // current display value
	key      string // config field identifier for save
}

func buildSettingsItems(info StatusInfo, cfg *config.Config, agents []model.AgentType) []settingsItem {
	var items []settingsItem

	// Section: Backends
	items = append(items, settingsItem{section: "backends", label: "integrations", itemType: "header"})
	for _, bs := range info.Backends {
		if bs.Name == "pty" {
			continue
		}
		statusLabel := backendStatusLabel(bs)
		items = append(items, settingsItem{
			section: "backends", label: "  " + bs.Name, itemType: "toggle",
			value: statusLabel, key: bs.Name,
		})
	}

	// Section: Watch directories
	items = append(items, settingsItem{section: "watch_dirs", label: "watch directories", itemType: "header"})
	for _, d := range cfg.WatchDirs {
		items = append(items, settingsItem{
			section: "watch_dirs", label: "  " + contractHome(d), itemType: "list-entry",
			value: d, key: "watch_dirs",
		})
	}

	// Section: Config
	items = append(items, settingsItem{section: "config", label: "config", itemType: "header"})

	// Default agent
	agentVal := "claude"
	if cfg.DefaultAgent != "" {
		agentVal = cfg.DefaultAgent
	} else if len(agents) > 0 {
		agentVal = string(agents[0])
	}
	items = append(items, settingsItem{
		section: "config", label: "  default agent", itemType: "choice",
		value: agentVal, key: "default_agent",
	})

	// PTY dimensions
	ptyCols := 120
	if cfg.PtyCols > 0 {
		ptyCols = cfg.PtyCols
	}
	ptyRows := 40
	if cfg.PtyRows > 0 {
		ptyRows = cfg.PtyRows
	}
	items = append(items, settingsItem{
		section: "config", label: "  PTY columns", itemType: "number",
		value: fmt.Sprintf("%d", ptyCols), key: "pty_cols",
	})
	items = append(items, settingsItem{
		section: "config", label: "  PTY rows", itemType: "number",
		value: fmt.Sprintf("%d", ptyRows), key: "pty_rows",
	})

	// tmux settings
	tmuxSession := "atria"
	if cfg.TmuxSession != "" {
		tmuxSession = cfg.TmuxSession
	}
	items = append(items, settingsItem{
		section: "config", label: "  tmux session name", itemType: "string",
		value: tmuxSession, key: "tmux_session",
	})

	return items
}

func backendStatusLabel(bs BackendStatus) string {
	if !bs.Enabled {
		return "\u00b7 disabled"
	}
	if bs.Reason != "" {
		return "\u00b7 enabled \u2014 " + bs.Reason
	}
	if !bs.Active {
		return "\u00b7 enabled"
	}
	if bs.Launch {
		return "\u2713 active \u2014 launch + discovery"
	}
	return "\u2713 active \u2014 discovery"
}

func renderSettings(items []settingsItem, cursor int, editing bool, editBuf string, width, _ int) string {
	var sb strings.Builder

	// Header: "Settings" left, "atria" branding right
	left := titleStyle.Render("  settings")
	right := brandingStyle.Render("atria  ")
	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	gap := width - leftW - rightW
	if gap < 0 {
		sb.WriteString(left)
	} else {
		sb.WriteString(left + strings.Repeat(" ", gap) + right)
	}
	sb.WriteString("\n")

	// Separator
	sepWidth := width - 2
	if sepWidth < 1 {
		sepWidth = 1
	}
	sb.WriteString(dimStyle.Render("  " + strings.Repeat("\u2500", sepWidth)))
	sb.WriteString("\n")

	for i, item := range items {
		if item.itemType == "header" {
			if i > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(titleStyle.Render("  " + item.label))
			sb.WriteString("\n")
			continue
		}

		isSelected := i == cursor

		// Build left label
		label := item.label

		// Build right value
		value := item.value

		// If editing this item, show the edit buffer
		if isSelected && editing {
			value = editBuf + "\u2588" // block cursor
		}

		// Calculate gap
		labelW := lipgloss.Width(label)
		valueW := lipgloss.Width(value)
		gap := width - labelW - valueW - 4 // 2 indent + 2 margin
		if gap < 2 {
			gap = 2
		}

		line := label + strings.Repeat(" ", gap) + value

		if isSelected {
			sb.WriteString(selectedStyle.Render(line))
		} else {
			switch {
			case strings.Contains(value, "\u2713"):
				sb.WriteString(normalStyle.Render(label) + strings.Repeat(" ", gap) + statusWorkingStyle.Render(value))
			case strings.Contains(value, "disabled") || strings.Contains(value, "unavailable"):
				sb.WriteString(normalStyle.Render(label) + strings.Repeat(" ", gap) + dimStyle.Render(value))
			default:
				sb.WriteString(normalStyle.Render(line))
			}
		}
		sb.WriteString("\n")
	}

	// Config path
	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("  config: ~/.config/atria/config.toml"))
	sb.WriteString("\n")

	// Footer — context-aware based on selected item
	sb.WriteString("\n")
	if editing {
		sb.WriteString(footerStyle.Render("  enter: confirm  esc: cancel"))
	} else {
		var hints []string
		hints = append(hints, "j/k: navigate")
		if cursor >= 0 && cursor < len(items) {
			switch items[cursor].itemType {
			case "toggle", "choice":
				hints = append(hints, "enter: toggle")
			case "string", "number":
				hints = append(hints, "enter: edit")
			case "list-entry":
				hints = append(hints, "d: remove")
			}
			if items[cursor].section == "watch_dirs" {
				hints = append(hints, "a: add")
			}
		}
		hints = append(hints, "esc: back")
		sb.WriteString(footerStyle.Render("  " + strings.Join(hints, "  ")))
	}

	return sb.String()
}
