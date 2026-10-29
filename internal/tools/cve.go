package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// This file gives the Analyst two real research tools, backed by public APIs:
//
//   cve_search — "what CVEs affect <product>?"  (Red Hat security data)
//   cve_detail — "tell me everything about <CVE-id>"  (CIRCL + Red Hat)
//
// Neither touches the target host — they only query vulnerability databases —
// so they need no scope check. They're plain HTTP GETs with JSON parsing.

const (
	circlCVEURL  = "https://cve.circl.lu/api/cve/"                          // + CVE-ID
	redhatCVEURL = "https://access.redhat.com/hydra/rest/securitydata/cve/" // + CVE-ID.json
	redhatSearch = "https://access.redhat.com/hydra/rest/securitydata/cve.json"
)

var cveIDRe = regexp.MustCompile(`^CVE-\d{4}-\d{4,}$`)

// httpClient is shared by the CVE tools. 30s is plenty for a JSON API.
var httpClient = &http.Client{Timeout: 30 * time.Second}

// --- cve_search -----------------------------------------------------------

// CVESearch finds CVEs affecting a named product/package via Red Hat's data.
// It may hold the shared store; search results are exploratory, so it does not
// auto-record (cve_detail records the specific, confirmed record instead).
type CVESearch struct{ findings *FindingsStore }

// NewCVESearch / NewCVEDetail build the CVE tools bound to the shared findings
// store so cve_detail can record the real CVE record as a VERIFIED fact.
func NewCVESearch(f *FindingsStore) CVESearch { return CVESearch{findings: f} }
func NewCVEDetail(f *FindingsStore) CVEDetail { return CVEDetail{findings: f} }

func (CVESearch) Name() string { return "cve_search" }
func (CVESearch) Description() string {
	return "Search for known CVEs affecting a software package/product. Give the package name from recon (e.g. Apache HTTP Server → 'httpd', OpenSSH → 'openssh'). Returns matching CVEs with severity and CVSS score."
}
func (CVESearch) Schema() json.RawMessage {
	return json.RawMessage(`{
      "type":"object",
      "properties":{
        "product":{"type":"string","description":"package/product name to search, e.g. 'httpd', 'openssh', 'vsftpd'"}
      },
      "required":["product"]
    }`)
}
func (CVESearch) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Product string `json:"product"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("bad input: %w", err)
	}
	in.Product = strings.TrimSpace(in.Product)
	if in.Product == "" {
		return "", fmt.Errorf("product must not be empty")
	}

	u := fmt.Sprintf("%s?package=%s&per_page=25", redhatSearch, url.QueryEscape(in.Product))
	data, err := getJSON(ctx, u)
	if err != nil {
		return "", err
	}
	items := asSlice(data)
	if len(items) == 0 {
		return fmt.Sprintf("No CVEs found for package %q in Red Hat data. Try a different package name (e.g. the base daemon name).", in.Product), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "CVEs affecting %q (%d shown, most recent first):\n", in.Product, len(items))
	for _, it := range items {
		m := asMap(it)
		fmt.Fprintf(&b, "  %-18s sev=%-10s cvss=%-4s  %s\n",
			mstr(m, "CVE"),
			mstr(m, "severity"),
			firstNonEmpty(mstr(m, "cvss3_score"), mstr(m, "cvss_score"), "-"),
			truncateLine(mstr(m, "bugzilla_description"), 90),
		)
	}
	b.WriteString("\nUse cve_detail on any CVE above to get full details before acting on it.")
	return b.String(), nil
}

// --- cve_detail -----------------------------------------------------------

// CVEDetail fetches full detail for one CVE ID, merging CIRCL (description) and
// Red Hat (severity, CVSS, affected packages, remediation state).
type CVEDetail struct{ findings *FindingsStore }

func (CVEDetail) Name() string { return "cve_detail" }
func (CVEDetail) Description() string {
	return "Get full detail for a specific CVE by ID (e.g. CVE-2021-41773): description, severity, CVSS score, affected packages, and references. Sources: CIRCL and Red Hat."
}
func (CVEDetail) Schema() json.RawMessage {
	return json.RawMessage(`{
      "type":"object",
      "properties":{
        "cve":{"type":"string","description":"a CVE identifier, e.g. CVE-2021-41773"}
      },
      "required":["cve"]
    }`)
}
func (c CVEDetail) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		CVE string `json:"cve"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("bad input: %w", err)
	}
	id := strings.ToUpper(strings.TrimSpace(in.CVE))
	if !cveIDRe.MatchString(id) {
		return "", fmt.Errorf("%q is not a valid CVE id (expected like CVE-2021-41773)", in.CVE)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", id)

	// Capture the key fields so we can record a compact verified finding too.
	var desc, sev, cvss string

	// CIRCL: a clean English description from the official CVE record.
	if data, err := getJSON(ctx, circlCVEURL+id); err == nil {
		if desc = circlDescription(data); desc != "" {
			fmt.Fprintf(&b, "Description: %s\n", desc)
		}
	}

	// Red Hat: severity, CVSS, affected packages, remediation.
	if data, err := getJSON(ctx, redhatCVEURL+id+".json"); err == nil {
		m := asMap(data)
		if sev = mstr(m, "threat_severity"); sev != "" {
			fmt.Fprintf(&b, "Severity: %s\n", sev)
		}
		if cvss3 := asMap(m["cvss3"]); cvss3 != nil {
			cvss = strings.TrimSpace(mstr(cvss3, "cvss3_base_score") + " " + mstr(cvss3, "cvss3_scoring_vector"))
			fmt.Fprintf(&b, "CVSS3: %s\n", cvss)
		}
		if det := asSlice(m["details"]); len(det) > 0 {
			fmt.Fprintf(&b, "Details: %s\n", truncateLine(toStr(det[0]), 300))
		}
		if pkgs := asSlice(m["package_state"]); len(pkgs) > 0 {
			b.WriteString("Affected packages (Red Hat): ")
			var parts []string
			for _, p := range pkgs {
				pm := asMap(p)
				parts = append(parts, fmt.Sprintf("%s[%s]", mstr(pm, "package_name"), mstr(pm, "fix_state")))
			}
			b.WriteString(strings.Join(parts, ", ") + "\n")
		}
	}

	out := b.String()
	if out == id+"\n" {
		return "", fmt.Errorf("no data found for %s in CIRCL or Red Hat", id)
	}

	// Auto-record the real CVE record as a VERIFIED fact (it came straight from
	// the vuln DB, not the model). This is what gives triage a trustworthy,
	// exploitable finding to hand the exploit-dev — no hallucination involved.
	// NOTE: "verified" means the CVE record is genuine; whether the in-scope
	// target actually runs the affected version is the analyst's correlation.
	if c.findings != nil {
		summary := id
		if sev != "" {
			summary += " (" + sev + ")"
		}
		if desc != "" {
			summary += ": " + truncateLine(desc, 120)
		}
		c.findings.Add("cve", "correlate", summary, truncateLine(out, 400))
	}
	return out, nil
}

// --- small JSON helpers (forgiving parsing of two different API shapes) ----

// getJSON does a GET and decodes the body into a generic value.
func getJSON(ctx context.Context, u string) (any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "onigami/0.1")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // cap at 1MB
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %s", u, resp.Status)
	}
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, fmt.Errorf("decode failed: %w", err)
	}
	return v, nil
}

// circlDescription digs the English description out of a CVE-5.0 record:
// containers.cna.descriptions[].value (lang starting "en").
func circlDescription(data any) string {
	cna := asMap(asMap(asMap(data)["containers"])["cna"])
	for _, d := range asSlice(cna["descriptions"]) {
		dm := asMap(d)
		if strings.HasPrefix(strings.ToLower(mstr(dm, "lang")), "en") {
			return truncateLine(mstr(dm, "value"), 400)
		}
	}
	return ""
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}
func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}
func mstr(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	return toStr(m[key])
}
func toStr(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return fmt.Sprintf("%g", x)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", x)
	}
}
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
func truncateLine(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
