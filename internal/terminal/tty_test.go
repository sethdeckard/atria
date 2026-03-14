package terminal

import "testing"

func TestTTYForPID(t *testing.T) {
	// Invalid PID returns empty.
	if tty := TTYForPID(0); tty != "" {
		t.Errorf("expected empty TTY for PID 0, got %q", tty)
	}
	if tty := TTYForPID(-1); tty != "" {
		t.Errorf("expected empty TTY for PID -1, got %q", tty)
	}
	// Non-existent PID returns empty.
	if tty := TTYForPID(999999999); tty != "" {
		t.Errorf("expected empty TTY for non-existent PID, got %q", tty)
	}
}
