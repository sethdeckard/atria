# libatria

libatria is the importable part of atria: terminal discovery, agent detection, status classification, and a watcher that follows agent sessions over time. It lives in the atria module at `github.com/sethdeckard/atria/libatria`, shares atria's version, and exists for programs like atria itself and for daemons that drive Claude and Codex sessions remotely.

This is the guide. [API.md](API.md) is the reference for the whole surface, and `go doc github.com/sethdeckard/atria/libatria/...` has the per-identifier detail.

## Install

```
go get github.com/sethdeckard/atria@latest
```

The module needs Go 1.26. Importing `libatria/terminal/pty` pulls in creack/pty and vt10x, and `libatria/terminal/iterm` pulls in gorilla/websocket and protobuf; the other clients are stdlib plus `charmbracelet/x/ansi`. Nothing under `libatria/` depends on Bubble Tea.

## Quick Start

Open the terminals you want to look inside, then hold the backend it returns:

```go
stack, err := libatria.Open(libatria.Options{
    Integrations: []string{libatria.Tmux, libatria.WezTerm},
    ProgramName:  "agent-dashboard",
})
if err != nil {
    return err
}
defer stack.Close()
b := stack.Backend()
```

`Open` doesn't fail when a terminal is missing. Each named integration is probed, and the ones that don't answer are recorded with a reason:

```go
for _, st := range stack.Statuses() {
    if st.Enabled && !st.Available {
        log.Printf("%s: %s", st.Name, st.Reason)
    }
}
```

List sessions and pick out the agents. A session's title usually names the agent; when it doesn't, the screen does:

```go
sessions, err := b.ListSessions()
if err != nil {
    return err
}
for _, s := range sessions {
    screen, _ := b.ReadScreen(s.ID, 40)
    t := agent.Detect(s.Name)
    if t == "" {
        t = agent.InferFromScreen(screen)
    }
    if t == "" {
        continue
    }
    status, line := agent.ClassifyScreen(screen, t)
    fmt.Println(s.ID, t, status, line)
}
```

Read 40 lines or more. Codex pads its screen with blank lines and its prompt can sit twenty lines from the bottom; fewer misses it.

Send a prompt, answer a permission dialog, and start a new agent:

```go
_ = agent.SendPrompt(b, id, "run the tests", agent.Claude)
_ = terminal.SendKey(b, id, terminal.KeyEscape)
_ = terminal.SendKey(b, id, terminal.Key("1"))

newID, err := agent.Launch(b, stack.PrimarySource(), "/Users/me/projects/api", "claude")
```

`SendPrompt` writes the text, waits 50ms, then writes a carriage return on its own, because the raw-mode TUIs these agents run drop a newline that arrives with the text. `Launch` creates a session on the primary, focuses it, waits 300ms for the shell, and runs `cd '<dir>' && <cmd>`. Pass the directory to `Launch` or build the command with `agent.LaunchCommand`, not both, or you get two `cd`s.

## Watching Sessions

`watch.Watcher` runs the discovery and status loop in a goroutine and delivers events on a channel. This prints every session it finds, with the agent process behind it, then streams status changes:

```go
w := watch.New(b, watch.Options{})
go func() {
    for ev := range w.Events() {
        switch ev.Kind {
        case watch.SessionAdded:
            fmt.Printf("+ %s %s pid=%d %v\n", ev.Session.ID, ev.Session.Type, ev.Session.Process.PID, ev.Session.Process.Argv)
        case watch.StatusChanged:
            fmt.Printf("  %s %s -> %s %q\n", ev.Session.ID, ev.From, ev.To, ev.MatchLine)
        case watch.SessionRemoved:
            fmt.Printf("- %s (%s)\n", ev.Session.ID, ev.Reason)
        }
    }
}()
err := w.Run(ctx) // blocks until ctx is cancelled and closes Events on return
```

Sends block when the channel is full rather than dropping an event, because a lost needs_input event is an agent waiting for someone who never comes. Drain the channel from its own goroutine. Cancellation is checked between backend calls and during event delivery; a send racing with cancellation may still succeed, and events already buffered stay readable after `Run` returns and closes the channel.

With `Options.WatchDirs` set, only sessions whose directory is under one of them are tracked. With none, every agent session the backend lists is a candidate; a daemon usually wants that and a dashboard usually doesn't. `Filter` and `Agents` narrow it further.

`Tracker` and `Identify` are the pieces under the Watcher, exported for programs with an event loop of their own. atria drives one `Tracker` per session from its Bubble Tea loop instead of running a Watcher.

## Which Terminal Launches

Every terminal you name contributes the sessions it can see. One of them is the primary and takes `NewSession` and `Launch`: the highest-ranked integration whose probe passed and whose environment says you're running inside it, in the order deviceterm, tmux, kitty, wezterm, iterm2, with the built-in PTY as the fallback. `Statuses` marks it with `Launch`, and `PrimarySource` names it.

Session IDs depend on role. The primary's sessions are listed bare; every other backend's carry a `source:` prefix (`tmux:%3`, `pty:pty-0`). `Enable`, `Disable`, and `Reprobe` change the set at runtime and return a `RoleChange` whenever the primary changed, because the backend that swapped roles now lists its sessions under different IDs. Rewrite anything you hold by ID from `RoleChange.Source`, `Prefix`, and `ToPrefixed`, or you'll address the wrong session after a toggle.

`Enable` and `Reprobe` build clients that never open the iTerm2 AppleScript dialog. `AllowITermPrompt` applies to the client `Open` builds inside iTerm2; that client keeps the setting and can prompt again if it reconnects and iTerm2 demands auth.

## Terminal Requirements and Caveats

### iTerm2

Enable the Python API (Settings > General > Magic > Enable Python API). The client speaks protobuf over the API's Unix socket; nothing else is installed.

Authentication is the sharp edge. Inside iTerm2, `Open` with `AllowITermPrompt` can request credentials through AppleScript, which shows a macOS Automation dialog; do that at startup, before any UI of your own. Everywhere else interactive authentication is suppressed. Credentials already in the environment are still used; without them the connection works only when iTerm2's automation auth is disabled: create `~/.config/iterm2/disable-automation-auth`.

The client reads `ITERM2_COOKIE` and `ITERM2_KEY` from its own environment and then unsets them, so processes it launches can't inherit the credentials. If your program needs them afterwards, read them first.

A dropped socket is reconnected on the next call, and calls in between return `ErrUnavailable`.

### tmux

Sessions are discovered across every tmux session with `list-panes -a`. The pane ID (`%3`) is the session ID, and `allow-rename on` (the tmux default) is what lets an agent's title escape reach `pane_title`.

With no tmux server running, `ListSessions` returns an empty list rather than an error, because a server that exited took every session with it. Permission denied, connection refused, and timeouts are `ErrUnavailable`.

Text goes through `send-keys -l`. A write that is exactly `\r` or `\n` is sent as the tmux `Enter` key, and semicolons are escaped so tmux's own parser doesn't eat them. `SendKey` uses tmux key names (`Escape`, `C-c`, `Up`) rather than raw bytes. Launches go into the current session when you're inside tmux and into a detached session named after `ProgramName` otherwise. `FocusSession` does nothing outside tmux.

### Kitty

Requires `allow_remote_control yes` and `listen_on unix:/tmp/kitty-{kitty_pid}` in `kitty.conf`. The probe needs a reachable remote-control socket in `KITTY_LISTEN_ON`, which Kitty sets for the processes it runs; it lists windows over that socket and reports which piece is missing.

### WezTerm

Needs a running WezTerm instance; `wezterm cli` finds the socket through `WEZTERM_UNIX_SOCKET`. Pane IDs are session IDs, and the listing carries title, cwd, and TTY, so discovery needs no `ps` lookup. The probe runs `wezterm cli list`.

### DeviceTerm

Needs DeviceTerm 0.11.0 or later and a process running inside an Automation tab (Shell ▸ Open Automation Tab, ⇧⌘T). The daemon authenticates each `deviceterm` call by walking the process ancestry back to that tab, so don't spawn your program detached (`setsid`, a launcher that reparents), don't nest it in tmux, and expect `DEVICETERM_SESSION` in the real environment: the client reads `os.Getenv`, not `Options.Getenv`.

DeviceTerm is the primary or absent, never a discovery integration. A granted Automation tab is never inside another terminal, and outside one the CLI can't capture, send, or focus, so it would list sessions it couldn't service.

A grant lost mid-run (the GUI connection went away) fails every refresh until the program restarts in a new Automation tab. The `cwd` it reports is best-effort and can be empty. There is no version check; an older CLI fails the grant check with `cli.invalidUsage`, which the reason text translates to "requires DeviceTerm 0.11.0 or later".

### PTY

The built-in backend: each session is a child process in a pseudo-terminal behind a vt10x emulator, with no external dependency. Screen reads come from memory in under a millisecond. Sessions die with your process. `Close` closes each PTY and sends the process SIGTERM; a process that ignores SIGTERM can outlive it.

The emulator holds `pty.DefaultRows` lines (40, or `Options.PTYRows`), so a read for more than that returns the whole buffer and no more. A bell rung since the last read is delivered as a `\x07` prefixed to the plain `ReadScreen` result. The styled read doesn't carry it (it would ring the viewer's own terminal), so a program that reads only styled screens should ask `terminal.BellSource.ConsumeBell`, which is what `watch.Options.StyledScreens` does.

## Terminal Loss and Recovery

Every client reports a connection-level failure so that `errors.Is(err, terminal.ErrUnavailable)` holds: a socket that won't dial or has closed, a tmux server refusing connections, a DeviceTerm `transport.*` error, and any command timeout. The CLI clients bound every subprocess with `Options.CommandTimeout` (5s by default), and a timeout also satisfies `errors.Is(err, context.DeadlineExceeded)`.

A composite `ListSessions` fails only when the primary fails. An integration's failure is reported through `terminal.FailureReporter`, and the Watcher keeps that source's sessions rather than removing them, so a terminal restart doesn't look like every agent exiting. Sessions that are gone for real are removed once their source lists successfully again.

Transport failures recover without a restart; DeviceTerm's grant loss is the exception and needs a new Automation tab. The iTerm2 client reconnects on its next call, the CLI clients spawn a fresh process per call, and `Stack.Reprobe` retries integrations whose probe failed at `Open`, adding or promoting the ones that now answer.

## Concurrency

Every client, `CompositeBackend`, `CachedBackend`, and `Stack` is safe for concurrent use. `Enable`, `Disable`, and `Reprobe` can run while a Watcher polls `stack.Backend()`; sessions whose IDs change role are removed and rediscovered under the new ID. The iTerm2 connection serializes its round trips, so parallel iTerm2 reads queue rather than fail.

`Backend` methods don't take a `context.Context` in v0.x. `CommandTimeout` bounds each CLI invocation and each iTerm2 request-response exchange, not a whole backend call: a method may run several, the iTerm2 dial and AppleScript request have no deadline, and PTY writes have none. Context-taking methods are the one change already planned for v1.

## What atria Does With It

atria is the first consumer and a fair template. `main.go` calls `libatria.Open` with the config's integration list, `ProgramName: "atria"`, and `AllowITermPrompt: true` (startup runs before the alt screen, so a dialog is safe there), then shows `Statuses` on its settings screen. Toggling an integration saves the config and calls `Enable` or `Disable`, and the `RoleChange` becomes a rewrite of tracked session IDs.

Each tracked session embeds a `watch.Tracker`, fed by the Bubble Tea loop: screen reads go to `Observe`, listings to `Refresh`, and `PollInterval` decides how soon to read again. Discovery goes through `watch.Identify`, gated on `watch_dirs` or, with none configured, on the known project directories, so an agent in an unrelated directory isn't picked up. Launching is `agent.Launch` with the primary source, and the chat view sends through `agent.SendPrompt`. atria doesn't run a Watcher; its event loop is Bubble Tea's.

## Stability

libatria ships inside the atria module and takes atria's version. While the module is v0.x, a minor release may change the libatria API, and every breaking change is listed in CHANGELOG.md under that release.

`terminal.Backend`, `terminal.Session`, `agent.Type`, `agent.Status`, and `watch.Event` are the surfaces most likely to hold still. The `Options` structs will grow fields. One change is already planned for v1: `Backend` methods will take a `context.Context`, and until then `CommandTimeout` bounds each CLI invocation and iTerm2 request-response exchange. The concurrency guarantees above are part of the contract from v0.7.0. A `v1.0.0` tag freezes the API.
