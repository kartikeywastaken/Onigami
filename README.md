# onigami

An agentic AI crew in Go for **authorized, ethical** red-team / security learning
(CTFs, labs, and systems you own or are cleared to test). It plans an engagement,
runs real security tools, grounds every finding in their output, and gates anything
that fires live behind an operator approval.

> Status: the engine runs end-to-end against the real **Groq** API, drives real
> tools (nmap, nikto, sqlmap, nuclei, ffuf, testssl.sh, optional Metasploit), and
> ships a Bubble Tea terminal dashboard.

## How it works

onigami runs an engagement as a **fixed pipeline** so a run always drives toward a
proof instead of wandering. Each stage is a fresh, stateless specialist agent; the
orchestrator decides what runs next and carries the findings between stages.

```
STAGE 1  RECON         map the in-scope target (ports, services, web surface)
STAGE 2  WEB TESTING   confirm OWASP-style issues on any web surface found
STAGE 3  TRIAGE        rank confirmed findings, pick what to prove
STAGE 4  EXPLOIT-DEV   write a proof-of-concept for each selected finding
STAGE 5  EXPLOITER     (optional) demonstrate with Metasploit — scope-checked,
                        operator-gated, and audited on every run
```

- **Findings come from tools, not from the model.** Each agent emits real tool
  calls (`exec.CommandContext` to the scanner on `PATH`); coded drivers
  (`recon_sweep`, `web_sweep`) run the scanners and record only *verified* findings.
  An agent that tries to finish without using a tool is bounced back.
- **Workers are ephemeral and stateless.** A fresh one is built per stage and
  discarded after it reports. The orchestrator is the only thing with memory.
- **Nothing dangerous runs unasked.** Every scan is scope-checked, and live
  exploitation (Stage 5) is gated behind an operator approval and written to an
  audit log.

There is also a free-form LLM orchestrator (`--llm-orch`) that delegates to workers
as tools rather than following the fixed stages; the structured pipeline is the
default.

## Layout

```
cmd/onigami/main.go        CLI: flags, model wiring, plain vs. TUI, mission prompt
internal/
  agent/agent.go           THE AGENT LOOP (start here)
  agent/tool.go            the Tool interface + ToolSet
  agent/approval.go        operator gate for live/destructive actions
  orchestrator/pipeline.go the fixed stage pipeline (recon → … → exploiter)
  orchestrator/orch.go     the free-form LLM orchestrator (--llm-orch)
  workers/workers.go       the specialists (recon, web, exploit-dev, exploiter)
  tools/                   real tools: exec, recon, scan, http, tcp, web,
                           websweep, cve, msf, findings, audit, notebook
  scope/scope.go           in-scope enforcement (hosts/targets)
  config/                  .env loading + env-overridable tunables
  llm/                     the provider-agnostic model client (Groq)
  ui/                      the Bubble Tea terminal dashboard
```

Read order for the design: `agent/agent.go` → `workers/workers.go` →
`orchestrator/pipeline.go`.

## Run

1. Put your Groq API key in `.env` at the project root (free at
   https://console.groq.com/keys). `.env` is gitignored — the key is never read
   from source.

   ```
   GROQ_API_KEY=your_key_here
   # optional: GROQ_MODEL=openai/gpt-oss-120b
   ```

2. Give it a mission and an in-scope target. Both are **required**; onigami prompts
   for whatever you don't supply before any scan runs.

   ```bash
   # mission via flag (or ONIGAMI_MISSION); scope prompted interactively
   go run ./cmd/onigami --mission "assess the in-scope web app for exploitable issues"

   # or prefill the scope and skip the prompt
   SCOPE=demo.testfire.net go run ./cmd/onigami --mission "..."

   # live terminal dashboard — enter the target + mission on the landing screen
   go run ./cmd/onigami --tui
   ```

   - **Mission**: `--mission`, the `ONIGAMI_MISSION` env var, or the prompt.
   - **Scope (the authorized target)**: the `SCOPE` env var, the plain-mode prompt,
     or the TUI landing screen. It's mandatory and enforced in code — every scan is
     checked against it.

Only point onigami at systems you are authorized to test.

### Tools

Each stage uses whatever scanners are on your `PATH`; missing ones are skipped. For
the full pipeline, install: `nmap`, `nikto`, `sqlmap`, `nuclei` (+ templates),
`ffuf`, `testssl.sh`, and optionally Metasploit for Stage 5.

### Models (Groq)

Groq is OpenAI-compatible and fast — ideal for agent loops, which make many
sequential calls. Defaults, each overridable by env var:

| Role | Env var | Default |
|---|---|---|
| orchestrator | `GROQ_MODEL_ORCH` | `openai/gpt-oss-120b` |
| web-tester / exploit-dev / exploiter | `GROQ_MODEL_WEBTESTER` etc. | `openai/gpt-oss-120b` |
| analyst (recon) | `GROQ_MODEL_ANALYST` | `openai/gpt-oss-20b` |

`GROQ_MODEL` sets the overall default. The smaller workers use a separate per-model
rate-limit bucket so the orchestrator isn't starved.

## Design notes

- **Everything depends on the `llm.Client` interface, never a concrete model.**
  Swapping or adding a provider is a one-file change in `internal/llm/`; the agents
  and pipeline never change.
- **Agents never import the UI.** They emit `agent.Event` values through an
  `Observer` callback; the CLI and the TUI decide how to display them.
- **Tunables are env-overridable, not hardcoded** — scan caps, timeouts, severities
  and model picks all read from the environment with documented defaults
  (`internal/config/tunables.go`).
- **Safety caps:** every agent has a `MaxTurns` limit so a confused loop can't run
  forever, and scope + approval + audit gate anything that touches a real target.

## Scope / ethics

onigami is for authorized security testing, CTFs, labs, and learning. Scope
enforcement, authorization checks, operator gating, and audit logging are
first-class: scans are checked against the configured scope, and live exploitation
requires an explicit operator approval.
