package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sethdeckard/atria/internal/config"
	"github.com/sethdeckard/atria/internal/model"
	"github.com/sethdeckard/atria/internal/terminal"
)

const setupStepCount = 3

var boldStyle = lipgloss.NewStyle().Bold(true)

func buildSetupStepItems(step int, info StatusInfo, cfg *config.Config, agents []model.AgentType) []settingsItem {
	switch step {
	case 0:
		return buildSetupIntegrationItems(info)
	case 1:
		return buildSetupWatchDirItems(cfg)
	case 2:
		return buildSetupDefaultItems(cfg, agents)
	}
	return nil
}

func buildSetupIntegrationItems(info StatusInfo) []settingsItem {
	var items []settingsItem
	for _, bs := range info.Backends {
		if bs.Name == "pty" {
			continue
		}
		statusLabel := backendStatusLabel(bs)
		label := "  " + bs.Name
		if envDetected(bs.Name) {
			label += "  (detected)"
		}
		items = append(items, settingsItem{
			section: "backends", label: label, itemType: "toggle",
			value: statusLabel, key: bs.Name,
		})
	}
	return items
}

func buildSetupWatchDirItems(cfg *config.Config) []settingsItem {
	var items []settingsItem
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
	return items
}

func buildSetupDefaultItems(cfg *config.Config, agents []model.AgentType) []settingsItem {
	var items []settingsItem

	// Default agent — one radio item per agent
	currentAgent := "claude"
	if cfg.DefaultAgent != "" {
		currentAgent = cfg.DefaultAgent
	} else if len(agents) > 0 {
		currentAgent = string(agents[0])
	}
	for _, a := range agents {
		name := string(a)
		display := agentDisplayName(a)
		indicator := "\u00b7" // ·
		if name == currentAgent {
			indicator = "\u2713" // ✓
		}
		items = append(items, settingsItem{
			section: "config", label: "  " + indicator + " " + display, itemType: "radio",
			value: name, key: "default_agent",
		})
	}

	// Tmux session name — only if tmux is enabled
	for _, integ := range cfg.Integrations {
		if integ == "tmux" {
			tmuxSession := "atria"
			if cfg.TmuxSession != "" {
				tmuxSession = cfg.TmuxSession
			}
			items = append(items, settingsItem{
				section: "config", label: "  tmux session name", itemType: "string",
				value: tmuxSession, key: "tmux_session",
			})
			break
		}
	}

	return items
}

// envDetected returns true when the environment suggests this integration is available.
func envDetected(name string) bool {
	switch name {
	case "tmux":
		return os.Getenv("TMUX") != ""
	case "iterm2":
		return os.Getenv("TERM_PROGRAM") == "iTerm.app"
	}
	return false
}

func agentDisplayName(a model.AgentType) string {
	switch a {
	case model.AgentClaude:
		return "Claude Code"
	case model.AgentCodex:
		return "Codex"
	case model.AgentOpenCode:
		return "OpenCode"
	}
	name := string(a)
	return strings.ToUpper(name[:1]) + name[1:]
}

func setupStepTitle(step int) string {
	switch step {
	case 0:
		return "integrations"
	case 1:
		return "watch directories"
	case 2:
		return "defaults"
	}
	return ""
}

func setupStepDescription(step int, cfg *config.Config) string {
	atria := boldStyle.Render("atria")
	switch step {
	case 0:
		inTmux := os.Getenv("TMUX") != ""
		inITerm := os.Getenv("TERM_PROGRAM") == "iTerm.app"

		switch {
		case inTmux && inITerm:
			return fmt.Sprintf("You're running inside tmux and iTerm2. Enabling these integrations\nlets %s discover agent sessions in your tmux windows and iTerm tabs and panes.", atria)
		case inTmux:
			return fmt.Sprintf("You're running inside tmux. Enabling the tmux integration lets\n%s discover agent sessions in your tmux windows.", atria)
		case inITerm:
			desc := fmt.Sprintf("You're running inside iTerm2. Enabling the iterm2 integration lets\n%s discover agent sessions in your iTerm tabs and panes.", atria)
			// Check if iterm2 was just toggled on and probe failed
			for _, integ := range cfg.Integrations {
				if integ == "iterm2" {
					for _, bs := range []string{} {
						_ = bs
					}
					// Check if it2 is not available via backend status
					break
				}
			}
			return desc
		default:
			return fmt.Sprintf("Integrations let %s discover agent sessions running in\nexternal terminal multiplexers like tmux or iTerm2.", atria)
		}

	case 1:
		return fmt.Sprintf("Watch directories are folders %s scans for running coding agents.", atria)

	case 2:
		return "Configure defaults. These can be changed later in settings (I)."
	}
	return ""
}

func setupStepDescriptionWithStatus(step int, cfg *config.Config, info StatusInfo) string {
	desc := setupStepDescription(step, cfg)
	if step != 0 {
		return desc
	}
	// Add restart note if iterm2 is enabled but not active (it2 not available)
	for _, bs := range info.Backends {
		if bs.Name == "iterm2" && bs.Enabled && !bs.Active && bs.Reason != "" {
			desc += "\n\nRestart Atria to complete iTerm2 setup."
			break
		}
	}
	return desc
}

func renderSetup(step int, items []settingsItem, cursor int, editing bool, editBuf string, width, _ int) string {
	var sb strings.Builder

	logo := logoStyle.Render(Logo)

	sb.WriteString(logo)
	sb.WriteString("\n\n")
	sb.WriteString(emptyHintStyle.Render("  Agent multiplexer for your terminal."))
	sb.WriteString("\n\n")

	// Separator
	sepWidth := width - 2
	if sepWidth < 1 {
		sepWidth = 1
	}
	sb.WriteString(dimStyle.Render("  " + strings.Repeat("\u2500", sepWidth)))
	sb.WriteString("\n\n")

	// Description
	sb.WriteString(dimStyle.Render("  " + strings.ReplaceAll(setupStepDescriptionPlaceholder, "\n", "\n  ")))
	sb.WriteString("\n\n")

	// Section title
	sb.WriteString(titleStyle.Render("  " + setupStepTitle(step)))
	sb.WriteString("\n")

	// Items
	for i, item := range items {
		isSelected := i == cursor

		label := item.label
		value := item.value

		// Radio items use value as internal data, not display
		if item.itemType == "radio" {
			value = ""
		}

		// Show edit buffer when editing this item
		if isSelected && editing {
			value = editBuf + "\u2588" // block cursor
		}

		labelW := lipgloss.Width(label)
		valueW := lipgloss.Width(value)
		gap := width - labelW - valueW - 4
		if gap < 2 {
			gap = 2
		}

		line := label + strings.Repeat(" ", gap) + value

		if isSelected {
			sb.WriteString(selectedStyle.Render(line))
		} else if item.itemType == "action" {
			sb.WriteString(dimStyle.Render(label))
		} else if item.itemType == "radio" && strings.Contains(label, "\u2713") {
			colored := strings.Replace(label, "\u2713", statusWorkingStyle.Render("\u2713"), 1)
			sb.WriteString(colored)
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

	// Footer
	sb.WriteString("\n")

	stepIndicator := fmt.Sprintf("(%d/%d)", step+1, setupStepCount)

	if editing {
		sb.WriteString(footerStyle.Render(fmt.Sprintf("  %s  enter: confirm  esc: cancel", stepIndicator)))
	} else {
		var hints []string
		hints = append(hints, "j/k: navigate")
		if cursor >= 0 && cursor < len(items) {
			switch items[cursor].itemType {
			case "toggle":
				hints = append(hints, "enter: toggle")
			case "radio":
				hints = append(hints, "enter: select")
			case "choice":
				hints = append(hints, "enter: toggle")
			case "string":
				hints = append(hints, "enter: edit")
			case "action":
				hints = append(hints, "enter: add")
			case "list-entry":
				hints = append(hints, "d: remove")
			}
		}
		if step < setupStepCount-1 {
			hints = append(hints, "tab: next")
		} else {
			hints = append(hints, "tab: done")
		}
		if step > 0 {
			hints = append(hints, "shift+tab: back")
		}
		hints = append(hints, "esc: skip")
		hints = append(hints, "q: exit")
		sb.WriteString(footerStyle.Render("  " + stepIndicator + "  " + strings.Join(hints, "  ")))
	}

	return sb.String()
}

// setupStepDescriptionPlaceholder is replaced at render time — this is a
// workaround because the description may contain lipgloss-styled text.
// We render via renderSetupWithDescription instead.
const setupStepDescriptionPlaceholder = "__SETUP_DESC__"

func renderSetupWithDescription(step int, items []settingsItem, cursor int, editing bool, editBuf string, info StatusInfo, cfg *config.Config, width, height int) string {
	raw := renderSetup(step, items, cursor, editing, editBuf, width, height)
	desc := setupStepDescriptionWithStatus(step, cfg, info)
	// Indent each line of description
	lines := strings.Split(desc, "\n")
	var indented []string
	for _, l := range lines {
		indented = append(indented, "  "+l)
	}
	styledDesc := dimStyle.Render(strings.Join(indented, "\n"))
	return strings.Replace(raw, dimStyle.Render("  "+setupStepDescriptionPlaceholder), styledDesc, 1)
}

func (m Model) exitSetupWizard() (Model, tea.Cmd) {
	m.settingsItems = buildSettingsItems(m.statusInfo, m.cfg, m.availableAgents)
	if m.settingsCursor >= len(m.settingsItems) {
		m.settingsCursor = len(m.settingsItems) - 1
	}
	if m.settingsCursor < 0 {
		m.settingsCursor = 0
	}
	m.view = m.setupReturnView
	return m, nil
}

func (m Model) handleSetupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.setupEditing {
		return m.handleSetupEditKey(msg)
	}

	switch {
	case key.Matches(msg, keys.Tab), key.Matches(msg, keys.Escape):
		m.setupStep++
		if m.setupStep >= setupStepCount {
			return m.exitSetupWizard()
		}
		m.setupItems = buildSetupStepItems(m.setupStep, m.statusInfo, m.cfg, m.availableAgents)
		m.setupCursor = firstSelectableItem(m.setupItems)
		return m, nil

	case key.Matches(msg, keys.ShiftTab):
		if m.setupStep > 0 {
			m.setupStep--
			m.setupItems = buildSetupStepItems(m.setupStep, m.statusInfo, m.cfg, m.availableAgents)
			m.setupCursor = firstSelectableItem(m.setupItems)
		}
		return m, nil

	case key.Matches(msg, keys.Quit):
		return m.exitSetupWizard()

	case key.Matches(msg, keys.Down):
		m.setupCursor = m.nextSetupItem(m.setupCursor, 1)
		return m, nil

	case key.Matches(msg, keys.Up):
		m.setupCursor = m.nextSetupItem(m.setupCursor, -1)
		return m, nil

	case key.Matches(msg, keys.Enter):
		if m.setupCursor < 0 || m.setupCursor >= len(m.setupItems) {
			return m, nil
		}
		item := m.setupItems[m.setupCursor]
		switch item.itemType {
		case "toggle":
			return m.toggleSetupIntegration(item)
		case "action":
			if item.key == "add_watch_dir" {
				return m.openSetupDirPicker()
			}
		case "radio":
			return m.selectSetupRadio(item)
		case "choice":
			return m.cycleSetupChoice(item)
		case "string":
			m.setupEditing = true
			m.setupEditBuf = item.value
			return m, nil
		}
		return m, nil

	case key.Matches(msg, keys.Add):
		if m.setupStep == 1 {
			return m.openSetupDirPicker()
		}
		return m, nil

	case key.Matches(msg, keys.Delete):
		if m.setupCursor < 0 || m.setupCursor >= len(m.setupItems) {
			return m, nil
		}
		item := m.setupItems[m.setupCursor]
		if item.itemType == "list-entry" && item.key == "watch_dirs" {
			prevWatchDirs := make([]string, len(m.cfg.WatchDirs))
			copy(prevWatchDirs, m.cfg.WatchDirs)
			dir := item.value
			filtered := make([]string, 0, len(m.cfg.WatchDirs))
			for _, d := range m.cfg.WatchDirs {
				if d != dir {
					filtered = append(filtered, d)
				}
			}
			m.cfg.WatchDirs = filtered
			m.watchDirs = m.cfg.WatchDirs
			m.setupItems = buildSetupStepItems(m.setupStep, m.statusInfo, m.cfg, m.availableAgents)
			if m.setupCursor >= len(m.setupItems) {
				m.setupCursor = len(m.setupItems) - 1
			}
			for m.setupCursor < len(m.setupItems) && m.setupItems[m.setupCursor].itemType == "header" {
				m.setupCursor++
			}
			return m, saveConfig(m.cfg, m.configPath, func(rm *Model) {
				rm.cfg.WatchDirs = prevWatchDirs
				rm.watchDirs = prevWatchDirs
			})
		}
		return m, nil
	}
	return m, nil
}

func (m Model) handleSetupEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEscape:
		m.setupEditing = false
		m.setupEditBuf = ""
		return m, nil

	case tea.KeyEnter:
		if m.setupCursor < 0 || m.setupCursor >= len(m.setupItems) {
			m.setupEditing = false
			m.setupEditBuf = ""
			return m, nil
		}
		item := m.setupItems[m.setupCursor]
		m.setupEditing = false
		val := m.setupEditBuf
		m.setupEditBuf = ""

		prevTmuxSession := m.cfg.TmuxSession

		switch item.key {
		case "tmux_session":
			if val != "" {
				m.cfg.TmuxSession = val
			}
		}

		m.setupItems = buildSetupStepItems(m.setupStep, m.statusInfo, m.cfg, m.availableAgents)
		return m, saveConfig(m.cfg, m.configPath, func(rm *Model) {
			rm.cfg.TmuxSession = prevTmuxSession
		})

	case tea.KeyBackspace:
		if len(m.setupEditBuf) > 0 {
			m.setupEditBuf = m.setupEditBuf[:len(m.setupEditBuf)-1]
		}
		return m, nil

	default:
		if len(msg.Runes) > 0 {
			m.setupEditBuf += string(msg.Runes)
		}
		return m, nil
	}
}

func (m Model) selectSetupRadio(item settingsItem) (Model, tea.Cmd) {
	if item.key != "default_agent" {
		return m, nil
	}
	name := item.value
	prevAgent := m.defaultAgent
	prevCfgAgent := m.cfg.DefaultAgent
	m.defaultAgent = model.AgentType(name)
	m.cfg.DefaultAgent = name
	m.setupItems = buildSetupStepItems(m.setupStep, m.statusInfo, m.cfg, m.availableAgents)
	return m, saveConfig(m.cfg, m.configPath, func(rm *Model) {
		rm.defaultAgent = prevAgent
		rm.cfg.DefaultAgent = prevCfgAgent
	})
}

func (m Model) cycleSetupChoice(item settingsItem) (Model, tea.Cmd) {
	if item.key != "default_agent" {
		return m, nil
	}
	agents := m.availableAgents
	if len(agents) == 0 {
		return m, nil
	}
	cur := item.value
	next := string(agents[0])
	for i, a := range agents {
		if string(a) == cur {
			next = string(agents[(i+1)%len(agents)])
			break
		}
	}
	prevAgent := m.defaultAgent
	prevCfgAgent := m.cfg.DefaultAgent
	m.defaultAgent = model.AgentType(next)
	m.cfg.DefaultAgent = next
	m.setupItems = buildSetupStepItems(m.setupStep, m.statusInfo, m.cfg, m.availableAgents)
	return m, saveConfig(m.cfg, m.configPath, func(rm *Model) {
		rm.defaultAgent = prevAgent
		rm.cfg.DefaultAgent = prevCfgAgent
	})
}

// firstSelectableItem returns the index of the first non-header item, or 0 if none.
func firstSelectableItem(items []settingsItem) int {
	for i, item := range items {
		if item.itemType != "header" {
			return i
		}
	}
	return 0
}

func (m Model) openSetupDirPicker() (Model, tea.Cmd) {
	startDir := ""
	if len(m.watchDirs) > 0 {
		startDir = m.watchDirs[0]
	}
	if startDir == "" {
		home, _ := os.UserHomeDir()
		startDir = home
	}
	m.setupDirPick = true
	return m, listDir(startDir)
}

func (m *Model) nextSetupItem(cur, dir int) int {
	n := len(m.setupItems)
	if n == 0 {
		return 0
	}
	next := cur + dir
	for next >= 0 && next < n {
		if m.setupItems[next].itemType != "header" {
			return next
		}
		next += dir
	}
	return cur
}

func (m Model) toggleSetupIntegration(item settingsItem) (Model, tea.Cmd) {
	var bs BackendStatus
	for _, b := range m.statusInfo.Backends {
		if b.Name == item.key {
			bs = b
			break
		}
	}
	enable := !bs.Enabled

	var composite *terminal.CompositeBackend
	if cb, ok := m.backend.(*terminal.CachedBackend); ok {
		composite, _ = cb.Inner().(*terminal.CompositeBackend)
	}
	if composite == nil {
		m.statusText = "Cannot modify backend"
		return m, nil
	}

	return m, toggleIntegration(item.key, enable, m.cfg, m.configPath, composite, m.ptyClient)
}
