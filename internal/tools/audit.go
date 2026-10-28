package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// AuditLog is an append-only, one-JSON-object-per-line record of everything the
// exploiter does. Because the exploiter fires real Metasploit modules at live
// targets, we write an immutable trail BEFORE and AFTER each action: what was
// requested, whether the operator approved it, and what happened. This is the
// accountability half of the exploiter's safety model (the other halves are the
// scope check on RHOSTS and the per-run human gate).
type AuditLog struct {
	mu   sync.Mutex
	path string
}

// NewAuditLog opens the audit log at ONIGAMI_AUDIT_LOG, or ~/.onigami/audit.log
// by default. The file is created lazily on first write.
func NewAuditLog() *AuditLog {
	p := strings.TrimSpace(os.Getenv("ONIGAMI_AUDIT_LOG"))
	if p == "" {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, ".onigami", "audit.log")
		} else {
			p = "onigami-audit.log"
		}
	}
	return &AuditLog{path: p}
}

// Record appends one event with a UTC timestamp and the given fields. It never
// panics and never blocks the caller on an error — a failed audit write is
// logged into the record's own best effort, not fatal to the engagement, but
// the exploiter treats an unwritable log as a reason to refuse (see MSFRun).
func (a *AuditLog) Record(event string, fields map[string]any) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	rec := map[string]any{
		"time":  time.Now().UTC().Format(time.RFC3339),
		"event": event,
	}
	for k, v := range fields {
		rec[k] = v
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(a.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

// Path returns where the audit log is written (for the startup banner).
func (a *AuditLog) Path() string { return a.path }
