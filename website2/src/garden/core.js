// Type Garden, ported from Akshat Agarwal's original (type-garden.vercel.app) to plain JS.
// Drawing and motion are kept as close to the original as possible. Differences:
//  - no React, no own requestAnimationFrame: call garden.tick() from one clock (gsap.ticker)
//  - one instance is either a typing garden ('type') or a looping poster ('poster')
//  - the multiplayer layer (Supabase cursors) is left out; double-click roses and butterflies stay
//  - Enter calls opts.onRun(line) instead of clearing, so a shell can answer it
//  - letters are set in Playfair Display Bold (the original uses GT Ultra Fine)

export const FONT = '"Playfair Display","Shippori Mincho B1","Hiragino Mincho ProN","Yu Mincho",Georgia,serif';

export const PRESETS = [
  { id: 'breathe', label: 'Breathe', jp: '息', sub: 'Slow sway · 3s', T: 3000 },
  { id: 'grow', label: 'Grow & wither', jp: '枯', sub: 'Bloom, then back · 6s', T: 6000 },
  { id: 'typed', label: 'Typed', jp: '打', sub: 'Type, cut, repeat · 5s', T: 5000 },
  { id: 'wind', label: 'Gust', jp: '風', sub: 'Wind sweeps through · 4s', T: 4000 },
  { id: 'reach', label: 'Reach', jp: '光', sub: 'Stems follow a light · 6s', T: 6000 },
  { id: 'scatter', label: 'Scatter', jp: '散', sub: 'Random bloom, fade · 6s', T: 6000 },
  { id: 'visit', label: 'Visitor', jp: '蝶', sub: 'Butterfly drops by · 7s', T: 7000 },
];

// id, name, Japanese name, background, flower, stem & leaf, flower line, type, muted (UI text)
export const PAL = [
  ['shrine', 'Shrine', '朱塗', '#0D0A09', '#F2452D', '#C9A24B', '#FFD9B8', '#F4EDE1', '#A9A196'],
  ['onigami', 'Onigami', '鬼神', '#000000', '#FF1400', '#3257FF', '#FFB4A8', '#FFFFFF', '#A8A8A8'],
  ['washi', 'Paper', '和紙', '#F3EEE4', '#FF3B1F', '#1C2B8F', '#FFD2C4', '#111111', '#55524C'],
  ['yoru', 'Midnight', '真夜中', '#0D1B4C', '#FF5A4E', '#9FB4FF', '#FFD6CF', '#FFFFFF', '#B4BCD6'],
  ['yuzu', 'Citrus', '柚子', '#0E1A12', '#FF8A00', '#1FA463', '#FFE2B8', '#FFF6E8', '#B3AD9F'],
  ['ran', 'Orchid', '蘭', '#140A1F', '#E63CFF', '#2FB8A6', '#F9D1FF', '#FFFFFF', '#B3ABBB'],
  ['koke', 'Moss', '苔', '#133A2A', '#FFB7C5', '#7FD18B', '#FFFFFF', '#F6F1E7', '#BFC4B9'],
  ['akaoni', 'Red oni', '赤鬼', '#FF1400', '#FFF1E0', '#0B0B0B', '#FF1400', '#000000', '#000000'],
  ['nanohana', 'Butter', '菜の花', '#FFE9A8', '#E4002B', '#0F5132', '#FFC2C2', '#1A1A1A', '#4F4732'],
  ['sakura', 'Blush', '桜', '#FAD4D8', '#C8102E', '#274690', '#FFE3E6', '#1A1A1A', '#4D3D40'],
  ['sumi', 'Mono', '墨', '#000000', '#F2F2F2', '#6E6E6E', '#000000', '#FFFFFF', '#A8A8A8'],
];

export const palColors = (i) => {
  const p = PAL[i] || PAL[0];
  return { bg: p[3], C: { red: p[4], blue: p[5], line: p[6], text: p[7], vein: p[3] } };
};

// one sakura petal along +x, length 1: rounded, with the notch at the tip
const SAKURA = [[0, 0], [0.22, 0.19], [0.52, 0.31], [0.84, 0.28], [1.0, 0.14], [0.88, 0.0], [1.0, -0.14], [0.84, -0.28], [0.52, -0.31], [0.22, -0.19]];

const SENT = String.fromCharCode(0x200b); // kept in the hidden field so Backspace on an empty field still reports

export class Garden {
  constructor(opts) {
    this.o = Object.assign({ mode: 'type', density: 1, boil: true, recoil: 20, transparent: false, reduced: false,
      layout: {}, palette: () => 0, onRun: null, onStatus: null, onChange: null, flies: true,
      flower: 'sakura', petals: false, wind: false }, opts);
    this.fallen = []; this.nextGust = performance.now() + 6000;
    this.cv = this.o.canvas;
    this.g = this.cv.getContext('2d');
    this.inp = this.o.input || null;
    this.mode = this.o.mode;
    this.F = FONT;
    this.letters = []; this.uid = 1; this.cache = {}; this.Sd = 0; this.St = 0; this.wrapped = false;
    this.t0 = performance.now(); this.lastKey = 0; this.rand = Math.random;
    this.fa = {}; this.mx = null; this.flies = []; this.perch = []; this.gifts = []; this.peers = new Map();
    this.poster = { text: 'in bloom', preset: 'breathe', seed: 7 };
    this.busy = false;
    this.off = [];
    this.on(window, 'resize', () => this.resize());
    this.resize();
    if (this.mode === 'type') this.bindType();
    if (document.fonts) document.fonts.load(`700 100px ${this.F}`).then(() => { this.cache = {}; this.layout(true); if (this.mode === 'poster') this.buildPoster(); }).catch(() => {});
    this.vmime = typeof MediaRecorder !== 'undefined' ? ['video/mp4;codecs=avc1.42E01E', 'video/mp4', 'video/webm;codecs=vp9', 'video/webm'].find((m) => MediaRecorder.isTypeSupported(m)) : null;
    if (this.mode === 'poster') this.buildPoster();
  }
  on(t, ev, fn, o) { t.addEventListener(ev, fn, o); this.off.push(() => t.removeEventListener(ev, fn, o)); }
  destroy() { this.off.splice(0).forEach((f) => f()); }

  bindType() {
    const c = this.cv;
    this.on(window, 'pointermove', (e) => { this.cx_ = e.clientX; this.cy_ = e.clientY; this.mIn = true; });
    this.on(document, 'mouseout', (e) => { if (!e.relatedTarget) this.mIn = false; });
    this.on(window, 'pointerup', (e) => { if (e.pointerType && e.pointerType !== 'mouse') this.mIn = false; }); // roses relax when the finger lifts
    this.on(window, 'pointercancel', () => { this.mIn = false; });
    // a click on the garden sends a butterfly to the nearest rose; a double click leaves a rose
    this.on(c, 'pointerdown', (e) => {
      const r = c.getBoundingClientRect(), x = e.clientX - r.left, y = e.clientY - r.top;
      if (this.o.flies && !this.o.reduced) this.spawnFly(x, y);
      const now = performance.now(), lp = this.lastTap;
      this.lastTap = { t: now, x, y };
      if (lp && now - lp.t < 340 && Math.hypot(x - lp.x, y - lp.y) < 40 && now - (this.lastPlant || 0) > 1800) {
        this.lastPlant = now; this.lastTap = null;
        this.plantRose({ x: x / this.W, y: y / this.H, s: Math.floor(Math.random() * 1e6) });
      }
    });
    const fld = this.inp; if (!fld) return;
    this.iv = SENT; fld.value = SENT;
    this.on(fld, 'keydown', (e) => this.key(e));
    this.on(fld, 'input', () => this.inputDiff(fld));
    this.on(fld, 'compositionstart', () => { this.composing = true; });
    this.on(fld, 'compositionend', () => { this.composing = false; setTimeout(() => { if (!this.composing) this.inputDiff(fld); }, 0); });
  }

  spawnFly(cx, cy) {
    if (this.flies.length >= 6) { const o = this.flies.find((f) => f.st !== 'leave'); if (o) this.flyOff(o); }
    const taken = new Set(this.flies.filter((f) => f.st !== 'leave').map((f) => f.pid));
    let best = null, bd = 1e9;
    for (const p of this.perch) { if (taken.has(p.id)) continue; const d = Math.hypot(p.x - cx, p.y - cy); if (d < bd) { bd = d; best = p; } }
    const left = cx < this.W / 2, sz = Math.max(9, this.Sd * 0.09);
    this.flies.push({ x: left ? -40 : this.W + 40, y: cy + (Math.random() - 0.5) * this.H * 0.4, vx: 0, vy: 0,
      st: best ? 'fly' : 'wander', pid: best ? best.id : null, hx: cx, hy: cy, t0: performance.now(), ph: Math.random() * 6, sz, seed: Math.random() * 100, ang: 0 });
  }
  flyOff(f) { f.st = 'leave'; f.pid = null; f.ex = f.x < this.W / 2 ? -80 : this.W + 80; f.ey = f.y - this.H * 0.3; }
  drawFlies(B, now, C) {
    const dt = Math.min(48, now - (this._fl || now)); this._fl = now;
    const byId = {}; for (const p of this.perch) byId[p.id] = p;
    this.flies = this.flies.filter((f) => {
      const t = (now - f.t0) / 1000;
      let tx, ty;
      if (f.st === 'sit' || f.st === 'fly') {
        const p = byId[f.pid];
        if (!p) { this.flyOff(f); }
        else { tx = p.x + p.R * 0.05; ty = p.y - p.R * 0.55; }
      }
      if (f.st === 'wander') {
        tx = f.hx + Math.sin(t * 1.3 + f.seed) * 70; ty = f.hy + Math.sin(t * 2.1 + f.seed) * 40;
        if (t > 3.5) this.flyOff(f);
      }
      if (f.st === 'leave') { tx = f.ex; ty = f.ey; }
      if (f.st === 'sit') {
        f.x += (tx - f.x) * 0.35; f.y += (ty - f.y) * 0.35; f.ang *= 0.85;
        f.ph += dt * 0.004;
        const burst = Math.sin(now / 1000 * 0.9 + f.seed) > 0.75;
        f.open = burst ? 0.2 + 0.8 * Math.abs(Math.cos(now / 1000 * 9 + f.seed)) : 0.25 + 0.15 * Math.sin(f.ph);
      } else {
        const dx = tx - f.x, dy = ty - f.y, d = Math.hypot(dx, dy) || 1, sp = f.st === 'leave' ? 0.42 : Math.min(0.34, 0.06 + d * 0.0016);
        const flut = Math.sin(t * 7 + f.seed) * 0.12, wob = Math.cos(t * 4.3 + f.seed) * 0.1;
        const ax = (dx / d * sp + wob) - f.vx, ay = (dy / d * sp + flut) - f.vy;
        f.vx += ax * 0.06 * dt / 16; f.vy += ay * 0.06 * dt / 16;
        f.x += f.vx * dt; f.y += f.vy * dt + Math.sin(t * 13 + f.seed) * 0.6;
        f.ang += (Math.max(-0.5, Math.min(0.5, f.vx * 1.4)) - f.ang) * 0.1;
        f.open = Math.abs(Math.cos(t * 17 + f.seed));
        if (f.st === 'fly' && d < 5) { f.st = 'sit'; f.ph = 0; }
        if (f.st === 'leave' && (f.x < -60 || f.x > this.W + 60 || f.y < -60)) return false;
      }
      this.butterfly(B, f.x, f.y, f.sz, f.ang, f.open, C);
      return true;
    });
  }
  butterfly(B, x, y, s, ang, o, C) {
    const ca = Math.cos(ang), sa = Math.sin(ang), T = (px, py) => [x + (px * ca - py * sa) * s, y + (px * sa + py * ca) * s];
    const wing = (cx, cy, rx, ry, a, side) => {
      const pts = [], c = Math.cos(a), sn = Math.sin(a);
      for (let k = 0; k < 22; k++) { const th = k / 22 * Math.PI * 2, r = 1 + 0.08 * Math.sin(th * 3), lx = Math.cos(th) * rx * r, ly = Math.sin(th) * ry * r; pts.push(T(side * (cx + lx * c - ly * sn) * (0.12 + 0.88 * o), cy + lx * sn + ly * c)); }
      return pts;
    };
    for (const sd of [-1, 1]) {
      B.fill(wing(0.46, 0.28, 0.36, 0.3, 0.5 * sd * sd, sd), C.text);
      B.fill(wing(0.55, -0.32, 0.55, 0.36, -0.55, sd), C.text);
      if (o > 0.35) B.fill(wing(0.66, -0.4, 0.13, 0.11, 0, sd), C.red);
    }
    const body = []; for (let k = 0; k < 18; k++) { const th = k / 18 * Math.PI * 2; body.push(T(Math.cos(th) * 0.08, Math.sin(th) * 0.48)); }
    B.fill(body, C.blue);
    const lw = Math.max(0.8, s * 0.05);
    B.stroke([T(0, -0.42), T(-0.14, -0.72), T(-0.24, -0.86)], C.blue, lw);
    B.stroke([T(0, -0.42), T(0.14, -0.72), T(0.24, -0.86)], C.blue, lw);
  }
  faceTurn(e, x, y) {
    let wx = 0, wy = 0;
    const A = this.near(x, y);
    if (A) {
      const dx = A[0] - x, dy = A[1] - y, d = Math.hypot(dx, dy), reach = 300;
      if (d < reach && d > 0.001) {
        const w = 1 - d / reach, s = w * w * (3 - 2 * w) * Math.min(1, d / 40);
        wx = dx / d * s; wy = dy / d * s;
      }
    }
    const c = this.fa[e.id] || [0, 0];
    c[0] += (wx - c[0]) * 0.09; c[1] += (wy - c[1]) * 0.09;
    this.fa[e.id] = c;
    return c;
  }
  near() { return this.mx != null ? [this.mx, this.my] : null; }
  plantRose(g) {
    const r = this.rng((g.s | 0) || 1);
    this.gifts.push({ x: Math.min(1, Math.max(0, g.x)), y: Math.min(1, Math.max(0, g.y)), t0: performance.now(), rot: (r() - 0.5) * 0.8,
      e: { ph1: r() * 6.283, ph2: r() * 6.283, turns: 1.4 + r() * 0.5 } });
    if (this.gifts.length > 14) this.gifts.shift();
  }

  // ---------- math ----------
  h(n) { const x = Math.sin(n) * 43758.5453; return x - Math.floor(x); }
  spr(t, k = 7, w = 16) { if (t <= 0) return 0; return 1 - Math.exp(-t * k) * Math.cos(t * w); }
  eo(t) { if (t <= 0) return 0; if (t >= 1) return 1; return 1 - Math.pow(1 - t, 3); }
  rng(seed) {
    let s = seed | 0;
    return () => { s = (s + 0x6D2B79F5) | 0; let t = Math.imul(s ^ (s >>> 15), 1 | s); t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t; return ((t ^ (t >>> 14)) >>> 0) / 4294967296; };
  }
  bez(a, b, c, d, n) {
    const P = [];
    for (let i = 0; i <= n; i++) { const t = i / n, u = 1 - t;
      P.push([u*u*u*a[0] + 3*u*u*t*b[0] + 3*u*t*t*c[0] + t*t*t*d[0], u*u*u*a[1] + 3*u*u*t*b[1] + 3*u*t*t*c[1] + t*t*t*d[1]]); }
    return P;
  }
  at(P, u) {
    const n = P.length; const i = Math.min(n - 2, Math.max(1, Math.round(u * (n - 1))));
    return [P[i], Math.atan2(P[i+1][1] - P[i-1][1], P[i+1][0] - P[i-1][0])];
  }
  jit(id, f, amp) {
    if (!amp) return [0, 0, 0];
    return [(this.h(id*1.37 + f*7.13)*2 - 1)*amp, (this.h(id*2.71 + f*3.11)*2 - 1)*amp, (this.h(id*5.3 + f*1.7)*2 - 1)*0.035];
  }
  path(c, p, closed) {
    const n = p.length; if (n < 2) return;
    const m = (a, b) => [(a[0] + b[0]) / 2, (a[1] + b[1]) / 2];
    if (closed) {
      const s = m(p[n-1], p[0]); c.moveTo(s[0], s[1]);
      for (let i = 0; i < n; i++) { const q = m(p[i], p[(i+1) % n]); c.quadraticCurveTo(p[i][0], p[i][1], q[0], q[1]); }
      c.closePath();
    } else {
      c.moveTo(p[0][0], p[0][1]);
      for (let i = 1; i < n - 1; i++) { const q = m(p[i], p[i+1]); c.quadraticCurveTo(p[i][0], p[i][1], q[0], q[1]); }
      c.lineTo(p[n-1][0], p[n-1][1]);
    }
  }
  backend(g) {
    return {
      fill: (p, col) => { g.beginPath(); this.path(g, p, true); g.fillStyle = col; g.fill(); },
      stroke: (p, col, w) => { g.beginPath(); this.path(g, p, false); g.strokeStyle = col; g.lineWidth = w; g.lineCap = 'round'; g.lineJoin = 'round'; g.stroke(); },
      rect: (x, y, w, h, col) => { g.fillStyle = col; g.fillRect(x, y, w, h); },
      text: (ch, x, y, S, col) => { g.font = `700 ${S}px ${this.F}`; g.textAlign = 'center'; g.textBaseline = 'alphabetic'; g.fillStyle = col; g.fillText(ch, x, y); },
    };
  }
  vine(base, dir, len, amp, waves, ph, bend, n) {
    n = n || 40; const P = [[base[0], base[1]]], step = len / n; let x = base[0], y = base[1];
    for (let i = 1; i <= n; i++) {
      const u = i / n, a = dir + bend * u + amp * Math.sin(u * Math.PI * waves + ph) * Math.min(1, u * 3);
      x += Math.cos(a) * step; y += Math.sin(a) * step; P.push([x, y]);
    }
    return P;
  }
  curl(P, sign, rad) {
    const n = P.length, e = P[n - 1], a = Math.atan2(e[1] - P[n - 2][1], e[0] - P[n - 2][0]);
    const c = [e[0] - Math.sin(a) * sign * rad, e[1] + Math.cos(a) * sign * rad];
    const a0 = Math.atan2(e[1] - c[1], e[0] - c[0]);
    for (let i = 1; i <= 14; i++) { const u = i / 14, t = a0 + sign * u * Math.PI * 1.6, rr = rad * (1 - 0.5 * u); P.push([c[0] + Math.cos(t) * rr, c[1] + Math.sin(t) * rr]); }
    return P;
  }
  weave(r, startLayer) {
    const cuts = []; const nc = 1 + Math.floor(r() * 3);
    for (let i = 0; i < nc; i++) cuts.push(0.15 + r() * 0.7);
    cuts.sort((a, b) => a - b);
    const segs = []; let u0 = 0, layer = startLayer;
    for (const c of cuts) {
      if (c <= u0) continue;
      segs.push({ u0, u1: c, layer });
      if (r() < 0.3) { const g = 0.02 + r() * 0.03; u0 = Math.min(0.98, c + g); } else u0 = c;
      layer = 1 - layer;
    }
    segs.push({ u0, u1: 1, layer });
    return segs;
  }
  layerAt(segs, u) { for (const s of segs) if (u >= s.u0 && u <= s.u1) return s.layer; return segs[segs.length - 1].layer; }

  // ---------- layout ----------
  resize() {
    const c = this.cv, r = c.getBoundingClientRect(), d = Math.min(innerWidth < 768 ? 1.5 : 2, window.devicePixelRatio || 1);
    if (!r.width) return;
    if (this.W === r.width && this.H === r.height && this.dpr === d) return;
    this.W = r.width; this.H = r.height; this.dpr = d;
    c.width = Math.round(r.width * d); c.height = Math.round(r.height * d);
    this.rect = r;
    if (this.mode === 'type') this.layout(true);
    this.dirty = true;
  }
  mw(ch) {
    if (this.cache[ch] != null) return this.cache[ch];
    this.g.font = `700 100px ${this.F}`;
    let w = this.g.measureText(ch).width / 100;
    if (ch === ' ') w *= 1.4;
    return (this.cache[ch] = w);
  }
  // sets tx/ty/tw on letters; keeps headroom for the garden above and below the type
  layoutCore(A, W, H, prevWrapped, lo_ = {}) {
    const maxW = W * (lo_.maxW ?? 0.8), S0 = H * (lo_.s0 ?? 0.28), LH = 2.1, CY = H * (lo_.cy ?? 0.5);
    const ws = A.map((l) => this.mw(l.ch));
    const words = []; let cur = null;
    A.forEach((l, i) => {
      if (l.ch === ' ') { if (cur) { words.push(cur); cur = null; } words.push({ sp: true, idx: [i] }); }
      else { if (!cur) cur = { idx: [] }; cur.idx.push(i); }
    });
    if (cur) words.push(cur);
    const lineW = (li, S) => { let e = li.length; while (e > 0 && A[li[e-1]].ch === ' ') e--; let w = 0; for (let k = 0; k < e; k++) w += ws[li[k]]; return w * S; };
    const wrap = (S) => {
      const lines = [[]]; let lw = 0;
      for (const w of words) {
        const ww = w.idx.reduce((s, i) => s + ws[i], 0) * S;
        if (!w.sp && lw > 0 && lw + ww > maxW) { lines.push([]); lw = 0; }
        lines[lines.length - 1].push(...w.idx); lw += ww;
      }
      return lines;
    };
    let lines = [A.map((_, i) => i)];
    const unit = lineW(lines[0], 1);
    let S = unit > 0 ? Math.min(S0, maxW / unit) : S0;
    const multi = words.filter((w) => !w.sp).length > 1;
    let wrapped = multi && (S < H * 0.1 || (prevWrapped && S < H * 0.14));
    if (wrapped) {
      let lo = H * 0.03, hi = H * 0.15;
      for (let k = 0; k < 22; k++) {
        const mid = (lo + hi) / 2, ls = wrap(mid);
        const ok = ls.every((li) => lineW(li, mid) <= maxW) && ls.length * LH * mid <= H * 0.82;
        if (ok) lo = mid; else hi = mid;
      }
      S = lo; lines = wrap(S);
      if (lines.length < 2) wrapped = false;
    }
    const n = lines.length, lh = LH * S;
    lines.forEach((li, k) => {
      let x = W / 2 - lineW(li, S) / 2;
      const y = CY + (k - (n - 1) / 2) * lh + 0.33 * S;
      li.forEach((i) => { const l = A[i], w = ws[i] * S; l.tx = x + w / 2; l.ty = y; l.tw = w; l.line = k; x += w; });
    });
    return { S, wrapped, n };
  }
  layout(snap) {
    if (!this.W || this.mode !== 'type') return;
    const A = this.letters.filter((l) => !l.dead);
    const res = this.layoutCore(A, this.W, this.H, this.wrapped, this.o.layout);
    this.wrapped = res.wrapped;
    A.forEach((l) => { if (l.x == null) { l.x = l.tx; l.y = l.ty; } });
    const S = res.S, last = A[A.length - 1];
    this.St = S;
    this.ctx_ = last ? last.tx + last.tw / 2 + 0.07 * S : this.W / 2;
    this.cty = last ? last.ty - 0.33 * S : this.H * (this.o.layout.cy ?? 0.5);
    if (snap || !this.Sd) { this.Sd = S; this.cx = this.ctx_; this.cy = this.cty; A.forEach((l) => { l.x = l.tx; l.y = l.ty; }); }
    this.dirty = true;
    this.o.onChange?.(this.text());
  }

  // ---------- input ----------
  text() { return (this.letters || []).filter((l) => !l.dead).map((l) => l.ch).join(''); }
  wither(now) {
    const A = this.letters.filter((l) => !l.dead), last = A[A.length - 1];
    if (!last) return;
    last.dead = now;
    const pv = A[A.length - 2];
    if (pv && pv.ch !== ' ') { pv.cut = null; pv.endEls = null; pv.tb = now; }
    this.layout();
  }
  /** Every letter withers, last first, `stagger` ms apart. Resolves when the garden is bare. */
  witherAll(stagger = 28) {
    const now = performance.now(), A = this.letters.filter((l) => !l.dead);
    A.reverse().forEach((l, i) => { l.dead = now + i * stagger; });
    this.lastKey = now;
    const total = A.length * stagger + 300;
    return new Promise((r) => { this._bare = { at: now + total, r }; });
  }
  clear() { this.letters = []; this.layout(); }

  // ---------- petals: every run scatters them; they drift down and settle at the foot of the garden ----------
  scatter(perLetter = 4) {
    if (!this.o.petals || this.o.reduced) return;
    const S = this.Sd, r = Math.random;
    for (const l of this.letters) {
      if (l.dead || l.ch === ' ') continue;
      for (let k = 0; k < perLetter; k++) this.petal(l.x + (r() - 0.5) * l.tw, l.y - S * (0.2 + r() * 0.9), (r() - 0.5) * 180 + 50, -(110 + r() * 220));
    }
  }
  petal(x, y, vx, vy) {
    const r = Math.random, S = this.Sd || 60;
    this.fallen.push({ x, y, vx, vy, rot: r() * 6.28, vr: (r() - 0.5) * 7, spin: r() * 6.28, vs: 3 + r() * 6, size: S * (0.055 + r() * 0.05), seed: r() * 100, landed: false, t: 0, fl: r() });
    if (this.fallen.length > 240) { const i = this.fallen.findIndex((p) => p.landed); this.fallen.splice(i > -1 ? i : 0, 1); }
  }
  stepPetals(now, dt) {
    const s = dt / 1000, floor = this.H * 0.985, g = this.gustAt(now);
    this.fallen = this.fallen.filter((p) => {
      p.t += s;
      if (g && Math.abs(p.x / this.W - g.front) < 0.07) {
        if (p.landed) { p.landed = false; p.vy = -(70 + Math.random() * 130); p.t = 0; }
        p.vx += 520 * s;
      }
      if (!p.landed) {
        p.vy = Math.min(p.vy + 380 * s, 55 + p.size * 2.2);
        p.vx *= 1 - 0.9 * s;
        p.x += (p.vx + Math.sin(now / 1000 * 2.1 + p.seed) * 28) * s; p.y += p.vy * s;
        p.rot += p.vr * s; p.spin += p.vs * s;
        const fy = floor - p.fl * p.size * 1.5;
        if (p.y >= fy) { p.y = fy; p.landed = true; p.t = 0; p.spin = 1.2; }
      }
      return !(p.landed && p.t > 28) && p.x > -60 && p.x < this.W + 60;
    });
  }
  drawPetals(B, now, C) {
    for (const p of this.fallen) {
      const ca = Math.cos(p.rot), sa = Math.sin(p.rot), flat = 0.25 + 0.75 * Math.abs(Math.cos(p.spin)), L = p.size;
      const fade = p.landed && p.t > 24 ? Math.max(0, 1 - (p.t - 24) / 4) : 1;
      if (fade <= 0) continue;
      const pts = SAKURA.map(([px, py]) => { const lx = (px - 0.5) * L, ly = py * L * flat; return [p.x + lx * ca - ly * sa, p.y + lx * sa + ly * ca]; });
      if (fade < 1) this.g.globalAlpha = fade;
      B.fill(pts, C.red);
      this.g.globalAlpha = 1;
    }
  }

  // ---------- torii wind: a slow gust crosses the garden while it sits idle ----------
  gustAt(now) {
    if (!this.gust) return null;
    const u = (now - this.gust.t0) / this.gust.dur;
    if (u >= 1) { this.gust = null; return null; }
    return { u, front: -0.35 + u * 1.9 };
  }
  windWarp(st, now) {
    if (!this.o.wind || this.o.reduced) return;
    if (!this.gust && now > this.nextGust && now - this.lastKey > 4000 && !this._oni) { this.gust = { t0: now, dur: 2600 }; this.nextGust = now + 7000 + Math.random() * 5000; }
    const g = this.gustAt(now); if (!g) return;
    const S = st.S, W = this.W, shape = (d) => (d < 0 ? Math.exp(d * 2.2) * Math.cos(-d * 9) : Math.exp(-d * d * 30));
    st.warp = (x, y, l) => {
      const hh = Math.max(0, (l.y - y) / S), k = shape(l.x / W - g.front) * 0.11;
      return [x + k * S * (hh * hh * 0.6 + hh * 0.4), y + Math.abs(k) * 0.15 * S * hh];
    };
    st.rotw = () => Math.sin(g.u * Math.PI) * 0.22;
  }

  // ---------- the oni: a failed command is eaten, letter by letter ----------
  devour() {
    const A = this.letters.filter((l) => !l.dead);
    const vis = A.filter((l) => l.ch !== ' ');
    if (!vis.length || this.o.reduced) return this.witherAll();
    const S = this.Sd, r = S * 0.62, now = performance.now();
    const xs = vis.map((l) => l.x);
    this._oni = { t0: now, dur: 420 + vis.length * 120, x0: Math.min(...xs) - S * 0.9 - r, x1: Math.max(...xs) + S * 0.9 + r * 2,
      y: vis.reduce((s, l) => s + l.y, 0) / vis.length - S * 0.36, r, ids: new Set(A.map((l) => l.id)) };
    this.lastKey = now;
    return new Promise((res) => { this._oni.res = res; });
  }
  stepOni(now) {
    const o = this._oni; if (!o) return;
    const u = Math.min(1, (now - o.t0) / o.dur), e = u < 0.5 ? 2 * u * u : 1 - Math.pow(-2 * u + 2, 2) / 2;
    o.x = o.x0 + (o.x1 - o.x0) * e;
    for (const l of this.letters) {
      if (l.dead || !o.ids.has(l.id) || l.x > o.x + o.r * 0.2) continue;
      l.dead = now;
      if (l.ch !== ' ' && this.o.petals) for (let k = 0; k < 3; k++) this.petal(l.x, l.y - this.Sd * 0.4, -40 - Math.random() * 120, -(60 + Math.random() * 160));
    }
    if (u >= 1 && !o.done) { o.done = true; setTimeout(() => { this._oni = null; this.letters = this.letters.filter((l) => !l.dead); this.layout(); o.res(); }, 280); }
  }
  /** Aka-oni seen side on: a round red head that chomps, gold horns, white fangs, one fierce eye. */
  drawOni(B, now, C) {
    const o = this._oni; if (!o || o.x == null) return;
    const t = (now - o.t0) / 1000, r = o.r, m = 0.12 + 0.6 * Math.abs(Math.sin(t * 15)), x = o.x, y = o.y + Math.sin(t * 15) * r * 0.05;
    const P = (a, rr = r) => [x + Math.cos(a) * rr, y + Math.sin(a) * rr];
    const horn = (a, lean) => { const b1 = P(a - 0.22), b2 = P(a + 0.22), tip = P(a + lean, r * 1.55); B.fill([b1, [ (b1[0] + tip[0]) / 2 - 2, (b1[1] + tip[1]) / 2 ], tip, b2], C.blue); };
    horn(-1.95, -0.25); horn(-1.25, 0.12);
    const head = [[x - r * 0.05, y]]; for (let k = 0; k <= 26; k++) head.push(P(m + (Math.PI * 2 - 2 * m) * k / 26));
    B.fill(head, C.red);
    for (const side of [-1, 1]) for (let k = 1; k <= 3; k++) {
      const a = side * m, d = r * (0.25 + k * 0.22), bx = x + Math.cos(a) * d, by = y + Math.sin(a) * d, tw = r * 0.09;
      const nx = -Math.sin(a) * side, ny = Math.cos(a) * side;
      B.fill([[bx - Math.cos(a) * tw, by - Math.sin(a) * tw], [bx + Math.cos(a) * tw, by + Math.sin(a) * tw], [bx - nx * r * 0.16, by - ny * r * 0.16]], C.text);
    }
    const eye = []; for (let k = 0; k < 10; k++) { const a = k / 10 * Math.PI * 2; eye.push([x + r * 0.12 + Math.cos(a) * r * 0.13, y - r * 0.48 + Math.sin(a) * r * 0.13]); }
    B.fill(eye, C.text);
    B.stroke([[x - r * 0.12, y - r * 0.78], [x + r * 0.32, y - r * 0.62]], C.text, Math.max(1.5, r * 0.08));
  }
  /** Types a string one character at a time (the intro, echo output). By default the result is a
   *  ghost: the first key the visitor presses withers it, like a placeholder. */
  typeOut(str, gap = 85, ghost = true) {
    const chars = [...str];
    this.ghost = ghost;
    if (this.o.reduced) { chars.forEach((ch) => this.add(ch, performance.now())); return Promise.resolve(); }
    this._typing = { chars, i: 0, gap, next: performance.now() };
    return new Promise((r) => { this._typing.r = r; });
  }
  // phone keyboards often skip real key events and write into the field instead, rewriting whole words while
  // they predict: mirror the field and apply the difference, new letters grow and removed ones wither
  inputDiff(el) {
    if (this._kd) { this.iv = this.composing ? el.value : SENT; if (!this.composing) el.value = SENT; return; }
    const old = this.iv == null ? SENT : this.iv, cur = el.value;
    let p = 0; while (p < old.length && p < cur.length && old[p] === cur[p]) p++;
    let dels = old.length - p;
    if (p === 0 && old[0] === SENT && old.length > 1) dels -= 1; // the sentinel is not a letter
    const now = performance.now(); this.lastKey = now;
    if (this.unghost() && dels) dels = 0;
    for (let i = 0; i < dels; i++) this.wither(now);
    for (const ch of cur.slice(p)) {
      if (ch === SENT) continue;
      if (ch.charCodeAt(0) === 10) this.run(); else this.add(ch, now);
    }
    if (this.composing) this.iv = cur; else { el.value = SENT; this.iv = SENT; }
  }
  /** The visitor's first key clears a ghost (the intro word, echo output) before it counts. */
  unghost() {
    if (!this.ghost) return false;
    this.ghost = false; this._typing = null;
    const now = performance.now();
    this.letters.filter((l) => !l.dead).reverse().forEach((l, i) => { l.dead = now + i * 14; });
    return true;
  }
  key(e) {
    if (e.metaKey || e.ctrlKey || e.altKey) return;
    if (e.key === 'Tab') return;
    if (e.key === 'Escape') { this.inp?.blur(); return; }
    if (e.key === 'Backspace' && this.ghost) { e.preventDefault(); this.mark(); this.unghost(); this.layout(); return; }
    if (e.key.length === 1 && !e.isComposing) this.unghost();
    const now = performance.now();
    if (e.key === 'Backspace') { e.preventDefault(); this.mark(); this._typing = null; this.wither(now); return; }
    if (e.key === 'Enter') { e.preventDefault(); this.mark(); this._typing = null; this.run(); return; }
    if (e.key.length !== 1 || e.isComposing) return;
    e.preventDefault(); this.mark(); this._typing = null;
    this.add(e.key, now);
  }
  run() {
    const line = this.text();
    if (this.o.onRun) this.o.onRun(line); else { this.scatter(); this.clear(); }
  }
  mark() { this.lastKey = performance.now(); this._kd = true; setTimeout(() => { this._kd = false; }, 0); }
  add(ch, now) {
    if (this.letters.filter((l) => !l.dead).length >= 64) return;
    if (this.o.reduced) now -= 1e5; // reduced motion: everything is already grown
    const A = this.letters.filter((l) => !l.dead), last = A[A.length - 1];
    this.lastKey = performance.now();
    if (ch === ' ') {
      if (last && last.ch !== ' ') { last.cut = now; this.genEnd(last, now - last.birth); }
      this.letters.push({ ch: ' ', id: this.uid++, birth: now, els: [] });
      this.layout(); return;
    }
    const same = last && last.ch !== ' ';
    const l = { ch, id: this.uid++, birth: now, tb: now, ws: same ? last.ws : Math.floor(this.rand() * 1e9), wi: same ? last.wi + 1 : 0, prev: same ? last : null };
    this.gen(l);
    this.letters.push(l);
    this.layout();
  }

  // ---------- growth ----------
  wordParams(ws) {
    const r = this.rng(ws * 7 + 13), dens = this.o.density ?? 1;
    return { roseP: 0.3 + r() * 0.3, leafy: (0.6 + r() * 0.8) * dens, dens, lean: (r() - 0.5) * 0.6, big: 0.85 + r() * 0.45, curvy: 0.8 + r() * 0.6, bridgeP: Math.min(0.95, (0.45 + r() * 0.35) * dens), w: 0.5 };
  }
  grow(r, E, nid, p, o) {
    const tip = o.tip || (r() < p.roseP ? 'rose' : r() < 0.45 ? 'fan' : r() < 0.6 ? 'leaf' : 'curl');
    let P = o.pts || this.vine(o.base, o.dir, o.len, (0.35 + r() * 0.45) * p.curvy, 1 + r() * 1.6, r() * 6.28, (r() - 0.5) * 1.2, 40);
    if (tip === 'curl') P = this.curl(P, r() < 0.5 ? -1 : 1, 0.05 + r() * 0.05);
    const segs = this.weave(r, o.layer0 != null ? o.layer0 : (r() < 0.5 ? 0 : 1));
    const dur = Math.max(320, (o.len || 0.8) * 620);
    const thorns = []; const nt = Math.floor(r() * 2.5);
    for (let i = 0; i < nt; i++) thorns.push({ u: 0.15 + r() * 0.65, s: r() < 0.5 ? -1 : 1 });
    E.push({ t: 'stem', id: nid(), pts: P, segs, d0: o.d0, dur, w: o.depth ? 0.82 : 1, thorns });
    const nl = Math.floor(r() * 2.8 * p.leafy);
    let sd = r() < 0.5 ? -1 : 1;
    for (let i = 0; i < nl; i++) {
      const u = 0.25 + r() * 0.6, [q, a] = this.at(P, u); sd = -sd;
      E.push({ t: 'leaf', id: nid(), x: q[0], y: q[1], a: a + sd * (0.55 + r() * 0.5), L: (0.14 + r() * 0.2) * (o.depth ? 0.8 : 1), bend: (r() - 0.5) * 1.2, layer: this.layerAt(segs, u), d0: o.d0 + dur * u });
    }
    const [tp, ta] = this.at(P, 1), td = o.d0 + dur * 0.8, tl = this.layerAt(segs, 1);
    if (tip === 'rose') {
      const R = (0.13 + r() * 0.15) * p.big * (o.depth ? 0.75 : 1);
      // legibility: a rose sitting over the letter body mostly goes behind it
      const over = tp[1] > -0.78 && tp[1] < 0.05 && Math.abs(tp[0]) < p.w * 0.5;
      const layer = over ? (r() < 0.22 ? 1 : 0) : (r() < 0.6 ? 1 : tl);
      E.push({ t: 'rose', id: nid(), x: tp[0], y: tp[1], R, rot: (r() - 0.5) * 1.0, ph1: r() * 6.28, ph2: r() * 6.28, turns: 1.8 + r() * 1, layer, d0: td });
      if (r() < 0.2) { const oo = ta + (r() < 0.5 ? 1 : -1) * 1.3; E.push({ t: 'rose', id: nid(), x: tp[0] + Math.cos(oo) * R * 1.4, y: tp[1] + Math.sin(oo) * R * 1.4, R: R * (0.6 + r() * 0.3), rot: (r() - 0.5) * 1.2, ph1: r() * 6.28, ph2: r() * 6.28, turns: 1.8 + r(), layer, d0: td + 120 }); }
    } else if (tip === 'fan') {
      const spread = 0.6 + r() * 0.3;
      for (let j = 0; j < 2; j++) E.push({ t: 'leaf', id: nid(), x: tp[0], y: tp[1], a: ta + (j - 0.5) * spread, L: 0.2 + r() * 0.2, bend: (j - 0.5) * 0.8, layer: tl, d0: td + j * 60 });
    } else if (tip === 'leaf') {
      E.push({ t: 'leaf', id: nid(), x: tp[0], y: tp[1], a: ta + (r() - 0.5) * 0.3, L: 0.16 + r() * 0.16, bend: (r() - 0.5), layer: tl, d0: td });
    }
    if (!o.depth && r() < 0.3) {
      const u = 0.35 + r() * 0.35, [q, a] = this.at(P, u);
      this.grow(r, E, nid, p, { base: q, dir: a + (r() < 0.5 ? -1 : 1) * (0.7 + r() * 0.4), len: (o.len || 0.8) * (0.35 + r() * 0.2), d0: o.d0 + dur * u, depth: 1, layer0: this.layerAt(segs, u) });
    }
    return P;
  }
  gen(l) {
    const r = this.rng((l.ws ^ Math.imul(l.wi + 1, 2654435761)) + Math.floor(this.rand() * 1e6));
    const p = this.wordParams(l.ws), E = []; let k0 = 0; const nid = () => l.id * 100 + (k0++);
    const w = this.mw(l.ch), inX = () => (r() - 0.5) * w * 0.7; p.w = w;
    const nUp = 1 + (r() < 0.45 * p.dens ? 1 : 0);
    for (let i = 0; i < nUp; i++) {
      this.grow(r, E, nid, p, { base: [inX(), -r() * 0.3], dir: -Math.PI / 2 + p.lean * 0.5 + (r() - 0.5) * 0.7, len: 0.6 + r() * 0.5, d0: 40 + i * 130, tip: l.wi === 0 && i === 0 ? 'rose' : null });
    }
    if (r() < 0.4 * p.dens) this.grow(r, E, nid, p, { base: [inX(), -0.1 - r() * 0.35], dir: Math.PI / 2 + (r() - 0.5) * 0.8, len: 0.35 + r() * 0.35, d0: 160 });
    if (l.prev && r() < p.bridgeP) {
      const px = -(this.mw(l.prev.ch) + w) / 2;
      const a = [inX(), -0.05 - r() * 0.55], b = [px + (r() - 0.5) * 0.25, -0.05 - r() * 0.55];
      const bulge = (r() < 0.55 ? -1 : 1) * (0.4 + r() * 0.4), dx = b[0] - a[0];
      let P = this.bez(a, [a[0] + dx * 0.15, a[1] + bulge], [b[0] - dx * 0.15, b[1] + bulge * 0.9], b, 40);
      if (r() < 0.45) P = P.slice(0, Math.floor(P.length * (0.7 + r() * 0.2)));
      this.grow(r, E, nid, p, { pts: P, len: Math.abs(dx) + Math.abs(bulge), d0: 90, tip: r() < 0.5 ? 'curl' : r() < 0.5 ? 'leaf' : 'rose' });
    }
    if (l.wi === 0) this.grow(r, E, nid, p, { base: [-w * 0.3, -0.15 - r() * 0.4], dir: Math.PI + (r() - 0.5) * 1.2, len: 0.45 + r() * 0.3, d0: 120, tip: 'curl' });
    l.els = E;
    l.endEls = null;
    const T = [], tr = this.rng(l.id * 977 + Math.floor(this.rand() * 1e6));
    this.grow(tr, T, () => l.id * 100 + 90 + T.length, p, { base: [w * 0.25, -0.1 - tr() * 0.45], dir: (tr() - 0.5) * 1.4, len: 0.45 + tr() * 0.25, d0: 0, tip: 'curl', depth: 1 });
    l.tend = T.filter((e) => e.t === 'stem').slice(0, 1);
  }
  genEnd(l, rel) {
    const r = this.rng(l.ws + l.wi * 31 + Math.floor(this.rand() * 1e6)), p = this.wordParams(l.ws), E = [];
    let k0 = 50; const nid = () => l.id * 100 + (k0++);
    const w = this.mw(l.ch), n = 1 + Math.floor(r() * 2.5); p.w = w;
    for (let i = 0; i < n; i++) {
      const dir = -Math.PI / 2 + 0.6 + (i - (n - 1) / 2) * 0.9 + (r() - 0.5) * 0.4;
      this.grow(r, E, nid, p, { base: [(r() - 0.2) * w * 0.6, -r() * 0.5], dir, len: 0.4 + r() * 0.45, d0: rel + 40 + i * 90, depth: 1, tip: i === 0 || r() < 0.5 ? 'rose' : 'fan' });
    }
    l.endEls = E;
  }

  strokeRange(B, P, a, b, col, w) {
    const n = P.length - 1, ia = a * n, ib = b * n;
    const lerp = (t) => { const i = Math.min(n - 1, Math.floor(t)), f = t - i; return [P[i][0] + (P[i+1][0] - P[i][0]) * f, P[i][1] + (P[i+1][1] - P[i][1]) * f]; };
    const pts = [lerp(ia)];
    for (let i = Math.floor(ia) + 1; i < ib; i++) pts.push(P[i]);
    pts.push(lerp(ib));
    if (pts.length >= 2) B.stroke(pts, col, w);
  }
  thorn(B, P, u, s, S) {
    const [p, a] = this.at(P, u), d = a + s * 2.3, L = S * 0.045;
    B.stroke([p, [p[0] + Math.cos(d) * L, p[1] + Math.sin(d) * L]], this.cc.blue, S * 0.022);
  }
  leaf(B, bx, by, a, L, bend) {
    if (L < 0.5) return;
    const ca = Math.cos(a), sa = Math.sin(a), px = -sa, py = ca;
    const ax = (u) => { const b = Math.sin(Math.PI * u) * bend * 0.15 * L; return [bx + ca * u * L + px * b, by + sa * u * L + py * b]; };
    const hw = (u) => L * 0.18 * Math.sin(Math.PI * Math.pow(u, 0.8));
    const N = 12, s1 = [], s2 = [];
    for (let i = 0; i <= N; i++) { const u = i / N, c = ax(u), w = hw(u); s1.push([c[0] + px * w, c[1] + py * w]); s2.push([c[0] - px * w, c[1] - py * w]); }
    const tip = ax(1), base = ax(0);
    B.fill([base, ...s1.slice(1, N), tip, tip, ...s2.slice(1, N).reverse(), base], this.cc.blue);
    if (L > 6) { const v = []; for (let i = 0; i <= 8; i++) v.push(ax(0.08 + 0.72 * i / 8)); B.stroke(v, this.cc.vein, Math.max(0.8, L * 0.03)); }
  }
  flower(B, cx, cy, R, rot, e, a, tl) { return this.o.flower === 'rose' ? this.rose(B, cx, cy, R, rot, e, a, tl) : this.sakura(B, cx, cy, R, rot, e, a, tl); }
  /** Sakura 桜: five notched petals unfurl one after another, then the stamens draw in. */
  sakura(B, cx, cy, R, rot, e, a, tl) {
    const cr = Math.cos(rot), sr = Math.sin(rot);
    const tm = tl ? Math.hypot(tl[0], tl[1]) : 0, ux = tm ? tl[0] / tm : 0, uy = tm ? tl[1] / tm : 0, sq = -0.28 * tm;
    const T = (x, y, z = 0) => {
      let dx = x * cr - y * sr, dy = x * sr + y * cr;
      if (tm) { const k = (dx * ux + dy * uy) * sq; dx += k * ux + tl[0] * R * z; dy += k * uy + tl[1] * R * z; }
      return [cx + dx, cy + dy];
    };
    const N = 5, st = 70, lw = Math.max(0.8, R * 0.045);
    for (let i = 0; i < N; i++) {
      const s = this.spr((a - i * st) / 1000, 8, 14); if (s <= 0.001) continue;
      const ang = e.ph1 + i * Math.PI * 2 / N + (1 - Math.min(1, s)) * 0.55;
      const L = R * 1.02 * s * (1 + 0.07 * Math.sin(e.ph2 + i * 1.7)), ca = Math.cos(ang), sa = Math.sin(ang);
      const at = (px, py) => { const lx = px * L, ly = py * L; return T(lx * ca - ly * sa, lx * sa + ly * ca, 0.1 * px); };
      B.fill(SAKURA.map(([px, py]) => at(px, py)), this.cc.red);
      if (R >= 12 && s > 0.5) this.strokeRange(B, [at(0.2, 0), at(0.4, 0.01), at(0.62, 0)], 0, Math.min(1, (s - 0.5) / 0.4), this.cc.line, lw * 0.7);
    }
    const fr = this.eo((a - N * st - 60) / 420);
    if (fr > 0 && R > 3) {
      const ring = []; for (let k = 0; k < 14; k++) { const th = k / 14 * Math.PI * 2; ring.push(T(Math.cos(th) * R * 0.15 * fr, Math.sin(th) * R * 0.15 * fr, 0.3)); }
      B.fill(ring, this.cc.line);
      if (R >= 9) for (let k = 0; k < 7; k++) {
        const th = e.ph2 + k * Math.PI * 2 / 7, rr = R * (0.36 + 0.07 * Math.sin(k * 2.3 + e.ph1)) * fr;
        const tip = T(Math.cos(th) * rr, Math.sin(th) * rr, 0.34);
        B.stroke([T(Math.cos(th) * R * 0.1, Math.sin(th) * R * 0.1, 0.3), tip], this.cc.line, lw * 0.6);
        const dot = []; for (let q = 0; q < 8; q++) { const t2 = q / 8 * Math.PI * 2; dot.push([tip[0] + Math.cos(t2) * lw * 1.3, tip[1] + Math.sin(t2) * lw * 1.3]); }
        B.fill(dot, this.cc.line);
      }
    }
  }
  rose(B, cx, cy, R, rot, e, a, tl) {
    const cr = Math.cos(rot), sr = Math.sin(rot);
    const tm = tl ? Math.hypot(tl[0], tl[1]) : 0, ux = tm ? tl[0] / tm : 0, uy = tm ? tl[1] / tm : 0, sq = -0.28 * tm;
    const T = (x, y, z = 0) => {
      let dx = x * cr - y * sr, dy = x * sr + y * cr;
      if (tm) { const k = (dx * ux + dy * uy) * sq; dx += k * ux + tl[0] * R * z; dy += k * uy + tl[1] * R * z; }
      return [cx + dx, cy + dy];
    };
    // petals, back to front: [cx, cy, rx, ry, angle, depth, edge]
    const P = [
      [0, -0.36, 0.56, 0.54, 0, 0, 0],
      [-0.5, -0.06, 0.6, 0.6, -0.35, 0, 0],
      [0.52, -0.06, 0.6, 0.6, 0.35, 0, 0],
      [-0.42, 0.3, 0.62, 0.48, 0.2, 0.12, 1],
      [0.47, 0.3, 0.62, 0.48, -0.2, 0.12, 1],
      [0.05, 0.42, 0.56, 0.38, 0, 0.14, 0],
    ];
    const ax0 = 0, ay0 = 0.5, lw = Math.max(0.8, R * 0.045), st = 85;
    P.forEach((q, i) => {
      const s = this.spr((a - i * st) / 1000, 8, 14); if (s <= 0.001) return;
      const open = (1 - Math.min(1, s)) * (q[0] < 0 ? 0.5 : -0.5), ang = q[4] + open, ca = Math.cos(ang), sa = Math.sin(ang);
      const pt = (th) => {
        const rr = 1 + 0.08 * Math.sin(3 * th + e.ph1 + i) + 0.04 * Math.sin(5 * th + e.ph2);
        const lx = Math.cos(th) * q[2] * rr, ly = Math.sin(th) * q[3] * rr;
        const px = q[0] + lx * ca - ly * sa, py = q[1] + lx * sa + ly * ca;
        return T((ax0 + (px - ax0) * s) * R, (ay0 + (py - ay0) * s) * R, q[5]);
      };
      const pts = []; for (let k = 0; k < 26; k++) pts.push(pt(k / 26 * Math.PI * 2));
      B.fill(pts, this.cc.red);
      if (q[6] && R >= 12 && s > 0.4) {
        const arc = []; for (let k = 0; k <= 14; k++) arc.push(pt(Math.PI * (1.18 + 0.64 * k / 14)));
        this.strokeRange(B, arc, 0, Math.min(1, (s - 0.4) / 0.5), this.cc.line, lw * 0.8);
      }
    });
    const fr = this.eo((a - P.length * st - 80) / 520);
    if (fr > 0 && R > 3) {
      const sp = [], scl = [];
      const turns = R < 20 ? Math.min(e.turns, 1.3) : R < 32 ? e.turns * 0.8 : e.turns;
      for (let i = 0; i <= 70; i++) { const u = i / 70, th = e.ph1 + u * turns * Math.PI * 2, rr = R * (0.08 + 0.57 * u); sp.push(T(Math.cos(th) * rr * 1.05, Math.sin(th) * rr * 0.72 - 0.12 * R, 0.34 - 0.24 * u)); }
      this.strokeRange(B, sp, 0, fr, this.cc.line, lw);
      if (R >= 14) {
        for (let i = 0; i <= 36; i++) { const u = i / 36; scl.push(T((-0.72 + 1.5 * u) * R, 0.36 * R + 0.16 * R * Math.abs(Math.sin(u * Math.PI * 3)), 0.18)); }
        this.strokeRange(B, scl, 0, fr, this.cc.line, lw);
      }
    }
  }

  // ---------- render (shared by typing view + poster) ----------
  /** One frame. Call from the single clock. */
  tick() {
    const now = performance.now(), dt = Math.min(64, now - (this.lt || now)); this.lt = now;
    if (this.mode === 'poster') {
      // reduced motion: one still frame, at a moment where the garden has fully grown
      const t = this.o.reduced && this.P ? this.P.T * 0.62 : now - (this.pt0 || 0);
      if (!this.busy) this.posterDraw(this.g, t, this.cv.width / 1080);
      return;
    }
    // the pointer, in canvas space
    this.rect = this.cv.getBoundingClientRect();
    if (this.mIn && this.cx_ != null) { this.mx = this.cx_ - this.rect.left; this.my = this.cy_ - this.rect.top; } else this.mx = null;
    if (this._typing) {
      const T = this._typing;
      while (T && now >= T.next && T.i < T.chars.length) { this.add(T.chars[T.i++], now); T.next = now + T.gap * (0.7 + Math.random() * 0.6); }
      if (T.i >= T.chars.length) { this._typing = null; T.r?.(); }
    }
    if (this._bare && now >= this._bare.at) { const b = this._bare; this._bare = null; this.letters = this.letters.filter((l) => !l.dead); this.layout(); b.r(); }
    this.stepOni(now);
    if (this.fallen.length) this.stepPetals(now, dt);
    const k = 1 - Math.exp(-dt / 80);
    this.Sd += (this.St - this.Sd) * k;
    this.cx += (this.ctx_ - this.cx) * k; this.cy += (this.cty - this.cy) * k;
    for (const l of this.letters) if (!l.dead && l.tx != null) { l.x += (l.tx - l.x) * k; l.y += (l.ty - l.y) * k; }
    this.letters = this.letters.filter((l) => !(l.dead && now - l.dead > 300));
    this.paint(now, this.o.caret !== false);
  }
  pal() { return palColors(this.o.palette()); }
  mainState() { return { letters: this.letters, S: this.Sd, C: this.pal().C, boil: this.o.boil && !this.o.reduced, recoil: this.o.recoil, speed: 1, wither: 260, face: true }; }
  paint(now, caret, opaque) {
    const g = this.g, d = this.dpr;
    g.setTransform(d, 0, 0, d, 0, 0);
    if (this.o.transparent && !opaque) g.clearRect(0, 0, this.W, this.H);
    else { g.fillStyle = this.pal().bg; g.fillRect(0, 0, this.W, this.H); }
    const B = this.backend(g), st = this.mainState();
    this.windWarp(st, now);
    this.render(B, now, st);
    this.drawOni(B, now, st.C);
    this.drawPetals(B, now, st.C);
    this.drawFlies(B, now, st.C);
    if (this.gifts.length) {
      const LIFE = 60000; this.gifts = this.gifts.filter((gf) => now - gf.t0 < LIFE);
      for (const gf of this.gifts) {
        const age = now - gf.t0, w = 1 - this.eo((age - (LIFE - 2200)) / 2200);
        g.globalAlpha = Math.max(0, w);
        this.flower(B, gf.x * this.W, gf.y * this.H + (1 - w) * this.Sd * 0.3, this.Sd * 0.24 * (0.65 + 0.35 * w), gf.rot + (1 - w) * 0.6, gf.e, this.o.reduced ? 1e5 : age, null);
      }
      g.globalAlpha = 1;
    }
    if (caret) {
      const S = this.Sd, on = this.o.reduced || now - this.lastKey < 500 || Math.floor(now / 530) % 2 === 0, hf = this.o.reduced ? 1 : this.eo((now - this.t0) / 600);
      if (on && hf > 0) { const h = 0.8 * S * hf, w = Math.max(2, S * 0.03); B.rect(this.cx - w / 2, this.cy - h / 2, w, h, st.C.text); }
    }
  }
  render(B, now, st) {
    const S = st.S, L = st.letters, act = L.filter((l) => !l.dead), sp = st.speed || 1;
    this.cc = st.C; this._now = now; if (st.face) this.perch = [];
    if (st.face) for (const l of L) {
      let tx = 0, ty = 0;
      const A = !l.dead && l.ch !== ' ' ? this.near(l.x, l.y - S * 0.6) : null;
      if (A) {
        const dx = A[0] - l.x, dy = A[1] - (l.y - S * 0.6), d = Math.hypot(dx, dy), reach = Math.max(260, S * 3.2);
        if (d < reach && d > 1) { const w = 1 - d / reach, s = w * w * (3 - 2 * w); tx = dx / d * s; ty = dy / d * s; }
      }
      const c = l.lean || (l.lean = [0, 0]);
      c[0] += (tx - c[0]) * 0.07; c[1] += (ty - c[1]) * 0.07;
    }
    const f = st.boil ? Math.floor(now / 120) : 0, amp = st.boil ? Math.max(0.8, S * 0.01) : 0;
    const kd = (l) => l.dead ? 1 - this.eo((now - l.dead) / st.wither) : 1;
    const draw = (layer) => {
      for (const l of L) {
        if (l.ch === ' ' || !l.els) continue;
        const age = (now - l.birth) * sp, kk = kd(l);
        const each = (e) => { if (e.t === 'stem') this.drawStem(B, e, l, age, kk, st, f, amp, layer); else if (e.layer === layer) this.drawEl(B, e, l, age, kk, st, f, amp); };
        l.els.forEach(each);
        if (l.endEls) l.endEls.forEach(each);
        const i = act.indexOf(l), nx = i >= 0 ? act[i + 1] : null;
        if (l.tend && (l.dead || !nx || nx.ch === ' ')) {
          let fr = this.eo(((now - l.tb) * sp - 150) / 450);
          if (l.cut != null) fr *= 1 - this.eo((now - l.cut) * sp / 150) * Math.min(0.85, st.recoil / (0.6 * S));
          for (const e of l.tend) this.drawStem(B, e, l, age, kk, st, f, amp, layer, fr * kk);
        }
      }
    };
    draw(0);
    // a letter withering on a schedule (witherAll) keeps its glyph until its turn comes
    for (const l of L) if (l.ch !== ' ' && (st.textAll || !l.dead || now < l.dead)) B.text(l.ch, l.x, l.y, S, st.C.text);
    draw(1);
  }
  wpt(st, l, x, y) {
    if (l.birth != null && this._now != null) {
      const t = (this._now - l.birth) * (st.speed || 1) / 1000;
      if (t > 0 && t < 2.5) {
        const hh = Math.max(0, (l.y - y) / st.S), dmp = Math.exp(-t * 3.2), dir = (l.id % 2 ? 1 : -1);
        x += st.S * 0.07 * hh * dmp * Math.sin(t * 11) * dir;
        y += st.S * 0.035 * hh * dmp * Math.sin(t * 11 + 1.2);
      }
    }
    if (st.face && l.lean) {
      const hh = Math.min(2.5, Math.max(0, (l.y - y) / st.S)), f = hh * hh * 0.5 + hh * 0.5;
      x += l.lean[0] * st.S * 0.1 * f;
      y += l.lean[1] * st.S * 0.05 * f;
    }
    return st.warp ? st.warp(x, y, l) : [x, y];
  }
  drawStem(B, e, l, age, kk, st, f, amp, layer, frO) {
    const S = st.S, fr = frO != null ? frO : this.eo((age - e.d0) / e.dur) * kk; if (fr <= 0) return;
    const j = this.jit(e.id, f, amp);
    const P = e.pts.map((p) => this.wpt(st, l, l.x + p[0] * S + j[0], l.y + p[1] * S + j[1]));
    for (const sg of e.segs) {
      if (sg.layer !== layer) continue;
      const b = Math.min(sg.u1, fr); if (b <= sg.u0) continue;
      this.strokeRange(B, P, sg.u0, b, st.C.blue, S * 0.022 * (e.w || 1));
    }
    for (const th of e.thorns) if (fr > th.u && this.layerAt(e.segs, th.u) === layer) this.thorn(B, P, th.u, th.s, S);
  }
  drawEl(B, e, l, age, kk, st, f, amp) {
    const S = st.S, j = this.jit(e.id, f, amp), wx = (x, y) => this.wpt(st, l, l.x + x * S + j[0], l.y + y * S + j[1]);
    const rw = st.rotw ? st.rotw(e) : 0;
    if (e.t === 'leaf') {
      const sc = this.spr((age - e.d0) / 1000, 7, 15) * kk; if (sc <= 0) return;
      const p = wx(e.x, e.y); this.leaf(B, p[0], p[1], e.a + j[2] + rw, e.L * S * sc, e.bend);
    } else if (e.t === 'rose') {
      const a = age - e.d0; if (a < 0) return;
      if (kk <= 0.001) return;
      const p = wx(e.x, e.y), tl = st.face ? this.faceTurn(e, p[0], p[1]) : null;
      if ((st.face || st.perchOn) && !l.dead && a > 700) this.perch.push({ id: e.id, x: p[0], y: p[1], R: e.R * S * kk });
      this.flower(B, p[0], p[1], e.R * S * kk, e.rot + j[2] + rw + (tl ? tl[0] * 0.22 : 0), e, a, tl);
    }
  }

  // ---------- poster ----------
  preset() { return PRESETS.find((p) => p.id === this.poster.preset) || PRESETS[0]; }
  setPoster(patch) { Object.assign(this.poster, patch); this.buildPoster(); }
  buildPoster() {
    const s = this.poster, pr = this.preset(), text = (s.text || '').slice(0, 40) || ' ';
    this.rand = this.rng(s.seed * 7919 + 1);
    const L = []; let prev = null, uid = 1;
    for (const ch of text) {
      if (ch === ' ') { const sp = { ch: ' ', id: 9000 + uid++, els: [] }; L.push(sp); prev = sp; continue; }
      const same = prev && prev.ch !== ' ';
      const l = { ch, id: 9000 + uid++, ws: same ? prev.ws : Math.floor(this.rand() * 1e9), wi: same ? prev.wi + 1 : 0, prev: same ? prev : null };
      this.gen(l); L.push(l); prev = l;
    }
    const n = L.length;
    const step = pr.id === 'grow' ? Math.min(110, 3000 / n) : pr.id === 'typed' ? Math.min(85, 3600 / n) : 0;
    const off = pr.id === 'grow' ? 200 : pr.id === 'typed' ? 300 : 0;
    L.forEach((l, i) => { l.b = off + i * step; });
    if (pr.id === 'scatter') { const r = this.rng(s.seed * 31 + 5); L.forEach((l) => { l.b = 150 + r() * 2400; }); }
    const wd = Math.min(60, 900 / n);
    L.forEach((l, i) => {
      l.d = pr.id === 'scatter' ? 4700 + i * 8 : 4200 + (n - 1 - i) * wd;
      const nx = L[i + 1];
      l.cutRel = null;
      if (pr.id !== 'scatter' && l.ch !== ' ' && nx && nx.ch === ' ') { l.cutRel = Math.max(30, nx.b - l.b); this.genEnd(l, l.cutRel); }
    });
    this.rand = Math.random;
    const res = this.layoutCore(L, 1080, 1080, false);
    // fit the whole garden (not just the type) inside a 6% margin
    let S = res.S, x0 = 1e9, y0 = 1e9, x1 = -1e9, y1 = -1e9;
    const addP = (l, x, y, r) => { const X = l.tx + x * S, Y = l.ty + y * S, R = (r || 0) * S; x0 = Math.min(x0, X - R); x1 = Math.max(x1, X + R); y0 = Math.min(y0, Y - R); y1 = Math.max(y1, Y + R); };
    for (const l of L) {
      if (l.ch === ' ') continue;
      addP(l, -l.tw / S / 2, -0.75); addP(l, l.tw / S / 2, 0.1);
      const all = [...l.els, ...(l.endEls || [])];
      for (const e of all) {
        if (e.t === 'stem') e.pts.forEach((q) => addP(l, q[0], q[1], 0.02));
        else if (e.t === 'leaf') addP(l, e.x, e.y, e.L);
        else addP(l, e.x, e.y, e.R * 1.15);
      }
    }
    // centre on the type itself; shrink just enough for the garden to clear the margins on every side
    let tx0 = 1e9, tx1 = -1e9, ty0 = 1e9, ty1 = -1e9;
    for (const l of L) if (l.ch !== ' ') { tx0 = Math.min(tx0, l.tx - l.tw / 2); tx1 = Math.max(tx1, l.tx + l.tw / 2); ty0 = Math.min(ty0, l.ty - 0.72 * S); ty1 = Math.max(ty1, l.ty); }
    const Z = 1080, half = Z * 0.44, bcx = (tx0 + tx1) / 2, bcy = (ty0 + ty1) / 2;
    const k = Math.min(1.25, half / Math.max(1, bcx - x0, x1 - bcx), half / Math.max(1, bcy - y0, y1 - bcy));
    L.forEach((l) => { l.tx = Z / 2 + (l.tx - bcx) * k; l.ty = Z / 2 + (l.ty - bcy) * k; l.tw *= k; l.x = l.tx; l.y = l.ty; });
    this.P = { letters: L, S: S * k, T: pr.T, preset: pr.id };
    this.pt0 = performance.now();
  }
  posterFly(B, tt, P, C) {
    const ps = this.perch.slice().sort((a, b) => a.x - b.x);
    if (!ps.length) return;
    const p1 = ps[Math.floor(ps.length * 0.25)], p2 = ps[Math.min(ps.length - 1, Math.floor(ps.length * 0.75))];
    const A = [-80, 300], s1 = [p1.x, p1.y - p1.R * 0.55], s2 = [p2.x, p2.y - p2.R * 0.55], E = [1160, 260];
    const sz = Math.max(12, P.S * 0.09), t = tt / 1000, eo = (v) => this.eo(Math.max(0, Math.min(1, v)));
    const leg = (a, b, u, lift) => { const e = eo(u); return [a[0] + (b[0] - a[0]) * e, a[1] + (b[1] - a[1]) * e - Math.sin(e * Math.PI) * lift]; };
    let p, flying = true, vx = 0;
    if (t < 1.8) { p = leg(A, s1, t / 1.8, 160); vx = 1; }
    else if (t < 3.4) { p = s1; flying = false; }
    else if (t < 4.6) { p = leg(s1, s2, (t - 3.4) / 1.2, 140); vx = s2[0] > s1[0] ? 1 : -1; }
    else if (t < 5.8) { p = s2; flying = false; }
    else { p = leg(s2, E, (t - 5.8) / 1.2, 60); vx = 1; }
    let o, ang;
    if (flying) { o = Math.abs(Math.cos(t * 17)); p = [p[0] + Math.sin(t * 7) * 6, p[1] + Math.sin(t * 13) * 4]; ang = vx * 0.3; }
    else { const burst = (t > 2.4 && t < 2.8) || (t > 5 && t < 5.3); o = burst ? 0.2 + 0.8 * Math.abs(Math.cos(t * 9)) : 0.3 + 0.1 * Math.sin(t * 3); ang = 0; }
    this.butterfly(B, p[0], p[1], sz, ang, o, C);
  }
  /** Draws the poster at loop time t (ms) into g, 1080 units wide, scaled by `scale`. */
  posterDraw(g, t, scale = 1) {
    const P = this.P; if (!P) return;
    const Z = 1080, pc = this.pal();
    g.setTransform(scale, 0, 0, scale, 0, 0);
    if (this.o.transparent && g === this.g) g.clearRect(0, 0, Z, Z); else { g.fillStyle = pc.bg; g.fillRect(0, 0, Z, Z); }
    const tt = ((t % P.T) + P.T) % P.T, L = P.letters, B = this.backend(g);
    const st = { letters: L, S: P.S, C: pc.C, boil: this.o.boil && !this.o.reduced, recoil: this.o.recoil, speed: 1, wither: 600, textAll: true };
    L.forEach((l) => { l.x = l.tx; l.y = l.ty; l.dead = null; });
    const set = (base) => L.forEach((l) => { l.birth = base + l.b; l.tb = l.birth; l.cut = l.cutRel != null ? l.birth + l.cutRel : null; });
    if (P.preset === 'breathe') {
      set(-1e5);
      const ph = tt / P.T * Math.PI * 2, S = P.S;
      st.warp = (x, y, l) => { const h = Math.max(0, (l.y - y) / S); return [x + Math.sin(ph + l.x * 0.004 + h * 1.1) * S * 0.03 * h, y + Math.cos(ph + x * 0.003) * S * 0.008 * h]; };
      st.rotw = (e) => Math.sin(ph + e.id * 0.7) * 0.12;
    } else if (P.preset === 'grow' || P.preset === 'scatter') {
      set(0);
      L.forEach((l) => { if (tt >= l.d) l.dead = l.d; });
    } else if (P.preset === 'wind') {
      set(-1e5);
      const S = P.S, u = tt / P.T, front = -0.35 + u * 1.9;
      st.warp = (x, y, l) => {
        const hh = Math.max(0, (l.y - y) / S), d = l.x / 1080 - front;
        const g2 = d < 0 ? Math.exp(d * 2.2) * Math.cos(-d * 9) : Math.exp(-d * d * 30);
        const idle = Math.sin(tt / 1000 * 1.6 + l.x * 0.01) * 0.012;
        return [x + (g2 * 0.13 + idle) * S * hh * hh * 0.6 + (g2 * 0.13 + idle) * S * hh * 0.4, y + Math.abs(g2) * 0.02 * S * hh];
      };
      st.rotw = (e) => { const d = e.x / 1080 - front; return (d < 0 ? Math.exp(d * 2.2) * Math.cos(-d * 9) : Math.exp(-d * d * 30)) * 0.35; };
    } else if (P.preset === 'reach') {
      set(-1e5);
      const S = P.S, ph = tt / P.T * Math.PI * 2, lx = 540 + Math.sin(ph) * 430, ly = 540 + Math.sin(ph * 2) * 260;
      this._light = [lx, ly];
      st.warp = (x, y, l) => {
        const dx = lx - l.x, dy = ly - (l.y - S * 0.6), d = Math.hypot(dx, dy) || 1, w = Math.max(0, 1 - d / 560), s = w * w * (3 - 2 * w);
        const hh = Math.min(2.5, Math.max(0, (l.y - y) / S)), f = hh * hh * 0.5 + hh * 0.5;
        return [x + dx / d * s * S * 0.12 * f, y + dy / d * s * S * 0.06 * f];
      };
      st.rotw = (e) => { const a = Math.atan2(ly - e.y, lx - e.x); return Math.sin(a) * 0.15; };
    } else if (P.preset === 'visit') {
      set(-1e5);
      st.perchOn = true; this.perch = [];
      const ph = tt / P.T * Math.PI * 2;
      st.warp = (x, y, l) => { const hh = Math.max(0, (l.y - y) / P.S); return [x + Math.sin(ph + l.x * 0.004 + hh) * P.S * 0.015 * hh, y]; };
    } else {
      if (tt >= 4400) return;
      set(0);
      st.letters = L.filter((l) => tt >= l.birth);
      st.textAll = false;
    }
    this.render(B, tt, st);
    if (P.preset === 'reach') {
      const [lx, ly] = this._light, r = P.S * 0.09, pts = [];
      for (let k = 0; k < 20; k++) { const a = k / 20 * Math.PI * 2; pts.push([lx + Math.cos(a) * r, ly + Math.sin(a) * r]); }
      B.fill(pts, st.C.line);
    }
    if (P.preset === 'visit') this.posterFly(B, tt, P, st.C);
    if (P.preset === 'typed') {
      const born = st.letters, last = born[born.length - 1], S = P.S;
      if (Math.floor(tt / 400) % 2 === 0 || (last && tt - last.birth < 300)) {
        const x = last ? last.tx + last.tw / 2 + 0.07 * S : 540, y = last ? last.ty - 0.33 * S : 540, w = Math.max(2, S * 0.03), h = 0.8 * S;
        B.rect(x - w / 2, y - h / 2, w, h, st.C.text);
      }
    }
  }

  // ---------- export ----------
  dl(blob, name) {
    const a = document.createElement('a'); a.href = URL.createObjectURL(blob); a.download = name;
    document.body.appendChild(a); a.click(); a.remove(); setTimeout(() => URL.revokeObjectURL(a.href), 4000);
  }
  savePNG() {
    if (this.mode === 'poster') {
      const c = document.createElement('canvas'); c.width = c.height = 1080;
      this.posterDraw(c.getContext('2d'), performance.now() - this.pt0, 1);
      c.toBlob((b) => b && this.dl(b, `${this.fname()}.png`)); return;
    }
    this.paint(performance.now(), false, true);
    this.cv.toBlob((b) => b && this.dl(b, 'garden.png'));
  }
  saveSVG() {
    const parts = [], n = (v) => Math.round(v * 100) / 100;
    const pb = () => ({ d: '', moveTo(x, y) { this.d += `M${n(x)} ${n(y)}`; }, lineTo(x, y) { this.d += `L${n(x)} ${n(y)}`; }, quadraticCurveTo(a, b, x, y) { this.d += `Q${n(a)} ${n(b)} ${n(x)} ${n(y)}`; }, closePath() { this.d += 'Z'; } });
    const esc = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
    const B = {
      fill: (p, col) => { const b = pb(); this.path(b, p, true); parts.push(`<path d="${b.d}" fill="${col}"/>`); },
      stroke: (p, col, w) => { const b = pb(); this.path(b, p, false); parts.push(`<path d="${b.d}" fill="none" stroke="${col}" stroke-width="${n(w)}" stroke-linecap="round" stroke-linejoin="round"/>`); },
      rect: () => {},
      text: (ch, x, y, S, col) => parts.push(`<text x="${n(x)}" y="${n(y)}" text-anchor="middle" font-family="Playfair Display, serif" font-weight="700" font-size="${n(S)}" fill="${col}">${esc(ch)}</text>`),
    };
    let w = this.W, h = this.H;
    if (this.mode === 'poster') { w = h = 1080; this.posterSVG = true; }
    if (this.mode === 'type') this.render(B, performance.now(), this.mainState());
    else {
      // poster: same drawing, recorded at the current loop time
      const g = this.g; this.g = { setTransform() {}, clearRect() {}, fillRect() {}, set fillStyle(v) {} };
      const be = this.backend; this.backend = () => B;
      try { this.posterDraw(this.g, performance.now() - this.pt0, 1); } finally { this.g = g; this.backend = be; }
    }
    const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${n(w)}" height="${n(h)}" viewBox="0 0 ${n(w)} ${n(h)}"><rect width="100%" height="100%" fill="${this.pal().bg}"/>${parts.join('')}</svg>`;
    this.dl(new Blob([svg], { type: 'image/svg+xml' }), this.mode === 'poster' ? `${this.fname()}.svg` : 'garden.svg');
  }
  fname() { return 'garden-' + (this.poster.text || 'loop').trim().replace(/[^a-z0-9]+/gi, '-').toLowerCase().slice(0, 24) + '-' + this.poster.preset; }
  status(msg) { this.o.onStatus?.(msg, this.busy); }
  async exportVideo() {
    if (!this.vmime || this.busy) { this.status('Video export is not supported in this browser. Use PNG frames.'); return; }
    const P = this.P, c = document.createElement('canvas'); c.width = c.height = 1080;
    const g = c.getContext('2d'), stream = c.captureStream(0), track = stream.getVideoTracks()[0];
    const rec = new MediaRecorder(stream, { mimeType: this.vmime, videoBitsPerSecond: 14e6 }), chunks = [];
    rec.ondataavailable = (e) => e.data && e.data.size && chunks.push(e.data);
    const done = new Promise((r) => { rec.onstop = r; });
    this.busy = true; this.status('Recording…');
    this.posterDraw(g, 0); rec.start();
    const N = Math.round(P.T / 1000 * 30), wait = (ms) => new Promise((r) => setTimeout(r, ms));
    const t0 = performance.now();
    for (let i = 0; i < N; i++) {
      this.posterDraw(g, i * 1000 / 30);
      if (track.requestFrame) track.requestFrame();
      if (i % 15 === 0) this.status(`Recording ${Math.round(i / N * 100)}%`);
      const target = t0 + (i + 1) * 1000 / 30; await wait(Math.max(0, target - performance.now()));
    }
    rec.stop(); await done;
    const ext = this.vmime.includes('mp4') ? 'mp4' : 'webm';
    this.dl(new Blob(chunks, { type: this.vmime.split(';')[0] }), `${this.fname()}.${ext}`);
    this.busy = false; this.status(`Saved ${ext.toUpperCase()} · ${N} frames`);
  }
  async exportFrames() {
    if (this.busy) return;
    const P = this.P, c = document.createElement('canvas'); c.width = c.height = 1080;
    const g = c.getContext('2d'), N = Math.round(P.T / 1000 * 30), files = [];
    this.busy = true; this.status('Rendering frames…');
    for (let i = 0; i < N; i++) {
      this.posterDraw(g, i * 1000 / 30);
      const b = await new Promise((r) => c.toBlob(r, 'image/png'));
      files.push({ name: `frame_${String(i + 1).padStart(4, '0')}.png`, data: new Uint8Array(await b.arrayBuffer()) });
      if (i % 10 === 0) this.status(`Rendering ${Math.round(i / N * 100)}%`);
    }
    this.dl(this.zip(files), `${this.fname()}-frames.zip`);
    this.busy = false; this.status(`Saved ${N} PNG frames · 30 fps`);
  }
  zip(files) {
    const crcT = this._crcT || (this._crcT = (() => { const t = new Uint32Array(256); for (let n = 0; n < 256; n++) { let c = n; for (let k = 0; k < 8; k++) c = c & 1 ? 0xEDB88320 ^ (c >>> 1) : c >>> 1; t[n] = c >>> 0; } return t; })());
    const crc = (d) => { let c = 0xFFFFFFFF; for (let i = 0; i < d.length; i++) c = crcT[(c ^ d[i]) & 255] ^ (c >>> 8); return (c ^ 0xFFFFFFFF) >>> 0; };
    const enc = new TextEncoder(), parts = [], cen = []; let off = 0;
    for (const f of files) {
      const nm = enc.encode(f.name), cr = crc(f.data), sz = f.data.length;
      const h = new DataView(new ArrayBuffer(30));
      h.setUint32(0, 0x04034b50, true); h.setUint16(4, 20, true); h.setUint16(8, 0, true); h.setUint16(12, 33, true);
      h.setUint32(14, cr, true); h.setUint32(18, sz, true); h.setUint32(22, sz, true); h.setUint16(26, nm.length, true);
      parts.push(h.buffer, nm, f.data);
      const c = new DataView(new ArrayBuffer(46));
      c.setUint32(0, 0x02014b50, true); c.setUint16(4, 20, true); c.setUint16(6, 20, true); c.setUint16(14, 33, true);
      c.setUint32(16, cr, true); c.setUint32(20, sz, true); c.setUint32(24, sz, true); c.setUint16(28, nm.length, true); c.setUint32(42, off, true);
      cen.push(c.buffer, nm);
      off += 30 + nm.length + sz;
    }
    const cs = cen.reduce((s, b) => s + (b.byteLength || b.length), 0);
    const e = new DataView(new ArrayBuffer(22));
    e.setUint32(0, 0x06054b50, true); e.setUint16(8, files.length, true); e.setUint16(10, files.length, true); e.setUint32(12, cs, true); e.setUint32(16, off, true);
    return new Blob([...parts, ...cen, e.buffer], { type: 'application/zip' });
  }
}
