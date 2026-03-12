# atria demo recording
# Prerequisites:
#   - atria on PATH
#   - ~/.config/atria/config.toml with watch directories
#   - 3+ agent sessions running (mix of claude/codex, varied statuses)
# Generate: vhs demo.tape

Output demo.gif
Set Shell zsh
Set FontSize 14
Set Width 1200
Set Height 700
Set Padding 20
Set Theme "Catppuccin Mocha"

# Launch atria
Type "atria"
Enter
Sleep 6s

# Navigate the agent list — show different statuses
Down
Sleep 1s
Down
Sleep 1s

# Toggle stream panel — show live terminal output
Type "v"
Sleep 3s

# Close stream, open chat view
Type "v"
Sleep 0.5s
Enter
Sleep 1.5s

# Type a prompt and send
Type "add unit tests for the config parser"
Sleep 1s
Ctrl+d
Sleep 10s

# Back to dashboard
Escape
Sleep 3s
