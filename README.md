# onigami

An agentic AI system in Go for **authorized, ethical** red-team / security
learning. One orchestrator agent plans an engagement and delegates to two
specialist workers — an **Analyst** (recon + vulnerability research) and an
**Exploit-Dev** (proof-of-concept code) — then synthesizes their reports.

> Status: early. The engine runs end-to-end against the real **Groq** API.
> The Bubble Tea TUI is next.

## The idea in one picture

```
            ORCHESTRATOR  (long-lived, holds the plan & findings)
                 │  delegates via "dispatch" tools
      ┌──────────┴───────────┐
      ▼                      ▼
   ANALYST               EXPLOIT-DEV
  (ephemeral,            (ephemeral,
   stateless)            stateless)
   tools: recon,         writes a PoC for ONE
   cve_lookup            confirmed finding
```

- **Orchestrator** is the only agent with memory. It decides what to do next,
  writes a tailored brief, spawns a worker, reads the one report that comes
  back, and repeats.
- **Workers are ephemeral and stateless**: a fresh one is built for every task
  and discarded after it reports. Nothing carries over between them.
- **The key trick:** a worker is exposed to the orchestrator as a *tool*
  (`dispatch_analyst`, `dispatch_exploit_dev`). So the orchestrator "delegating"
  and a worker "using nmap" are the same mechanism — the agent loop — at two
  levels.

## Layout

```
cmd/onigami/main.go        CLI entry point; runs a demo engagement
internal/
  llm/llm.go               the model abstraction (provider-agnostic)
  llm/groq.go              the Groq client (OpenAI-compatible)
  config/env.go            loads the .env file
  agent/tool.go            the Tool interface + ToolSet
  agent/agent.go           THE AGENT LOOP (start here)
  orchestrator/orch.go     assembles the brain + its two workers
  workers/workers.go       the specialists + "worker-as-a-tool"
  tools/stubs.go           placeholder safe tools (recon, cve_lookup)
```

Read order for understanding the design: `agent/agent.go` →
`workers/workers.go` → `orchestrator/orch.go`.

## Run

1. Put your Groq API key in `.env` at the project root (get one free at
   https://console.groq.com/keys):

   ```
   GROQ_API_KEY=your_key_here
   # optional: GROQ_MODEL=openai/gpt-oss-120b
   ```

2. Run it:

   ```bash
   go run ./cmd/onigami
   ```

You'll see the orchestrator spawn the Analyst (which calls its tools), then the
Exploit-Dev, then print a final engagement summary.

The key is read from `.env` (which is gitignored) or a real `GROQ_API_KEY`
environment variable — never from the source code.

### Model choice (Groq)

Groq is OpenAI-compatible and fast — ideal for agent loops, which make many
sequential calls. Free-tier models worth using here:

| Model | Why |
|---|---|
| `llama-3.3-70b-versatile` (default) | most reliable tool-calling |
| `openai/gpt-oss-120b` | stronger planning; good for the orchestrator |
| `openai/gpt-oss-20b` | smaller/faster |

The key is read from `GROQ_API_KEY` — it never goes in the code.

## Design notes

- **Everything depends on the `llm.Client` interface, never a concrete model.**
  Swapping or adding a model provider is a one-file change (`internal/llm/`);
  the agents and orchestrator never change.
- **Agents never import the UI.** They emit `agent.Event` values through an
  `Observer` callback; the CLI (and later the TUI) decide how to display them.
- **Safety cap:** every agent has a `MaxTurns` limit so a confused loop can't
  run forever.

## Scope / ethics

onigami is for authorized security testing, CTFs, labs, and learning. The
orchestrator and worker prompts state the engagement is authorized and ethical.
Scope enforcement, authorization checks, and audit logging are planned as
first-class features.
# Onigami
