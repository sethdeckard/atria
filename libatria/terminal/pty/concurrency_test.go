package pty

import (
	"sync"
	"testing"
)

// NewSession and Resize must not race on the client's dimensions. Run with
// -race to check synchronization of cols and rows.
func TestNewSessionDuringResizeIsSafe(t *testing.T) {
	c := NewClient(80, 24)
	t.Cleanup(func() { _ = c.Close() })

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.NewSession(); err != nil {
				t.Errorf("NewSession: %v", err)
			}
		}()
	}
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c.Resize(100+i, 30+i)
		}(i)
	}
	wg.Wait()

	sessions, err := c.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 4 {
		t.Errorf("listed %d sessions, want 4", len(sessions))
	}
}
