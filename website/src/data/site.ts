// All visible content lives here.
// Assumptions (placeholder: true, owner must confirm or replace):
// - What onigami is: a Go CLI that runs a crew of AI agents against targets you are
//   AUTHORIZED to test (CTFs, labs, your own systems) — recon, web testing, triage,
//   proof-of-concept — grounded in real tools, with gated exploitation.
// - Version 0.1.0, the GitHub URL, the production domain, and the install commands.
// - License MIT. The LLM backend is Groq (OpenAI-compatible).
// - Every example session below is illustrative output, not a recording.

export const site = {
  name: 'onigami',
  role: 'An agentic red-team crew for your terminal',
  url: 'https://onigami.dev', // placeholder: production URL (canonical, OG, sitemap)
  description:
    'onigami sends a crew of AI agents at the targets you are cleared to test — recon, web testing, triage and proof-of-concept — and every finding it keeps was produced by a real tool, never guessed. Live exploitation waits for your yes.',
  version: '0.1.0', // placeholder
  github: 'https://github.com/kartikeywastaken/onigami', // placeholder
  license: 'MIT', // placeholder
  install: [
    { label: 'Source', cmd: 'go build -o onigami ./cmd/onigami' },
    { label: 'Go', cmd: 'go install github.com/kartikeywastaken/onigami@latest' }, // placeholder
    { label: 'Brew', cmd: 'brew install onigami' }, // placeholder
  ],
  placeholder: true,
};

export type Line = { k?: 'cmd' | 'out' | 'dim' | 'hit' | 'ok'; t: string };
export type Chapter = {
  id: string;
  name: string;
  title: string;
  text: string;
  session: Line[];
};

// The engagement, stage by stage. Output is illustrative (placeholder).
export const chapters: Chapter[] = [
  {
    id: 'recon',
    name: 'recon',
    title: 'Map the target in one pass.',
    text: 'The analyst runs the whole mechanical sweep in a single coded call — ports, service versions, HTTP fingerprints, endpoints and parameters — then correlates what it found against the CVE databases. Nothing here is a model guess; every line is something a tool actually returned.',
    session: [
      { k: 'cmd', t: 'SCOPE=demo.testfire.net onigami --tui --mission "assess for an exploitable web flaw"' },
      { k: 'dim', t: 'STAGE 1 · RECON — analyst' },
      { k: 'out', t: 'recon_sweep  nmap + http fingerprint + endpoint enum' },
      { k: 'hit', t: '80/tcp open   Apache-Coyote/1.1 (Tomcat)' },
      { k: 'hit', t: 'endpoint /login.jsp   params: uid, passw' },
      { k: 'ok', t: '61 findings recorded, verified — 0 tokens spent' },
    ],
  },
  {
    id: 'test',
    name: 'test',
    title: 'Confirm with real scanners.',
    text: 'The web-tester runs nuclei, ffuf and sqlmap against what recon found, in one coded sweep, and records only what a scanner returned. A weak model cannot narrate a finding into being here — if a tool did not confirm it, it does not exist.',
    session: [
      { k: 'cmd', t: 'STAGE 2 · WEB TESTING — web-tester' },
      { k: 'out', t: 'web_sweep  nuclei · ffuf · sqlmap' },
      { k: 'out', t: 'ffuf     /admin  /backup.zip  /swagger' },
      { k: 'out', t: 'nuclei   [apache-detect] [tomcat-manager-exposed]' },
      { k: 'hit', t: 'sqlmap   uid on /login.jsp is injectable (boolean-blind)' },
      { k: 'ok', t: '3 verified findings — each was run, none asserted' },
    ],
  },
  {
    id: 'triage',
    name: 'triage',
    title: 'Only what is proven advances.',
    text: 'Triage ranks the findings by evidence and exploitability and hands the top ones onward. It is deterministic Go, not a model: an unverified claim, however confident, is never selected — so an invented "RCE confirmed" can never reach exploitation.',
    session: [
      { k: 'cmd', t: 'STAGE 3 · TRIAGE' },
      { k: 'dim', t: 'reviewing 64 findings — verified only' },
      { k: 'out', t: '✗  "RCE confirmed on /api"  unverified claim — dropped' },
      { k: 'hit', t: '✓  SQL injection on /login.jsp (uid)       score 6' },
      { k: 'hit', t: '✓  CVE-2021-41773 path traversal           score 5' },
      { k: 'ok', t: '2 targets handed to the exploit-dev' },
    ],
  },
  {
    id: 'prove',
    name: 'prove',
    title: 'A proof-of-concept, grounded.',
    text: 'The exploit-dev writes a proof for one verified finding — grounded in the real target, version and parameters, and in a real Metasploit module when one fits. It never invents a module, endpoint or CVE, and firing anything live stays paused for your approval.',
    session: [
      { k: 'cmd', t: 'STAGE 4 · EXPLOIT-DEV' },
      { k: 'dim', t: 'grounding the PoC in a real module…' },
      { k: 'out', t: 'msf_search tomcat → exploit/multi/http/tomcat_mgr_upload' },
      { k: 'hit', t: 'PoC  authenticated WAR upload → JSP shell (demo only)' },
      { k: 'out', t: 'detect  access-log hits on /manager/html/upload' },
      { k: 'ok', t: 'live exploitation stays gated — you approve each step' },
    ],
  },
];

// Reference for the crew — also used by the home index list. Each entry is a
// stage of the engagement, not a subcommand: onigami runs one pipeline.
export type Command = { name: string; synopsis: string; summary: string; detail: string[]; options: [string, string][] };
export const commands: Command[] = [
  {
    name: 'recon',
    synopsis: 'analyst · recon_sweep',
    summary: 'Map the target in one coded pass',
    detail: [
      'Runs nmap, HTTP fingerprinting and endpoint/parameter enumeration in a single Go call, then correlates service versions against the Red Hat and CIRCL CVE databases. Everything it records came from a tool, so it is marked verified.',
      'Collapsing what used to be ~20 model turns into one deterministic call keeps a run fast, cheap and repeatable.',
    ],
    options: [
      ['SCOPE', 'the authorized target(s); enforced in code, not in a prompt'],
      ['ONIGAMI_NMAP_SUDO=0', 'skip the sudo SYN-scan prompt (unprivileged scan)'],
    ],
  },
  {
    name: 'test',
    synopsis: 'web-tester · web_sweep',
    summary: 'Confirm web issues with real scanners',
    detail: [
      'Runs nuclei (templated CVEs and misconfigurations), ffuf (content discovery) and sqlmap (SQL injection) against the endpoints recon found, and records only what a scanner actually returned.',
      'A confirmed issue cannot be asserted in prose — it has to be run. This is what keeps a weak model from hallucinating findings into the report.',
    ],
    options: [
      ['ONIGAMI_WEB_STAGE', 'auto (run only if a web surface was found) | always | never'],
      ['ONIGAMI_WEB_SQLMAP_MAX', 'how many endpoints sqlmap tests per sweep (default 3)'],
    ],
  },
  {
    name: 'triage',
    synopsis: 'deterministic selector',
    summary: 'Advance only verified, exploitable facts',
    detail: [
      'Ranks the findings by evidence and exploitability and hands the top ones to the exploit-dev. An unverified agent claim is never selected, so an invented "SQL injection confirmed" can never reach the exploitation path.',
    ],
    options: [
      ['ONIGAMI_MAX_EXPLOIT_TARGETS', 'how many findings advance to the exploit-dev (default 2)'],
    ],
  },
  {
    name: 'prove',
    synopsis: 'exploit-dev',
    summary: 'Write a grounded proof-of-concept',
    detail: [
      'Builds a proof-of-concept for one verified finding, grounded in the real target, version and parameters — and, when Metasploit is present, in a real module and its real options. It never invents a module, endpoint or CVE id.',
    ],
    options: [
      ['GROQ_MODEL_EXPLOIT', 'model for the exploit-dev (default openai/gpt-oss-120b)'],
    ],
  },
  {
    name: 'demo',
    synopsis: 'exploiter · Metasploit',
    summary: 'Demonstrate, scope-checked and gated',
    detail: [
      'Drives Metasploit over msfrpcd to demonstrate a confirmed finding. Every run is scope-checked, written to an audit log, and paused for your approval first. Nothing fires against a target without a human yes.',
    ],
    options: [
      ['ONIGAMI_MSFRPCD', 'path to msfrpcd (the Metasploit RPC daemon)'],
      ['ONIGAMI_AUDIT_LOG', 'where gated exploit actions are recorded'],
    ],
  },
  {
    name: 'scope',
    synopsis: 'authorization boundary',
    summary: 'Stay inside what you may test',
    detail: [
      'Scope is parsed once and enforced in code by every tool: a target outside it is refused before a single packet is sent. onigami is for authorized testing only — CTFs, labs, and systems you own or have written permission to assess.',
    ],
    options: [
      ['SCOPE', 'IP, CIDR or hostname; space or comma separated'],
    ],
  },
];

export const files = [
  ['~/.onigami/findings-*.jsonl', 'the shared notebook: every verified fact, and every flagged claim'],
  ['~/.onigami/audit.log', 'every gated Metasploit action, with its scope and timestamp'],
  ['.env', 'Groq keys and tunables — gitignored, never committed'],
] as const;

export const env = [
  ['SCOPE', 'the authorized target(s) for the engagement (required)'],
  ['ONIGAMI_MISSION', 'the objective the crew works toward (required)'],
  ['GROQ_API_KEY', 'the LLM backend key; onigami talks to Groq, an OpenAI-compatible API'],
  ['GROQ_TEMPERATURE', 'sampling temperature; low curbs hallucination (default 0.2)'],
  ['ONIGAMI_UI_BG', 'the dashboard background tint, as a hex colour'],
] as const;
