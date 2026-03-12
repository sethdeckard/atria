package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sethdeckard/atria/internal/config"
	"github.com/sethdeckard/atria/internal/model"
	"github.com/sethdeckard/atria/internal/terminal"
	"github.com/sethdeckard/atria/internal/terminal/iterm"
	"github.com/sethdeckard/atria/internal/terminal/kitty"
	"github.com/sethdeckard/atria/internal/terminal/tmux"
)

func checkBackend(backend terminal.Backend) tea.Cmd {
	return func() tea.Msg {
		err := backend.Available()
		return BackendAvailableMsg{Err: err}
	}
}

func refreshSessions(backend terminal.Backend) tea.Cmd {
	return func() tea.Msg {
		sessions, err := backend.ListSessions()
		return SessionsRefreshedMsg{Sessions: sessions, Err: err}
	}
}

func launchAgent(backend terminal.Backend, projectDir string, agentType model.AgentType) tea.Cmd {
	return func() tea.Msg {
		sessionID, err := backend.NewSession()
		if err != nil {
			return AgentLaunchedMsg{ProjectDir: projectDir, Err: err}
		}
		time.Sleep(300 * time.Millisecond)
		cmd := "claude"
		switch agentType {
		case model.AgentCodex:
			cmd = "codex"
		case model.AgentOpenCode:
			cmd = "opencode"
		}
		shellCmd := fmt.Sprintf("cd %s && %s", shellEscape(projectDir), cmd)
		err = backend.RunCommand(sessionID, shellCmd)
		if err != nil {
			return AgentLaunchedMsg{ProjectDir: projectDir, Err: err}
		}
		return AgentLaunchedMsg{
			ProjectDir: projectDir,
			SessionID:  sessionID,
			AgentType:  agentType,
		}
	}
}

func sendPrompt(backend terminal.Backend, sessionID, text string, projectDir string) tea.Cmd {
	return func() tea.Msg {
		err := backend.SendText(sessionID, text)
		if err != nil {
			return PromptSentMsg{ProjectDir: projectDir, Err: err}
		}
		time.Sleep(50 * time.Millisecond)
		err = backend.SendText(sessionID, "\r")
		return PromptSentMsg{ProjectDir: projectDir, Err: err}
	}
}

func focusSession(backend terminal.Backend, sessionID string) tea.Cmd {
	return func() tea.Msg {
		err := backend.FocusSession(sessionID)
		return FocusedMsg{Err: err}
	}
}

func startMonitor(backend terminal.Backend, sessionID, logPath, patterns string, projectDir string) tea.Cmd {
	return func() tea.Msg {
		pid, err := backend.MonitorOutput(sessionID, logPath, patterns)
		return MonitorStartedMsg{
			ProjectDir: projectDir,
			PID:        pid,
			LogPath:    logPath,
			Err:        err,
		}
	}
}

func readScreen(backend terminal.Backend, sessionID, projectDir string) tea.Cmd {
	return func() tea.Msg {
		content, err := backend.ReadScreen(sessionID, 25)
		return ScreenReadMsg{
			SessionID:  sessionID,
			ProjectDir: projectDir,
			Content:    content,
			Err:        err,
		}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return TickMsg{}
	})
}

func spinnerTickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return SpinnerTickMsg{}
	})
}

func bellCmd() tea.Cmd {
	return func() tea.Msg {
		if tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0); err == nil {
			tty.Write([]byte("\a"))
			tty.Close()
		}
		return StatusMsg{Text: "⚠ Agent needs input"}
	}
}

func listDir(path string) tea.Cmd {
	return func() tea.Msg {
		var dirs []DirBrowserItem
		// ".." unless at root
		if filepath.Dir(path) != path {
			dirs = append(dirs, DirBrowserItem{
				Path:     filepath.Dir(path),
				Name:     "..",
				IsParent: true,
			})
		}
		entries, err := os.ReadDir(path)
		if err == nil {
			for _, e := range entries {
				if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
					continue
				}
				dirs = append(dirs, DirBrowserItem{
					Path: filepath.Join(path, e.Name()),
					Name: e.Name(),
				})
			}
		}
		return DirBrowserMsg{Dirs: dirs, CurrentDir: path}
	}
}

// integrationMeta maps a config name (e.g. "iterm2") to the prefix and source
// used by the composite backend. These must match the values used at startup.
func integrationMeta(name string) (prefix, source string) {
	switch name {
	case "iterm2":
		return "iterm:", "iterm"
	case "tmux":
		return "tmux:", "tmux"
	case "kitty":
		return "kitty:", "kitty"
	default:
		return name + ":", name
	}
}

func toggleIntegration(name string, enable bool, cfg *config.Config, configPath string, composite *terminal.CompositeBackend, ptyClient terminal.Backend) tea.Cmd {
	return func() tea.Msg {
		prefix, source := integrationMeta(name)
		status := BackendStatus{Name: name, Enabled: enable}

		if !enable {
			// Persist first — remove from config and save.
			prevIntegrations := cfg.Integrations
			filtered := make([]string, 0, len(cfg.Integrations))
			for _, n := range cfg.Integrations {
				if n != name {
					filtered = append(filtered, n)
				}
			}
			if len(filtered) == 0 {
				cfg.Integrations = nil
			} else {
				cfg.Integrations = filtered
			}

			if err := cfg.Save(configPath); err != nil {
				// Restore config on save failure.
				cfg.Integrations = prevIntegrations
				return IntegrationToggledMsg{Name: name, Status: status, Err: err}
			}

			// Save succeeded — apply runtime changes.
			var remapped map[string]string
			composite.RemoveIntegration(prefix)
			if composite.PrimarySource() == source {
				// Re-derive primary from remaining integrations.
				newPrimary, newSource := derivePrimary(composite.Integrations(), ptyClient)
				composite.SetPrimary(newPrimary, newSource)
				if newPrimary == ptyClient {
					// PTY promoted back to primary — remap pty:pty-N → pty-N.
					ptySessions, _ := ptyClient.ListSessions()
					if len(ptySessions) > 0 {
						remapped = make(map[string]string, len(ptySessions))
						for _, s := range ptySessions {
							remapped["pty:"+s.ID] = s.ID
						}
					}
					composite.RemoveIntegration("pty:")
				}
			}

			return IntegrationToggledMsg{Name: name, Status: status, RemappedIDs: remapped}
		}

		// Persist first — add to config and save.
		prevIntegrations := cfg.Integrations
		if !containsString(cfg.Integrations, name) {
			cfg.Integrations = append(cfg.Integrations, name)
		}

		if err := cfg.Save(configPath); err != nil {
			cfg.Integrations = prevIntegrations
			return IntegrationToggledMsg{Name: name, Status: status, Err: err}
		}

		// Probe the backend.
		var backend terminal.Backend
		var probeErr error

		switch name {
		case "iterm2":
			it := iterm.NewClient()
			it.SetNoPrompt(true) // suppress AppleScript dialogs during TUI
			probeErr = it.Available()
			backend = it
		case "tmux":
			tm := tmux.NewClient(cfg.TmuxPath, cfg.TmuxSession)
			probeErr = tm.Available()
			backend = tm
		case "kitty":
			kt := kitty.NewClient(cfg.KittenPath)
			probeErr = kt.Available()
			backend = kt
		}

		if probeErr != nil {
			status.Reason = probeErr.Error()
			// Config saved (toggle remembered) but no runtime changes.
			return IntegrationToggledMsg{Name: name, Status: status}
		}

		// Save succeeded and probe OK — apply runtime changes.
		composite.AddIntegration(terminal.Integration{
			Prefix:  prefix,
			Source:  source,
			Backend: backend,
		})

		// Mark active only when the environment matches.
		if (name == "iterm2" && os.Getenv("TERM_PROGRAM") == "iTerm.app") ||
			(name == "tmux" && os.Getenv("TMUX") != "") ||
			(name == "kitty" && os.Getenv("KITTY_WINDOW_ID") != "") {
			status.Active = true
		}

		// Re-derive primary based on environment.
		// When PTY is demoted from primary to integration, existing PTY
		// session IDs change from "pty-N" to "pty:pty-N". Build a remap
		// so the store can migrate tracked sessions.
		var remapped map[string]string
		demotePTY := func() {
			ptySessions, _ := ptyClient.ListSessions()
			if len(ptySessions) > 0 {
				remapped = make(map[string]string, len(ptySessions))
				for _, s := range ptySessions {
					remapped[s.ID] = "pty:" + s.ID
				}
			}
			composite.AddIntegration(terminal.Integration{
				Prefix: "pty:", Source: "pty", Backend: ptyClient,
			})
		}

		if name == "tmux" && os.Getenv("TMUX") != "" {
			if composite.PrimarySource() == "pty" {
				demotePTY()
			}
			composite.SetPrimary(backend, "tmux")
			status.Launch = true
		} else if name == "kitty" && os.Getenv("KITTY_WINDOW_ID") != "" && composite.PrimarySource() != "tmux" {
			if composite.PrimarySource() == "pty" {
				demotePTY()
			}
			composite.SetPrimary(backend, "kitty")
			status.Launch = true
		} else if name == "iterm2" && os.Getenv("TERM_PROGRAM") == "iTerm.app" && composite.PrimarySource() == "pty" {
			demotePTY()
			composite.SetPrimary(backend, "iterm")
			status.Launch = true
		}

		return IntegrationToggledMsg{Name: name, Status: status, RemappedIDs: remapped}
	}
}

func saveConfig(cfg *config.Config, path string, rollback func(m *Model)) tea.Cmd {
	return func() tea.Msg {
		err := cfg.Save(path)
		return ConfigSavedMsg{Err: err, Rollback: rollback}
	}
}

// derivePrimary selects the best launch backend from available integrations,
// following documented precedence: tmux (if in tmux) > iterm (if in iTerm) > PTY.
func derivePrimary(integrations []terminal.Integration, ptyClient terminal.Backend) (terminal.Backend, string) {
	integMap := make(map[string]terminal.Backend)
	for _, integ := range integrations {
		integMap[integ.Source] = integ.Backend
	}
	if b, ok := integMap["tmux"]; ok && os.Getenv("TMUX") != "" {
		return b, "tmux"
	}
	if b, ok := integMap["kitty"]; ok && os.Getenv("KITTY_WINDOW_ID") != "" {
		return b, "kitty"
	}
	if b, ok := integMap["iterm"]; ok && os.Getenv("TERM_PROGRAM") == "iTerm.app" {
		return b, "iterm"
	}
	return ptyClient, "pty"
}

func containsString(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func discoverAgent(backend terminal.Backend, sess terminal.Session, agentType model.AgentType, watchDirs []string, projectDirs []string) tea.Cmd {
	return func() tea.Msg {
		dir := terminal.DiscoverCWD(backend, sess, watchDirs, projectDirs)
		return AgentDiscoveredMsg{
			SessionID: sess.ID,
			AgentType: agentType,
			Dir:       dir,
		}
	}
}

func shellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
