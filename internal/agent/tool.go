package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"onigami/internal/llm"
)

// Tool is any capability an agent can invoke — a port scan, a CVE lookup, an
// HTTP request, or (crucially) "spawn a worker". The agent loop doesn't care
// what a tool actually does; it only knows this interface.
//
// Go interfaces are satisfied implicitly: any type with these four methods IS
// a Tool, no "implements" keyword needed. That's why both a real network tool
// and a worker-dispatcher can slot into the same loop.
type Tool interface {
	// Name is the identifier the model uses to call the tool. Keep it snake_case.
	Name() string

	// Description tells the model when and why to use the tool. The model reads
	// this to decide — treat it like a prompt, make it clear.
	Description() string

	// Schema is the JSON Schema for the tool's input arguments.
	Schema() json.RawMessage

	// Run executes the tool. input is the raw JSON arguments the model supplied;
	// the string returned is handed back to the model as the tool's result.
	Run(ctx context.Context, input json.RawMessage) (string, error)
}

// ToolSet is a named collection of tools. Each agent gets its own ToolSet — the
// Analyst's differs from the Exploit-Dev's, which is how we give each worker a
// different "tool belt".
type ToolSet struct {
	tools map[string]Tool
	order []string // preserves the order tools were added, for stable listing
}

// NewToolSet builds a set from zero or more tools.
func NewToolSet(ts ...Tool) *ToolSet {
	s := &ToolSet{tools: make(map[string]Tool)}
	for _, t := range ts {
		s.Add(t)
	}
	return s
}

// Add registers a tool. A later tool with the same name replaces an earlier one.
func (s *ToolSet) Add(t Tool) {
	if _, exists := s.tools[t.Name()]; !exists {
		s.order = append(s.order, t.Name())
	}
	s.tools[t.Name()] = t
}

// Definitions converts the set into the []llm.ToolDef we advertise to the model.
func (s *ToolSet) Definitions() []llm.ToolDef {
	defs := make([]llm.ToolDef, 0, len(s.order))
	for _, name := range s.order {
		t := s.tools[name]
		defs = append(defs, llm.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			Schema:      t.Schema(),
		})
	}
	return defs
}

// Names lists the tool names in the order they were added (handy for UI/logging).
func (s *ToolSet) Names() []string { return s.order }

// Has reports whether a tool with the given name is in the set.
func (s *ToolSet) Has(name string) bool { _, ok := s.tools[name]; return ok }

// Run looks up the tool named in a model's tool_use block and executes it.
// Returns the llm.Content block to send back — including the error, if any, so
// the model can see what went wrong and adapt.
func (s *ToolSet) Run(ctx context.Context, call llm.Content) llm.Content {
	t, ok := s.tools[call.ToolName]
	if !ok {
		return llm.ToolResultBlock(call.ToolID,
			fmt.Sprintf("no such tool: %q", call.ToolName), true)
	}
	out, err := t.Run(ctx, call.ToolInput)
	if err != nil {
		return llm.ToolResultBlock(call.ToolID, err.Error(), true)
	}
	return llm.ToolResultBlock(call.ToolID, out, false)
}
