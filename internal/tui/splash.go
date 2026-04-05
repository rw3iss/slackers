package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const splashBanner = `
 ██████╗ ██╗      ██████╗  ██████╗██╗  ██╗███████╗██████╗  ██████╗
██╔════╝ ██║     ██╔═══██╗██╔════╝██║ ██╔╝██╔════╝██╔══██╗██╔════╝
╚█████╗  ██║     ████████║██║     █████╔╝ █████╗  ██████╔╝╚█████╗
 ╚═══██╗ ██║     ██╔═══██║██║     ██╔═██╗ ██╔══╝  ██╔══██╗ ╚═══██╗
██████╔╝ ███████╗██║   ██║╚██████╗██║  ██╗███████╗██║  ██║██████╔╝
╚═════╝  ╚══════╝╚═╝   ╚═╝ ╚═════╝╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝╚═════╝
`

const splashDuration = 618 * time.Millisecond

// SplashDoneMsg signals that the splash screen timer has elapsed.
type SplashDoneMsg struct{}

func splashTimerCmd() tea.Cmd {
	return tea.Tick(splashDuration, func(t time.Time) tea.Msg {
		return SplashDoneMsg{}
	})
}

func renderSplash(width, height int) string {
	bannerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("15")).
		Bold(true)

	taglineStyle := lipgloss.NewStyle().
		Foreground(ColorMuted).
		Italic(true)

	banner := bannerStyle.Render(splashBanner)
	tagline := taglineStyle.Render("terminal slack client")

	block := lipgloss.JoinVertical(lipgloss.Center, banner, "", tagline)

	return lipgloss.Place(width, height,
		lipgloss.Center, lipgloss.Center,
		block)
}

// BannerText returns the ASCII banner for use in CLI output.
func BannerText() string {
	return splashBanner
}
