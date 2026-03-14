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

func TestUpgradeNotice(t *testing.T) {
	got := upgradeNotice("v0.5.0", "brew upgrade atria")
	want := "Update available: v0.5.0 (brew upgrade atria)"
	if got != want {
		t.Errorf("upgradeNotice() = %q, want %q", got, want)
	}
}
