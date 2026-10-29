// The garden palette themes the whole site. One index, stored per visitor, written to <html>
// as CSS variables so every text colour follows (DESIGN.md: Palette).
import { PAL } from '../garden/core.js';

const KEY = 'onigami:pal';
let current = 0;
const subs = new Set<(i: number) => void>();

const hexA = (hex: string, a: number) => {
  const n = parseInt(hex.slice(1), 16);
  return `rgb(${(n >> 16) & 255} ${(n >> 8) & 255} ${n & 255} / ${a})`;
};

const lum = (hex: string) => {
  const n = parseInt(hex.slice(1), 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255]
    .map((v) => { v /= 255; return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4; })
    .reduce((a, v, i) => a + v * [0.2126, 0.7152, 0.0722][i], 0);
};
/** Black or white, whichever reads better on the flower colour (button labels on the red fill). */
const onColor = (hex: string) => ((lum(hex) + 0.05) / 0.05 > 1.05 / (lum(hex) + 0.05) ? '#000000' : '#FFFFFF');

export function applyPalette(i: number) {
  const p = PAL[i] || PAL[0];
  const s = document.documentElement.style;
  s.setProperty('--c-bg', p[3]);
  s.setProperty('--c-flower', p[4]);
  s.setProperty('--c-on-flower', onColor(p[4]));
  s.setProperty('--c-stem', p[5]);
  s.setProperty('--c-petal', p[6]);
  s.setProperty('--c-fg', p[7]);
  s.setProperty('--c-muted', p[8]);
  s.setProperty('--c-line', hexA(p[7], 0.16));
  s.setProperty('--c-veil', hexA(p[3], 0.86));
  document.documentElement.dataset.pal = p[0];
  // the head script reads this back before first paint, so a light palette never flashes black
  try { localStorage.setItem(KEY + ':css', s.cssText); } catch { /* storage blocked */ }
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', p[3]);
}

export function initPalette() {
  try { const v = parseInt(localStorage.getItem(KEY) || '0', 10); if (PAL[v]) current = v; } catch { /* storage blocked */ }
  applyPalette(current);
}

export const palette = () => current;
export const onPalette = (fn: (i: number) => void) => { subs.add(fn); return () => subs.delete(fn); };

/** Switches palette. The new colours bloom out of the swatch that was pressed. */
export function setPalette(i: number, from?: { x: number; y: number }) {
  if (i === current || !PAL[i]) return;
  const commit = () => {
    current = i;
    try { localStorage.setItem(KEY, String(i)); } catch { /* storage blocked */ }
    applyPalette(i);
    subs.forEach((f) => f(i));
  };
  const doc = document as Document & { startViewTransition?: (cb: () => void) => { ready: Promise<void> } };
  const reduced = matchMedia('(prefers-reduced-motion: reduce)').matches;
  if (!doc.startViewTransition || reduced || !from) return commit();
  const r = Math.hypot(Math.max(from.x, innerWidth - from.x), Math.max(from.y, innerHeight - from.y));
  document.documentElement.classList.add('is-repainting');
  const vt = doc.startViewTransition(commit);
  vt.ready.then(() => {
    document.documentElement.animate(
      { clipPath: [`circle(0px at ${from.x}px ${from.y}px)`, `circle(${r}px at ${from.x}px ${from.y}px)`] },
      { duration: 900, easing: 'cubic-bezier(0.16, 1, 0.3, 1)', pseudoElement: '::view-transition-new(root)' },
    ).finished.finally(() => document.documentElement.classList.remove('is-repainting'));
  }).catch(() => document.documentElement.classList.remove('is-repainting'));
}
