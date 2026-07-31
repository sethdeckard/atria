package tui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// UpgradeAvailableMsg is sent when a newer version is available on GitHub.
type UpgradeAvailableMsg struct {
	LatestVersion string
	InstallHint   string
}

func checkUpgrade(currentVersion string) tea.Cmd {
	return func() tea.Msg {
		if currentVersion == "dev" {
			return nil
		}

		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get("https://api.github.com/repos/sethdeckard/atria/releases/latest")
		if err != nil {
			return nil
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil
		}

		var release struct {
			TagName string `json:"tag_name"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
			return nil
		}

		latest := strings.TrimPrefix(release.TagName, "v")
		current := strings.TrimPrefix(currentVersion, "v")

		if !isNewer(current, latest) {
			return nil
		}

		hint := goInstallHint
		if exe, err := os.Executable(); err == nil {
			hint = installHint(exe)
		}

		return UpgradeAvailableMsg{
			LatestVersion: release.TagName,
			InstallHint:   hint,
		}
	}
}

const (
	brewUpgradeHint = "brew upgrade atria"
	goInstallHint   = "go install github.com/sethdeckard/atria@latest"
)

// isHomebrewPath reports whether exe lives inside a Homebrew installation,
// covering both the formula (Cellar) and cask (Caskroom) layouts on macOS and
// Linux. Casks matter because atria is distributed as one: on Linux,
// os.Executable reads the fully resolved /proc/self/exe, which lands in
// Caskroom rather than Cellar.
func isHomebrewPath(exe string) bool {
	if exe == "" {
		return false
	}
	if strings.Contains(exe, "/Cellar/") || strings.Contains(exe, "/Caskroom/") {
		return true
	}
	return strings.HasPrefix(exe, "/opt/homebrew/") ||
		strings.HasPrefix(exe, "/home/linuxbrew/.linuxbrew/")
}

// installHint returns the upgrade command appropriate to how atria was installed.
func installHint(exe string) string {
	if isHomebrewPath(exe) {
		return brewUpgradeHint
	}
	return goInstallHint
}

// isNewer returns true if latest is a higher semver than current.
func isNewer(current, latest string) bool {
	cp := parseSemver(current)
	lp := parseSemver(latest)
	if cp == nil || lp == nil {
		return false
	}
	for i := 0; i < 3; i++ {
		if lp[i] > cp[i] {
			return true
		}
		if lp[i] < cp[i] {
			return false
		}
	}
	return false
}

func parseSemver(v string) []int {
	parts := strings.SplitN(v, ".", 3)
	if len(parts) != 3 {
		return nil
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		nums[i] = n
	}
	return nums
}

func upgradeNotice(version, hint string) string {
	return fmt.Sprintf("Update available: %s (%s)", version, hint)
}
