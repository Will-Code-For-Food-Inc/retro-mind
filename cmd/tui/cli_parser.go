package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

type tokenKind int

const (
	tokenEOF tokenKind = iota
	tokenIdent
	tokenString
	tokenNumber
	tokenBool
	tokenNull
	tokenEqual
	tokenComma
	tokenLBracket
	tokenRBracket
)

type token struct {
	kind tokenKind
	text string
	pos  int
}

type tokenCursor struct {
	tokens []token
	pos    int
}

func (c *tokenCursor) mark() int {
	return c.pos
}

func (c *tokenCursor) restore(mark int) {
	c.pos = mark
}

func (c *tokenCursor) peek() token {
	if c.pos >= len(c.tokens) {
		return token{kind: tokenEOF, pos: len(c.tokens)}
	}
	return c.tokens[c.pos]
}

func (c *tokenCursor) next() token {
	tok := c.peek()
	if c.pos < len(c.tokens) {
		c.pos++
	}
	return tok
}

type parseFn[T any] func(*tokenCursor) (T, error)

func mapParser[A any, B any](p parseFn[A], fn func(A) (B, error)) parseFn[B] {
	return func(c *tokenCursor) (B, error) {
		v, err := p(c)
		if err != nil {
			var zero B
			return zero, err
		}
		return fn(v)
	}
}

func choice[T any](parsers ...parseFn[T]) parseFn[T] {
	return func(c *tokenCursor) (T, error) {
		var lastErr error
		for _, p := range parsers {
			mark := c.mark()
			v, err := p(c)
			if err == nil {
				return v, nil
			}
			c.restore(mark)
			lastErr = err
		}
		var zero T
		if lastErr == nil {
			lastErr = fmt.Errorf("no parser matched")
		}
		return zero, lastErr
	}
}

func many[T any](p parseFn[T]) parseFn[[]T] {
	return func(c *tokenCursor) ([]T, error) {
		var out []T
		for {
			mark := c.mark()
			v, err := p(c)
			if err != nil {
				c.restore(mark)
				return out, nil
			}
			out = append(out, v)
		}
	}
}

func expect(kind tokenKind) parseFn[token] {
	return func(c *tokenCursor) (token, error) {
		tok := c.next()
		if tok.kind != kind {
			return token{}, fmt.Errorf("expected %s, got %q", tokenKindName(kind), tok.text)
		}
		return tok, nil
	}
}

func tokenKindName(kind tokenKind) string {
	switch kind {
	case tokenIdent:
		return "identifier"
	case tokenString:
		return "string"
	case tokenNumber:
		return "number"
	case tokenBool:
		return "boolean"
	case tokenNull:
		return "null"
	case tokenEqual:
		return "="
	case tokenComma:
		return ","
	case tokenLBracket:
		return "["
	case tokenRBracket:
		return "]"
	default:
		return "token"
	}
}

func lexCommand(input string) ([]token, error) {
	var toks []token
	for i := 0; i < len(input); {
		r := rune(input[i])
		if unicode.IsSpace(r) {
			i++
			continue
		}
		switch input[i] {
		case '=':
			toks = append(toks, token{kind: tokenEqual, text: "=", pos: i})
			i++
		case ',':
			toks = append(toks, token{kind: tokenComma, text: ",", pos: i})
			i++
		case '[':
			toks = append(toks, token{kind: tokenLBracket, text: "[", pos: i})
			i++
		case ']':
			toks = append(toks, token{kind: tokenRBracket, text: "]", pos: i})
			i++
		case '"', '\'':
			quote := input[i]
			start := i
			i++
			var b strings.Builder
			for i < len(input) {
				ch := input[i]
				if ch == '\\' && i+1 < len(input) {
					b.WriteByte(ch)
					b.WriteByte(input[i+1])
					i += 2
					continue
				}
				if ch == quote {
					raw := string(quote) + b.String() + string(quote)
					s, err := strconv.Unquote(raw)
					if err != nil {
						return nil, fmt.Errorf("invalid string at %d: %w", start, err)
					}
					toks = append(toks, token{kind: tokenString, text: s, pos: start})
					i++
					goto next
				}
				b.WriteByte(ch)
				i++
			}
			return nil, fmt.Errorf("unterminated string at %d", start)
		default:
			start := i
			for i < len(input) {
				ch := input[i]
				if unicode.IsSpace(rune(ch)) || strings.ContainsRune("=,[]", rune(ch)) {
					break
				}
				i++
			}
			word := input[start:i]
			switch word {
			case "true", "false":
				toks = append(toks, token{kind: tokenBool, text: word, pos: start})
			case "null":
				toks = append(toks, token{kind: tokenNull, text: word, pos: start})
			default:
				if _, err := strconv.ParseFloat(word, 64); err == nil && looksNumeric(word) {
					toks = append(toks, token{kind: tokenNumber, text: word, pos: start})
				} else {
					toks = append(toks, token{kind: tokenIdent, text: word, pos: start})
				}
			}
		}
	next:
	}
	toks = append(toks, token{kind: tokenEOF, pos: len(input)})
	return toks, nil
}

func looksNumeric(s string) bool {
	if s == "" {
		return false
	}
	if s[0] == '-' || s[0] == '+' {
		s = s[1:]
	}
	return s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(unicode.IsDigit(r) || r == '.')
	}) == -1
}

type commandArg struct {
	name  string
	value interface{}
}

type parsedCommand struct {
	name       string
	named      map[string]interface{}
	positional []interface{}
}

func parseCommand(input string) (parsedCommand, error) {
	toks, err := lexCommand(strings.TrimSpace(input))
	if err != nil {
		return parsedCommand{}, err
	}
	cursor := &tokenCursor{tokens: toks}

	nameTok, err := expect(tokenIdent)(cursor)
	if err != nil {
		return parsedCommand{}, fmt.Errorf("expected command name")
	}

	args, err := many(parseCommandArg())(cursor)
	if err != nil {
		return parsedCommand{}, err
	}
	if tok := cursor.peek(); tok.kind != tokenEOF {
		return parsedCommand{}, fmt.Errorf("unexpected token %q", tok.text)
	}

	cmd := parsedCommand{
		name:  nameTok.text,
		named: make(map[string]interface{}),
	}
	for _, arg := range args {
		if arg.name == "" {
			cmd.positional = append(cmd.positional, arg.value)
			continue
		}
		cmd.named[arg.name] = arg.value
	}
	return cmd, nil
}

func parseCommandArg() parseFn[commandArg] {
	return choice(parseNamedArg(), parsePositionalArg())
}

func parseNamedArg() parseFn[commandArg] {
	return func(c *tokenCursor) (commandArg, error) {
		mark := c.mark()
		nameTok, err := expect(tokenIdent)(c)
		if err != nil {
			return commandArg{}, err
		}
		if _, err := expect(tokenEqual)(c); err != nil {
			c.restore(mark)
			return commandArg{}, fmt.Errorf("not a named argument")
		}
		v, err := parseValue()(c)
		if err != nil {
			return commandArg{}, err
		}
		return commandArg{name: nameTok.text, value: v}, nil
	}
}

func parsePositionalArg() parseFn[commandArg] {
	return mapParser(parseValue(), func(v interface{}) (commandArg, error) {
		return commandArg{value: v}, nil
	})
}

func parseValue() parseFn[interface{}] {
	return choice(
		mapParser(expect(tokenString), func(t token) (interface{}, error) { return t.text, nil }),
		mapParser(expect(tokenNumber), func(t token) (interface{}, error) {
			if strings.Contains(t.text, ".") {
				return strconv.ParseFloat(t.text, 64)
			}
			n, err := strconv.ParseInt(t.text, 10, 64)
			if err == nil {
				return n, nil
			}
			return strconv.ParseFloat(t.text, 64)
		}),
		mapParser(expect(tokenBool), func(t token) (interface{}, error) { return t.text == "true", nil }),
		mapParser(expect(tokenNull), func(token) (interface{}, error) { return nil, nil }),
		parseArray(),
		mapParser(expect(tokenIdent), func(t token) (interface{}, error) { return t.text, nil }),
	)
}

func parseArray() parseFn[interface{}] {
	return func(c *tokenCursor) (interface{}, error) {
		if _, err := expect(tokenLBracket)(c); err != nil {
			return nil, err
		}
		var values []interface{}
		if c.peek().kind == tokenRBracket {
			c.next()
			return values, nil
		}
		for {
			v, err := parseValue()(c)
			if err != nil {
				return nil, err
			}
			values = append(values, v)
			if c.peek().kind == tokenComma {
				c.next()
				continue
			}
			if _, err := expect(tokenRBracket)(c); err != nil {
				return nil, err
			}
			return values, nil
		}
	}
}

func bindCommandToTool(cmd parsedCommand, catalog map[string]toolSchema) (toolInvocation, error) {
	schema, ok := catalog[cmd.name]
	if !ok {
		return toolInvocation{}, fmt.Errorf("unknown tool %q", cmd.name)
	}

	args := make(map[string]interface{}, len(cmd.named))
	for k, v := range cmd.named {
		if _, ok := schema.InputSchema.Properties[k]; !ok {
			return toolInvocation{}, fmt.Errorf("unknown argument %q for %s", k, cmd.name)
		}
		coerced, err := coerceValue(v, schema.InputSchema.Properties[k])
		if err != nil {
			return toolInvocation{}, fmt.Errorf("%s: %w", k, err)
		}
		args[k] = coerced
	}

	var positionalTargets []string
	for _, name := range schema.InputSchema.Required {
		if _, exists := args[name]; !exists {
			positionalTargets = append(positionalTargets, name)
		}
	}
	if len(positionalTargets) == 0 && len(cmd.positional) > 0 && len(schema.InputSchema.Properties) == 1 {
		for name := range schema.InputSchema.Properties {
			if _, exists := args[name]; !exists {
				positionalTargets = append(positionalTargets, name)
			}
		}
	}
	if len(cmd.positional) > len(positionalTargets) {
		return toolInvocation{}, fmt.Errorf("too many positional arguments for %s", cmd.name)
	}
	for i, raw := range cmd.positional {
		name := positionalTargets[i]
		coerced, err := coerceValue(raw, schema.InputSchema.Properties[name])
		if err != nil {
			return toolInvocation{}, fmt.Errorf("%s: %w", name, err)
		}
		args[name] = coerced
	}

	var missing []string
	for _, name := range schema.InputSchema.Required {
		if _, ok := args[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return toolInvocation{}, fmt.Errorf("missing required args: %s", strings.Join(missing, ", "))
	}

	return toolInvocation{Name: schema.Name, Arguments: args}, nil
}

func coerceValue(v interface{}, prop toolProperty) (interface{}, error) {
	switch prop.Type {
	case "", "string":
		switch x := v.(type) {
		case nil:
			return "", nil
		case string:
			return x, nil
		case int64:
			return strconv.FormatInt(x, 10), nil
		case float64:
			return strconv.FormatFloat(x, 'f', -1, 64), nil
		case bool:
			return strconv.FormatBool(x), nil
		default:
			return nil, fmt.Errorf("expected string")
		}
	case "integer":
		switch x := v.(type) {
		case int64:
			return x, nil
		case float64:
			return int64(x), nil
		case string:
			n, err := strconv.ParseInt(x, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("expected integer")
			}
			return n, nil
		default:
			return nil, fmt.Errorf("expected integer")
		}
	case "number":
		switch x := v.(type) {
		case int64:
			return float64(x), nil
		case float64:
			return x, nil
		case string:
			n, err := strconv.ParseFloat(x, 64)
			if err != nil {
				return nil, fmt.Errorf("expected number")
			}
			return n, nil
		default:
			return nil, fmt.Errorf("expected number")
		}
	case "boolean":
		switch x := v.(type) {
		case bool:
			return x, nil
		case string:
			b, err := strconv.ParseBool(x)
			if err != nil {
				return nil, fmt.Errorf("expected boolean")
			}
			return b, nil
		default:
			return nil, fmt.Errorf("expected boolean")
		}
	case "array":
		raw, ok := v.([]interface{})
		if !ok {
			raw = []interface{}{v}
		}
		out := make([]interface{}, 0, len(raw))
		for _, item := range raw {
			if prop.Items == nil || prop.Items.Type == "" {
				out = append(out, item)
				continue
			}
			cv, err := coerceValue(item, *prop.Items)
			if err != nil {
				return nil, err
			}
			out = append(out, cv)
		}
		return out, nil
	default:
		return v, nil
	}
}

func formatToolUsage(schema toolSchema) string {
	var parts []string
	for _, name := range schema.InputSchema.Required {
		parts = append(parts, "<"+name+">")
	}
	var optional []string
	for name := range schema.InputSchema.Properties {
		if contains(schema.InputSchema.Required, name) {
			continue
		}
		optional = append(optional, name+"=<value>")
	}
	sort.Strings(optional)
	parts = append(parts, optional...)
	return strings.TrimSpace(schema.Name + " " + strings.Join(parts, " "))
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

type toolInvocation struct {
	Name      string
	Arguments map[string]interface{}
}
