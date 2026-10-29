// pipeline.go holds onigami's STRUCTURED engagement driver.
//
// orch.go builds an LLM "brain" that is free to delegate to its workers in
// whatever order it decides. That is elegant, but in practice the brain
// meanders, repeats itself, or runs out of its token budget before it ever
// reaches the Exploit-Dev — so a run rarely gets past recon.
//
// The Pipeline fixes that by making the STAGES themselves deterministic. It is
// a plain Go state machine that runs the engagement as an explicit, ordered
// sequence and GUARANTEES the progression recon → web-testing → triage →
// exploit-dev (→ exploiter). The individual agents still think for themselves
// WITHIN a stage (the Analyst follows its own phase order, the Web-Tester
// follows its own, etc.) — but WHICH stage runs next, and the hand-off between
// them, is decided here in code, not by a model. That is the "structured
// mechanism for trying everything in order and finally reaching the exploit
// dev" the engagement needs.
//
// Everything is a pure consumer of the same Config orch.go uses, so the two
// drivers are interchangeable (main.go picks one; --llm-orch selects the old
// brain).
package orchestrator

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"onigami/internal/agent"
	"onigami/internal/config"
	"onigami/internal/tools"
	"onigami/internal/workers"
)

// versionRe spots a software version like "1.1", "2.4.49", "8.5" — a versioned
// service is a legitimate, researchable exploit-dev target.
var versionRe = regexp.MustCompile(`\d+\.\d+`)

// endpointHints mark a finding as a web endpoint worth probing (has parameters
// or a form), which the exploit-dev can reason about concretely.
var endpointHints = []string{"param", "form", "query", "?", "=", "login", "search", "id="}

// actionable reports whether a verified finding is something the exploit-dev can
// actually build a PoC around: a CVE, a versioned service, an injectable/known
// web surface, or an explicit vuln keyword. Bare reachability lines are not.
func actionable(low string) bool {
	if strings.Contains(low, "cve-") {
		return true
	}
	for _, kw := range exploitKeywords {
		if strings.Contains(low, kw) {
			return true
		}
	}
	for _, kw := range endpointHints {
		if strings.Contains(low, kw) {
			return true
		}
	}
	return versionRe.MatchString(low)
}

// pipelineDepth is the nesting level the pipeline's own banners render at; the
// workers it spawns run one deeper (workerDepth), so the UI indents their work.
const (
	pipelineDepth = 0
	workerDepth   = 1
)

// defaultMission is used when the operator supplies no mission of their own. It
// keeps the generic "find something exploitable and PoC it" behaviour.
const defaultMission = "Assess the in-scope target(s) for exploitable vulnerabilities and, for every confirmed issue, produce a proof-of-concept that demonstrates it."

// Pipeline runs an engagement as a fixed sequence of stages. Build it with
// NewPipeline and call Run.
type Pipeline struct {
	cfg     Config
	mission string

	// resolved per-agent models (same fallback rules as New()).
	analystModel    string
	webTesterModel  string
	exploitDevModel string
	exploiterModel  string
}

// NewPipeline assembles the structured driver from the same Config the LLM
// orchestrator uses. mission is the operator's objective; blank falls back to
// defaultMission.
func NewPipeline(cfg Config, mission string) *Pipeline {
	if strings.TrimSpace(mission) == "" {
		mission = defaultMission
	}
	pick := func(m string) string {
		if m == "" {
			return cfg.OrchestratorModel
		}
		return m
	}
	return &Pipeline{
		cfg:             cfg,
		mission:         strings.TrimSpace(mission),
		analystModel:    pick(cfg.AnalystModel),
		webTesterModel:  pick(cfg.WebTesterModel),
		exploitDevModel: pick(cfg.ExploitDevModel),
		exploiterModel:  pick(cfg.ExploiterModel),
	}
}

// Run drives the whole engagement and returns a written summary. An error is
// returned only when a stage fails hard (e.g. the model is unreachable); a
// stage that simply finds nothing is not an error — the pipeline moves on and
// says so. Findings are durable in the store regardless, so the caller can
// always print what was gathered.
func (p *Pipeline) Run(ctx context.Context) (string, error) {
	p.emit(agent.EventStart, "structured engagement — mission: "+p.mission)

	var sections []string
	record := func(title, body string) {
		sections = append(sections, "### "+title+"\n"+strings.TrimSpace(body))
	}

	// ---- STAGE 1: RECON -------------------------------------------------
	p.banner("STAGE 1 · RECON", "map the target: ports, services, versions, endpoints")
	reconReport, _, err := p.runWorker(ctx,
		workers.NewAnalyst(p.cfg.Analyst, p.analystModel,
			agent.NewToolSet(tools.AnalystTools(p.cfg.Scope, p.cfg.Findings)...), workerDepth),
		p.analystBrief(), nil)
	if err != nil {
		record("Recon (incomplete)", "recon stage failed: "+err.Error())
		return p.assemble(sections), fmt.Errorf("recon stage: %w", err)
	}
	record("Recon", reconReport)

	// ---- STAGE 2: WEB TESTING (conditional) -----------------------------
	if p.shouldWebTest() {
		p.banner("STAGE 2 · WEB TESTING", "actively confirm OWASP Top-10 issues on the web surface")
		webReport, _, werr := p.runWorker(ctx,
			workers.NewWebTester(p.cfg.WebTester, p.webTesterModel,
				agent.NewToolSet(tools.WebTesterTools(p.cfg.Scope, p.cfg.Findings)...), workerDepth),
			p.webBrief(), nil)
		if werr != nil {
			// A web-testing failure is not fatal: recon findings may still be
			// worth handing to the exploit-dev, so note it and keep going.
			p.emit(agent.EventError, "web-testing stage failed, continuing with recon findings: "+werr.Error())
			record("Web testing (incomplete)", "web-testing stage failed: "+werr.Error())
		} else {
			record("Web testing", webReport)
		}
	} else {
		p.emit(agent.EventMessage, "STAGE 2 · WEB TESTING skipped (no web surface found in recon; set ONIGAMI_WEB_STAGE=always to force)")
	}

	// ---- STAGE 3: TRIAGE ------------------------------------------------
	p.banner("STAGE 3 · TRIAGE", "rank confirmed findings and select what to build a PoC for")
	candidates := p.triage()
	record("Triage", p.triageReport(candidates))
	if len(candidates) == 0 {
		p.emit(agent.EventMessage, "no confirmed, exploitable finding to hand off — stopping before exploit-dev")
		return p.assemble(sections), nil
	}

	// ---- STAGE 4: EXPLOIT-DEV (loop over the selected findings) ---------
	// This is the loop the engagement never used to reach: one PoC per selected
	// finding, each gated by the operator.
	p.banner("STAGE 4 · EXPLOIT-DEV", fmt.Sprintf("produce a PoC for the top %d finding(s)", len(candidates)))
	for i, c := range candidates {
		p.emit(agent.EventMessage, fmt.Sprintf("exploit-dev target %d/%d: %s", i+1, len(candidates), oneLine(c.Summary, 120)))
		poc, proceed, derr := p.runWorker(ctx,
			workers.NewExploitDev(p.cfg.ExploitDev, p.exploitDevModel, p.exploitDevTools(), workerDepth),
			p.exploitDevBrief(c), p.cfg.Approver)
		if derr != nil {
			p.emit(agent.EventError, "exploit-dev failed for this finding, moving on: "+derr.Error())
			record(fmt.Sprintf("Exploit-Dev (target %d, failed)", i+1), derr.Error())
			continue
		}
		p.cfg.Findings.Add(string(agent.RoleExploitDev), "poc", oneLine(c.Summary, 160), poc)
		record(fmt.Sprintf("Exploit-Dev PoC (target %d)", i+1), poc)
		if !proceed {
			p.emit(agent.EventMessage, "operator held after this PoC — stopping the engagement here")
			record("Engagement paused", "The operator reviewed the PoC above and chose to HOLD; later targets were not attempted.")
			return p.assemble(sections), nil
		}

		// ---- STAGE 5: EXPLOITER (optional, only when MSF is available) --
		if p.cfg.MSF != nil && p.cfg.MSF.Available() && c.MSFShaped {
			p.banner("STAGE 5 · EXPLOITER", "demonstrate with Metasploit (each run scope-checked, gated, audited)")
			xr, _, xerr := p.runWorker(ctx,
				workers.NewExploiter(p.cfg.Exploiter, p.exploiterModel,
					agent.NewToolSet(tools.ExploiterTools(p.cfg.MSF, p.cfg.Scope, p.cfg.Approver, p.cfg.Audit)...), workerDepth),
				p.exploiterBrief(c), nil)
			if xerr != nil {
				p.emit(agent.EventError, "exploiter failed: "+xerr.Error())
				record(fmt.Sprintf("Exploiter (target %d, failed)", i+1), xerr.Error())
			} else {
				p.cfg.Findings.Add(string(agent.RoleExploiter), "demo", oneLine(c.Summary, 160), xr)
				record(fmt.Sprintf("Exploiter (target %d)", i+1), xr)
			}
		}
	}

	p.emit(agent.EventReport, "structured engagement complete")
	return p.assemble(sections), nil
}

// exploitDevTools builds the exploit-dev's tool belt. It is prose-first (it
// reasons a PoC in writing), but when Metasploit is wired up it also gets the
// READ-ONLY module-grounding tools so a PoC can cite a real module, its real
// options, and the versions it actually affects — instead of an invented one.
// It never gets a tool that runs or checks a module; that stays with the gated
// exploiter (Stage 5).
func (p *Pipeline) exploitDevTools() *agent.ToolSet {
	if p.cfg.MSF != nil && p.cfg.MSF.Available() {
		return agent.NewToolSet(tools.MSFGroundingTools(p.cfg.MSF)...)
	}
	return agent.NewToolSet()
}

// --- stage helpers --------------------------------------------------------

// runWorker runs a freshly-built worker on a brief and returns (summary,
// proceed, err). proceed is the operator's gate decision (always true when gate
// is nil). The worker streams its own events to the observer as it goes.
func (p *Pipeline) runWorker(ctx context.Context, w *agent.Agent, brief string, gate agent.Approver) (string, bool, error) {
	rep, err := w.Run(ctx, agent.Task{Objective: string(w.Role) + " task", Brief: brief}, p.cfg.Observer)
	if err != nil {
		return "", false, err
	}
	if !gate.Ask(ctx, w.Role, rep.Summary) {
		return rep.Summary, false, nil
	}
	return rep.Summary, true, nil
}

// emit sends one event to the observer (nil-safe).
func (p *Pipeline) emit(kind agent.EventKind, text string) {
	if p.cfg.Observer != nil {
		p.cfg.Observer(agent.Event{Agent: agent.RoleOrchestrator, Kind: kind, Text: text, Depth: pipelineDepth})
	}
}

// banner announces a stage boundary in a way that stands out in both the plain
// log and the TUI activity stream.
func (p *Pipeline) banner(title, detail string) {
	p.emit(agent.EventMessage, "══════ "+title+" ══════\n"+detail)
}

// --- briefs ---------------------------------------------------------------

func (p *Pipeline) scopeLine() string {
	return fmt.Sprintf("IN-SCOPE TARGETS (authorized, do not stray outside): %s", p.cfg.Scope)
}

func (p *Pipeline) analystBrief() string {
	return fmt.Sprintf(`MISSION: %s

%s

You are STAGE 1 (RECON) of a structured engagement. Follow your fixed phase
order (SWEEP → CORRELATE → REPORT) and record everything to the shared
notebook. Your report feeds the next stage, so be concrete: list each open
service with its version, every endpoint/parameter you found, and any CVE that
matches, each with a calibrated confidence.`, p.mission, p.scopeLine())
}

func (p *Pipeline) webBrief() string {
	return fmt.Sprintf(`MISSION: %s

%s

You are STAGE 2 (WEB TESTING) of a structured engagement. Recon has already run
and recorded what it found — here is a digest:

%s

Orient from the shared notebook FIRST (list_findings), then call web_sweep ONCE
to run the mechanical nuclei+ffuf+sqlmap pass — it records verified findings for
you. Follow up by hand only where it left a concrete gap. The verified findings
are what the next stage will build a proof-of-concept from; do not assert any
issue a tool did not actually confirm.`,
		p.mission, p.scopeLine(), p.findingsDigest(24))
}

func (p *Pipeline) exploitDevBrief(c candidate) string {
	return fmt.Sprintf(`MISSION: %s

%s

You are STAGE 4 (EXPLOIT-DEV) of a structured engagement. The following finding
is TOOL-VERIFIED (it came from a scanner or the CVE database, not a guess).
Build a clear proof-of-concept for THIS finding and nothing else.

FINDING: %s
EVIDENCE: %s

GROUNDING RULES (important):
- Use ONLY the target, version, endpoint, and parameters given above. Do NOT
  invent any endpoint, parameter, credential, version, or CVE id. If a detail
  you need is not provided, write "unknown — would need to confirm" instead of
  guessing.
- If the finding is a CVE record, state the version precondition plainly: the
  PoC applies IF the target runs the affected version.
- If msf_search / msf_module_info are available and this is a CVE or versioned
  service, ground the PoC in a REAL Metasploit module: search, read its real
  options and affected versions, and cite them. These are read-only — do not run
  or check a module. If nothing matches, say so and write a manual PoC instead.
- Explain what the PoC does, its preconditions, and how a defender detects or
  remediates it. Keep it scoped to this one finding.`,
		p.mission, p.scopeLine(), strings.TrimSpace(c.Summary), orDash(c.Evidence))
}

func (p *Pipeline) exploiterBrief(c candidate) string {
	return fmt.Sprintf(`MISSION: %s

%s

You are STAGE 5 (EXPLOITER) of a structured engagement. Demonstrate the
following already-confirmed finding against the in-scope target with
Metasploit. Prefer mode "check" first. Every run is scope-checked, operator-
gated, and audited.

FINDING: %s
EVIDENCE: %s`,
		p.mission, p.scopeLine(), strings.TrimSpace(c.Summary), orDash(c.Evidence))
}

// --- triage ---------------------------------------------------------------

// candidate is one finding selected for exploitation, with the score and the
// MSF-shaped flag triage computed for it.
type candidate struct {
	Summary   string
	Evidence  string
	Agent     string
	score     int
	MSFShaped bool
}

// exploitKeywords mark a finding as something an exploit-dev can actually act
// on (as opposed to a bare banner or an informational note).
var exploitKeywords = []string{
	"inject", "sqli", "sql injection", "xss", "cross-site", "rce",
	"remote code", "command injection", "commix", "traversal", "lfi", "rfi",
	"ssrf", "idor", "access control", "auth bypass", "authentication",
	"default cred", "weak", "exposed", "disclosure", "misconfig",
	"cve-", "vulnerab", "deserial", "upload", "csrf", "open redirect",
}

// msfKeywords hint that a finding maps onto a Metasploit module (a versioned
// service CVE), so the optional exploiter stage is worth offering for it.
var msfKeywords = []string{"cve-", "exploit", "metasploit", "ms0", "ms1", "eternalblue", "smb", "rce", "remote code"}

// triage reads the shared findings store and selects, deterministically, which
// findings to hand to the exploit-dev — ranked, capped by
// ONIGAMI_MAX_EXPLOIT_TARGETS. It never depends on a model, so reaching the
// exploit-dev stage is guaranteed whenever anything exploitable was recorded.
func (p *Pipeline) triage() []candidate {
	max := config.IntEnv("ONIGAMI_MAX_EXPLOIT_TARGETS", 2)
	if max <= 0 {
		max = 1
	}

	var cands []candidate
	seen := map[string]bool{}
	for _, f := range p.cfg.Findings.List() {
		// HALLUCINATION GUARD: only tool-produced (verified) facts are ever handed
		// to the exploit-dev. An agent's unverified claim — however confident —
		// never becomes something we act on. This is the hard line that keeps
		// invented findings out of the exploitation path.
		if !f.Verified {
			continue
		}
		// Skip the pipeline's own PoC/demo records and anything with no substance.
		if f.Phase == "poc" || f.Phase == "demo" || strings.TrimSpace(f.Summary) == "" {
			continue
		}
		low := strings.ToLower(f.Summary + " " + f.Evidence)
		// Must actually be something exploitable — a CVE, a versioned service, or
		// an injectable endpoint — not a bare "GET / → 200" recon line.
		if !actionable(low) {
			continue
		}
		score := 1 // verified + actionable baseline
		if strings.TrimSpace(f.Evidence) != "" {
			score += 2
		}
		if versionRe.MatchString(low) {
			score++
		}
		for _, kw := range exploitKeywords {
			if strings.Contains(low, kw) {
				score++
			}
		}
		// Confirmed/exploitable language is a strong positive; "unconfirmed/not
		// tested" is a strong negative.
		if strings.Contains(low, "confirm") || strings.Contains(low, "exploitable") || strings.Contains(low, "vulnerable") {
			score += 2
		}
		if strings.Contains(low, "unconfirmed") || strings.Contains(low, "not tested") || strings.Contains(low, "out of scope") {
			score -= 3
		}
		if score <= 0 {
			continue
		}
		key := oneLine(strings.ToLower(f.Summary), 80)
		if seen[key] {
			continue
		}
		seen[key] = true

		msf := false
		for _, kw := range msfKeywords {
			if strings.Contains(low, kw) {
				msf = true
				break
			}
		}
		cands = append(cands, candidate{
			Summary: f.Summary, Evidence: f.Evidence, Agent: f.Agent, score: score, MSFShaped: msf,
		})
	}

	// Highest score first; stable so equal scores keep recording order.
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].score > cands[j].score })
	if len(cands) > max {
		cands = cands[:max]
	}
	return cands
}

// triageReport renders the triage decision for the final summary and the log.
func (p *Pipeline) triageReport(cands []candidate) string {
	if len(cands) == 0 {
		return fmt.Sprintf("Reviewed %d recorded finding(s); none were BOTH tool-verified AND actionable, so the engagement stops before the exploit-dev rather than hand it an unverified guess. (Unverified agent claims are never acted on.)", p.cfg.Findings.Count())
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Selected %d tool-verified finding(s) to hand to the exploit-dev (ranked by evidence + exploitability; unverified agent claims excluded):\n", len(cands))
	for i, c := range cands {
		fmt.Fprintf(&b, "  %d. [%s, score %d%s] %s\n", i+1, c.Agent, c.score, msfTag(c.MSFShaped), oneLine(c.Summary, 160))
	}
	return b.String()
}

func msfTag(b bool) string {
	if b {
		return ", msf-candidate"
	}
	return ""
}

// --- web-stage gating -----------------------------------------------------

// webSurfaceHints are tokens in the recorded findings that mean "there is a web
// application here worth actively testing".
var webSurfaceHints = []string{"http", "https", ":80", ":443", ":8080", ":8443", "web", "apache", "nginx", "iis", "html", "tomcat", "php", "login", "form", "endpoint", "url"}

// shouldWebTest decides whether STAGE 2 runs. ONIGAMI_WEB_STAGE = auto
// (default) | always | never. In auto mode it runs only when recon recorded
// something that looks like a web surface, so we don't spend budget web-testing
// a host with no web app.
func (p *Pipeline) shouldWebTest() bool {
	switch strings.ToLower(strings.TrimSpace(envOr("ONIGAMI_WEB_STAGE", "auto"))) {
	case "always":
		return true
	case "never":
		return false
	}
	for _, f := range p.cfg.Findings.List() {
		low := strings.ToLower(f.Summary + " " + f.Evidence)
		for _, h := range webSurfaceHints {
			if strings.Contains(low, h) {
				return true
			}
		}
	}
	return false
}

// --- small utilities ------------------------------------------------------

// findingsDigest renders up to n recorded findings compactly, for a worker
// brief (so the web-tester sees what recon found without re-discovering it).
func (p *Pipeline) findingsDigest(n int) string {
	items := p.cfg.Findings.List()
	if len(items) == 0 {
		return "(nothing recorded yet)"
	}
	if len(items) > n {
		items = items[len(items)-n:]
	}
	var b strings.Builder
	for _, f := range items {
		fmt.Fprintf(&b, "- [%s] %s\n", f.Agent, oneLine(f.Summary, 140))
	}
	return strings.TrimRight(b.String(), "\n")
}

// assemble joins the stage sections into the engagement's written summary.
func (p *Pipeline) assemble(sections []string) string {
	header := fmt.Sprintf("ENGAGEMENT (structured pipeline)\nMission: %s\nScope:   %s\n", p.mission, p.cfg.Scope)
	return header + "\n" + strings.Join(sections, "\n\n")
}

// oneLine collapses whitespace and clips to max runes with an ellipsis.
func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len([]rune(s)) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "…"
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(no separate evidence string; see the finding summary)"
	}
	return s
}

func envOr(name, def string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return def
}
