# onigami website

Showcase site for **onigami**, an agentic red-team crew for your terminal. Built
with Astro, GSAP, Lenis and OGL (WebGL). The hero is a 3D "well" with a watching
eye, drawn live in terminal glyphs.

All visible copy lives in `src/data/site.ts`. The planning artifacts (concept,
palette, type, motion) are in `DESIGN.md`.

## Develop

```
npm install
npm run dev        # http://localhost:4321
npm run build      # static output in dist/
npm run preview
```

## Placeholders to replace

Marked `placeholder: true` in `src/data/site.ts`: the production URL, the GitHub
URL, the version, the license, and the install commands. Also regenerate
`public/og.png` and the favicons for the onigami brand, and the `site:` URL in
`astro.config.mjs` / `public/robots.txt`.
