#!/usr/bin/env bash
# demo/seed.sh — scaffold demo data for the atria demo tape.
#
# Creates 4 throwaway git projects under ../atria.demo-projects and a tmux
# session 'atria-demo' with one labeled window per agent you'll launch — each
# cd'd into the right project with the launch command pre-typed (review it and
# press Enter to start the REAL agent). A final 'atria' window runs atria against
# an isolated config (tmux integration) so it discovers the agents without
# touching your real ~/.config/atria.
#
# Workflow:
#   ./demo/seed.sh                              # create dirs + tmux session
#   tmux -L atria-demo attach -t atria-demo     # launch each agent (Enter), arrange states
#   vhs demo/atria.tape                         # record browsing + the stream panel
#   ./demo/seed.sh --teardown                   # kill the session and remove the tree
#
# Requirements: tmux, and the real agent CLIs (claude/codex/copilot/opencode)
# on PATH. The agent windows use your real $HOME so the agents authenticate
# normally; only the atria window is pointed at the throwaway config.

set -euo pipefail

# Isolate git from global/system config so hooks/signing can't abort the seed.
export GIT_CONFIG_GLOBAL=/dev/null
export GIT_CONFIG_NOSYSTEM=1
export GIT_TERMINAL_PROMPT=0

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
DEMO_ROOT="$(cd "$REPO_ROOT/.." && pwd)/atria.demo-projects"
DEMO_HOME="$DEMO_ROOT/.home"     # throwaway $HOME → isolated atria config
SESSION="atria-demo"

# Run atria built from current source, not whatever's on PATH — a stale release
# (e.g. an older Homebrew build) predates the terminal-color feature this tape
# exists to show off, so it would render agent screens monochrome.
ATRIA_BIN="$REPO_ROOT/atria"

# Dedicated tmux socket + no user config so the demo is isolated from the user's
# real tmux server. atria, run inside this server, inherits $TMUX and targets
# the same socket automatically.
TMUX_BIN="${TMUX_BIN:-tmux}"
TM=("$TMUX_BIN" -L "$SESSION" -f /dev/null)

# Projects: "lang name" — one throwaway git repo each, shown in atria as
# "lang/name" (atria contracts to a 2-segment path).
PROJECTS=(
    "go spaceship-api"
    "ruby quantum-todo"
    "rust nebula-cli"
    "swift aurora-notes"
)

# Roster: "lang|project|agent" — one labeled tmux window each, with the agent
# command pre-typed (not run). Edit here to change the matrix.
ROSTER=(
    "go|spaceship-api|claude"
    "go|spaceship-api|codex"
    "ruby|quantum-todo|copilot"
    "rust|nebula-cli|opencode"
    "swift|aurora-notes|claude"
    "swift|aurora-notes|codex"
)

DRY_RUN=0
TEARDOWN=0

usage() {
    cat <<EOF
Usage: $0 [--dry-run] [--teardown]

  --dry-run    Print what would happen without touching anything.
  --teardown   Kill the tmux session and remove the demo tree, then exit.
  -h, --help   Show this message.
EOF
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --dry-run)  DRY_RUN=1; shift ;;
        --teardown) TEARDOWN=1; shift ;;
        -h|--help)  usage; exit 0 ;;
        *)          echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
    esac
done

manifest_for() {
    case "$1" in
        go)    echo "go.mod" ;;
        ruby)  echo "Gemfile" ;;
        rust)  echo "Cargo.toml" ;;
        swift) echo "Package.swift" ;;
        *)     echo "" ;;
    esac
}

manifest_body() {
    local lang=$1 name=$2
    case "$lang" in
        go)    printf 'module demo/%s\n\ngo 1.23\n' "$name" ;;
        ruby)  printf 'source "https://rubygems.org"\n\ngem "rake"\n' ;;
        rust)  printf '[package]\nname = "%s"\nversion = "0.1.0"\nedition = "2021"\n' "$name" ;;
        swift) printf '// swift-tools-version:5.9\nimport PackageDescription\n\nlet package = Package(name: "%s")\n' "$name" ;;
    esac
}

if [[ $TEARDOWN -eq 1 ]]; then
    if [[ $DRY_RUN -eq 1 ]]; then
        echo "DRY RUN — would kill tmux server '$SESSION' and remove $DEMO_ROOT"
        exit 0
    fi
    "${TM[@]}" kill-server 2>/dev/null || true
    rm -rf "$DEMO_ROOT"
    echo "tore down session '$SESSION' and removed $DEMO_ROOT"
    exit 0
fi

# ---------------------------------------------------------------------------
# Dry run.
if [[ $DRY_RUN -eq 1 ]]; then
    echo "DRY RUN — would create $DEMO_ROOT with projects:"
    for p in "${PROJECTS[@]}"; do
        read -r lang name <<<"$p"
        printf '  %s/%s  (%s)\n' "$lang" "$name" "$(manifest_for "$lang")"
    done
    echo
    echo "would build atria from source ($REPO_ROOT) -> $ATRIA_BIN"
    echo "would start tmux session '$SESSION' with windows (agent pre-typed):"
    for row in "${ROSTER[@]}"; do
        IFS='|' read -r lang name agent <<<"$row"
        printf '  %-9s %-22s  $ %s\n' "$agent" "$lang/$name" "$agent"
    done
    printf '  %-9s %s\n' "atria" "(runs $ATRIA_BIN against $DEMO_HOME config)"
    echo
    echo "(no filesystem or tmux changes made)"
    exit 0
fi

# ---------------------------------------------------------------------------
# Projects.
echo "removing any existing session and tree"
"${TM[@]}" kill-server 2>/dev/null || true
rm -rf "$DEMO_ROOT"

echo "seeding projects under $DEMO_ROOT"
for p in "${PROJECTS[@]}"; do
    read -r lang name <<<"$p"
    dir="$DEMO_ROOT/$lang/$name"
    mkdir -p "$dir"

    manifest=$(manifest_for "$lang")
    [[ -n "$manifest" ]] && manifest_body "$lang" "$name" > "$dir/$manifest"
    printf '# %s\n\nDemo project for the atria tape.\n' "$name" > "$dir/README.md"

    git -C "$dir" init -q --initial-branch=main
    git -C "$dir" config user.email "demo@atria.local"
    git -C "$dir" config user.name "Atria Demo"
    git -C "$dir" config commit.gpgsign false
    git -C "$dir" add -A
    GIT_AUTHOR_DATE="2024-01-15T10:00:00" GIT_COMMITTER_DATE="2024-01-15T10:00:00" \
        git -C "$dir" commit -q -m "Initial commit"
done

# Isolated atria config: a throwaway $HOME so the demo never touches the real
# ~/.config/atria. tmux integration is what discovers the agent panes.
mkdir -p "$DEMO_HOME/.config/atria"
cat > "$DEMO_HOME/.config/atria/config.toml" <<EOF
watch_dirs = ["$DEMO_ROOT"]
integrations = ["tmux"]
update_check = false
EOF

# ---------------------------------------------------------------------------
# Build atria from current source so the demo shows the current color feature,
# not a stale release on PATH (set -e aborts the seed if the build fails).
echo "building atria from source -> $ATRIA_BIN"
(cd "$REPO_ROOT" && make build)

# ---------------------------------------------------------------------------
# tmux session: one labeled window per agent, cd'd into its project, with the
# launch command pre-typed (not run). Agent windows use the real $HOME.
echo "starting tmux session '$SESSION'"
first=1
for row in "${ROSTER[@]}"; do
    IFS='|' read -r lang name agent <<<"$row"
    dir="$DEMO_ROOT/$lang/$name"
    win="$agent $lang/$name"
    if [[ $first -eq 1 ]]; then
        pane=$("${TM[@]}" new-session -d -s "$SESSION" -n "$win" -c "$dir" -P -F '#{pane_id}')
        first=0
    else
        pane=$("${TM[@]}" new-window -t "$SESSION" -n "$win" -c "$dir" -P -F '#{pane_id}')
    fi
    # Pre-type the launch command so you just review and press Enter.
    "${TM[@]}" send-keys -t "$pane" -l "$agent"
done

# 256-color terminal; keep the status bar ON so the window labels are visible
# while you set up (the tape turns it off before recording). Force RGB so the
# agents' truecolor output passes through to whatever records the tape, even if
# the attaching client doesn't advertise truecolor itself.
"${TM[@]}" set-option -g default-terminal tmux-256color
"${TM[@]}" set-option -ga terminal-features '*:RGB'

# atria itself (built from source above), against the isolated config (throwaway HOME).
"${TM[@]}" new-window -t "$SESSION" -n atria -c "$REPO_ROOT" -e "HOME=$DEMO_HOME" "$ATRIA_BIN"

# Start on the first agent window for setup.
"${TM[@]}" select-window -t "$SESSION:0"

cat <<EOF

demo ready. Next:
  1) tmux -L $SESSION attach -t $SESSION
  2) In each labeled window, press Enter to launch the real agent and get it
     into an interesting state (working / waiting on a prompt / idle):
EOF
for row in "${ROSTER[@]}"; do
    IFS='|' read -r lang name agent <<<"$row"
    printf '       %-9s %s\n' "$agent" "$lang/$name"
done
cat <<EOF
  3) Switch to the 'atria' window to confirm they're discovered.
  4) Record:    vhs demo/atria.tape
  5) Tear down: ./demo/seed.sh --teardown
EOF
