package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type tagsView struct {
	tags     []Tag
	filtered []Tag
	cursor   int
	offset   int
	filter   string
	height   int
	width    int
}

func newTagsView() tagsView {
	return tagsView{}
}

func (v *tagsView) loadTags() {
	tags, err := queryTags()
	if err != nil {
		v.filtered = nil
		return
	}
	v.tags = tags
	v.applyFilter(v.filter)
}

func (v *tagsView) applyFilter(filter string) {
	v.filter = filter
	if filter == "" {
		v.filtered = v.tags
		v.cursor = 0
		v.offset = 0
		return
	}
	lower := strings.ToLower(filter)
	var out []Tag
	for _, t := range v.tags {
		if strings.Contains(strings.ToLower(t.Name), lower) {
			out = append(out, t)
		}
	}
	v.filtered = out
	v.cursor = 0
	v.offset = 0
}

func (v tagsView) selectedTag() *Tag {
	if len(v.filtered) == 0 {
		return nil
	}
	return &v.filtered[v.cursor]
}

func (v tagsView) visibleRows() int {
	h := v.height - 4
	if h < 1 {
		h = 1
	}
	return h
}

func (v *tagsView) update(msg tea.KeyMsg) (drillToGames bool) {
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

func (v tagsView) render() string {
	var b strings.Builder

	count := fmt.Sprintf(" (%d)", len(v.filtered))
	b.WriteString(renderTitle(v.width, "Tags"+count) + "\n")

	nameW := v.width - 16
	if nameW < 10 {
		nameW = 10
	}
	header := fmt.Sprintf("  %-*s %s", nameW, "Tag", "Games")
	b.WriteString(headerStyle.Render(header) + "\n")

	if len(v.filtered) == 0 {
		b.WriteString(helpDescStyle.Render("  No tags found.") + "\n")
		return b.String()
	}

	visible := v.visibleRows()
	end := v.offset + visible
	if end > len(v.filtered) {
		end = len(v.filtered)
	}

	for i := v.offset; i < end; i++ {
		t := v.filtered[i]
		name := truncate(t.Name, nameW)
		countStr := fmt.Sprintf("%d", t.Count)

		line := fmt.Sprintf("  %-*s %s", nameW, name, countStr)

		if i == v.cursor {
			b.WriteString(selectedRowStyle.Render(line))
		} else {
			b.WriteString(fmt.Sprintf("  %s %s",
				tagStyle.Render(fmt.Sprintf("%-*s", nameW, name)),
				helpDescStyle.Render(countStr),
			))
		}
		b.WriteString("\n")
	}

	if len(v.filtered) > visible {
		pct := 0
		if len(v.filtered)-visible > 0 {
			pct = v.offset * 100 / (len(v.filtered) - visible)
		}
		b.WriteString(helpDescStyle.Render(fmt.Sprintf("  -- %d%% --", pct)) + "\n")
	}

	return b.String()
}
