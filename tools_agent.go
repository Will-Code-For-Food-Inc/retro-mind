//go:build !slim

package main

import "fmt"

func extraToolSchemas() []map[string]interface{} {
	return []map[string]interface{}{agentToolSchema()}
}

func callExtraTool(name string, args map[string]interface{}) (string, bool) {
	switch name {
	case "agent_query":
		return callAgentTool(args)
	default:
		return fmt.Sprintf(`{"error":"unknown tool: %s"}`, name), true
	}
}

func agentToolSchema() map[string]interface{} {
	return map[string]interface{}{
		"name":        "agent_query",
		"description": "Run a natural language query against the ROM library. The agent will use available tools to fulfill the request and return a terse data-only response.",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"prompt": map[string]interface{}{
					"type":        "string",
					"description": "Natural language instruction",
				},
			},
			"required": []string{"prompt"},
		},
	}
}

func callAgentTool(args map[string]interface{}) (string, bool) {
	if globalAgent == nil {
		return "agent not available: Ollama not reachable", true
	}
	prompt, _ := args["prompt"].(string)
	if prompt == "" {
		return "prompt required", true
	}
	result, err := globalAgent.runAgentLoop(prompt)
	if err != nil {
		return err.Error(), true
	}
	return result, false
}
