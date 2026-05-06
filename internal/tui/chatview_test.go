package tui

import (
	"fmt"
	"testing"
)

func TestChatViewAddEntryCaps(t *testing.T) {
	c := newChatView()
	for i := 0; i < maxChatEntries+50; i++ {
		c.addEntry(chatEntry{Text: fmt.Sprintf("e%d", i)})
	}
	if got := len(c.entries); got != maxChatEntries {
		t.Fatalf("len = %d, want %d", got, maxChatEntries)
	}
	if got := cap(c.entries); got != maxChatEntries {
		t.Fatalf("cap = %d, want %d (backing array should not be pinned)",
			got, maxChatEntries)
	}
	wantLast := fmt.Sprintf("e%d", maxChatEntries+49)
	if last := c.entries[len(c.entries)-1].Text; last != wantLast {
		t.Fatalf("last entry = %q, want %q", last, wantLast)
	}
	wantFirst := fmt.Sprintf("e%d", 50)
	if first := c.entries[0].Text; first != wantFirst {
		t.Fatalf("first entry = %q, want %q (oldest %d entries should be dropped)",
			first, wantFirst, 50)
	}
}
