package iterm

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	headerStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffaa33"))
	cmdStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#6699ff"))
	successStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#4ec969"))
	errorStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ff4444"))
	istyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	promptStyle  = lipgloss.NewStyle().Bold(true)

	stdinReader = bufio.NewReader(os.Stdin)
)

// VenvDir returns the absolute path to atria's recommended venv directory.
func VenvDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "atria", "venv")
}

// VenvIT2Path returns the path to it2 inside atria's venv.
func VenvIT2Path() string {
	return filepath.Join(VenvDir(), "bin", "it2")
}

// NeedsInstall returns true if it2 can't be found anywhere.
func NeedsInstall(configPath string) bool {
	if configPath != "" {
		if _, err := os.Stat(configPath); err == nil {
			return false
		}
	}
	return findIT2() == ""
}

// Preflight checks if the iTerm2 backend is ready. If it2 is installed but
// can't connect, prints a helpful message and returns false. If it2 is missing,
// delegates to PromptInstall. Returns the resolved it2 path and whether to proceed.
// This must be called before the TUI starts (it uses stdin/stdout directly).
func Preflight(it2Path string) (resolvedPath string, ok bool) {
	// If user configured an explicit path, trust it
	if it2Path != "" {
		if _, err := os.Stat(it2Path); err != nil {
			fmt.Println()
			fmt.Println(errorStyle.Render("  it2 not found at configured path: " + it2Path))
			fmt.Println()
			fmt.Println(istyle.Render("  Check it2_path in ~/.config/atria/config.toml"))
			fmt.Println()
			return "", false
		}
		if !checkIT2Connection(it2Path) {
			return it2Path, false
		}
		return it2Path, true
	}

	// Try to find it2
	found := findIT2()
	if found == "" {
		installed := PromptInstall()
		if installed == "" {
			return "", false
		}
		found = installed
	}

	if !checkIT2Connection(found) {
		return found, false
	}

	return found, true
}

func checkIT2Connection(it2Path string) bool {
	for {
		c := &Client{it2Path: it2Path}
		_, err := c.run("session", "list", "--json")
		if err == nil {
			return true
		}

		fmt.Println()
		fmt.Println(headerStyle.Render("  Cannot connect to iTerm2"))
		fmt.Println()
		fmt.Println("  " + istyle.Render("it2 found at ") + cmdStyle.Render(it2Path))
		fmt.Println()
		fmt.Println("  Please check:")
		fmt.Println("    1. " + promptStyle.Render("Enable the Python API"))
		fmt.Println("       " + istyle.Render("iTerm2 > Settings > General > Magic > Enable Python API"))
		fmt.Println("    2. " + promptStyle.Render("Run atria inside iTerm2"))
		fmt.Println("       " + istyle.Render("The it2 CLI only works from within iTerm2"))
		fmt.Println()

		if !retryOrQuit() {
			return false
		}
	}
}

// PromptInstall asks the user if they want to auto-install it2 and does so.
// Returns the path to the installed it2, or empty string if declined/failed.
// This must be called before the TUI starts (it uses stdin/stdout directly).
func PromptInstall() string {
	venv := VenvDir()

	fmt.Println()
	fmt.Println(headerStyle.Render("  it2 not found") + istyle.Render(" — required for iTerm2 backend"))
	fmt.Println()
	fmt.Println(istyle.Render("  To install manually:"))
	fmt.Println("    " + cmdStyle.Render(fmt.Sprintf("python3 -m venv %s", venv)))
	fmt.Println("    " + cmdStyle.Render(fmt.Sprintf("%s/bin/pip install it2", venv)))
	fmt.Println()
	fmt.Print("  " + promptStyle.Render("Install automatically?") + " [Y/n] ")

	answer := readLine()
	if answer != "" && answer != "y" && answer != "yes" {
		fmt.Println()
		fmt.Println(istyle.Render("  Skipped. Run the commands above, then restart atria."))
		fmt.Println()
		return ""
	}

	fmt.Println()
	if err := installIT2(venv); err != nil {
		fmt.Println("  " + errorStyle.Render("Installation failed: "+err.Error()))
		fmt.Println()
		fmt.Println(istyle.Render("  Run the commands above to install manually."))
		fmt.Println()
		return ""
	}

	it2 := filepath.Join(venv, "bin", "it2")
	fmt.Println("  " + successStyle.Render("Installed it2 at "+it2))
	fmt.Println()
	waitForEnter()
	return it2
}

// Uninstall removes the auto-installed it2 venv.
func Uninstall() {
	venv := VenvDir()
	if _, err := os.Stat(venv); os.IsNotExist(err) {
		fmt.Println(istyle.Render("  Nothing to remove — no venv at " + venv))
		return
	}
	fmt.Printf("  Remove %s? [y/N] ", cmdStyle.Render(venv))
	answer := readLine()
	if answer != "y" && answer != "yes" {
		fmt.Println(istyle.Render("  Cancelled."))
		return
	}
	if err := os.RemoveAll(venv); err != nil {
		fmt.Println(errorStyle.Render("  Failed: " + err.Error()))
		return
	}
	fmt.Println(successStyle.Render("  Removed " + venv))
}

func readLine() string {
	line, _ := stdinReader.ReadString('\n')
	return strings.TrimSpace(strings.ToLower(line))
}

func waitForEnter() {
	fmt.Print(istyle.Render("  Press Enter to continue..."))
	stdinReader.ReadString('\n')
	fmt.Println()
}

func retryOrQuit() bool {
	fmt.Print("  " + promptStyle.Render("Retry?") + " [Y/n] ")
	answer := readLine()
	fmt.Println()
	return answer == "" || answer == "y" || answer == "yes"
}

func installIT2(venvDir string) error {
	if err := os.MkdirAll(filepath.Dir(venvDir), 0o755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	fmt.Println("  " + istyle.Render("Creating Python venv..."))
	venvCmd := exec.Command("python3", "-m", "venv", venvDir)
	venvCmd.Stderr = os.Stderr
	if err := venvCmd.Run(); err != nil {
		return fmt.Errorf("python3 -m venv failed: %w", err)
	}

	pip := filepath.Join(venvDir, "bin", "pip")
	fmt.Println("  " + istyle.Render("Installing it2..."))
	pipCmd := exec.Command(pip, "install", "--quiet", "--disable-pip-version-check", "it2")
	pipCmd.Stderr = os.Stderr
	if err := pipCmd.Run(); err != nil {
		return fmt.Errorf("pip install it2 failed: %w", err)
	}

	return nil
}
