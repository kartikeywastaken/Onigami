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
	return "Save a confirmed fact to the shared engagement notebook: an open port, a service+version, an endpoint, a parameter, a misconfiguration, a CVE, etc. Record every fact the moment you learn it so you never re-run a tool to recall it, and build your final report from these."
}
func (recordFinding) Schema() json.RawMessage {
	return json.RawMessage(`{
      "type":"object",
      "properties":{
        "phase":{"type":"string","description":"which phase this came from (triage, discover, enumerate, correlate), optional"},
        "summary":{"type":"string","description":"the fact itself, concise, e.g. \"port 80 open: Apache Tomcat/Coyote 1.1\" or \"/login.jsp has form fields uid,passw\""},
        "evidence":{"type":"string","description":"the tool output or detail that supports it, optional"}
      },
      "required":["summary"]
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
	n := t.store.Add(t.role, in.Phase, in.Summary, in.Evidence)
	return fmt.Sprintf("recorded (%d finding(s) in the engagement notebook).", n), nil
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
