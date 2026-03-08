package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sethdeckard/atria/internal/model"
	"github.com/sethdeckard/atria/internal/terminal"
	"github.com/sethdeckard/atria/internal/terminal/iterm"
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
		if agentType == model.AgentCodex {
			cmd = "codex"
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

func discoverAgent(backend terminal.Backend, sess terminal.Session, agentType model.AgentType, watchDirs []string, projectDirs []string) tea.Cmd {
	return func() tea.Msg {
		dir := iterm.DiscoverCWD(backend, sess, watchDirs, projectDirs)
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
