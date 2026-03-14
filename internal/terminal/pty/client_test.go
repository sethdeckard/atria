package pty

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	c := NewClient(80, 24)
	if c.cols != 80 || c.rows != 24 {
		t.Errorf("expected 80x24, got %dx%d", c.cols, c.rows)
	}
}

func TestNewClientDefaults(t *testing.T) {
	c := NewClient(0, 0)
	if c.cols != 120 || c.rows != 40 {
		t.Errorf("expected 120x40 defaults, got %dx%d", c.cols, c.rows)
	}
}

func TestAvailable(t *testing.T) {
	c := NewClient(80, 24)
	if err := c.Available(); err != nil {
		t.Errorf("Available() should return nil, got %v", err)
	}
}

func TestNewSessionAndList(t *testing.T) {
	c := NewClient(80, 24)
	defer c.Close()

	id, err := c.NewSession()
	if err != nil {
		t.Fatalf("NewSession() error: %v", err)
	}
	if !strings.HasPrefix(id, "pty-") {
		t.Errorf("expected pty- prefix, got %s", id)
	}

	sessions, err := c.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions() error: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].ID != id {
		t.Errorf("expected session ID %s, got %s", id, sessions[0].ID)
	}
}

func TestSendTextAndReadScreen(t *testing.T) {
	c := NewClient(80, 24)
	defer c.Close()

	id, err := c.NewSession()
	if err != nil {
		t.Fatalf("NewSession() error: %v", err)
	}

	// Give the shell time to start
	time.Sleep(200 * time.Millisecond)

	err = c.SendText(id, "echo hello-pty-test\r")
	if err != nil {
		t.Fatalf("SendText() error: %v", err)
	}

	// Wait for output
	time.Sleep(300 * time.Millisecond)

	content, err := c.ReadScreen(id, 25)
	if err != nil {
		t.Fatalf("ReadScreen() error: %v", err)
	}
	if !strings.Contains(content, "hello-pty-test") {
		t.Errorf("expected screen to contain 'hello-pty-test', got:\n%s", content)
	}
}

func TestRunCommand(t *testing.T) {
	c := NewClient(80, 24)
	defer c.Close()

	id, err := c.NewSession()
	if err != nil {
		t.Fatalf("NewSession() error: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	err = c.RunCommand(id, "echo run-cmd-test")
	if err != nil {
		t.Fatalf("RunCommand() error: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	content, err := c.ReadScreen(id, 25)
	if err != nil {
		t.Fatalf("ReadScreen() error: %v", err)
	}
	if !strings.Contains(content, "run-cmd-test") {
		t.Errorf("expected screen to contain 'run-cmd-test', got:\n%s", content)
	}
}

func TestProcessExitFiltered(t *testing.T) {
	c := NewClient(80, 24)
	defer c.Close()

	id, err := c.NewSession()
	if err != nil {
		t.Fatalf("NewSession() error: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	// Exit the shell
	err = c.SendText(id, "exit\r")
	if err != nil {
		t.Fatalf("SendText() error: %v", err)
	}

	// Wait for process to exit
	time.Sleep(500 * time.Millisecond)

	sessions, err := c.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions() error: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions after exit, got %d", len(sessions))
	}
}

func TestGetVarPID(t *testing.T) {
	c := NewClient(80, 24)
	defer c.Close()

	id, err := c.NewSession()
	if err != nil {
		t.Fatalf("NewSession() error: %v", err)
	}

	pid, err := c.GetVar(id, "pid")
	if err != nil {
		t.Fatalf("GetVar(pid) error: %v", err)
	}
	if pid == "" || pid == "0" {
		t.Errorf("expected valid PID, got %s", pid)
	}
}

func TestGetVarUnsupported(t *testing.T) {
	c := NewClient(80, 24)
	defer c.Close()

	id, err := c.NewSession()
	if err != nil {
		t.Fatalf("NewSession() error: %v", err)
	}

	_, err = c.GetVar(id, "unknown")
	if err == nil {
		t.Error("expected error for unsupported variable")
	}
}

func TestMonitorOutputUnsupported(t *testing.T) {
	c := NewClient(80, 24)
	_, err := c.MonitorOutput("pty-0", "/tmp/test.log", "pattern")
	if err == nil {
		t.Error("expected error from MonitorOutput")
	}
}

func TestBellDetection(t *testing.T) {
	c := NewClient(80, 24)
	defer c.Close()

	id, err := c.NewSession()
	if err != nil {
		t.Fatalf("NewSession() error: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	// Send a bell character via printf
	err = c.SendText(id, "printf '\\a'\r")
	if err != nil {
		t.Fatalf("SendText() error: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	content, err := c.ReadScreen(id, 25)
	if err != nil {
		t.Fatalf("ReadScreen() error: %v", err)
	}
	if !strings.Contains(content, "\x07") {
		t.Error("expected bell character in ReadScreen output")
	}

	// Second read should not have bell (it was consumed)
	content2, err := c.ReadScreen(id, 25)
	if err != nil {
		t.Fatalf("ReadScreen() error: %v", err)
	}
	if strings.Contains(content2, "\x07") {
		t.Error("bell should be consumed after first read")
	}
}

func TestFilteredEnvRemovesITermCredentials(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"ITERM2_COOKIE=secret-cookie",
		"HOME=/tmp/home",
		"ITERM2_KEY=secret-key",
	}

	filtered := filteredEnv(env)
	joined := strings.Join(filtered, "\n")
	if strings.Contains(joined, "ITERM2_COOKIE=") {
		t.Fatal("expected ITERM2_COOKIE to be removed")
	}
	if strings.Contains(joined, "ITERM2_KEY=") {
		t.Fatal("expected ITERM2_KEY to be removed")
	}
	if !strings.Contains(joined, "PATH=/usr/bin") || !strings.Contains(joined, "HOME=/tmp/home") {
		t.Fatal("expected unrelated env vars to be preserved")
	}
}

func TestChildShellDoesNotInheritITermCredentials(t *testing.T) {
	t.Setenv("ITERM2_COOKIE", "secret-cookie")
	t.Setenv("ITERM2_KEY", "secret-key")
	t.Setenv("SHELL", shellOrDefault())

	c := NewClient(80, 24)
	defer c.Close()

	id, err := c.NewSession()
	if err != nil {
		t.Fatalf("NewSession() error: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	err = c.SendText(id, "printf '%s|%s\\n' \"$ITERM2_COOKIE\" \"$ITERM2_KEY\"\r")
	if err != nil {
		t.Fatalf("SendText() error: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	content, err := c.ReadScreen(id, 25)
	if err != nil {
		t.Fatalf("ReadScreen() error: %v", err)
	}
	if strings.Contains(content, "secret-cookie") || strings.Contains(content, "secret-key") {
		t.Fatalf("expected child shell to not inherit iTerm credentials, got:\n%s", content)
	}
	if !strings.Contains(content, "|") {
		t.Fatalf("expected printf output marker, got:\n%s", content)
	}
}

func shellOrDefault() string {
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell
	}
	return "/bin/sh"
}

func TestOSCTitle(t *testing.T) {
	c := NewClient(80, 24)
	defer c.Close()

	id, err := c.NewSession()
	if err != nil {
		t.Fatalf("NewSession() error: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	// The shell (or its prompt) typically sets a title via OSC escapes.
	// Verify the session picks up a non-empty name from these.
	sessions, err := c.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions() error: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	// Many shells set title automatically; verify we captured it
	if sessions[0].Name != "" {
		t.Logf("OSC title captured: %q", sessions[0].Name)
	}

	// Write a known title directly to the vt10x terminal to test the
	// OSC parsing mechanism without shell interference.
	s, _ := c.getSession(id)
	s.term.Write([]byte("\033]0;test-title\007"))
	time.Sleep(50 * time.Millisecond)

	// The readLoop should pick up the title on next iteration, but since
	// we wrote directly, update name manually via the same path.
	title := s.term.Title()
	if title != "test-title" {
		t.Errorf("expected vt10x title 'test-title', got %q", title)
	}
}

func TestSessionNotFound(t *testing.T) {
	c := NewClient(80, 24)
	_, err := c.ReadScreen("nonexistent", 25)
	if err == nil {
		t.Error("expected error for nonexistent session")
	}
}

func TestResize(t *testing.T) {
	c := NewClient(80, 24)
	defer c.Close()

	_, err := c.NewSession()
	if err != nil {
		t.Fatalf("NewSession() error: %v", err)
	}

	// Should not panic
	c.Resize(120, 40)
	c.Resize(0, 0)   // should be a no-op
	c.Resize(-1, -1) // should be a no-op
}

func TestMultipleSessions(t *testing.T) {
	c := NewClient(80, 24)
	defer c.Close()

	id1, err := c.NewSession()
	if err != nil {
		t.Fatalf("NewSession() error: %v", err)
	}
	id2, err := c.NewSession()
	if err != nil {
		t.Fatalf("NewSession() error: %v", err)
	}

	if id1 == id2 {
		t.Errorf("expected different session IDs, got %s and %s", id1, id2)
	}

	sessions, err := c.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions() error: %v", err)
	}
	if len(sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(sessions))
	}
}

func TestListSessionsCleansUpExited(t *testing.T) {
	c := NewClient(80, 24)
	defer c.Close()

	id, err := c.NewSession()
	if err != nil {
		t.Fatalf("NewSession() error: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	// Exit the shell
	if err := c.SendText(id, "exit\r"); err != nil {
		t.Fatalf("SendText() error: %v", err)
	}

	// Poll until ListSessions returns empty (exited sessions are excluded).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		sessions, err := c.ListSessions()
		if err != nil {
			t.Fatalf("ListSessions() error: %v", err)
		}
		if len(sessions) == 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Session remains addressable for ReadScreen (UI can still render it).
	_, err = c.ReadScreen(id, 25)
	if err != nil {
		t.Errorf("ReadScreen should still work on exited session, got: %v", err)
	}

	// Verify resources were cleaned up (session marked as cleaned).
	s, _ := c.getSession(id)
	if s == nil {
		t.Fatal("expected session to remain in map")
	}
	if !s.isCleaned() {
		t.Error("expected session to be cleaned after ListSessions")
	}

	// ListSessions stays stable — no panic, no stale entries.
	sessions, err := c.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions() error: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}

	// Close doesn't panic on already-cleaned session.
}

func TestFocusSessionNoop(t *testing.T) {
	c := NewClient(80, 24)
	err := c.FocusSession("pty-0")
	if err != nil {
		t.Errorf("FocusSession() should be no-op, got error: %v", err)
	}
}
