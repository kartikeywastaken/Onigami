package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// FindingsStore is the engagement's shared, durable knowledge base. Unlike a
// per-worker notebook (which dies with the worker), ONE store is created per
// engagement and shared by every worker, and each finding is flushed to disk the
// moment it's recorded. That gives us two things the earlier runs lacked:
//   - cross-worker memory (the web_tester sees what the analyst recorded), and
//   - durability: a rate-limit 429 or a give-up mid-run no longer discards
//     everything learned — main can still write a report from what's on disk.
type FindingsStore struct {
	mu    sync.Mutex
	items []Finding
	path  string
}

// Finding is one recorded fact, tagged with which worker found it.
type Finding struct {
	Time     string `json:"time"`
	Agent    string `json:"agent"`
	Phase    string `json:"phase,omitempty"`
	Summary  string `json:"summary"`
	Evidence string `json:"evidence,omitempty"`
}

// NewFindingsStore opens a store at ONIGAMI_FINDINGS, or a timestamped file under
// ~/.onigami/ by default.
func NewFindingsStore() *FindingsStore {
	p := strings.TrimSpace(os.Getenv("ONIGAMI_FINDINGS"))
	if p == "" {
		p = "onigami-findings.jsonl"
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, ".onigami", fmt.Sprintf("findings-%s.jsonl", time.Now().Format("20060102-150405")))
		}
	}
	return &FindingsStore{path: p}
}

// Add records a finding in memory and appends it to the on-disk log (best
// effort — a failed disk write never blocks the engagement).
func (s *FindingsStore) Add(agent, phase, summary, evidence string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := Finding{
		Time:     time.Now().UTC().Format(time.RFC3339),
		Agent:    agent,
		Phase:    phase,
		Summary:  summary,
		Evidence: evidence,
	}
	s.items = append(s.items, f)
	if b, err := json.Marshal(f); err == nil {
		_ = os.MkdirAll(filepath.Dir(s.path), 0o755)
		if fh, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			_, _ = fh.Write(append(b, '\n'))
			_ = fh.Close()
		}
	}
	return len(s.items)
}

// List returns a copy of the recorded findings.
func (s *FindingsStore) List() []Finding {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Finding, len(s.items))
	copy(out, s.items)
	return out
}

// Count is how many findings have been recorded.
func (s *FindingsStore) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}

// Path is where findings are persisted (for the startup banner / final output).
func (s *FindingsStore) Path() string { return s.path }

// Report renders the findings as a readable summary — used both by the
// list_findings tool and as main's guaranteed final output when the LLM loop
// couldn't produce its own report.
func (s *FindingsStore) Report() string {
	items := s.List()
	if len(items) == 0 {
		return "(no findings were recorded during the engagement)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d finding(s) recorded during the engagement:\n", len(items))
	for i, f := range items {
		phase := f.Phase
		if phase == "" {
			phase = "-"
		}
		fmt.Fprintf(&b, "  %d. [%s/%s] %s", i+1, f.Agent, phase, f.Summary)
		if f.Evidence != "" {
			fmt.Fprintf(&b, "  (evidence: %s)", clip(f.Evidence, 200))
		}
		b.WriteString("\n")
	}
	return b.String()
}
