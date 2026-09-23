package watch

import (
	"strconv"
	"strings"

	"github.com/sethdeckard/atria/libatria/agent"
	"github.com/sethdeckard/atria/libatria/terminal"
)

// DefaultScreenLines is how many screen lines Identify and the Watcher read
// when their options leave it unset. Codex pads its screen with blank lines
// and its prompt can sit twenty lines from the bottom, so fewer misses it.
const DefaultScreenLines = 40

// SkipReason says why Identify could not identify a session.
type SkipReason int

// Skip reasons.
const (
	SkipNone                  SkipReason = iota
	SkipUnknownTitleNoDir                // gated mode: title names no agent and no directory was found, so no screen read was spent
	SkipScreenReadFailed                 // the screen read that would have identified the agent failed; see Identity.Err
	SkipUnknownTitleAndScreen            // neither the title nor the screen names an agent
	SkipOutsideWatchDirs                 // gated mode: the directory found is not under any watch directory
)

// String returns a human-readable reason for debug logging.
func (r SkipReason) String() string {
	switch r {
	case SkipUnknownTitleNoDir:
		return "unknown title and empty dir"
	case SkipScreenReadFailed:
		return "screen read failed"
	case SkipUnknownTitleAndScreen:
		return "unknown title and screen"
	case SkipOutsideWatchDirs:
		return "outside watch dirs"
	default:
		return ""
	}
}

// IdentifyOptions configures Identify.
type IdentifyOptions struct {
	// WatchDirs, when non-empty, gates discovery: only sessions whose
	// directory is under one of them qualify. Empty means no gate.
	WatchDirs []string
	// ProjectDirs seeds the title-to-directory name match in
	// terminal.DiscoverCWD (gated mode only).
	ProjectDirs []string
	// ScreenLines is how many lines to read when the title gives nothing;
	// zero means DefaultScreenLines.
	ScreenLines int
}

// Identity is what Identify learned about a session.
type Identity struct {
	Type    agent.Type       // "" when no agent was identified
	Dir     string           // working directory, "" when not resolved
	Process terminal.Process // the agent process, zero when not resolved
	Gated   bool             // WatchDirs were given, so Dir had to validate against them
	Skip    SkipReason
	Err     error // set with SkipScreenReadFailed

	// Screen is the capture Identify read when the title gave nothing, and
	// Observed reports that such a read happened. A caller that starts
	// tracking the session should feed Screen to its first Observe rather
	// than start blind: the read may have consumed a pending bell, and it
	// saves a second read.
	Screen   string
	Observed bool
}

// OK reports whether the session can be tracked: an agent was identified,
// and in gated mode a directory under the watch list was found too.
func (id Identity) OK() bool {
	return id.Type != "" && (id.Dir != "" || !id.Gated)
}

// Identify decides whether sess is an agent session and in which directory.
//
// With watch directories it resolves the directory with terminal.DiscoverCWD
// and rejects one outside the list (DiscoverCWD's title-to-project fallback
// can return a project anywhere), then reads the agent from the title, and
// only when the title gives nothing and a directory was found spends a screen
// read on agent.InferFromScreen. When the agent process is found, its own cwd
// is the authority: in scope it becomes Dir, out of scope the session is
// skipped, whatever the shell's directory says. A known title with no
// directory is not a skip; OK is false and the caller decides.
//
// With no watch directories nothing is gated: the directory comes from the
// agent process's cwd when ResolveProcess finds one, else from
// GetVar("path"), and may be empty; an unknown title always gets a screen
// read.
//
// The process lookup is best-effort; when it resolves a cwd, that cwd can
// exclude the session from the watch directories.
func Identify(b terminal.Backend, sess terminal.Session, opts IdentifyOptions) Identity {
	lines := opts.ScreenLines
	if lines <= 0 {
		lines = DefaultScreenLines
	}
	id := Identity{Gated: len(opts.WatchDirs) > 0}

	if id.Gated {
		dir := terminal.DiscoverCWD(b, sess, opts.WatchDirs, opts.ProjectDirs)
		if dir != "" && !terminal.UnderAnyDir(dir, opts.WatchDirs) {
			id.Skip = SkipOutsideWatchDirs
			id.Type = agent.Detect(sess.Name)
			return id
		}
		id.Dir = dir
		id.Type = agent.Detect(sess.Name)
		if id.Type == "" {
			if dir == "" {
				id.Skip = SkipUnknownTitleNoDir
				return id
			}
			inferFromScreen(b, sess, lines, &id)
		}
		if id.Type != "" {
			id.Process, _ = resolveProcess(b, sess)
			if cwd := id.Process.Cwd; cwd != "" {
				if !terminal.UnderAnyDir(cwd, opts.WatchDirs) {
					id.Dir = ""
					id.Skip = SkipOutsideWatchDirs
					return id
				}
				id.Dir = cwd
			}
		}
		return id
	}

	proc, _ := resolveProcess(b, sess)
	id.Process = proc
	id.Dir = proc.Cwd
	if id.Dir == "" {
		if v, err := b.GetVar(sess.ID, "path"); err == nil {
			id.Dir = strings.TrimSpace(v)
		}
	}
	id.Type = agent.Detect(sess.Name)
	if id.Type == "" {
		inferFromScreen(b, sess, lines, &id)
	}
	return id
}

func inferFromScreen(b terminal.Backend, sess terminal.Session, lines int, id *Identity) {
	content, err := b.ReadScreen(sess.ID, lines)
	if err != nil {
		id.Skip = SkipScreenReadFailed
		id.Err = err
		return
	}
	id.Screen = content
	id.Observed = true
	id.Type = agent.InferFromScreen(content)
	if id.Type == "" {
		id.Skip = SkipUnknownTitleAndScreen
	}
}

// ResolveProcess finds the agent process behind a session: the processes on
// the session's TTY (or, for a backend that owns its child and reports no
// TTY, the TTY of the pid GetVar returns) narrowed by agent.FindProcess. It
// returns the zero Process and "" when nothing matches or the lookup fails,
// and is exported so a caller that hears about a session from a hook can
// refresh the process on demand.
func ResolveProcess(b terminal.Backend, sess terminal.Session) (terminal.Process, agent.Type) {
	tty := sess.TTY
	if tty == "" {
		v, err := b.GetVar(sess.ID, "pid")
		if err != nil {
			return terminal.Process{}, ""
		}
		pid, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || pid <= 0 {
			return terminal.Process{}, ""
		}
		tty = terminal.TTYForPID(pid)
		if tty == "" {
			return terminal.Process{}, ""
		}
	}
	procs, err := terminal.ProcessesOnTTY(tty)
	if err != nil {
		return terminal.Process{}, ""
	}
	p, typ, ok := agent.FindProcess(procs)
	if !ok {
		return terminal.Process{}, ""
	}
	return p, typ
}
