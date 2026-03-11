package tui

// Version is set by main.go from build-time ldflags.
var Version = "dev"

// Logo is the standard figlet-font ASCII art for Atria, shared across
// the version flag, empty state, and setup wizard.
const Logo = `        _        _
   __ _| |_ _ __(_) __ _
  / _` + "`" + ` | __| '__| |/ _` + "`" + ` |
 | (_| | |_| |  | | (_| |
  \__,_|\__|_|  |_|\__,_|`
