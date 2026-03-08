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
		backend = iterm.NewClient(cfg.IT2Path)
	default:
		backend = iterm.NewClient(cfg.IT2Path)
	}
	cached := terminal.NewCachedBackend(backend, cfg.CacheTTL)

	if err := tui.EnsureMonitorDir(cfg.MonitorDir); err != nil {
		fmt.Fprintf(os.Stderr, "monitor dir error: %v\n", err)
		os.Exit(1)
	}

	m := tui.NewModel(cached, store, cfg.WatchDirs, cfg.MonitorDir)

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
