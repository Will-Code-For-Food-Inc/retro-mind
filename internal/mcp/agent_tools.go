package mcp

// callAgentTool handles the agent_query tool.
func (s *Server) callAgentTool(args map[string]interface{}) (string, bool) {
	if s.Agent == nil {
		return "agent not available: Ollama not reachable", true
	}
	prompt, _ := args["prompt"].(string)
	if prompt == "" {
		return "prompt required", true
	}
	result, err := s.Agent.RunAgentLoop(prompt, s.ToolsJSON, s.CallTool)
	if err != nil {
		return err.Error(), true
	}
	return result, false
}
