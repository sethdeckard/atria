package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sethdeckard/atria/internal/config"
	"github.com/sethdeckard/atria/internal/model"
	"github.com/sethdeckard/atria/internal/terminal"
	"github.com/sethdeckard/atria/internal/terminal/iterm"
	ptybackend "github.com/sethdeckard/atria/internal/terminal/pty"
	"github.com/sethdeckard/atria/internal/terminal/tmux"
	"github.com/sethdeckard/atria/internal/tui"
)

func main() {
	debug := false
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--uninstall-it2":
			iterm.Uninstall()
			return
		case "--debug":
			debug = true
		case "--help", "-h":
			fmt.Println("Usage: atria [options]")
			fmt.Println()
			fmt.Println("Options:")
			fmt.Println("  --debug           Log screen reads to /tmp/atria-debug.log")
			fmt.Println("  --uninstall-it2   Remove the auto-installed it2 venv")
			fmt.Println("  --help, -h        Show this help")
			return
		}
	}

	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	store := model.NewStore(cfg.DataDir)
	_ = store.LoadProjects()
	_ = store.LoadSessions()

	// Auto-detect integrations from environment when not configured.
	if cfg.Integrations == nil {
		if os.Getenv("TERM_PROGRAM") == "iTerm.app" {
			cfg.Integrations = append(cfg.Integrations, "iterm2")
		}
		if os.Getenv("TMUX") != "" {
			cfg.Integrations = append(cfg.Integrations, "tmux")
		}
	}

	// Always create PTY as the fallback.
	ptyClient := ptybackend.NewClient(cfg.PtyCols, cfg.PtyRows)

	// Probe integrations — failures are silently skipped.
	var integrations []terminal.Integration
	availableIntegrations := make(map[string]terminal.Backend)
	for _, name := range cfg.Integrations {
		switch name {
		case "iterm2":
			it2Path, ok := iterm.Preflight(cfg.IT2Path)
			if !ok {
				continue
			}
			it := iterm.NewClient(it2Path)
			if it.Available() != nil {
				continue
			}
			availableIntegrations["iterm2"] = it
			integrations = append(integrations, terminal.Integration{
				Prefix: "iterm:", Source: "iterm", Backend: it,
			})
		case "tmux":
			tm := tmux.NewClient(cfg.TmuxPath, cfg.TmuxSession)
			if tm.Available() != nil {
				continue
			}
			availableIntegrations["tmux"] = tm
			integrations = append(integrations, terminal.Integration{
				Prefix: "tmux:", Source: "tmux", Backend: tm,
			})
		default:
			fmt.Fprintf(os.Stderr, "unknown integration: %s\n", name)
		}
	}

	// Derive launch target from environment + available integrations.
	// Prefer tmux (most specific), then iTerm, then PTY.
	var primary terminal.Backend = ptyClient
	primarySource := "pty"
	if b, ok := availableIntegrations["tmux"]; ok && os.Getenv("TMUX") != "" {
		primary = b
		primarySource = "tmux"
	} else if b, ok := availableIntegrations["iterm2"]; ok && os.Getenv("TERM_PROGRAM") == "iTerm.app" {
		primary = b
		primarySource = "iterm"
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

	if err := tui.EnsureMonitorDir(cfg.MonitorDir); err != nil {
		fmt.Fprintf(os.Stderr, "monitor dir error: %v\n", err)
		os.Exit(1)
	}

	m := tui.NewModelWithConfig(cached, store, cfg.WatchDirs, cfg.MonitorDir, cfg.DefaultAgent, cfg.LaunchDir)

	if debug {
		if err := m.EnableDebugLog("/tmp/atria-debug.log"); err != nil {
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
