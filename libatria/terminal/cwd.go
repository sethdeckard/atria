package terminal

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// DiscoverCWD tries to find the working directory of a session using multiple strategies.
// watchDirs are the configured watch directories used to validate results.
// projectDirs are the known project directories for name matching.
//
// Strategies in order:
//  1. get-var path - fast but often returns $HOME for TUI agents
//  2. lsof on TTY - most reliable, gets CWD from processes on the TTY
//  3. Name matching - last resort, matches project basenames in session name
func DiscoverCWD(backend Backend, session Session, watchDirs []string, projectDirs []string) string {
	if cwd := cwdFromGetVar(backend, session, watchDirs); cwd != "" {
		return cwd
	}
	if cwd := cwdFromLsof(session.TTY, watchDirs); cwd != "" {
		return cwd
	}
	if cwd := cwdFromNameMatch(session.Name, projectDirs); cwd != "" {
		return cwd
	}
	return ""
}

// cwdFromGetVar attempts to get the CWD using the backend's GetVar method.
// It validates the result is under one of the watch directories.
func cwdFromGetVar(backend Backend, session Session, watchDirs []string) string {
	val, err := backend.GetVar(session.ID, "path")
	if err != nil {
		return ""
	}
	cwd := strings.TrimSpace(string(val))
	if cwd == "" {
		return ""
	}
	if isUnderWatchDir(cwd, watchDirs) {
		return cwd
	}
	return ""
}

// cwdFromLsof attempts to discover the CWD by inspecting processes running on the session's TTY.
// It runs ps to find PIDs, then lsof to find their CWDs, returning the first that is under a watch dir.
func cwdFromLsof(tty string, watchDirs []string) string {
	if tty == "" {
		return ""
	}
	// Strip /dev/ prefix for ps -t
	ttyShort := strings.TrimPrefix(tty, "/dev/")

	psOut, err := exec.Command("ps", "-t", ttyShort, "-o", "pid=").Output()
	if err != nil {
		return ""
	}

	pids := strings.Fields(strings.TrimSpace(string(psOut)))
	for _, pid := range pids {
		lsofOut, err := exec.Command("lsof", "-a", "-d", "cwd", "-p", pid, "-F", "n").Output()
		if err != nil {
			continue
		}
		// lsof -F n output has lines like "p<pid>" and "n<path>"
		for _, line := range strings.Split(string(lsofOut), "\n") {
			if strings.HasPrefix(line, "n") {
				dir := line[1:]
				if isUnderWatchDir(dir, watchDirs) {
					return dir
				}
			}
		}
	}
	return ""
}

// cwdFromNameMatch checks if any project's basename appears in the session name.
// Returns the first matching project directory.
func cwdFromNameMatch(name string, projectDirs []string) string {
	if name == "" {
		return ""
	}
	nameLower := strings.ToLower(name)
	for _, dir := range projectDirs {
		base := strings.ToLower(filepath.Base(dir))
		if base != "" && strings.Contains(nameLower, base) {
			return dir
		}
	}
	return ""
}

// isUnderWatchDir checks if the given path is under one of the watch directories.
func isUnderWatchDir(path string, watchDirs []string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	for _, wd := range watchDirs {
		absWD, err := filepath.Abs(wd)
		if err != nil {
			continue
		}
		if strings.HasPrefix(absPath, fmt.Sprintf("%s/", absWD)) || absPath == absWD {
			return true
		}
	}
	return false
}
