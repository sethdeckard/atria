package tui

import (
	"os"
	"path/filepath"
	"strings"
)

// GitInfo holds git worktree metadata for a directory.
type GitInfo struct {
	IsWorktree bool
	ParentRepo string // basename of main repo
	Branch     string // worktree branch name
}

// detectGitWorktree checks if dir is a git worktree (not a regular repo).
// Worktrees have a .git file (not dir) containing "gitdir: /path/to/main/.git/worktrees/<name>".
func detectGitWorktree(dir string) GitInfo {
	gitPath := filepath.Join(dir, ".git")
	info, err := os.Lstat(gitPath)
	if err != nil || info.IsDir() {
		return GitInfo{}
	}

	data, err := os.ReadFile(gitPath)
	if err != nil {
		return GitInfo{}
	}

	content := strings.TrimSpace(string(data))
	if !strings.HasPrefix(content, "gitdir: ") {
		return GitInfo{}
	}

	gitdir := strings.TrimPrefix(content, "gitdir: ")
	// Expected format: /path/to/main/.git/worktrees/<branch>
	// Find ".git/worktrees/" in the path
	idx := strings.Index(gitdir, "/.git/worktrees/")
	if idx < 0 {
		return GitInfo{}
	}

	mainRepo := gitdir[:idx]
	branch := gitdir[idx+len("/.git/worktrees/"):]

	return GitInfo{
		IsWorktree: true,
		ParentRepo: filepath.Base(mainRepo),
		Branch:     branch,
	}
}
