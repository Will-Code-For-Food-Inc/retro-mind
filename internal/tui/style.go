package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Colors
	colorPrimary   = lipgloss.Color("#7aafff")
	colorSecondary = lipgloss.Color("#aaddff")
	colorMuted     = lipgloss.Color("#666666")
	colorBright    = lipgloss.Color("#ffffff")
	colorDim       = lipgloss.Color("#999999")
	colorTag       = lipgloss.Color("#b8a0d8")
	colorPlatform  = lipgloss.Color("#7ae0a0")
	colorHighlight = lipgloss.Color("#3a3a5c")
	colorError     = lipgloss.Color("#ff5555")

	// Status bar
	statusBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#1a1a2e")).
			Foreground(colorDim).
			Padding(0, 1)

	statusActiveTab = lipgloss.NewStyle().
			Background(lipgloss.Color("#2a2a4e")).
			Foreground(colorPrimary).
			Bold(true).
			Padding(0, 1)

	statusTab = lipgloss.NewStyle().
			Background(lipgloss.Color("#1a1a2e")).
			Foreground(colorMuted).
			Padding(0, 1)

	// Dim text (tool call traces in agent view)
	dimStyle = lipgloss.NewStyle().Foreground(colorDim)

	// Title
	titleStyle = lipgloss.NewStyle().
			Foreground(colorBright).
			Bold(true).
			MarginBottom(1)

	// Table
	headerStyle = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true).
			BorderBottom(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("#444444"))

	selectedRowStyle = lipgloss.NewStyle().
				Background(colorHighlight).
				Foreground(colorBright)

	normalRowStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	// Tags
	tagStyle = lipgloss.NewStyle().
			Foreground(colorTag)

	platformStyle = lipgloss.NewStyle().
			Foreground(colorPlatform)

	// Search
	searchPromptStyle = lipgloss.NewStyle().
				Foreground(colorPrimary).
				Bold(true)

	commandPromptStyle = lipgloss.NewStyle().
				Foreground(colorPlatform).
				Bold(true)

	// Detail view
	detailLabelStyle = lipgloss.NewStyle().
				Foreground(colorPrimary).
				Bold(true).
				Width(12)

	detailValueStyle = lipgloss.NewStyle().
				Foreground(colorBright)

	detailBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#444444")).
				Padding(1, 2)

	// Help
	helpKeyStyle = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	helpDescStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	// Error
	errorStyle = lipgloss.NewStyle().
			Foreground(colorError).
			Bold(true)
)

func renderTitle(width int, text string) string {
	maxWidth := width / 3
	if maxWidth < 12 {
		maxWidth = 12
	}
	return titleStyle.Render(truncate(text, maxWidth))
}
