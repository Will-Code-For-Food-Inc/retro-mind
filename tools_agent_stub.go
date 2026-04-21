//go:build ignore

package main

import "fmt"

func extraToolSchemas() []map[string]interface{} { return nil }

func callExtraTool(name string, args map[string]interface{}) (string, bool) {
	return fmt.Sprintf(`{"error":"unknown tool: %s"}`, name), true
}
