package terminal

import (
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
	if UnderAnyDir(cwd, watchDirs) {
		return cwd
	}
	return ""
}

// cwdFromLsof discovers the CWD from the processes on the session's TTY,
// returning the first working directory under a watch dir.
func cwdFromLsof(tty string, watchDirs []string) string {
	if tty == "" {
		return ""
	}
	procs, err := ProcessesOnTTY(tty)
	if err != nil {
		return ""
	}
	for _, p := range procs {
		if p.Cwd != "" && UnderAnyDir(p.Cwd, watchDirs) {
			return p.Cwd
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

// UnderAnyDir reports whether path is one of dirs or lies beneath one of them,
// comparing absolute paths. An empty dirs matches nothing; a dir of "/"
// matches every absolute path.
func UnderAnyDir(path string, dirs []string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	for _, wd := range dirs {
		absWD, err := filepath.Abs(wd)
		if err != nil {
			continue
		}
		if absPath == absWD {
			return true
		}
		// filepath.Abs cleans trailing separators away except for the root,
		// so only "/" already ends in one.
		prefix := absWD
		if !strings.HasSuffix(prefix, string(filepath.Separator)) {
			prefix += string(filepath.Separator)
		}
		if strings.HasPrefix(absPath, prefix) {
			return true
		}
	}
	return false
}
