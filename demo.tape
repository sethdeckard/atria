# atria demo recording
# Prerequisites:
#   - atria built and on PATH
#   - ~/.config/atria/config.toml with watch directories and WezTerm integration enabled
#   - 2+ agent sessions running in WezTerm panes (discovered via integration)
#   - 2+ embedded PTY agents already running
#   - Mix of statuses across agents (idle, working, needs_input) for visual variety
# Generate: vhs demo.tape

Output demo.gif
Set Shell zsh
Set FontSize 14
Set Width 1200
Set Height 700
Set Padding 20
Set Theme "Catppuccin Mocha"

# 1. Launch and dashboard (~6s)
Type "atria"
Enter
Sleep 6s

# 2. Navigate the agent list (~4s)
Down
Sleep 1.5s
Down
Sleep 1.5s
Up
Sleep 1s

# 3. Stream panel + navigate (~8s)
Type "v"
Sleep 2s
Down
Sleep 2s
Down
Sleep 2s
Up
Sleep 2s

# 4. Launch new embedded agent with stream open (~6s)
Type "n"
Sleep 1.5s
Enter
Sleep 1s
Down
Sleep 1s
Enter
Sleep 2.5s

# 5. Close stream panel
Type "v"
Sleep 0.5s

# 6. Chat view (~10s)
Up
Up
Sleep 0.5s
Enter
Sleep 1.5s
Type "add unit tests for the config parser"
Sleep 1s
Ctrl+d
Sleep 7s

# 7. Back to dashboard and quit (~3s)
Escape
Sleep 2s
Type "q"
Sleep 1s
