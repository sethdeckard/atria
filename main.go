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

	var backend terminal.Backend
	switch cfg.Backend {
	case "iterm2":
		it2Path, ok := iterm.Preflight(cfg.IT2Path)
		if !ok {
			os.Exit(1)
		}
		backend = iterm.NewClient(it2Path)
	default:
		backend = iterm.NewClient(cfg.IT2Path)
	}
	cached := terminal.NewCachedBackend(backend, cfg.CacheTTL)

	if err := tui.EnsureMonitorDir(cfg.MonitorDir); err != nil {
		fmt.Fprintf(os.Stderr, "monitor dir error: %v\n", err)
		os.Exit(1)
	}

	m := tui.NewModelWithConfig(cached, store, cfg.WatchDirs, cfg.MonitorDir, cfg.DefaultAgent)

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
