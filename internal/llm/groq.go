package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Groq is a real llm.Client backed by Groq's API.
//
// Groq speaks the OpenAI "chat completions" format, which is shaped a little
// differently from our internal types:
//
//   - a system prompt is just another message with role "system"
//   - when the model wants a tool, it returns `tool_calls` on the assistant
//     message, and we reply with separate messages of role "tool"
//   - a tool call's arguments arrive as a JSON *string*, not a JSON object
//
// So this file's real job is TRANSLATION: our llm.Request → Groq's JSON on the
// way out, and Groq's JSON → our llm.Response on the way back. The rest of
// onigami never sees any of this; it only knows the Client interface.
type Groq struct {
	apiKey  string
	baseURL string
	http    *http.Client
}

// NewGroq builds a Groq client. The API key should come from the environment
// (GROQ_API_KEY), not hard-coded — see main.go.
func NewGroq(apiKey string) *Groq {
	return &Groq{
		apiKey:  apiKey,
		baseURL: "https://api.groq.com/openai/v1",
		// Generous timeout: agent turns with big contexts can take a while.
		http: &http.Client{Timeout: 120 * time.Second},
	}
}

// --- the wire format (only the fields we use) ----------------------------
//
// These structs with `json:"..."` tags describe exactly the JSON Groq expects
// and returns. Fields tagged `omitempty` disappear from the JSON when empty,
// which keeps requests clean and valid.

type groqRequest struct {
	Model           string        `json:"model"`
	Messages        []groqMessage `json:"messages"`
	Tools           []groqTool    `json:"tools,omitempty"`
	MaxTokens       int           `json:"max_tokens,omitempty"`
	ReasoningFormat string        `json:"reasoning_format,omitempty"` // "parsed" for reasoning models
}

type groqMessage struct {
	Role       string         `json:"role"`                   // system | user | assistant | tool
	Content    string         `json:"content"`                // text (may be empty for tool-call turns)
	Reasoning  string         `json:"reasoning,omitempty"`    // chain-of-thought (reasoning models)
	ToolCalls  []groqToolCall `json:"tool_calls,omitempty"`   // assistant asking for tools
	ToolCallID string         `json:"tool_call_id,omitempty"` // which call a "tool" message answers
}

type groqTool struct {
	Type     string       `json:"type"` // always "function"
	Function groqFunction `json:"function"`
}

type groqFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"` // JSON Schema
}

type groqToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // "function"
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // JSON, but as a *string*
	} `json:"function"`
}

type groqResponse struct {
	Choices []struct {
		Message      groqMessage `json:"message"`
		FinishReason string      `json:"finish_reason"` // stop | tool_calls | length
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Complete implements the Client interface: translate, send, translate back.
//
// On the free tier, token-per-minute limits mean HTTP 429 is expected, not
// exceptional. So instead of failing, we read how long Groq says to wait, sleep
// that long (respecting cancellation), and retry — up to a few times.
func (g *Groq) Complete(ctx context.Context, req Request) (Response, error) {
	body := groqRequest{
		Model:     req.Model,
		Messages:  toGroqMessages(req.System, req.Messages),
		Tools:     toGroqTools(req.Tools),
		MaxTokens: req.MaxTokens,
	}
	// Ask reasoning models to return their chain-of-thought in a separate field
	// (so we can show it). Only for models that support it, or the call errors.
	if isReasoningModel(req.Model) {
		body.ReasoningFormat = "parsed"
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("encode request: %w", err)
	}

	const maxAttempts = 6
	for attempt := 1; ; attempt++ {
		respBytes, status, headers, err := g.send(ctx, raw)
		if err != nil {
			return Response{}, err
		}

		if status == http.StatusTooManyRequests && attempt < maxAttempts {
			wait := parseRetryAfter(headers, string(respBytes))
			fmt.Fprintf(os.Stderr, "[groq] rate limited; waiting %s then retrying (%d/%d)\n",
				wait.Round(time.Second), attempt, maxAttempts)
			select {
			case <-time.After(wait):
				continue
			case <-ctx.Done():
				return Response{}, ctx.Err()
			}
		}

		if status != http.StatusOK {
			// Surface Groq's own error text — it usually says exactly what's wrong.
			return Response{}, fmt.Errorf("groq returned %d %s: %s",
				status, http.StatusText(status), strings.TrimSpace(string(respBytes)))
		}

		var gr groqResponse
		if err := json.Unmarshal(respBytes, &gr); err != nil {
			return Response{}, fmt.Errorf("decode response: %w", err)
		}
		if gr.Error != nil {
			return Response{}, fmt.Errorf("groq error: %s", gr.Error.Message)
		}
		if len(gr.Choices) == 0 {
			return Response{}, fmt.Errorf("groq returned no choices")
		}
		return fromGroqChoice(gr.Choices[0].Message, gr.Choices[0].FinishReason), nil
	}
}

// send performs one HTTP attempt, returning the raw body, status code, and
// response headers.
func (g *Groq) send(ctx context.Context, raw []byte) ([]byte, int, http.Header, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		g.baseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return nil, 0, nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+g.apiKey)

	resp, err := g.http.Do(httpReq)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("groq request failed: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return b, resp.StatusCode, resp.Header, nil
}

// --- outbound translation: our types → Groq's ----------------------------

// toGroqMessages flattens our conversation into Groq's message list. The system
// prompt goes first; then each of our messages becomes one or more Groq
// messages depending on what blocks it holds.
func toGroqMessages(system string, msgs []Message) []groqMessage {
	out := make([]groqMessage, 0, len(msgs)+1)
	if system != "" {
		out = append(out, groqMessage{Role: "system", Content: system})
	}

	for _, m := range msgs {
		switch m.Role {
		case RoleUser:
			// A user turn is either plain text, or a batch of tool results.
			// Tool results must each become their own role:"tool" message.
			var text string
			hadToolResult := false
			for _, c := range m.Content {
				switch c.Type {
				case ContentText:
					text += c.Text
				case ContentToolResult:
					hadToolResult = true
					out = append(out, groqMessage{
						Role:       "tool",
						ToolCallID: c.ToolResultFor,
						Content:    c.ToolResult,
					})
				}
			}
			if text != "" || !hadToolResult {
				out = append(out, groqMessage{Role: "user", Content: text})
			}

		case RoleAssistant:
			// An assistant turn may carry text and/or tool calls.
			gm := groqMessage{Role: "assistant"}
			for _, c := range m.Content {
				switch c.Type {
				case ContentText:
					gm.Content += c.Text
				case ContentToolUse:
					tc := groqToolCall{ID: c.ToolID, Type: "function"}
					tc.Function.Name = c.ToolName
					// Groq wants arguments as a string; our input is raw JSON.
					tc.Function.Arguments = string(c.ToolInput)
					gm.ToolCalls = append(gm.ToolCalls, tc)
				}
			}
			out = append(out, gm)
		}
	}
	return out
}

// toGroqTools converts our tool definitions into Groq's function-tool format.
func toGroqTools(defs []ToolDef) []groqTool {
	if len(defs) == 0 {
		return nil
	}
	out := make([]groqTool, 0, len(defs))
	for _, d := range defs {
		out = append(out, groqTool{
			Type: "function",
			Function: groqFunction{
				Name:        d.Name,
				Description: d.Description,
				Parameters:  d.Schema,
			},
		})
	}
	return out
}

// --- inbound translation: Groq's reply → our types -----------------------

// fromGroqChoice turns Groq's assistant message + finish reason back into our
// Response (one assistant Message plus a StopReason the agent loop branches on).
func fromGroqChoice(m groqMessage, finish string) Response {
	msg := Message{Role: RoleAssistant}

	if m.Content != "" {
		msg.Content = append(msg.Content, Content{Type: ContentText, Text: m.Content})
	}
	for _, tc := range m.ToolCalls {
		msg.Content = append(msg.Content, Content{
			Type:      ContentToolUse,
			ToolID:    tc.ID,
			ToolName:  tc.Function.Name,
			ToolInput: json.RawMessage(tc.Function.Arguments),
		})
	}

	stop := StopEndTurn
	switch finish {
	case "tool_calls":
		stop = StopToolUse
	case "length":
		stop = StopMaxTokens
	}
	// Some models signal tool use only by returning tool_calls, without setting
	// finish_reason to "tool_calls". Treat any tool call as "must run tools".
	if len(m.ToolCalls) > 0 {
		stop = StopToolUse
	}

	return Response{Message: msg, StopReason: stop, Reasoning: m.Reasoning}
}

// --- helpers --------------------------------------------------------------

// isReasoningModel reports whether a model exposes a chain-of-thought we can
// request separately. Our available reasoning models are gpt-oss* and qwen3*.
func isReasoningModel(model string) bool {
	m := strings.ToLower(model)
	return strings.Contains(m, "gpt-oss") || strings.Contains(m, "qwen3")
}

// retryMsgRe pulls the "try again in 23.9s" hint out of a 429 body.
var retryMsgRe = regexp.MustCompile(`try again in ([0-9]+(?:\.[0-9]+)?)s`)

// parseRetryAfter decides how long to wait before retrying a 429: it prefers
// the Retry-After header, falls back to the body's "try again in Xs" hint, and
// otherwise uses a small default. A 1s cushion is added and the wait is capped.
func parseRetryAfter(h http.Header, body string) time.Duration {
	var secs float64
	if v := h.Get("retry-after"); v != "" {
		secs, _ = strconv.ParseFloat(v, 64)
	}
	if secs == 0 {
		if m := retryMsgRe.FindStringSubmatch(body); m != nil {
			secs, _ = strconv.ParseFloat(m[1], 64)
		}
	}
	if secs == 0 {
		secs = 5 // sensible default when Groq didn't say
	}
	d := time.Duration(secs*float64(time.Second)) + time.Second // +1s cushion
	if d > 90*time.Second {
		d = 90 * time.Second
	}
	return d
}

// compile-time check that *Groq satisfies the Client interface.
var _ Client = (*Groq)(nil)
