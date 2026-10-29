package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"onigami/internal/config"
	"onigami/internal/scope"
)

// WebSweep is to the web-tester what ReconSweep is to the analyst: it runs the
// MECHANICAL active-testing steps — nuclei (templated CVE/misconfig scan), ffuf
// (content discovery), and sqlmap (SQL-injection detection) — in Go, in a single
// call, and records every real result into the shared FindingsStore as VERIFIED.
//
// Why it exists: left to call sqlmap/nuclei/ffuf itself, a weak model very often
// NARRATES a fake result ("I ran sqlmap and confirmed SQLi…") without ever
// emitting the tool call — a hallucination that then poisons triage. By running
// the scanners deterministically in code, a "confirmed" web finding can only
// exist if a scanner actually produced it. The LLM is left to do what only it
// can: interpret the results and decide what's worth a PoC.
//
// Everything here is DETECTION only: nuclei matches templates, ffuf lists paths,
// sqlmap runs at --level/--risk 1 with no --dump/--os-shell. Going further is the
// gated exploiter's job.
type WebSweep struct {
	scope    *scope.Scope
	findings *FindingsStore
	http     *HTTPRequest
}

// NewWebSweep builds the sweep bound to the scope and the shared findings store.
func NewWebSweep(sc *scope.Scope, findings *FindingsStore) *WebSweep {
	return &WebSweep{scope: sc, findings: findings, http: NewHTTPRequest(sc)}
}

func (*WebSweep) Name() string { return "web_sweep" }
func (*WebSweep) Description() string {
	return "Run a full automated web-testing sweep of an in-scope target in ONE step: nuclei (templated CVE/misconfig scan), ffuf (path discovery), and sqlmap (SQL-injection detection) against the endpoints recon found. It records every real result to the shared notebook as a verified finding and returns a summary. Call this ONCE at the start of web testing; then interpret what it found and write your report."
}
func (*WebSweep) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"target":{"type":"string","description":"in-scope host, or a base URL like http://host"}},"required":["target"]}`)
}

// urlRe pulls an http(s) URL token out of a finding summary/evidence.
var urlRe = regexp.MustCompile(`https?://[^\s"'<>]+`)

// sqlmapHitRe spots sqlmap's positive verdicts in its output.
var sqlmapHitRe = regexp.MustCompile(`(?i)(is vulnerable|appears to be injectable|identified the following injection point|Parameter: .* \(.*\))`)

func (w *WebSweep) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Target string `json:"target"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("bad input: %w", err)
	}
	if err := checkTarget(w.scope, in.Target); err != nil {
		return "", err
	}

	var out strings.Builder
	record := func(phase, summary, evidence string) {
		w.findings.Add("web", phase, summary, evidence)
		fmt.Fprintf(&out, "  - %s\n", summary)
	}

	base, ok := w.baseURL(ctx, in.Target)
	if !ok {
		out.WriteString("no reachable HTTP(S) service on the target — nothing to web-test.\n")
		return truncate(out.String()), nil
	}
	fmt.Fprintf(&out, "BASE: %s\n", base)

	// --- PHASE: nuclei (known CVEs / misconfig / exposures) --------------
	out.WriteString("NUCLEI:\n")
	if installed("nuclei") {
		sev := config.StringEnv("ONIGAMI_WEB_NUCLEI_SEVERITY", "medium,high,critical")
		args := []string{"-u", base, "-silent", "-nc", "-duc", "-timeout", "10"}
		if severityRe.MatchString(sev) {
			args = append(args, "-severity", sev)
		}
		res, err := runCommand(ctx, 240*time.Second, "nuclei", args...)
		switch {
		case err != nil:
			fmt.Fprintf(&out, "  nuclei failed: %v\n", err)
		default:
			var hits int
			for _, line := range splitLines(res) {
				// Skip nuclei's operational/error log lines ([FTL]/[ERR]/[WRN]/[INF]/
				// [DBG]) — only a real template match is a finding. Recording an error
				// line here would plant a FALSE verified finding, defeating the whole
				// verified-vs-claim guarantee.
				if isNucleiLog(line) {
					if strings.Contains(strings.ToLower(line), "no templates") {
						out.WriteString("  nuclei has no templates installed — run `nuclei -update-templates` once, then re-run\n")
					} else {
						fmt.Fprintf(&out, "  (nuclei: %s)\n", clip(oneLine(line), 160))
					}
					continue
				}
				record("nuclei", "nuclei: "+clip(oneLine(line), 180), line)
				hits++
			}
			if hits == 0 {
				out.WriteString("  no templates matched\n")
			}
		}
	} else {
		out.WriteString("  (nuclei not installed — skipped)\n")
	}

	// --- PHASE: ffuf (content/path discovery) ----------------------------
	out.WriteString("FFUF:\n")
	if installed("ffuf") {
		wordlist, cleanup, err := wordlistPath()
		if err != nil {
			fmt.Fprintf(&out, "  ffuf wordlist error: %v\n", err)
		} else {
			defer cleanup()
			args := []string{"-u", strings.TrimRight(base, "/") + "/FUZZ", "-w", wordlist,
				"-mc", "200,204,301,302,307,401,403,405", "-s", "-t", "20"}
			res, ferr := runCommand(ctx, 120*time.Second, "ffuf", args...)
			switch {
			case ferr != nil:
				fmt.Fprintf(&out, "  ffuf failed: %v\n", ferr)
			case strings.TrimSpace(res) == "":
				out.WriteString("  no paths found\n")
			default:
				max := config.IntEnv("ONIGAMI_WEB_FFUF_MAX", 40)
				for i, line := range splitLines(res) {
					if i >= max {
						fmt.Fprintf(&out, "  … (%d more paths)\n", len(splitLines(res))-max)
						break
					}
					record("discover", "path: "+clip(oneLine(line), 160), "ffuf on "+base)
				}
			}
		}
	} else {
		out.WriteString("  (ffuf not installed — skipped)\n")
	}

	// --- PHASE: sqlmap (SQL injection on parameterized endpoints) --------
	out.WriteString("SQLMAP:\n")
	if installed("sqlmap") {
		candidates := w.sqlmapCandidates(base)
		if len(candidates) == 0 {
			out.WriteString("  no parameterized endpoint to test (recon found no query parameters)\n")
		}
		for _, u := range candidates {
			if err := checkTarget(w.scope, u); err != nil {
				continue // never test out-of-scope
			}
			args := []string{"-u", u, "--batch", "--level=1", "--risk=1",
				"--random-agent", "--disable-coloring", "--flush-session"}
			res, err := runCommand(ctx, 180*time.Second, "sqlmap", args...)
			if err != nil {
				fmt.Fprintf(&out, "  sqlmap error on %s: %v\n", u, err)
				continue
			}
			if sqlmapHitRe.MatchString(res) {
				record("inject", "SQL injection confirmed: "+u, sqlmapEvidence(res))
			} else {
				fmt.Fprintf(&out, "  not injectable: %s\n", u)
			}
		}
	} else {
		out.WriteString("  (sqlmap not installed — skipped)\n")
	}

	fmt.Fprintf(&out, "\nRecorded verified findings to the notebook (store now holds %d). Next: interpret these and write your report.", w.findings.Count())
	return truncate(out.String()), nil
}

// baseURL resolves a reachable base URL for the target. If the target already
// carries a scheme it's used directly; otherwise the standard web ports are
// probed and the first that answers is returned.
func (w *WebSweep) baseURL(ctx context.Context, target string) (string, bool) {
	var candidates []string
	if strings.Contains(target, "://") {
		candidates = []string{strings.TrimRight(target, "/")}
	} else {
		candidates = []string{"http://" + target, "https://" + target, "http://" + target + ":8080"}
	}
	for _, b := range candidates {
		if _, _, _, err := w.http.fetchGet(ctx, b+"/"); err == nil {
			return b, true
		}
	}
	return "", false
}

// sqlmapCandidates builds the list of parameterized URLs to hand sqlmap, grounded
// in what recon actually recorded: first any concrete URL that already carries a
// query string, then (if none) base+endpoint?param=1 combos from the recorded
// endpoints and parameters. Capped by ONIGAMI_WEB_SQLMAP_MAX (default 3) to stay
// inside the time/budget envelope.
func (w *WebSweep) sqlmapCandidates(base string) []string {
	max := config.IntEnv("ONIGAMI_WEB_SQLMAP_MAX", 3)
	if max <= 0 {
		max = 1
	}
	seen := map[string]bool{}
	var urls []string
	add := func(u string) {
		if u != "" && !seen[u] && len(urls) < max {
			seen[u] = true
			urls = append(urls, u)
		}
	}

	var endpoints, params []string
	for _, f := range w.findings.List() {
		text := f.Summary + " " + f.Evidence
		// (a) a concrete parameterized URL recon already saw.
		for _, u := range urlRe.FindAllString(text, -1) {
			if strings.Contains(u, "?") && strings.Contains(u, "=") {
				add(strings.TrimRight(u, ".,);"))
			}
		}
		if p, ok := strings.CutPrefix(strings.TrimSpace(f.Summary), "endpoint: "); ok {
			endpoints = append(endpoints, strings.TrimSpace(p))
		}
		if p, ok := strings.CutPrefix(strings.TrimSpace(f.Summary), "parameter: "); ok {
			params = append(params, strings.TrimSpace(p))
		}
	}
	// (b) construct base+endpoint?param=1 only if we found no concrete URL.
	if len(urls) == 0 && len(params) > 0 {
		b := strings.TrimRight(base, "/")
		for _, ep := range endpoints {
			if !strings.HasPrefix(ep, "/") {
				ep = "/" + ep
			}
			if strings.Contains(ep, "?") {
				add(b + ep)
				continue
			}
			add(fmt.Sprintf("%s%s?%s=1", b, ep, params[0]))
		}
		if len(urls) == 0 {
			// no endpoints at all — try the root with the first parameter.
			add(fmt.Sprintf("%s/?%s=1", b, params[0]))
		}
	}
	return urls
}

// sqlmapEvidence pulls the most informative lines out of sqlmap's output for the
// finding's evidence string.
func sqlmapEvidence(out string) string {
	var keep []string
	for _, line := range splitLines(out) {
		if sqlmapHitRe.MatchString(line) || strings.Contains(strings.ToLower(line), "type:") || strings.Contains(strings.ToLower(line), "payload:") {
			keep = append(keep, strings.TrimSpace(line))
		}
		if len(keep) >= 6 {
			break
		}
	}
	if len(keep) == 0 {
		return clip(oneLine(out), 200)
	}
	return clip(strings.Join(keep, " | "), 300)
}

// nucleiLogRe matches nuclei's operational/error log prefixes, which are NOT
// findings: [FTL] [ERR] [WRN] [INF] [DBG] [VER] [FTL] etc.
var nucleiLogRe = regexp.MustCompile(`^\[(FTL|ERR|WRN|INF|DBG|VER|FAT)\]`)

// isNucleiLog reports whether a nuclei output line is an operational/error log
// rather than a template match.
func isNucleiLog(line string) bool {
	return nucleiLogRe.MatchString(strings.TrimSpace(line))
}

// splitLines returns the non-empty, trimmed lines of s.
func splitLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			out = append(out, t)
		}
	}
	return out
}
