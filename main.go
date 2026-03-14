package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sethdeckard/atria/internal/config"
	"github.com/sethdeckard/atria/internal/model"
	"github.com/sethdeckard/atria/internal/terminal"
	"github.com/sethdeckard/atria/internal/terminal/iterm"
	"github.com/sethdeckard/atria/internal/terminal/kitty"
	ptybackend "github.com/sethdeckard/atria/internal/terminal/pty"
	"github.com/sethdeckard/atria/internal/terminal/tmux"
	weztermbackend "github.com/sethdeckard/atria/internal/terminal/wezterm"
	"github.com/sethdeckard/atria/internal/tui"
)

var (
	version = "dev"
	commit  = ""
	date    = ""
)

func main() {
	tui.Version = version
	opts, err := parseOptions(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n\n%s", err, helpText())
		os.Exit(2)
	}
	if opts.showHelp {
		fmt.Print(helpText())
		return
	}
	if opts.showVersion {
		fmt.Print(versionText())
		return
	}

	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	configPath := config.DefaultPath()
	store := model.NewStore(cfg.DataDir)
	if err := store.LoadProjects(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: loading projects: %v\n", err)
	}
	// Always create PTY as the fallback.
	ptyClient := ptybackend.NewClient(cfg.PtyCols, cfg.PtyRows)

	// Build backend status info for settings screen.
	// All known integrations are listed; only configured ones are probed.
	backendStatuses := []tui.BackendStatus{
		{Name: "pty", Enabled: true, Active: true},
	}

	// Probe configured integrations.
	var integrations []terminal.Integration
	availableIntegrations := make(map[string]terminal.Backend)
	configuredSet := make(map[string]bool)
	for _, name := range cfg.Integrations {
		configuredSet[name] = true
		switch name {
		case "iterm2":
			bs := tui.BackendStatus{Name: "iterm2", Enabled: true}
			it := iterm.NewClient()
			if os.Getenv("TERM_PROGRAM") != "iTerm.app" {
				it.SetNoPrompt(true) // passive discovery only outside iTerm2
			}
			if err := it.Available(); err != nil {
				bs.Reason = err.Error()
				backendStatuses = append(backendStatuses, bs)
				continue
			}
			availableIntegrations["iterm2"] = it
			integrations = append(integrations, terminal.Integration{
				Prefix: "iterm:", Source: "iterm", Backend: it,
			})
			if os.Getenv("TERM_PROGRAM") == "iTerm.app" {
				bs.Active = true
			}
			backendStatuses = append(backendStatuses, bs)
		case "tmux":
			bs := tui.BackendStatus{Name: "tmux", Enabled: true}
			tm := tmux.NewClient(cfg.TmuxPath, cfg.TmuxSession)
			if err := tm.Available(); err != nil {
				bs.Reason = err.Error()
				backendStatuses = append(backendStatuses, bs)
				continue
			}
			availableIntegrations["tmux"] = tm
			integrations = append(integrations, terminal.Integration{
				Prefix: "tmux:", Source: "tmux", Backend: tm,
			})
			if os.Getenv("TMUX") != "" {
				bs.Active = true
			}
			backendStatuses = append(backendStatuses, bs)
		case "kitty":
			bs := tui.BackendStatus{Name: "kitty", Enabled: true}
			kt := kitty.NewClient(cfg.KittenPath)
			if err := kt.Available(); err != nil {
				bs.Reason = err.Error()
				backendStatuses = append(backendStatuses, bs)
				continue
			}
			availableIntegrations["kitty"] = kt
			integrations = append(integrations, terminal.Integration{
				Prefix: "kitty:", Source: "kitty", Backend: kt,
			})
			if os.Getenv("KITTY_WINDOW_ID") != "" {
				bs.Active = true
			}
			backendStatuses = append(backendStatuses, bs)
		case "wezterm":
			bs := tui.BackendStatus{Name: "wezterm", Enabled: true}
			wt := weztermbackend.NewClient(cfg.WezTermPath)
			if err := wt.Available(); err != nil {
				bs.Reason = err.Error()
				backendStatuses = append(backendStatuses, bs)
				continue
			}
			availableIntegrations["wezterm"] = wt
			integrations = append(integrations, terminal.Integration{
				Prefix: "wezterm:", Source: "wezterm", Backend: wt,
			})
			if os.Getenv("TERM_PROGRAM") == "WezTerm" || os.Getenv("WEZTERM_UNIX_SOCKET") != "" {
				bs.Active = true
			}
			backendStatuses = append(backendStatuses, bs)
		default:
			fmt.Fprintf(os.Stderr, "unknown integration: %s\n", name)
		}
	}

	// Add unconfigured integrations as disabled entries.
	if !configuredSet["iterm2"] {
		backendStatuses = append(backendStatuses, tui.BackendStatus{Name: "iterm2"})
	}
	if !configuredSet["tmux"] {
		backendStatuses = append(backendStatuses, tui.BackendStatus{Name: "tmux"})
	}
	if !configuredSet["kitty"] {
		backendStatuses = append(backendStatuses, tui.BackendStatus{Name: "kitty"})
	}
	if !configuredSet["wezterm"] {
		backendStatuses = append(backendStatuses, tui.BackendStatus{Name: "wezterm"})
	}

	// Derive launch target from environment + available integrations.
	// Prefer tmux (most specific), then iTerm, then PTY.
	var primary terminal.Backend = ptyClient
	primarySource := "pty"
	if b, ok := availableIntegrations["tmux"]; ok && os.Getenv("TMUX") != "" {
		primary = b
		primarySource = "tmux"
	} else if b, ok := availableIntegrations["kitty"]; ok && os.Getenv("KITTY_WINDOW_ID") != "" {
		primary = b
		primarySource = "kitty"
	} else if b, ok := availableIntegrations["wezterm"]; ok && (os.Getenv("TERM_PROGRAM") == "WezTerm" || os.Getenv("WEZTERM_UNIX_SOCKET") != "") {
		primary = b
		primarySource = "wezterm"
	} else if b, ok := availableIntegrations["iterm2"]; ok && os.Getenv("TERM_PROGRAM") == "iTerm.app" {
		primary = b
		primarySource = "iterm"
	}

	// Mark launch targets in status info.
	for i, bs := range backendStatuses {
		if bs.Active && tui.MatchesPrimarySource(bs, primarySource) {
			backendStatuses[i].Launch = true
		}
	}

	// When primary is non-PTY, add PTY as an integration so its
	// sessions remain discoverable and routable.
	if primary != ptyClient {
		integrations = append(integrations, terminal.Integration{
			Prefix: "pty:", Source: "pty", Backend: ptyClient,
		})
	}

	backend := terminal.NewCompositeBackend(primary, primarySource, integrations)
	cached := terminal.NewCachedBackend(backend, cfg.CacheTTL)

	statusInfo := tui.StatusInfo{
		Backends:   backendStatuses,
		ConfigPath: configPath,
	}

	if err := tui.EnsureMonitorDir(cfg.MonitorDir); err != nil {
		fmt.Fprintf(os.Stderr, "monitor dir error: %v\n", err)
		os.Exit(1)
	}

	m := tui.NewModelWithConfig(cached, store, cfg.WatchDirs, cfg.MonitorDir, cfg.DefaultAgent, cfg.LaunchDir)
	m.SetStatusInfo(statusInfo)
	m.SetConfig(cfg, configPath)
	m.SetPTYClient(ptyClient)

	if opts.debug {
		if err := m.EnableDebugLog(debugLogPath(cfg.DataDir), opts.debugUnsafe); err != nil {
			fmt.Fprintf(os.Stderr, "debug log error: %v\n", err)
		}
	}

	// Cleanup monitors on exit
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		m.Cleanup()
		os.Exit(0)
	}()

	p := tea.NewProgram(m, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if fm, ok := finalModel.(tui.Model); ok {
		fm.Cleanup()
	}
}

type options struct {
	debug       bool
	debugUnsafe bool
	showHelp    bool
	showVersion bool
}

func parseOptions(args []string) (options, error) {
	var opts options
	fs := flag.NewFlagSet("atria", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.BoolVar(&opts.debug, "debug", false, "")
	fs.BoolVar(&opts.debugUnsafe, "debug-unsafe", false, "")
	fs.BoolVar(&opts.showHelp, "help", false, "")
	fs.BoolVar(&opts.showHelp, "h", false, "")
	fs.BoolVar(&opts.showVersion, "version", false, "")
	fs.BoolVar(&opts.showVersion, "v", false, "")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if fs.NArg() > 0 {
		return options{}, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if opts.debugUnsafe {
		opts.debug = true
	}
	return opts, nil
}

func helpText() string {
	return fmt.Sprintf(`atria - Agent multiplexer for your terminal

Usage: atria [options]

Options:
  --debug           Log screen-read metadata to %s
  --debug-unsafe    Log full raw screen contents to %s (may capture secrets)
  --version, -v     Show version information
  --help, -h        Show this help

Config: %s

On first run, press S to open the setup wizard.
`, debugLogPath(config.DefaultDataDir), debugLogPath(config.DefaultDataDir), config.DefaultPath())
}

func versionText() string {
	var out string
	out += tui.Logo + "\n\n"
	out += fmt.Sprintf("atria v%s", version)
	if commit != "" {
		out += fmt.Sprintf(" (%s)", commit[:min(7, len(commit))])
	}
	if date != "" {
		out += fmt.Sprintf(" built %s", date)
	}
	out += "\n"
	return out
}

func debugLogPath(dataDir string) string {
	return filepath.Join(dataDir, "debug.log")
}
