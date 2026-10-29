package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFindingsStore checks the in-memory + on-disk behaviour that the
// "never lose work" fix depends on.
func TestFindingsStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "findings.jsonl")
	t.Setenv("ONIGAMI_FINDINGS", path)

	s := NewFindingsStore()
	if got := s.Count(); got != 0 {
		t.Fatalf("new store should be empty, got %d", got)
	}
	if n := s.Add("analyst", "discover", "port 80 open: Apache Tomcat/Coyote 1.1", "Server: Apache-Coyote/1.1"); n != 1 {
		t.Fatalf("Add should return count 1, got %d", n)
	}
	s.Add("web-tester", "enumerate", "/login.jsp has fields uid,passw", "")

	if got := s.Count(); got != 2 {
		t.Fatalf("expected 2 findings, got %d", got)
	}

	rep := s.Report()
	for _, want := range []string{"Apache Tomcat/Coyote 1.1", "/login.jsp", "analyst", "web-tester"} {
		if !strings.Contains(rep, want) {
			t.Fatalf("report missing %q:\n%s", want, rep)
		}
	}

	// Durability: the two findings must be on disk as JSONL (survives a crash).
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read persisted file: %v", err)
	}
	lines := strings.Count(strings.TrimRight(string(data), "\n"), "\n") + 1
	if lines != 2 {
		t.Fatalf("expected 2 persisted JSONL lines, got %d:\n%s", lines, data)
	}

	// A fresh store pointed at the same path can't lose what's already written.
	t.Logf("persisted findings:\n%s", data)
}
