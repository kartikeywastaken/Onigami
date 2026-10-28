
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"onigami/internal/agent"
	"onigami/internal/config"
	"onigami/internal/llm"
	"onigami/internal/orchestrator"
	"onigami/internal/scope"
	"onigami/internal/tools"
)

func main() {
	// loading env
	if err := config.LoadDotenv(".env"); err != nil {
		fmt.Fprintln(os.Stderr, "warning: could not read .env:", err)
	}

	// converts thoughts in visible wors in the ui
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

	// The user MUST define the scope. Every scan tool enforces it; nothing is
	// scanned outside it. (For a CTF, this is just the box's IP or range.)
	sc, err := promptScope()
	if err != nil {
		fmt.Fprintln(os.Stderr, "scope error:", err)
		os.Exit(1)
	}

	// The proceed/hold gate the Exploit-Dev's output passes through.
	approve := terminalApprover

	orch := orchestrator.New(client, model, sc, obs, approve)

	fmt.Printf("\nonigami — engagement (groq: %s)\n", model)
	fmt.Printf("scope:    %s\n", sc)
	fmt.Printf("scanners: %s\n", tools.ScannerStatus())
	fmt.Println(strings.Repeat("─", 64))

	objective := "Assess the in-scope target(s) for exploitable vulnerabilities and, if you confirm one, have the Exploit-Dev produce a proof-of-concept."
	brief := fmt.Sprintf("%s\n\nIN-SCOPE TARGETS (authorized): %s\nStay strictly within this scope. Work in steps and delegate to your specialists.",
		objective, sc)

	report, err := orch.Run(context.Background(), agent.Task{
		Objective: objective,
		Brief:     brief,
	}, obs)
	if err != nil {
		fmt.Fprintln(os.Stderr, "run failed:", err)
		os.Exit(1)
	}

	fmt.Println(strings.Repeat("─", 64))
	fmt.Println("FINAL REPORT:")
	fmt.Println(report.Summary)
}

// defaultGroqModel is our pick for agentic tool-calling. gpt-oss-120b has
// strong reasoning + reliable function calling and is available on this key.
// Override it with the GROQ_MODEL env var (e.g. "openai/gpt-oss-20b" for speed,
// or "qwen/qwen3.8-27b").
const defaultGroqModel = "openai/gpt-oss-120b"

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

// stdin is a single shared reader for all interactive prompts. (Using separate
// bufio.Readers on os.Stdin can swallow buffered input, so we keep just one.)
var stdin = bufio.NewReader(os.Stdin)

// promptScope gets the authorized scope from the user. It reads the SCOPE env
// var if set, otherwise asks interactively. Scope is required — no default.
func promptScope() (*scope.Scope, error) {
	raw := os.Getenv("SCOPE")
	if raw == "" {
		fmt.Print("Enter engagement scope (IP, CIDR, or hostname — space/comma separated): ")
		line, _ := stdin.ReadString('\n')
		raw = strings.TrimSpace(line)
	}
	if raw == "" {
		return nil, fmt.Errorf("scope is required (nothing was entered)")
	}
	return scope.Parse(raw)
}

// terminalApprover is the proceed/hold gate. The worker's output has already
// been printed above, so the user can read it, then type their decision.
func terminalApprover(_ context.Context, role agent.Role, _ string) bool {
	fmt.Println()
	fmt.Println(strings.Repeat("─", 64))
	fmt.Printf("⚠  The %s produced the output above (review it).\n", role)
	fmt.Print("   Proceed with it, or hold? [proceed/hold]: ")

	line, _ := stdin.ReadString('\n')
	ans := strings.TrimSpace(strings.ToLower(line))

	proceed := ans == "proceed" || ans == "p"
	if proceed {
		fmt.Println("   → proceeding.")
	} else {
		fmt.Println("   → holding; the engagement will stop here.")
	}
	fmt.Println(strings.Repeat("─", 64))
	return proceed
}

// indentBlock indents every line of a multi-line string, for readable nesting.
func indentBlock(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, ln := range lines {
		lines[i] = prefix + ln
	}
	return strings.Join(lines, "\n")
}
