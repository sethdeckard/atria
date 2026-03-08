# Atria - Agent Conventions

## Project Overview

Atria is a Go TUI tool for managing multiple AI coding agents (Claude Code, Codex) running in terminal tabs/panes. Built with Bubble Tea (bubbletea, lipgloss, bubbles).

## Code Style

- Idiomatic Go: follow Effective Go patterns, standard naming conventions
- Format all code with `gofmt` / `goimports`
- Error handling: return errors, don't panic. Wrap errors with context using `fmt.Errorf("context: %w", err)`
- No `log.Fatal` in library code (internal packages). Only `main.go` may exit the process

## Project Structure

```
main.go                          # Entry point
internal/
  config/config.go               # TOML config parsing
  model/types.go                 # Domain types (Project, AgentSession, etc.)
  model/store.go                 # In-memory store + JSON persistence
  terminal/backend.go            # Backend interface
  terminal/detect.go             # Agent detection from session name
  terminal/monitor.go            # Log reading + status classification
  terminal/cache.go              # Cached session list with TTL
  terminal/iterm/client.go       # iTerm2 backend (it2 CLI wrapper)
  terminal/iterm/cwd.go          # CWD discovery strategies
  project/discover.go            # Watch dir scanning
  tui/app.go                     # Root Bubble Tea model
  tui/messages.go                # tea.Msg types
  tui/commands.go                # tea.Cmd factories
  tui/keys.go                    # Key bindings
  tui/styles.go                  # Lip Gloss styles
  tui/projectlist.go             # Project list view
  tui/chatview.go                # Chat/send view
```

## Testing

- Use `_test.go` files in the same package
- Table-driven tests for logic (detection, classification, parsing)
- Use `t.TempDir()` for filesystem tests
- Mock the `terminal.Backend` interface for tests that need terminal operations
- Run: `go test ./...`

## Architecture Rules

- All terminal backend calls must happen in `tea.Cmd` functions (never in Update/View)
- TUI Model owns all state; terminal/project packages are stateless
- Use `tea.Batch` for parallel commands, sequential when dependencies exist
- Monitor processes are OS-level (spawned via os/exec), not goroutines

## Key Patterns

- Two-step send for raw-mode TUI agents: `SendText(text)`, 50ms sleep, `SendText("\r")`
- Session list cached with 5s TTL, invalidated after mutations
- Agent detection: `\u2733` prefix or "claude" -> Claude; "codex" -> Codex
- Config uses TOML at `~/.config/atria/config.toml`
