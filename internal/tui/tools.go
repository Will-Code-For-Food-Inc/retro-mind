package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type toolRun struct {
	Command string
	Output  string
	IsError bool
}

type toolsView struct {
	width   int
	height  int
	cursor  int
	offset  int
	filter  string
	tools   []toolSchema
	history []toolRun
	err     string
	loading bool
	client  *mcpClient
}

func newToolsView() toolsView {
	return toolsView{
		client:  newMCPClient(),
		loading: true,
	}
}

func (v *toolsView) close() {
	if v.client != nil {
		_ = v.client.Close()
	}
}

func (v *toolsView) toolCatalog() map[string]toolSchema {
	out := make(map[string]toolSchema, len(v.tools))
	for _, tool := range v.tools {
		out[tool.Name] = tool
	}
	return out
}

func (v *toolsView) visibleTools() []toolSchema {
	if v.filter == "" {
		return v.tools
	}
	filter := strings.ToLower(v.filter)
	var out []toolSchema
	for _, tool := range v.tools {
		if strings.Contains(strings.ToLower(tool.Name), filter) || strings.Contains(strings.ToLower(tool.Description), filter) {
			out = append(out, tool)
		}
	}
	return out
}

func (v *toolsView) selectedTool() *toolSchema {
	filtered := v.visibleTools()
	if len(filtered) == 0 || v.cursor >= len(filtered) {
		return nil
	}
	return &filtered[v.cursor]
}

func (v *toolsView) visibleRows() int {
	rows := (v.height / 2) - 3
	if rows < 4 {
		rows = 4
	}
	return rows
}

func loadToolCatalogCmd(client *mcpClient) tea.Cmd {
	return func() tea.Msg {
		tools, err := client.ListTools()
		if err != nil {
			return toolCatalogMsg{err: err}
		}
		sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
		return toolCatalogMsg{tools: tools}
	}
}

func callToolCmd(client *mcpClient, tools []toolSchema, raw string) tea.Cmd {
	return func() tea.Msg {
		if len(tools) == 0 {
			loaded, err := client.ListTools()
			if err != nil {
				return toolCallMsg{command: raw, err: err}
			}
			sort.Slice(loaded, func(i, j int) bool { return loaded[i].Name < loaded[j].Name })
			tools = loaded
		}
		cmd, err := parseCommand(raw)
		if err != nil {
			return toolCallMsg{command: raw, err: err}
		}
		inv, err := bindCommandToTool(cmd, catalogFromList(tools))
		if err != nil {
			return toolCallMsg{command: raw, err: err}
		}
		out, isError, err := client.CallTool(inv.Name, inv.Arguments)
		return toolCallMsg{
			command: raw,
			output:  out,
			isError: isError,
			err:     err,
		}
	}
}

func catalogFromList(tools []toolSchema) map[string]toolSchema {
	out := make(map[string]toolSchema, len(tools))
	for _, tool := range tools {
		out[tool.Name] = tool
	}
	return out
}

func (v *toolsView) update(msg tea.KeyMsg) (openCommand bool, commandSeed string) {
	filtered := v.visibleTools()
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
		if v.cursor < len(filtered)-1 {
			v.cursor++
			if v.cursor >= v.offset+visible {
				v.offset = v.cursor - visible + 1
			}
		}
	case "home", "g":
		v.cursor = 0
		v.offset = 0
	case "end", "G":
		v.cursor = len(filtered) - 1
		if v.cursor < 0 {
			v.cursor = 0
		}
		v.offset = max(0, v.cursor-visible+1)
	case "enter":
		if tool := v.selectedTool(); tool != nil {
			return true, tool.Name + " "
		}
	}
	return false, ""
}

func (v *toolsView) render() string {
	var b strings.Builder
	b.WriteString(renderTitle(v.width, "Tools") + "\n")
	b.WriteString(helpDescStyle.Render("  Browse the MCP tool catalog. Press : to run a command, Enter to prefill the selected tool.") + "\n")
	if v.err != "" {
		b.WriteString(errorStyle.Render("  "+v.err) + "\n")
	}
	b.WriteString("\n")

	filtered := v.visibleTools()
	visible := v.visibleRows()
	end := min(len(filtered), v.offset+visible)

	nameW := 26
	if v.width > 0 {
		nameW = min(32, max(18, v.width/3))
	}
	b.WriteString(headerStyle.Render(fmt.Sprintf("  %-*s %s", nameW, "Tool", "Description")) + "\n")
	if len(filtered) == 0 {
		if v.loading {
			b.WriteString(helpDescStyle.Render("  Loading tools...") + "\n")
		} else {
			b.WriteString(helpDescStyle.Render("  No tools found.") + "\n")
		}
	} else {
		for i := v.offset; i < end; i++ {
			tool := filtered[i]
			line := fmt.Sprintf("  %-*s %s", nameW, truncate(tool.Name, nameW), truncate(tool.Description, max(20, v.width-nameW-6)))
			if i == v.cursor {
				b.WriteString(selectedRowStyle.Render(line) + "\n")
			} else {
				b.WriteString(line + "\n")
			}
		}
	}

	b.WriteString("\n")
	b.WriteString(headerStyle.Render("  Recent Runs") + "\n")
	if len(v.history) == 0 {
		b.WriteString(helpDescStyle.Render("  No commands run yet.") + "\n")
	} else {
		for _, run := range v.history {
			style := helpDescStyle
			if run.IsError {
				style = errorStyle
			}
			b.WriteString(helpKeyStyle.Render("  : "+run.Command) + "\n")
			for _, line := range wrapLines(run.Output, max(20, v.width-4)) {
				b.WriteString(style.Render("    "+line) + "\n")
			}
			b.WriteString("\n")
		}
	}

	if tool := v.selectedTool(); tool != nil {
		b.WriteString(detailBorderStyle.Width(max(30, v.width-4)).Render(renderToolHelp(*tool)))
	}
	return b.String()
}

func renderToolHelp(tool toolSchema) string {
	var b strings.Builder
	b.WriteString(detailLabelStyle.Render("Selected") + detailValueStyle.Render(tool.Name) + "\n")
	b.WriteString(detailLabelStyle.Render("Usage") + helpDescStyle.Render(formatToolUsage(tool)) + "\n")
	b.WriteString(detailLabelStyle.Render("About") + helpDescStyle.Render(tool.Description) + "\n")
	if len(tool.InputSchema.Properties) > 0 {
		b.WriteString(detailLabelStyle.Render("Args") + "\n")
		names := make([]string, 0, len(tool.InputSchema.Properties))
		for name := range tool.InputSchema.Properties {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			prop := tool.InputSchema.Properties[name]
			required := ""
			if contains(tool.InputSchema.Required, name) {
				required = " required"
			}
			b.WriteString("  " + helpKeyStyle.Render(name) + helpDescStyle.Render(fmt.Sprintf(" (%s%s)", prop.Type, required)))
			if prop.Description != "" {
				b.WriteString(helpDescStyle.Render("  " + prop.Description))
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

func wrapLines(text string, width int) []string {
	if text == "" {
		return []string{"(empty response)"}
	}
	var out []string
	for _, raw := range strings.Split(text, "\n") {
		line := raw
		if line == "" {
			out = append(out, "")
			continue
		}
		for lipgloss.Width(line) > width {
			runes := []rune(line)
			cut := width
			if cut > len(runes) {
				cut = len(runes)
			}
			out = append(out, string(runes[:cut]))
			line = string(runes[cut:])
		}
		out = append(out, line)
	}
	return out
}
