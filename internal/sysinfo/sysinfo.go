package sysinfo

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// automationTools are the external binaries our tools (or the model via
// shell_exec) rely on for desktop automation. Detected once at startup so
// the model knows what's actually available instead of guessing.
var automationTools = []string{"xdotool", "wmctrl", "ydotool", "xclip", "xsel", "wl-copy", "wl-paste", "gnome-screenshot", "grim", "maim", "import"}

// DetectEnvironment inspects the host once at startup and returns a plain
// text description for the model: OS, desktop/display server, and which
// automation tools are installed vs missing. This lets the model tell the
// user "I need xdotool for that, want me to install it?" instead of
// guessing at an unrelated action when a capability is missing.
func DetectEnvironment() string {
	var b strings.Builder

	b.WriteString("SYSTEM ENVIRONMENT (detected at startup — trust this over guessing):\n")
	fmt.Fprintf(&b, "- OS: %s/%s (%s)\n", runtime.GOOS, runtime.GOARCH, osRelease())
	fmt.Fprintf(&b, "- Display server: %s\n", envOr("XDG_SESSION_TYPE", "unknown"))
	fmt.Fprintf(&b, "- Desktop environment: %s\n", envOr("XDG_CURRENT_DESKTOP", envOr("DESKTOP_SESSION", "unknown")))

	var have, missing []string
	for _, t := range automationTools {
		if _, err := exec.LookPath(t); err == nil {
			have = append(have, t)
		} else {
			missing = append(missing, t)
		}
	}
	fmt.Fprintf(&b, "- Installed automation tools: %s\n", joinOrNone(have))
	fmt.Fprintf(&b, "- Missing automation tools: %s\n", joinOrNone(missing))

	if _, err := exec.LookPath("uvx"); err != nil {
		b.WriteString("- uvx is NOT installed, so the desktop-control MCP server (take_screenshot, click_screen, move_mouse, " +
			"press_key/press_hotkey) cannot be fetched, meaning no key-press simulation and no visual verification of GUI " +
			"actions right now — say so plainly instead of guessing at outcomes, and propose installing uv " +
			"(`curl -LsSf https://astral.sh/uv/install.sh | sh`) if the user wants it.\n")
	} else {
		b.WriteString("- Key-press simulation and visual verification are available via the press_key/press_hotkey and take_screenshot tools — reach for take_screenshot only when you genuinely can't tell what happened otherwise, not as a routine step.\n")
	}

	return b.String()
}

func osRelease() string {
	data, err := os.ReadFile("/etc/os-release")

	if err != nil {
		return "unknown"
	}

	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
		}
	}
	return "unknown"
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func joinOrNone(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, ", ")
}
