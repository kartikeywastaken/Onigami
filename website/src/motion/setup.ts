// Global motion for onigami. Runs once on the client, before the first page initializes.
import { gsap } from 'gsap';
import { ScrollTrigger } from 'gsap/ScrollTrigger';
import {
  onPage, onScroll, setLoaderFormat, setLoaderExit, setTransition,
  ease, isMobile, isTouch, prefersReducedMotion,
} from '../engine';
import { initWell, state, drop, glReady, wellOn, redraw } from './well';

const RAMP = '.:-=+*#%@';

/** Characters climb the glyph ramp, then settle into the word, left to right.
 *  The scramble is drawn by a CSS pseudo-element with empty alt text, so the real text never
 *  changes: assistive tech, innerText and copy-to-clipboard always get the real words. */
const running = new WeakMap<HTMLElement, gsap.core.Tween>();
export function decode(el: HTMLElement, duration = 0.45) {
  const real = el.textContent || '';
  running.get(el)?.kill();
  const o = { p: 0 };
  const done = () => { el.classList.remove('is-decoding'); delete el.dataset.fx; running.delete(el); };
  el.classList.add('dc', 'is-decoding');
  const t = gsap.to(o, {
    p: 1, duration, ease: 'none',
    onUpdate: () => {
      const n = Math.floor(o.p * real.length);
      el.dataset.fx = real.slice(0, n) + [...real.slice(n)]
        .map((c, i) => (c === ' ' ? ' ' : RAMP[(i + Math.floor(o.p * 40)) % RAMP.length])).join('');
    },
    onComplete: done,
    onInterrupt: done,
  });
  running.set(el, t);
  return t;
}

type Mode = { water: number; eye: number; band: number; depth: number; shift: number; lift: number; dim: number };
const modes: Record<string, () => Mode> = {
  home: () => ({ water: 1, eye: 1, band: 0, depth: 0, shift: isMobile() ? 0 : 0.3, lift: 0, dim: 1 }),
  manual: () => ({ water: 1, eye: 1, band: 1, depth: 0.9, shift: isMobile() ? 0 : 0.18, lift: isMobile() ? 0.3 : 0.24, dim: 0.9 }),
  dry: () => ({ water: 0, eye: 0, band: 0, depth: 0.15, shift: isMobile() ? 0 : 0.3, lift: isMobile() ? 0.22 : 0, dim: isMobile() ? 0.55 : 0.8 }),
};

function applyMode() {
  const name = document.querySelector<HTMLElement>('main')?.dataset.well || 'home';
  Object.assign(state, (modes[name] || modes.home)());
  if (prefersReducedMotion()) redraw();
}

export function setupMotion() {
  const reduced = prefersReducedMotion();
  initWell(document.getElementById('gl'));

  // No loader on this page (manual, 404): the well drains as soon as its first frame is drawn.
  if (!document.getElementById('loader')) glReady().then(() => gsap.to(state, { flood: 0, duration: 1.6, ease: ease.inOut }));

  setLoaderFormat((p) => `${String(Math.round(p)).padStart(3, '0')}%`);
  setLoaderExit((root) => {
    const tl = gsap.timeline();
    tl.to(root.querySelectorAll('[data-loader-out]'), { yPercent: -120, duration: 0.45, ease: ease.inOut, stagger: 0.04 })
      .to(root, { autoAlpha: 0, duration: 0.35, ease: 'power2.out' }, '-=0.15')
      // The well was flooded under the loader; it drains as the hero surfaces.
      .add(() => { glReady().then(() => gsap.to(state, { flood: 0, duration: 1.5, ease: ease.inOut })); }, '-=0.3');
    return tl;
  });

  // Page transition: the well floods the screen with glyphs, the page swaps under it, it drains.
  setTransition({
    leave: (c) => {
      const html = document.documentElement;
      if (!wellOn()) {
        return gsap.timeline().set(c, { autoAlpha: 1 })
          .fromTo(c.querySelector('.curtain__panel'), { yPercent: 100 }, { yPercent: 0, duration: 0.8, ease: ease.inOut });
      }
      html.classList.add('is-submerged');
      return gsap.timeline()
        .to(state, { dim: 1, duration: 0.4 }, 0)
        .to(state, { flood: 1, duration: 0.85, ease: ease.inOut }, 0);
    },
    enter: (c) => {
      const html = document.documentElement;
      if (!wellOn()) {
        return gsap.timeline()
          .to(c.querySelector('.curtain__panel'), { yPercent: -100, duration: 0.9, ease: ease.inOut })
          .set(c, { autoAlpha: 0 });
      }
      html.classList.remove('is-submerged');
      gsap.to(state, { flood: 0, duration: 1.1, ease: ease.inOut });
      return gsap.timeline().to({}, { duration: 0.35 });
    },
  });

  // Fixed corners step aside while reading downwards, return on the way up.
  let last = 0;
  onScroll(({ y }) => {
    if (Math.abs(y - last) < 4) return;
    document.documentElement.classList.toggle('frame-away', y > 160 && y > last);
    last = y;
  });

  onPage(() => {
    applyMode();
    // Current page in the nav.
    document.querySelectorAll<HTMLAnchorElement>('[data-nav] a').forEach((a) => {
      const here = a.pathname === location.pathname && !a.hash;
      if (here) a.setAttribute('aria-current', 'page'); else a.removeAttribute('aria-current');
    });
  });

  // Hover and focus: decode on primary text targets.
  onPage(() => {
    if (reduced) return;
    const off: (() => void)[] = [];
    document.querySelectorAll<HTMLElement>('[data-decode]').forEach((host) => {
      const label = host.querySelector<HTMLElement>('[data-decode-text]') || host;
      const run = () => { if (isTouch()) return; decode(label); };
      const runFocus = () => { if (host.matches(':focus-visible')) decode(label); };
      const stop = () => running.get(label)?.progress(1);
      host.addEventListener('pointerenter', run);
      host.addEventListener('focus', runFocus);
      host.addEventListener('blur', stop);
      off.push(() => { stop(); host.removeEventListener('pointerenter', run); host.removeEventListener('focus', runFocus); host.removeEventListener('blur', stop); });
    });
    return () => off.forEach((f) => f());
  });

  // Copy buttons.
  onPage(() => {
    const off: (() => void)[] = [];
    document.querySelectorAll<HTMLButtonElement>('[data-copy]').forEach((btn) => {
      const src = document.getElementById(btn.dataset.copy || '');
      const label = btn.querySelector<HTMLElement>('[data-copy-label]');
      const click = async () => {
        const text = src?.textContent?.trim() || '';
        try { await navigator.clipboard.writeText(text); } catch { return; }
        if (label) { label.textContent = 'Copied'; setTimeout(() => (label.textContent = 'Copy'), 1600); }
        drop(0, 0, 1.2);
      };
      btn.addEventListener('click', click);
      off.push(() => btn.removeEventListener('click', click));
    });
    return () => off.forEach((f) => f());
  });

  // Install tabs (Homebrew / Script / Cargo).
  onPage(() => {
    const tabs = [...document.querySelectorAll<HTMLButtonElement>('[data-tab]')];
    if (!tabs.length) return;
    const out = document.getElementById('install-cmd');
    const select = (btn: HTMLButtonElement) => {
      tabs.forEach((t) => t.setAttribute('aria-selected', String(t === btn)));
      if (out) { out.textContent = btn.dataset.tab || ''; if (!reduced) decode(out, 0.5); }
    };
    const off = tabs.map((t) => {
      const fn = () => select(t);
      t.addEventListener('click', fn);
      return () => t.removeEventListener('click', fn);
    });
    return () => off.forEach((f) => f());
  });

  // HOME: the descent. Hero scroll drives depth; later sections dim the well; the footer lifts it.
  onPage(({ intro }) => {
    const hero = document.querySelector<HTMLElement>('[data-hero]');
    if (!hero) return;
    const readout = document.querySelector<HTMLElement>('[data-depth]');
    const base = state.shift;
    const write = (p: number) => {
      if (readout) readout.textContent = `−${(p * 4.6).toFixed(2)} m`;
    };
    write(0);
    if (reduced) return;
    intro.from('[data-hero-meta]', { autoAlpha: 0, y: 16, duration: 1, ease: ease.out, stagger: 0.08 }, 0.35);

    ScrollTrigger.create({
      trigger: hero, start: 'top top', end: 'bottom bottom', scrub: true,
      onUpdate: (s) => {
        const p = s.progress;
        state.depth = p;
        state.shift = base * (1 - gsap.parseEase('power2.inOut')(Math.min(p * 1.6, 1)));
        write(p);
      },
    });
    gsap.to('[data-hero-a]', {
      autoAlpha: 0, yPercent: -18, ease: 'none',
      scrollTrigger: { trigger: hero, start: 'top top', end: '28% top', scrub: true },
    });
    gsap.fromTo('[data-hero-b]', { autoAlpha: 0, y: 40 }, {
      autoAlpha: 1, y: 0, ease: 'none',
      scrollTrigger: { trigger: hero, start: '55% bottom', end: '80% bottom', scrub: true },
    });
    gsap.fromTo(state, { dim: 1 }, {
      dim: 0.26, ease: 'none', immediateRender: false,
      scrollTrigger: { trigger: '[data-after-hero]', start: 'top 85%', end: 'top 25%', scrub: true },
    });
    gsap.fromTo(state, { dim: 0.26 }, {
      dim: 0.6, ease: 'none', immediateRender: false,
      scrollTrigger: { trigger: '[data-finale]', start: 'top bottom', end: 'bottom bottom', scrub: true },
    });
  });

  // HOME: the session. Each chapter types its output and drops a stone into the well.
  onPage(() => {
    const chapters = [...document.querySelectorAll<HTMLElement>('[data-chapter]')];
    const outs = [...document.querySelectorAll<HTMLElement>('[data-out]')];
    if (!chapters.length || !outs.length) return;
    const tab = document.querySelector<HTMLElement>('[data-session-name]');
    let active = -1;
    const show = (i: number) => {
      if (i === active) return;
      active = i;
      chapters.forEach((c, j) => c.classList.toggle('is-active', j === i));
      outs.forEach((o, j) => {
        const on = j === i;
        o.hidden = !on;
        if (!on || reduced) return;
        const cmd = o.querySelector<HTMLElement>('.ln--cmd .ln__t');
        if (cmd) decode(cmd, 0.6);
        gsap.fromTo(o.querySelectorAll('.ln:not(.ln--cmd)'), { autoAlpha: 0, y: 6 }, {
          autoAlpha: 1, y: 0, duration: 0.5, ease: ease.soft, stagger: 0.07, delay: 0.45,
        });
      });
      if (tab) tab.textContent = `onigami ${chapters[i].dataset.chapter}`;
      drop(undefined, undefined, 1);
    };
    show(0);
    if (reduced) {
      chapters.forEach((c, i) => ScrollTrigger.create({ trigger: c, start: 'top 60%', end: 'bottom 60%', onToggle: (s) => s.isActive && show(i) }));
      return;
    }
    chapters.forEach((c, i) => ScrollTrigger.create({
      trigger: c, start: 'top 60%', end: 'bottom 60%',
      onToggle: (s) => s.isActive && show(i),
    }));
  });

  // HOME: command index rows drop a small stone where you point.
  onPage(() => {
    const rows = [...document.querySelectorAll<HTMLElement>('[data-ripple]')];
    if (!rows.length || reduced) return;
    const off = rows.map((r) => {
      const fn = () => drop(undefined, undefined, 0.6);
      r.addEventListener('pointerenter', fn);
      return () => r.removeEventListener('pointerenter', fn);
    });
    return () => off.forEach((f) => f());
  });

  // MANUAL: the well shows in the top band, then sinks away as you read.
  onPage(() => {
    const head = document.querySelector('.man__head');
    if (!head || reduced) return;
    gsap.fromTo(state, { dim: 0.9 }, {
      dim: 0, ease: 'none', immediateRender: false,
      scrollTrigger: { trigger: head, start: 'top top', end: 'bottom top', scrub: true },
    });
  });

  // MANUAL: the index follows the reader.
  onPage(() => {
    const links = [...document.querySelectorAll<HTMLAnchorElement>('[data-toc] a')];
    if (!links.length) return;
    links.forEach((a) => {
      const sec = document.getElementById(a.hash.slice(1));
      if (!sec) return;
      ScrollTrigger.create({
        trigger: sec, start: 'top 40%', end: 'bottom 40%',
        onToggle: (s) => {
          if (!s.isActive) return;
          links.forEach((l) => l.removeAttribute('aria-current'));
          a.setAttribute('aria-current', 'location');
        },
      });
    });
    // At the very bottom the last section is the one being read, even if it is short.
    const last = links[links.length - 1];
    ScrollTrigger.create({
      trigger: document.body, start: 'bottom-=2 bottom', end: 'bottom bottom',
      onEnter: () => { links.forEach((l) => l.removeAttribute('aria-current')); last.setAttribute('aria-current', 'location'); },
    });
  });
}
