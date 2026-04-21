// Package tui provides the retro-mind TUI.
//
// command.go implements a command-line-first DSL parser for the agent tab input.
// Known commands are parsed deterministically via a participle grammar; unrecognized
// input falls through to the LLM as a chat prompt.
package tui

import (
	"strings"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
)

// Usage strings for commands that require arguments.
const (
	UsageTag   = "usage: tag <platform>"
	UsageQuery = "usage: query <game name>"
)

// knownVerbs is the set of verbs recognized by the DSL.
// Anything not in this set falls through to the LLM.
var knownVerbs = map[string]bool{
	"tag":       true,
	"query":     true,
	"normalize": true,
	"status":    true,
	"metrics":   true,
}

// rawCommand is the internal participle AST node.
// It captures the first token as Verb and any trailing tokens as Args.
type rawCommand struct {
	Verb string   `parser:"@Ident"`
	Args []string `parser:"@Ident*"`
}

// Command is the parsed representation of a single agent tab input.
// On a successful parse of a known verb, Verb is set and Args contains
// any positional arguments. Raw always holds the original untrimmed input.
type Command struct {
	// Verb is the command verb (e.g. "tag", "normalize"). Empty on chat fallback.
	Verb string
	// Args are the positional arguments following the verb.
	Args []string
	// Raw is the original input string, always populated.
	Raw string
}

// cmdLexer recognizes identifiers and whitespace for the command grammar.
var cmdLexer = lexer.MustStateful(lexer.Rules{
	"Root": {
		{Name: "Ident", Pattern: `[a-zA-Z0-9_\-]+`, Action: nil},
		{Name: "whitespace", Pattern: `\s+`, Action: nil},
	},
})

var cmdParser = participle.MustBuild[rawCommand](
	participle.Lexer(cmdLexer),
	participle.Elide("whitespace"),
)

// ParseCommand parses raw input into a Command.
//
// If the input starts with a recognized verb (tag, query, normalize, status,
// metrics), it returns the parsed Command and ok=true. Otherwise it returns
// a zero Command and ok=false to indicate the caller should treat the input
// as a chat prompt.
func ParseCommand(raw string) (Command, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Command{Raw: raw}, false
	}

	rc, err := cmdParser.ParseString("", trimmed)
	if err != nil || rc == nil {
		return Command{Raw: raw}, false
	}
	if !knownVerbs[rc.Verb] {
		return Command{Raw: raw}, false
	}
	return Command{Verb: rc.Verb, Args: rc.Args, Raw: raw}, true
}
