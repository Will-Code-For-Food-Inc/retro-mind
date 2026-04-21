package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type playlistsView struct {
	playlists []Playlist
	cursor    int
	offset    int
	height    int
	width     int

	// Sub-view: viewing games in a playlist
	viewing      bool
	viewPlaylist *Playlist
	viewGames    []Game
	viewCursor   int
	viewOffset   int

	// Export prompt
	exporting    bool
	exportFormat int
	exportResult string
}

var exportFormats = []string{"pegasus", "m3u", "retroarch", "emulationstation"}

func newPlaylistsView() playlistsView {
	return playlistsView{}
}

func (v *playlistsView) loadPlaylists() {
	pls, err := queryPlaylists()
	if err != nil {
		v.playlists = nil
		return
	}
	v.playlists = pls
	v.cursor = 0
	v.offset = 0
}

func (v playlistsView) selectedPlaylist() *Playlist {
	if len(v.playlists) == 0 {
		return nil
	}
	return &v.playlists[v.cursor]
}

func (v *playlistsView) openPlaylist(p *Playlist) {
	games, _ := queryPlaylistGames(p.ID)
	v.viewing = true
	v.viewPlaylist = p
	v.viewGames = games
	v.viewCursor = 0
	v.viewOffset = 0
	v.exporting = false
	v.exportResult = ""
}

func (v playlistsView) visibleRows() int {
	h := v.height - 4
	if h < 1 {
		h = 1
	}
	return h
}

func (v *playlistsView) update(msg tea.KeyMsg) (needGameDetail bool, selectedGame *Game) {
	if v.exporting {
		return v.updateExport(msg)
	}
	if v.viewing {
		return v.updateViewing(msg)
	}
	return v.updateList(msg)
}

func (v *playlistsView) updateList(msg tea.KeyMsg) (bool, *Game) {
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
		if v.cursor < len(v.playlists)-1 {
			v.cursor++
			if v.cursor >= v.offset+visible {
				v.offset = v.cursor - visible + 1
			}
		}
	case "enter":
		if p := v.selectedPlaylist(); p != nil {
			v.openPlaylist(p)
		}
	}
	return false, nil
}

func (v *playlistsView) updateViewing(msg tea.KeyMsg) (bool, *Game) {
	visible := v.visibleRows() - 3 // room for playlist info
	if visible < 1 {
		visible = 1
	}
	switch msg.String() {
	case "up", "k":
		if v.viewCursor > 0 {
			v.viewCursor--
			if v.viewCursor < v.viewOffset {
				v.viewOffset = v.viewCursor
			}
		}
	case "down", "j":
		if v.viewCursor < len(v.viewGames)-1 {
			v.viewCursor++
			if v.viewCursor >= v.viewOffset+visible {
				v.viewOffset = v.viewCursor - visible + 1
			}
		}
	case "enter":
		if len(v.viewGames) > 0 {
			g := v.viewGames[v.viewCursor]
			return true, &g
		}
	case "e":
		v.exporting = true
		v.exportFormat = 0
		v.exportResult = ""
	case "esc":
		v.viewing = false
		v.viewPlaylist = nil
		v.viewGames = nil
	}
	return false, nil
}

func (v *playlistsView) updateExport(msg tea.KeyMsg) (bool, *Game) {
	switch msg.String() {
	case "up", "k":
		if v.exportFormat > 0 {
			v.exportFormat--
		}
	case "down", "j":
		if v.exportFormat < len(exportFormats)-1 {
			v.exportFormat++
		}
	case "enter":
		v.doExport()
	case "esc":
		v.exporting = false
		v.exportResult = ""
	}
	return false, nil
}

func (v *playlistsView) doExport() {
	if v.viewPlaylist == nil {
		return
	}
	format := exportFormats[v.exportFormat]

	games, err := queryPlaylistGames(v.viewPlaylist.ID)
	if err != nil {
		v.exportResult = fmt.Sprintf("Error: %v", err)
		return
	}

	var content string
	switch format {
	case "pegasus":
		content = emitPegasus(v.viewPlaylist.Name, v.viewPlaylist.Description, games)
	case "m3u":
		content = emitM3U(v.viewPlaylist.Name, v.viewPlaylist.Description, games)
	case "retroarch":
		content = emitRetroArch(v.viewPlaylist.Name, games)
	case "emulationstation":
		content = emitES(v.viewPlaylist.Name, games)
	}

	home, _ := os.UserHomeDir()
	outDir := filepath.Join(home, "Emulation", "playlists")
	slug := strings.ReplaceAll(strings.ToLower(v.viewPlaylist.Name), " ", "-")

	var outPath string
	switch format {
	case "pegasus":
		dir := filepath.Join(outDir, slug)
		os.MkdirAll(dir, 0755)
		outPath = filepath.Join(dir, "metadata.pegasus.txt")
	case "emulationstation":
		dir := filepath.Join(outDir, slug)
		os.MkdirAll(dir, 0755)
		outPath = filepath.Join(dir, "gamelist.xml")
	case "retroarch":
		os.MkdirAll(outDir, 0755)
		outPath = filepath.Join(outDir, slug+".lpl")
	default:
		os.MkdirAll(outDir, 0755)
		outPath = filepath.Join(outDir, slug+".m3u")
	}

	if err := os.WriteFile(outPath, []byte(content), 0644); err != nil {
		v.exportResult = fmt.Sprintf("Error writing: %v", err)
		return
	}
	v.exportResult = fmt.Sprintf("Exported to %s", outPath)
}

func (v *playlistsView) handleEsc() bool {
	if v.exporting {
		v.exporting = false
		v.exportResult = ""
		return true
	}
	if v.viewing {
		v.viewing = false
		v.viewPlaylist = nil
		v.viewGames = nil
		return true
	}
	return false
}

func (v playlistsView) render() string {
	if v.exporting {
		return v.renderExport()
	}
	if v.viewing {
		return v.renderViewing()
	}
	return v.renderList()
}

func (v playlistsView) renderList() string {
	var b strings.Builder

	count := fmt.Sprintf(" (%d)", len(v.playlists))
	b.WriteString(renderTitle(v.width, "Playlists"+count) + "\n")

	nameW := v.width / 3
	if nameW < 15 {
		nameW = 15
	}
	descW := v.width - nameW - 20
	if descW < 10 {
		descW = 10
	}

	header := fmt.Sprintf("  %-*s %-*s %s", nameW, "Name", descW, "Description", "Games")
	b.WriteString(headerStyle.Render(header) + "\n")

	if len(v.playlists) == 0 {
		b.WriteString(helpDescStyle.Render("  No playlists found.") + "\n")
		return b.String()
	}

	visible := v.visibleRows()
	end := v.offset + visible
	if end > len(v.playlists) {
		end = len(v.playlists)
	}

	for i := v.offset; i < end; i++ {
		p := v.playlists[i]
		name := truncate(p.Name, nameW)
		desc := truncate(p.Description, descW)
		countStr := fmt.Sprintf("%d", p.GameCount)

		line := fmt.Sprintf("  %-*s %-*s %s", nameW, name, descW, desc, countStr)

		if i == v.cursor {
			b.WriteString(selectedRowStyle.Render(line))
		} else {
			b.WriteString(fmt.Sprintf("  %s %s %s",
				detailValueStyle.Render(fmt.Sprintf("%-*s", nameW, name)),
				helpDescStyle.Render(fmt.Sprintf("%-*s", descW, desc)),
				helpDescStyle.Render(countStr),
			))
		}
		b.WriteString("\n")
	}

	return b.String()
}

func (v playlistsView) renderViewing() string {
	var b strings.Builder

	p := v.viewPlaylist
	b.WriteString(renderTitle(v.width, fmt.Sprintf("Playlist: %s", p.Name)) + "\n")
	if p.Description != "" {
		b.WriteString(helpDescStyle.Render("  "+p.Description) + "\n")
	}
	b.WriteString(helpDescStyle.Render(fmt.Sprintf("  %d games | Created %s | e=export", p.GameCount, p.CreatedAt)) + "\n")
	b.WriteString("\n")

	nameW := v.width - 20 - 30 - 6
	if nameW < 20 {
		nameW = 20
	}
	platW := 12
	header := fmt.Sprintf("  # %-*s %-*s %s", nameW, "Name", platW, "Platform", "Tags")
	b.WriteString(headerStyle.Render(header) + "\n")

	if len(v.viewGames) == 0 {
		b.WriteString(helpDescStyle.Render("  No games in playlist.") + "\n")
		return b.String()
	}

	visible := v.visibleRows() - 4
	if visible < 1 {
		visible = 1
	}
	end := v.viewOffset + visible
	if end > len(v.viewGames) {
		end = len(v.viewGames)
	}

	for i := v.viewOffset; i < end; i++ {
		g := v.viewGames[i]
		name := truncate(g.Name, nameW)
		plat := truncate(g.Platform, platW)
		tags := truncate(strings.Join(g.Tags, ", "), 30)
		pos := fmt.Sprintf("%2d", i+1)

		line := fmt.Sprintf("  %s %-*s %-*s %s", pos, nameW, name, platW, plat, tags)

		if i == v.viewCursor {
			b.WriteString(selectedRowStyle.Render(line))
		} else {
			b.WriteString(fmt.Sprintf("  %s %s %s %s",
				helpDescStyle.Render(pos),
				detailValueStyle.Render(fmt.Sprintf("%-*s", nameW, name)),
				platformStyle.Render(fmt.Sprintf("%-*s", platW, plat)),
				tagStyle.Render(tags),
			))
		}
		b.WriteString("\n")
	}

	if v.exportResult != "" {
		b.WriteString("\n")
		b.WriteString(helpKeyStyle.Render("  "+v.exportResult) + "\n")
	}

	return b.String()
}

func (v playlistsView) renderExport() string {
	var b strings.Builder

	b.WriteString(renderTitle(v.width, fmt.Sprintf("Export: %s", v.viewPlaylist.Name)) + "\n")
	b.WriteString(helpDescStyle.Render("  Select format and press Enter") + "\n\n")

	for i, f := range exportFormats {
		line := fmt.Sprintf("  %s", f)
		if i == v.exportFormat {
			b.WriteString(selectedRowStyle.Render(line))
		} else {
			b.WriteString(helpDescStyle.Render(line))
		}
		b.WriteString("\n")
	}

	if v.exportResult != "" {
		b.WriteString("\n")
		b.WriteString(helpKeyStyle.Render("  "+v.exportResult) + "\n")
	}

	return b.String()
}

// ── Simple playlist emitters (self-contained, no romBase lookup) ──────────

func emitPegasus(name, description string, games []Game) string {
	var b strings.Builder
	fmt.Fprintf(&b, "collection: %s\n", name)
	if description != "" {
		fmt.Fprintf(&b, "summary: %s\n", description)
	}
	b.WriteString("\n")
	for _, g := range games {
		fmt.Fprintf(&b, "game: %s\n", g.Name)
		fmt.Fprintf(&b, "# platform: %s\n\n", g.Platform)
	}
	return b.String()
}

func emitM3U(name, description string, games []Game) string {
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	if description != "" {
		fmt.Fprintf(&b, "# %s\n", description)
	}
	for _, g := range games {
		fmt.Fprintf(&b, "#EXTINF:-1,%s\n# platform: %s\n", g.Name, g.Platform)
	}
	return b.String()
}

func emitRetroArch(_ string, games []Game) string {
	var b strings.Builder
	// Simplified JSON .lpl
	b.WriteString("{\n  \"version\": \"1.5\",\n  \"items\": [\n")
	for i, g := range games {
		crc := ""
		if len(g.CRCs) > 0 {
			crc = g.CRCs[0]
		}
		b.WriteString(fmt.Sprintf("    {\"label\": %q, \"crc32\": %q}", g.Name, crc))
		if i < len(games)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("  ]\n}\n")
	return b.String()
}

func emitES(_ string, games []Game) string {
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<gameList>\n")
	for _, g := range games {
		tags := strings.Join(g.Tags, ", ")
		b.WriteString(fmt.Sprintf("  <game>\n    <name>%s</name>\n    <desc>%s</desc>\n  </game>\n", g.Name, tags))
	}
	b.WriteString("</gameList>\n")
	return b.String()
}
