package libatria_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/sethdeckard/atria/libatria"
	"github.com/sethdeckard/atria/libatria/agent"
	"github.com/sethdeckard/atria/libatria/watch"
)

// Open the terminals named in your configuration, list the agent sessions
// they expose, and stream status changes until interrupted.
func Example() {
	stack, err := libatria.Open(libatria.Options{
		Integrations: []string{libatria.Tmux, libatria.WezTerm},
		ProgramName:  "agent-dashboard",
	})
	if err != nil {
		log.Fatal(err)
	}

	for _, st := range stack.Statuses() {
		if st.Enabled && !st.Available {
			fmt.Printf("%s: %s\n", st.Name, st.Reason)
		}
	}

	sessions, err := stack.Backend().ListSessions()
	if err != nil {
		stack.Close()
		log.Fatal(err)
	}
	for _, s := range sessions {
		if t := agent.Detect(s.Name); t != "" {
			fmt.Printf("%s %s %q\n", s.ID, t, s.Name)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	w := watch.New(stack.Backend(), watch.Options{})
	go func() {
		for ev := range w.Events() {
			if ev.Kind == watch.StatusChanged {
				fmt.Printf("%s %s -> %s\n", ev.Session.ID, ev.From, ev.To)
			}
		}
	}()
	if err := w.Run(ctx); err != nil && ctx.Err() == nil {
		log.Print(err)
	}
	stack.Close()
}
