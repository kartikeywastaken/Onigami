package tools

import (
	"strings"
	"testing"

	"onigami/internal/scope"
)

func newTestWebSweep(t *testing.T) *WebSweep {
	t.Helper()
	t.Setenv("ONIGAMI_FINDINGS", t.TempDir()+"/findings.jsonl")
	sc, err := scope.Parse("demo.testfire.net")
	if err != nil {
		t.Fatalf("scope: %v", err)
	}
	return NewWebSweep(sc, NewFindingsStore())
}

// TestSqlmapCandidatesPrefersConcreteURL: when recon recorded a real
// parameterized URL, that exact URL is tested — not a constructed guess.
func TestSqlmapCandidatesPrefersConcreteURL(t *testing.T) {
	w := newTestWebSweep(t)
	w.findings.Add("recon", "enumerate", "endpoint: /listproducts.php", "")
	w.findings.Add("recon", "enumerate", "GET http://demo.testfire.net/listproducts.php?cat=1 -> 200", "")
	w.findings.Add("recon", "enumerate", "parameter: cat", "")

	got := w.sqlmapCandidates("http://demo.testfire.net")
	if len(got) == 0 {
		t.Fatal("expected at least one candidate URL")
	}
	if !strings.Contains(got[0], "listproducts.php?cat=1") {
		t.Errorf("expected the concrete recon URL first, got %v", got)
	}
}

// TestSqlmapCandidatesConstructsFromEndpointAndParam: with no concrete URL, it
// builds base+endpoint?param=1 from what recon recorded — grounded, not invented.
func TestSqlmapCandidatesConstructsFromEndpointAndParam(t *testing.T) {
	w := newTestWebSweep(t)
	w.findings.Add("recon", "enumerate", "endpoint: /search.jsp", "")
	w.findings.Add("recon", "enumerate", "parameter: query", "")

	got := w.sqlmapCandidates("http://demo.testfire.net")
	if len(got) != 1 {
		t.Fatalf("expected one constructed candidate, got %v", got)
	}
	if got[0] != "http://demo.testfire.net/search.jsp?query=1" {
		t.Errorf("unexpected constructed URL: %q", got[0])
	}
}

// TestSqlmapCandidatesCapped honours ONIGAMI_WEB_SQLMAP_MAX so a sprawling recon
// can't make the sweep test dozens of URLs.
func TestSqlmapCandidatesCapped(t *testing.T) {
	w := newTestWebSweep(t)
	t.Setenv("ONIGAMI_WEB_SQLMAP_MAX", "2")
	for _, u := range []string{"a", "b", "c", "d"} {
		w.findings.Add("recon", "enumerate", "GET http://demo.testfire.net/x.php?"+u+"=1 -> 200", "")
	}
	got := w.sqlmapCandidates("http://demo.testfire.net")
	if len(got) != 2 {
		t.Errorf("expected cap of 2 candidates, got %d: %v", len(got), got)
	}
}

// TestIsNucleiLog: nuclei's operational/error lines must be recognised as logs
// (so they are never recorded as verified findings), while a real template match
// is not.
func TestIsNucleiLog(t *testing.T) {
	logs := []string{
		"[FTL] Could not run nuclei: no templates provided for scan",
		"[ERR] could not load templates",
		"[INF] Templates loaded: 0",
		"[WRN] skipping",
	}
	for _, l := range logs {
		if !isNucleiLog(l) {
			t.Errorf("expected %q to be treated as a nuclei log line", l)
		}
	}
	hits := []string{
		"[tomcat-detect] [http] [info] http://demo.testfire.net",
		"[CVE-2021-41773] [http] [critical] http://host/cgi-bin/",
	}
	for _, l := range hits {
		if isNucleiLog(l) {
			t.Errorf("expected %q to be treated as a real finding, not a log line", l)
		}
	}
}

// TestSqlmapCandidatesNoneWithoutParams: no parameters recorded → nothing to test
// (the sweep must not invent a parameter).
func TestSqlmapCandidatesNoneWithoutParams(t *testing.T) {
	w := newTestWebSweep(t)
	w.findings.Add("recon", "discover", "port 80/tcp open: http Apache", "")
	if got := w.sqlmapCandidates("http://demo.testfire.net"); len(got) != 0 {
		t.Errorf("expected no candidates without any parameter, got %v", got)
	}
}
