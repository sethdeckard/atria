package terminal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Process describes one process found on a session's TTY.
type Process struct {
	PID  int
	PPID int
	Argv []string // whitespace-split from ps's args column; original boundaries are lost, so a path containing a space is split
	Cwd  string   // best-effort; "" when the lookup failed
}

// readProcCwd resolves /proc/<pid>/cwd. Tests replace it so a fixture PID
// that happens to exist on the host can't bypass the fake runner.
var readProcCwd = func(pid int) (string, error) {
	return os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid))
}

// runCommand executes a helper binary (ps, lsof) with a timeout and returns
// its stdout. Tests replace it to run hermetically.
var runCommand = func(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = PipeGrace
	out, err := cmd.Output()
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, Timeout(name, DefaultCommandTimeout)
	}
	return out, err
}

// ProcessesOnTTY lists every process whose controlling terminal is tty
// ("/dev/ttys003" or "ttys003"), with each Cwd filled in by ProcessCWD where
// that succeeds. It runs ps once, then resolves each process's cwd in turn
// through /proc or lsof, so latency grows with the process count and each
// lsof can take up to its own timeout. An empty tty is an error; a TTY with
// no processes returns an empty slice.
func ProcessesOnTTY(tty string) ([]Process, error) {
	if tty == "" {
		return nil, errors.New("no tty")
	}
	short := strings.TrimPrefix(tty, "/dev/")
	out, err := runCommand("ps", "-t", short, "-o", "pid=,ppid=,args=")
	if err != nil {
		return nil, fmt.Errorf("ps -t %s: %w", short, err)
	}
	procs := parsePS(out)
	for i := range procs {
		if cwd, err := ProcessCWD(procs[i].PID); err == nil {
			procs[i].Cwd = cwd
		}
	}
	return procs, nil
}

// parsePS parses "pid ppid args..." rows as printed by ps -o pid=,ppid=,args=.
func parsePS(out []byte) []Process {
	var procs []Process
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			continue
		}
		procs = append(procs, Process{PID: pid, PPID: ppid, Argv: fields[2:]})
	}
	return procs
}

// ProcessCWD returns a process's working directory. It reads /proc/<pid>/cwd
// first, which works on Linux, and falls back to lsof -a -d cwd, which works
// on macOS and is slower (one subprocess per call).
func ProcessCWD(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("invalid pid %d", pid)
	}
	if target, err := readProcCwd(pid); err == nil {
		return target, nil
	}
	out, err := runCommand("lsof", "-a", "-d", "cwd", "-p", strconv.Itoa(pid), "-F", "n")
	if err != nil {
		return "", fmt.Errorf("cwd lookup failed for pid %d: %w", pid, err)
	}
	// lsof -F n prints "p<pid>" then "n<path>" lines.
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "n") && len(line) > 1 {
			return line[1:], nil
		}
	}
	return "", fmt.Errorf("cwd not found for pid %d", pid)
}
