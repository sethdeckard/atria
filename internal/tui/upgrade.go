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

		hint := "go install github.com/sethdeckard/atria@latest"
		if exe, err := os.Executable(); err == nil {
			if strings.HasPrefix(exe, "/opt/homebrew/") || strings.Contains(exe, "/Cellar/") {
				hint = "brew upgrade atria"
			}
		}

		return UpgradeAvailableMsg{
			LatestVersion: release.TagName,
			InstallHint:   hint,
		}
	}
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
