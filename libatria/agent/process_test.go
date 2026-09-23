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
