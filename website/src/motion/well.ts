// The well: one persistent canvas, one state object, driven from the GSAP ticker.
// Pages set the mode (data-well on <main>), scroll and pointer feed the state, commands drop stones.
import { gsap } from 'gsap';
import { Renderer, Program, Mesh, Triangle, RenderTarget, Texture } from 'ogl';
import { VERT, SCENE, GLYPH } from './shaders';
import { isMobile, isTouch, prefersReducedMotion, onScroll } from '../engine';

export const state = {
  depth: 0,
  shift: 0,
  lift: 0,
  dim: 1,
  flood: 1,
  band: 0,
  water: 1,
  eye: 1,
  blink: 0,
};

const RAMP = ' .\'`:-~=+<*xo#%@';                 // 16 steps, dark to bright
const RUNES = ['i', 'k', 'u', 'f', 'z', 'o', 'd', 'm']; // drawn as Elder Futhark strokes
const GLYPHS = RAMP.length + RUNES.length;

let renderer: Renderer | null = null;
let still = false;
let render: (() => void) | null = null;
const drops = [0, 1, 2, 3].map(() => [0, 0, -99, 0]);
let dropIndex = 0;
let ready: Promise<void> = Promise.resolve();

export const glReady = () => ready;
/** Resolves when the first frame is drawn, or immediately without GL. */
export const wellOn = () => !!renderer;

/** A stone falls into the water: rings spread from (x, z) in well units. */
export function drop(x = (Math.random() - 0.5) * 0.9, z = (Math.random() - 0.5) * 0.9, strength = 1) {
  if (!renderer) return;
  drops[dropIndex++ % 4] = [x, z, gsap.ticker.time, strength];
  if (still) render?.();
}

/** Re-render once (used under reduced motion, where nothing runs on the ticker). */
export const redraw = () => render?.();

function rune(ctx: CanvasRenderingContext2D, name: string, x: number, w: number, h: number) {
  const L = x + w * 0.3, R = x + w * 0.7, C = x + w * 0.5, T = h * 0.16, B = h * 0.84, M = h * 0.5;
  const seg = (a: number, b: number, c: number, d: number) => { ctx.moveTo(a, b); ctx.lineTo(c, d); };
  ctx.beginPath();
  switch (name) {
    case 'i': seg(C, T, C, B); break;                                      // isa
    case 'k': seg(R, T, L, M); seg(L, M, R, B); break;                     // kaunan
    case 'u': seg(L, B, L, T); seg(L, T, R, h * 0.36); seg(R, h * 0.36, R, B); break; // uruz
    case 'f': seg(L, T, L, B); seg(L, h * 0.42, R, T); seg(L, h * 0.62, R, h * 0.36); break; // fehu
    case 'z': seg(C, T, C, B); seg(C, M, L, T); seg(C, M, R, T); break;    // algiz
    case 'o': seg(C, T, L, h * 0.42); seg(C, T, R, h * 0.42); seg(L, h * 0.42, R, B); seg(R, h * 0.42, L, B); break; // othala
    case 'd': seg(L, T, L, B); seg(R, T, R, B); seg(L, T, R, B); seg(R, T, L, B); break; // dagaz
    case 'm': seg(L, T, L, B); seg(R, T, R, B); seg(L, T, R, h * 0.5); seg(R, T, L, h * 0.5); break; // mannaz
  }
  ctx.stroke();
}

function atlas(): HTMLCanvasElement {
  const w = 48, h = 80;
  const c = document.createElement('canvas');
  c.width = w * GLYPHS; c.height = h;
  const ctx = c.getContext('2d')!;
  ctx.fillStyle = '#fff';
  ctx.strokeStyle = '#fff';
  ctx.textAlign = 'center';
  ctx.textBaseline = 'middle';
  ctx.font = `500 50px "Martian Mono", ui-monospace, Menlo, monospace`;
  const mw = ctx.measureText('M').width;
  const size = Math.min(50, 50 * (w * 0.92) / mw);
  ctx.font = `500 ${size}px "Martian Mono", ui-monospace, Menlo, monospace`;
  [...RAMP].forEach((ch, i) => ctx.fillText(ch, i * w + w / 2, h / 2 + 2));
  ctx.lineWidth = 5;
  ctx.lineCap = 'square';
  RUNES.forEach((r, i) => rune(ctx, r, (RAMP.length + i) * w, w, h));
  return c;
}

export function initWell(host: HTMLElement | null) {
  if (!host || renderer) return;
  if (location.search.includes('debug')) (window as unknown as { __well: typeof state }).__well = state;
  const reduced = prefersReducedMotion();
  const saveData = (navigator as unknown as { connection?: { saveData?: boolean } }).connection?.saveData;
  if (saveData) return;
  try {
    renderer = new Renderer({ dpr: Math.min(devicePixelRatio, isMobile() ? 1.5 : 2), alpha: false, antialias: false, depth: false });
  } catch {
    renderer = null;
    return;
  }
  const gl = renderer.gl;
  if (!gl) { renderer = null; return; }
  gl.clearColor(0.043, 0.039, 0.031, 1);
  const canvas = gl.canvas as HTMLCanvasElement;
  canvas.classList.add('gl-canvas');
  host.appendChild(canvas);

  const geometry = new Triangle(gl);
  const target = new RenderTarget(gl, {
    width: 2, height: 2, depth: false,
    minFilter: gl.NEAREST, magFilter: gl.NEAREST, generateMipmaps: false,
  } as ConstructorParameters<typeof RenderTarget>[1]);
  const atlasTex = new Texture(gl, { generateMipmaps: false, flipY: false, minFilter: gl.LINEAR, magFilter: gl.LINEAR });

  const look = { x: 0, y: 0 };
  const sway = { x: 0, y: 0 };
  const sceneU = {
    uRes: { value: [1, 1] }, uCell: { value: [1, 1] }, uTime: { value: 0 },
    uDepth: { value: 0 }, uShift: { value: 0 }, uLift: { value: 0 }, uWater: { value: 1 }, uEye: { value: 1 }, uBlink: { value: 0 },
    uLook: { value: [0, 0] }, uSway: { value: [0, 0] },
    uD0: { value: drops[0] }, uD1: { value: drops[1] }, uD2: { value: drops[2] }, uD3: { value: drops[3] },
  };
  const glyphU = {
    tScene: { value: target.texture }, tAtlas: { value: atlasTex },
    uCell: { value: [1, 1] }, uGrid: { value: [1, 1] }, uGlyphs: { value: GLYPHS },
    uDim: { value: 1 }, uFlood: { value: 1 }, uBand: { value: 0 }, uTime: { value: 0 }, uDebug: { value: 0 },
  };
  const sceneMesh = new Mesh(gl, { geometry, program: new Program(gl, { vertex: VERT, fragment: SCENE, uniforms: sceneU, depthTest: false }) });
  const glyphMesh = new Mesh(gl, { geometry, program: new Program(gl, { vertex: VERT, fragment: GLYPH, uniforms: glyphU, depthTest: false }) });

  const resize = () => {
    const dpr = renderer!.dpr;
    const cw = (isMobile() ? 7 : 9) * dpr;
    const ch = cw / 0.6;
    renderer!.setSize(innerWidth, innerHeight);
    const W = innerWidth * dpr, H = innerHeight * dpr;
    const cols = Math.ceil(W / cw), rows = Math.ceil(H / ch);
    target.setSize(cols, rows);
    sceneU.uRes.value = [W, H];
    sceneU.uCell.value = [cw, ch];
    glyphU.uCell.value = [cw, ch];
    glyphU.uGrid.value = [cols, rows];
    if (still) render?.();
  };

  render = () => {
    const t = gsap.ticker.time;
    sceneU.uTime.value = t; glyphU.uTime.value = t;
    sceneU.uDepth.value = state.depth;
    sceneU.uShift.value = state.shift;
    sceneU.uLift.value = state.lift;
    sceneU.uWater.value = state.water;
    sceneU.uEye.value = state.eye;
    sceneU.uBlink.value = state.blink;
    sceneU.uLook.value = [look.x, look.y];
    sceneU.uSway.value = [sway.x, sway.y];
    sceneU.uD0.value = drops[0]; sceneU.uD1.value = drops[1]; sceneU.uD2.value = drops[2]; sceneU.uD3.value = drops[3];
    glyphU.uDim.value = state.dim;
    glyphU.uFlood.value = state.flood;
    glyphU.uBand.value = state.band;
    glyphU.uDebug.value = (state as { debug?: number }).debug || 0;
    renderer!.render({ scene: sceneMesh, target });
    renderer!.render({ scene: glyphMesh });
  };

  // The atlas needs the mono face; wait for it (the loader waits for this too).
  ready = document.fonts.load('500 50px "Martian Mono"').catch(() => {}).then(() => {
    atlasTex.image = atlas();
    resize();
    render!();
    document.documentElement.classList.add('gl-on');
  });

  addEventListener('resize', resize);

  if (reduced) {
    still = true;
    state.flood = 0;
    return;
  }

  // Pointer (or, on touch, scroll velocity) feeds the eye and a little camera sway.
  const aim = { x: 0, y: 0 };
  const touch = isTouch();
  if (touch) onScroll(({ velocity }) => { aim.y = gsap.utils.clamp(-0.5, 0.5, velocity / 50); });
  else addEventListener('pointermove', (e) => { aim.x = e.clientX / innerWidth - 0.5; aim.y = e.clientY / innerHeight - 0.5; }, { passive: true });

  // Blink every few seconds, like something alive down there.
  const blink = () => {
    gsap.timeline({ onComplete: () => { gsap.delayedCall(2.5 + Math.random() * 4, blink); } })
      .to(state, { blink: 1, duration: 0.09, ease: 'power2.in' })
      .to(state, { blink: 0, duration: 0.22, ease: 'power2.out' });
  };
  gsap.delayedCall(3, blink);

  let wasHidden = false;
  gsap.ticker.add((time) => {
    const r = gsap.ticker.deltaRatio();
    if (touch) aim.y *= Math.pow(0.92, r);
    const k = 1 - Math.pow(0.94, r);
    // idle: the eye wanders a little on its own; the pointer leads it
    const ix = aim.x + Math.sin(time * 0.37) * 0.08;
    const iy = aim.y + Math.sin(time * 0.23 + 1.3) * 0.06;
    look.x += (ix - look.x) * k; look.y += (iy - look.y) * k;
    sway.x += (aim.x * 0.5 + Math.sin(time * 0.11) * 0.08 - sway.x) * k * 0.6;
    sway.y += (aim.y * 0.5 + Math.cos(time * 0.13) * 0.06 - sway.y) * k * 0.6;
    if (!atlasTex.image) return;
    const hidden = state.dim < 0.004 && state.flood < 0.001;
    if (hidden && wasHidden) return;           // nothing to draw: the well has sunk away
    wasHidden = hidden;
    render!();
  });
}
