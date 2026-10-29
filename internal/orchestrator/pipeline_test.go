package orchestrator

import (
	"context"
	"strings"
	"sync"
	"testing"

	"onigami/internal/agent"
	"onigami/internal/llm"
	"onigami/internal/scope"
	"onigami/internal/tools"
)

// stubLLM stands in for Groq: it records the system prompt of every call (so a
// test can see which agents actually ran) and returns a one-line text report
// with no tool calls, ending the worker's loop in a single turn.
type stubLLM struct {
	mu      sync.Mutex
	systems []string
}

func (s *stubLLM) Complete(_ context.Context, req llm.Request) (llm.Response, error) {
	s.mu.Lock()
	s.systems = append(s.systems, req.System)
	s.mu.Unlock()
	return llm.Response{
		Message:    llm.Message{Role: llm.RoleAssistant, Content: []llm.Content{{Type: llm.ContentText, Text: "stub report"}}},
		StopReason: llm.StopEndTurn,
	}, nil
}

func (s *stubLLM) sawRole(marker string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sys := range s.systems {
		if strings.Contains(sys, marker) {
			return true
		}
	}
	return false
}

// TestPipelineReachesExploitDev is the regression guard for the whole point of
// the structured pipeline: given a confirmed, exploitable finding in the store,
// the engagement MUST progress all the way to the exploit-dev — the stage that
// free-form runs never used to reach.
func TestPipelineReachesExploitDev(t *testing.T) {
	t.Setenv("ONIGAMI_FINDINGS", t.TempDir()+"/findings.jsonl")

	sc, err := scope.Parse("demo.testfire.net")
	if err != nil {
		t.Fatalf("scope parse: %v", err)
	}

	store := tools.NewFindingsStore()
	// Seed what recon/web-testing would have recorded: a confirmed SQLi with
	// evidence on a web endpoint (so web-stage auto-detection also fires).
	store.Add("web-tester", "test", "SQL injection confirmed on /login parameter uid", "sqlmap: parameter uid is injectable (boolean-based blind)")

	stub := &stubLLM{}
	var stages []string
	obs := func(e agent.Event) {
		if e.Kind == agent.EventMessage && strings.Contains(e.Text, "STAGE") {
			stages = append(stages, e.Text)
		}
	}

	cfg := Config{
		Orchestrator: stub, Analyst: stub, WebTester: stub, ExploitDev: stub, Exploiter: stub,
		OrchestratorModel: "stub",
		Scope:             sc,
		Observer:          obs,
		Approver:          nil, // auto-proceed through the PoC gate
		Findings:          store,
	}

	summary, err := NewPipeline(cfg, "Demonstrate the login SQL injection end to end").Run(context.Background())
	if err != nil {
		t.Fatalf("pipeline run: %v", err)
	}

	if !stub.sawRole("ANALYST") {
		t.Error("recon stage (Analyst) never ran")
	}
	if !stub.sawRole("WEB-TESTER") {
		t.Error("web-testing stage never ran despite a web finding in the store")
	}
	if !stub.sawRole("EXPLOIT-DEV") {
		t.Fatal("engagement never reached the EXPLOIT-DEV — the pipeline's whole purpose")
	}
	if !strings.Contains(summary, "Exploit-Dev PoC") {
		t.Errorf("final summary missing the exploit-dev PoC section:\n%s", summary)
	}
	// A PoC should have been written back into the shared store.
	foundPoC := false
	for _, f := range store.List() {
		if f.Phase == "poc" {
			foundPoC = true
		}
	}
	if !foundPoC {
		t.Error("no PoC finding was recorded in the store")
	}
}

// TestPipelineIgnoresUnverifiedClaims is the hallucination regression guard: an
// agent's UNVERIFIED claim — even a confident "SQLi confirmed" — must never be
// handed to the exploit-dev, while a tool-VERIFIED CVE is.
func TestPipelineIgnoresUnverifiedClaims(t *testing.T) {
	t.Setenv("ONIGAMI_FINDINGS", t.TempDir()+"/findings.jsonl")
	t.Setenv("ONIGAMI_WEB_STAGE", "never")

	sc, _ := scope.Parse("demo.testfire.net")
	store := tools.NewFindingsStore()
	// A hallucination-shaped claim: scary, keyword-rich, but unverified.
	store.AddClaim("web-tester", "test", "SQL injection confirmed on /admin — full DB dump possible", "")
	// A genuine tool-verified CVE record.
	store.Add("cve", "correlate", "CVE-2021-41773 (Important): Apache path traversal to RCE", "CVSS3: 7.5; source: Red Hat securitydata")

	stub := &stubLLM{}
	var pocTargets []string
	obs := func(e agent.Event) {
		if e.Kind == agent.EventMessage && strings.Contains(e.Text, "exploit-dev target") {
			pocTargets = append(pocTargets, e.Text)
		}
	}
	cfg := Config{
		Orchestrator: stub, Analyst: stub, WebTester: stub, ExploitDev: stub, Exploiter: stub,
		OrchestratorModel: "stub", Scope: sc, Observer: obs, Findings: store,
	}
	if _, err := NewPipeline(cfg, "").Run(context.Background()); err != nil {
		t.Fatalf("pipeline run: %v", err)
	}
	if !stub.sawRole("EXPLOIT-DEV") {
		t.Fatal("verified CVE was not handed to the exploit-dev")
	}
	for _, tg := range pocTargets {
		if strings.Contains(strings.ToLower(tg), "/admin") || strings.Contains(strings.ToLower(tg), "db dump") {
			t.Errorf("an UNVERIFIED claim reached the exploit-dev: %q", tg)
		}
	}
}

// TestPipelineStopsWhenNothingExploitable checks the other half of the
// contract: with only an informational, unconfirmed finding, triage selects
// nothing and the pipeline stops BEFORE the exploit-dev rather than handing it a
// guess.
func TestPipelineStopsWhenNothingExploitable(t *testing.T) {
	t.Setenv("ONIGAMI_FINDINGS", t.TempDir()+"/findings.jsonl")
	t.Setenv("ONIGAMI_WEB_STAGE", "never") // keep it to just recon + triage

	sc, _ := scope.Parse("10.0.0.5")
	store := tools.NewFindingsStore()
	store.Add("analyst", "sweep", "Host responds to ping; port 80 closed; nothing confirmed, not tested further", "")

	stub := &stubLLM{}
	cfg := Config{
		Orchestrator: stub, Analyst: stub, WebTester: stub, ExploitDev: stub, Exploiter: stub,
		OrchestratorModel: "stub",
		Scope:             sc,
		Findings:          store,
	}

	if _, err := NewPipeline(cfg, "").Run(context.Background()); err != nil {
		t.Fatalf("pipeline run: %v", err)
	}
	if stub.sawRole("EXPLOIT-DEV") {
		t.Error("exploit-dev ran even though nothing exploitable was confirmed")
	}
}
