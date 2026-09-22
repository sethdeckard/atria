package terminal

import (
	"strconv"
	"strings"
)

// TTYForPID returns the controlling TTY ("/dev/ttys003") of a process by
// running ps, or "" when the process has none or the lookup fails.
func TTYForPID(pid int) string {
	if pid <= 0 {
		return ""
	}
	out, err := runCommand("ps", "-p", strconv.Itoa(pid), "-o", "tty=")
	if err != nil {
		return ""
	}
	tty := strings.TrimSpace(string(out))
	if tty == "" || tty == "??" {
		return ""
	}
	return "/dev/" + tty
}
