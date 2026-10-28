package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"onigami/internal/config"
	"onigami/internal/scope"
)

// TCPProbe opens a raw TCP connection to an in-scope host:port, optionally
// sends a payload, and reads back whatever the service replies — a banner, a
// greeting, an error. It is the safe, pure-Go stand-in for the one thing a human
// analyst reaches for netcat to do: poke a port and read the raw bytes.
//
// We deliberately do NOT shell out to nc/ncat. Those binaries can execute
// commands (-e/--sh-exec), open listeners (-l), and transfer files, and BSD nc
// and ncat differ — far too much power to hand an LLM. This tool exposes exactly
// "connect, optionally send these bytes, read the reply" and nothing else, and
// it can only talk to in-scope hosts.
type TCPProbe struct{ scope *scope.Scope }

// NewTCPProbe builds the probe bound to the engagement scope.
func NewTCPProbe(sc *scope.Scope) *TCPProbe { return &TCPProbe{scope: sc} }

func (*TCPProbe) Name() string { return "tcp_probe" }
func (*TCPProbe) Description() string {
	return "Open a raw TCP connection to an in-scope host:port, optionally send a payload, and read the service's reply (banner/greeting). Fast way to grab a banner (SSH, SMTP, FTP, Redis, etc.) or poke a non-HTTP port. Use findings from nmap (an open port) as input here, and feed banners you learn into cve_search. For HTTP, prefer http_request."
}
func (*TCPProbe) Schema() json.RawMessage {
	return json.RawMessage(`{
      "type":"object",
      "properties":{
        "host":{"type":"string","description":"in-scope IP or hostname"},
        "port":{"type":"integer","description":"TCP port to connect to (1-65535)"},
        "payload":{"type":"string","description":"optional bytes to send after connecting (e.g. \"HEAD / HTTP/1.0\\r\\n\\r\\n\"). Omit to just read a banner the server sends on connect."}
      },
      "required":["host","port"]
    }`)
}

func (t *TCPProbe) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Host    string `json:"host"`
		Port    int    `json:"port"`
		Payload string `json:"payload"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("bad input: %w", err)
	}
	if err := validTarget(in.Host); err != nil {
		return "", err
	}
	if !t.scope.Allows(in.Host) {
		return "", fmt.Errorf("host %q is OUT OF SCOPE; refusing. In-scope: %s", in.Host, t.scope.String())
	}
	if in.Port < 1 || in.Port > 65535 {
		return "", fmt.Errorf("port %d out of range (1-65535)", in.Port)
	}

	// Short, tunable timeouts: a dead port should cost seconds, not stall.
	dialTimeout := config.SecondsEnv("ONIGAMI_TCP_DIAL_TIMEOUT", 8*time.Second)
	readTimeout := config.SecondsEnv("ONIGAMI_TCP_READ_TIMEOUT", 5*time.Second)

	addr := net.JoinHostPort(in.Host, fmt.Sprintf("%d", in.Port))
	dialer := net.Dialer{Timeout: dialTimeout}
	start := time.Now()
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return "", fmt.Errorf("connect to %s failed: %w", addr, err)
	}
	defer conn.Close()
	dialed := time.Since(start)

	if in.Payload != "" {
		_ = conn.SetWriteDeadline(time.Now().Add(readTimeout))
		if _, err := conn.Write([]byte(in.Payload)); err != nil {
			return "", fmt.Errorf("send to %s failed: %w", addr, err)
		}
	}

	_ = conn.SetReadDeadline(time.Now().Add(readTimeout))
	buf := make([]byte, 4096)
	n, readErr := conn.Read(buf)

	var b strings.Builder
	fmt.Fprintf(&b, "TCP %s — connected in %d ms\n", addr, dialed.Milliseconds())
	if n > 0 {
		fmt.Fprintf(&b, "Read %d bytes:\n%s", n, string(buf[:n]))
	} else {
		// No reply is itself a finding (open but silent, or needs a payload).
		b.WriteString("Port is open but returned no data")
		if in.Payload == "" {
			b.WriteString(" on connect (try sending a payload)")
		}
		if readErr != nil {
			fmt.Fprintf(&b, " [%v]", readErr)
		}
		b.WriteString(".")
	}
	return truncate(b.String()), nil
}
