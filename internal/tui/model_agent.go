package tui

import (
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

const viewAgent viewID = 5

func (m *model) agentWindowSize(width, height int) {
	m.agent.width = width
	m.agent.height = height - 3
}

func (m *model) agentHandleKey(msg tea.KeyMsg) tea.Cmd {
	return m.agent.update(msg)
}

func (m *model) agentHandleResponse(msg agentResponseMsg) {
	m.agent.handleResponse(msg)
}

func (m *model) agentHandleToolCatalog(msg toolCatalogMsg) {
	m.agent.handleToolCatalog(msg)
}

func (m *model) agentHandleSpinnerTick(msg spinner.TickMsg) tea.Cmd {
	var cmd tea.Cmd
	m.agent.spinner, cmd = m.agent.spinner.Update(msg)
	return cmd
}

func (m *model) agentRender() string {
	return m.agent.render()
}

func (m *model) agentClose() {
	m.agent.close()
}

func (m *model) agentFocus() tea.Cmd {
	if m.agent.state == agentStateIdle {
		return m.agent.input.Focus()
	}
	return nil
}

func agentTabEntry() struct {
	label string
	id    viewID
	key   string
} {
	return struct {
		label string
		id    viewID
		key   string
	}{"Agent", viewAgent, "5"}
}
