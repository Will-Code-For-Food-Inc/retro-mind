//go:build !slim

package main

import (
	"fmt"
	"strings"
)

const (
	normAutoCollapseThreshold = float32(0.93) // auto-use existing, clear duplicate
	normConfirmLowThreshold   = float32(0.80) // below this, auto-create new tag
)

// normalizeTagBatch maps a slice of proposed tags to canonical tags.
// Each proposed tag is either resolved to an existing tag or added as new.
// Requires globalAgent to be set for LLM confirmation in the ambiguous band.
func normalizeTagBatch(proposed []string) []string {
	out := make([]string, 0, len(proposed))
	seen := make(map[string]bool)

	for _, tag := range proposed {
		canon := normalizeOneTag(tag)
		if !seen[canon] {
			out = append(out, canon)
			seen[canon] = true
		}
	}
	return out
}

// normalizeOneTag resolves a single proposed tag to a canonical form.
func normalizeOneTag(tag string) string {
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
	vec := embedTagIfNeeded(tag)
	if vec == nil {
		// Embedding unavailable — add as-is
		ensureTagExists(tag)
		return tag
	}

	// Find nearest existing tag
	matches := semanticSearch(vec, 1)
	if len(matches) == 0 {
		ensureTagExists(tag)
		return tag
	}

	best := matches[0]

	// Zone 1: clear duplicate — auto-collapse
	if best.Similarity >= normAutoCollapseThreshold {
		return best.Tag
	}

	// Zone 3: clearly distinct — auto-create
	if best.Similarity < normConfirmLowThreshold {
		ensureTagExists(tag)
		return tag
	}

	// Zone 2: ambiguous band — ask LLM
	if globalAgent != nil {
		decision := llmClassifyTagPair(tag, best.Tag)
		switch decision {
		case "SAME", "RELATED":
			return best.Tag
		}
	}

	// Fallback: no agent or non-collapsing decision — create new
	ensureTagExists(tag)
	return tag
}

// llmClassifyTagPair asks the LLM whether two tags are the same, related,
// opposite, or distinct. Returns one of: SAME, RELATED, OPPOSITE, DISTINCT.
func llmClassifyTagPair(proposed, existing string) string {
	prompt := fmt.Sprintf(
		`Two video game experience descriptors:
A: "%s"
B: "%s"

Are A and B the same concept (just phrased differently), semantically related (similar vibe, different nuance), antonyms/opposites, or clearly distinct concepts?
Reply with exactly one word: SAME, RELATED, OPPOSITE, or DISTINCT.`,
		proposed, existing,
	)

	msgs := []chatMessage{
		{Role: "system", Content: "You classify pairs of descriptors. Reply with exactly one word."},
		{Role: "user", Content: prompt},
	}

	reply, err := globalAgent.generate(msgs)
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

// ensureTagExists adds the tag to the DB if it doesn't already exist.
func ensureTagExists(tag string) {
	var count int
	db.QueryRow("SELECT COUNT(*) FROM tags WHERE name = ?", tag).Scan(&count)
	if count == 0 {
		dbAddTag(tag)
	}
}
