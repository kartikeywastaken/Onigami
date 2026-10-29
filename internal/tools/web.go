package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"onigami/internal/agent"
	"onigami/internal/scope"
)

// This file holds the web_tester worker's active OWASP-testing tools. They wrap
// real binaries (sqlmap, nuclei, ffuf, commix, testssl.sh) behind the same two
// safety rules as the scanners: no shell (args as a slice) and a hard scope
// check on every target. Every wrapper runs in DETECTION mode only — none of
// them dump data, drop a shell, or write to the target. Confirming a finding by
// going further is the (human-gated) exploiter's job, not this worker's.

// --- sqlmap: SQL injection ------------------------------------------------

// SQLMap tests a URL (and optional POST data / cookie) for SQL injection.
type SQLMap struct{ Scope *scope.Scope }

func (SQLMap) Name() string { return "sqlmap" }
func (SQLMap) Description() string {
	return "Test an in-scope URL for SQL injection with sqlmap (detection only — no data dump, no shell). Give a URL with a parameter you found (e.g. http://host/item.php?id=1); optionally POST data and a cookie. Chain this after http_request reveals a parameter."
}
func (SQLMap) Schema() json.RawMessage {
	return json.RawMessage(`{
      "type":"object",
      "properties":{
        "url":{"type":"string","description":"in-scope URL with a parameter to test, e.g. http://host/item.php?id=1"},
        "data":{"type":"string","description":"optional POST body (e.g. user=1&pass=2) — tests these params instead"},
        "cookie":{"type":"string","description":"optional Cookie header value for authenticated testing"}
      },
      "required":["url"]
    }`)
}
func (s SQLMap) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		URL    string `json:"url"`
		Data   string `json:"data"`
		Cookie string `json:"cookie"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("bad input: %w", err)
	}
	if err := checkTarget(s.Scope, in.URL); err != nil {
		return "", err
	}
	// --batch: never prompt. level/risk 1: least aggressive. No --dump/--os-*.
	args := []string{"-u", in.URL, "--batch", "--level=1", "--risk=1",
		"--random-agent", "--disable-coloring", "--flush-session"}
	if in.Data != "" {
		args = append(args, "--data", in.Data)
	}
	if in.Cookie != "" {
		args = append(args, "--cookie", in.Cookie)
	}
	return runCommand(ctx, 180*time.Second, "sqlmap", args...)
}

// --- nuclei: templated vulnerability scan ---------------------------------

// severityRe validates a nuclei severity filter like "medium,high,critical".
var severityRe = regexp.MustCompile(`^(info|low|medium|high|critical)(,(info|low|medium|high|critical))*$`)

// Nuclei runs projectdiscovery's community templates against a URL.
type Nuclei struct{ Scope *scope.Scope }

func (Nuclei) Name() string { return "nuclei" }
func (Nuclei) Description() string {
	return "Scan an in-scope URL with nuclei's community templates for known CVEs, misconfigurations, exposures, and default creds. Returns only matched findings. Optionally filter by severity."
}
func (Nuclei) Schema() json.RawMessage {
	return json.RawMessage(`{
      "type":"object",
      "properties":{
        "url":{"type":"string","description":"in-scope target URL, e.g. http://host/"},
        "severity":{"type":"string","description":"optional severity filter, comma list of: info,low,medium,high,critical"}
      },
      "required":["url"]
    }`)
}
func (n Nuclei) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		URL      string `json:"url"`
		Severity string `json:"severity"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("bad input: %w", err)
	}
	if err := checkTarget(n.Scope, in.URL); err != nil {
		return "", err
	}
	// -silent: findings only. -nc: no color. -duc: don't phone home for updates.
	args := []string{"-u", in.URL, "-silent", "-nc", "-duc", "-timeout", "10"}
	if in.Severity != "" {
		if !severityRe.MatchString(in.Severity) {
			return "", fmt.Errorf("invalid severity %q (use info,low,medium,high,critical)", in.Severity)
		}
		args = append(args, "-severity", in.Severity)
	}
	out, err := runCommand(ctx, 240*time.Second, "nuclei", args...)
	if err == nil && strings.TrimSpace(out) == "" {
		return "nuclei ran and matched no templates (no findings).", nil
	}
	return out, err
}

// --- ffuf: content discovery ----------------------------------------------

//go:embed wordlists/common.txt
var commonWordlist string

// Ffuf brute-forces paths/files using a built-in common-paths wordlist (or a
// bigger list via ONIGAMI_WORDLIST).
type Ffuf struct{ Scope *scope.Scope }

func (Ffuf) Name() string { return "ffuf" }
func (Ffuf) Description() string {
	return "Discover hidden paths/files on an in-scope web server with ffuf, using a built-in common-paths wordlist. The URL MUST contain the keyword FUZZ where the wordlist is substituted, e.g. http://host/FUZZ."
}
func (Ffuf) Schema() json.RawMessage {
	return json.RawMessage(`{
      "type":"object",
      "properties":{
        "url":{"type":"string","description":"in-scope URL containing the literal keyword FUZZ, e.g. http://host/FUZZ"}
      },
      "required":["url"]
    }`)
}
func (f Ffuf) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("bad input: %w", err)
	}
	if err := checkTarget(f.Scope, in.URL); err != nil {
		return "", err
	}
	if !strings.Contains(in.URL, "FUZZ") {
		return "", fmt.Errorf("url must contain the keyword FUZZ (e.g. http://host/FUZZ)")
	}
	wordlist, cleanup, err := wordlistPath()
	if err != nil {
		return "", err
	}
	defer cleanup()
	// -mc: treat these codes as hits. -s: silent (results only). -t: cap threads.
	args := []string{"-u", in.URL, "-w", wordlist, "-mc", "200,204,301,302,307,401,403,405", "-s", "-t", "20"}
	out, err := runCommand(ctx, 120*time.Second, "ffuf", args...)
	if err == nil && strings.TrimSpace(out) == "" {
		return "ffuf ran and found no paths from the common wordlist.", nil
	}
	return out, err
}

// wordlistPath returns a path to a wordlist file for ffuf. It prefers
// ONIGAMI_WORDLIST (e.g. a SecLists file) if set and readable; otherwise it
// materializes the embedded common list to a temp file. cleanup removes the
// temp file (and is a no-op for an external list).
func wordlistPath() (path string, cleanup func(), err error) {
	if ext := strings.TrimSpace(os.Getenv("ONIGAMI_WORDLIST")); ext != "" {
		if _, statErr := os.Stat(ext); statErr == nil {
			return ext, func() {}, nil
		}
	}
	tmp, err := os.CreateTemp("", "onigami-wordlist-*.txt")
	if err != nil {
		return "", func() {}, fmt.Errorf("wordlist temp: %w", err)
	}
	if _, err := tmp.WriteString(commonWordlist); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", func() {}, fmt.Errorf("wordlist write: %w", err)
	}
	tmp.Close()
	return tmp.Name(), func() { os.Remove(tmp.Name()) }, nil
}

// --- commix: OS command injection -----------------------------------------

// Commix tests a URL for OS command injection. It has no brew formula, so it's
// run as a Python script from a git checkout (see commixPath).
type Commix struct{ Scope *scope.Scope }

func (Commix) Name() string { return "commix" }
func (Commix) Description() string {
	return "Test an in-scope URL for OS command injection with commix (detection only — no shell). Give a URL with a parameter, optionally POST data and a cookie."
}
func (Commix) Schema() json.RawMessage {
	return json.RawMessage(`{
      "type":"object",
      "properties":{
        "url":{"type":"string","description":"in-scope URL with a parameter to test"},
        "data":{"type":"string","description":"optional POST body"},
        "cookie":{"type":"string","description":"optional Cookie header value"}
      },
      "required":["url"]
    }`)
}
func (c Commix) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		URL    string `json:"url"`
		Data   string `json:"data"`
		Cookie string `json:"cookie"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("bad input: %w", err)
	}
	if err := checkTarget(c.Scope, in.URL); err != nil {
		return "", err
	}
	script := commixPath()
	if script == "" {
		return "", fmt.Errorf("commix is not available")
	}
	args := []string{script, "-u", in.URL, "--batch", "--disable-coloring"}
	if in.Data != "" {
		args = append(args, "--data", in.Data)
	}
	if in.Cookie != "" {
		args = append(args, "--cookie", in.Cookie)
	}
	return runCommand(ctx, 180*time.Second, "python3", args...)
}

// commixPath locates commix.py: ONIGAMI_COMMIX if set, else the default git
// checkout under ~/.onigami-tools. Returns "" if not found.
func commixPath() string {
	if p := strings.TrimSpace(os.Getenv("ONIGAMI_COMMIX")); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".onigami-tools", "commix", "commix.py")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// --- testssl.sh: TLS/crypto configuration ---------------------------------

// TestSSL checks a host's TLS configuration for weak protocols/ciphers and
// known issues.
type TestSSL struct{ Scope *scope.Scope }

func (TestSSL) Name() string { return "testssl" }
func (TestSSL) Description() string {
	return "Check an in-scope HTTPS host's TLS/SSL configuration with testssl.sh: protocols, ciphers, certificate, and known issues (Heartbleed, weak ciphers, etc.)."
}
func (TestSSL) Schema() json.RawMessage {
	return json.RawMessage(`{
      "type":"object",
      "properties":{
        "target":{"type":"string","description":"in-scope host, host:port, or https URL"}
      },
      "required":["target"]
    }`)
}
func (t TestSSL) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Target string `json:"target"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("bad input: %w", err)
	}
	if err := checkTarget(t.Scope, in.Target); err != nil {
		return "", err
	}
	// --fast skips the slowest probes; --quiet/--color 0 keep output clean.
	args := []string{"--quiet", "--color", "0", "--fast", in.Target}
	return runCommand(ctx, 240*time.Second, "testssl.sh", args...)
}

// --- assembly -------------------------------------------------------------

// WebTesterTools returns the web_tester's belt: whichever active-testing tools
// are installed, plus the manual primitives (http_request, tcp_probe) it needs
// to craft payloads and confirm findings, plus CVE research. Everything is
// bound to the scope.
func WebTesterTools(sc *scope.Scope, findings *FindingsStore) []agent.Tool {
	var ts []agent.Tool
	if installed("sqlmap") {
		ts = append(ts, SQLMap{sc})
	}
	if installed("nuclei") {
		ts = append(ts, Nuclei{sc})
	}
	if installed("ffuf") {
		ts = append(ts, Ffuf{sc})
	}
	if commixPath() != "" {
		ts = append(ts, Commix{sc})
	}
	if installed("testssl.sh") {
		ts = append(ts, TestSSL{sc})
	}
	if installed("nikto") {
		ts = append(ts, Nikto{sc})
	}
	// Manual primitives + research + working memory, always available.
	ts = append(ts, NewHTTPRequest(sc), NewTCPProbe(sc), NewCVESearch(findings), NewCVEDetail(findings))
	ts = append(ts, NotebookTools(findings, "web-tester")...)
	return ts
}

// WebTesterStatus reports which active web-testing tools are present, for the
// startup banner.
func WebTesterStatus() string {
	var out string
	for _, name := range []string{"sqlmap", "nuclei", "ffuf", "testssl.sh", "nikto"} {
		mark := "✗"
		if installed(name) {
			mark = "✓"
		}
		out += fmt.Sprintf("%s %s  ", mark, name)
	}
	mark := "✗"
	if commixPath() != "" {
		mark = "✓"
	}
	out += fmt.Sprintf("%s commix  ", mark)
	return out
}
