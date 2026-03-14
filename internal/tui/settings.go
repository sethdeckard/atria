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
	for _, bs := range sortedIntegrations(info.Backends) {
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
	items = append(items, settingsItem{
		section: "watch_dirs", label: "  + add directory", itemType: "action",
		key: "add_watch_dir",
	})

	// Section: Config
	items = append(items, settingsItem{section: "config", label: "config", itemType: "header"})

	// Default agent
	var agentType model.AgentType
	if cfg.DefaultAgent != "" {
		agentType = model.AgentType(cfg.DefaultAgent)
	} else if len(agents) > 0 {
		agentType = agents[0]
	} else {
		agentType = model.AgentClaude
	}
	items = append(items, settingsItem{
		section: "config", label: "  default agent", itemType: "choice",
		value: agentTypeLabel(agentType), key: "default_agent",
	})

	// PTY dimensions
	ptyCols := config.DefaultPtyCols
	if cfg.PtyCols > 0 {
		ptyCols = cfg.PtyCols
	}
	ptyRows := config.DefaultPtyRows
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
	tmuxSession := config.DefaultTmuxSession
	if cfg.TmuxSession != "" {
		tmuxSession = cfg.TmuxSession
	}
	items = append(items, settingsItem{
		section: "config", label: "  tmux session name", itemType: "string",
		value: tmuxSession, key: "tmux_session",
	})

	// update check
	updateLabel := "enabled"
	if !cfg.UpdateCheckEnabled() {
		updateLabel = "disabled"
	}
	items = append(items, settingsItem{
		section: "config", label: "  update check", itemType: "choice",
		value: updateLabel, key: "update_check",
	})

	return items
}

// sortedIntegrations returns a copy of backends with the current terminal's
// integration sorted to the top (after pty which is always first).
func sortedIntegrations(backends []BackendStatus) []BackendStatus {
	sorted := make([]BackendStatus, len(backends))
	copy(sorted, backends)
	for i := 1; i < len(sorted); i++ {
		if envDetected(sorted[i].Name) {
			// Move to position 1 (after pty at 0).
			bs := sorted[i]
			copy(sorted[2:i+1], sorted[1:i])
			sorted[1] = bs
			break
		}
	}
	return sorted
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

func nextSelectableItem(items []settingsItem, cur, dir int) int {
	n := len(items)
	if n == 0 {
		return 0
	}
	next := cur + dir
	for next >= 0 && next < n {
		if items[next].itemType != "header" {
			return next
		}
		next += dir
	}
	return cur
}

func renderSettings(items []settingsItem, cursor int, editing bool, editBuf string, width, _ int) string {
	var sb strings.Builder

	sb.WriteString(renderTitleBar("settings", width))

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
		} else if item.itemType == "action" {
			sb.WriteString(dimStyle.Render(label))
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
	sb.WriteString(dimStyle.Render("  config: ~/.config/atria/config.toml  \u00b7  v" + Version))
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
			case "action":
				hints = append(hints, "enter: add")
			case "list-entry":
				hints = append(hints, "d: remove")
			}
			if items[cursor].section == "watch_dirs" {
				hints = append(hints, "a: add")
			}
		}
		hints = append(hints, "S: setup")
		hints = append(hints, "esc: back")
		sb.WriteString(footerStyle.Render("  " + strings.Join(hints, "  ")))
	}

	return sb.String()
}
