package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type gamesView struct {
	games     []Game
	filtered  []Game
	cursor    int
	offset    int
	filter    string
	height    int
	width     int
	tagFilter string // when drilling from tag browser
}

func newGamesView() gamesView {
	return gamesView{}
}

func (v *gamesView) loadGames(filter string) {
	v.filter = filter
	games, err := queryGames(filter)
	if err != nil {
		v.filtered = nil
		return
	}
	v.games = games
	v.filtered = games
	v.cursor = 0
	v.offset = 0
}

func (v *gamesView) loadGamesByTag(tag string) {
	v.tagFilter = tag
	v.filter = ""
	games, err := queryGamesByTag(tag)
	if err != nil {
		v.filtered = nil
		return
	}
	v.games = games
	v.filtered = games
	v.cursor = 0
	v.offset = 0
}

func (v *gamesView) applyFilter(filter string) {
	v.filter = filter
	if filter == "" && v.tagFilter == "" {
		v.loadGames("")
		return
	}
	if v.tagFilter != "" && filter == "" {
		v.loadGamesByTag(v.tagFilter)
		return
	}
	// Filter within current set
	lower := strings.ToLower(filter)
	var out []Game
	for _, g := range v.games {
		if matchesFilter(g, lower) {
			out = append(out, g)
		}
	}
	v.filtered = out
	v.cursor = 0
	v.offset = 0
}

func (v *gamesView) clearTagFilter() {
	v.tagFilter = ""
	v.loadGames(v.filter)
}

func (v gamesView) selectedGame() *Game {
	if len(v.filtered) == 0 {
		return nil
	}
	return &v.filtered[v.cursor]
}

func (v gamesView) visibleRows() int {
	h := v.height - 5 // header + status + padding
	if v.tagFilter != "" {
		h -= 1
	}
	if h < 1 {
		h = 1
	}
	return h
}

func (v *gamesView) update(msg tea.KeyMsg) (needDetail bool) {
	visible := v.visibleRows()
	switch msg.String() {
	case "up", "k":
		if v.cursor > 0 {
			v.cursor--
			if v.cursor < v.offset {
				v.offset = v.cursor
			}
		}
	case "down", "j":
		if v.cursor < len(v.filtered)-1 {
			v.cursor++
			if v.cursor >= v.offset+visible {
				v.offset = v.cursor - visible + 1
			}
		}
	case "pgup":
		v.cursor -= visible
		if v.cursor < 0 {
			v.cursor = 0
		}
		v.offset = v.cursor
	case "pgdown":
		v.cursor += visible
		if v.cursor >= len(v.filtered) {
			v.cursor = len(v.filtered) - 1
		}
		if v.cursor < 0 {
			v.cursor = 0
		}
		v.offset = v.cursor - visible + 1
		if v.offset < 0 {
			v.offset = 0
		}
	case "home", "g":
		v.cursor = 0
		v.offset = 0
	case "end", "G":
		v.cursor = len(v.filtered) - 1
		if v.cursor < 0 {
			v.cursor = 0
		}
		v.offset = v.cursor - visible + 1
		if v.offset < 0 {
			v.offset = 0
		}
	case "enter":
		return true
	}
	return false
}

func (v gamesView) render() string {
	var b strings.Builder

	// Column widths
	nameW := v.width - 20 - 30 - 6 // remaining for name
	if nameW < 20 {
		nameW = 20
	}
	platW := 12
	tagW := v.width - nameW - platW - 6
	if tagW < 10 {
		tagW = 10
	}

	title := "Games"
	if v.tagFilter != "" {
		title = fmt.Sprintf("Games tagged %q", v.tagFilter)
	}
	count := fmt.Sprintf(" (%d)", len(v.filtered))
	b.WriteString(renderTitle(v.width, title+count) + "\n")

	if v.tagFilter != "" {
		b.WriteString(helpDescStyle.Render("  Esc to clear tag filter") + "\n")
	}

	// Header
	header := fmt.Sprintf("  %-*s %-*s %s",
		nameW, "Name",
		platW, "Platform",
		"Tags",
	)
	b.WriteString(headerStyle.Render(header) + "\n")

	if len(v.filtered) == 0 {
		b.WriteString(helpDescStyle.Render("  No games found.") + "\n")
		return b.String()
	}

	visible := v.visibleRows()
	end := v.offset + visible
	if end > len(v.filtered) {
		end = len(v.filtered)
	}

	for i := v.offset; i < end; i++ {
		g := v.filtered[i]
		name := truncate(g.Name, nameW)
		plat := truncate(g.Platform, platW)
		tags := truncate(strings.Join(g.Tags, ", "), tagW)

		line := fmt.Sprintf("  %-*s %-*s %s", nameW, name, platW, plat, tags)

		if i == v.cursor {
			b.WriteString(selectedRowStyle.Render(line))
		} else {
			platRendered := platformStyle.Render(plat)
			tagRendered := tagStyle.Render(tags)
			line = fmt.Sprintf("  %-*s %s%s %s",
				nameW,
				lipgloss.NewStyle().Foreground(colorBright).Render(name),
				platRendered,
				strings.Repeat(" ", max(0, platW-lipgloss.Width(plat))),
				tagRendered,
			)
			b.WriteString(line)
		}
		b.WriteString("\n")
	}

	// Scroll indicator
	if len(v.filtered) > visible {
		pct := 0
		if len(v.filtered)-visible > 0 {
			pct = v.offset * 100 / (len(v.filtered) - visible)
		}
		b.WriteString(helpDescStyle.Render(fmt.Sprintf("  -- %d%% --", pct)) + "\n")
	}

	return b.String()
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
