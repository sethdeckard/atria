package agent

import (
	"errors"
	"strings"
	"testing"
)

func TestSendPromptTwoStep(t *testing.T) {
	slept := stubSleep(t)
	rec := &recorder{}
	if err := SendPrompt(rec, "s1", "fix the tests\nplease", Claude); err != nil {
		t.Fatal(err)
	}
	want := []string{"send:s1:fix the tests\nplease", "send:s1:\r"}
	if strings.Join(rec.calls, "|") != strings.Join(want, "|") {
		t.Errorf("calls = %q, want %q", rec.calls, want)
	}
	if len(*slept) != 1 || (*slept)[0] != SubmitDelay {
		t.Errorf("slept %v, want one SubmitDelay", *slept)
	}
}

func TestSendPromptCopilotTypesRunes(t *testing.T) {
	slept := stubSleep(t)
	rec := &recorder{}
	if err := SendPrompt(rec, "s1", " ab\r\nc\nd ", Copilot); err != nil {
		t.Fatal(err)
	}
	want := []string{"send:s1:a", "send:s1:b", "send:s1: ", "send:s1:c", "send:s1: ", "send:s1:d", "send:s1:\r"}
	if strings.Join(rec.calls, "|") != strings.Join(want, "|") {
		t.Errorf("calls = %q, want %q", rec.calls, want)
	}
	// One CopilotRuneDelay per rune, then SubmitDelay before the return.
	if len(*slept) != 7 || (*slept)[0] != CopilotRuneDelay || (*slept)[6] != SubmitDelay {
		t.Errorf("slept %v", *slept)
	}
}

func TestSendPromptPropagatesErrors(t *testing.T) {
	stubSleep(t)
	boom := errors.New("boom")
	rec := &recorder{sendErr: boom}
	if err := SendPrompt(rec, "s1", "x", Codex); !errors.Is(err, boom) {
		t.Errorf("err = %v, want boom", err)
	}
	if len(rec.calls) != 1 {
		t.Errorf("no return should be sent after a failed write, got %v", rec.calls)
	}
	rec = &recorder{sendErr: boom}
	if err := SendPrompt(rec, "s1", "xy", Copilot); !errors.Is(err, boom) || len(rec.calls) != 1 {
		t.Errorf("copilot: err = %v, calls = %v", err, rec.calls)
	}
}
