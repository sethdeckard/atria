package terminal

import (
	"fmt"
	"testing"
)

// cwdMockBackend is a mock implementation of Backend for CWD discovery tests.
type cwdMockBackend struct {
	getVarResult string
	getVarErr    error
}

func (m *cwdMockBackend) Available() error                                          { return nil }
func (m *cwdMockBackend) ListSessions() ([]Session, error)                          { return nil, nil }
func (m *cwdMockBackend) NewSession() (string, error)                               { return "", nil }
func (m *cwdMockBackend) SendText(sessionID, text string) error                     { return nil }
func (m *cwdMockBackend) RunCommand(sessionID, cmd string) error                    { return nil }
func (m *cwdMockBackend) FocusSession(sessionID string) error                       { return nil }
func (m *cwdMockBackend) ReadScreen(sessionID string, lines int) (string, error)    { return "", nil }
func (m *cwdMockBackend) GetVar(sessionID, varName string) (string, error)          { return m.getVarResult, m.getVarErr }
func (m *cwdMockBackend) MonitorOutput(sessionID, logPath, patterns string) (int, error) {
	return 0, nil
}

func TestCwdFromNameMatch(t *testing.T) {
	projectDirs := []string{
		"/home/user/projects/myapp",
		"/home/user/projects/backend-service",
		"/home/user/projects/frontend",
	}

	tests := []struct {
		name     string
		sessName string
		want     string
	}{
		{
			name:     "exact basename match",
			sessName: "myapp",
			want:     "/home/user/projects/myapp",
		},
		{
			name:     "basename in longer name",
			sessName: "working on myapp stuff",
			want:     "/home/user/projects/myapp",
		},
		{
			name:     "case insensitive match",
			sessName: "MyApp Session",
			want:     "/home/user/projects/myapp",
		},
		{
			name:     "matches hyphenated project",
			sessName: "backend-service",
			want:     "/home/user/projects/backend-service",
		},
		{
			name:     "no match",
			sessName: "random session",
			want:     "",
		},
		{
			name:     "empty session name",
			sessName: "",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cwdFromNameMatch(tt.sessName, projectDirs)
			if got != tt.want {
				t.Errorf("cwdFromNameMatch(%q) = %q, want %q", tt.sessName, got, tt.want)
			}
		})
	}
}

func TestCwdFromNameMatchEmptyProjectDirs(t *testing.T) {
	got := cwdFromNameMatch("anything", nil)
	if got != "" {
		t.Errorf("expected empty string for nil projectDirs, got %q", got)
	}
}

func TestCwdFromGetVarSuccess(t *testing.T) {
	backend := &cwdMockBackend{
		getVarResult: "/home/user/projects/myapp\n",
	}
	session := Session{ID: "test-id", Name: "test", TTY: "/dev/ttys001"}
	watchDirs := []string{"/home/user/projects"}

	got := cwdFromGetVar(backend, session, watchDirs)
	if got != "/home/user/projects/myapp" {
		t.Errorf("expected /home/user/projects/myapp, got %q", got)
	}
}

func TestCwdFromGetVarNotUnderWatchDir(t *testing.T) {
	backend := &cwdMockBackend{
		getVarResult: "/home/user\n",
	}
	session := Session{ID: "test-id", Name: "test", TTY: "/dev/ttys001"}
	watchDirs := []string{"/home/user/projects"}

	got := cwdFromGetVar(backend, session, watchDirs)
	if got != "" {
		t.Errorf("expected empty string for path not under watch dir, got %q", got)
	}
}

func TestCwdFromGetVarError(t *testing.T) {
	backend := &cwdMockBackend{
		getVarErr: fmt.Errorf("not available"),
	}
	session := Session{ID: "test-id", Name: "test", TTY: "/dev/ttys001"}
	watchDirs := []string{"/home/user/projects"}

	got := cwdFromGetVar(backend, session, watchDirs)
	if got != "" {
		t.Errorf("expected empty string on error, got %q", got)
	}
}

func TestIsUnderWatchDir(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		watchDirs []string
		want      bool
	}{
		{
			name:      "direct child",
			path:      "/home/user/projects/myapp",
			watchDirs: []string{"/home/user/projects"},
			want:      true,
		},
		{
			name:      "nested child",
			path:      "/home/user/projects/myapp/src",
			watchDirs: []string{"/home/user/projects"},
			want:      true,
		},
		{
			name:      "not under watch dir",
			path:      "/tmp/something",
			watchDirs: []string{"/home/user/projects"},
			want:      false,
		},
		{
			name:      "exact match",
			path:      "/home/user/projects",
			watchDirs: []string{"/home/user/projects"},
			want:      true,
		},
		{
			name:      "partial name collision",
			path:      "/home/user/projects-other/myapp",
			watchDirs: []string{"/home/user/projects"},
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isUnderWatchDir(tt.path, tt.watchDirs)
			if got != tt.want {
				t.Errorf("isUnderWatchDir(%q, %v) = %v, want %v", tt.path, tt.watchDirs, got, tt.want)
			}
		})
	}
}
