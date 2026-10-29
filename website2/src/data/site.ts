// All visible content lives here.
// Facts from the owner's repo (~/Desktop/agent): onigami is a Go CLI agentic red-team
// assistant for AUTHORIZED testing (CTFs, labs, your own systems). The LLM backend is Groq
// (OpenAI-compatible). It runs a fixed pipeline: recon -> web testing -> triage -> exploit-dev
// -> gated exploiter, grounded in real tools, with a terminal dashboard (--tui).
// Assumptions (placeholder: true, owner must confirm or replace):
// - the production domain onigami.dev and the public source URL
// - the exact states in `stages` (update by hand as capabilities land)

export const site = {
  name: 'Onigami',
  kanji: '鬼神',
  kana: 'おにがみ',
  role: 'an agentic red-team crew',
  url: 'https://onigami.dev', // placeholder: production URL (canonical, OG, sitemap)
  description:
    'Onigami 鬼神 is an agentic red-team crew for your terminal. It walks a target you are cleared to test, and every finding it keeps grows from a real tool, never a guess.',
  github: 'https://github.com/kartikeywastaken/onigami', // placeholder: public repo
  author: 'kartikeywastaken',
  lang: 'Go',
  clone: 'git clone https://github.com/kartikeywastaken/onigami.git', // placeholder (same repo)
  build: ['cd onigami', 'go build -o onigami ./cmd/onigami', 'SCOPE=demo.testfire.net ./onigami --tui'],
  placeholder: true,
};

// The four stages of an engagement, in the order onigami runs them. Each has a poster.
export type Step = { id: string; jp: string; kana: string; en: string; title: string; text: string; poster: { text: string; preset: string; seed: number }; code: string };
export const steps: Step[] = [
  {
    id: 'recon', jp: '探', kana: 'さぐる', en: 'Recon',
    title: 'Map the target in one pass.',
    text: 'The analyst runs the whole mechanical sweep in one coded call: ports, service versions, HTTP fingerprints, endpoints and parameters, then correlates them against the CVE databases. Nothing here is a model guess; every line is what a tool returned.',
    poster: { text: 'onigami recon', preset: 'grow', seed: 4 },
    code: 'recon_sweep(scope)   // nmap + http + enum\n// records every result as verified',
  },
  {
    id: 'test', jp: '験', kana: 'ためす', en: 'Test',
    title: 'Confirm with real scanners.',
    text: 'The web-tester runs nuclei, ffuf and sqlmap against what recon found, in one coded sweep, and records only what a scanner returned. A weak model cannot narrate a finding into being here; if a tool did not confirm it, it does not exist.',
    poster: { text: 'web sweep', preset: 'reach', seed: 23 },
    code: 'web_sweep(endpoints) // nuclei·ffuf·sqlmap\n// a confirmed issue has to be run',
  },
  {
    id: 'triage', jp: '選', kana: 'えらぶ', en: 'Triage',
    title: 'Only what is proven advances.',
    text: 'Triage ranks the findings by evidence and exploitability and hands the top ones onward. It is deterministic Go, not a model, so an unverified claim is never selected, and an invented "RCE confirmed" can never reach exploitation.',
    poster: { text: 'triage', preset: 'typed', seed: 11 },
    code: 'for _, f := range findings {\n  if !f.Verified { continue }\n}',
  },
  {
    id: 'prove', jp: '証', kana: 'あかす', en: 'Prove',
    title: 'A proof-of-concept, grounded.',
    text: 'The exploit-dev writes a proof for one verified finding, grounded in the real target, version and parameters, and in a real Metasploit module when one fits. It never invents a module, endpoint or CVE, and firing anything live waits for your yes.',
    poster: { text: 'onigami prove', preset: 'visit', seed: 9 },
    code: 'msf_search(cve)      // a real module\n// live exploitation stays gated',
  },
];

// onigami's capabilities, as a growth record. State is a placeholder: update as each lands.
// grown = shipped and tested, bud = in progress, seed = planned.
export type Stage = { n: string; name: string; state: 'grown' | 'bud' | 'seed'; note: string };
export const stages: Stage[] = [
  { n: '01', name: 'Coded recon sweep', state: 'grown', note: 'nmap, HTTP fingerprint and endpoint enum in one call' },
  { n: '02', name: 'CVE correlation', state: 'grown', note: 'Red Hat and CIRCL, auto-recorded as verified' },
  { n: '03', name: 'Verified vs. claimed findings', state: 'grown', note: 'tool facts are trusted; agent assertions are not' },
  { n: '04', name: 'Coded web sweep', state: 'grown', note: 'nuclei, ffuf and sqlmap, each result recorded' },
  { n: '05', name: 'Triage hallucination guard', state: 'grown', note: 'only verified, exploitable facts reach the exploit-dev' },
  { n: '06', name: 'Grounded exploit-dev', state: 'grown', note: 'a PoC tied to the real target, version and params' },
  { n: '07', name: 'Metasploit module grounding', state: 'grown', note: 'read-only msf_search and msf_module_info' },
  { n: '08', name: 'Gated, audited exploiter', state: 'grown', note: 'scope-checked, approved, written to an audit log' },
  { n: '09', name: 'Reflector guard', state: 'grown', note: 'bounces a report that ran no tool; low temperature' },
  { n: '10', name: 'Terminal dashboard', state: 'grown', note: 'roster, activity and findings, in the growth states' },
  { n: '11', name: 'Mission and scope, enforced', state: 'grown', note: 'a mission is required; scope is checked in code' },
  { n: '12', name: 'Scanner-written web findings', state: 'bud', note: 'extend tool-grounded recording across every tool' },
  { n: '13', name: 'Container execution', state: 'seed', note: 'run every tool in a sandbox, PentAGI-style' },
  { n: '14', name: 'More models and providers', state: 'seed', note: 'beyond Groq; pick per cost and latency' },
];

export const states = {
  grown: { jp: '育った', en: 'grown' },
  bud: { jp: '芽', en: 'sprouting' },
  seed: { jp: '種', en: 'seed' },
} as const;
