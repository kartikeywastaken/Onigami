package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"onigami/internal/scope"
)

// ReconSweep is the Phase-2 coded recon scaffold: it runs the whole MECHANICAL
// reconnaissance — port/service/version discovery, HTTP fingerprinting, and
// endpoint/parameter enumeration — in Go, in a single call, and records every
// result into the shared FindingsStore.
//
// Why it exists: doing those steps through the LLM cost ~10 turns of think→call→
// observe each, which exhausted turn budgets and hammered the rate limit. Here
// the deterministic work is just code; the LLM is left to do what only it can —
// correlate findings with CVEs and judge what's worth deeper testing.
type ReconSweep struct {
	scope    *scope.Scope
	findings *FindingsStore
	http     *HTTPRequest
}

// NewReconSweep builds the sweep bound to the scope and the shared findings store.
func NewReconSweep(sc *scope.Scope, findings *FindingsStore) *ReconSweep {
	return &ReconSweep{scope: sc, findings: findings, http: NewHTTPRequest(sc)}
}

func (*ReconSweep) Name() string { return "recon_sweep" }
func (*ReconSweep) Description() string {
	return "Run a full automated reconnaissance sweep of an in-scope target in ONE step: nmap port/service/version discovery, HTTP fingerprinting, and endpoint/parameter enumeration. It records everything to the shared notebook and returns a summary. Call this ONCE at the start of recon; then use cve_search/cve_detail on what it found and write your report."
}
func (*ReconSweep) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"target":{"type":"string","description":"in-scope IP or hostname"}},"required":["target"]}`)
}

func (r *ReconSweep) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Target string `json:"target"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("bad input: %w", err)
	}
	if err := checkTarget(r.scope, in.Target); err != nil {
		return "", err
	}

	var out strings.Builder
	record := func(phase, summary, evidence string) {
		r.findings.Add("recon", phase, summary, evidence)
		fmt.Fprintf(&out, "  - %s\n", summary)
	}

	// --- PHASE: discover (nmap) ------------------------------------------
	out.WriteString("DISCOVER (nmap):\n")
	scan, err := nmapScan(ctx, in.Target, "")
	if err != nil {
		fmt.Fprintf(&out, "  nmap failed: %v\n", err)
	}
	ports := parseNmapPorts(scan)
	if len(ports) == 0 {
		out.WriteString("  no open ports parsed (host may be filtered or nmap timed out)\n")
	}
	for _, p := range ports {
		svc := strings.TrimSpace(p.Service + " " + p.Version)
		record("discover", fmt.Sprintf("port %d/tcp open: %s", p.Num, svc), p.Raw)
	}

	// --- PHASE: enumerate ------------------------------------------------
	out.WriteString("ENUMERATE:\n")

	// Collect base URLs to enumerate from TWO sources, deduped:
	//  (a) web ports nmap reported, and
	//  (b) direct probes of the standard web ports — because this host's firewall
	//      makes nmap miss 80/443 intermittently, a real HTTP response is the
	//      reliable proof of a web service.
	bases := map[string]bool{}
	var baseList []string
	addBase := func(b string) {
		if !bases[b] {
			bases[b] = true
			baseList = append(baseList, b)
		}
	}
	for _, wp := range webPortsFrom(ports) {
		addBase(webBaseURL(in.Target, wp))
	}
	addBase(fmt.Sprintf("http://%s", in.Target))
	addBase(fmt.Sprintf("https://%s", in.Target))
	addBase(fmt.Sprintf("http://%s:8080", in.Target))

	seenEndpoint := map[string]bool{}
	seenParam := map[string]bool{}
	liveWeb := 0
	for _, base := range baseList {
		for _, path := range []string{"/", "/robots.txt"} {
			status, hdr, body, err := r.http.fetchGet(ctx, base+path)
			if err != nil {
				if path == "/" {
					break // base not reachable — don't bother with robots.txt
				}
				continue
			}
			if path == "/" {
				liveWeb++
			}
			record("enumerate", fmt.Sprintf("GET %s%s -> %d (Server: %s)", base, path, status, orDash(hdr.Get("Server"))), "")
			// Pull endpoints and parameters out of the body.
			for _, link := range extractLinks(body) {
				if seenEndpoint[link] || len(seenEndpoint) >= 40 {
					continue
				}
				seenEndpoint[link] = true
				record("enumerate", fmt.Sprintf("endpoint: %s", link), "found on "+base+path)
			}
			for _, name := range extractParams(body) {
				if seenParam[name] || len(seenParam) >= 40 {
					continue
				}
				seenParam[name] = true
				record("enumerate", fmt.Sprintf("parameter: %s", name), "input/query on "+base+path)
			}
		}
	}
	if liveWeb == 0 {
		out.WriteString("  no reachable HTTP(S) service found\n")
	}

	// --- PHASE: banners (non-web open ports) -----------------------------
	for _, p := range ports {
		if isWebPort(p) {
			continue
		}
		if b := grabBanner(ctx, in.Target, p.Num); b != "" {
			record("enumerate", fmt.Sprintf("port %d banner: %s", p.Num, clip(oneLine(b), 120)), b)
		}
	}

	fmt.Fprintf(&out, "\nRecorded %d finding(s) to the notebook. Next: cve_search/cve_detail the services above, then report.", r.findings.Count())
	return truncate(out.String()), nil
}

// --- parsing helpers ------------------------------------------------------

type portInfo struct {
	Num     int
	Service string
	Version string
	Raw     string
}

// nmapPortRe matches a line like "80/tcp open http Apache Tomcat/Coyote 1.1".
// It is strictly single-line: intra-line gaps are [ \t]+ and the version is
// [^\r\n]* so a greedy \s+ can't jump the capture onto a following line.
var nmapPortRe = regexp.MustCompile(`(?m)^(\d{1,5})/tcp[ \t]+open[ \t]+(\S+)(?:[ \t]+([^\r\n]*?))?[ \t]*\r?$`)

func parseNmapPorts(output string) []portInfo {
	var ports []portInfo
	for _, m := range nmapPortRe.FindAllStringSubmatch(output, -1) {
		num, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		ports = append(ports, portInfo{Num: num, Service: m[2], Version: strings.TrimSpace(m[3]), Raw: strings.TrimSpace(m[0])})
	}
	return ports
}

// linkRe pulls href/src/action targets; inputRe pulls form field names.
var (
	linkRe  = regexp.MustCompile(`(?i)(?:href|src|action)\s*=\s*["']([^"'>\s]+)["']`)
	inputRe = regexp.MustCompile(`(?i)<(?:input|select|textarea)\b[^>]*\bname\s*=\s*["']([^"']+)["']`)
)

// extractLinks returns distinct same-site paths (and the query keys they carry).
func extractLinks(body string) []string {
	set := map[string]bool{}
	var out []string
	for _, m := range linkRe.FindAllStringSubmatch(body, -1) {
		link := strings.TrimSpace(m[1])
		if link == "" || strings.HasPrefix(link, "mailto:") || strings.HasPrefix(link, "javascript:") || strings.HasPrefix(link, "data:") {
			continue
		}
		// Keep only paths/relative links and the path of absolute same-looking URLs.
		if i := strings.Index(link, "://"); i >= 0 {
			if s := strings.IndexByte(link[i+3:], '/'); s >= 0 {
				link = link[i+3+s:]
			} else {
				continue
			}
		}
		if !strings.HasPrefix(link, "/") {
			link = "/" + link
		}
		// Record the path as the endpoint; query/fragment are tracked as params.
		if i := strings.IndexAny(link, "?#"); i >= 0 {
			link = link[:i]
		}
		if link == "" {
			continue
		}
		if !set[link] {
			set[link] = true
			out = append(out, link)
		}
	}
	sort.Strings(out)
	return out
}

// extractParams returns distinct form field names and query-string keys.
func extractParams(body string) []string {
	set := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s != "" && !set[s] {
			set[s] = true
			out = append(out, s)
		}
	}
	for _, m := range inputRe.FindAllStringSubmatch(body, -1) {
		add(m[1])
	}
	// Query keys from any links in the body (e.g. listproducts.php?cat=1).
	for _, link := range linkRe.FindAllStringSubmatch(body, -1) {
		if q := strings.IndexByte(link[1], '?'); q >= 0 {
			for _, pair := range strings.Split(link[1][q+1:], "&") {
				if k, _, ok := strings.Cut(pair, "="); ok {
					add(k)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// --- small helpers --------------------------------------------------------

var webPortSet = map[int]bool{80: true, 443: true, 8000: true, 8008: true, 8080: true, 8081: true, 8443: true, 8888: true, 9000: true}

func isWebPort(p portInfo) bool {
	return webPortSet[p.Num] || strings.Contains(strings.ToLower(p.Service), "http")
}

func webPortsFrom(ports []portInfo) []portInfo {
	var out []portInfo
	for _, p := range ports {
		if isWebPort(p) {
			out = append(out, p)
		}
	}
	return out
}

// webBaseURL builds the base URL for a web port, choosing https for TLS ports and
// omitting the port when it's the scheme default.
func webBaseURL(host string, p portInfo) string {
	https := p.Num == 443 || p.Num == 8443 || strings.Contains(strings.ToLower(p.Service), "ssl") || strings.Contains(strings.ToLower(p.Service), "https")
	scheme := "http"
	def := 80
	if https {
		scheme, def = "https", 443
	}
	if p.Num == def {
		return fmt.Sprintf("%s://%s", scheme, host)
	}
	return fmt.Sprintf("%s://%s:%d", scheme, host, p.Num)
}

// grabBanner opens a short TCP connection and reads whatever the service greets
// with (for non-HTTP ports).
func grabBanner(ctx context.Context, host string, port int) string {
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return ""
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(4 * time.Second))
	buf := make([]byte, 512)
	n, _ := conn.Read(buf)
	return strings.TrimSpace(string(buf[:n]))
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
