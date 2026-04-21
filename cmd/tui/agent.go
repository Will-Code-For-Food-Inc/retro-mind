package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Ollama types ─────────────────────────────────────────────────────────────

type ollamaChatMessage struct {
	Role      string             `json:"role"`
	Content   string             `json:"content,omitempty"`
	ToolCalls []ollamaToolCall   `json:"tool_calls,omitempty"`
}

type ollamaToolCall struct {
	Function ollamaToolCallFunc `json:"function"`
}

type ollamaToolCallFunc struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

type ollamaTool struct {
	Type     string           `json:"type"`
	Function ollamaToolSchema `json:"function"`
}

type ollamaToolSchema struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"`
}

type ollamaClient struct {
	baseURL string
	model   string
	http    *http.Client
}

func newOllamaClient() *ollamaClient {
	model := os.Getenv("ROM_TAGGER_MODEL")
	if model == "" {
		model = "qwen3.5:9b"
	}
	baseURL := os.Getenv("ROM_TAGGER_OLLAMA_URL")
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	return &ollamaClient{
		baseURL: baseURL,
		model:   model,
		http:    &http.Client{Timeout: 120 * time.Second},
	}
}

func (c *ollamaClient) chat(messages []ollamaChatMessage, tools []ollamaTool) (ollamaChatMessage, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"model":    c.model,
		"messages": messages,
		"tools":    tools,
		"stream":     false,
		"keep_alive": "15m",
		"options": map[string]interface{}{
			"num_ctx":        20000,
			"temperature":    0.3,
			"top_p":          0.7,
			"top_k":          20,
			"num_predict":    1024,
			"repeat_penalty": 1.1,
		},
	})
	resp, err := c.http.Post(c.baseURL+"/api/chat", "application/json", bytes.NewReader(body))
	if err != nil {
		return ollamaChatMessage{}, fmt.Errorf("ollama: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ollamaChatMessage{}, fmt.Errorf("ollama: HTTP %d", resp.StatusCode)
	}
	var out struct {
		Message           ollamaChatMessage `json:"message"`
		PromptEvalCount   int               `json:"prompt_eval_count"`
		EvalCount         int               `json:"eval_count"`
		TotalDuration     int64             `json:"total_duration"`
		PromptEvalDuration int64            `json:"prompt_eval_duration"`
		EvalDuration      int64             `json:"eval_duration"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return ollamaChatMessage{}, fmt.Errorf("ollama decode: %w", err)
	}
	tokPerSec := float64(out.EvalCount) / (float64(out.EvalDuration)/1e9 + 0.001)
	slog.Info("ollama",
		"tokens_in", out.PromptEvalCount,
		"tokens_out", out.EvalCount,
		"duration_ms", out.TotalDuration/1e6,
		"prompt_ms", out.PromptEvalDuration/1e6,
		"gen_ms", out.EvalDuration/1e6,
		"tok_per_sec", tokPerSec,
	)

	// Record first tool call name if present, for context
	toolName := ""
	if len(out.Message.ToolCalls) > 0 {
		toolName = out.Message.ToolCalls[0].Function.Name
	}
	if db != nil {
		db.Exec(`INSERT INTO llm_metrics (model, prompt_tokens, completion_tokens, total_ms, prompt_ms, gen_ms, tok_per_sec, tool_name)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			c.model, out.PromptEvalCount, out.EvalCount,
			out.TotalDuration/1e6, out.PromptEvalDuration/1e6, out.EvalDuration/1e6,
			tokPerSec, toolName,
		)
	}

	return out.Message, nil
}

// agentHiddenTools lists tools that are hidden from the LLM to reduce schema
// noise. These are admin/maintenance tools that small models don't need.
var agentHiddenTools = map[string]bool{
	"sync_embeddings":      true,
	"sync_game_embeddings": true,
	"resync_metadata_cache": true,
	"prime_metadata_cache": true,
	"reset_queue":          true,
	"emit_playlist":        true,
	"merge_tags":           true,
	"delete_tag":           true,
	"check_tag":            true,
	"add_tag":              true,
	"agent_query":          true,
}

// mcpToolsToOllama converts the MCP tool catalog to Ollama's tool format,
// filtering out tools that shouldn't be exposed to the LLM.
func mcpToolsToOllama(tools []toolSchema) []ollamaTool {
	out := make([]ollamaTool, 0, len(tools))
	for _, t := range tools {
		if agentHiddenTools[t.Name] {
			continue
		}
		out = append(out, ollamaTool{
			Type: "function",
			Function: ollamaToolSchema{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}
	return out
}

// ── Agent view ────────────────────────────────────────────────────────────────

const agentSystemPrompt = `You are a ROM library agent. Use the provided tools to answer questions and perform tasks. Be terse and data-focused.

When asked to tag a game with vibe tags:
1. Call fetch_game_metadata to get the game's description.
2. Generate 4-8 tags describing the experience of playing. Use hyphenated-slugs.
3. Call set_game_tags to save them. You MUST call set_game_tags before responding.

Tag axes (use as guidance, not exhaustive):
- Emotional: wholesome, punishing, cathartic, tense, whimsical, cozy, melancholy, triumphant, creepy
- Session: pick-up-and-play, long-sessions, short-bursts, grind-heavy, one-more-turn
- Social: couch-co-op, good-for-kids, solo-only, competitive
- Engagement: exploratory, story-driven, button-masher, puzzle-forward, muscle-memory, systems-heavy
- Sensory: great-soundtrack, visually-striking, chill-music

When done, give a one-line summary.`

const agentMaxTurns = 12

type agentMessage struct {
	role    string // "user" | "assistant" | "tool" | "error"
	content string
}

// agentState represents the agent tab's state machine.
//
//	┌─────────────┐
//	│ loadingTools │──catalog──→┌──────┐
//	└─────────────┘            │ idle │←─────────────────┐
//	                           └──┬───┘                  │
//	                     enter/cmd│                      │
//	                           ┌──▼──────┐  response/err │
//	                           │thinking │──────────────→┘
//	                           └─────────┘
type agentState int

const (
	agentStateLoadingTools agentState = iota // waiting for MCP tool catalog
	agentStateIdle                           // prompt active, waiting for input
	agentStateThinking                       // LLM generating / tool calls in flight
)

type agentView struct {
	width, height int
	state         agentState
	statusText    string // context for spinner (e.g. "thinking", "normalizing tags")
	history       []agentMessage
	lastExchange  []ollamaChatMessage // last agent turn for session continuity
	spinner       spinner.Model
	input         textinput.Model
	mcp           *mcpClient
	ollama        *ollamaClient
	tools         []toolSchema
	ollamaTools   []ollamaTool
}

type agentResponseMsg struct {
	messages []agentMessage      // all new messages from the run (tool calls + final)
	context  []ollamaChatMessage // full conversation from this turn (for session continuity)
	err      error
}

func newAgentView() agentView {
	sp := spinner.New()
	sp.Spinner = spinner.Dot

	ti := textinput.New()
	ti.Prompt = "> "
	ti.PromptStyle = commandPromptStyle
	ti.CharLimit = 512
	ti.Focus()

	return agentView{
		state:   agentStateLoadingTools,
		spinner: sp,
		input:   ti,
		mcp:     newMCPClient(),
		ollama:  newOllamaClient(),
	}
}

func (v *agentView) close() {
	v.mcp.Close()
}

// loadAgentToolsCmd fetches the MCP tool catalog for the agent.
func loadAgentToolsCmd(mcp *mcpClient) tea.Cmd {
	return func() tea.Msg {
		tools, err := mcp.ListTools()
		if err != nil {
			return toolCatalogMsg{err: err}
		}
		return toolCatalogMsg{tools: tools}
	}
}

// runAgentCmd runs the full agent loop and returns all messages produced.
// priorContext carries the last exchange for session continuity (can be nil).
func runAgentCmd(ollama *ollamaClient, mcp *mcpClient, tools []toolSchema, prompt string, priorContext []ollamaChatMessage) tea.Cmd {
	return func() tea.Msg {
		ollamaTools := mcpToolsToOllama(tools)
		messages := []ollamaChatMessage{
			{Role: "system", Content: agentSystemPrompt},
		}
		// Carry forward the last exchange for continuity.
		if len(priorContext) > 0 {
			messages = append(messages, priorContext...)
		}
		messages = append(messages, ollamaChatMessage{Role: "user", Content: prompt})

		var produced []agentMessage
		lastToolCall := "" // detect repeated identical calls
		toolCallCount := 0

		// After this many tool calls, strip read tools to force a summary.
		const gatherLimit = 8

		// Tools that are only available during the gathering phase.
		gatherOnlyTools := map[string]bool{
			"fetch_game_metadata":       true,
			"list_games":               true,
			"similar_games":            true,
			"game_vibes":               true,
			"get_game":                 true,
			"list_tags":                true,
			"fetch_game_metadata_by_id": true,
		}

		for turn := 0; turn < agentMaxTurns; turn++ {
			// Phase-based tool filtering: after enough gathering, strip read tools.
			activeTools := ollamaTools
			if toolCallCount >= gatherLimit {
				activeTools = filterOllamaTools(ollamaTools, gatherOnlyTools)
			}

			reply, err := ollama.chat(messages, activeTools)
			if err != nil {
				return agentResponseMsg{err: err}
			}
			messages = append(messages, reply)

			// No tool calls — final answer.
			if len(reply.ToolCalls) == 0 {
				if reply.Content != "" {
					produced = append(produced, agentMessage{role: "assistant", content: reply.Content})
				}
				lastCtx := trimContext(messages)
				return agentResponseMsg{messages: produced, context: lastCtx}
			}

			// Execute each tool call.
			for _, tc := range reply.ToolCalls {
				name := tc.Function.Name
				args := tc.Function.Arguments

				argsJSON, _ := json.Marshal(args)
				callSig := name + string(argsJSON)

				// Break loops: if the model calls the exact same tool with the same args, stop.
				if callSig == lastToolCall {
					produced = append(produced, agentMessage{
						role:    "error",
						content: fmt.Sprintf("loop detected: %s called twice with same args, stopping", name),
					})
					lastCtx := trimContext(messages)
					return agentResponseMsg{messages: produced, context: lastCtx}
				}
				lastToolCall = callSig
				toolCallCount++
				produced = append(produced, agentMessage{
					role:    "tool",
					content: fmt.Sprintf("→ %s(%s)", name, string(argsJSON)),
				})

				result, isError, err := mcp.CallTool(name, args)
				var toolResult string
				if err != nil {
					toolResult = fmt.Sprintf("error: %v", err)
					isError = true
				} else {
					toolResult = result
				}

				if isError {
					produced = append(produced, agentMessage{role: "error", content: toolResult})
				} else {
					produced = append(produced, agentMessage{role: "tool", content: "  ← " + truncateStr(toolResult, 120)})
				}

				messages = append(messages, ollamaChatMessage{
					Role:    "tool",
					Content: toolResult,
				})
			}
		}

		lastCtx := trimContext(messages)
		return agentResponseMsg{
			context: lastCtx,
			err:     fmt.Errorf("agent: max turns (%d) reached without final answer", agentMaxTurns),
		}
	}
}

// filterOllamaTools returns tools excluding those in the exclude set.
func filterOllamaTools(tools []ollamaTool, exclude map[string]bool) []ollamaTool {
	out := make([]ollamaTool, 0, len(tools))
	for _, t := range tools {
		if !exclude[t.Function.Name] {
			out = append(out, t)
		}
	}
	return out
}

// trimContext extracts the last user message and everything after it.
// This gives the next call just enough history for continuity.
func trimContext(messages []ollamaChatMessage) []ollamaChatMessage {
	// Find the last user message (skip system).
	lastUser := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			lastUser = i
			break
		}
	}
	if lastUser < 0 {
		return nil
	}
	return messages[lastUser:]
}

func truncateStr(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func (v *agentView) update(msg tea.KeyMsg) tea.Cmd {
	if v.state == agentStateThinking {
		return nil
	}
	switch msg.String() {
	case "enter":
		raw := strings.TrimSpace(v.input.Value())
		if raw == "" || len(v.tools) == 0 {
			return nil
		}
		v.history = append(v.history, agentMessage{role: "user", content: raw})
		v.input.SetValue("")

		// Command dispatch — deterministic commands before LLM fallback.
		if cmd := v.dispatchCommand(raw); cmd != nil {
			return cmd
		}

		// Fallback: pass to LLM.
		v.state = agentStateThinking
		v.statusText = "thinking"
		return tea.Batch(v.spinner.Tick, runAgentCmd(v.ollama, v.mcp, v.tools, raw, v.lastExchange))
	case "up":
	}
	var cmd tea.Cmd
	v.input, cmd = v.input.Update(msg)
	return cmd
}

// ── Command dispatch ────────────────────────────────────────────────────────

// dispatchCommand routes input through the DSL parser (ParseCommand) and
// dispatches to the appropriate handler. Returns nil to fall through to LLM.
func (v *agentView) dispatchCommand(raw string) tea.Cmd {
	cmd, ok := ParseCommand(raw)
	if !ok {
		return nil
	}
	switch cmd.Verb {
	case "tag":
		if len(cmd.Args) == 0 {
			v.history = append(v.history, agentMessage{role: "error", content: UsageTag})
			return nil
		}
		platform := cmd.Args[0]
		v.state = agentStateThinking
		v.statusText = "vibe-checking " + platform
		return tea.Batch(v.spinner.Tick, vibeCheckOneGame(v.mcp, platform))
	case "query":
		if len(cmd.Args) == 0 {
			v.history = append(v.history, agentMessage{role: "error", content: UsageQuery})
			return nil
		}
		game := strings.Join(cmd.Args, " ")
		v.state = agentStateThinking
		v.statusText = "looking up " + game
		return tea.Batch(v.spinner.Tick, runAgentCmd(v.ollama, v.mcp, v.tools, "Look up: "+game, v.lastExchange))
	case "normalize":
		v.state = agentStateThinking
		v.statusText = "normalizing tags"
		return tea.Batch(v.spinner.Tick, normalizeCmd(v.mcp))
	case "status":
		v.state = agentStateThinking
		v.statusText = "checking status"
		return tea.Batch(v.spinner.Tick, statusCmd(v.mcp))
	case "metrics":
		return v.metricsCmd()
	}
	return nil
}

type normalizeResult struct {
	message string
	err     error
}

func normalizeCmd(mcp *mcpClient) tea.Cmd {
	return func() tea.Msg {
		result, _, err := mcp.CallTool("sync_embeddings", map[string]interface{}{})
		if err != nil {
			return agentResponseMsg{err: fmt.Errorf("sync_embeddings: %w", err)}
		}
		return agentResponseMsg{
			messages: []agentMessage{{role: "assistant", content: "Embeddings synced: " + truncateStr(result, 200)}},
		}
	}
}

func statusCmd(mcp *mcpClient) tea.Cmd {
	return func() tea.Msg {
		var lines []string

		// Check each platform for untagged work
		for _, platform := range []string{"nes", "snes", "n64", "gba", "genesis", "gamegear", "mastersystem"} {
			result, _, err := mcp.CallTool("get_work_batch", map[string]interface{}{"platform": platform, "batch_size": 1})
			if err != nil {
				continue
			}
			// Quick parse for count
			var resp struct{ Count int `json:"count"` }
			if json.Unmarshal([]byte(result), &resp) == nil && resp.Count > 0 {
				lines = append(lines, fmt.Sprintf("  %s: has untagged games", platform))
			} else {
				lines = append(lines, fmt.Sprintf("  %s: done", platform))
			}
		}

		// Metrics summary
		var totalCalls int
		var avgTokSec float64
		if db != nil {
			db.QueryRow("SELECT COUNT(*), COALESCE(AVG(tok_per_sec), 0) FROM llm_metrics").Scan(&totalCalls, &avgTokSec)
		}
		if totalCalls > 0 {
			lines = append(lines, fmt.Sprintf("  LLM calls: %d (avg %.1f tok/s)", totalCalls, avgTokSec))
		}

		return agentResponseMsg{
			messages: []agentMessage{{role: "assistant", content: strings.Join(lines, "\n")}},
		}
	}
}

func (v *agentView) metricsCmd() tea.Cmd {
	var lines []string
	if db != nil {
		rows, err := db.Query(`SELECT model, COUNT(*), SUM(prompt_tokens), SUM(completion_tokens),
			AVG(tok_per_sec), SUM(total_ms)/1000 FROM llm_metrics GROUP BY model`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var model string
				var calls, promptTok, completionTok int
				var avgTokSec float64
				var totalSec int
				rows.Scan(&model, &calls, &promptTok, &completionTok, &avgTokSec, &totalSec)
				lines = append(lines, fmt.Sprintf("  %s: %d calls, %d in / %d out tokens, %.1f tok/s avg, %ds total", model, calls, promptTok, completionTok, avgTokSec, totalSec))
			}
		}
	}
	if len(lines) == 0 {
		lines = append(lines, "  No metrics recorded yet.")
	}
	v.history = append(v.history, agentMessage{role: "assistant", content: strings.Join(lines, "\n")})
	return nil
}

// ── Sequential vibe-check loop ──────────────────────────────────────────────

type vibeCheckStartMsg struct {
	platform string
	game     string
	crc      string
}

type vibeCheckProgressMsg struct {
	platform string
	game     string
	tags     string // formatted tag list or error reason
	err      error
	done     bool
}

// vibeCheckOneGame fetches the next untagged game and emits a start message so
// the UI can show the game name before the LLM call begins.
func vibeCheckOneGame(mcp *mcpClient, platform string) tea.Cmd {
	return func() tea.Msg {
		batchResult, _, err := mcp.CallTool("get_work_batch", map[string]interface{}{
			"platform":   platform,
			"batch_size": 1,
		})
		if err != nil {
			return vibeCheckProgressMsg{err: fmt.Errorf("get_work_batch: %w", err)}
		}
		var batch struct {
			Count int `json:"count"`
			Items []struct {
				Name string `json:"name"`
				CRC  string `json:"crc"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(batchResult), &batch); err != nil {
			return vibeCheckProgressMsg{err: fmt.Errorf("parse batch: %w", err)}
		}
		if batch.Count == 0 {
			return vibeCheckProgressMsg{done: true}
		}
		game := batch.Items[0]
		return vibeCheckStartMsg{platform: platform, game: game.Name, crc: game.CRC}
	}
}

// vibeCheckRunGame calls the LLM to tag a single game and returns the result.
func vibeCheckRunGame(ollama *ollamaClient, mcp *mcpClient, tools []toolSchema, platform, gameName, crc string) tea.Cmd {
	return func() tea.Msg {
		prompt := fmt.Sprintf("Tag '%s' on %s with vibe tags. CRC: %s.", gameName, platform, crc)
		agentCmd := runAgentCmd(ollama, mcp, tools, prompt, nil)
		result := agentCmd().(agentResponseMsg)

		summary := gameName + " → "
		if result.err != nil {
			summary += "error: " + result.err.Error()
		} else {
			for _, m := range result.messages {
				if m.role == "assistant" {
					summary += m.content
					break
				}
			}
		}

		slog.Info("vibe-check",
			"game", gameName,
			"platform", platform,
			"model", ollama.model,
		)

		return vibeCheckProgressMsg{
			platform: platform,
			game:     gameName,
			tags:     summary,
		}
	}
}

func (v *agentView) handleVibeStart(msg vibeCheckStartMsg) tea.Cmd {
	v.statusText = fmt.Sprintf("vibe-checking… (%s)", msg.game)
	return vibeCheckRunGame(v.ollama, v.mcp, v.tools, msg.platform, msg.game, msg.crc)
}

func (v *agentView) handleVibeProgress(msg vibeCheckProgressMsg) tea.Cmd {
	if msg.err != nil {
		v.history = append(v.history, agentMessage{role: "error", content: msg.err.Error()})
		v.state = agentStateIdle
		v.statusText = ""
		return nil
	}
	if msg.done {
		v.history = append(v.history, agentMessage{role: "assistant", content: "All games tagged for this platform."})
		v.state = agentStateIdle
		v.statusText = ""
		return nil
	}

	v.history = append(v.history, agentMessage{role: "assistant", content: msg.tags})
	// Fetch the next game; handleVibeStart will update statusText when it arrives.
	return vibeCheckOneGame(v.mcp, msg.platform)
}

func (v *agentView) handleToolCatalog(msg toolCatalogMsg) {
	if msg.err != nil {
		v.history = append(v.history, agentMessage{
			role:    "error",
			content: "failed to load MCP tools: " + msg.err.Error(),
		})
		// Stay in loadingTools — can't proceed without tools.
		return
	}
	v.tools = msg.tools
	v.ollamaTools = mcpToolsToOllama(msg.tools)
	v.state = agentStateIdle
}

func (v *agentView) handleResponse(msg agentResponseMsg) {
	v.state = agentStateIdle
	v.statusText = ""
	if msg.context != nil {
		v.lastExchange = msg.context
	}
	if msg.err != nil {
		v.history = append(v.history, agentMessage{role: "error", content: msg.err.Error()})
		return
	}
	v.history = append(v.history, msg.messages...)
}

func (v *agentView) render() string {
	if v.width == 0 {
		return ""
	}

	var b strings.Builder

	historyHeight := v.height - 3
	if historyHeight < 1 {
		historyHeight = 1
	}

	var lines []string
	for _, msg := range v.history {
		switch msg.role {
		case "user":
			lines = append(lines, helpKeyStyle.Render("you")+"  "+msg.content)
		case "assistant":
			for _, line := range strings.Split(msg.content, "\n") {
				lines = append(lines, "      "+helpDescStyle.Render(line))
			}
		case "tool":
			lines = append(lines, dimStyle.Render(msg.content))
		case "error":
			lines = append(lines, errorStyle.Render("err")+"  "+msg.content)
		}
		lines = append(lines, "")
	}

	if len(lines) == 0 {
		switch v.state {
		case agentStateLoadingTools:
			lines = append(lines, dimStyle.Render("  Loading tools…"))
		case agentStateIdle:
			lines = append(lines, dimStyle.Render(fmt.Sprintf("  Agent ready — %d tools loaded. Type a command or prompt.", len(v.tools))))
		}
	}

	if len(lines) > historyHeight {
		lines = lines[len(lines)-historyHeight:]
	}
	for len(lines) < historyHeight {
		lines = append([]string{""}, lines...)
	}

	for _, l := range lines {
		b.WriteString(l + "\n")
	}

	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(strings.Repeat("─", v.width)) + "\n")

	switch v.state {
	case agentStateLoadingTools:
		b.WriteString(v.spinner.View() + " loading tools…")
	case agentStateThinking:
		b.WriteString(v.spinner.View() + " " + v.statusText)
	case agentStateIdle:
		b.WriteString(v.input.View())
	}

	return b.String()
}
