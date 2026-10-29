package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"onigami/internal/agent"
	"onigami/internal/config"
	"onigami/internal/llm"
	"onigami/internal/orchestrator"
	"onigami/internal/scope"
	"onigami/internal/tools"
	"onigami/internal/ui"
)

func main() {
	// --tui opts into the terminal dashboard; by default we keep the plain line
	// printer, which stays clean when piped/teed to a log file.
	tuiFlag := flag.Bool("tui", false, "show the live terminal dashboard instead of plain line output")
	missionFlag := flag.String("mission", "", "the engagement objective the crew works toward (also ONIGAMI_MISSION; prompted if unset)")
	llmOrchFlag := flag.Bool("llm-orch", false, "drive the engagement with the free-form LLM orchestrator instead of the structured stage pipeline (default)")
	flag.Parse()

	// loading env
	if err := config.LoadDotenv(".env"); err != nil {
		fmt.Fprintln(os.Stderr, "warning: could not read .env:", err)
	}

	// converts thoughts in visible wors in the ui
	obs := func(e agent.Event) {
		indent := strings.Repeat("  ", e.Depth)
		label := fmt.Sprintf("%-12s", e.Agent)
		switch e.Kind {
		case agent.EventStart:
			fmt.Printf("%s▶ %s  started: %s\n", indent, label, e.Text)
		case agent.EventThinking:
			fmt.Printf("%s  %s  reasoning:\n%s\n", indent, label, indentBlock(e.Text, indent+"      "))
		case agent.EventToolCall:
			fmt.Printf("%s  %s  → calling tool: %s\n", indent, label, e.Text)
		case agent.EventToolResult:
			fmt.Printf("%s  %s  ← %s\n", indent, label, e.Text)
		case agent.EventMessage:
			fmt.Printf("%s  %s  thinks:\n%s\n", indent, label, indentBlock(e.Text, indent+"      "))
		case agent.EventReport:
			fmt.Printf("%s✔ %s  reported.\n", indent, label)
		case agent.EventError:
			fmt.Printf("%s✘ %s  error: %s\n", indent, label, e.Text)
		}
	}

	// Build the real Groq client for the orchestrator. If the key is missing we
	// stop with a clear message pointing at the .env file.
	orchClient, model, err := buildClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// The user MUST define the scope. Every scan tool enforces it; nothing is
	// scanned outside it. (For a CTF, this is just the box's IP or range.)
	sc, err := promptScope()
	if err != nil {
		fmt.Fprintln(os.Stderr, "scope error:", err)
		os.Exit(1)
	}

	// Per-agent keys: each specialist can use its own API key, which only helps
	// with rate limits if the keys belong to SEPARATE Groq accounts (limits are
	// per account, not per key). Missing keys fall back to the main one.
	analystClient := clientFromEnv("GROQ_API_KEY_ANALYST", orchClient)
	exploitClient := clientFromEnv("GROQ_API_KEY_EXPLOIT", orchClient)
	// The Web-Tester reuses the Analyst's key unless given its own.
	webTesterClient := clientFromEnv("GROQ_API_KEY_WEBTESTER", analystClient)

	// Per-agent models. Groq's rate limits are per-model-per-org, so we split the
	// crew across two model buckets by default: the low-volume brain and
	// exploitation workers stay on the base model; the high-volume recon/testing
	// workers run on the smaller model, drawing on an independent budget. Each is
	// overridable with its own env var.
	orchModel := modelEnv("GROQ_MODEL_ORCH", model)
	analystModel := modelEnv("GROQ_MODEL_ANALYST", smallGroqModel)
	webTesterModel := modelEnv("GROQ_MODEL_WEBTESTER", smallGroqModel)
	exploitDevModel := modelEnv("GROQ_MODEL_EXPLOIT", model)
	exploiterModel := modelEnv("GROQ_MODEL_EXPLOITER", model)

	// Prime sudo so nmap can run fast, accurate SYN scans (-sS needs root).
	// This asks for your password ONCE, here in the terminal; only nmap is
	// elevated, the agent itself stays unprivileged. Skip with ONIGAMI_NMAP_SUDO=0.
	primeNmapSudo()

	// Metasploit plumbing for the Exploiter. The manager starts msfrpcd lazily
	// (only if the exploiter is actually used) and must be shut down on exit.
	audit := tools.NewAuditLog()
	msfMgr := tools.NewMSFManager()
	defer msfMgr.Shutdown()

	// Shared, disk-backed findings store: every worker records into it, so a
	// rate-limit 429 or a give-up mid-run no longer loses what was learned — we
	// can always write a report from it (see the end of main).
	findings := tools.NewFindingsStore()

	// makeConfig assembles the orchestrator Config. Both the plain and TUI paths,
	// and both engagement drivers (the structured pipeline and the LLM
	// orchestrator), use it with their own observer/approver — so the engagement
	// itself is identical; only how we watch it and answer the gate differ.
	makeConfig := func(observer agent.Observer, approver agent.Approver) orchestrator.Config {
		return orchestrator.Config{
			Orchestrator: orchClient,
			Analyst:      analystClient,
			WebTester:    webTesterClient,
			ExploitDev:   exploitClient,
			Exploiter:    exploitClient, // reuses the exploit key unless you split it
			MSF:          msfMgr,
			Audit:        audit,
			Findings:     findings,

			OrchestratorModel: orchModel,
			AnalystModel:      analystModel,
			WebTesterModel:    webTesterModel,
			ExploitDevModel:   exploitDevModel,
			ExploiterModel:    exploiterModel,

			Scope:    sc,
			Observer: observer,
			Approver: approver,
		}
	}

	// The mission is the operator's objective — what the crew should achieve.
	// Supplied via --mission, the ONIGAMI_MISSION env var, or an interactive
	// prompt; a blank mission falls back to the generic default inside the driver.
	mission := resolveMission(*missionFlag)

	// drive runs one engagement with the given observer/approver and returns its
	// summary. By default it's the structured stage pipeline (recon → web →
	// triage → exploit-dev → exploiter), which guarantees the progression;
	// --llm-orch selects the older free-form LLM brain instead.
	drive := func(observer agent.Observer, approver agent.Approver) (string, error) {
		cfg := makeConfig(observer, approver)
		if *llmOrchFlag {
			objective := mission
			brief := fmt.Sprintf("%s\n\nIN-SCOPE TARGETS (authorized): %s\nStay strictly within this scope. Work in steps and delegate to your specialists.",
				objective, sc)
			rep, e := orchestrator.New(cfg).Run(context.Background(), agent.Task{Objective: objective, Brief: brief}, observer)
			return rep.Summary, e
		}
		return orchestrator.NewPipeline(cfg, mission).Run(context.Background())
	}

	var report agent.Report
	if *tuiFlag {
		// Dashboard: the UI runs the engagement on a goroutine and supplies the
		// observer (activity stream) and approver (the footer proceed/hold gate).
		summary, runErr := ui.Run(ui.Config{
			Scope:    sc.String(),
			Findings: findings,
			Start: func(observer agent.Observer, approver agent.Approver) (string, error) {
				// Route Groq retry/backoff notices into the activity stream as
				// events, so they don't write over the alt-screen TUI.
				llm.RetryNotifier = func(msg string) {
					observer(agent.Event{Agent: agent.Role("groq"), Kind: agent.EventMessage, Text: msg})
				}
				defer func() { llm.RetryNotifier = nil }()
				return drive(observer, approver)
			},
		})
		report, err = agent.Report{Summary: summary}, runErr
	} else {
		// Plain line output (the default): print the banner, then stream events.
		fmt.Printf("\nonigami — engagement\n")
		fmt.Printf("driver:   %s\n", driverName(*llmOrchFlag))
		fmt.Printf("mission:  %s\n", mission)
		fmt.Printf("models:   orch=%s  analyst=%s  web=%s  exploit=%s  exploiter=%s\n",
			orchModel, analystModel, webTesterModel, exploitDevModel, exploiterModel)
		fmt.Printf("scope:    %s\n", sc)
		fmt.Printf("scanners: %s\n", tools.ScannerStatus())
		fmt.Printf("web:      %s\n", tools.WebTesterStatus())
		fmt.Printf("exploit:  %s\n", tools.MSFStatus(msfMgr))
		if msfMgr.Available() {
			fmt.Printf("audit:    %s\n", audit.Path())
		}
		fmt.Printf("keys:     %s\n", keyStatus())
		fmt.Printf("findings: %s\n", findings.Path())
		fmt.Println(strings.Repeat("─", 64))

		summary, runErr := drive(obs, terminalApprover)
		report, err = agent.Report{Summary: summary}, runErr
	}

	// Always produce output. Whether the orchestrator finished cleanly or the run
	// died (a rate-limit 429, a worker give-up), we print what was actually
	// gathered — findings are durable in the store, so the engagement is never a
	// total loss. The orchestrator's own summary is shown when it produced one.
	fmt.Println(strings.Repeat("─", 64))
	if err != nil {
		fmt.Fprintln(os.Stderr, "run ended early:", err)
		fmt.Println("RUN ENDED EARLY — reporting what was gathered before it stopped:")
		fmt.Println()
		fmt.Println(findingsReport(findings))
		os.Exit(1)
	}
	fmt.Println("FINAL REPORT:")
	if s := strings.TrimSpace(report.Summary); s != "" {
		fmt.Println(s)
	} else {
		// The orchestrator finished without writing a summary — fall back to the
		// findings so the operator still gets the engagement's results.
		fmt.Println("(the orchestrator produced no written summary; findings below)")
	}
	fmt.Println()
	fmt.Println(findingsReport(findings))
}

// findingsReport renders the shared findings store for the final output.
func findingsReport(f *tools.FindingsStore) string {
	return fmt.Sprintf("ENGAGEMENT FINDINGS (%d, persisted at %s):\n%s",
		f.Count(), f.Path(), f.Report())
}

// defaultGroqModel is our pick for agentic tool-calling. gpt-oss-120b has
// strong reasoning + reliable function calling and is available on this key.
// Override it with the GROQ_MODEL env var (e.g. "openai/gpt-oss-20b" for speed,
// or "qwen/qwen3.8-27b").
const defaultGroqModel = "openai/gpt-oss-120b"

// smallGroqModel is the default for the high-volume recon/testing workers, so
// they draw on a different per-model rate-limit bucket than the orchestrator.
const smallGroqModel = "openai/gpt-oss-20b"

// primeNmapSudo asks for the sudo password once up front (in this terminal) and,
// on success, sets ONIGAMI_NMAP_PRIVILEGED so the nmap tool uses sudo SYN scans.
// It's a no-op if we're already root, sudo is missing, or ONIGAMI_NMAP_SUDO=0.
func primeNmapSudo() {
	if os.Getenv("ONIGAMI_NMAP_SUDO") == "0" || os.Geteuid() == 0 {
		return
	}
	if _, err := exec.LookPath("sudo"); err != nil {
		return
	}
	if _, err := exec.LookPath("nmap"); err != nil {
		return // no nmap, no point
	}
	fmt.Println("Priming sudo for faster nmap SYN scans (enter your password; or set ONIGAMI_NMAP_SUDO=0 to skip).")
	cmd := exec.Command("sudo", "-v")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "  → sudo not primed; nmap will run unprivileged (slower, less accurate).")
		return
	}
	os.Setenv("ONIGAMI_NMAP_PRIVILEGED", "1")
	// Keep the sudo credential warm for the length of the engagement so scans
	// late in the run don't silently drop back to unprivileged.
	go func() {
		for {
			time.Sleep(60 * time.Second)
			_ = exec.Command("sudo", "-n", "-v").Run()
		}
	}()
}

// modelEnv returns the model id from env var `name`, or `def` if it's unset.
func modelEnv(name, def string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return def
}

// buildClient constructs the real Groq client from configuration. It returns an
// error (rather than falling back to anything) if the API key is missing.
func buildClient() (llm.Client, string, error) {
	key := os.Getenv("GROQ_API_KEY")
	if key == "" {
		return nil, "", fmt.Errorf(
			"GROQ_API_KEY is not set.\n  → Put your key in the .env file at the project root:\n    %s/.env\n  Get a free key at https://console.groq.com/keys",
			mustCwd())
	}
	model := os.Getenv("GROQ_MODEL")
	if model == "" {
		model = defaultGroqModel
	}
	return llm.NewGroq(key), model, nil
}

// clientFromEnv returns a Groq client built from the key in env var `name`, or
// `fallback` if that var is unset. Lets each agent optionally use its own key.
func clientFromEnv(name string, fallback llm.Client) llm.Client {
	if k := strings.TrimSpace(os.Getenv(name)); k != "" {
		return llm.NewGroq(k)
	}
	return fallback
}

// keyStatus reports, for the startup banner, which agents have their own key.
func keyStatus() string {
	own := func(name string) string {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			return "own"
		}
		return "shared"
	}
	return fmt.Sprintf("orchestrator=main  analyst=%s  exploit=%s",
		own("GROQ_API_KEY_ANALYST"), own("GROQ_API_KEY_EXPLOIT"))
}

// mustCwd returns the current working directory for a friendlier error message,
// or "." if it can't be determined.
func mustCwd() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

// stdin is a single shared reader for all interactive prompts. (Using separate
// bufio.Readers on os.Stdin can swallow buffered input, so we keep just one.)
var stdin = bufio.NewReader(os.Stdin)

// promptScope gets the authorized scope from the user. It reads the SCOPE env
// var if set, otherwise asks interactively. Scope is required — no default.
func promptScope() (*scope.Scope, error) {
	raw := os.Getenv("SCOPE")
	if raw == "" {
		fmt.Print("Enter engagement scope (IP, CIDR, or hostname — space/comma separated): ")
		line, _ := stdin.ReadString('\n')
		raw = strings.TrimSpace(line)
	}
	if raw == "" {
		return nil, fmt.Errorf("scope is required (nothing was entered)")
	}
	return scope.Parse(raw)
}

// resolveMission figures out the engagement objective, in priority order:
// the --mission flag, then the ONIGAMI_MISSION env var, then an interactive
// prompt. A blank result is fine — the driver falls back to a generic default.
func resolveMission(flagVal string) string {
	if m := strings.TrimSpace(flagVal); m != "" {
		return m
	}
	if m := strings.TrimSpace(os.Getenv("ONIGAMI_MISSION")); m != "" {
		return m
	}
	fmt.Println("What should this engagement achieve? (the mission the crew works toward)")
	fmt.Print("Mission [blank = assess for any exploitable vulnerability]: ")
	line, _ := stdin.ReadString('\n')
	return strings.TrimSpace(line)
}

// driverName labels which engagement driver is in use, for the startup banner.
func driverName(llmOrch bool) string {
	if llmOrch {
		return "LLM orchestrator (free-form; --llm-orch)"
	}
	return "structured pipeline (recon → web → triage → exploit-dev → exploiter)"
}

// terminalApprover is the proceed/hold gate. The worker's output has already
// been printed above, so the user can read it, then type their decision.
func terminalApprover(_ context.Context, role agent.Role, _ string) bool {
	fmt.Println()
	fmt.Println(strings.Repeat("─", 64))
	fmt.Printf("⚠  The %s produced the output above (review it).\n", role)
	fmt.Print("   Proceed with it, or hold? [proceed/hold]: ")

	line, _ := stdin.ReadString('\n')
	ans := strings.TrimSpace(strings.ToLower(line))

	proceed := ans == "proceed" || ans == "p"
	if proceed {
		fmt.Println("   → proceeding.")
	} else {
		fmt.Println("   → holding; the engagement will stop here.")
	}
	fmt.Println(strings.Repeat("─", 64))
	return proceed
}

// indentBlock indents every line of a multi-line string, for readable nesting.
func indentBlock(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, ln := range lines {
		lines[i] = prefix + ln
	}
	return strings.Join(lines, "\n")
}
