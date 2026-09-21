package terminal

import (
	"os/exec"
	"strconv"
	"strings"
)

// TTYForPID returns the controlling TTY for a process by running ps.
// Returns empty string on any failure.
func TTYForPID(pid int) string {
	if pid <= 0 {
		return ""
	}
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "tty=").Output()
	if err != nil {
		return ""
	}
	tty := strings.TrimSpace(string(out))
	if tty == "" || tty == "??" {
		return ""
	}
	return "/dev/" + tty
}
