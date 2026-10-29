package tools

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"onigami/internal/scope"
)

func TestParseNmapPorts(t *testing.T) {
	sample := `Starting Nmap...
Nmap scan report for demo.testfire.net (65.61.137.117)
Host is up (0.22s latency).
80/tcp   open  http      Apache Tomcat/Coyote JSP engine 1.1
443/tcp  open  ssl/http  Apache Tomcat/Coyote JSP engine 1.1
22/tcp   open  ssh       OpenSSH 7.4
Service detection performed.`
	ports := parseNmapPorts(sample)
	if len(ports) != 3 {
		t.Fatalf("expected 3 ports, got %d: %+v", len(ports), ports)
	}
	if ports[0].Num != 80 || !strings.Contains(ports[0].Version, "Tomcat") {
		t.Fatalf("bad first port: %+v", ports[0])
	}
	if !isWebPort(ports[1]) { // 443 ssl/http
		t.Fatalf("443 should be a web port: %+v", ports[1])
	}
	if webBaseURL("h", ports[1]) != "https://h" {
		t.Fatalf("443 base url wrong: %s", webBaseURL("h", ports[1]))
	}
}

func TestExtractLinksParams(t *testing.T) {
	html := `<html><body>
	  <a href="/login.jsp">login</a>
	  <a href="http://demo.testfire.net/listproducts.php?cat=1">products</a>
	  <form action="/search.jsp"><input name="query" type="text"></form>
	  <a href="mailto:x@y.z">mail</a>
	</body></html>`
	links := extractLinks(html)
	if !contains(links, "/login.jsp") || !contains(links, "/search.jsp") || !contains(links, "/listproducts.php") {
		t.Fatalf("missing expected endpoints: %v", links)
	}
	if contains(links, "mailto:x@y.z") {
		t.Fatalf("mailto should be skipped: %v", links)
	}
	params := extractParams(html)
	if !contains(params, "query") || !contains(params, "cat") {
		t.Fatalf("missing expected params: %v", params)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// TestReconSweepLive runs the entire coded recon pipeline against a real target
// with NO LLM involved — proving Phase 2 works end to end for free. Skipped
// unless ONIGAMI_NET_IT=1 (it scans the network and takes ~a minute).
func TestReconSweepLive(t *testing.T) {
	if os.Getenv("ONIGAMI_NET_IT") == "" {
		t.Skip("set ONIGAMI_NET_IT=1 to run the live recon sweep")
	}
	sc, err := scope.Parse("demo.testfire.net")
	if err != nil {
		t.Fatalf("scope: %v", err)
	}
	store := NewFindingsStore()
	sweep := NewReconSweep(sc, store)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Second)
	defer cancel()

	out, err := sweep.Run(ctx, json.RawMessage(`{"target":"demo.testfire.net"}`))
	if err != nil {
		t.Fatalf("recon_sweep failed: %v", err)
	}
	t.Logf("recon_sweep output:\n%s", out)
	if store.Count() == 0 {
		t.Fatalf("expected findings to be recorded, got 0")
	}
	rep := store.Report()
	if !strings.Contains(rep, "port 80") && !strings.Contains(rep, "port 443") {
		t.Fatalf("expected a web port in findings:\n%s", rep)
	}
}
