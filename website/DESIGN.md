# onigami: design system

This site reuses the visual engine, palette and the WebGL "well + eye" of the
original Mímir launch microsite (kept intact on request), and re-anchors the
concept to onigami: an agentic red-team crew for the terminal.

## Brief
- Subject: onigami, a Go CLI that runs a crew of AI agents against targets you are
  AUTHORIZED to test (CTFs, labs, systems you own) — recon, web testing, triage,
  proof-of-concept — grounded in real tools, with gated exploitation. LLM backend
  is Groq (OpenAI-compatible).
- Assumed (placeholder, owner to confirm): the production URL, GitHub URL, version,
  license and install commands.
- Audience: security engineers and CTF players who live in a terminal. Job: make
  them trust it and run it.
- Stack: Astro (static showcase where motion is the product) + OGL for one
  raymarched WebGL scene rendered at character-cell resolution (cheap on phones).

## Concept
The eye in the well is kept, and re-read for onigami. In the old myth the eye in
the well was the price of knowing what had been. Here the eye is the **watcher you
send ahead**: it sees the target's attack surface the way an adversary would, so
you find the weak joins before a real 鬼 (oni, demon) does. 鬼神 (onigami) is a
fierce *protective* spirit — a demon you keep on your side.

This joins the retained visual (an amber glyph well with a watching eye) to the
product (a crew that looks at a target and brings back proof) and the name (鬼神).

## Concept card
```
Idea:        Send a demon ahead. onigami walks the target the way an attacker
             would, and brings back proof, not guesses.
Metaphor:    the eye at the bottom of the well as a red-team watcher, drawn the
             way a terminal draws: in glyphs.
Signature:   a live 3D well rendered as ASCII; scroll descends the shaft to the
             eye, which follows the pointer. Page transitions flood the screen
             with glyphs, like scanner output filling a buffer.
Emotion:     the hush of looking down a deep well at night; then recognition:
             it is a terminal, and the eye is yours to aim.
Motion verb: surface. Things rise from below and resolve from dense glyphs into
             letters.
Screenshot:  the amber glyph well, the pale eye at the bottom, onigami set huge
             on the rim; 鬼 as the mark.
```

## Palette (amber phosphor terminals + peat water) — unchanged
| Role | Hex | Note |
|---|---|---|
| ground | #0b0a08 | peat black, bottom of the well |
| ink | #efe6d2 | bone |
| muted | #a39a86 | secondary text, ~7:1 on ground |
| line | rgb(239 230 210 / .14) | hairlines |
| accent | #f2a93b | amber phosphor: glyph light, prompts, focus, CTA fill |
| eye | #9cc4c9 | cold water; used only for the eye and its glyphs |

Amber on black is the IBM-era amber monitor, not the generic acid-green hacker
look. It also matches onigami's own terminal dashboard (amber/yellow accent).
Used as light, not decoration.

## Type — unchanged
- Display + terminal: Martian Mono (variable 100–800). Wordmark at 800, tracking -0.06em.
- Text: Host Grotesk (variable 300–800) for reading copy.
- Scale: tokens.css. Body 17–18 px / 1.55.
- Mark: 鬼 (the oni kanji), the same glyph the terminal dashboard wears.

## Grid, shape, motion — unchanged
- 12 columns, gutter clamp(1rem → 2.5rem). Strong lines: col 1, col 5, col 9.
- Radius: 0 everywhere (terminal cells). Hairlines only where a terminal would draw a box.
- Eases: expo.out entrances, expo.inOut flood transitions, power3.out hovers.
- Hovers: nav and primary links decode (characters cycle up the glyph ramp, then
  settle); CTAs fill upward like water rising in amber; arrows nudge.
- Idle: the water ripples, the eye drifts and blinks, the camera sways with the
  pointer. All on the GSAP ticker. Reduced motion: one still frame, no idle.

## Composition
```
HERO (scene, sticky)               STATEMENT (8 cols)          SESSION (sticky narrative)
+--------------------------+      +----------------------+     +-----------+--------------+
|鬼 onigami     Manual  GH |      |   鬼神 is a fierce   |     | recon     | onigami recon|
|      ::::####::::        |      |   protective spirit  |     | test      | > output     |
|    :#  (eye)  #:         |      |                      |     | triage    |  (sticky)    |
|onigami     depth 0.00 m  |      +----------------------+     +-----------+--------------+
+--------------------------+
STAGES (index list, man-style)     TRUST (asymmetric pair)     FOOTER finale
+--------------------------+      +--------+-------------+     +--------------------------+
| recon  map in one pass   |      | ~/.onigami tree      |     | go build -o onigami [⧉] |
| test   confirm w/ tools  |      | (pre)  | copy  cols  |     | 鬼 onigami  big         |
+--------------------------+      +--------+-------------+     +--------------------------+
```
Manual page: man-page layout (NAME, SYNOPSIS, ... SEE ALSO) with a sticky index;
the well sits quietly in the top band. 404: the well is dry — the path is out of scope.

## Honesty notes
- onigami is NOT local-only: it talks to the Groq API to reason and to the target
  to test. The trust claim made on the site is the true one — every *finding* is
  tool-produced, scope is enforced in code, exploitation is human-gated — not
  "nothing leaves the machine".
- No invented credentials, metrics or logos. Example sessions are illustrative and
  labelled as such.
