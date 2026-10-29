package tools

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"onigami/internal/agent"
	"onigami/internal/config"
	"onigami/internal/scope"
)

// This file is the exploiter's engine: a lifecycle manager + JSON-RPC client for
// msfrpcd (Metasploit's RPC daemon), and the tools the exploiter worker uses to
// search, inspect, and RUN modules. Running a module is real exploitation, so
// MSFRun is wrapped in three independent safety controls:
//   1. scope — RHOSTS must be in the authorized scope (enforced here, in code);
//   2. gate  — the operator must approve each run (module + options + target);
//   3. audit — every request/decision/result is written to an append-only log.

// --- the daemon manager ---------------------------------------------------

// MSFManager starts msfrpcd once (lazily, on first use), bound to localhost with
// a generated token, and speaks JSON-RPC to it. One manager is shared across all
// exploiter spawns so the (slow) daemon boots a single time per engagement.
type MSFManager struct {
	bin   string
	host  string
	port  int
	token string
	http  *http.Client

	mu      sync.Mutex
	started bool
	proc    *os.Process
}

// NewMSFManager builds a manager. It resolves the msfrpcd binary from
// ONIGAMI_MSFRPCD, then PATH, then the default install path. Available() reports
// whether it was found.
func NewMSFManager() *MSFManager {
	return &MSFManager{
		bin:   msfrpcdPath(),
		host:  "127.0.0.1", // never bind the exploitation RPC to a public iface
		port:  config.IntEnv("ONIGAMI_MSF_PORT", 55553),
		token: randomToken(),
		http:  &http.Client{Timeout: config.SecondsEnv("ONIGAMI_MSF_TIMEOUT", 300*time.Second)},
	}
}

func msfrpcdPath() string {
	if p := strings.TrimSpace(os.Getenv("ONIGAMI_MSFRPCD")); p != "" {
		if fileExists(p) {
			return p
		}
	}
	if p, err := exec.LookPath("msfrpcd"); err == nil {
		return p
	}
	if def := "/opt/metasploit-framework/bin/msfrpcd"; fileExists(def) {
		return def
	}
	return ""
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// randomToken makes a fresh ≥16-char token for MSF_WS_JSON_RPC_API_TOKEN.
func randomToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		// Extremely unlikely; fall back to a time-seeded value still ≥16 chars.
		return "onigami" + strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b)
}

// Available reports whether msfrpcd was found on this machine.
func (m *MSFManager) Available() bool { return m != nil && m.bin != "" }

func (m *MSFManager) endpoint() string {
	return fmt.Sprintf("http://%s:%d/api/v1/json-rpc", m.host, m.port)
}

// ensureStarted boots msfrpcd (JSON-RPC, no SSL, no DB, localhost) once and
// waits for the port to accept connections.
func (m *MSFManager) ensureStarted(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return nil
	}
	if m.bin == "" {
		return fmt.Errorf("msfrpcd not found; install Metasploit or set ONIGAMI_MSFRPCD")
	}

	// If the port is already serving (e.g. a stale daemon), we can't reuse it —
	// it has a different token — so refuse rather than fight over it.
	if portOpen(m.host, m.port) {
		return fmt.Errorf("port %d is already in use; stop any running msfrpcd first", m.port)
	}

	cmd := exec.Command(m.bin, "-j", "-S", "-a", m.host, "-p", strconv.Itoa(m.port), "-n")
	cmd.Env = append(os.Environ(), "MSF_WS_JSON_RPC_API_TOKEN="+m.token)
	// msfrpcd daemonizes (double-forks); this launcher process exits quickly and
	// the real thin server runs on. We record what we can and rely on Shutdown
	// to stop it via the pidfile.
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start msfrpcd: %w", err)
	}
	m.proc = cmd.Process
	_ = cmd.Wait() // the launcher returns almost immediately after daemonizing

	// Wait (bounded) for the JSON-RPC server to come up.
	deadline := time.Now().Add(config.SecondsEnv("ONIGAMI_MSF_BOOT_TIMEOUT", 90*time.Second))
	for {
		if portOpen(m.host, m.port) {
			m.started = true
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("msfrpcd did not start listening on %s:%d within the timeout", m.host, m.port)
		}
		select {
		case <-time.After(1 * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func portOpen(host string, port int) bool {
	c, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 2*time.Second)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// Shutdown stops the daemon we started (via its pidfile, since it daemonized),
// and the launcher process as a fallback. Safe to call when nothing was started.
func (m *MSFManager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.started {
		return
	}
	if home, err := os.UserHomeDir(); err == nil {
		if data, err := os.ReadFile(home + "/.msf4/msf-json-rpc.pid"); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
				if p, err := os.FindProcess(pid); err == nil {
					_ = p.Signal(os.Interrupt)
				}
			}
		}
	}
	if m.proc != nil {
		_ = m.proc.Kill()
	}
	m.started = false
}

// --- JSON-RPC call --------------------------------------------------------

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

// call sends one JSON-RPC request and returns the raw result (or an error, which
// may be transport-level or a Metasploit error object).
func (m *MSFManager) call(ctx context.Context, method string, params ...any) (json.RawMessage, error) {
	if err := m.ensureStarted(ctx); err != nil {
		return nil, err
	}
	if params == nil {
		params = []any{}
	}
	reqBody, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": method, "params": params,
	})
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, m.endpoint(), bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+m.token)

	resp, err := m.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("msf rpc call %s: %w", method, err)
	}
	defer resp.Body.Close()
	var r rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("msf rpc decode %s: %w", method, err)
	}
	if r.Error != nil {
		return nil, fmt.Errorf("msf %s error %d: %s", method, r.Error.Code, r.Error.Message)
	}
	return r.Result, nil
}

// splitModule turns "exploit/windows/smb/ms17_010_eternalblue" into
// ("exploit", "windows/smb/ms17_010_eternalblue").
func splitModule(full string) (mtype, name string, err error) {
	full = strings.TrimSpace(full)
	i := strings.IndexByte(full, '/')
	if i <= 0 || i == len(full)-1 {
		return "", "", fmt.Errorf("module %q must be a full name like exploit/windows/smb/ms17_010_eternalblue", full)
	}
	return full[:i], full[i+1:], nil
}

// --- tools ----------------------------------------------------------------

// MSFSearch — read-only: find modules by keyword.
type MSFSearch struct{ mgr *MSFManager }

func (MSFSearch) Name() string { return "msf_search" }
func (MSFSearch) Description() string {
	return "Search Metasploit modules by keyword (e.g. a CVE, product, or service). Read-only. Returns matching module full names, types, and ranks."
}
func (MSFSearch) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"search term, e.g. \"ms17_010\", \"vsftpd\", \"CVE-2017-0144\""}},"required":["query"]}`)
}
func (t MSFSearch) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("bad input: %w", err)
	}
	res, err := t.mgr.call(ctx, "module.search", in.Query)
	if err != nil {
		return "", err
	}
	var mods []struct {
		Type           string `json:"type"`
		Name           string `json:"name"`
		Fullname       string `json:"fullname"`
		Rank           string `json:"rank"`
		Disclosuredate string `json:"disclosuredate"`
	}
	if err := json.Unmarshal(res, &mods); err != nil {
		return truncate(string(res)), nil // fall back to raw if shape surprises us
	}
	if len(mods) == 0 {
		return fmt.Sprintf("No Metasploit modules matched %q.", in.Query), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d module(s) matching %q:\n", len(mods), in.Query)
	for i, m := range mods {
		if i >= 40 {
			fmt.Fprintf(&b, "… (%d more)\n", len(mods)-40)
			break
		}
		fmt.Fprintf(&b, "  [%s] %s  (rank=%s)\n", m.Type, m.Fullname, m.Rank)
	}
	return truncate(b.String()), nil
}

// MSFModuleInfo — read-only: a module's description + its datastore options (so
// the exploiter knows what to set before running).
type MSFModuleInfo struct{ mgr *MSFManager }

func (MSFModuleInfo) Name() string { return "msf_module_info" }
func (MSFModuleInfo) Description() string {
	return "Show a Metasploit module's details and its settable options (names, whether required, defaults). Read-only. Provide the full module name from msf_search."
}
func (MSFModuleInfo) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"module":{"type":"string","description":"full module name, e.g. exploit/windows/smb/ms17_010_eternalblue"}},"required":["module"]}`)
}
func (t MSFModuleInfo) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Module string `json:"module"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("bad input: %w", err)
	}
	mtype, name, err := splitModule(in.Module)
	if err != nil {
		return "", err
	}
	info, err := t.mgr.call(ctx, "module.info", mtype, name)
	if err != nil {
		return "", err
	}
	opts, err := t.mgr.call(ctx, "module.options", mtype, name)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "MODULE %s\nINFO:\n%s\nOPTIONS:\n%s\n", in.Module, indentJSON(info), indentJSON(opts))
	return truncate(b.String()), nil
}

// MSFRun — THE dangerous one: set options and execute (or check) a module. Scope
// check + human gate + audit, in that order, before anything runs.
type MSFRun struct {
	mgr     *MSFManager
	scope   *scope.Scope
	approve agent.Approver
	audit   *AuditLog
}

func (MSFRun) Name() string { return "msf_run" }
func (MSFRun) Description() string {
	return "Run a Metasploit module against an IN-SCOPE target. Set options via the options object (RHOSTS must be in scope). mode=\"check\" verifies exploitability without exploiting; mode=\"exploit\" (default) runs it for real. Every run requires operator approval and is written to the audit log."
}
func (MSFRun) Schema() json.RawMessage {
	return json.RawMessage(`{
      "type":"object",
      "properties":{
        "module":{"type":"string","description":"full module name, e.g. exploit/windows/smb/ms17_010_eternalblue"},
        "options":{"type":"object","description":"datastore options as a flat map, e.g. {\"RHOSTS\":\"10.0.0.5\",\"RPORT\":445,\"PAYLOAD\":\"windows/x64/meterpreter/reverse_tcp\",\"LHOST\":\"10.0.0.1\"}. RHOSTS is required and must be in scope.","additionalProperties":true},
        "mode":{"type":"string","enum":["check","exploit"],"description":"check = verify only; exploit = run for real (default)"}
      },
      "required":["module","options"]
    }`)
}
func (t MSFRun) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Module  string         `json:"module"`
		Options map[string]any `json:"options"`
		Mode    string         `json:"mode"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("bad input: %w", err)
	}
	mtype, name, err := splitModule(in.Module)
	if err != nil {
		return "", err
	}
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode == "" {
		mode = "exploit"
	}
	if mode != "exploit" && mode != "check" {
		return "", fmt.Errorf("mode must be \"check\" or \"exploit\", got %q", in.Mode)
	}

	// SAFETY 1 — scope. Every target host in RHOSTS/RHOST must be in scope.
	targets := extractHosts(in.Options)
	if len(targets) == 0 {
		return "", fmt.Errorf("refusing: no RHOSTS/RHOST set — a module must have an in-scope target")
	}
	for _, h := range targets {
		if !t.scope.Allows(h) {
			return "", fmt.Errorf("refusing: target %q is OUT OF SCOPE. In-scope: %s", h, t.scope.String())
		}
	}

	// SAFETY 3 (part 1) — audit the request BEFORE asking/running, so there is a
	// record even if the operator declines or the run errors. An unwritable audit
	// log is itself a reason to refuse.
	if err := t.audit.Record("msf.request", map[string]any{
		"module": in.Module, "mode": mode, "options": in.Options, "targets": targets,
	}); err != nil {
		return "", fmt.Errorf("refusing: could not write audit log (%v)", err)
	}

	// SAFETY 2 — human gate. The operator sees exactly what will run.
	desc := fmt.Sprintf("Metasploit %s\n  module : %s\n  mode   : %s\n  targets: %s\n  options: %s",
		strings.ToUpper(mode), in.Module, mode, strings.Join(targets, ", "), compactJSON(in.Options))
	if !t.approve.Ask(ctx, agent.RoleExploiter, desc) {
		_ = t.audit.Record("msf.held", map[string]any{"module": in.Module, "targets": targets})
		return "The operator reviewed this module run and chose to HOLD. It was NOT executed. Stop and report that exploitation was paused at the operator's request.", nil
	}

	method := "module.execute"
	if mode == "check" {
		method = "module.check"
	}
	res, runErr := t.mgr.call(ctx, method, mtype, name, in.Options)
	_ = t.audit.Record("msf.result", map[string]any{
		"module": in.Module, "mode": mode, "targets": targets,
		"ok": runErr == nil, "error": errString(runErr), "result": rawOrNil(res),
	})
	if runErr != nil {
		return "", runErr
	}
	return truncate(fmt.Sprintf("Executed %s (%s) against %s:\n%s",
		in.Module, mode, strings.Join(targets, ", "), indentJSON(res))), nil
}

// MSFSessions — read-only: list any sessions currently open (did a run land one?).
type MSFSessions struct{ mgr *MSFManager }

func (MSFSessions) Name() string { return "msf_sessions" }
func (MSFSessions) Description() string {
	return "List Metasploit sessions currently open (e.g. a shell/meterpreter from a successful exploit). Read-only."
}
func (MSFSessions) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (t MSFSessions) Run(ctx context.Context, _ json.RawMessage) (string, error) {
	res, err := t.mgr.call(ctx, "session.list")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(string(res)) == "{}" || strings.TrimSpace(string(res)) == "null" {
		return "No open sessions.", nil
	}
	return truncate("Open sessions:\n" + indentJSON(res)), nil
}

// --- assembly -------------------------------------------------------------

// ExploiterTools returns the exploiter worker's belt. MSFRun carries the scope,
// the approver (per-run gate), and the audit log. The read-only tools don't need
// the gate.
func ExploiterTools(mgr *MSFManager, sc *scope.Scope, approve agent.Approver, audit *AuditLog) []agent.Tool {
	return []agent.Tool{
		MSFSearch{mgr},
		MSFModuleInfo{mgr},
		MSFRun{mgr: mgr, scope: sc, approve: approve, audit: audit},
		MSFSessions{mgr},
		NewTCPProbe(sc), // to confirm an effect on the target
	}
}

// MSFGroundingTools returns ONLY the read-only Metasploit tools (module search
// and module info). They query the local msfrpcd for real module names,
// options, and affected-version data, but can never run or check a module — so
// they are safe to hand to the exploit-dev, whose PoCs should cite a REAL
// Metasploit module instead of an invented one. Actually firing a module stays
// with the gated, scope-checked exploiter (ExploiterTools / MSFRun).
func MSFGroundingTools(mgr *MSFManager) []agent.Tool {
	return []agent.Tool{
		MSFSearch{mgr},
		MSFModuleInfo{mgr},
	}
}

// MSFStatus reports whether Metasploit is wired up, for the startup banner.
func MSFStatus(mgr *MSFManager) string {
	if mgr.Available() {
		return "✓ msfrpcd (" + mgr.bin + ")"
	}
	return "✗ msfrpcd (not found; exploiter disabled)"
}

// --- small helpers --------------------------------------------------------

// extractHosts pulls target hosts out of RHOSTS/RHOST (space/comma separated).
func extractHosts(opts map[string]any) []string {
	var out []string
	for _, key := range []string{"RHOSTS", "RHOST", "rhosts", "rhost"} {
		if v, ok := opts[key]; ok {
			for _, f := range strings.Fields(strings.ReplaceAll(toStr(v), ",", " ")) {
				if f != "" {
					out = append(out, f)
				}
			}
		}
	}
	return out
}

func indentJSON(raw json.RawMessage) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "  ", "  "); err != nil {
		return string(raw)
	}
	return buf.String()
}

func compactJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func rawOrNil(r json.RawMessage) any {
	if len(r) == 0 {
		return nil
	}
	return json.RawMessage(r)
}
