// Package tools holds onigami's real, external tools: the scanners the Analyst
// runs (nmap, nikto, …) and the CVE lookup it uses for research.
//
// Two rules keep this package safe to hand to an LLM:
//  1. We never run a shell. Commands are executed with an explicit argument
//     list (exec.CommandContext(name, args...)), so there is no string for the
//     model to inject ";" or "|" into.
//  2. The model supplies only a target (and a few bounded options). We build
//     every flag ourselves, and validate the target (no leading "-", no
//     whitespace) so it can't smuggle in extra arguments.
package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// maxOutput caps how much tool output we feed back to the model, so a chatty
// scanner can't blow out the context window.
const maxOutput = 8000

// runCommand executes name with args, bounded by timeout, and returns the
// combined stdout+stderr. Scanners often exit non-zero yet still produce useful
// output, so we return whatever they printed and only surface an error when
// there's nothing to show (or the command couldn't start / timed out).
func runCommand(ctx context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	runErr := cmd.Run()
	out := strings.TrimSpace(buf.String())

	if ctx.Err() == context.DeadlineExceeded {
		note := fmt.Sprintf("[%s timed out after %s — partial output below]\n", name, timeout)
		return truncate(note + out), nil
	}
	if runErr != nil && out == "" {
		return "", fmt.Errorf("%s failed: %w", name, runErr)
	}
	return truncate(out), nil
}

// installed reports whether a binary is on PATH. Used at wiring time so we only
// offer the model tools that actually exist on this machine.
func installed(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// validTarget rejects targets that could inject extra command-line flags or
// arguments. Because we pass args as a slice (no shell), the real risks are a
// leading "-" (parsed as a flag) and embedded whitespace (splits into args).
func validTarget(target string) error {
	t := strings.TrimSpace(target)
	if t == "" {
		return fmt.Errorf("target is empty")
	}
	if strings.HasPrefix(t, "-") {
		return fmt.Errorf("target %q may not start with '-'", t)
	}
	if strings.ContainsAny(t, " \t\r\n") {
		return fmt.Errorf("target %q may not contain whitespace", t)
	}
	return nil
}

// truncate shortens output to maxOutput characters, noting what was cut.
func truncate(s string) string {
	if len(s) <= maxOutput {
		return s
	}
	return s[:maxOutput] + fmt.Sprintf("\n…[truncated %d bytes]", len(s)-maxOutput)
}
