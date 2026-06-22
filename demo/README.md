# atria demo scaffold

Reproducible demo data for recording the [VHS](https://github.com/charmbracelet/vhs)
tape, using **real agents** in throwaway projects — no fakes, and your real
`~/.config/atria` is never touched.

## What `seed.sh` creates

A sibling tree `../atria.demo-projects/` with four throwaway git projects, and an
isolated tmux server (socket `-L atria-demo`, no user config) with one **labeled
window per agent** — each `cd`'d into the right project with the launch command
**pre-typed** (you review it and press Enter to start the real agent). A final
`atria` window runs atria against a throwaway config (`integrations = ["tmux"]`)
so it discovers the agents without touching your real config.

The intended matrix (edit the `PROJECTS`/`ROSTER` tables in `seed.sh` to change):

| Project (shown in atria) | Agent |
|---|---|
| `go/spaceship-api` | claude |
| `go/spaceship-api` | codex |
| `ruby/quantum-todo` | copilot |
| `rust/nebula-cli` | opencode |
| `swift/aurora-notes` | claude |
| `swift/aurora-notes` | codex |

Four projects, six agents, all four agent types; two projects run two agents to
show atria's multiple-agents-per-directory support.

## Requirements

- [`vhs`](https://github.com/charmbracelet/vhs) (`brew install vhs`)
- `tmux`
- The real agent CLIs on PATH (`claude`, `codex`, `copilot`, `opencode`)

`seed.sh` builds atria from the current source (`make build`) and runs that binary
directly, so the recording always exercises the current feature set — no `make
install` and no dependence on whatever `atria` happens to be first on your PATH.

## Record

```sh
# 1. Scaffold projects + the labeled tmux session
./demo/seed.sh

# 2. Attach and set the starting state: in each labeled window press Enter to
#    launch the real agent, and get a nice mix of states (working / waiting on
#    a prompt / idle). The window names tell you which agent goes where.
tmux -L atria-demo attach -t atria-demo
#    Switch to the 'atria' window to confirm they're discovered, then detach
#    with C-b d (atria keeps running).

# 3. Record browsing the list + the streaming panel
vhs demo/atria.tape

# 4. Tear down (kills the session and removes the demo tree)
./demo/seed.sh --teardown
```

`./demo/seed.sh --dry-run` prints the plan without touching anything.

## Notes

- The agent windows use your **real `$HOME`** so the agents authenticate
  normally; only the `atria` window is pointed at the throwaway config.
- atria runs from the source build (`../atria`), not PATH — a stale release
  (e.g. an older Homebrew build) predates the terminal-color feature this tape
  exists to show off and would render agent screens monochrome.
- The tape doesn't launch agents or build atria — it just attaches, hides the
  tmux status bar, and drives the dashboard + stream panel. Set up the agents
  first; re-run `vhs demo/atria.tape` as many times as you like.
- `vhs` attaches with `-d`, so detach your own client first (or let it bump you).
