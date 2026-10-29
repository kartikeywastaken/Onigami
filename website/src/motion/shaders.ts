// The well. Pass 1 raymarches a real 3D scene (stone shaft, water, Odin's eye) at one sample per
// terminal cell. Pass 2 draws every cell as a glyph from an atlas, chosen by brightness.
export const VERT = /* glsl */ `
attribute vec2 uv;
attribute vec2 position;
varying vec2 vUv;
void main() { vUv = uv; gl_Position = vec4(position, 0.0, 1.0); }
`;

export const SCENE = /* glsl */ `
precision highp float;
uniform vec2 uRes;      // screen, device px
uniform vec2 uCell;     // one cell, device px
uniform float uTime;
uniform float uDepth;   // 0 = above the rim, 1 = at the water
uniform float uShift;   // horizontal offset of the well in the frame
uniform float uLift;    // vertical offset
uniform float uWater;   // 1 = water, 0 = dry (404)
uniform float uEye;     // 1 = the eye is there
uniform float uBlink;   // 0 open, 1 shut
uniform vec2 uLook;     // pointer, -0.5..0.5
uniform vec2 uSway;     // camera sway, lagged pointer
uniform vec4 uD0, uD1, uD2, uD3; // drops: x, z, start time, strength
varying vec2 vUv;

const float H = 4.6;    // depth of the shaft
const float ER = 0.44;  // eye radius

float hash(vec2 p) { return fract(sin(dot(p, vec2(127.1, 311.7))) * 43758.5453); }
float noise(vec2 p) {
  vec2 i = floor(p), f = fract(p);
  vec2 u = f * f * (3.0 - 2.0 * f);
  return mix(mix(hash(i), hash(i + vec2(1, 0)), u.x), mix(hash(i + vec2(0, 1)), hash(i + vec2(1, 1)), u.x), u.y);
}

vec3 eyePos() { return vec3(0.0, -H + 0.1, 0.0); }

float drop(vec2 xz, vec4 d) {
  float age = uTime - d.z;
  if (age <= 0.0 || age >= 7.0 || d.w <= 0.0) return 0.0;
  float rr = length(xz - d.xy);
  float front = age * 0.5;
  float w = exp(-pow((rr - front) * 4.0, 2.0)) * exp(-age * 0.45) * d.w;
  return sin((rr - front) * 26.0) * w * 3.0;
}

float ripple(vec2 xz) {
  float r = length(xz);
  float h = sin(r * 13.0 - uTime * 1.5) * 0.35 * exp(-r * 1.1);
  h += (noise(xz * 3.0 + uTime * 0.12) - 0.5) * 0.7;
  h += drop(xz, uD0) + drop(xz, uD1) + drop(xz, uD2) + drop(xz, uD3);
  return h;
}

float bricks(vec3 p) {
  float a = atan(p.z, p.x) / 6.28318 * 22.0;
  float v = p.y / 0.27;
  float row = floor(v);
  a += mod(row, 2.0) * 0.5;
  vec2 f = fract(vec2(a, v));
  vec2 e = min(f, 1.0 - f);
  float groove = smoothstep(0.0, 0.09, min(e.x * 0.85, e.y));
  float rough = noise(vec2(floor(a) * 3.1, row * 1.7));
  return (1.0 - groove) * 0.04 - rough * 0.03;
}

// x: distance, y: material (0 stone, 1 ground, 2 water, 3 eye)
vec2 map(vec3 p) {
  float r = length(p.xz);
  float rim = max(abs(r - 1.2) - 0.2, p.y - 0.4);
  if (rim < 0.12) rim += bricks(p);
  float ground = max(p.y + (abs(p.y) < 0.12 ? 0.03 * noise(p.xz * 4.0) : 0.0), 1.38 - r);
  vec2 res = vec2(rim, 0.0);
  if (ground < res.x) res = vec2(ground, 1.0);
  if (uWater > 0.5) {
    float water = p.y + H;
    if (water < 0.15) water -= ripple(p.xz) * 0.018;
    if (water < res.x) res = vec2(water, 2.0);
  } else {
    float rubble = p.y + H + 0.3 - noise(p.xz * 5.0) * 0.2;
    if (rubble < res.x) res = vec2(rubble, 0.0);
  }
  if (uEye > 0.5) {
    float e = length(p - eyePos()) - ER;
    if (e < res.x) res = vec2(e, 3.0);
  }
  return res;
}

vec3 normalAt(vec3 p) {
  vec2 e = vec2(0.002, 0.0);
  return normalize(vec3(
    map(p + e.xyy).x - map(p - e.xyy).x,
    map(p + e.yxy).x - map(p - e.yxy).x,
    map(p + e.yyx).x - map(p - e.yyx).x));
}

void main() {
  vec2 frag = (gl_FragCoord.xy) * uCell;           // centre-ish of this cell, device px
  float m = min(uRes.x, uRes.y);
  vec2 uv = (frag - 0.5 * uRes) / m;
  uv.x -= uShift;
  uv.y -= uLift;

  float d = uDepth;
  float e = d * d * (3.0 - 2.0 * d);
  vec3 ro = mix(vec3(0.0, 6.6, 1.5), vec3(0.0, -H + 1.95, 0.02), e);
  ro.xz += uSway * mix(0.9, 0.12, e);
  vec3 ta = mix(vec3(0.0, -2.5, -0.2), eyePos(), e);
  vec3 ww = normalize(ta - ro);
  vec3 uu = normalize(cross(ww, vec3(0.0, 0.0, -1.0)));
  vec3 vv = cross(uu, ww);
  vec3 rd = normalize(uv.x * uu + uv.y * vv + 1.55 * ww);

  float t = 0.0;
  vec2 h = vec2(1.0, -1.0);
  float steps = 0.0;
  for (int i = 0; i < 72; i++) {
    vec3 p = ro + rd * t;
    h = map(p);
    if (h.x < 0.0015 * t || t > 30.0) break;
    t += h.x * 0.9;
    steps += 1.0;
  }

  float lum = 0.0;
  float mat = 0.0;
  if (t < 30.0) {
    vec3 p = ro + rd * t;
    vec3 n = normalAt(p);
    vec3 ld = normalize(vec3(-0.55, 1.0, -0.45));
    float diff = max(dot(n, ld), 0.0);
    float ao = 1.0 - steps / 150.0;
    mat = h.y;
    if (h.y < 0.5) {                                   // stone
      float fall = exp(min(p.y, 0.0) * 0.3);
      float glow = (uWater > 0.5 ? 0.5 : 0.08) * exp(-(p.y + H) * 0.8);
      lum = (0.2 + 0.8 * diff) * fall * ao + glow;
    } else if (h.y < 1.5) {                            // ground around the well
      float r = length(p.xz);
      float grass = 0.55 + 0.45 * noise(p.xz * 6.0);
      lum = diff * 0.42 * grass * (1.0 - smoothstep(1.4, 4.2, r));
    } else if (h.y < 2.5) {                            // water: reflects the opening above
      vec3 rr = reflect(rd, n);
      float k = (0.4 - p.y) / max(rr.y, 0.001);
      vec2 hit = (p.xz + rr.xz * k) * 0.5;                                // cheat: the opening looks wider
      float sky = 1.0 - smoothstep(0.82, 1.0, length(hit));                 // the opening above
      float moon = 1.0 - smoothstep(0.18, 0.3, length(hit - vec2(0.22, -0.16))); // the moon in it
      float slope = 1.0 - n.y;
      lum = 0.12 + 0.34 * sky + 0.55 * moon * sky + min(slope * 14.0, 0.45);
    } else {                                           // the eye
      vec3 q = p - eyePos();
      vec3 look = normalize(ro - eyePos() + uu * uLook.x * 3.2 - vv * uLook.y * 3.2);
      float c = dot(normalize(q), look);
      // lids: an almond opening across the camera's horizontal axis
      float lu = dot(q, uu) / ER;
      float lv = dot(q, vv) / ER;
      float open = 0.52 * (1.0 - uBlink) * max(1.0 - lu * lu, 0.0);
      if (abs(lv) > open) {                            // lids: stone-coloured folds
        mat = 0.0;
        float fold = 0.5 + 0.5 * sin((abs(lv) - open) * 40.0);
        lum = 0.22 + 0.35 * diff + 0.22 * fold * smoothstep(0.0, 0.08, abs(lv) - open);
      } else if (c > 0.962) {
        lum = 0.0;                                     // pupil
      } else if (c > 0.84) {                           // iris: bright, streaked
        mat = 4.0;
        float ang = atan(dot(q, vv), dot(q, uu));
        lum = 0.72 + 0.26 * sin(ang * 11.0 + uTime * 0.25);
        lum *= (1.0 - smoothstep(0.94, 0.962, c)) * 0.4 + 0.6;
      } else {                                         // white of the eye
        lum = 0.3 + 0.35 * diff;
      }
    }
  }
  lum = clamp(lum, 0.0, 1.0);
  gl_FragColor = vec4(lum, mat / 4.0, 0.0, 1.0);
}
`;

export const GLYPH = /* glsl */ `
precision highp float;
uniform sampler2D tScene;
uniform sampler2D tAtlas;
uniform vec2 uCell;
uniform vec2 uGrid;
uniform float uGlyphs;  // glyphs in the atlas
uniform float uDim;
uniform float uFlood;   // 0 dry screen, 1 screen flooded with glyphs
uniform float uBand;    // 1 = only the top band shows (manual page)
uniform float uTime;
uniform float uDebug;
varying vec2 vUv;

float hash(vec2 p) { return fract(sin(dot(p, vec2(41.3, 289.1))) * 15731.743); }

void main() {
  vec2 px = gl_FragCoord.xy;
  vec2 cell = floor(px / uCell);
  vec2 cuv = fract(px / uCell);
  vec4 s = texture2D(tScene, (cell + 0.5) / uGrid);
  float lum = s.r;
  float mat = floor(s.g * 4.0 + 0.5);   // 0 stone, 1 ground, 2 water, 3 eye, 4 iris
  float sy = (cell.y + 0.5) / uGrid.y;

  lum *= mix(1.0, smoothstep(0.42, 0.9, sy), uBand);
  lum *= uDim;
  // phosphor shimmer, one cell at a time
  lum *= 0.94 + 0.06 * hash(cell + floor(uTime * 6.0));

  // flood: glyph water rises from the bottom with a moving crest
  float edge = uFlood * 1.3 - 0.15 + 0.035 * sin(cell.x * 0.19 + uTime * 3.1) + 0.02 * sin(cell.x * 0.47 - uTime * 2.3);
  float below = step(sy, edge);
  float crest = below * smoothstep(edge - 0.07, edge, sy);
  float body = 0.42 + 0.22 * hash(cell + floor(uTime * 12.0));
  lum = mix(lum, mix(body, 0.98, crest), below);

  float runes = uGlyphs - 16.0;
  float idx;
  bool eye = mat > 2.5 && below < 0.5;
  if (mat > 3.5 && below < 0.5) idx = lum < 0.06 ? 0.0 : 16.0 + floor(clamp(lum, 0.0, 0.999) * runes);
  else idx = floor(clamp(lum, 0.0, 0.999) * 16.0);
  vec2 a = vec2((idx + cuv.x) / uGlyphs, 1.0 - cuv.y);
  float g = texture2D(tAtlas, a).a;

  vec3 ground = vec3(0.043, 0.039, 0.031);
  vec3 col;
  if (eye) {
    col = mix(vec3(0.24, 0.36, 0.38), vec3(0.72, 0.86, 0.87), smoothstep(0.1, 0.8, lum));
  } else {
    col = mix(vec3(0.36, 0.22, 0.07), vec3(0.95, 0.66, 0.23), smoothstep(0.05, 0.62, lum));
    col = mix(col, vec3(0.94, 0.9, 0.82), smoothstep(0.78, 1.0, lum));
  }
  gl_FragColor = vec4(mix(ground, col, g), 1.0);
  if (uDebug > 0.5) gl_FragColor = vec4(s.r, s.g, 0.0, 1.0);
}
`;
