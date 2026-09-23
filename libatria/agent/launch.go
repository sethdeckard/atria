package agent

import (
	"strings"
	"time"

	"github.com/sethdeckard/atria/libatria/terminal"
)

// LaunchSettle is how long Launch waits between focusing a new session and
// running the command in it, so the terminal has attached a shell.
const LaunchSettle = 300 * time.Millisecond

// sleep is a seam so tests can run Launch and SendPrompt without waiting.
var sleep = time.Sleep

// ShellQuote returns s as one POSIX shell word: wrapped in single quotes,
// with every embedded single quote closed, escaped, and reopened.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// LaunchCommand builds the shell command that starts agent t in dir with
// args: "cd '<dir>' && <binary> '<arg>'...". Every argument is quoted with
// ShellQuote, so pass them as separate strings, not as one pre-joined
// string. An empty dir omits the cd.
func LaunchCommand(dir string, t Type, args ...string) string {
	var b strings.Builder
	if dir != "" {
		b.WriteString("cd " + ShellQuote(dir) + " && ")
	}
	b.WriteString(string(t))
	for _, a := range args {
		b.WriteString(" " + ShellQuote(a))
	}
	return b.String()
}

// Launch creates a session, changes to dir, and runs cmd in it, returning
// the new session ID. The session is created on source through
// terminal.SourceLauncher when b implements it and source is non-empty, and
// with NewSession otherwise. It is focused before the command runs (iTerm2
// doesn't populate a background tab's buffer until it has been shown once)
// and given LaunchSettle to attach a shell.
//
// cmd runs as given after "cd '<dir>' && " (no cd when dir is empty), so
// quote your own arguments or build it with LaunchCommand; note that
// LaunchCommand adds its own cd, so pass it an empty dir or pass Launch one.
// If RunCommand fails, Launch returns the created session ID with the error.
// The session may contain partially delivered input.
func Launch(b terminal.Backend, source, dir, cmd string) (string, error) {
	var id string
	var err error
	if sl, ok := b.(terminal.SourceLauncher); ok && source != "" {
		id, err = sl.NewSessionOn(source)
	} else {
		id, err = b.NewSession()
	}
	if err != nil {
		return "", err
	}
	_ = b.FocusSession(id)
	sleep(LaunchSettle)
	shell := cmd
	if dir != "" {
		shell = "cd " + ShellQuote(dir) + " && " + cmd
	}
	if err := b.RunCommand(id, shell); err != nil {
		return id, err
	}
	return id, nil
}
