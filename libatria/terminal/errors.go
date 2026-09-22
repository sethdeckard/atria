package terminal

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrUnavailable marks a failure to reach the terminal application at all:
// its socket is gone, its server exited, its CLI cannot connect, or a call
// timed out. Every backend wraps such failures so errors.Is(err,
// ErrUnavailable) holds, which lets a caller tell "the terminal is gone" from
// an ordinary per-session error and keep tracked sessions instead of dropping
// them. A timeout additionally satisfies errors.Is(err,
// context.DeadlineExceeded).
var ErrUnavailable = errors.New("terminal unavailable")

// DefaultCommandTimeout is the default limit for CLI commands, the ps/lsof
// helpers, and iTerm2 socket round trips when a client's options leave the
// timeout unset.
const DefaultCommandTimeout = 5 * time.Second

// Unavailable wraps cause as an ErrUnavailable failure of op. A nil cause
// produces "op: terminal unavailable".
func Unavailable(op string, cause error) error {
	if cause == nil {
		return fmt.Errorf("%s: %w", op, ErrUnavailable)
	}
	return fmt.Errorf("%s: %w: %w", op, ErrUnavailable, cause)
}

// Timeout reports that op exceeded its deadline. The result satisfies both
// ErrUnavailable and context.DeadlineExceeded.
func Timeout(op string, limit time.Duration) error {
	return fmt.Errorf("%s: %w after %s: %w", op, ErrUnavailable, limit, context.DeadlineExceeded)
}

// PipeGrace is how long a timed-out subprocess's stdout and stderr are given
// to close after it is killed before they are forced shut. Without it, a
// descendant that inherited the pipes (a shell script's child, say) would
// keep the caller waiting past the timeout.
const PipeGrace = 200 * time.Millisecond

// TimeoutOr returns opts if positive and DefaultCommandTimeout otherwise.
func TimeoutOr(opts time.Duration) time.Duration {
	if opts > 0 {
		return opts
	}
	return DefaultCommandTimeout
}
