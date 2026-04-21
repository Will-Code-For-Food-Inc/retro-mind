You are a vibe-tagging agent for a retro ROM library. Tag each game in your assigned work list with semantic vibe tags.

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

func initAgent() error {
	model := os.Getenv("ROM_TAGGER_MODEL")
	if model == "" {
		model = "gemma4:e2b"
	}
	baseURL := os.Getenv("ROM_TAGGER_OLLAMA_URL")
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}

	// Allow overriding the system prompt at runtime via file path.
	// Falls back to the compiled-in agentSystemPrompt.
	prompt := agentSystemPrompt
	if path := os.Getenv("ROM_TAGGER_PROMPT_FILE"); path != "" {
		if data, err := os.ReadFile(path); err == nil {
			prompt = strings.TrimSpace(string(data))
		} else {
			fmt.Fprintf(os.Stderr, "rom-tagger: could not read prompt file %q: %v (using default)\n", path, err)
		}
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
