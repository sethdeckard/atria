package tui

import "testing"

func TestIsNewer(t *testing.T) {
	tests := []struct {
		current, latest string
		want            bool
	}{
		{"0.4.0", "0.5.0", true},
		{"0.5.0", "0.5.0", false},
		{"0.5.1", "0.5.0", false},
		{"0.5.0", "1.0.0", true},
		{"1.0.0", "0.9.0", false},
		{"0.5.0", "0.5.1", true},
		{"1.2.3", "1.2.4", true},
		{"1.2.3", "1.3.0", true},
		{"1.2.3", "2.0.0", true},
		{"invalid", "0.5.0", false},
		{"0.5.0", "invalid", false},
		{"0.5", "0.5.0", false},
	}
	for _, tt := range tests {
		got := isNewer(tt.current, tt.latest)
		if got != tt.want {
			t.Errorf("isNewer(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
		}
	}
}

func TestInstallHint(t *testing.T) {
	tests := []struct {
		name string
		exe  string
		want string
	}{
		{"macos arm symlink", "/opt/homebrew/bin/atria", brewUpgradeHint},
		{"macos cask", "/opt/homebrew/Caskroom/atria/0.6.0/atria", brewUpgradeHint},
		{"macos intel formula", "/usr/local/Cellar/atria/0.5.0/bin/atria", brewUpgradeHint},
		{"linux formula", "/home/linuxbrew/.linuxbrew/Cellar/atria/0.5.0/bin/atria", brewUpgradeHint},
		{"linux cask", "/home/linuxbrew/.linuxbrew/Caskroom/atria/0.6.0/atria", brewUpgradeHint},
		{"go install", "/Users/seth/go/bin/atria", goInstallHint},
		{"hand installed", "/usr/local/bin/atria", goInstallHint},
		{"empty", "", goInstallHint},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := installHint(tt.exe); got != tt.want {
				t.Errorf("installHint(%q) = %q, want %q", tt.exe, got, tt.want)
			}
		})
	}
}

func TestUpgradeNotice(t *testing.T) {
	got := upgradeNotice("v0.5.0", "brew upgrade atria")
	want := "Update available: v0.5.0 (brew upgrade atria)"
	if got != want {
		t.Errorf("upgradeNotice() = %q, want %q", got, want)
	}
}
