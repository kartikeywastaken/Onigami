// Package llm is onigami's abstraction over a large language model.
//
// Nothing else in the project talks to a model provider directly. Everyone
// depends on the Client interface defined here. That means swapping or adding a
// provider (Groq today; anything tomorrow) never touches the agent or
// orchestrator code. This is the single most important design idea in the
// codebase: depend on an interface, not a concrete implementation.
//
// The types below use a message/content-block/tool-call shape that maps cleanly
// onto modern chat APIs, so each provider client (see groq.go) is a thin
// translation layer rather than a rewrite.
package llm

import (
	"context"
	"encoding/json"
)

// Role is who authored a message in the conversation.
type Role string

const (
	RoleUser      Role = "user"      // us (the program) talking to the model
	RoleAssistant Role = "assistant" // the model talking back
)

// ContentType tags what a single Content block holds.
//
// A message is a list of blocks, not just a string, because one assistant turn
// can contain BOTH some text AND one or more requests to call tools. Likewise a
// single user turn can carry the results of several tool calls at once.
type ContentType string

const (
	ContentText       ContentType = "text"        // plain text
	ContentToolUse    ContentType = "tool_use"    // model asks us to run a tool
	ContentToolResult ContentType = "tool_result" // we hand a tool's output back
)

// Content is one block inside a Message.
//
// Which fields are meaningful depends on Type:
//   - ContentText:       Text
//   - ContentToolUse:    ToolID, ToolName, ToolInput   (the model's request)
//   - ContentToolResult: ToolResultFor, ToolResult, IsError  (our reply)
//
// We keep it as one struct with optional fields (rather than a Go interface
// with three implementations) because it keeps each provider client
// simple to write. The cost is that some fields are unused per block.
type Content struct {
	Type ContentType

	// Text — set when Type == ContentText.
	Text string

	// Tool call — set when Type == ContentToolUse.
	ToolID    string          // unique id the model assigns to this call
	ToolName  string          // which tool it wants to run
	ToolInput json.RawMessage // the arguments, as raw JSON

	// Tool result — set when Type == ContentToolResult.
	ToolResultFor string // the ToolID this result answers
	ToolResult    string // the tool's output (we keep it as text)
	IsError       bool   // true if the tool failed
}

// Message is one turn in the conversation: a role plus its content blocks.
type Message struct {
	Role    Role
	Content []Content
}

// --- small constructors & helpers, so callers never build blocks by hand ---

// UserText makes a user message that is just a line of text.
func UserText(s string) Message {
	return Message{Role: RoleUser, Content: []Content{{Type: ContentText, Text: s}}}
}

// UserResults bundles one or more tool results into a single user message.
// (After the model asks for N tools, we run them and reply with all N results
// in one turn — that's what the API expects.)
func UserResults(results ...Content) Message {
	return Message{Role: RoleUser, Content: results}
}

// ToolResultBlock builds the block we send back after running a tool.
func ToolResultBlock(callID, output string, failed bool) Content {
	return Content{
		Type:          ContentToolResult,
		ToolResultFor: callID,
		ToolResult:    output,
		IsError:       failed,
	}
}

// Text concatenates all the text blocks in a message. Handy for pulling the
// model's final written answer out of its last turn.
func (m Message) Text() string {
	var out string
	for _, c := range m.Content {
		if c.Type == ContentText {
			out += c.Text
		}
	}
	return out
}

// ToolCalls returns just the tool_use blocks — i.e. the tools the model is
// asking us to run this turn. Empty if it only produced text.
func (m Message) ToolCalls() []Content {
	var calls []Content
	for _, c := range m.Content {
		if c.Type == ContentToolUse {
			calls = append(calls, c)
		}
	}
	return calls
}

// ToolDef describes a tool to the model so it knows the tool exists, what it's
// for, and what arguments it takes. Schema is a JSON Schema object.
type ToolDef struct {
	Name        string
	Description string
	Schema      json.RawMessage
}

// StopReason tells us WHY the model stopped generating this turn. The agent
// loop branches on this: if it stopped to use a tool, we run the tool and
// continue; otherwise the model is done and we take its text as the answer.
type StopReason string

const (
	StopEndTurn   StopReason = "end_turn"   // model finished its answer
	StopToolUse   StopReason = "tool_use"   // model wants a tool run, then more
	StopMaxTokens StopReason = "max_tokens" // model hit the length limit
)

// Request is everything we send for one model call.
type Request struct {
	System    string    // system prompt: defines the agent's role & rules
	Messages  []Message // the conversation so far
	Tools     []ToolDef // tools the model may call this turn
	Model     string    // model id (e.g. a Groq model)
	MaxTokens int       // cap on the response length
}

// Response is what one model call gives back.
type Response struct {
	Message    Message    // the assistant's turn (text and/or tool calls)
	StopReason StopReason // why it stopped
	Reasoning  string     // the model's chain-of-thought, if it exposed one
}

// Client is the one interface the rest of onigami depends on. The Groq client
// in groq.go implements it; any other provider would implement the same thing.
type Client interface {
	Complete(ctx context.Context, req Request) (Response, error)
}
