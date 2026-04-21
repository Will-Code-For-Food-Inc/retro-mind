//go:build ignore

package main

// agentView is a zero-size stub when the agent build tag is not active.
// The Agent tab is absent entirely in non-agent builds.
type agentView struct{}

func newAgentView() agentView { return agentView{} }

type agentResponseMsg struct {
	output string
	err    error
}
