package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const agentSystemPrompt = `You are a vibe-tagging agent for a retro ROM library. Tag each game in your assigned work list with semantic vibe tags.

For each game:

## 1. Get context
Call fetch_game_metadata(name). Validate the result — title similarity is the primary signal:
- GOOD: "VR Troopers" → "VR Troopers", "The Immortal" → "Immortal"
- BAD: "VR Troopers" → "Starship Troopers: Extermination" (superficial word match), "The Immortal" → "Diablo: Immortal" (single word match)
Date check is secondary — RAWG may show re-release dates (Virtual Console etc.). Platform eras: nes=1983-1994, snes=1990-1999, n64=1996-2002, gamegear=1990-2000, mastersystem=1985-1992, gba=2001-2008.
If the match is bad: call flag_for_review(game_name, reason). Skip tagging. Do NOT tag from your own knowledge.
If found:false legitimately: call flag_for_review(game_name, "not found on RAWG"). Skip.

## 2. Generate tags
Pick 4–8 tags describing the experience of playing — not genre, not platform, not franchise name.
Favor mixing existing tags over inventing new ones.

Tag axes:
- Emotional feel: wholesome, punishing, cathartic, existential, tense, whimsical, cozy, melancholy, triumphant, creepy
- Session shape: pick-up-and-play, long-sessions, short-bursts, grind-heavy, one-more-turn
- Social: couch-co-op, good-for-kids, solo-only, competitive
- Engagement: exploratory, story-driven, button-masher, puzzle-forward, muscle-memory, collectathon, systems-heavy
- Sensory: great-soundtrack, visually-striking, chill-music, loud

## 3. Record
You MUST call set_game_tags(name, platform, tags, crcs) before reporting. Do not output a result until set_game_tags has been called for every game. Tags will be normalized automatically — propose your best tags freely.

## 4. Report
One line per game: title → tags applied (or [flagged for review] / [flagged for review — not on RAWG]).

When invoking a tool, respond with a single JSON object and nothing else:
{"name": "<tool_name>", "arguments": {<args>}}
When done, respond with plain text only — one line per game: title → tags applied.`

const agentMaxTurns = 40
const agentNumCtx = 16384

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type agentLLM struct {
	baseURL      string
	model        string
	systemPrompt string
	client       *http.Client
}

var globalAgent *agentLLM

func initAgent(dataDir string) error {
	model := os.Getenv("ROM_TAGGER_MODEL")
	if model == "" {
		model = "gemma4:e2b"
	}
	baseURL := os.Getenv("ROM_TAGGER_OLLAMA_URL")
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}

	// Load the vibe-check agent prompt from the data dir.
	// Copy prompts/vibe-check.md from the repo into the data dir to override the compiled-in default.
	// ROM_TAGGER_PROMPT_FILE overrides the path entirely (escape hatch).
	prompt := agentSystemPrompt
	promptPath := filepath.Join(dataDir, "prompts", "vibe-check.md")
	if override := os.Getenv("ROM_TAGGER_PROMPT_FILE"); override != "" {
		promptPath = override
	}
	if data, err := os.ReadFile(promptPath); err == nil {
		prompt = strings.TrimSpace(string(data))
	}

	globalAgent = &agentLLM{
		baseURL:      baseURL,
		model:        model,
		systemPrompt: prompt,
		client:       &http.Client{Timeout: 120 * time.Second},
	}
	return nil
}

// generate sends a chat history to Ollama and returns the assistant reply.
func (a *agentLLM) generate(messages []chatMessage) (string, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"model":    a.model,
		"messages": messages,
		"stream":  false,
		"options": map[string]interface{}{"num_ctx": agentNumCtx},
	})
	resp, err := a.client.Post(a.baseURL+"/api/chat", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("ollama: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama: HTTP %d", resp.StatusCode)
	}
	var out struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("ollama decode: %w", err)
	}
	return stripThinkingTokens(out.Message.Content), nil
}

type toolCall struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

func (a *agentLLM) runAgentLoop(prompt string) (string, error) {
	sysContent := a.systemPrompt + "\n\nAvailable tools:\n" + toolsJSON()
	messages := []chatMessage{
		{Role: "system", Content: sysContent},
		{Role: "user", Content: prompt},
	}

	for turn := 0; turn < agentMaxTurns; turn++ {
		output, err := a.generate(messages)
		if err != nil {
			return "", err
		}

		messages = append(messages, chatMessage{Role: "assistant", Content: output})

		tc, ok := parseToolCall(output)
		if !ok {
			return output, nil
		}

		result, isError := callTool(tc.Name, tc.Arguments)
		role := "tool"
		if isError {
			role = "tool_error"
		}
		messages = append(messages, chatMessage{
			Role:    role,
			Content: fmt.Sprintf("[%s] %s", tc.Name, result),
		})
	}

	return "", fmt.Errorf("agent: max turns (%d) reached without final answer", agentMaxTurns)
}

// toolsJSON returns available tool schemas as a JSON string, excluding agent_query itself
// and internal dedup tools (check_tag, add_tag) since normalization handles those transparently.
func toolsJSON() string {
	hidden := map[string]bool{
		"agent_query": true,
		"check_tag":   true,
		"add_tag":     true,
	}
	schemas := toolSchemas()
	filtered := schemas[:0]
	for _, s := range schemas {
		if !hidden[s["name"].(string)] {
			filtered = append(filtered, s)
		}
	}
	b, _ := json.MarshalIndent(filtered, "", "  ")
	return string(b)
}

func parseToolCall(s string) (toolCall, bool) {
	s = strings.TrimSpace(stripCodeFence(s))
	// Try the whole string first (handles pretty-printed JSON).
	var tc toolCall
	if err := json.Unmarshal([]byte(s), &tc); err == nil && tc.Name != "" {
		return tc, true
	}
	// Fall back to line-by-line (handles batched single-line calls).
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		if err := json.Unmarshal([]byte(line), &tc); err == nil && tc.Name != "" {
			return tc, true
		}
	}
	return toolCall{}, false
}

func stripCodeFence(s string) string {
	s = trimPrefix(s, "```json\n")
	s = trimPrefix(s, "```\n")
	s = trimSuffix(s, "\n```")
	s = trimSuffix(s, "```")
	return s
}

// stripThinkingTokens removes <think>...</think> blocks that some models emit.
func stripThinkingTokens(s string) string {
	for {
		start := strings.Index(s, "<think>")
		if start == -1 {
			break
		}
		end := strings.Index(s, "</think>")
		if end == -1 {
			s = s[:start]
			break
		}
		s = s[:start] + s[end+len("</think>"):]
	}
	return strings.TrimSpace(s)
}

func trimPrefix(s, prefix string) string {
	if strings.HasPrefix(s, prefix) {
		return s[len(prefix):]
	}
	return s
}

func trimSuffix(s, suffix string) string {
	if strings.HasSuffix(s, suffix) {
		return s[:len(s)-len(suffix)]
	}
	return s
}
