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

// TestHTTPRequestTLSSkip confirms the HTTP tool no longer rejects a target with
// an expired/invalid certificate (demo.testfire.net's cert is expired, which
// blocked every HTTPS fetch before the InsecureSkipVerify fix). Skipped unless
// ONIGAMI_NET_IT=1 (it makes a real network request).
func TestHTTPRequestTLSSkip(t *testing.T) {
	if os.Getenv("ONIGAMI_NET_IT") == "" {
		t.Skip("set ONIGAMI_NET_IT=1 to run the network TLS test")
	}
	sc, err := scope.Parse("demo.testfire.net")
	if err != nil {
		t.Fatalf("scope: %v", err)
	}
	h := NewHTTPRequest(sc)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	out, err := h.Run(ctx, json.RawMessage(`{"method":"HEAD","url":"https://demo.testfire.net/"}`))
	if err != nil {
		t.Fatalf("HEAD https failed: %v", err)
	}
	if strings.Contains(out, "certificate") {
		t.Fatalf("still a cert error: %s", out)
	}
	t.Logf("HTTPS HEAD ok:\n%s", out)
}
