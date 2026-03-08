package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectGitWorktree_RegularRepo(t *testing.T) {
	dir := t.TempDir()
	// Regular repo: .git is a directory
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	info := detectGitWorktree(dir)
	if info.IsWorktree {
		t.Error("expected regular repo to not be detected as worktree")
	}
}

func TestDetectGitWorktree_Worktree(t *testing.T) {
	dir := t.TempDir()
	// Worktree: .git is a file pointing to main repo's worktrees dir
	content := "gitdir: /home/user/projects/myapp/.git/worktrees/feature-branch"
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	info := detectGitWorktree(dir)
	if !info.IsWorktree {
		t.Fatal("expected worktree to be detected")
	}
	if info.ParentRepo != "myapp" {
		t.Errorf("expected parent repo 'myapp', got %q", info.ParentRepo)
	}
	if info.Branch != "feature-branch" {
		t.Errorf("expected branch 'feature-branch', got %q", info.Branch)
	}
}

func TestDetectGitWorktree_NoGit(t *testing.T) {
	dir := t.TempDir()
	info := detectGitWorktree(dir)
	if info.IsWorktree {
		t.Error("expected no .git to not be detected as worktree")
	}
}

func TestDetectGitWorktree_BadGitFile(t *testing.T) {
	dir := t.TempDir()
	// .git file with unexpected content
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("not a gitdir line"), 0o644); err != nil {
		t.Fatal(err)
	}

	info := detectGitWorktree(dir)
	if info.IsWorktree {
		t.Error("expected bad .git file to not be detected as worktree")
	}
}
