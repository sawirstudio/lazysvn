// Visual effects for lazysvn.sawirstudio.com: matrix rain, a WebGL hologram
// logo, a CRT pass over the live demo, a boot log and scrambling headings.
//
// Rules every effect follows:
//   - it is decoration only: the page works and reads the same without it;
//   - it never sits on top of text people need to read at full strength;
//   - prefers-reduced-motion turns animation off (static frames at most);
//   - animation pauses when its element is off-screen or the tab is hidden.
(() => {
'use strict';

const RM = matchMedia('(prefers-reduced-motion: reduce)').matches;
const root = document.documentElement;
const DPR = Math.min(window.devicePixelRatio || 1, 2);

// Runs cb(true/false) as el enters or leaves the viewport.
function onVisible(el, cb) {
  new IntersectionObserver(([e]) => cb(e.isIntersecting), { rootMargin: '120px' }).observe(el);
}

// A requestAnimationFrame loop that can be paused and resumed.
function loop(step) {
  let id = 0, on = false;
  const tick = (t) => { if (!on) return; step(t); id = requestAnimationFrame(tick); };
  return {
    start() { if (!on) { on = true; id = requestAnimationFrame(tick); } },
    stop() { on = false; cancelAnimationFrame(id); },
  };
}

function program(gl, vs, fs) {
  const sh = (type, src) => {
    const s = gl.createShader(type);
    gl.shaderSource(s, src); gl.compileShader(s);
    if (!gl.getShaderParameter(s, gl.COMPILE_STATUS)) throw new Error(gl.getShaderInfoLog(s));
    return s;
  };
  const p = gl.createProgram();
  gl.attachShader(p, sh(gl.VERTEX_SHADER, vs));
  gl.attachShader(p, sh(gl.FRAGMENT_SHADER, fs));
  gl.linkProgram(p);
  if (!gl.getProgramParameter(p, gl.LINK_STATUS)) throw new Error(gl.getProgramInfoLog(p));
  gl.useProgram(p);
  const buf = gl.createBuffer();
  gl.bindBuffer(gl.ARRAY_BUFFER, buf);
  gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 1, -1, -1, 1, 1, 1]), gl.STATIC_DRAW);
  const loc = gl.getAttribLocation(p, 'aPos');
  gl.enableVertexAttribArray(loc);
  gl.vertexAttribPointer(loc, 2, gl.FLOAT, false, 0, 0);
  const u = {};
  const n = gl.getProgramParameter(p, gl.ACTIVE_UNIFORMS);
  for (let i = 0; i < n; i++) { const name = gl.getActiveUniform(p, i).name; u[name] = gl.getUniformLocation(p, name); }
  return u;
}

function fitCanvas(c, gl) {
  const w = Math.max(1, Math.round(c.clientWidth * DPR)), h = Math.max(1, Math.round(c.clientHeight * DPR));
  if (c.width !== w || c.height !== h) { c.width = w; c.height = h; }
  if (gl) gl.viewport(0, 0, w, h);
}

// ============================================================ matrix rain
// Falling svn status letters and diff signs behind the page. The text column
// has its own dark backdrop (see .page::before), and the rain fades as you
// scroll away from the top.
function rain() {
  const c = document.getElementById('rain');
  if (!c || RM) return;
  const ctx = c.getContext('2d');
  const FS = 16;
  const GLYPHS = 'MMAADD??C!~+-|@#<>=/01svn'.split('');
  const TINT = { M: '#f3f99d', A: '#5af78e', D: '#ff6b6b', '?': '#57c7ff', C: '#e0443e' };
  let w = 0, h = 0, drops = [], speed = [];
  const size = () => {
    w = c.width = innerWidth; h = c.height = innerHeight;
    const cols = Math.ceil(w / FS);
    drops = Array.from({ length: cols }, () => Math.random() * -h / FS);
    speed = Array.from({ length: cols }, () => 0.35 + Math.random() * 0.75);
    ctx.font = `${FS}px "JetBrains Mono", Menlo, monospace`;
    ctx.textBaseline = 'top';
  };
  size();
  addEventListener('resize', size);
  const fade = () => root.style.setProperty('--rain', Math.max(0.16, 0.55 - scrollY / 1400).toFixed(3));
  fade();
  addEventListener('scroll', fade, { passive: true });

  let last = 0;
  loop((t) => {
    if (t - last < 40) return; // ~25 fps is plenty for rain
    last = t;
    ctx.fillStyle = 'rgba(10,12,13,0.16)';
    ctx.fillRect(0, 0, w, h);
    for (let i = 0; i < drops.length; i++) {
      const x = i * FS, y = Math.floor(drops[i]) * FS;
      const g = GLYPHS[(Math.random() * GLYPHS.length) | 0];
      // Re-colour the previous head as trail, then draw a bright new head.
      ctx.fillStyle = TINT[g] && Math.random() < 0.35 ? TINT[g] : '#2fbf6b';
      ctx.fillText(GLYPHS[(Math.random() * GLYPHS.length) | 0], x, y - FS);
      ctx.fillStyle = '#d9ffe6';
      ctx.fillText(g, x, y);
      drops[i] += speed[i];
      if (y > h && Math.random() > 0.97) drops[i] = Math.random() * -20;
    }
  }).start();
}

// ============================================================ hologram logo
// The ASCII logo, rasterised to a texture and drawn as a stack of additive
// slices in perspective: depth, a cyan falloff, scan bands, flicker, glitch
// bursts and a power-on sweep. It tilts toward the pointer.
const HOLO_VS = `
attribute vec2 aPos;
uniform mat4 uMVP;
uniform vec2 uSize;
uniform float uZ;
varying vec2 vUv;
void main() {
  vUv = vec2(aPos.x * 0.5 + 0.5, 0.5 - aPos.y * 0.5);
  gl_Position = uMVP * vec4(aPos * uSize, uZ, 1.0);
}`;
const HOLO_FS = `
precision mediump float;
uniform sampler2D uTex;
uniform float uTime, uLayer, uPower, uGlitch, uFront;
varying vec2 vUv;
float hash(float n) { return fract(sin(n) * 43758.5453); }
void main() {
  vec2 uv = vUv;
  float band = floor(uv.y * 16.0);
  float g = uGlitch * step(0.55, hash(band + floor(uTime * 30.0)));
  uv.x += g * (hash(band * 7.0 + floor(uTime * 30.0)) - 0.5) * 0.09;
  float reveal = step(abs(vUv.y - 0.5), uPower * 0.62) * step(0.0, uPower - hash(floor(uTime * 60.0)) * (1.0 - uPower));
  vec3 col; float a;
  if (uFront > 0.5) {
    float off = 0.0035 + g * 0.012;
    float r = texture2D(uTex, uv + vec2(off, 0.0)).a;
    float m = texture2D(uTex, uv).a;
    float b = texture2D(uTex, uv - vec2(off, 0.0)).a;
    col = vec3(0.20 * m + 0.45 * r, 0.82 * m, 0.42 * m + 0.5 * b);
    a = max(m, max(r, b) * 0.6) * 0.9;
  } else {
    float s = texture2D(uTex, uv).a;
    col = mix(vec3(0.25, 0.85, 0.5), vec3(0.3, 0.75, 1.0), uLayer);
    a = s * pow(1.0 - uLayer, 1.8) * 0.09;
  }
  float scan = 0.78 + 0.22 * sin(vUv.y * 190.0 - uTime * 7.0);
  float bar = 0.35 * exp(-pow((fract(uTime * 0.16) * 1.4 - 0.2 - vUv.y) * 8.0, 2.0));
  float flicker = 0.9 + 0.1 * hash(floor(uTime * 22.0));
  gl_FragColor = vec4(col * (scan + bar) * flicker, a * reveal * flicker);
}`;

function mat4mul(a, b) {
  const o = new Float32Array(16);
  for (let i = 0; i < 4; i++) for (let j = 0; j < 4; j++) {
    let s = 0;
    for (let k = 0; k < 4; k++) s += a[k * 4 + j] * b[i * 4 + k];
    o[i * 4 + j] = s;
  }
  return o;
}
const perspective = (fov, asp, n, f) => {
  const t = 1 / Math.tan(fov / 2);
  return new Float32Array([t / asp, 0, 0, 0, 0, t, 0, 0, 0, 0, (f + n) / (n - f), -1, 0, 0, (2 * f * n) / (n - f), 0]);
};
const translate = (x, y, z) => new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, x, y, z, 1]);
const rotX = (r) => { const c = Math.cos(r), s = Math.sin(r); return new Float32Array([1, 0, 0, 0, 0, c, s, 0, 0, -s, c, 0, 0, 0, 0, 1]); };
const rotY = (r) => { const c = Math.cos(r), s = Math.sin(r); return new Float32Array([c, 0, -s, 0, 0, 1, 0, 0, s, 0, c, 0, 0, 0, 0, 1]); };

async function hologram() {
  const canvas = document.getElementById('holo');
  const art = document.querySelector('.logo');
  if (!canvas || !art) return;
  const gl = canvas.getContext('webgl', { alpha: true, premultipliedAlpha: false, antialias: true });
  if (!gl) return;
  try { await document.fonts.load('48px "JetBrains Mono"'); } catch (_) { /* fall back to Menlo */ }

  // Rasterise the logo into a texture.
  const lines = art.textContent.replace(/\n+$/, '').split('\n');
  const FS = 48, LH = Math.round(FS * 1.08), PAD = 12;
  const tc = document.createElement('canvas');
  const tx = tc.getContext('2d');
  const font = `${FS}px "JetBrains Mono", Menlo, monospace`;
  tx.font = font;
  tc.width = Math.ceil(Math.max(...lines.map((l) => tx.measureText(l).width))) + PAD * 2;
  tc.height = LH * lines.length + PAD * 2;
  tx.font = font; tx.textBaseline = 'top'; tx.fillStyle = '#fff';
  lines.forEach((l, i) => tx.fillText(l, PAD, PAD + i * LH));
  const texAspect = tc.width / tc.height;

  let u;
  try { u = program(gl, HOLO_VS, HOLO_FS); } catch (e) { return; }
  const tex = gl.createTexture();
  gl.bindTexture(gl.TEXTURE_2D, tex);
  gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, tc);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
  gl.enable(gl.BLEND);
  gl.blendFuncSeparate(gl.SRC_ALPHA, gl.ONE, gl.ONE, gl.ONE);
  root.classList.add('fx-holo');

  const LAYERS = 22, FOV = 0.6, DIST = 4;
  let rx = 0, ry = 0, tx0 = 0, ty0 = 0;
  addEventListener('pointermove', (e) => {
    tx0 = (e.clientX / innerWidth - 0.5) * 0.9;
    ty0 = (e.clientY / innerHeight - 0.5) * 0.5;
  }, { passive: true });

  const t0 = performance.now();
  let glitchUntil = 0, nextGlitch = t0 + 1400;

  function draw(now) {
    fitCanvas(canvas, gl);
    const t = (now - t0) / 1000;
    const power = RM ? 1 : Math.min(1, t / 1.1);
    if (!RM && now > nextGlitch) { glitchUntil = now + 90 + Math.random() * 120; nextGlitch = now + 2500 + Math.random() * 4000; }
    const glitch = !RM && (now < glitchUntil || power < 1) ? 1 : 0;
    const sway = RM ? 0 : Math.sin(t * 0.6) * 0.1;
    rx += (ty0 - rx) * 0.06; ry += (tx0 + sway - ry) * 0.06;

    const asp = canvas.width / canvas.height;
    const halfW = Math.tan(FOV / 2) * DIST * asp * 0.86;
    const size = [halfW, halfW / texAspect];
    const mvp = mat4mul(perspective(FOV, asp, 0.1, 50), mat4mul(translate(0, 0, -DIST), mat4mul(rotX(rx), rotY(ry))));

    gl.clearColor(0, 0, 0, 0);
    gl.clear(gl.COLOR_BUFFER_BIT);
    gl.uniformMatrix4fv(u.uMVP, false, mvp);
    gl.uniform2f(u.uSize, size[0], size[1]);
    gl.uniform1f(u.uTime, RM ? 1.3 : t);
    gl.uniform1f(u.uPower, power);
    gl.uniform1f(u.uGlitch, glitch);
    const depth = size[1] * 0.9;
    for (let i = LAYERS; i >= 0; i--) {
      gl.uniform1f(u.uLayer, i / LAYERS);
      gl.uniform1f(u.uFront, i === 0 ? 1 : 0);
      gl.uniform1f(u.uZ, -depth * (i / LAYERS));
      gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
    }
  }

  if (RM) { requestAnimationFrame(draw); addEventListener('resize', () => requestAnimationFrame(draw)); return; }
  const l = loop(draw);
  onVisible(canvas, (v) => (v ? l.start() : l.stop()));
}

// ============================================================ CRT pass
// A transparent WebGL layer over the live demo: scanlines, tube vignette, a
// rolling refresh bar, grain and a glass reflection. The DOM text underneath
// stays crisp, selectable and keyboard-driven; CSS adds the phosphor glow.
const CRT_FS = `
precision mediump float;
uniform vec2 uRes;
uniform float uTime;
varying vec2 vUv;
float hash(vec2 p) { return fract(sin(dot(p, vec2(12.9898, 78.233))) * 43758.5453); }
void main() {
  vec2 uv = vUv;
  vec2 c = uv - 0.5;
  float scan = 0.5 + 0.5 * sin(uv.y * uRes.y * 3.14159 / 1.6);
  float dark = 0.09 * scan;
  dark += 0.5 * smoothstep(0.42, 0.9, length(c * vec2(0.9, 1.15)));
  float bar = exp(-pow((fract(uTime * 0.11) * 1.3 - 0.15 - (1.0 - uv.y)) * 12.0, 2.0));
  float light = 0.045 * bar;
  light += (hash(uv * uRes + fract(uTime * 7.0) * 91.0) - 0.5) * 0.05;
  light += 0.045 * smoothstep(0.45, 0.0, length((uv - vec2(0.2, 0.85)) * vec2(1.0, 2.4)));
  light = max(light, 0.0);
  float a = clamp(dark + light, 0.0, 0.8);
  gl_FragColor = vec4(vec3(0.78, 1.0, 0.86) * light, a);
}`;
const CRT_VS = `
attribute vec2 aPos;
varying vec2 vUv;
void main() { vUv = aPos * 0.5 + 0.5; gl_Position = vec4(aPos, 0.0, 1.0); }`;

function crt() {
  const screen = document.querySelector('.term .screen');
  if (!screen) return;
  const canvas = document.createElement('canvas');
  canvas.className = 'crt';
  canvas.setAttribute('aria-hidden', 'true');
  screen.appendChild(canvas);
  const gl = canvas.getContext('webgl', { alpha: true, premultipliedAlpha: true });
  if (!gl) { canvas.remove(); return; }
  let u;
  try { u = program(gl, CRT_VS, CRT_FS); } catch (e) { canvas.remove(); return; }
  gl.enable(gl.BLEND);
  gl.blendFunc(gl.ONE, gl.ONE_MINUS_SRC_ALPHA);
  root.classList.add('fx-crt');

  const t0 = performance.now();
  const draw = (now) => {
    fitCanvas(canvas, gl);
    gl.clearColor(0, 0, 0, 0);
    gl.clear(gl.COLOR_BUFFER_BIT);
    gl.uniform2f(u.uRes, canvas.width, canvas.height);
    gl.uniform1f(u.uTime, RM ? 0 : (now - t0) / 1000);
    gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
  };
  if (RM) { new ResizeObserver(() => requestAnimationFrame(draw)).observe(screen); return; }
  let last = 0;
  const l = loop((now) => { if (now - last < 33) return; last = now; draw(now); });
  onVisible(screen, (v) => (v ? l.start() : l.stop()));
}

// ============================================================ boot log
function boot() {
  const el = document.getElementById('boot');
  if (!el) return;
  const ver = (document.querySelector('.ver') || {}).textContent || '0.4.2';
  const LINES = [
    ['v', `lazysvn ${ver} - booting`],
    ['ok', 'probing svn client ......... svn 1.15'],
    ['ok', 'mounting working copy ...... ~/acme/trunk'],
    ['ok', 'svn status --xml ........... 7 changes'],
    ['ok', 'pairing diff hunks ......... 0.5 ms/frame'],
    ['ok', 'loading keymap ............. vim'],
    ['p', 'ready. try the demo below'],
  ];
  const esc = (s) => s.replace(/[&<>]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' }[c]));
  const line = ([k, text], typed, done) => {
    const t = esc(text.slice(0, typed));
    if (k === 'v') return `<span class="v">${t}</span>`;
    if (k === 'p') return `<span class="ok">&gt;</span> ${t}${done ? '<span class="bc"></span>' : ''}`;
    return `${done ? '<span class="ok">[ ok ]</span>' : '<span class="wait">[ .. ]</span>'} ${t}`;
  };
  const all = () => LINES.map((l) => line(l, l[1].length, true)).join('\n');
  if (RM) { el.innerHTML = all(); return; }

  let li = 0, ci = 0, timer = 0;
  const finish = () => { clearInterval(timer); el.innerHTML = all(); removeEventListener('keydown', finish); removeEventListener('pointerdown', finish); };
  const tick = () => {
    ci += 3;
    if (ci >= LINES[li][1].length) { li++; ci = 0; }
    if (li >= LINES.length) return finish();
    el.innerHTML = LINES.slice(0, li).map((l) => line(l, l[1].length, true))
      .concat(line(LINES[li], ci, false)).join('\n');
  };
  setTimeout(() => { timer = setInterval(tick, 16); }, 500); // after the logo powers on
  addEventListener('keydown', finish, { once: true });
  addEventListener('pointerdown', finish, { once: true });
}

// ============================================================ headings
// Section headings decode from noise the first time they scroll into view.
// The heading keeps its real text in the DOM until it is visible and again
// within 650 ms, so search engines, copy-paste and screen readers see it.
function scramble() {
  if (RM) return;
  const POOL = '█▓▒░<>/\\|=+*#%@01MAD?C';
  document.querySelectorAll('section > h2').forEach((h) => {
    const text = h.textContent;
    const s = h;
    const io = new IntersectionObserver(([e]) => {
      if (!e.isIntersecting) return;
      io.disconnect();
      const t0 = performance.now(), DUR = 650;
      const step = (now) => {
        const p = Math.min(1, (now - t0) / DUR);
        s.textContent = [...text].map((ch, i) =>
          ch === ' ' || p > (i + 1) / text.length * 0.85 + 0.15 * Math.random() * (1 - p) ? ch : POOL[(Math.random() * POOL.length) | 0]).join('');
        if (p < 1) requestAnimationFrame(step); else s.textContent = text;
      };
      requestAnimationFrame(step);
    }, { threshold: 1 });
    io.observe(h);
  });
}

// Each effect is independent: one failing must not take the others down.
for (const fx of [rain, hologram, crt, boot, scramble]) {
  try { const r = fx(); if (r && r.catch) r.catch(() => {}); } catch (_) { /* decoration only */ }
}
})();
