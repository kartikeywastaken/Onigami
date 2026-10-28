package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"onigami/internal/scope"
)

// HTTPRequest lets an agent send a real HTTP request to an in-scope host and
// see the ACTUAL response — status, key headers, body, and timing.
//
// This is the direct antidote to the hallucination we saw: instead of imagining
// what a page returns, the agent fetches it and reasons over real bytes. The
// tool keeps a cookie jar, so logging in on one call authenticates later calls
// within the SAME worker — the foundation for access-control / IDOR testing.
//
// Because each worker is built fresh per dispatch (see orchestrator), each
// worker also gets its own jar: sessions never leak between workers.
type HTTPRequest struct {
	scope  *scope.Scope
	client *http.Client
}

// NewHTTPRequest builds the tool with its own cookie jar and a redirect policy
// that re-checks scope on every hop (so a redirect can't walk us off-target).
func NewHTTPRequest(sc *scope.Scope) *HTTPRequest {
	jar, _ := cookiejar.New(nil)
	return &HTTPRequest{
		scope: sc,
		client: &http.Client{
			Jar:     jar,
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("stopped after 10 redirects")
				}
				if !sc.Allows(req.URL.Hostname()) {
					return fmt.Errorf("redirect to out-of-scope host %q blocked", req.URL.Hostname())
				}
				return nil
			},
		},
	}
}

func (*HTTPRequest) Name() string { return "http_request" }
func (*HTTPRequest) Description() string {
	return "Send an HTTP request to an in-scope URL and get the REAL response (status, headers, body, timing). Use this to read pages, follow links, and test inputs — base every finding on what it actually returns, never on assumption. Cookies persist across calls, so you can log in and then act as that session."
}
func (*HTTPRequest) Schema() json.RawMessage {
	return json.RawMessage(`{
      "type":"object",
      "properties":{
        "method":{"type":"string","description":"HTTP method (GET, POST, PUT, DELETE, …). Default GET."},
        "url":{"type":"string","description":"full URL on an in-scope host, e.g. https://target/path?x=1"},
        "headers":{"type":"object","description":"optional request headers as a flat string->string map","additionalProperties":{"type":"string"}},
        "body":{"type":"string","description":"optional request body for POST/PUT (set a matching Content-Type header)"}
      },
      "required":["url"]
    }`)
}

func (t *HTTPRequest) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Method  string            `json:"method"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("bad input: %w", err)
	}

	// Validate and scope-check the URL before touching the network.
	u, err := url.Parse(strings.TrimSpace(in.URL))
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("invalid url: %q", in.URL)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("url scheme must be http or https, got %q", u.Scheme)
	}
	if !t.scope.Allows(u.Hostname()) {
		return "", fmt.Errorf("host %q is OUT OF SCOPE; refusing. In-scope: %s", u.Hostname(), t.scope.String())
	}

	method := strings.ToUpper(strings.TrimSpace(in.Method))
	if method == "" {
		method = http.MethodGet
	}

	var body io.Reader
	if in.Body != "" {
		body = strings.NewReader(in.Body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "onigami/0.1")
	for k, v := range in.Headers {
		req.Header.Set(k, v)
	}

	start := time.Now()
	resp, err := t.client.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024)) // read at most 64KB

	// Build a compact, structured, truthful result for the model to reason over.
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", method, u.String())
	fmt.Fprintf(&b, "→ %s  (%d ms)\n", resp.Status, elapsed.Milliseconds())
	b.WriteString("Headers:\n")
	for _, h := range reportedHeaders {
		if v := resp.Header.Values(h); len(v) > 0 {
			fmt.Fprintf(&b, "  %s: %s\n", h, strings.Join(v, "; "))
		}
	}
	b.WriteString("Body:\n")
	b.Write(raw)
	return truncate(b.String()), nil
}

// reportedHeaders is the curated set we surface — the security-relevant and
// fingerprinting ones — to keep token use (and the TPM budget) in check.
var reportedHeaders = []string{
	"Server", "Content-Type", "Content-Length", "Location", "Set-Cookie",
	"X-Powered-By", "WWW-Authenticate",
	"Strict-Transport-Security", "Content-Security-Policy",
	"X-Frame-Options", "X-Content-Type-Options", "Referrer-Policy",
}
