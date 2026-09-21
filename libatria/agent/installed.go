package agent

import "os/exec"

// Installed returns the agent types whose CLI binary is on PATH, in the order
// of [Types]. It runs exec.LookPath once per type and is the only function in
// this package that touches the environment.
func Installed() []Type {
	var out []Type
	for _, t := range detectableAgents {
		if _, err := exec.LookPath(string(t)); err == nil {
			out = append(out, t)
		}
	}
	return out
}
