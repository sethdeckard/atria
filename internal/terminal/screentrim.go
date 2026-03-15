package terminal

import "strings"

// TrimScreenTail keeps at most the last n lines, anchored to the last
// nonblank line rather than the absolute bottom of the screen.
func TrimScreenTail(text string, n int) string {
	allLines := strings.Split(text, "\n")
	if n > 0 && len(allLines) > n {
		end := len(allLines)
		for i := len(allLines) - 1; i >= 0; i-- {
			if strings.TrimSpace(allLines[i]) != "" {
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
