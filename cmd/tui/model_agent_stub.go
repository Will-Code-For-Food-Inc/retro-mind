//go:build ignore

package main

import tea "github.com/charmbracelet/bubbletea"

const viewAgent viewID = 5

func (m *model) agentWindowSize(width, height int)       {}
func (m *model) agentHandleKey(msg tea.KeyMsg) tea.Cmd   { return nil }
func (m *model) agentHandleResponse(msg agentResponseMsg) {}
func (m *model) agentRender() string                      { return "" }
func (m *model) agentClose()                              {}
func (m *model) agentFocus() tea.Cmd                      { return nil }

func agentTabEntry() struct {
	label string
	id    viewID
	key   string
} {
	return struct {
		label string
		id    viewID
		key   string
	}{}
}
