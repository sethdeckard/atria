package terminal

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// TrimScreenTail keeps at most the last n lines, anchored to the last
// nonblank line rather than the absolute bottom of the screen. Blankness is
// measured after stripping ANSI escapes, so styled rows that hold only SGR
// sequences plus spaces (common as bottom padding in heavily-padded TUIs) are
// correctly treated as blank. For plain text the strip is a no-op.
func TrimScreenTail(text string, n int) string {
	allLines := strings.Split(text, "\n")
	if n > 0 && len(allLines) > n {
		end := len(allLines)
		for i := len(allLines) - 1; i >= 0; i-- {
			if strings.TrimSpace(ansi.Strip(allLines[i])) != "" {
				end = i + 1
				break
			}
		}
		start := end - n
		if start < 0 {
			start = 0
		}
		allLines = allLines[start:end]
	}
	return strings.Join(allLines, "\n")
}
