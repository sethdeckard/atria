package watch

import (
	"strings"
	"testing"
	"time"

	"github.com/sethdeckard/atria/libatria/agent"
	"github.com/sethdeckard/atria/libatria/terminal"
)

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

const blank25 = "\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n"

func TestObserveTable(t *testing.T) {
	tests := []struct {
		name          string
		seed          Tracker
		screen        string
		wantStatus    agent.Status
		wantApply     bool
		wantReason    Reason
		wantUnmatched int
		wantStamp     bool // LastActivity == now
	}{
		{
			name:       "matched idle prompt applies",
			seed:       Tracker{Type: agent.Claude, Status: agent.StatusWorking},
			screen:     "some output\n❯",
			wantStatus: agent.StatusIdle, wantApply: true, wantReason: ReasonMatched, wantStamp: true,
		},
		{
			name:       "matched idle despite recent activity",
			seed:       Tracker{Type: agent.Claude, Status: agent.StatusWorking, LastActivity: t0.Add(-time.Second)},
			screen:     "some output\n❯",
			wantStatus: agent.StatusIdle, wantApply: true, wantReason: ReasonMatched, wantStamp: true,
		},
		{
			name:       "matched idle with unchanged screen still transitions",
			seed:       Tracker{Type: agent.Claude, Status: agent.StatusWorking, LastScreen: "some output\n❯"},
			screen:     "some output\n❯",
			wantStatus: agent.StatusIdle, wantApply: true, wantReason: ReasonMatched, wantStamp: true,
		},
		{
			name:       "stale read: unchanged screen, same status, not applied",
			seed:       Tracker{Type: agent.Claude, Status: agent.StatusIdle, LastScreen: "some output\n❯", UnmatchedReads: 2, LastActivity: t0.Add(-time.Hour)},
			screen:     "some output\n❯",
			wantStatus: agent.StatusIdle, wantApply: false, wantReason: ReasonMatched, wantUnmatched: 0, wantStamp: false,
		},
		{
			name:       "blank reads while working go idle at two",
			seed:       Tracker{Type: agent.Claude, Status: agent.StatusWorking, LastScreen: blank25, UnmatchedReads: 1},
			screen:     blank25,
			wantStatus: agent.StatusIdle, wantApply: true, wantReason: ReasonBlank, wantUnmatched: 2, wantStamp: true,
		},
		{
			name:       "changed unmatched screen resets counter and stays working",
			seed:       Tracker{Type: agent.Codex, Status: agent.StatusWorking, LastScreen: "› some codex prompt\n", UnmatchedReads: 2},
			screen:     "myhost% \n",
			wantStatus: agent.StatusWorking, wantApply: false, wantReason: ReasonNone, wantUnmatched: 0,
		},
		{
			name:       "needs_input then changed unmatched screen means moved on",
			seed:       Tracker{Type: agent.Claude, Status: agent.StatusNeedsInput, Attention: "Do you want to proceed?", LastScreen: "Do you want to proceed?\n"},
			screen:     "plain output\n",
			wantStatus: agent.StatusWorking, wantApply: true, wantReason: ReasonMovedOn, wantUnmatched: 0, wantStamp: true,
		},
		{
			name:       "unchanged unmatched while idle only counts",
			seed:       Tracker{Type: agent.Claude, Status: agent.StatusIdle, LastScreen: "plain\n", UnmatchedReads: 5},
			screen:     "plain\n",
			wantStatus: agent.StatusIdle, wantApply: false, wantReason: ReasonNone, wantUnmatched: 6,
		},
		{
			name:       "NUL bytes normalize to spaces before comparing",
			seed:       Tracker{Type: agent.Claude, Status: agent.StatusWorking, LastScreen: "a b\n❯"},
			screen:     "a\x00b\n❯",
			wantStatus: agent.StatusIdle, wantApply: true, wantReason: ReasonMatched, wantStamp: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := tt.seed
			got := tr.Observe(tt.screen, t0)
			if tr.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", tr.Status, tt.wantStatus)
			}
			if got.Applied != tt.wantApply {
				t.Errorf("Applied = %v, want %v", got.Applied, tt.wantApply)
			}
			if got.Reason != tt.wantReason {
				t.Errorf("Reason = %v, want %v", got.Reason, tt.wantReason)
			}
			if tr.UnmatchedReads != tt.wantUnmatched {
				t.Errorf("UnmatchedReads = %d, want %d", tr.UnmatchedReads, tt.wantUnmatched)
			}
			if stamped := tr.LastActivity.Equal(t0); stamped != tt.wantStamp {
				t.Errorf("LastActivity stamped = %v, want %v", stamped, tt.wantStamp)
			}
			if !tr.ScreenChecked || !tr.LastRead.Equal(t0) {
				t.Error("ScreenChecked and LastRead must be set on every read")
			}
			if tr.LastScreen != strings.ReplaceAll(tt.screen, "\x00", " ") {
				t.Error("LastScreen must hold the normalized screen")
			}
			if got.To != tr.Status || got.From != tt.seed.Status {
				t.Errorf("From/To = %q/%q, want %q/%q", got.From, got.To, tt.seed.Status, tr.Status)
			}
		})
	}
}

func TestObserveAgentExitedSequence(t *testing.T) {
	tr := Tracker{Type: agent.Codex, Status: agent.StatusWorking, LastScreen: "› some codex prompt\n"}
	shell := "myhost% \n"
	// Read 1: changed, unmatched: counter reset, still working.
	if got := tr.Observe(shell, t0); got.Applied || tr.Status != agent.StatusWorking || tr.UnmatchedReads != 0 {
		t.Fatalf("read 1: %+v status=%q unmatched=%d", got, tr.Status, tr.UnmatchedReads)
	}
	// Reads 2-3: stable, counter 1 and 2, still working.
	for i := 1; i <= 2; i++ {
		if got := tr.Observe(shell, t0.Add(time.Duration(i)*time.Second)); got.Applied || tr.Status != agent.StatusWorking {
			t.Fatalf("read %d should stay working: %+v", i+1, got)
		}
	}
	// Read 4: third stable unmatched read: idle.
	got := tr.Observe(shell, t0.Add(3*time.Second))
	if !got.Applied || !got.StatusChanged() || got.To != agent.StatusIdle || got.Reason != ReasonStableUnmatched {
		t.Fatalf("read 4: %+v", got)
	}
}

func TestObserveChangingOutputStaysWorking(t *testing.T) {
	tr := Tracker{Type: agent.Claude, Status: agent.StatusWorking, LastScreen: "initial\n"}
	for i := 0; i < 10; i++ {
		tr.Observe("plain output line "+strings.Repeat("x", i)+"\n", t0)
	}
	if tr.Status != agent.StatusWorking || tr.UnmatchedReads != 0 {
		t.Fatalf("status=%q unmatched=%d", tr.Status, tr.UnmatchedReads)
	}
}

func TestObserveUnmatchedCounterResetsOnMatch(t *testing.T) {
	tr := Tracker{Type: agent.Claude, Status: agent.StatusWorking, LastScreen: "✻ Reading…\n"}
	tr.Observe("some plain output\n", t0)
	tr.Observe("some plain output\n", t0)
	tr.Observe("some plain output\n", t0)
	if tr.UnmatchedReads != 2 {
		t.Fatalf("unmatched = %d, want 2", tr.UnmatchedReads)
	}
	got := tr.Observe("✻ Editing…\n", t0)
	if tr.UnmatchedReads != 0 || tr.Status != agent.StatusWorking || got.Reason != ReasonMatched {
		t.Fatalf("after match: unmatched=%d status=%q %+v", tr.UnmatchedReads, tr.Status, got)
	}
}

func TestObserveNeedsInputEdgesAndAttention(t *testing.T) {
	tr := Tracker{Type: agent.Claude, Status: agent.StatusWorking}
	got := tr.Observe("Do you want to proceed?\n", t0)
	if !got.EnteredNeedsInput() || tr.Attention != "Do you want to proceed?" {
		t.Fatalf("enter: %+v attention=%q", got, tr.Attention)
	}
	// Staying in needs_input with a changed screen is applied but not a
	// status change, and refreshes the attention line.
	got = tr.Observe("extra\nAllow file edit?\n", t0)
	if !got.Applied || got.StatusChanged() || got.EnteredNeedsInput() || tr.Attention != "Allow file edit?" {
		t.Fatalf("stay: %+v attention=%q", got, tr.Attention)
	}
	got = tr.Observe("done\n❯", t0)
	if !got.LeftNeedsInput() || tr.Attention != "" || tr.Status != agent.StatusIdle {
		t.Fatalf("leave: %+v attention=%q status=%q", got, tr.Attention, tr.Status)
	}
}

func TestRefreshTable(t *testing.T) {
	tests := []struct {
		name         string
		seed         Tracker
		sess         terminal.Session
		wantType     agent.Type
		wantActivity string
		wantTicks    int
		wantOrphan   bool
		wantStamp    bool
	}{
		{
			name:     "activity from title stamps LastActivity",
			seed:     Tracker{Type: agent.Claude, Status: agent.StatusWorking},
			sess:     terminal.Session{Name: "✳ Editing main.go (sourcekit-lsp)"},
			wantType: agent.Claude, wantActivity: "Editing main.go", wantStamp: true,
		},
		{
			name:     "activity cleared to empty does not stamp",
			seed:     Tracker{Type: agent.Claude, Status: agent.StatusWorking, Activity: "Editing"},
			sess:     terminal.Session{Name: "claude"},
			wantType: agent.Claude, wantActivity: "",
		},
		{
			name:     "retype on pane reuse",
			seed:     Tracker{Type: agent.Claude, Status: agent.StatusIdle},
			sess:     terminal.Session{Name: "codex"},
			wantType: agent.Codex, wantTicks: 0,
		},
		{
			name:     "same agent in title keeps type",
			seed:     Tracker{Type: agent.Claude, Status: agent.StatusWorking},
			sess:     terminal.Session{Name: "✳ Editing main.go (claude)"},
			wantType: agent.Claude, wantActivity: "Editing main.go", wantStamp: true,
		},
		{
			name:     "non-agent title never retypes; not orphan without screen check",
			seed:     Tracker{Type: agent.Claude, Status: agent.StatusIdle},
			sess:     terminal.Session{Name: "zsh"},
			wantType: agent.Claude, wantActivity: "zsh", wantTicks: 0, wantStamp: true,
		},
		{
			name:     "working session is never orphaned",
			seed:     Tracker{Type: agent.Codex, Status: agent.StatusWorking, ScreenChecked: true, LastScreen: "user@host ~ %"},
			sess:     terminal.Session{Name: "zsh"},
			wantType: agent.Codex, wantActivity: "zsh", wantTicks: 0, wantStamp: true,
		},
		{
			name:     "idle, generic title, no agent screen: one tick",
			seed:     Tracker{Type: agent.Codex, Status: agent.StatusIdle, ScreenChecked: true, LastScreen: "user@host ~ %"},
			sess:     terminal.Session{Name: "~/projects/myproject"},
			wantType: agent.Codex, wantActivity: "~/projects/myproject", wantTicks: 1, wantStamp: true,
		},
		{
			name:     "second tick reaches the threshold",
			seed:     Tracker{Type: agent.Codex, Status: agent.StatusIdle, ScreenChecked: true, LastScreen: "user@host ~ %", OrphanTicks: 1},
			sess:     terminal.Session{Name: "zsh"},
			wantType: agent.Codex, wantActivity: "zsh", wantTicks: 2, wantOrphan: true, wantStamp: true,
		},
		{
			name:     "idle with agent prompt on screen is kept",
			seed:     Tracker{Type: agent.Claude, Status: agent.StatusIdle, ScreenChecked: true, LastScreen: "some conversation output\n\n❯ ", OrphanTicks: 1},
			sess:     terminal.Session{Name: "Editing foo.go"},
			wantType: agent.Claude, wantActivity: "Editing foo.go", wantTicks: 0, wantStamp: true,
		},
		{
			name:     "iterm shell job is an orphan tick even with agent screen",
			seed:     Tracker{Type: agent.Claude, Source: "iterm", Status: agent.StatusIdle, ScreenChecked: true, LastScreen: "old Claude output\n\n❯ "},
			sess:     terminal.Session{Name: "..ts/go/loadout (-zsh)", Source: "iterm", Job: "zsh"},
			wantType: agent.Claude, wantActivity: "..ts/go/loadout", wantTicks: 1, wantStamp: true,
		},
		{
			name:     "shell job on a non-iterm source is not the fallback",
			seed:     Tracker{Type: agent.Claude, Source: "tmux", Status: agent.StatusIdle, ScreenChecked: true, LastScreen: "❯ "},
			sess:     terminal.Session{Name: "loadout", Source: "tmux", Job: "zsh"},
			wantType: agent.Claude, wantActivity: "loadout", wantTicks: 0, wantStamp: true,
		},
		{
			name:     "retype applies before the screen check",
			seed:     Tracker{Type: agent.Claude, Status: agent.StatusIdle, ScreenChecked: true, LastScreen: "› "},
			sess:     terminal.Session{Name: "codex"},
			wantType: agent.Codex, wantTicks: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := tt.seed
			r := tr.Refresh(tt.sess, t0)
			if tr.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", tr.Type, tt.wantType)
			}
			if tr.Activity != tt.wantActivity {
				t.Errorf("Activity = %q, want %q", tr.Activity, tt.wantActivity)
			}
			if tr.OrphanTicks != tt.wantTicks {
				t.Errorf("OrphanTicks = %d, want %d", tr.OrphanTicks, tt.wantTicks)
			}
			if r.Orphan != tt.wantOrphan {
				t.Errorf("Orphan = %v, want %v", r.Orphan, tt.wantOrphan)
			}
			if stamped := tr.LastActivity.Equal(t0); stamped != tt.wantStamp {
				t.Errorf("LastActivity stamped = %v, want %v", stamped, tt.wantStamp)
			}
			if r.Retyped != (tt.seed.Type != tt.wantType) {
				t.Errorf("Retyped = %v", r.Retyped)
			}
			if r.Retyped && r.PrevType != tt.seed.Type {
				t.Errorf("PrevType = %q, want %q", r.PrevType, tt.seed.Type)
			}
			if tt.sess.Source != "" && tr.Source != tt.sess.Source {
				t.Errorf("Source = %q, want %q", tr.Source, tt.sess.Source)
			}
		})
	}
}

func TestPollInterval(t *testing.T) {
	active, idle := time.Second, 3*time.Second
	for _, s := range []agent.Status{agent.StatusWorking, agent.StatusNeedsInput, agent.StatusError} {
		if PollInterval(s, active, idle) != active {
			t.Errorf("%q should poll at the active interval", s)
		}
	}
	if PollInterval(agent.StatusIdle, active, idle) != idle || PollInterval("", active, idle) != idle {
		t.Error("idle and unknown should poll at the idle interval")
	}
}

func TestObserveBellDoesNotCountAsScreenChange(t *testing.T) {
	tr := Tracker{Type: agent.Claude, Status: agent.StatusWorking, LastScreen: "waiting\n"}
	got := tr.Observe("\x07waiting\n", t0)
	if !got.EnteredNeedsInput() {
		t.Fatalf("bell read should enter needs_input: %+v", got)
	}
	if tr.LastScreen != "waiting\n" {
		t.Fatalf("LastScreen must not keep the bell byte, got %q", tr.LastScreen)
	}
	// The same screen without the bell is an unchanged, unmatched read: the
	// agent did not move on.
	got = tr.Observe("waiting\n", t0.Add(time.Second))
	if got.Changed || got.Applied || tr.Status != agent.StatusNeedsInput || got.Reason != ReasonNone {
		t.Fatalf("read after bell = %+v status=%q", got, tr.Status)
	}
	if tr.UnmatchedReads != 1 {
		t.Fatalf("unmatched = %d, want 1", tr.UnmatchedReads)
	}
}
