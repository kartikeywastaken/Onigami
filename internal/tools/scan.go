package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"onigami/internal/agent"
	"onigami/internal/scope"
)

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

	// -sV: version detection, -T4: faster timing, -Pn: skip host discovery
	// (common for CTF boxes that drop pings).
	args := []string{"-sV", "-T4", "-Pn"}
	if in.Ports != "" {
		if !portsRe.MatchString(in.Ports) {
			return "", fmt.Errorf("invalid ports spec %q", in.Ports)
		}
		args = append(args, "-p", in.Ports)
	}
	args = append(args, in.Target)
	return runCommand(ctx, 240*time.Second, "nmap", args...)
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
func AnalystTools(sc *scope.Scope) []agent.Tool {
	var ts []agent.Tool
	if installed("rustscan") {
		ts = append(ts, RustScan{sc})
	}
	if installed("nmap") {
		ts = append(ts, Nmap{sc})
	}
	if installed("nikto") {
		ts = append(ts, Nikto{sc})
	}
	ts = append(ts, CVESearch{}, CVEDetail{})
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
