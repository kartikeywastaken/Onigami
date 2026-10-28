// Package scope defines what targets onigami is authorized to act against.
//
// This is the HARD enforcement layer we talked about: scope is set once from
// the user's input and every scan tool checks a target against it before doing
// anything. Crucially, enforcement lives HERE, in code — not in a system
// prompt. An agent can be told the scope (so it plans sensibly), but it can
// never talk its way past this check, because the tools consult Scope directly.
package scope

import (
	"fmt"
	"net"
	"strings"
)

// Scope is a set of allowed targets. Each entry the user gives is one of:
//   - a CIDR range   e.g. 10.10.10.0/24
//   - a single IP    e.g. 10.10.10.5
//   - a hostname     e.g. target.htb
type Scope struct {
	raw   []string     // original entries, for display
	nets  []*net.IPNet // CIDR ranges
	ips   []net.IP     // single IPs
	hosts []string     // hostnames, lowercased
}

// Parse builds a Scope from a line the user types. Entries may be separated by
// spaces, commas, or both. Returns an error if nothing usable is found.
func Parse(input string) (*Scope, error) {
	fields := strings.FieldsFunc(input, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})

	s := &Scope{}
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		s.raw = append(s.raw, f)

		// Is it a CIDR? (ParseCIDR accepts "10.0.0.0/24")
		if _, ipnet, err := net.ParseCIDR(f); err == nil {
			s.nets = append(s.nets, ipnet)
			continue
		}
		// Is it a bare IP?
		if ip := net.ParseIP(f); ip != nil {
			s.ips = append(s.ips, ip)
			continue
		}
		// Otherwise treat it as a hostname.
		s.hosts = append(s.hosts, strings.ToLower(f))
	}

	if len(s.raw) == 0 {
		return nil, fmt.Errorf("no valid scope entries found in %q", input)
	}
	return s, nil
}

// Allows reports whether target is in scope. target may be a bare IP, a
// hostname, or a URL (we extract the host from it either way).
func (s *Scope) Allows(target string) bool {
	host := hostOnly(target)
	if host == "" {
		return false
	}

	// Hostname match (case-insensitive).
	for _, h := range s.hosts {
		if h == host {
			return true
		}
	}

	// IP match: exact IP, or inside an allowed CIDR.
	if ip := net.ParseIP(host); ip != nil {
		for _, allowed := range s.ips {
			if allowed.Equal(ip) {
				return true
			}
		}
		for _, n := range s.nets {
			if n.Contains(ip) {
				return true
			}
		}
	}
	return false
}

// String renders the scope for display/logging.
func (s *Scope) String() string { return strings.Join(s.raw, ", ") }

// hostOnly reduces a target to just its host part: it strips a URL scheme
// (http://), any path, and a trailing :port. "http://10.0.0.5:8080/x" → "10.0.0.5".
func hostOnly(target string) string {
	t := strings.TrimSpace(strings.ToLower(target))
	if t == "" {
		return ""
	}
	// Strip scheme.
	if i := strings.Index(t, "://"); i >= 0 {
		t = t[i+3:]
	}
	// Strip path.
	if i := strings.IndexByte(t, '/'); i >= 0 {
		t = t[:i]
	}
	// Strip :port — but leave bracketed IPv6 ([::1]) alone for simplicity.
	if !strings.Contains(t, "]") {
		if i := strings.LastIndexByte(t, ':'); i >= 0 {
			t = t[:i]
		}
	}
	return t
}
