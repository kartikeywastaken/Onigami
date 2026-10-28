// Command onigami is the CLI entry point.
//
// It runs a single demo engagement against the real Groq API and prints the
// live event stream to stdout, so you can see the orchestrator delegate to its
// workers and the workers call their tools. The Bubble Tea TUI will replace
// this plain printing next — but the engine underneath stays the same, because
// it only speaks agent.Event.
//
// Configuration comes from the .env file at the project root (or real env
// vars): GROQ_API_KEY is required; GROQ_MODEL optionally overrides the model.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"onigami/internal/agent"
	"onigami/internal/config"
	"onigami/internal/llm"
	"onigami/internal/orchestrator"
)

func main() {
	// Load .env from the project root (if present) before reading any config.
	if err := config.LoadDotenv(".env"); err != nil {
		fmt.Fprintln(os.Stderr, "warning: could not read .env:", err)
	}

	// A console observer: turns engine events into indented, labelled lines.
	obs := func(e agent.Event) {
		indent := strings.Repeat("  ", e.Depth)
		label := fmt.Sprintf("%-12s", e.Agent)
		switch e.Kind {
		case agent.EventStart:
			fmt.Printf("%s▶ %s  started: %s\n", indent, label, e.Text)
		case agent.EventToolCall:
			fmt.Printf("%s  %s  → calling tool: %s\n", indent, label, e.Text)
		case agent.EventToolResult:
			fmt.Printf("%s  %s  ← %s\n", indent, label, e.Text)
		case agent.EventMessage:
			fmt.Printf("%s  %s  thinks:\n%s\n", indent, label, indentBlock(e.Text, indent+"      "))
		case agent.EventReport:
			fmt.Printf("%s✔ %s  reported.\n", indent, label)
		case agent.EventError:
			fmt.Printf("%s✘ %s  error: %s\n", indent, label, e.Text)
		}
	}

	// Build the real Groq client. If the key is missing we stop with a clear
	// message pointing at the .env file.
	client, model, err := buildClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	orch := orchestrator.New(client, model, obs)

	objective := "Assess host 10.0.0.5 for exploitable vulnerabilities and demonstrate one."
	fmt.Printf("onigami — demo engagement (groq: %s)\n", model)
	fmt.Println(strings.Repeat("─", 64))

	report, err := orch.Run(context.Background(), agent.Task{
		Objective: objective,
		Brief:     objective + " Work in steps and delegate to your specialists.",
	}, obs)
	if err != nil {
		fmt.Fprintln(os.Stderr, "run failed:", err)
		os.Exit(1)
	}

	fmt.Println(strings.Repeat("─", 64))
	fmt.Println("FINAL REPORT:")
	fmt.Println(report.Summary)
}

// defaultGroqModel is our pick for agentic tool-calling: Llama 3.3 70B has the
// most reliable function-calling support on Groq's free tier. Override it with
// the GROQ_MODEL env var (e.g. "openai/gpt-oss-120b" for stronger planning).
const defaultGroqModel = "llama-3.3-70b-versatile"

// buildClient constructs the real Groq client from configuration. It returns an
// error (rather than falling back to anything) if the API key is missing.
func buildClient() (llm.Client, string, error) {
	key := os.Getenv("GROQ_API_KEY")
	if key == "" {
		return nil, "", fmt.Errorf(
			"GROQ_API_KEY is not set.\n  → Put your key in the .env file at the project root:\n    %s/.env\n  Get a free key at https://console.groq.com/keys",
			mustCwd())
	}
	model := os.Getenv("GROQ_MODEL")
	if model == "" {
		model = defaultGroqModel
	}
	return llm.NewGroq(key), model, nil
}

// mustCwd returns the current working directory for a friendlier error message,
// or "." if it can't be determined.
func mustCwd() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

// indentBlock indents every line of a multi-line string, for readable nesting.
func indentBlock(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, ln := range lines {
		lines[i] = prefix + ln
	}
	return strings.Join(lines, "\n")
}
