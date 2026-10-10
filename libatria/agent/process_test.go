package agent

import (
	"testing"

	"github.com/sethdeckard/atria/libatria/terminal"
)

func TestFindProcess(t *testing.T) {
	procs := []terminal.Process{
		{PID: 10, Argv: []string{"/bin/zsh", "-l"}},
		{PID: 11, PPID: 10, Argv: []string{"/usr/local/bin/codex", "--model", "x"}, Cwd: "/p"},
		{PID: 12, PPID: 10, Argv: []string{"claude"}},
	}
	p, typ, ok := FindProcess(procs)
	if !ok || typ != Codex || p.PID != 11 || p.Cwd != "/p" {
		t.Fatalf("FindProcess = %+v, %q, %v; want the codex process", p, typ, ok)
	}
	if _, _, ok := FindProcess([]terminal.Process{{PID: 1, Argv: []string{"zsh"}}, {PID: 2}}); ok {
		t.Fatal("no agent binary should report false")
	}
	if _, _, ok := FindProcess(nil); ok {
		t.Fatal("nil list should report false")
	}
}

func TestFindProcessArgv(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want Type
	}{
		// Native installs, as ps shows them.
		{"claude resuming", []string{"claude", "--resume", "c4f0042b"}, Claude},
		{"codex resuming", []string{"codex", "resume", "01a11ecb"}, Codex},
		// terminal.ProcessesOnTTY keeps an executable path with a space whole.
		{"installed in a path with a space", []string{"/Users/me/Agent Tools/codex", "resume"}, Codex},
		// npm installs run under their interpreter.
		{"claude under node", []string{"node", "/opt/homebrew/bin/claude"}, Claude},
		{"opencode under bun", []string{"/usr/local/bin/bun", "/Users/example/.bun/bin/opencode"}, OpenCode},
		{"codex under node with a flag", []string{"node", "--no-warnings", "/usr/local/bin/codex"}, Codex},
		{"deno run puts its subcommand first, so it isn't recognized", []string{"deno", "run", "/x/copilot"}, ""},
		{"a node server", []string{"node", "server.js"}, ""},
		{"node alone", []string{"node"}, ""},
		{"a non-agent program", []string{"bin/inventoryd"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, got, ok := FindProcess([]terminal.Process{{PID: 1, Argv: tt.argv}})
			if got != tt.want || ok != (tt.want != "") {
				t.Errorf("FindProcess(%q) = %q, %v; want %q", tt.argv, got, ok, tt.want)
			}
		})
	}
}
