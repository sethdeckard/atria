package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sethdeckard/atria/internal/config"
	"github.com/sethdeckard/atria/libatria/agent"
	"github.com/sethdeckard/atria/libatria/terminal"
	devicetermbackend "github.com/sethdeckard/atria/libatria/terminal/deviceterm"
	"github.com/sethdeckard/atria/libatria/terminal/iterm"
	"github.com/sethdeckard/atria/libatria/terminal/kitty"
	"github.com/sethdeckard/atria/libatria/terminal/tmux"
	weztermbackend "github.com/sethdeckard/atria/libatria/terminal/wezterm"
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
		if cb, ok := backend.(*terminal.CachedBackend); ok {
			if comp, ok := cb.Inner().(*terminal.CompositeBackend); ok {
				source = comp.PrimarySource()
			}
		}
	}
	return func() tea.Msg {
		var sessionID string
		var err error
		if ns, ok := backend.(terminal.SourceLauncher); ok {
			sessionID, err = ns.NewSessionOn(source)
		} else {
			sessionID, err = backend.NewSession()
		}
		if err != nil {
			return AgentLaunchedMsg{ProjectDir: projectDir, Err: err}
		}
		// Focus the new session so the terminal renders its screen buffer.
		// Without this, iTerm2 may not populate the buffer for background tabs.
		_ = backend.FocusSession(sessionID)
		time.Sleep(300 * time.Millisecond)
		cmd := string(agentType)
		shellCmd := fmt.Sprintf("cd %s && %s", shellEscape(projectDir), cmd)
		err = backend.RunCommand(sessionID, shellCmd)
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
		prompt := text
		// Copilot's input interprets Enter as newline, not submit.
		// Replace newlines with spaces to avoid triggering / command menu.
		if agentType == agent.Copilot {
			prompt = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(prompt)
			prompt = strings.TrimSpace(prompt)
		}
		// Copilot drops Enter when bulk text is sent to a short
		// terminal. Send character-by-character to match how focus
		// mode forwards keystrokes individually.
		if agentType == agent.Copilot {
			for _, r := range prompt {
				if err := backend.SendText(sessionID, string(r)); err != nil {
					return PromptSentMsg{ProjectDir: projectDir, Err: err}
				}
				time.Sleep(5 * time.Millisecond)
			}
			time.Sleep(50 * time.Millisecond)
			err := backend.SendText(sessionID, "\r")
			return PromptSentMsg{ProjectDir: projectDir, Err: err}
		}
		err := backend.SendText(sessionID, prompt)
		if err != nil {
			return PromptSentMsg{ProjectDir: projectDir, Err: err}
		}
		time.Sleep(50 * time.Millisecond)
		err = backend.SendText(sessionID, "\r")
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
	case "wezterm":
		return "wezterm:", "wezterm"
	case "deviceterm":
		return "deviceterm:", "deviceterm"
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
			filtered := removeString(cfg.Integrations, name)
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
			var remap *SourceRemap
			composite.RemoveIntegration(prefix)
			if composite.PrimarySource() == source {
				// Re-derive primary from remaining integrations.
				newPrimary, newSource := derivePrimary(composite.Integrations(), ptyClient)
				composite.SetPrimary(newPrimary, newSource)
				remap = promoteRemap(composite, newSource)
			}

			return IntegrationToggledMsg{Name: name, Status: status, Remap: remap, NewPrimary: composite.PrimarySource()}
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
			// Never prompt from inside the TUI: an AppleScript dialog over the
			// alt screen is what NoPrompt exists to prevent.
			it := iterm.NewClient(iterm.Options{NoPrompt: true, ClientName: "atria"})
			probeErr = it.Available()
			backend = it
		case "tmux":
			tm := tmux.NewClient(tmux.Options{Path: cfg.TmuxPath, LaunchSession: cfg.TmuxSession, FallbackSession: "atria"})
			probeErr = tm.Available()
			backend = tm
		case "kitty":
			kt := kitty.NewClient(kitty.Options{Path: cfg.KittenPath})
			probeErr = kt.Available()
			backend = kt
		case "wezterm":
			wt := weztermbackend.NewClient(weztermbackend.Options{Path: cfg.WezTermPath})
			probeErr = wt.Available()
			backend = wt
		case "deviceterm":
			dt := devicetermbackend.NewClient(devicetermbackend.Options{Path: cfg.DeviceTermPath, ProgramName: "atria"})
			probeErr = dt.Available()
			backend = dt
		}

		if probeErr != nil {
			status.Reason = probeErr.Error()
			// Config saved (toggle remembered) but no runtime changes.
			return IntegrationToggledMsg{Name: name, Status: status}
		}

		// Save succeeded and probe OK — apply runtime changes.
		// DeviceTerm participates only as a primary candidate; promotion
		// happens below.
		if name != "deviceterm" {
			composite.AddIntegration(terminal.Integration{
				Prefix:  prefix,
				Source:  source,
				Backend: backend,
			})
		}

		// Mark active only when the environment matches.
		if (name == "iterm2" && os.Getenv("TERM_PROGRAM") == "iTerm.app") ||
			(name == "tmux" && os.Getenv("TMUX") != "") ||
			(name == "kitty" && os.Getenv("KITTY_WINDOW_ID") != "") ||
			(name == "wezterm" && (os.Getenv("TERM_PROGRAM") == "WezTerm" || os.Getenv("WEZTERM_UNIX_SOCKET") != "")) {
			status.Active = true
		}

		// Promote using the same precedence startup applies, so the launch
		// target does not depend on the order integrations were toggled.
		// The outgoing primary's sessions change id when it becomes an
		// integration; the handler rewrites tracked ids from the remap.
		var remap *SourceRemap
		if outranksPrimary(name, composite.PrimarySource()) {
			remap = demoteRemap(composite, ptyClient)
			composite.SetPrimary(backend, source)
			if name == "deviceterm" {
				// Active only once it is the primary (never a secondary discoverer).
				status.Active = true
			}
		}

		return IntegrationToggledMsg{Name: name, Status: status, Remap: remap, NewPrimary: composite.PrimarySource()}
	}
}

func saveConfig(cfg *config.Config, path string, rollback func(m *Model)) tea.Cmd {
	return func() tea.Msg {
		err := cfg.Save(path)
		return ConfigSavedMsg{Err: err, Rollback: rollback}
	}
}

// integrationPrefix returns the prefix that serves source in the
// integration role. PTY has no standing entry, so it is answered directly.
func integrationPrefix(composite *terminal.CompositeBackend, source string) (string, bool) {
	if source == "pty" {
		return "pty:", true
	}
	for _, integ := range composite.Integrations() {
		if integ.Source == source {
			return integ.Prefix, true
		}
	}
	return "", false
}

// demoteRemap prepares the current primary for demotion and describes the
// resulting session id change; callers change the primary afterwards with
// SetPrimary. PTY gains the integration entry it lacks; every other backend
// keeps the entry it already has, and the composite dedups by TTY. A
// primary with no entry (DeviceTerm) cannot be routed afterwards, so nil is
// returned and its sessions drop on refresh.
func demoteRemap(composite *terminal.CompositeBackend, ptyClient terminal.Backend) *SourceRemap {
	prev := composite.PrimarySource()
	if prev == "pty" {
		composite.AddIntegration(terminal.Integration{
			Prefix: "pty:", Source: "pty", Backend: ptyClient,
		})
	}
	prefix, ok := integrationPrefix(composite, prev)
	if !ok {
		return nil
	}
	return &SourceRemap{Source: prev, Prefix: prefix, ToPrefixed: true}
}

// promoteRemap is the reverse: the backend named by source has just become
// primary, so its tracked sessions lose their prefix. PTY's entry is
// detached (its sessions are unprefixed only while it is primary, and
// detaching rather than removing keeps the client open); other backends
// keep their entry.
func promoteRemap(composite *terminal.CompositeBackend, source string) *SourceRemap {
	prefix, ok := integrationPrefix(composite, source)
	if !ok {
		return nil
	}
	if source == "pty" {
		composite.DetachIntegration("pty:")
	}
	return &SourceRemap{Source: source, Prefix: prefix, ToPrefixed: false}
}

// primaryRank orders launch backends by composite source name, highest
// first: deviceterm > tmux > kitty > wezterm > iterm > pty. It is the single
// precedence that startup (main.go) and the settings toggle both follow.
func primaryRank(source string) int {
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

// outranksPrimary reports whether the integration named by config name has
// a matching environment and a rank strictly above the current primary
// source. Callers must probe availability first; this checks environment
// and rank only. A lower backend never displaces a higher one, and a higher
// one always takes over, regardless of toggle order.
func outranksPrimary(name, current string) bool {
	if !envDetected(name) {
		return false
	}
	_, source := integrationMeta(name)
	return primaryRank(source) > primaryRank(current)
}

// derivePrimary selects the best launch backend from available integrations,
// following documented precedence: tmux > kitty > wezterm > iterm > PTY, each
// only when its environment matches. DeviceTerm is excluded because it is
// never a discovery integration. PTY is the fallback.
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
	if b, ok := integMap["wezterm"]; ok && (os.Getenv("TERM_PROGRAM") == "WezTerm" || os.Getenv("WEZTERM_UNIX_SOCKET") != "") {
		return b, "wezterm"
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

func removeString(ss []string, s string) []string {
	filtered := make([]string, 0, len(ss))
	for _, v := range ss {
		if v != s {
			filtered = append(filtered, v)
		}
	}
	return filtered
}

func discoverAgent(backend terminal.Backend, sess terminal.Session, watchDirs []string, projectDirs []string) tea.Cmd {
	return func() tea.Msg {
		dir := terminal.DiscoverCWD(backend, sess, watchDirs, projectDirs)
		agentType := agent.Detect(sess.Name)
		debugSkip := ""
		if agentType == "" {
			if dir == "" {
				debugSkip = "unknown title and empty dir"
			} else {
				content, err := backend.ReadScreen(sess.ID, defaultScreenReadLines)
				if err != nil {
					debugSkip = "screen read failed: " + err.Error()
				} else {
					agentType = agent.InferFromScreen(content)
					if agentType == "" {
						debugSkip = "unknown title and screen"
					}
				}
			}
		}
		return AgentDiscoveredMsg{
			SessionID: sess.ID,
			AgentType: agentType,
			Source:    sess.Source,
			Dir:       dir,
			DebugSkip: debugSkip,
		}
	}
}

func shellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// sanitizeForPath replaces characters unsafe for filenames with underscores.
func sanitizeForPath(s string) string {
	return strings.NewReplacer(":", "_", "/", "_", "\\", "_").Replace(s)
}
