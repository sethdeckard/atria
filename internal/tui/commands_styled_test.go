package tui

import (
	"errors"
	"testing"
)

// styledStub is a Backend that also implements terminal.StyledReader, with a
// configurable styled result/error.
type styledStub struct {
	*stubBackend
	plain     string
	styled    string
	styledErr error
}

func (s *styledStub) ReadScreen(sessionID string, lines int) (string, error) {
	return s.plain, nil
}

func (s *styledStub) ReadScreenStyled(sessionID string, lines int) (string, error) {
	return s.styled, s.styledErr
}

func TestReadScreenLinesStyled(t *testing.T) {
	t.Run("styled success populates content and marks fetched", func(t *testing.T) {
		b := &styledStub{stubBackend: &stubBackend{}, plain: "plain", styled: "\x1b[31mplain\x1b[0m"}
		msg := readScreenLinesStyled(b, "s1", "/dir", 40)().(ScreenReadMsg)
		if !msg.StyledFetched {
			t.Error("StyledFetched should be true on success")
		}
		if msg.StyledContent != "\x1b[31mplain\x1b[0m" {
			t.Errorf("StyledContent = %q", msg.StyledContent)
		}
	})

	t.Run("styled error marks fetched with empty content", func(t *testing.T) {
		b := &styledStub{stubBackend: &stubBackend{}, plain: "fresh", styledErr: errors.New("boom")}
		msg := readScreenLinesStyled(b, "s1", "/dir", 40)().(ScreenReadMsg)
		// Fetched=true with empty content tells handleScreenRead to clear any
		// stale styled snapshot so the view falls back to the fresh plain read.
		if !msg.StyledFetched {
			t.Error("StyledFetched should be true even when the styled read fails")
		}
		if msg.StyledContent != "" {
			t.Errorf("StyledContent should be empty on error, got %q", msg.StyledContent)
		}
		if msg.Content != "fresh" {
			t.Errorf("plain Content should still be present, got %q", msg.Content)
		}
	})

	t.Run("non-styled backend leaves fetched false", func(t *testing.T) {
		msg := readScreenLinesStyled(&stubBackend{}, "s1", "/dir", 40)().(ScreenReadMsg)
		if msg.StyledFetched {
			t.Error("StyledFetched should be false when backend lacks StyledReader")
		}
	})
}
