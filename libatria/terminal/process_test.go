package terminal

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// withFakeRunner swaps the ps/lsof runner for the test's duration and makes
// the /proc lookup fail, so fixture PIDs never touch the host.
func withFakeRunner(t *testing.T, fn func(name string, args ...string) ([]byte, error)) {
	t.Helper()
	prevRun, prevProc := runCommand, readProcCwd
	runCommand = fn
	readProcCwd = func(int) (string, error) { return "", errors.New("no /proc in tests") }
	t.Cleanup(func() { runCommand, readProcCwd = prevRun, prevProc })
}

func TestProcessCWDPrefersProc(t *testing.T) {
	withFakeRunner(t, func(string, ...string) ([]byte, error) {
		t.Error("lsof must not run when /proc answers")
		return nil, nil
	})
	readProcCwd = func(pid int) (string, error) { return "/from/proc", nil }
	if got, err := ProcessCWD(5); err != nil || got != "/from/proc" {
		t.Fatalf("ProcessCWD = %q, %v", got, err)
	}
}

func TestProcessesOnTTYParsesPSAndFillsCwd(t *testing.T) {
	withFakeRunner(t, func(name string, args ...string) ([]byte, error) {
		switch name {
		case "ps":
			if len(args) < 2 || args[0] != "-t" || args[1] != "ttys003" {
				t.Errorf("ps args = %v", args)
			}
			if args[len(args)-1] == "pid=,comm=" {
				return []byte(" 4242 /bin/zsh\n 4300 claude\n"), nil
			}
			return []byte(" 4242  4200 /bin/zsh -l\n 4300  4242 claude --resume abc\n\nbogus line\n"), nil
		case "lsof":
			// args: -a -d cwd -p <pid> -F n
			switch args[4] {
			case "4242":
				return []byte("p4242\nn/Users/me\n"), nil
			case "4300":
				return []byte("p4300\nn/Users/me/projects/app\n"), nil
			}
		}
		return nil, errors.New("unexpected " + name)
	})

	procs, err := ProcessesOnTTY("/dev/ttys003")
	if err != nil {
		t.Fatal(err)
	}
	if len(procs) != 2 {
		t.Fatalf("got %d processes, want 2: %+v", len(procs), procs)
	}
	if procs[0].PID != 4242 || procs[0].PPID != 4200 || procs[0].Argv[0] != "/bin/zsh" || procs[0].Cwd != "/Users/me" {
		t.Errorf("procs[0] = %+v", procs[0])
	}
	if procs[1].PID != 4300 || len(procs[1].Argv) != 3 || procs[1].Argv[0] != "claude" || procs[1].Cwd != "/Users/me/projects/app" {
		t.Errorf("procs[1] = %+v", procs[1])
	}
}

func TestProcessesOnTTYKeepsSpacesInExecutable(t *testing.T) {
	withFakeRunner(t, func(name string, args ...string) ([]byte, error) {
		if name != "ps" {
			return nil, errors.New("no lsof")
		}
		if args[len(args)-1] == "pid=,comm=" {
			return []byte(" 4242 -zsh\n 4300 /Users/me/Agent Tools/codex\n 4400 Web Content\n"), nil
		}
		return []byte(" 4242  4200 -zsh\n 4300  4242 /Users/me/Agent Tools/codex resume abc\n 4400  4242 /usr/lib/browser -contentproc\n"), nil
	})
	procs, err := ProcessesOnTTY("ttys003")
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"-zsh"},
		{"/Users/me/Agent Tools/codex", "resume", "abc"},
		{"/usr/lib/browser", "-contentproc"}, // comm doesn't spell its args, so they stand
	}
	for i, w := range want {
		if strings.Join(procs[i].Argv, "|") != strings.Join(w, "|") {
			t.Errorf("procs[%d].Argv = %q, want %q", i, procs[i].Argv, w)
		}
	}
}

func TestProcessesOnTTYFailsWithoutComm(t *testing.T) {
	withFakeRunner(t, func(name string, args ...string) ([]byte, error) {
		if name == "ps" && args[len(args)-1] == "pid=,ppid=,args=" {
			return []byte(" 4300  4242 /Users/me/Agent Tools/codex\n"), nil
		}
		return nil, errors.New("unavailable")
	})
	if procs, err := ProcessesOnTTY("ttys003"); err == nil {
		t.Fatalf("a failed comm read must fail the listing rather than return a split path, got %+v", procs)
	}
}

func TestJoinExecutable(t *testing.T) {
	tests := []struct {
		name, args, comm, want string // args and want fields joined by "|"
	}{
		{"macOS path with a space", "/Users/me/Agent|Tools/codex|resume", "/Users/me/Agent Tools/codex", "/Users/me/Agent Tools/codex|resume"},
		{"Linux name for a path with a space", "/home/me/Agent|Tools/codex|resume", "codex", "/home/me/Agent Tools/codex|resume"},
		{"Linux name truncated to 15", "/opt/My|Apps/claude-code-wrapper", "claude-code-wra", "/opt/My Apps/claude-code-wrapper"},
		{"not split", "claude|--resume", "claude", "claude|--resume"},
		{"an interpreter keeps its script separate", "node|/usr/local/lib/cli.js", "node", "node|/usr/local/lib/cli.js"},
		{"an argument named like an agent", "vim|/tmp/codex", "vim", "vim|/tmp/codex"},
		{"a login shell", "-zsh", "zsh", "-zsh"},
		{"comm that matches nothing", "/usr/lib/browser|-contentproc", "Web Content", "/usr/lib/browser|-contentproc"},
		{"no comm", "/Users/me/Agent|Tools/codex", "", "/Users/me/Agent|Tools/codex"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strings.Join(joinExecutable(strings.Split(tt.args, "|"), tt.comm), "|")
			if got != tt.want {
				t.Errorf("joinExecutable = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProcessesOnTTYErrors(t *testing.T) {
	if _, err := ProcessesOnTTY(""); err == nil {
		t.Fatal("empty tty must error")
	}
	withFakeRunner(t, func(string, ...string) ([]byte, error) { return nil, errors.New("ps exploded") })
	if _, err := ProcessesOnTTY("ttys001"); err == nil || !strings.Contains(err.Error(), "ps exploded") {
		t.Fatalf("err = %v", err)
	}
	withFakeRunner(t, func(name string, _ ...string) ([]byte, error) {
		if name == "ps" {
			return []byte(""), nil
		}
		return nil, errors.New("no lsof")
	})
	procs, err := ProcessesOnTTY("ttys001")
	if err != nil || len(procs) != 0 {
		t.Fatalf("empty ps output should give an empty slice, got %v, %v", procs, err)
	}
}

func TestProcessCWDFallsBackToLsofAndReportsMissing(t *testing.T) {
	if _, err := ProcessCWD(0); err == nil {
		t.Fatal("pid 0 must error")
	}
	withFakeRunner(t, func(name string, args ...string) ([]byte, error) {
		if name != "lsof" {
			t.Errorf("unexpected %s", name)
		}
		if args[4] == "77" {
			return []byte("p77\n"), nil // no n line
		}
		return []byte("p78\nn/tmp/work\n"), nil
	})
	if _, err := ProcessCWD(77); err == nil {
		t.Fatal("missing n line must error")
	}
	if got, err := ProcessCWD(78); err != nil || got != "/tmp/work" {
		t.Fatalf("ProcessCWD(78) = %q, %v", got, err)
	}
}

func TestTTYForPIDUsesRunner(t *testing.T) {
	withFakeRunner(t, func(name string, args ...string) ([]byte, error) {
		if name != "ps" || args[1] != "99" {
			t.Errorf("args = %s %v", name, args)
		}
		return []byte("ttys009\n"), nil
	})
	if got := TTYForPID(99); got != "/dev/ttys009" {
		t.Fatalf("TTYForPID = %q", got)
	}
	withFakeRunner(t, func(string, ...string) ([]byte, error) { return []byte("??\n"), nil })
	if got := TTYForPID(99); got != "" {
		t.Fatalf("no-tty marker should give \"\", got %q", got)
	}
}

func TestDiscoverCWDUsesTTYProcesses(t *testing.T) {
	withFakeRunner(t, func(name string, args ...string) ([]byte, error) {
		switch name {
		case "ps":
			return []byte(" 10 1 zsh\n 11 10 claude\n"), nil
		case "lsof":
			if args[4] == "10" {
				return []byte("n/Users/me\n"), nil
			}
			return []byte("n/Users/me/projects/app\n"), nil
		}
		return nil, errors.New("unexpected")
	})
	b := &cwdMockBackend{} // GetVar returns nothing useful
	got := DiscoverCWD(b, Session{ID: "s", TTY: "/dev/ttys003"}, []string{"/Users/me/projects"}, nil)
	if got != "/Users/me/projects/app" {
		t.Fatalf("DiscoverCWD = %q, want the process cwd under the watch dir", got)
	}
}

func TestUnavailableAndTimeoutHelpers(t *testing.T) {
	err := Unavailable("tmux list-panes", errors.New("boom"))
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Unavailable = %v", err)
	}
	if err := Unavailable("op", nil); !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "<nil>") {
		t.Fatalf("Unavailable(nil) = %v", err)
	}
	terr := Timeout("kitten @ ls", DefaultCommandTimeout)
	if !errors.Is(terr, ErrUnavailable) || !errors.Is(terr, context.DeadlineExceeded) {
		t.Fatalf("Timeout = %v", terr)
	}
	if TimeoutOr(0) != DefaultCommandTimeout || TimeoutOr(-1) != DefaultCommandTimeout || TimeoutOr(1) != 1 {
		t.Fatal("TimeoutOr defaults wrong")
	}
}

func TestPIDsByTTY(t *testing.T) {
	withFakeRunner(t, func(name string, args ...string) ([]byte, error) {
		if name != "ps" || strings.Join(args, " ") != "-A -o tty=,pid=" {
			t.Errorf("ran %s %v", name, args)
		}
		return []byte("??        1\n" +
			"ttys003 412\n" +
			"ttys001  90\n" +
			"ttys003  77\n" +
			"?       300\n" +
			"pts/0  1200\n" +
			"garbage\n"), nil
	})
	got, err := PIDsByTTY()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"ttys003": "77,412", "ttys001": "90", "pts/0": "1200"}
	if len(got) != len(want) {
		t.Fatalf("PIDsByTTY = %v, want %v", got, want)
	}
	for tty, pids := range want {
		if got[tty] != pids {
			t.Errorf("PIDsByTTY[%q] = %q, want %q", tty, got[tty], pids)
		}
	}
}

func TestPIDsByTTYError(t *testing.T) {
	withFakeRunner(t, func(string, ...string) ([]byte, error) { return nil, errors.New("no ps") })
	if _, err := PIDsByTTY(); err == nil {
		t.Fatal("expected an error")
	}
}
