package watch

import "time"

// Clock is the time source the Watcher schedules on. The zero Options use
// the wall clock; tests supply their own so a poll cycle can be driven
// without sleeping.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }
