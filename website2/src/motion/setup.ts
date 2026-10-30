// Global motion for Onigami. Runs once on the client, before the first page initializes.
// One clock: every garden is ticked from gsap.ticker, and only while it is on screen.
import { gsap } from 'gsap';
import { ScrollTrigger } from 'gsap/ScrollTrigger';
import { navigate } from 'astro:transitions/client';
import { onPage, onScroll, setTransition, ease, isTouch, prefersReducedMotion, scrollToTarget } from '../engine';
import { Garden, PRESETS } from '../garden/core.js';
import { run } from '../garden/shell';
import { initPalette, applyPalette, palette, setPalette, onPalette } from './theme';

type G = InstanceType<typeof Garden>;

/** Ticks a garden from the one clock while its element is on screen. */
function drive(garden: G, el: Element, still = false, every = 1) {
  let on = false, n = 0;
  const tick = () => { if (++n % every === 0) garden.tick(); };
  const io = new IntersectionObserver(([e]) => {
    if (still) { if (e.isIntersecting) garden.tick(); return; }
    if (e.isIntersecting && !on) { gsap.ticker.add(tick); on = true; }
    else if (!e.isIntersecting && on) { gsap.ticker.remove(tick); on = false; }
  });
  io.observe(el);
  return () => { io.disconnect(); gsap.ticker.remove(tick); garden.destroy(); };
}

/** Draws a poster once, at a moment where everything has grown (reduced motion). */
function stillPoster(g: G) {
  const P = g.P; if (!P) return;
  g.pt0 = performance.now() - P.T * 0.62;
  g.tick();
}

export function setupMotion() {
  const reduced = prefersReducedMotion();
  initPalette();
  document.addEventListener('astro:after-swap', () => applyPalette(palette()));

  // Page transition: a flower-red curtain grows up from the baseline carrying 鬼 (the oni cuts the
  // page away); the new page arrives under 神 (the kami grows it back), then the curtain withers upward.
  setTransition({
    leave: (c) => {
      const panel = c.querySelector<HTMLElement>('.curtain__panel')!;
      const glyph = c.querySelector<HTMLElement>('[data-curtain-glyph]')!;
      const kana = c.querySelector<HTMLElement>('[data-curtain-kana]')!;
      glyph.textContent = '鬼'; kana.textContent = 'おに';
      return gsap.timeline()
        .set(c, { autoAlpha: 1 })
        .fromTo(panel, { clipPath: 'inset(100% 0% 0% 0%)' }, { clipPath: 'inset(0% 0% 0% 0%)', duration: 0.6, ease: ease.inOut })
        .fromTo(glyph, { yPercent: 60, scale: 0.6, autoAlpha: 0 }, { yPercent: 0, scale: 1, autoAlpha: 1, duration: 0.55, ease: ease.spring }, 0.22)
        .fromTo(kana, { autoAlpha: 0, y: 12 }, { autoAlpha: 1, y: 0, duration: 0.35, ease: ease.soft }, 0.32);
    },
    enter: (c) => {
      const panel = c.querySelector<HTMLElement>('.curtain__panel')!;
      const glyph = c.querySelector<HTMLElement>('[data-curtain-glyph]')!;
      const kana = c.querySelector<HTMLElement>('[data-curtain-kana]')!;
      // resolves as the curtain starts to lift, so the new page's intro plays while it withers away
      return new Promise<void>((resolve) => {
        gsap.timeline()
          .to([glyph, kana], { autoAlpha: 0, scale: 0.85, duration: 0.14, ease: 'power2.in' })
          .add(() => { glyph.textContent = '神'; kana.textContent = 'かみ'; })
          .to([glyph, kana], { autoAlpha: 1, scale: 1, duration: 0.35, ease: ease.spring })
          .add(resolve, '+=0.02')
          .to(panel, { clipPath: 'inset(0% 0% 100% 0%)', duration: 0.65, ease: ease.inOut }, '<')
          .to(glyph, { yPercent: -40, autoAlpha: 0, duration: 0.4, ease: ease.inOut }, '<')
          .set(c, { autoAlpha: 0 });
      });
    },
  });

  // Fixed corners step aside while reading downwards, return on the way up.
  let last = 0;
  onScroll(({ y }) => {
    if (Math.abs(y - last) < 4) return;
    document.documentElement.classList.toggle('frame-away', y > 160 && y > last);
    last = y;
  });

  // Current page in the nav.
  onPage(() => {
    document.querySelectorAll<HTMLAnchorElement>('[data-nav] a').forEach((a) => {
      const here = a.pathname === location.pathname && !a.hash && a.origin === location.origin;
      if (here) a.setAttribute('aria-current', 'page'); else a.removeAttribute('aria-current');
    });
  });

  // Same-page hash links (e.g. the "/#growth" Roadmap link): Lenis only hijacks
  // bare "#id" links, so route "/path#id" through Lenis ourselves when the path is
  // already the current one; otherwise let the browser navigate as usual.
  onPage(() => {
    const onClick = (e: MouseEvent) => {
      if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
      const a = (e.target as Element | null)?.closest<HTMLAnchorElement>('a[href]');
      if (!a || a.origin !== location.origin || !a.hash || a.pathname !== location.pathname) return;
      const el = document.querySelector(a.hash);
      if (!el) return;
      e.preventDefault();
      scrollToTarget(el as HTMLElement);
      history.pushState(null, '', a.hash);
    };
    document.addEventListener('click', onClick);
    return () => document.removeEventListener('click', onClick);
  });

  // Palette swatches: any [data-swatch] on any page. The new colours bloom from the swatch.
  onPage(() => {
    const sw = [...document.querySelectorAll<HTMLButtonElement>('[data-swatch]')];
    const sync = (i: number) => sw.forEach((b) => b.setAttribute('aria-pressed', String(Number(b.dataset.swatch) === i)));
    sync(palette());
    const off = sw.map((b) => {
      const fn = () => { const r = b.getBoundingClientRect(); setPalette(Number(b.dataset.swatch), { x: r.left + r.width / 2, y: r.top + r.height / 2 }); };
      b.addEventListener('click', fn);
      return () => b.removeEventListener('click', fn);
    });
    const unsub = onPalette(sync);
    return () => { off.forEach((f) => f()); unsub(); };
  });

  // Copy buttons.
  onPage(() => {
    const off: (() => void)[] = [];
    document.querySelectorAll<HTMLButtonElement>('[data-copy]').forEach((btn) => {
      const src = document.getElementById(btn.dataset.copy || '');
      const label = btn.querySelector<HTMLElement>('[data-copy-label]');
      const click = async () => {
        const text = (src?.textContent || '').split('\n').map((l) => l.replace(/^\$\s?/, '')).join('\n').trim();
        try { await navigator.clipboard.writeText(text); } catch { return; }
        if (label) { label.textContent = 'Copied'; setTimeout(() => (label.textContent = 'Copy'), 1600); }
      };
      btn.addEventListener('click', click);
      off.push(() => btn.removeEventListener('click', click));
    });
    return () => off.forEach((f) => f());
  });

  // HOME: the prompt. Type, space cuts, backspace withers, enter runs the toy shell.
  onPage(({ intro }) => {
    const hero = document.querySelector<HTMLElement>('[data-hero]');
    const canvas = document.querySelector<HTMLCanvasElement>('[data-garden]');
    const input = document.querySelector<HTMLInputElement>('[data-garden-input]');
    const out = document.querySelector<HTMLElement>('[data-garden-out]');
    if (!hero || !canvas || !input) return;
    const phone = matchMedia('(max-width: 767px)').matches;
    let busy = false, pending: string | null = null;

    const print = (line: string, lines: string[], err?: boolean) => {
      if (!out) return;
      out.replaceChildren();
      const cmd = document.createElement('span');
      cmd.className = 'out__cmd';
      cmd.textContent = `$ ${line}`;
      out.append(cmd);
      lines.forEach((t) => { const s = document.createElement('span'); s.className = err ? 'out__ln out__ln--err' : 'out__ln'; s.textContent = t; out.append(s); });
      if (!reduced) gsap.fromTo(out.children, { yPercent: 100, autoAlpha: 0 }, { yPercent: 0, autoAlpha: 1, duration: 0.6, ease: ease.out, stagger: 0.06 });
    };

    const garden: G = new Garden({
      canvas, input, mode: 'type', transparent: true, reduced, palette, petals: true, wind: true,
      layout: phone ? { cy: 0.46, s0: 0.2, maxW: 0.74 } : { cy: 0.43, s0: 0.25, maxW: 0.74 },
      onChange: (t: string) => hero.classList.toggle('is-empty', !t.trim()),
      onRun: async (line: string) => {
        // a line entered while the last one is still being eaten waits its turn
        if (busy) { pending = line; return; }
        const trimmed = line.trim();
        if (!trimmed) { garden.witherAll(); return; }
        busy = true;
        const r = run(trimmed);
        if (r.clear) out?.replaceChildren(); else print(trimmed, r.out, r.err);
        // every run shakes petals loose; a command that fails is eaten by the oni
        garden.scatter(r.err ? 1 : 4);
        await (r.err ? garden.devour() : garden.witherAll(r.exit ? 70 : 26));
        busy = false;
        if (pending != null) { const next = pending; pending = null; garden.o.onRun(next); return; }
        if (r.go) { navigate(r.go); return; }
        if (r.replant) garden.typeOut(r.replant, 55);
        if (r.exit) { input.blur(); gsap.delayedCall(1.8, () => garden.typeOut('onigami')); }
      },
    });

    // Pointer: a click on the garden starts typing. On touch, the tap's click opens the keyboard.
    const focus = (e: PointerEvent) => { if (e.pointerType === 'mouse') { e.preventDefault(); input.focus({ preventScroll: true }); } };
    const tap = () => { if (isTouch()) input.focus({ preventScroll: true }); };
    canvas.addEventListener('pointerdown', focus);
    canvas.addEventListener('click', tap);
    const typing = () => hero.classList.add('is-typing');
    const idle = () => hero.classList.remove('is-typing');
    input.addEventListener('focus', typing);
    input.addEventListener('blur', idle);

    const stop = drive(garden, hero);
    // The intro: the shell types its own name once the letters' font is ready.
    const fontReady = document.fonts ? document.fonts.load('700 100px "Playfair Display"').catch(() => {}) : Promise.resolve();
    let alive = true;
    if (reduced) fontReady.then(() => alive && garden.typeOut('onigami'));
    else intro.add(() => { fontReady.then(() => alive && gsap.delayedCall(0.25, () => alive && garden.typeOut('onigami', 95))); }, 0.1);
    if (!reduced) intro.from('[data-hero-ui] > *', { autoAlpha: 0, y: 18, duration: 1, ease: ease.out, stagger: 0.08 }, 0.5);

    return () => {
      alive = false; stop();
      canvas.removeEventListener('pointerdown', focus); canvas.removeEventListener('click', tap);
      input.removeEventListener('focus', typing); input.removeEventListener('blur', idle);
    };
  });

  // HOME, phone: every step grows its own small garden inline.
  onPage(() => {
    if (!matchMedia('(max-width: 767px)').matches) return;
    const stops = [...document.querySelectorAll<HTMLElement>('[data-step]')].map((el) => {
      const canvas = el.querySelector<HTMLCanvasElement>('[data-step-canvas]');
      if (!canvas) return () => {};
      const s = el.dataset;
      const g: G = new Garden({ canvas, mode: 'poster', transparent: true, reduced, palette });
      g.setPoster({ text: s.text, preset: s.preset, seed: Number(s.seed) });
      return drive(g, canvas, reduced, 2);
    });
    return () => stops.forEach((f) => f());
  });

  // HOME, desktop: the loop. One sticky poster changes word and motion as each step comes into view.
  onPage(() => {
    if (matchMedia('(max-width: 767px)').matches) return;
    const canvas = document.querySelector<HTMLCanvasElement>('[data-loop-canvas]');
    const items = [...document.querySelectorAll<HTMLElement>('[data-step]')];
    if (!canvas || !items.length) return;
    const cap = document.querySelector<HTMLElement>('[data-loop-cap]');
    const capJp = document.querySelector<HTMLElement>('[data-loop-jp]');
    const g: G = new Garden({ canvas, mode: 'poster', transparent: true, reduced, palette });
    const stop = drive(g, canvas, reduced, matchMedia('(max-width: 767px)').matches ? 2 : 1);
    const unsub = reduced ? onPalette(() => g.tick()) : () => {};
    let active = -1;
    const show = (i: number) => {
      if (i === active) return;
      active = i;
      items.forEach((el, j) => el.classList.toggle('is-active', j === i));
      const s = items[i].dataset;
      const set = () => {
        g.setPoster({ text: s.text, preset: s.preset, seed: Number(s.seed) });
        if (cap) { const a = document.createElement('span'), b = document.createElement('code'); a.textContent = `${PRESETS.find((p) => p.id === s.preset)?.label.toLowerCase()} ·`; b.textContent = s.text || ''; cap.replaceChildren(a, ' ', b); }
        if (capJp) capJp.textContent = s.jp || '庭';
        if (reduced) stillPoster(g);
      };
      if (reduced) return set();
      gsap.timeline()
        .to(canvas, { autoAlpha: 0, scale: 0.97, duration: 0.25, ease: 'power2.in' })
        .add(set)
        .to(canvas, { autoAlpha: 1, scale: 1, duration: 0.6, ease: ease.out });
    };
    show(0);
    items.forEach((el, i) => ScrollTrigger.create({ trigger: el, start: 'top 62%', end: 'bottom 62%', onToggle: (st) => st.isActive && show(i) }));
    return () => { stop(); unsub(); };
  });

  // HOME: growth record. The meter grows to the number of passing stages.
  onPage(() => {
    const meter = document.querySelector<HTMLElement>('[data-meter]');
    if (!meter || reduced) return;
    gsap.from(meter, { scaleX: 0, transformOrigin: '0% 50%', duration: 1.6, ease: ease.out, scrollTrigger: { trigger: meter, start: 'top 85%', once: true } });
  });

  // STUDIO: the poster maker.
  onPage(() => {
    const canvas = document.querySelector<HTMLCanvasElement>('[data-studio-canvas]');
    if (!canvas) return;
    const text = document.querySelector<HTMLInputElement>('[data-studio-text]');
    const status = document.querySelector<HTMLElement>('[data-status]');
    const info = document.querySelector<HTMLElement>('[data-studio-info]');
    const g: G = new Garden({
      canvas, mode: 'poster', reduced, palette,
      onStatus: (msg: string, busy: boolean) => {
        if (status) status.textContent = msg;
        document.querySelectorAll<HTMLButtonElement>('[data-export]').forEach((b) => (b.disabled = busy));
      },
    });
    const vbtn = document.querySelector<HTMLButtonElement>('[data-export="video"] [data-copy-label], [data-export="video"] .btn__label');
    if (vbtn) vbtn.textContent = g.vmime ? `Export ${g.vmime.includes('mp4') ? 'MP4' : 'WebM'} loop` : 'Video export unavailable';
    const presets = [...document.querySelectorAll<HTMLInputElement>('[data-preset]')];
    const sync = () => {
      const pr = PRESETS.find((p) => p.id === g.poster.preset)!;
      if (info) info.textContent = `1:1 · ${pr.T / 1000}s loop · 30 fps`;
      if (reduced) stillPoster(g);
    };
    g.setPoster({ text: text?.value || 'in bloom', preset: presets.find((p) => p.checked)?.value || 'breathe', seed: 7 });
    sync();
    const stop = drive(g, canvas, reduced);
    const handlers: [EventTarget, string, EventListener][] = [];
    const on = (t: EventTarget | null, ev: string, fn: EventListener) => { if (t) { t.addEventListener(ev, fn); handlers.push([t, ev, fn]); } };
    on(text, 'input', () => { g.setPoster({ text: text!.value }); sync(); });
    on(document.querySelector('[data-regrow]'), 'click', () => { g.setPoster({ seed: Math.floor(Math.random() * 1e6) }); sync(); });
    presets.forEach((p) => on(p, 'change', () => { if (p.checked) { g.setPoster({ preset: p.value }); sync(); } }));
    document.querySelectorAll<HTMLButtonElement>('[data-export]').forEach((b) => on(b, 'click', () => {
      const k = b.dataset.export;
      if (k === 'video') g.exportVideo(); else if (k === 'frames') g.exportFrames(); else if (k === 'png') g.savePNG(); else if (k === 'svg') g.saveSVG();
    }));
    const unsub = reduced ? onPalette(() => g.tick()) : () => {};
    return () => { stop(); unsub(); handlers.forEach(([t, ev, fn]) => t.removeEventListener(ev, fn)); };
  });

  // 404: the missing path grows, then withers, on a loop.
  onPage(() => {
    const canvas = document.querySelector<HTMLCanvasElement>('[data-lost-canvas]');
    if (!canvas) return;
    const g: G = new Garden({ canvas, mode: 'poster', transparent: true, reduced, palette });
    g.setPoster({ text: '404', preset: 'grow', seed: 3 });
    if (reduced) stillPoster(g);
    const stop = drive(g, canvas, reduced);
    const unsub = reduced ? onPalette(() => stillPoster(g)) : () => {};
    const p = document.querySelector('[data-path]');
    if (p) p.textContent = location.pathname;
    return () => { stop(); unsub(); };
  });
}
