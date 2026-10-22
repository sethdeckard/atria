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

func TestSendPromptClosesMentionPopups(t *testing.T) {
	tests := []struct{ text, want string }{
		{"$static-review-simple", "$static-review-simple "},
		{"run $static-review-simple", "run $static-review-simple "},
		{"look at @main.go", "look at @main.go "},
		{"first line\nthen @main.go", "first line\nthen @main.go "},
		{"run $x ", "run $x "},
		{"run $x\n", "run $x\n"},
		{"run $x\u3000", "run $x\u3000"},
		{"$x then go", "$x then go"},
		{"/review", "/review"},
		{"email me@example.com", "email me@example.com"},
		{"fix the tests", "fix the tests"},
		{"", ""},
	}
	for _, tt := range tests {
		stubSleep(t)
		rec := &recorder{}
		if err := SendPrompt(rec, "s1", tt.text, Codex); err != nil {
			t.Fatal(err)
		}
		want := []string{"send:s1:" + tt.want, "send:s1:\r"}
		if strings.Join(rec.calls, "|") != strings.Join(want, "|") {
			t.Errorf("SendPrompt(%q) calls = %q, want %q", tt.text, rec.calls, want)
		}
	}

	// Copilot's path types the trimmed text as it is.
	stubSleep(t)
	rec := &recorder{}
	if err := SendPrompt(rec, "s1", "@x", Copilot); err != nil {
		t.Fatal(err)
	}
	if want := "send:s1:@|send:s1:x|send:s1:\r"; strings.Join(rec.calls, "|") != want {
		t.Errorf("copilot calls = %q, want %q", rec.calls, want)
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
