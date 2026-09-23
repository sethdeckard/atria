package agent

import (
	"path/filepath"

	"github.com/sethdeckard/atria/libatria/terminal"
)

// FindProcess returns the first process whose argv[0] basename is a known
// agent binary, with the matching Type. It is how a session's TTY process
// list (terminal.ProcessesOnTTY) is narrowed to the agent itself rather than
// the shell that launched it. Processes are examined in the order given, so
// pass them as ps reported them (parent before child) when that matters.
func FindProcess(procs []terminal.Process) (terminal.Process, Type, bool) {
	for _, p := range procs {
		if len(p.Argv) == 0 {
			continue
		}
		name := filepath.Base(p.Argv[0])
		for _, t := range detectableAgents {
			if name == string(t) {
				return p, t, true
			}
		}
	}
	return terminal.Process{}, "", false
}
