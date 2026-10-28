package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"onigami/internal/agent"
)

// Notebook is a worker's working memory. The to-and-fro we kept seeing came from
// the model re-running tools to "recall" what it already learned (fetch login.jsp,
// forget it, fetch it again). The notebook gives it somewhere to put facts as it
// finds them, so each fact is gathered ONCE and the final report is written from
// the record — the discipline a human analyst keeps in their notes.
//
// One Notebook is created per worker spawn (see NotebookTools), so it's per-worker
// state that lives across that worker's turns and dies with it — consistent with
// the ephemeral-worker design.
type Notebook struct {
	mu    sync.Mutex
	items []note
}

type note struct {
	Phase    string
	Summary  string
	Evidence string
}

// NotebookTools returns the record_finding/list_findings pair sharing one fresh
// Notebook. Add it to a worker's tool belt.
func NotebookTools() []agent.Tool {
	nb := &Notebook{}
	return []agent.Tool{recordFinding{nb}, listFindings{nb}}
}

// --- record_finding -------------------------------------------------------

type recordFinding struct{ nb *Notebook }

func (recordFinding) Name() string { return "record_finding" }
func (recordFinding) Description() string {
	return "Save a confirmed fact to your working notebook: an open port, a service+version, an endpoint, a parameter, a misconfiguration, a CVE, etc. Record every fact the moment you learn it so you never re-run a tool to recall it, and build your final report from these."
}
func (recordFinding) Schema() json.RawMessage {
	return json.RawMessage(`{
      "type":"object",
      "properties":{
        "phase":{"type":"string","description":"which recon phase this came from (triage, discover, enumerate, correlate), optional"},
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
	t.nb.mu.Lock()
	t.nb.items = append(t.nb.items, note{Phase: in.Phase, Summary: in.Summary, Evidence: in.Evidence})
	n := len(t.nb.items)
	t.nb.mu.Unlock()
	return fmt.Sprintf("recorded (%d finding(s) in the notebook).", n), nil
}

// --- list_findings --------------------------------------------------------

type listFindings struct{ nb *Notebook }

func (listFindings) Name() string { return "list_findings" }
func (listFindings) Description() string {
	return "List everything you've recorded in your notebook so far. Check this before running a tool (to avoid repeating work) and use it to write your final report."
}
func (listFindings) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (t listFindings) Run(_ context.Context, _ json.RawMessage) (string, error) {
	t.nb.mu.Lock()
	defer t.nb.mu.Unlock()
	if len(t.nb.items) == 0 {
		return "Notebook is empty — nothing recorded yet.", nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d finding(s) recorded:\n", len(t.nb.items))
	for i, it := range t.nb.items {
		phase := it.Phase
		if phase == "" {
			phase = "-"
		}
		fmt.Fprintf(&b, "  %d. [%s] %s", i+1, phase, it.Summary)
		if it.Evidence != "" {
			fmt.Fprintf(&b, "  (evidence: %s)", clip(it.Evidence, 160))
		}
		b.WriteString("\n")
	}
	return truncate(b.String()), nil // package truncate caps at maxOutput
}

// clip shortens a single value for display (the package-level truncate caps the
// whole blob at maxOutput; this trims one field).
func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
