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
	"github.com/sethdeckard/atria/internal/tui"
	"github.com/sethdeckard/atria/libatria"
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

	if config.NormalizeTheme(cfg.Theme) == config.ThemeANSI {
		tui.ApplyANSITheme()
	}

	configPath := config.DefaultPath()
	store := model.NewStore(cfg.DataDir)
	if err := store.LoadProjects(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: loading projects: %v\n", err)
	}
	stackOpts := tui.StackOptions(cfg)
	// Startup runs before the alt screen, so an AppleScript auth dialog
	// inside iTerm2 is safe here and nowhere else.
	stackOpts.AllowITermPrompt = true
	stack, err := libatria.Open(stackOpts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "backend error: %v\n", err)
		os.Exit(1)
	}
	for _, name := range stack.Ignored() {
		fmt.Fprintf(os.Stderr, "unknown integration: %s\n", name)
	}

	statusInfo := tui.StatusInfo{
		Backends:   backendStatuses(cfg.Integrations, stack.Statuses()),
		ConfigPath: configPath,
	}

	if err := tui.EnsureMonitorDir(cfg.MonitorDir); err != nil {
		fmt.Fprintf(os.Stderr, "monitor dir error: %v\n", err)
		os.Exit(1)
	}

	m := tui.NewModelWithConfig(stack.Backend(), store, cfg.WatchDirs, cfg.MonitorDir, cfg.DefaultAgent, cfg.LaunchDir)
	m.SetStatusInfo(statusInfo)
	m.SetConfig(cfg, configPath)
	m.SetStack(stack)

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

// backendStatuses orders statuses for the settings screen: PTY, the
// configured integrations in config order, then the remaining integrations.
func backendStatuses(configured []string, statuses []libatria.Status) []tui.BackendStatus {
	byName := make(map[string]libatria.Status, len(statuses))
	for _, st := range statuses {
		byName[st.Name] = st
	}
	var out []tui.BackendStatus
	seen := make(map[string]bool)
	add := func(name string) {
		if st, ok := byName[name]; ok && !seen[name] {
			seen[name] = true
			out = append(out, tui.BackendStatusFrom(st))
		}
	}
	add(libatria.PTY)
	for _, name := range configured {
		add(name)
	}
	for _, name := range []string{libatria.ITerm2, libatria.Tmux, libatria.Kitty, libatria.WezTerm, libatria.DeviceTerm} {
		add(name)
	}
	return out
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
	return tui.Logo + "\n\n" + fmt.Sprintf(`atria - Agent multiplexer for your terminal

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
