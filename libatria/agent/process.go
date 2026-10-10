package agent

import (
	"path/filepath"
	"strings"

	"github.com/sethdeckard/atria/libatria/terminal"
)

// FindProcess returns the first process whose argv[0] basename is a known
// agent binary, with the matching Type. An agent installed through npm runs
// under its interpreter, as "node /opt/homebrew/bin/claude", so for node,
// bun, and deno the first argument that isn't a flag is checked instead. It
// is how a session's TTY process list (terminal.ProcessesOnTTY) is narrowed
// to the agent itself rather than the shell that launched it. Processes are
// examined in the order given, so pass them as ps reported them (parent
// before child) when that matters.
func FindProcess(procs []terminal.Process) (terminal.Process, Type, bool) {
	for _, p := range procs {
		if t := agentBinary(p.Argv); t != "" {
			return p, t, true
		}
	}
	return terminal.Process{}, "", false
}

// agentBinary returns the agent argv runs, or "" when it runs none.
func agentBinary(argv []string) Type {
	if len(argv) == 0 {
		return ""
	}
	name := filepath.Base(argv[0])
	switch name {
	case "node", "bun", "deno":
		name = ""
		for _, arg := range argv[1:] {
			if !strings.HasPrefix(arg, "-") {
				name = filepath.Base(arg)
				break
			}
		}
	}
	for _, t := range detectableAgents {
		if name == string(t) {
			return t
		}
	}
	return ""
}
