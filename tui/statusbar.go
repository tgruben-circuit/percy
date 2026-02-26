package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	statusBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("236")).
			Foreground(lipgloss.Color("252")).
			Padding(0, 1)

	statusConnected    = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("●")
	statusDisconnected = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("●")
	statusWorking      = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render("◉")
)

// StatusBar renders the status bar at the bottom of the TUI.
type StatusBar struct {
	Connected         bool
	Working           bool
	Model             string
	ContextWindowSize uint64
	Width             int
	Cwd               string
}

// View renders the status bar.
func (s StatusBar) View() string {
	var indicator string
	switch {
	case s.Working:
		indicator = statusWorking + " working"
	case s.Connected:
		indicator = statusConnected + " connected"
	default:
		indicator = statusDisconnected + " disconnected"
	}

	model := s.Model
	if model == "" {
		model = "no model"
	}

	var ctx string
	if s.ContextWindowSize > 0 {
		ctx = fmt.Sprintf(" | ctx: %dk", s.ContextWindowSize/1000)
	}

	var cwd string
	if s.Cwd != "" {
		cwd = " | " + shortenPath(s.Cwd, 30)
	}

	content := fmt.Sprintf(" %s | %s%s%s", indicator, model, ctx, cwd)
	return statusBarStyle.Width(s.Width).Render(content)
}

// shortenPath truncates a long path with a "..." prefix, keeping the trailing segments.
func shortenPath(path string, maxLen int) string {
	if path == "" || len(path) <= maxLen {
		return path
	}
	parts := strings.Split(path, "/")
	for i := 1; i < len(parts); i++ {
		candidate := ".../" + strings.Join(parts[i:], "/")
		if len(candidate) <= maxLen {
			return candidate
		}
	}
	// Last resort: just the last segment with prefix
	return ".../" + parts[len(parts)-1]
}
