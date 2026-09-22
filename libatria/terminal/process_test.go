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
