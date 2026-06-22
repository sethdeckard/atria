# atria demo recording — browsing the dashboard + the streaming panel.
#
# Prerequisites (one-time, before recording):
#   ./demo/seed.sh                            # build atria from source, create
#                                             # dirs + the tmux session (the
#                                             # 'atria' window runs ../atria)
#   tmux -L atria-demo attach -t atria-demo   # launch the real agent in each
#                                             # labeled window, arrange states
#   (detach with C-b d; the 'atria' window keeps running)
#
# Then, from the repo root:  vhs demo/atria.tape
# Tear down when done:       ./demo/seed.sh --teardown

Output demo.gif
Set Shell zsh
Set FontSize 14
Set Width 1200
Set Height 700
Set Padding 20
Set Theme "Catppuccin Mocha"

# --- Enter the demo (hidden): hide the tmux status bar, focus atria, attach ---
Hide
Type "tmux -L atria-demo set -g status off" Enter
Type "tmux -L atria-demo select-window -t atria-demo:atria" Enter
Type "tmux -L atria-demo attach -d -t atria-demo" Enter
Sleep 3s
Show

# The stream panel starts closed; we open it right away and close it at the end
# so the recording is re-runnable (it ends in the same state it began).

# --- 1. Move to the second agent ---------------------------------------------
# Cursor can start mid-list, so go to the top first (Up clamps at row 0),
# then down one to land on the second agent. Slight pause before opening.
Sleep 1s
Up 8
Down
Sleep 1.5s

# --- 2. Open the stream panel on the second agent and linger -----------------
Type "v"
Sleep 5s

# --- 3. Walk down the stack and back up (panel open) -------------------------
Down
Sleep 2.5s
Down
Sleep 2.5s
Down
Sleep 2.5s
Down
Sleep 2.5s
Up
Sleep 2.5s
Up
Sleep 2.5s
Up
Sleep 2.5s
Up
Sleep 2.5s

# --- 4. Close the panel ------------------------------------------------------
Type "v"
Sleep 1s

# --- Leave (hidden): detach, leaving atria + agents running for re-recording --
Hide
Ctrl+b
Type "d"
Sleep 1s
