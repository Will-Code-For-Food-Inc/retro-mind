//go:build !slim

package core

import (
	"fmt"
	"strings"

	"github.com/Will-Code-For-Food-Inc/retro-mind/internal/db"
)

// ChatMessage is a role+content pair for LLM generation.
type ChatMessage struct {
	Role    string
	Content string
}

// GenerateFn is a callback that sends messages to an LLM and returns the reply.
type GenerateFn func(messages []ChatMessage) (string, error)

const (
	NormAutoCollapseThreshold = float32(0.93) // auto-use existing, clear duplicate
	NormConfirmLowThreshold   = float32(0.80) // below this, auto-create new tag
)

// NormalizeTagBatch maps a slice of proposed tags to canonical tags.
// Each proposed tag is either resolved to an existing tag or added as new.
// generateFn may be nil; if non-nil it is used for LLM confirmation in the ambiguous band.
func NormalizeTagBatch(proposed []string, generateFn GenerateFn) []string {
	out := make([]string, 0, len(proposed))
	seen := make(map[string]bool)

	for _, tag := range proposed {
		canon := normalizeOneTag(tag, generateFn)
		if !seen[canon] {
			out = append(out, canon)
			seen[canon] = true
		}
	}
	return out
}

// normalizeOneTag resolves a single proposed tag to a canonical form.
func normalizeOneTag(tag string, generateFn GenerateFn) string {
	tag = strings.TrimSpace(strings.ToLower(tag))
	tag = strings.Map(func(r rune) rune {
		if r == ' ' || r == '_' || r == '-' {
			return '-'
		}
		return r
	}, tag)
	tag = strings.Trim(tag, "-")
	if tag == "" {
		return tag
	}

	// Embed the proposed tag (ephemeral if not in DB yet)
	vec := EmbedTagIfNeeded(tag)
	if vec == nil {
		// Embedding unavailable — add as-is
		EnsureTagExists(tag)
		return tag
	}

	// Find nearest existing tag
	matches := SemanticSearch(vec, 1)
	if len(matches) == 0 {
		EnsureTagExists(tag)
		return tag
	}

	best := matches[0]

	// Zone 1: clear duplicate — auto-collapse
	if best.Similarity >= NormAutoCollapseThreshold {
		return best.Tag
	}

	// Zone 3: clearly distinct — auto-create
	if best.Similarity < NormConfirmLowThreshold {
		EnsureTagExists(tag)
		return tag
	}

	// Zone 2: ambiguous band — ask LLM
	if generateFn != nil {
		decision := LLMClassifyTagPair(tag, best.Tag, generateFn)
		switch decision {
		case "SAME", "RELATED":
			return best.Tag
		}
	}

	// Fallback: no agent or non-collapsing decision — create new
	EnsureTagExists(tag)
	return tag
}

// LLMClassifyTagPair asks the LLM whether two tags are the same, related,
// opposite, or distinct. Returns one of: SAME, RELATED, OPPOSITE, DISTINCT.
func LLMClassifyTagPair(proposed, existing string, generateFn GenerateFn) string {
	prompt := fmt.Sprintf(
		`Two video game experience descriptors:
A: "%s"
B: "%s"

Are A and B the same concept (just phrased differently), semantically related (similar vibe, different nuance), antonyms/opposites, or clearly distinct concepts?
Reply with exactly one word: SAME, RELATED, OPPOSITE, or DISTINCT.`,
		proposed, existing,
	)

	msgs := []ChatMessage{
		{Role: "system", Content: "You classify pairs of descriptors. Reply with exactly one word."},
		{Role: "user", Content: prompt},
	}

	reply, err := generateFn(msgs)
	if err != nil {
		return "DISTINCT" // safe fallback
	}

	reply = strings.ToUpper(strings.TrimSpace(reply))
	// Strip any extra words the model might emit
	for _, word := range []string{"SAME", "RELATED", "OPPOSITE", "DISTINCT"} {
		if strings.HasPrefix(reply, word) {
			return word
		}
	}
	return "DISTINCT" // safe fallback
}

// EnsureTagExists adds the tag to the DB if it doesn't already exist.
func EnsureTagExists(tag string) {
	exists, err := db.HasTag(tag)
	if err != nil || !exists {
		db.AddTag(tag)
	}
}
