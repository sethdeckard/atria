package project

import (
	"os"
	"path/filepath"
	"sort"
)

// Discover scans the given watch directories and returns all immediate subdirectories
// that are themselves directories (potential project roots).
// Returns absolute paths, sorted. Skips hidden directories (starting with .).
func Discover(watchDirs []string) []string {
	var projects []string
	for _, dir := range watchDirs {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		entries, err := os.ReadDir(absDir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name := entry.Name()
			if len(name) > 0 && name[0] == '.' {
				continue
			}
			projects = append(projects, filepath.Join(absDir, name))
		}
	}
	sort.Strings(projects)
	return projects
}
