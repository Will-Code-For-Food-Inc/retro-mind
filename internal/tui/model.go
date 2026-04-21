package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type viewID int

const (
	viewGames     viewID = 0
	viewTags      viewID = 1
	viewPlaylists viewID = 2
	viewTools     viewID = 3
	viewDetail    viewID = 4
)

type model struct {
	width  int
	height int

	activeView viewID
	prevView   viewID

	games     gamesView
	tags      tagsView
	playlists playlistsView
	tools     toolsView
	agent     agentView

	// Game detail
	detailGame *Game

	// Search
	searching   bool
	searchInput textinput.Model

	// Command prompt
	commanding   bool
	commandInput textinput.Model
}

func initialModel() model {
	ti := textinput.New()
	ti.Prompt = "/ "
	ti.PromptStyle = searchPromptStyle
	ti.CharLimit = 100

	cmd := textinput.New()
	cmd.Prompt = ": "
	cmd.PromptStyle = commandPromptStyle
	cmd.CharLimit = 512

	m := model{
		activeView:   viewGames,
		games:        newGamesView(),
		tags:         newTagsView(),
		playlists:    newPlaylistsView(),
		tools:        newToolsView(),
		agent:        newAgentView(),
		searchInput:  ti,
		commandInput: cmd,
	}
	return m
}

type dbLoadedMsg struct{}

func loadData() tea.Msg {
	return dbLoadedMsg{}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(loadData, loadToolCatalogCmd(m.tools.client))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.games.width = msg.Width
		m.games.height = msg.Height - 3 // status bar
		m.tags.width = msg.Width
		m.tags.height = msg.Height - 3
		m.playlists.width = msg.Width
		m.playlists.height = msg.Height - 3
		m.tools.width = msg.Width
		m.tools.height = msg.Height - 3
		m.agentWindowSize(msg.Width, msg.Height)
		return m, nil

	case agentResponseMsg:
		m.agentHandleResponse(msg)
		return m, nil

	case vibeCheckStartMsg:
		cmd := m.agent.handleVibeStart(msg)
		return m, cmd

	case vibeCheckProgressMsg:
		cmd := m.agent.handleVibeProgress(msg)
		return m, cmd

	case dbLoadedMsg:
		m.games.loadGames("")
		m.tags.loadTags()
		m.playlists.loadPlaylists()
		return m, nil

	case spinner.TickMsg:
		return m, m.agentHandleSpinnerTick(msg)

	case toolCatalogMsg:
		m.tools.loading = false
		if msg.err != nil {
			m.tools.err = msg.err.Error()
			return m, nil
		}
		m.tools.tools = msg.tools
		m.tools.err = ""
		m.agentHandleToolCatalog(msg)
		return m, nil

	case toolCallMsg:
		entry := toolRun{
			Command: msg.command,
			Output:  msg.output,
			IsError: msg.isError || msg.err != nil,
		}
		if msg.err != nil {
			entry.Output = msg.err.Error()
		}
		m.tools.history = append([]toolRun{entry}, m.tools.history...)
		if len(m.tools.history) > 5 {
			m.tools.history = m.tools.history[:5]
		}
		m.activeView = viewTools
		return m, nil

	case tea.KeyMsg:
		// Global quit
		if msg.String() == "ctrl+c" {
			m.tools.close()
			m.agentClose()
			return m, tea.Quit
		}

		// If searching, handle search input
		if m.searching {
			return m.updateSearch(msg)
		}
		if m.commanding {
			return m.updateCommand(msg)
		}

		// Agent tab owns all input when its text input has focus.
		// Only ctrl+c and esc escape to global handling.
		if m.activeView == viewAgent && m.agent.state == agentStateIdle {
			if msg.String() != "esc" {
				cmd := m.agentHandleKey(msg)
				return m, cmd
			}
		}

		// Global navigation
		switch msg.String() {
		case "q":
			if m.activeView == viewDetail {
				m.activeView = m.prevView
				m.detailGame = nil
				return m, nil
			}
			m.tools.close()
			m.agentClose()
			return m, tea.Quit
		case "1":
			m.activeView = viewGames
			m.games.loadGames("")
			return m, nil
		case "2":
			m.activeView = viewTags
			m.tags.loadTags()
			return m, nil
		case "3":
			m.activeView = viewPlaylists
			m.playlists.loadPlaylists()
			return m, nil
		case "4":
			m.activeView = viewTools
			if len(m.tools.tools) == 0 && !m.tools.loading {
				m.tools.loading = true
				return m, loadToolCatalogCmd(m.tools.client)
			}
			return m, nil
		case "5":
			m.activeView = viewAgent
			return m, m.agentFocus()
		case "tab":
			switch m.activeView {
			case viewGames:
				m.activeView = viewTags
				m.tags.loadTags()
			case viewTags:
				m.activeView = viewPlaylists
				m.playlists.loadPlaylists()
			case viewPlaylists:
				m.activeView = viewTools
			case viewTools:
				m.activeView = viewAgent
			case viewAgent:
				m.activeView = viewGames
				m.games.loadGames("")
			case viewDetail:
				m.activeView = m.prevView
				m.detailGame = nil
			}
			return m, nil
		case "/":
			if m.activeView != viewDetail {
				m.searching = true
				m.searchInput.SetValue("")
				m.searchInput.Focus()
				return m, textinput.Blink
			}
		case ":":
			m.commanding = true
			m.commandInput.Focus()
			if m.activeView == viewTools {
				if tool := m.tools.selectedTool(); tool != nil {
					m.commandInput.SetValue(tool.Name + " ")
					m.commandInput.SetCursor(len(m.commandInput.Value()))
				} else {
					m.commandInput.SetValue("")
				}
			} else {
				m.commandInput.SetValue("")
			}
			return m, textinput.Blink
		case "esc":
			return m.handleEsc()
		}

		// View-specific updates
		switch m.activeView {
		case viewGames:
			if needDetail := m.games.update(msg); needDetail {
				if g := m.games.selectedGame(); g != nil {
					full, err := queryGameByID(g.ID)
					if err == nil {
						m.detailGame = full
						m.prevView = viewGames
						m.activeView = viewDetail
					}
				}
			}
		case viewTags:
			if drillToGames := m.tags.update(msg); drillToGames {
				if t := m.tags.selectedTag(); t != nil {
					m.games.loadGamesByTag(t.Name)
					m.activeView = viewGames
				}
			}
		case viewPlaylists:
			needDetail, g := m.playlists.update(msg)
			if needDetail && g != nil {
				full, err := queryGameByID(g.ID)
				if err == nil {
					m.detailGame = full
					m.prevView = viewPlaylists
					m.activeView = viewDetail
				}
			}
		case viewTools:
			if openPrompt, seed := m.tools.update(msg); openPrompt {
				m.commanding = true
				m.commandInput.Focus()
				m.commandInput.SetValue(seed)
				m.commandInput.SetCursor(len(seed))
				return m, textinput.Blink
			}
		case viewAgent:
			return m, m.agentHandleKey(msg)
		case viewDetail:
			if msg.String() == "enter" || msg.String() == "esc" {
				m.activeView = m.prevView
				m.detailGame = nil
			}
		}
	}
	return m, nil
}

func (m model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.searching = false
		m.searchInput.Blur()
		query := m.searchInput.Value()
		switch m.activeView {
		case viewGames:
			m.games.applyFilter(query)
		case viewTags:
			m.tags.applyFilter(query)
		case viewTools:
			m.tools.filter = query
			m.tools.cursor = 0
			m.tools.offset = 0
		}
		return m, nil
	case "esc":
		m.searching = false
		m.searchInput.Blur()
		return m, nil
	default:
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)
		// Live filter as you type
		query := m.searchInput.Value()
		switch m.activeView {
		case viewGames:
			m.games.applyFilter(query)
		case viewTags:
			m.tags.applyFilter(query)
		case viewTools:
			m.tools.filter = query
			m.tools.cursor = 0
			m.tools.offset = 0
		}
		return m, cmd
	}
}

func (m model) updateCommand(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		raw := strings.TrimSpace(m.commandInput.Value())
		m.commanding = false
		m.commandInput.Blur()
		m.commandInput.SetValue("")
		if raw == "" {
			return m, nil
		}
		m.activeView = viewTools
		return m, callToolCmd(m.tools.client, m.tools.tools, raw)
	case "esc":
		m.commanding = false
		m.commandInput.Blur()
		m.commandInput.SetValue("")
		return m, nil
	default:
		var cmd tea.Cmd
		m.commandInput, cmd = m.commandInput.Update(msg)
		return m, cmd
	}
}

func (m model) handleEsc() (tea.Model, tea.Cmd) {
	switch m.activeView {
	case viewDetail:
		m.activeView = m.prevView
		m.detailGame = nil
	case viewGames:
		if m.games.tagFilter != "" {
			m.games.clearTagFilter()
		} else if m.games.filter != "" {
			m.games.applyFilter("")
		}
	case viewTags:
		if m.tags.filter != "" {
			m.tags.applyFilter("")
		}
	case viewPlaylists:
		m.playlists.handleEsc()
	case viewTools:
		if m.tools.filter != "" {
			m.tools.filter = ""
			m.tools.cursor = 0
			m.tools.offset = 0
		}
	}
	return m, nil
}

func (m model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	var content string

	switch m.activeView {
	case viewGames:
		content = m.games.render()
	case viewTags:
		content = m.tags.render()
	case viewPlaylists:
		content = m.playlists.render()
	case viewTools:
		content = m.tools.render()
	case viewAgent:
		content = m.agentRender()
	case viewDetail:
		content = m.renderDetail()
	}

	// Search bar
	var searchBar string
	if m.searching {
		searchBar = m.searchInput.View() + "\n"
	}
	if m.commanding {
		searchBar = m.commandInput.View() + "\n"
	}

	// Status bar
	status := m.renderStatusBar()

	// Assemble: content fills available space
	contentHeight := m.height - 2 // status bar + search
	if m.searching {
		contentHeight--
	}

	// Pad content to fill screen
	contentLines := strings.Count(content, "\n")
	if contentLines < contentHeight {
		content += strings.Repeat("\n", contentHeight-contentLines)
	}

	return content + searchBar + status
}

func (m model) renderStatusBar() string {
	tabs := []struct {
		label string
		id    viewID
		key   string
	}{
		{"Games", viewGames, "1"},
		{"Tags", viewTags, "2"},
		{"Playlists", viewPlaylists, "3"},
		{"Tools", viewTools, "4"},
	}
	if t := agentTabEntry(); t.label != "" {
		tabs = append(tabs, t)
	}

	var parts []string
	for _, t := range tabs {
		label := fmt.Sprintf(" %s %s ", t.key, t.label)
		if m.activeView == t.id || (m.activeView == viewDetail && m.prevView == t.id) {
			parts = append(parts, statusActiveTab.Render(label))
		} else {
			parts = append(parts, statusTab.Render(label))
		}
	}

	tabBar := lipgloss.JoinHorizontal(lipgloss.Top, parts...)

	// Help hints
	var hints []string
	hints = append(hints, helpKeyStyle.Render("/")+helpDescStyle.Render(" search"))
	hints = append(hints, helpKeyStyle.Render(":")+helpDescStyle.Render(" command"))
	hints = append(hints, helpKeyStyle.Render("Tab")+helpDescStyle.Render(" switch"))
	if m.activeView == viewDetail {
		hints = append(hints, helpKeyStyle.Render("Esc")+helpDescStyle.Render(" back"))
	}
	hints = append(hints, helpKeyStyle.Render("q")+helpDescStyle.Render(" quit"))
	helpBar := strings.Join(hints, "  ")

	// Stats
	gc, tc, pc := queryStats()
	stats := helpDescStyle.Render(fmt.Sprintf("%dG %dT %dP", gc, tc, pc))

	gap := m.width - lipgloss.Width(tabBar) - lipgloss.Width(helpBar) - lipgloss.Width(stats) - 2
	if gap < 1 {
		gap = 1
	}

	return lipgloss.JoinHorizontal(lipgloss.Top,
		tabBar,
		strings.Repeat(" ", gap),
		helpBar,
		"  ",
		stats,
	)
}

func (m model) renderDetail() string {
	if m.detailGame == nil {
		return "No game selected."
	}
	g := m.detailGame

	var b strings.Builder

	b.WriteString(renderTitle(m.width, g.Name) + "\n\n")

	b.WriteString(detailLabelStyle.Render("Platform") + detailValueStyle.Render(g.Platform) + "\n\n")

	if len(g.CRCs) > 0 {
		b.WriteString(detailLabelStyle.Render("CRCs") + "\n")
		for _, crc := range g.CRCs {
			b.WriteString("  " + helpDescStyle.Render(crc) + "\n")
		}
		b.WriteString("\n")
	} else {
		b.WriteString(detailLabelStyle.Render("CRCs") + helpDescStyle.Render("(none)") + "\n\n")
	}

	if len(g.Tags) > 0 {
		b.WriteString(detailLabelStyle.Render("Tags") + "\n")
		var tagParts []string
		for _, t := range g.Tags {
			tagParts = append(tagParts, tagStyle.Render(t))
		}
		// Wrap tags
		line := "  "
		for _, tp := range tagParts {
			if lipgloss.Width(line)+lipgloss.Width(tp) > m.width-4 {
				b.WriteString(line + "\n")
				line = "  "
			}
			line += tp + "  "
		}
		if strings.TrimSpace(line) != "" {
			b.WriteString(line + "\n")
		}
	} else {
		b.WriteString(detailLabelStyle.Render("Tags") + helpDescStyle.Render("(none)") + "\n")
	}

	b.WriteString("\n")
	b.WriteString(helpDescStyle.Render("  Press Esc or Enter to go back") + "\n")

	boxed := detailBorderStyle.Render(b.String())
	// Center it
	padLeft := (m.width - lipgloss.Width(boxed)) / 2
	if padLeft < 0 {
		padLeft = 0
	}
	return "\n" + strings.Repeat(" ", padLeft) + boxed
}
