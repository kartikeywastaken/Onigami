package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"time"

	"onigami/internal/agent"
	"onigami/internal/scope"
)

// defaultNmapPorts is a curated set of high-value ports spanning web, databases,
// remote access, mail, and common services. We scan these explicitly instead of
// --top-ports: measured against real firewalled hosts, an explicit list is both
// faster under -sV and more accurate (no false-positives, never misses 80/443).
const defaultNmapPorts = "21,22,23,25,53,80,110,111,135,139,143,389,443,445,587,993,995,1433,1521,1720,2049,3306,3389,5060,5432,5900,6379,8000,8080,8081,8443,8888,9000,9200,11211,27017"

// nmapPrivilege reports whether nmap can run a privileged SYN scan: directly if
// we're already root, or via sudo if main.go primed it (ONIGAMI_NMAP_PRIVILEGED)
// and the sudo credential is still valid. useSudo is true only in the latter
// case (we must prefix the command with sudo); privileged covers both.
func nmapPrivilege() (useSudo, privileged bool) {
	if os.Geteuid() == 0 {
		return false, true
	}
	if os.Getenv("ONIGAMI_NMAP_PRIVILEGED") == "1" {
		// Confirm the sudo timestamp is still warm; otherwise fall back quietly.
		if exec.Command("sudo", "-n", "true").Run() == nil {
			return true, true
		}
	}
	return false, false
}

// portsRe validates an nmap-style port spec: "-", or numbers/ranges/lists like
// "80", "1-1000", "22,80,443". This keeps the model from sneaking anything else
// into the -p argument.
var portsRe = regexp.MustCompile(`^(-|\d{1,5}(-\d{1,5})?(,\d{1,5}(-\d{1,5})?)*)$`)

// checkTarget runs the two guards every scanner shares: a format check (no flag
// injection) and the hard scope check. Returned errors go back to the model as
// the tool result, so it can see "out of scope" and adapt.
func checkTarget(sc *scope.Scope, target string) error {
	if err := validTarget(target); err != nil {
		return err
	}
	if !sc.Allows(target) {
		return fmt.Errorf("target %q is OUT OF SCOPE; refusing. In-scope: %s", target, sc.String())
	}
	return nil
}

// --- nmap -----------------------------------------------------------------

// Nmap runs an nmap service/version scan against an in-scope host.
type Nmap struct{ Scope *scope.Scope }

func (Nmap) Name() string { return "nmap" }
func (Nmap) Description() string {
	return "Scan an in-scope host with nmap for open ports, service names, and version banners. Run this first to learn what a target is running."
}
func (Nmap) Schema() json.RawMessage {
	return json.RawMessage(`{
      "type":"object",
      "properties":{
        "target":{"type":"string","description":"in-scope IP or hostname"},
        "ports":{"type":"string","description":"optional port spec: '22,80,443', '1-1000', or '-' for all ports. Omit for the top 1000."}
      },
      "required":["target"]
    }`)
}
func (n Nmap) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Target string `json:"target"`
		Ports  string `json:"ports"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("bad input: %w", err)
	}
	if err := checkTarget(n.Scope, in.Target); err != nil {
		return "", err
	}
	if in.Ports != "" && !portsRe.MatchString(in.Ports) {
		return "", fmt.Errorf("invalid ports spec %q", in.Ports)
	}
	return nmapScan(ctx, in.Target, in.Ports)
}

// nmapScan runs a version-detection scan of target (ports "" means the curated
// default list) and returns the raw output. Factored out so both the nmap tool
// and the coded recon sweep use identical, measured flags. The caller is
// responsible for the scope/format check on target.
//
// Flag choices come from measured behaviour against real, firewalled hosts:
//   - An explicit curated port list beats --top-ports: the latter both times out
//     under -sV and returns firewall false-positives while MISSING 80/443.
//   - -n (no DNS), --max-retries 2, --version-intensity 2 keep it fast.
//   - -Pn: skip host discovery (many hosts drop pings).
//   - --host-timeout bounds a stubborn host instead of hanging.
//   - SYN scan (-sS) when root/sudo-primed; else the (slower) connect scan.
func nmapScan(ctx context.Context, target, ports string) (string, error) {
	args := []string{"-Pn", "-n", "-T4", "--max-retries", "2", "--host-timeout", "150s"}
	useSudo, privileged := nmapPrivilege()
	if privileged {
		args = append(args, "-sS")
	}
	args = append(args, "-sV", "--version-intensity", "2")
	if ports != "" {
		args = append(args, "-p", ports)
	} else {
		args = append(args, "-p", defaultNmapPorts)
	}
	args = append(args, target)

	// sudo needs nmap's absolute path (its secure_path rarely includes
	// /opt/homebrew/bin) and the -n flag so it never blocks on a prompt.
	name, runArgs := "nmap", args
	if useSudo {
		if p, err := exec.LookPath("nmap"); err == nil {
			name = "sudo"
			runArgs = append([]string{"-n", p}, args...)
		}
	}
	return runCommand(ctx, 180*time.Second, name, runArgs...)
}

// --- nikto ----------------------------------------------------------------

// Nikto scans an in-scope web server for known issues.
type Nikto struct{ Scope *scope.Scope }

func (Nikto) Name() string { return "nikto" }
func (Nikto) Description() string {
	return "Scan an in-scope web server with nikto for known vulnerabilities, dangerous files, and misconfigurations. Use after nmap reveals an HTTP/HTTPS service."
}
func (Nikto) Schema() json.RawMessage {
	return json.RawMessage(`{
      "type":"object",
      "properties":{
        "target":{"type":"string","description":"in-scope host, IP, or URL of the web server"},
        "port":{"type":"integer","description":"optional port number if not 80/443"}
      },
      "required":["target"]
    }`)
}
func (n Nikto) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Target string `json:"target"`
		Port   int    `json:"port"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("bad input: %w", err)
	}
	if err := checkTarget(n.Scope, in.Target); err != nil {
		return "", err
	}

	// -ask no: never prompt for update checks. -maxtime: bound the run.
	args := []string{"-h", in.Target, "-ask", "no", "-maxtime", "120"}
	if in.Port > 0 {
		args = append(args, "-p", strconv.Itoa(in.Port))
	}
	return runCommand(ctx, 180*time.Second, "nikto", args...)
}

// --- rustscan -------------------------------------------------------------

// RustScan is a fast port scanner (only offered if installed).
type RustScan struct{ Scope *scope.Scope }

func (RustScan) Name() string { return "rustscan" }
func (RustScan) Description() string {
	return "Very fast port scan of an in-scope host with rustscan. Good for quickly finding open ports across the full range before a detailed nmap scan."
}
func (RustScan) Schema() json.RawMessage {
	return json.RawMessage(`{
      "type":"object",
      "properties":{
        "target":{"type":"string","description":"in-scope IP or hostname"}
      },
      "required":["target"]
    }`)
}
func (r RustScan) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Target string `json:"target"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("bad input: %w", err)
	}
	if err := checkTarget(r.Scope, in.Target); err != nil {
		return "", err
	}
	args := []string{"-a", in.Target, "--ulimit", "5000", "-g"}
	return runCommand(ctx, 180*time.Second, "rustscan", args...)
}

// --- assembly -------------------------------------------------------------

// AnalystTools returns the Analyst's real tool belt: whichever scanners are
// installed on this machine (bound to the scope), plus the two CVE research
// tools (which need no scope, since they only query databases).
func AnalystTools(sc *scope.Scope, findings *FindingsStore) []agent.Tool {
	var ts []agent.Tool
	// recon_sweep runs the whole mechanical recon in one coded call (Phase 2) —
	// the primary entry point; the individual scanners below remain for targeted
	// follow-up.
	if installed("nmap") {
		ts = append(ts, NewReconSweep(sc, findings))
	}
	if installed("rustscan") {
		ts = append(ts, RustScan{sc})
	}
	if installed("nmap") {
		ts = append(ts, Nmap{sc})
	}
	if installed("nikto") {
		ts = append(ts, Nikto{sc})
	}
	// http_request and tcp_probe need no external binary — they're always
	// available. http_request reads real pages (instead of imagining them);
	// tcp_probe grabs raw banners from non-HTTP ports (the safe nc stand-in).
	ts = append(ts, NewHTTPRequest(sc))
	ts = append(ts, NewTCPProbe(sc))
	ts = append(ts, CVESearch{}, CVEDetail{})
	ts = append(ts, NotebookTools(findings, "analyst")...) // shared engagement notebook
	return ts
}

// ScannerStatus reports which external scanners are present, for a startup note.
func ScannerStatus() string {
	var out string
	for _, name := range []string{"nmap", "rustscan", "nikto"} {
		mark := "✗"
		if installed(name) {
			mark = "✓"
		}
		out += fmt.Sprintf("%s %s  ", mark, name)
	}
	return out
}
