# onigami — site

The showcase site for **onigami**, an agentic red-team crew for your terminal.

The concept is the name itself: 鬼神 (*onigami*, "a fierce god") read as two hands —
**鬼 oni, the strike** (the tools that reach into a target) and **神 kami, the proof**
(the discipline that keeps every finding grounded and gated). The signature is the
**Type Garden**: type into the hero and your words grow stems and sakura — space cuts
a word, backspace withers a letter, enter runs it. Shrine palette (朱塗 vermilion +
gold on lacquer), set in Playfair Display, DM Mono and Shippori Mincho.

Built with Astro + GSAP + Lenis. No WebGL — the garden is a 2D canvas. The full
concept, palette and motion system are in [`DESIGN.md`](./DESIGN.md).

## Develop

Needs Node 20+.

```
npm install
npm run dev        # http://localhost:4321
npm run build      # → dist/
npm run preview    # serve the production build
```

## Structure

```
src/
  pages/        index.astro, 404.astro
  layouts/      Base.astro — <head>, fonts, JSON-LD, skip link
  components/   Header, Footer, Swatches, StateIcon
  data/site.ts  all copy + the growth record (edit here, not in markup)
  garden/       shell.ts (toy prompt), core.js (canvas renderer)
  motion/       setup.ts, theme.ts — page motion wired to the engine
  engine/       framework-agnostic scroll / reveal / transition / loader
  styles/       tokens.css, site.css, engine.css
scripts/
  jp-subset.mjs  re-subsets the Shippori Mincho woff2 to the Japanese
                 characters actually used in src/ — run after editing copy
```

All copy lives in `src/data/site.ts`: `site` (name, links, build commands),
`steps` (the four-stage pipeline), `stages` (the capability growth record with
seed/bud/grown states, mirroring onigami's TUI), and `states` (the legend).

## Placeholders to replace

These are marked `placeholder: true` in `src/data/site.ts` and should be confirmed
or swapped before launch:

- `url` — the production/canonical URL (`onigami.dev`), also used for OG and sitemap
- `github` / `clone` — the public repo URL
- the install/build commands shown in the Summon section

Also regenerate `public/og.png`, the favicons (`favicon.svg`, `favicon-32.png`,
`apple-touch-icon.png`) for the brand, and update the `site:` URL in
`astro.config.mjs` and `public/robots.txt` to match.

After changing any Japanese text in `src/`, run `node scripts/jp-subset.mjs` to keep
the subset font in sync (it needs network access to fetch from Google Fonts).
