package terminal

import (
	"fmt"
	"unicode"
	"unicode/utf8"
)

// Key names a keystroke to send to a session. The named keys below cover what
// an agent's prompt UI asks for (Enter, Escape, arrows, Tab, Ctrl-C); a single
// printable, non-whitespace rune other than U+FFFD, such as "1" or "y", is also
// a valid Key (use KeySpace for a space).
type Key string

// Named keys. Their string values are stable and are what ParseKey accepts.
const (
	KeyEnter     Key = "enter"
	KeyEscape    Key = "escape"
	KeyTab       Key = "tab"
	KeyBackTab   Key = "backtab"
	KeyBackspace Key = "backspace"
	KeyUp        Key = "up"
	KeyDown      Key = "down"
	KeyLeft      Key = "left"
	KeyRight     Key = "right"
	KeyCtrlC     Key = "ctrl-c"
	KeyCtrlD     Key = "ctrl-d"
	KeySpace     Key = "space"
)

var keySequences = map[Key]string{
	KeyEnter:     "\r",
	KeyEscape:    "\x1b",
	KeyTab:       "\t",
	KeyBackTab:   "\x1b[Z",
	KeyBackspace: "\x7f",
	KeyUp:        "\x1b[A",
	KeyDown:      "\x1b[B",
	KeyLeft:      "\x1b[D",
	KeyRight:     "\x1b[C",
	KeyCtrlC:     "\x03",
	KeyCtrlD:     "\x04",
	KeySpace:     " ",
}

// ParseKey returns the Key for a name such as "enter" or "ctrl-c", or for a
// string holding exactly one printable, non-whitespace rune. Anything else,
// including a bare space and U+FFFD, is an error, so a daemon can validate a
// key name from the network before sending it.
func ParseKey(s string) (Key, error) {
	k := Key(s)
	if _, ok := keySequences[k]; ok {
		return k, nil
	}
	if r, size := utf8.DecodeRuneInString(s); size == len(s) && size > 0 && r != utf8.RuneError && unicode.IsPrint(r) && !unicode.IsSpace(r) {
		return k, nil
	}
	return "", fmt.Errorf("unknown key %q", s)
}

// Sequence returns the bytes a terminal sends for the key: "\r" for Enter,
// "\x1b" for Escape, the CSI arrow sequences, and so on. A single printable,
// non-whitespace rune other than U+FFFD returns itself. An unknown name returns
// "" so a bad Key sends nothing rather than the name spelled out.
func (k Key) Sequence() string {
	if seq, ok := keySequences[k]; ok {
		return seq
	}
	if _, err := ParseKey(string(k)); err == nil {
		return string(k)
	}
	return ""
}

// KeySender is implemented by backends that have a better way to send a named
// key than writing its byte sequence: tmux, whose send-keys wants key names.
// Composite and cached backends implement it by routing to the owner.
type KeySender interface {
	SendKey(sessionID string, key Key) error
}

// SendKey sends a named key to a session. It uses the backend's KeySender when
// it has one and otherwise writes key.Sequence() through SendText, which is
// correct for every backend that delivers text bytes verbatim.
func SendKey(b Backend, sessionID string, key Key) error {
	if ks, ok := b.(KeySender); ok {
		return ks.SendKey(sessionID, key)
	}
	seq := key.Sequence()
	if seq == "" {
		return fmt.Errorf("unknown key %q", string(key))
	}
	return b.SendText(sessionID, seq)
}
