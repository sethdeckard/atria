package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sethdeckard/atria/internal/model"
)

func TestEnableDebugLogSafeModeOmitsScreenContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logs", "debug.log")

	m := NewModel(nil, model.NewStore(t.TempDir()), nil, "")
	if err := m.EnableDebugLog(path, false); err != nil {
		t.Fatalf("EnableDebugLog: %v", err)
	}

	sess := &model.AgentSession{
		ProjectDir: "/tmp/project",
		SessionID:  "sess-1",
		Type:       model.AgentCodex,
		Status:     model.StatusIdle,
		Source:     "pty",
	}
	m.store.SetSession(sess)
	m.handleScreenRead(ScreenReadMsg{
		SessionID:  "sess-1",
		ProjectDir: "/tmp/project",
		Content:    "top secret token\n› prompt",
	})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	logText := string(data)
	if strings.Contains(logText, "top secret token") {
		t.Fatalf("expected safe debug log to omit raw screen content, got %q", logText)
	}
	if !strings.Contains(logText, `match="› prompt"`) {
		t.Fatalf("expected safe debug log metadata, got %q", logText)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected debug log mode 0600, got %o", got)
	}
}

func TestEnableDebugLogUnsafeModeIncludesScreenContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logs", "debug.log")

	m := NewModel(nil, model.NewStore(t.TempDir()), nil, "")
	if err := m.EnableDebugLog(path, true); err != nil {
		t.Fatalf("EnableDebugLog: %v", err)
	}

	sess := &model.AgentSession{
		ProjectDir: "/tmp/project",
		SessionID:  "sess-1",
		Type:       model.AgentCodex,
		Status:     model.StatusIdle,
		Source:     "pty",
	}
	m.store.SetSession(sess)
	m.handleScreenRead(ScreenReadMsg{
		SessionID:  "sess-1",
		ProjectDir: "/tmp/project",
		Content:    "top secret token\n› prompt",
	})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "top secret token") {
		t.Fatalf("expected unsafe debug log to include raw screen content, got %q", string(data))
	}
}

func TestEnsureMonitorDirUsesPrivatePermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "monitors")
	if err := EnsureMonitorDir(dir); err != nil {
		t.Fatalf("EnsureMonitorDir: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("expected monitor dir mode 0700, got %o", got)
	}
}
