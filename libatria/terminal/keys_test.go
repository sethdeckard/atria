package terminal

import (
	"errors"
	"testing"
)

func TestParseKey(t *testing.T) {
	tests := []struct {
		in      string
		want    Key
		wantErr bool
	}{
		{"enter", KeyEnter, false},
		{"ctrl-c", KeyCtrlC, false},
		{"1", Key("1"), false},
		{"y", Key("y"), false},
		{"é", Key("é"), false},
		{"", "", true},
		{" ", "", true},
		{"12", "", true},
		{"Enter", "", true},
		{"\x1b", "", true},
	}
	for _, tt := range tests {
		got, err := ParseKey(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseKey(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseKey(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestKeySequence(t *testing.T) {
	tests := []struct {
		key  Key
		want string
	}{
		{KeyEnter, "\r"},
		{KeyEscape, "\x1b"},
		{KeyTab, "\t"},
		{KeyBackTab, "\x1b[Z"},
		{KeyBackspace, "\x7f"},
		{KeyUp, "\x1b[A"},
		{KeyDown, "\x1b[B"},
		{KeyLeft, "\x1b[D"},
		{KeyRight, "\x1b[C"},
		{KeyCtrlC, "\x03"},
		{KeyCtrlD, "\x04"},
		{KeySpace, " "},
		{Key("1"), "1"},
		{Key("bogus"), ""},
	}
	for _, tt := range tests {
		if got := tt.key.Sequence(); got != tt.want {
			t.Errorf("%q.Sequence() = %q, want %q", tt.key, got, tt.want)
		}
	}
}

// keySendingBackend records SendKey calls so routing can be observed.
type keySendingBackend struct {
	mockBackend
	keys []Key
}

func (k *keySendingBackend) SendKey(_ string, key Key) error {
	k.keys = append(k.keys, key)
	return nil
}

func TestSendKeyUsesKeySenderWhenAvailable(t *testing.T) {
	b := &keySendingBackend{}
	if err := SendKey(b, "s1", KeyEscape); err != nil {
		t.Fatal(err)
	}
	if len(b.keys) != 1 || b.keys[0] != KeyEscape {
		t.Fatalf("keys = %v, want [escape]", b.keys)
	}
	if len(b.sent) != 0 {
		t.Fatalf("SendText should not be called when KeySender exists, got %v", b.sent)
	}
}

func TestSendKeyFallsBackToSequence(t *testing.T) {
	b := &mockBackend{}
	if err := SendKey(b, "s1", KeyUp); err != nil {
		t.Fatal(err)
	}
	if len(b.sent) != 1 || b.sent[0] != "\x1b[A" {
		t.Fatalf("sent = %q, want the up-arrow sequence", b.sent)
	}
}

func TestSendKeyRejectsUnknownKey(t *testing.T) {
	b := &mockBackend{}
	err := SendKey(b, "s1", Key("nope"))
	if err == nil {
		t.Fatal("expected error for unknown key")
	}
	if len(b.sent) != 0 {
		t.Fatalf("nothing should be sent for an unknown key, got %q", b.sent)
	}
	var target interface{ Error() string }
	if !errors.As(err, &target) {
		t.Fatal("expected an error value")
	}
}
