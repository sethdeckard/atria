package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscover(t *testing.T) {
	tmp := t.TempDir()

	// Create some project directories.
	os.Mkdir(filepath.Join(tmp, "alpha"), 0o755)
	os.Mkdir(filepath.Join(tmp, "beta"), 0o755)
	os.Mkdir(filepath.Join(tmp, "gamma"), 0o755)

	// Create a file (should be ignored).
	os.WriteFile(filepath.Join(tmp, "README.md"), []byte("hello"), 0o644)

	projects := Discover([]string{tmp})

	expected := []string{
		filepath.Join(tmp, "alpha"),
		filepath.Join(tmp, "beta"),
		filepath.Join(tmp, "gamma"),
	}

	if len(projects) != len(expected) {
		t.Fatalf("expected %d projects, got %d: %v", len(expected), len(projects), projects)
	}
	for i, p := range projects {
		if p != expected[i] {
			t.Errorf("projects[%d] = %q, want %q", i, p, expected[i])
		}
	}
}

func TestDiscoverHiddenDirectoriesSkipped(t *testing.T) {
	tmp := t.TempDir()

	os.Mkdir(filepath.Join(tmp, ".hidden"), 0o755)
	os.Mkdir(filepath.Join(tmp, "visible"), 0o755)

	projects := Discover([]string{tmp})

	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d: %v", len(projects), projects)
	}
	if projects[0] != filepath.Join(tmp, "visible") {
		t.Errorf("expected %q, got %q", filepath.Join(tmp, "visible"), projects[0])
	}
}

func TestDiscoverNonExistentWatchDir(t *testing.T) {
	projects := Discover([]string{"/nonexistent/path/that/does/not/exist"})

	if len(projects) != 0 {
		t.Errorf("expected 0 projects for non-existent dir, got %d: %v", len(projects), projects)
	}
}

func TestDiscoverMultipleWatchDirs(t *testing.T) {
	tmp1 := t.TempDir()
	tmp2 := t.TempDir()

	os.Mkdir(filepath.Join(tmp1, "proj-a"), 0o755)
	os.Mkdir(filepath.Join(tmp2, "proj-b"), 0o755)

	projects := Discover([]string{tmp1, tmp2})

	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d: %v", len(projects), projects)
	}
}

func TestDiscoverEmptyWatchDirs(t *testing.T) {
	projects := Discover(nil)
	if len(projects) != 0 {
		t.Errorf("expected 0 projects for nil watchDirs, got %d", len(projects))
	}
}

func TestDiscoverSorted(t *testing.T) {
	tmp := t.TempDir()

	os.Mkdir(filepath.Join(tmp, "zebra"), 0o755)
	os.Mkdir(filepath.Join(tmp, "apple"), 0o755)
	os.Mkdir(filepath.Join(tmp, "mango"), 0o755)

	projects := Discover([]string{tmp})

	for i := 1; i < len(projects); i++ {
		if projects[i] < projects[i-1] {
			t.Errorf("projects not sorted: %v", projects)
			break
		}
	}
}
