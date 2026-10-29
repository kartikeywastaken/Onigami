# Onigami 鬼神: design system

> Adapted (2026-10-29, via /awwwards) from the sibling "Onigami the C shell" site. The shrine
> palette, Type Garden signature, Playfair/DM Mono/Shippori type and the growth metaphor are kept
> intact; the CONTENT is re-anchored to onigami the agentic red-team crew (the Go CLI in
> ~/Desktop/agent). The 鬼神 duality is re-read as strike (the oni: the tools that reach into a
> target) and proof (the kami: the discipline that keeps every finding grounded and gated).

## Brief
- Subject: Onigami, a Go CLI agentic red-team crew for AUTHORIZED testing (CTFs, labs, your own
  systems). It runs a fixed pipeline — recon, web testing, triage, exploit-dev, gated exploiter —
  grounded in real tools, with a terminal dashboard. The growth states (seed/bud/grown) are the
  same ones its TUI already uses for its agents.
- Name: 鬼神 is a real Japanese word, read *kishin* or *onigami*, "a fierce god". Its two kanji are
  exactly 鬼 (oni, demon) and 神 (kami, god or spirit), which is the "mixture of oni and kami".
- Audience: developers and the people who follow the owner's work. Job: show the shell, its
  progress, and the idea, and get people to clone and follow it.
- Given asset: Type Garden by Akshat Agarwal (canvas generator, stems and roses grow out of typed
  letters; space cuts, backspace withers, enter clears). Owner asked for Playfair Display Bold for
  the letters, DM Mono for the interface, and Japanese text on the site.
- Stack: Astro + GSAP + Lenis, same engine as the sibling site `../mimir`. No WebGL: the
  signature is a 2D canvas, so OGL is not loaded.

## Concepts
| | Idea | Anchor | Signature | O/F/Fe/P |
|---|---|---|---|---|
| **A. The tokenizer garden** | A shell reads a line and cuts it into words at every space. Type Garden does the same thing in ink: each word grows its own garden, space cuts the stems. The oni 鬼 cuts, the kami 神 grows. | the name 鬼神 = oni + kami; what a shell's tokenizer does; the generator's red rose / blue stem palette is aka-oni 赤鬼 and ao-oni 青鬼 | a live prompt in the hero. You type a command, every word grows, Enter runs it in a toy shell: the garden withers and `echo` plants its output as a new garden | 5 / 5 / 5 / 1 |
| B. Shrine gate | The prompt as a torii: you pass through it into the shell | torii as threshold | gate opens on scroll | 3 / 3 / 4 / 4 |
| C. Oni mask | The page as a carved mask that reacts to commands | oni masks in folk theatre | mask expressions per exit code | 3 / 2 / 3 / 3 |

Pick A: it joins the name (two kanji), the product (a tokenizer and a REPL) and the given asset
(a generator whose rules already are "space cuts"). B and C are costumes; A is the mechanism.

## Concept card
```
Idea:        Onigami is a shell where every word you type grows its own garden.
Metaphor:    鬼神. The oni cuts (space, backspace, exit), the kami grows (every letter).
Signature:   the hero is a working prompt drawn by Type Garden. Type, space cuts, backspace
             withers, Enter runs a toy shell in the browser: echo / type / pwd / cd / help / exit,
             unknown words answer "command not found" exactly like the real shell does today.
             echo replants its output as a new garden; cd garden walks to the studio page.
             Owner's choices (round 1): sakura 桜 instead of roses; every Enter shakes petals loose
             and they settle at the foot of the garden; a failed command is eaten by an aka-oni
             (red head, gold horns) that chomps across the line; an idle torii wind gusts through
             the stems every 7 to 12 s and lifts the fallen petals.
Emotion:     the first second: a red rose opening on a black page. Then: oh, it is a prompt.
Motion verb: grow. Things rise from a baseline and settle on a spring; leaving is withering
             (scale down from the root, last first).
Screenshot:  "onigami" in rice-paper Playfair on lacquer black, vermilion sakura on gold stems,
             鬼神 in vertical type down the right edge.
```

## Palette (shrine vermilion on lacquer, chosen by the owner)
Eleven garden palettes: the owner's 朱塗 Shrine as default, then the original ten, each named in
Japanese (鬼神 keeps the original red/blue, aka-oni and ao-oni). The selected one themes the whole
site through CSS variables on `<html>` (`--c-bg`, `--c-fg`, `--c-muted`, `--c-flower`, `--c-stem`,
`--c-line`), so every text colour follows. Default `朱塗 Shrine`:

| Role | Hex | Note |
|---|---|---|
| ground | #0D0A09 | urushi lacquer black |
| ink | #F4EDE1 | rice paper: letters, body (17:1) |
| muted | #A9A196 | 7.7:1 on ground |
| flower (accent) | #F2452D | torii vermilion: sakura, the oni, focus, CTA fill |
| stem | #C9A24B | gold leaf: stems, leaves, oni horns |
| petal line | #FFD9B8 | stamens, petal veins |
| line | ink at 16% | hairlines |

`muted` is hand-picked per palette for 4.5:1 or better (see `PAL` in `src/garden/core.js`).

## Type
- Display: Playfair Display (Google, variable 400–900), set at 700–800, tracking -0.03 to -0.05em.
  The garden letters use the same face, so headings and garden read as one hand.
- Japanese: Shippori Mincho B1 800 (Google), subset to the characters on the site. Mincho has the
  same high-contrast brush logic as Playfair. Vertical (tategaki) where it is a label.
- Interface: DM Mono 400/500, 12–14 px for labels, prompts and hints.
- Body: Playfair Display 400 at 18–20 px / 1.5 (a serif body is part of the voice; the mono
  carries every functional line).

## Grid, shape, motion
- 12 columns, gutter clamp(1rem → 2.5rem). Strong lines: col 1, col 7.
- Radius: 999px for controls (the original's pills), 0 for everything else.
- Eases: expo.out entrances, expo.inOut transitions, back.out for small pops. Growth uses the
  garden's own spring.
- Hovers: nav and primary links grow an underline from the left like a stem and the label rises;
  buttons fill upward in flower red; index rows grow a stem line under them.
- Idle: the garden boils (line jitter every 120 ms) and roses turn to face the pointer; the
  studio poster loops. All on the GSAP ticker; canvases pause off screen. Reduced motion: no
  boil, everything already grown, no autotype.
- The first screen never "settles" on purpose: the garden boils every 120 ms and roses turn to the
  pointer. That is the concept breathing at rest, not a loader that never ends.
- Page transition: a curtain in the flower colour grows up from the baseline carrying 鬼 or 神
  (leaving shows 鬼, the oni cuts; arriving shows 神, the kami grows), then withers upward.

## Composition
```
HERO 100svh (canvas)                 鬼 / 神 diptych                  THE LOOP (sticky poster)
+----------------------------------+ +------------+------------+     +-----------+------------+
|鬼神 Onigami        庭 道 Source  | |  鬼         |   神        |     | 01 読 Read |  [poster   |
|                               鬼  | |  cuts      |   grows     |     | 02 切 Split|   canvas   |
|      o n i g a m i  (garden)  神  | |  (text)    |   (text)    |     | 03 探 Find |   sticky]  |
|  $ output line                    | +------------+------------+     | 04 走 Run  |            |
|H1 A shell where every word ...   |                                  +-----------+------------+
|hints · swatches                   |
+----------------------------------+
GROWTH RECORD (index rows)            SUMMON (finale)                 FOOTER
+----------------------------------+ +----------------------------+ +--------------------------+
| 01 Print a prompt      育った ✓   | | 召喚 Summon it.             | | 鬼神 · colophon · links   |
| 04 The exit builtin    芽         | | $ git clone ... [Copy]     | +--------------------------+
| ...                    種         | +----------------------------+
+----------------------------------+
```
Inner page `/garden/`: the full poster studio (words, 7 motion presets, 10 palettes, MP4/WebM and
PNG-frame export) plus a link to the standalone single-file version `/type-garden.html`.
404: 道に迷った, the path "404" grows and withers on a loop.

## Genericness test
- Generic: a dark hero with a fake terminal window. Changed: the prompt is the garden itself, no
  window chrome, and it really accepts input.
- Generic: three feature cards. Changed: the REPL as four real steps (read, split, find, run)
  told by one sticky poster that changes motion per step.
- Generic: a roadmap with checkmarks. Changed: the Codecrafters stages as a growth record:
  育った grown, 芽 sprouting, 種 seed.
- Generic Japanese styling: random katakana as decoration. Changed: every Japanese word is a real
  word that carries meaning (the name, the four verbs, the growth states), set in proper mincho.
