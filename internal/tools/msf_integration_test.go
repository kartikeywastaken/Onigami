package tools

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestMSFIntegration exercises the real msfrpcd lifecycle + JSON-RPC client:
// start the daemon, authenticate with the generated token, and make two
// read-only calls. It is skipped by default (it needs Metasploit installed and
// takes ~a minute to boot the daemon); run it with:
//
//	ONIGAMI_MSF_IT=1 go test ./internal/tools -run TestMSFIntegration -v -timeout 180s
func TestMSFIntegration(t *testing.T) {
	if os.Getenv("ONIGAMI_MSF_IT") == "" {
		t.Skip("set ONIGAMI_MSF_IT=1 to run the Metasploit integration test")
	}
	mgr := NewMSFManager()
	if !mgr.Available() {
		t.Skip("msfrpcd not found on this machine")
	}
	defer mgr.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()

	ver, err := mgr.call(ctx, "core.version")
	if err != nil {
		t.Fatalf("core.version failed: %v", err)
	}
	t.Logf("core.version => %s", ver)

	mods, err := mgr.call(ctx, "module.search", "vsftpd")
	if err != nil {
		t.Fatalf("module.search failed: %v", err)
	}
	t.Logf("module.search(vsftpd) => %s", truncate(string(mods)))
}
