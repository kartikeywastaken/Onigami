package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"onigami/internal/agent"
)

// The notebook tools (record_finding / list_findings) are how a worker reads and
// writes the shared, durable FindingsStore. Giving the model somewhere to put
// facts — and recall them — is what stopped the re-fetch wandering; making that
// store engagement-wide and disk-backed (see findings.go) is what stops a crash
// from losing the work.

// NotebookTools returns record_finding/list_findings bound to the shared store,
// tagging everything this worker records with its role.
func NotebookTools(store *FindingsStore, role string) []agent.Tool {
	return []agent.Tool{recordFinding{store, role}, listFindings{store}}
}

// --- record_finding -------------------------------------------------------

type recordFinding struct {
	store *FindingsStore
	role  string
}

func (recordFinding) Name() string { return "record_finding" }
func (recordFinding) Description() string {
	return "Record a finding you have DIRECT tool evidence for (an open port, a service+version, an endpoint, a parameter, a confirmed vuln). You MUST paste the supporting tool output into `evidence` — a finding with no evidence is rejected. Do NOT record anything a tool did not actually return this session; invented facts are the one thing that ruins the engagement. These records are marked 'unverified' (agent-asserted) and are NOT acted on automatically — only tool-produced facts are."
}
func (recordFinding) Schema() json.RawMessage {
	return json.RawMessage(`{
      "type":"object",
      "properties":{
        "phase":{"type":"string","description":"which phase this came from (triage, discover, enumerate, correlate), optional"},
        "summary":{"type":"string","description":"the fact itself, concise, e.g. \"port 80 open: Apache Tomcat/Coyote 1.1\" or \"/login.jsp has form fields uid,passw\""},
        "evidence":{"type":"string","description":"REQUIRED: the exact tool output/detail that supports this fact, copied from a tool result you saw this session"}
      },
      "required":["summary","evidence"]
    }`)
}
func (t recordFinding) Run(_ context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Phase    string `json:"phase"`
		Summary  string `json:"summary"`
		Evidence string `json:"evidence"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("bad input: %w", err)
	}
	if strings.TrimSpace(in.Summary) == "" {
		return "", fmt.Errorf("summary must not be empty")
	}
	// Evidence is mandatory: it forces the model to cite a real tool result
	// rather than assert a fact from nowhere. This is the cheapest, strongest
	// brake on hallucinated findings.
	if strings.TrimSpace(in.Evidence) == "" {
		return "", fmt.Errorf("evidence is required — paste the tool output that supports this finding; do not record a fact you cannot back with a tool result")
	}
	// Agent-asserted → unverified. Only mechanical tools write verified facts.
	n := t.store.AddClaim(t.role, in.Phase, in.Summary, in.Evidence)
	return fmt.Sprintf("recorded as an UNVERIFIED claim (%d finding(s) total). Only tool-produced facts are acted on automatically.", n), nil
}

// --- list_findings --------------------------------------------------------

type listFindings struct{ store *FindingsStore }

func (listFindings) Name() string { return "list_findings" }
func (listFindings) Description() string {
	return "List everything recorded in the shared engagement notebook so far (including facts other workers found). Check this before running a tool, and use it to write your final report."
}
func (listFindings) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (t listFindings) Run(_ context.Context, _ json.RawMessage) (string, error) {
	return truncate(t.store.Report()), nil
}

// clip shortens a single value for display (the package-level truncate caps the
// whole blob at maxOutput; this trims one field).
func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
