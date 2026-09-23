package tui

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sethdeckard/atria/internal/config"
	"github.com/sethdeckard/atria/libatria"
	"github.com/sethdeckard/atria/libatria/agent"
	"github.com/sethdeckard/atria/libatria/terminal"
	"github.com/sethdeckard/atria/libatria/watch"
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

func launchAgent(backend terminal.Backend, projectDir string, agentType agent.Type, source string) tea.Cmd {
	// Resolve source eagerly so a concurrent primary change can't misclassify.
	if source == "" {
		source = "pty"
		if pr, ok := backend.(terminal.PrimaryReporter); ok {
			source = pr.PrimarySource()
		}
	}
	return func() tea.Msg {
		sessionID, err := agent.Launch(backend, source, projectDir, string(agentType))
		if err != nil {
			return AgentLaunchedMsg{ProjectDir: projectDir, Err: err}
		}
		return AgentLaunchedMsg{
			ProjectDir: projectDir,
			SessionID:  sessionID,
			AgentType:  agentType,
			Source:     source,
		}
	}
}

func sendPrompt(backend terminal.Backend, sessionID, text string, projectDir string, agentType agent.Type) tea.Cmd {
	return func() tea.Msg {
		err := agent.SendPrompt(backend, sessionID, text, agentType)
		return PromptSentMsg{ProjectDir: projectDir, Err: err}
	}
}

func sendKey(backend terminal.Backend, sessionID, text string, projectDir string) tea.Cmd {
	return func() tea.Msg {
		err := backend.SendText(sessionID, text)
		return PromptSentMsg{ProjectDir: projectDir, Err: err}
	}
}

func armQuickResponseTimeout(sessionID string) tea.Cmd {
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg {
		return QuickResponseArmExpiredMsg{SessionID: sessionID}
	})
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
			SessionID:  sessionID,
			ProjectDir: projectDir,
			PID:        pid,
			LogPath:    logPath,
			Err:        err,
		}
	}
}

// launchReadScreen polls the screen until non-blank content appears or
// maxAttempts is exhausted. This handles varying agent startup times across
// backends (PTY is instant, iTerm/Kitty may take longer).
func launchReadScreen(backend terminal.Backend, sessionID, projectDir string) tea.Cmd {
	return func() tea.Msg {
		const (
			initialDelay = 500 * time.Millisecond
			retryDelay   = 500 * time.Millisecond
			maxAttempts  = 6 // up to ~3s total
		)
		time.Sleep(initialDelay)
		for i := range maxAttempts {
			content, err := backend.ReadScreen(sessionID, 40)
			if err == nil && strings.TrimSpace(content) != "" {
				return ScreenReadMsg{
					SessionID:  sessionID,
					ProjectDir: projectDir,
					Content:    content,
				}
			}
			if i < maxAttempts-1 {
				time.Sleep(retryDelay)
			}
		}
		// Return whatever the last read produced.
		content, err := backend.ReadScreen(sessionID, 40)
		return ScreenReadMsg{
			SessionID:  sessionID,
			ProjectDir: projectDir,
			Content:    content,
			Err:        err,
		}
	}
}

func readScreenLines(backend terminal.Backend, sessionID, projectDir string, lines int) tea.Cmd {
	return func() tea.Msg {
		content, err := backend.ReadScreen(sessionID, lines)
		return ScreenReadMsg{
			SessionID:  sessionID,
			ProjectDir: projectDir,
			Content:    content,
			Err:        err,
		}
	}
}

// readScreenLinesStyled reads both the plain screen (for status classification)
// and, when the backend supports it, the styled screen (for colored display).
// Used for display-driving reads (visible chat/stream/terminal); background
// status ticks use the plain readScreenLines to avoid extra work.
func readScreenLinesStyled(backend terminal.Backend, sessionID, projectDir string, lines int) tea.Cmd {
	return func() tea.Msg {
		content, err := backend.ReadScreen(sessionID, lines)
		msg := ScreenReadMsg{
			SessionID:  sessionID,
			ProjectDir: projectDir,
			Content:    content,
			Err:        err,
		}
		if err == nil {
			if sr, ok := backend.(terminal.StyledReader); ok {
				// Mark the styled read as attempted regardless of outcome. On
				// success we adopt its content; on failure we leave it empty so
				// handleScreenRead clears any stale styled snapshot and the view
				// falls back to the fresh plain content instead of freezing.
				msg.StyledFetched = true
				if styled, serr := sr.ReadScreenStyled(sessionID, lines); serr == nil {
					msg.StyledContent = styled
				}
			}
		}
		return msg
	}
}

func discoveryTickCmd() tea.Cmd {
	return tea.Tick(discoveryRefreshInterval, func(t time.Time) tea.Msg {
		return DiscoveryTickMsg{}
	})
}

func statusTickCmd() tea.Cmd {
	return tea.Tick(backgroundActiveInterval, func(t time.Time) tea.Msg {
		return StatusTickMsg{}
	})
}

func visibleRefreshCmd(sessionID string, interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return VisibleRefreshMsg{SessionID: sessionID}
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
			tty.Write([]byte("\a")) //nolint:errcheck // best-effort bell
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

// StackOptions builds the library options atria derives from its config:
// integration list, binary paths, tmux launch session, PTY size, cache TTL,
// and the program name the terminals see. main.go adds AllowITermPrompt
// for the one moment (startup) when an AppleScript dialog is safe.
func StackOptions(cfg *config.Config) libatria.Options {
	return libatria.Options{
		Integrations:   cfg.Integrations,
		TmuxPath:       cfg.TmuxPath,
		TmuxSession:    cfg.TmuxSession,
		KittenPath:     cfg.KittenPath,
		WezTermPath:    cfg.WezTermPath,
		DeviceTermPath: cfg.DeviceTermPath,
		PTYCols:        cfg.PtyCols,
		PTYRows:        cfg.PtyRows,
		CacheTTL:       time.Duration(cfg.CacheTTL) * time.Second,
		ProgramName:    "atria",
	}
}

// toggleIntegration persists the change to config first (a save failure
// leaves the runtime untouched), then applies it to the stack. The stack's
// RoleChange becomes the message's SourceRemap so tracked session ids
// follow a backend that changed role.
func toggleIntegration(name string, enable bool, cfg *config.Config, configPath string, stack *libatria.Stack) tea.Cmd {
	return func() tea.Msg {
		status := BackendStatus{Name: name, Enabled: enable}

		if !enable {
			prevIntegrations := cfg.Integrations
			filtered := removeString(cfg.Integrations, name)
			if len(filtered) == 0 {
				cfg.Integrations = nil
			} else {
				cfg.Integrations = filtered
			}
			if err := cfg.Save(configPath); err != nil {
				cfg.Integrations = prevIntegrations
				return IntegrationToggledMsg{Name: name, Status: status, Err: err}
			}
			rc, err := stack.Disable(name)
			if err != nil {
				return IntegrationToggledMsg{Name: name, Status: status, Err: err}
			}
			return IntegrationToggledMsg{Name: name, Status: status, Remap: sourceRemap(rc), NewPrimary: stack.PrimarySource()}
		}

		prevIntegrations := cfg.Integrations
		if !containsString(cfg.Integrations, name) {
			cfg.Integrations = append(cfg.Integrations, name)
		}
		if err := cfg.Save(configPath); err != nil {
			cfg.Integrations = prevIntegrations
			return IntegrationToggledMsg{Name: name, Status: status, Err: err}
		}
		// Settings edited since startup (tmux_session, binary paths) must
		// reach the client Enable builds.
		stack.Configure(StackOptions(cfg))
		st, rc, err := stack.Enable(name)
		if err != nil {
			return IntegrationToggledMsg{Name: name, Status: status, Err: err}
		}
		status = BackendStatusFrom(st)
		if !st.Available {
			// Config saved (toggle remembered) but no runtime change, so the
			// primary and the Launch flags stand.
			return IntegrationToggledMsg{Name: name, Status: status}
		}
		return IntegrationToggledMsg{Name: name, Status: status, Remap: sourceRemap(rc), NewPrimary: stack.PrimarySource()}
	}
}

// sourceRemap converts a stack RoleChange to the handler's SourceRemap. A
// nil change, or one where no ids moved, yields nil.
func sourceRemap(rc *libatria.RoleChange) *SourceRemap {
	if rc == nil || rc.Source == "" {
		return nil
	}
	return &SourceRemap{Source: rc.Source, Prefix: rc.Prefix, ToPrefixed: rc.ToPrefixed}
}

func saveConfig(cfg *config.Config, path string, rollback func(m *Model)) tea.Cmd {
	return func() tea.Msg {
		err := cfg.Save(path)
		return ConfigSavedMsg{Err: err, Rollback: rollback}
	}
}

func containsString(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func removeString(ss []string, s string) []string {
	filtered := make([]string, 0, len(ss))
	for _, v := range ss {
		if v != s {
			filtered = append(filtered, v)
		}
	}
	return filtered
}

// discoverAgent identifies an untracked session with watch.Identify. The
// skip reason is carried for the debug log; the handler decides on Dir and
// AgentType alone.
func discoverAgent(backend terminal.Backend, sess terminal.Session, watchDirs []string, projectDirs []string) tea.Cmd {
	return func() tea.Msg {
		id := watch.Identify(backend, sess, watch.IdentifyOptions{
			WatchDirs:   watchDirs,
			ProjectDirs: projectDirs,
			ScreenLines: defaultScreenReadLines,
		})
		debugSkip := id.Skip.String()
		if id.Err != nil {
			debugSkip += ": " + id.Err.Error()
		}
		return AgentDiscoveredMsg{
			SessionID: sess.ID,
			AgentType: id.Type,
			Source:    sess.Source,
			Dir:       id.Dir,
			DebugSkip: debugSkip,
		}
	}
}

// sanitizeForPath replaces characters unsafe for filenames with underscores.
func sanitizeForPath(s string) string {
	return strings.NewReplacer(":", "_", "/", "_", "\\", "_").Replace(s)
}
