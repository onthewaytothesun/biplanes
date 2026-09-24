(() => {
  'use strict';
  const W = 320, H = 200, GROUND = 180, TAU = Math.PI * 2, DIRS = 32;
  const HANGAR = [24, 296];
  const BARN_X = 160;
  const P = { THRUST: 95, DRAG: 0.9, GALONG: 70, MAXS: 125, STALL: 30, TAKEOFF: 42, TURN: 2.7, GRAV: 140, BULLET: 210, BLIFE: 0.75, FIRECD: 0.22 };
  const TEAM = [
    { name: 'Красные', body: '#c8413b', dark: '#7d231f', wing: '#ec8a6a' },
    { name: 'Синие', body: '#3561c4', dark: '#1d3577', wing: '#7ea6ee' },
  ];

  const cv = document.getElementById('cv');
  const ctx = cv.getContext('2d');
  ctx.imageSmoothingEnabled = false;
  const $ = (id) => document.getElementById(id);

  // ---------- helpers ----------
  const mk = (w, h) => { const c = document.createElement('canvas'); c.width = w; c.height = h; return c; };
  const wrapX = (x) => ((x % W) + W) % W;
  const wrapDx = (from, to) => (((to - from) % W) + W * 1.5) % W - W / 2;
  const angDiff = (a, b) => { let d = a - b; while (d > Math.PI) d -= TAU; while (d < -Math.PI) d += TAU; return d; };
  const norm = (a) => angDiff(a, 0);
  const clamp = (v, a, b) => Math.max(a, Math.min(b, v));
  const rnd = (a, b) => a + Math.random() * (b - a);
  const hex = (h) => [parseInt(h.slice(1, 3), 16), parseInt(h.slice(3, 5), 16), parseInt(h.slice(5, 7), 16)];
  const dist = (a, b) => Math.hypot(wrapDx(a.x, b.x), b.y - a.y);

  // ---------- background ----------
  function buildBg() {
    const c = mk(W, H), g = c.getContext('2d');
    const img = g.createImageData(W, H), d = img.data;
    const sky = ['#4f8fcf', '#5c9bd6', '#6aa7dc', '#79b3e1', '#8abfe6', '#9ccaea', '#b0d6ee', '#c6e2f1'].map(hex);
    const bayer = [0, 8, 2, 10, 12, 4, 14, 6, 3, 11, 1, 9, 15, 7, 13, 5];
    const put = (x, y, rgb) => { const i = (y * W + x) * 4; d[i] = rgb[0]; d[i + 1] = rgb[1]; d[i + 2] = rgb[2]; d[i + 3] = 255; };
    for (let y = 0; y < GROUND; y++) {
      const t = y / (GROUND - 1) * (sky.length - 1), i = Math.floor(t), f = t - i;
      for (let x = 0; x < W; x++) {
        const th = (bayer[(y & 3) * 4 + (x & 3)] + 0.5) / 16;
        put(x, y, f > th ? sky[Math.min(i + 1, sky.length - 1)] : sky[i]);
      }
    }
    const far = hex('#8db7ad'), near = hex('#6b9d68'), tree = hex('#4c7d4d');
    const k = TAU / W;
    for (let x = 0; x < W; x++) {
      const hf = Math.round(GROUND - 20 - 7 * Math.sin(x * k * 3 + 1) - 4 * Math.sin(x * k * 7 + 2));
      const hn = Math.round(GROUND - 8 - 4 * Math.sin(x * k * 5) - 2 * Math.sin(x * k * 11 + 1));
      for (let y = hf; y < GROUND; y++) put(x, y, far);
      for (let y = hn; y < GROUND; y++) put(x, y, near);
      if ((x * 37) % 23 === 0) for (let y = hn - 3; y < hn + 1; y++) { put(x, y, tree); if (y > hn - 3) put(Math.min(W - 1, x + 1), y, tree); }
    }
    const grass1 = hex('#7cc651'), grass2 = hex('#5aa13d'), dirt = hex('#7a5632'), dirtD = hex('#65462a'), dirtL = hex('#8d673f');
    for (let y = GROUND; y < H; y++) for (let x = 0; x < W; x++) {
      if (y < GROUND + 2) put(x, y, grass1);
      else if (y < GROUND + 3 || (y === GROUND + 3 && (x & 1))) put(x, y, grass2);
      else { const r = Math.random(); put(x, y, r < 0.12 ? dirtD : r < 0.17 ? dirtL : dirt); }
    }
    g.putImageData(img, 0, 0);

    const R = (x, y, w, h, col) => { g.fillStyle = col; g.fillRect(x, y, w, h); };
    // hangars
    HANGAR.forEach((c, team) => {
      const T = TEAM[team], wallTop = GROUND - 16;
      R(c - 15, wallTop, 30, 16, '#cfc5ae');
      for (let x = c - 15; x < c + 15; x += 5) R(x, wallTop, 1, 16, '#b7ad95');
      [[-15, 30], [-14, 28], [-12, 24], [-9, 18], [-5, 10]].forEach((r, i) => R(c + r[0], wallTop - 1 - i, r[1], 1, i % 2 ? '#7b7e88' : '#6a6d77'));
      R(c - 10, GROUND - 14, 20, 2, T.body);
      R(c - 9, GROUND - 12, 18, 12, '#2a2622');
      R(c - 9, GROUND - 12, 18, 1, '#171513');
      const px = team ? c + 18 : c - 18;
      R(px, GROUND - 30, 1, 30, '#e8e8e8');
      R(team ? px - 6 : px + 1, GROUND - 30, 6, 4, T.body);
      R(team ? px - 6 : px + 1, GROUND - 28, 6, 1, T.dark);
    });
    // barn in the middle
    R(152, 150, 16, 30, '#8b5e3c');
    for (let x = 155; x < 168; x += 4) R(x, 150, 1, 30, '#744d31');
    for (let i = 0; i < 7; i++) { const w = 4 + 2 * i; R(BARN_X - w / 2, 143 + i, w, 1, '#4a3426'); }
    R(158, 155, 4, 3, '#f2d27a');
    R(156, 168, 8, 12, '#3a281c');
    for (let i = 0; i < 8; i++) { R(156 + i, 168 + Math.round(i * 1.5), 1, 1, '#b59a6a'); R(163 - i, 168 + Math.round(i * 1.5), 1, 1, '#b59a6a'); }
    return c;
  }

  function makeCloud(w, h) {
    const c = mk(w, h), g = c.getContext('2d');
    const img = g.createImageData(w, h), d = img.data;
    const blobs = [];
    const n = 4 + Math.floor(Math.random() * 3);
    for (let i = 0; i < n; i++) {
      const r = rnd(h * 0.3, h * 0.5);
      blobs.push({ x: rnd(r, w - r), y: h - r - rnd(0, h * 0.25), r });
    }
    blobs.push({ x: w / 2, y: h * 0.62, r: h * 0.38 });
    const top = hex('#ffffff'), mid = hex('#eef5fb'), low = hex('#cfe0ee');
    for (let y = 0; y < h; y++) for (let x = 0; x < w; x++) {
      let inside = false, shade = 0;
      for (const b of blobs) {
        const dx = x + 0.5 - b.x, dy = y + 0.5 - b.y;
        if (dx * dx + dy * dy <= b.r * b.r) { inside = true; shade = Math.max(shade, (dy / b.r)); }
      }
      if (!inside) continue;
      const col = y > h - 4 ? low : shade > 0.35 ? mid : top;
      const i = (y * w + x) * 4; d[i] = col[0]; d[i + 1] = col[1]; d[i + 2] = col[2]; d[i + 3] = 255;
    }
    g.putImageData(img, 0, 0);
    return c;
  }

  // ---------- sprites ----------
  function planeBase(team, pilot, prop) {
    const c = mk(20, 12), g = c.getContext('2d'), T = TEAM[team];
    const R = (x, y, w, h, col) => { g.fillStyle = col; g.fillRect(x + 10, y + 6, w, h); };
    R(-9, -4, 2, 3, T.dark);
    R(-8, -1, 13, 2, T.body);
    R(-8, 1, 13, 1, T.dark);
    if (pilot) { R(-5, -3, 2, 1, '#3b2a1e'); R(-5, -2, 2, 1, '#f0c8a0'); }
    R(5, -2, 2, 4, '#5b606b');
    if (prop) R(7, -5, 1, 10, '#e2e2da'); else R(7, -2, 1, 4, '#9aa0a8');
    R(-3, -4, 9, 1, T.wing);
    R(-2, -3, 1, 2, '#2b2b30'); R(3, -3, 1, 2, '#2b2b30');
    R(-3, 2, 8, 1, T.wing);
    R(1, 3, 1, 1, '#2b2b30'); R(0, 4, 3, 1, '#16161a');
    return c;
  }
  function rotSprite(base, ang, flip) {
    const src = base.getContext('2d').getImageData(0, 0, 20, 12).data;
    const c = mk(24, 24), g = c.getContext('2d'), out = g.createImageData(24, 24), o = out.data;
    const ca = Math.cos(ang), sa = Math.sin(ang);
    for (let dy = 0; dy < 24; dy++) for (let dx = 0; dx < 24; dx++) {
      const rx = dx + 0.5 - 12, ry = dy + 0.5 - 12;
      const lx = rx * ca + ry * sa; let ly = -rx * sa + ry * ca;
      if (flip) ly = -ly;
      const sx = Math.floor(lx + 10), sy = Math.floor(ly + 6);
      if (sx < 0 || sy < 0 || sx >= 20 || sy >= 12) continue;
      const si = (sy * 20 + sx) * 4, di = (dy * 24 + dx) * 4;
      o[di] = src[si]; o[di + 1] = src[si + 1]; o[di + 2] = src[si + 2]; o[di + 3] = src[si + 3];
    }
    g.putImageData(out, 0, 0);
    return c;
  }
  // sprites[team][pilot][prop][flip][dir]
  const sprites = [0, 1].map((team) => [0, 1].map((pil) => [0, 1].map((prop) => {
    const base = planeBase(team, pil, prop);
    return [0, 1].map((flip) => Array.from({ length: DIRS }, (_, i) => rotSprite(base, i * TAU / DIRS, flip)));
  })));

  const bg = buildBg();
  const clouds = [
    { x: 40, y: 48, img: makeCloud(58, 20), v: 5, fg: true },
    { x: 190, y: 84, img: makeCloud(64, 22), v: 5, fg: true },
    { x: 110, y: 118, img: makeCloud(48, 18), v: 5, fg: true },
    { x: 260, y: 30, img: makeCloud(38, 14), v: 2.5, fg: false },
    { x: 150, y: 20, img: makeCloud(30, 12), v: 2.5, fg: false },
  ];

  // ---------- audio ----------
  let ac = null, muted = false;
  function audioOn() {
    if (!ac) { try { ac = new (window.AudioContext || window.webkitAudioContext)(); } catch (e) { ac = null; } }
    if (ac && ac.state === 'suspended') ac.resume().catch(() => {});
  }
  function tone(f1, f2, dur, type, vol) {
    const t = ac.currentTime, o = ac.createOscillator(), g = ac.createGain();
    o.type = type; o.frequency.setValueAtTime(f1, t); o.frequency.exponentialRampToValueAtTime(f2, t + dur);
    g.gain.setValueAtTime(vol, t); g.gain.exponentialRampToValueAtTime(0.0001, t + dur);
    o.connect(g).connect(ac.destination); o.start(t); o.stop(t + dur + 0.02);
  }
  function noise(dur, vol, freq) {
    const t = ac.currentTime, len = Math.floor(ac.sampleRate * dur), buf = ac.createBuffer(1, len, ac.sampleRate), ch = buf.getChannelData(0);
    for (let i = 0; i < len; i++) ch[i] = Math.random() * 2 - 1;
    const s = ac.createBufferSource(), f = ac.createBiquadFilter(), g = ac.createGain();
    s.buffer = buf; f.type = 'lowpass'; f.frequency.value = freq;
    g.gain.setValueAtTime(vol, t); g.gain.exponentialRampToValueAtTime(0.0001, t + dur);
    s.connect(f).connect(g).connect(ac.destination); s.start(t);
  }
  function sfx(name) {
    if (!ac || muted || game.mode !== 'play') return;
    try {
      if (name === 'shot') tone(900, 380, 0.05, 'square', 0.035);
      else if (name === 'hit') noise(0.09, 0.25, 2400);
      else if (name === 'boom') { noise(0.7, 0.5, 700); tone(120, 40, 0.5, 'sine', 0.3); }
      else if (name === 'eject') tone(300, 900, 0.18, 'square', 0.05);
      else if (name === 'chute') tone(500, 250, 0.25, 'triangle', 0.08);
      else if (name === 'land') noise(0.06, 0.12, 600);
      else if (name === 'fix') { tone(660, 660, 0.08, 'square', 0.04); setTimeout(() => ac && tone(990, 990, 0.1, 'square', 0.04), 90); }
      else if (name === 'point') { tone(520, 520, 0.09, 'square', 0.05); setTimeout(() => ac && tone(780, 780, 0.14, 'square', 0.05), 100); }
    } catch (e) { /* audio is optional */ }
  }

  // ---------- state ----------
  const game = { mode: 'attract', paused: false, over: false, vsBot: true, target: 10, playTime: 0, stats: null };
  // Статистика красного (игрок 1) для отправки на сервер
  const track = (k) => { if (game.mode === 'play' && game.stats) game.stats[k]++; };
  let players, planes, pilots, bullets, parts;

  function newPlane(team) {
    return { team, x: HANGAR[team], y: GROUND - 5, a: team ? Math.PI : 0, speed: 0, thr: 0, state: 'ground', hp: 2, pilot: true, vx: 0, vy: 0, cd: 0, prop: 0, repair: 0, smoke: 0, dead: false };
  }
  function resetWorld(bots) {
    planes = []; pilots = []; bullets = []; parts = [];
    players = [0, 1].map((team) => {
      const pl = newPlane(team); planes.push(pl);
      return { team, plane: pl, pilot: null, respawn: 0, score: 0, bot: bots[team], ai: { t: Math.random() * 10 } };
    });
  }

  // ---------- effects ----------
  function part(x, y, vx, vy, life, color, grav = 0, size = 1) { parts.push({ x, y, vx, vy, t: life, life, color, grav, size }); }
  function explosion(x, y, team) {
    const fire = ['#fff3b0', '#ffd23f', '#ff8c2a', '#e2412b', '#6b6b6b'];
    for (let i = 0; i < 26; i++) { const a = rnd(0, TAU), s = rnd(15, 90); part(x, y, Math.cos(a) * s, Math.sin(a) * s, rnd(0.3, 0.9), fire[i % fire.length], 30, Math.random() < 0.3 ? 2 : 1); }
    for (let i = 0; i < 8; i++) part(x, y, rnd(-60, 60), rnd(-90, -20), rnd(0.8, 1.6), i % 2 ? TEAM[team].body : '#3a3a40', 170, 2);
    for (let i = 0; i < 8; i++) part(x + rnd(-4, 4), y + rnd(-4, 4), rnd(-8, 8), rnd(-25, -8), rnd(0.8, 1.4), '#8a8f98', -5, 2);
  }
  function sparks(x, y) { for (let i = 0; i < 6; i++) part(x, y, rnd(-50, 50), rnd(-50, 50), rnd(0.1, 0.3), i % 2 ? '#fff6c0' : '#ffb13b'); }
  function dust(x, y) { for (let i = 0; i < 3; i++) part(x, y, rnd(-20, 20), rnd(-30, -10), rnd(0.2, 0.4), '#9b7a4e', 120); }

  let toastTimer = 0;
  function toast(msg) {
    if (game.mode !== 'play') return;
    const el = $('toast'); el.textContent = msg; el.classList.remove('hide'); toastTimer = 1.8;
  }

  // ---------- scoring ----------
  function pilotDied(team, msg, byTeam) {
    if (team === 0) track('deaths');
    else if (byTeam === 0) track('kills');
    const pl = players[team];
    pl.pilot = null; pl.plane = pl.plane && pl.plane.dead ? null : pl.plane; pl.respawn = 2.2;
    const other = players[1 - team];
    other.score++;
    if (game.mode === 'attract') { if (other.score >= 99) players.forEach((p) => p.score = 0); return; }
    sfx('point');
    toast(msg + ' · +1 ' + TEAM[1 - team].name.toLowerCase());
    if (other.score >= game.target) setTimeout(() => endMatch(1 - team), 900);
  }
  function destroyPlane(pl, byTeam) {
    if (pl.dead) return;
    pl.dead = true;
    explosion(pl.x, pl.y, pl.team); sfx('boom');
    const owner = players[pl.team];
    if (owner.plane === pl) owner.plane = null;
    if (pl.pilot) {
      const msg = byTeam === 1 - pl.team ? (byTeam === 0 ? 'Красные сбили синих' : 'Синие сбили красных') : (TEAM[pl.team].name + ' разбились');
      pilotDied(pl.team, msg, byTeam);
    }
  }
  function killPilot(pt, msg, byTeam) {
    if (pt.dead) return;
    pt.dead = true;
    for (let i = 0; i < 8; i++) part(pt.x, pt.y, rnd(-40, 40), rnd(-60, -10), rnd(0.4, 0.8), i % 2 ? '#b8322a' : TEAM[pt.team].body, 150);
    sfx('hit');
    pilotDied(pt.team, msg, byTeam);
  }

  // ---------- physics ----------
  function hitBarn(x, y, r) { return Math.abs(wrapDx(BARN_X, x)) < 8 + r && y + r >= 147 && y - r < GROUND; }

  function updatePlane(pl, I, dt) {
    I = I || {};
    pl.cd -= dt;
    pl.prop += dt * (4 + pl.thr * 22);
    if (!pl.pilot) {
      pl.thr = Math.max(0, pl.thr - 0.35 * dt);
      if (pl.state === 'air') pl.a += (Math.cos(pl.a) >= 0 ? 1 : -1) * 0.6 * dt;
      I = {};
    }
    if (I.up) pl.thr = Math.min(1, pl.thr + dt * 1.1);
    if (I.down) pl.thr = Math.max(0, pl.thr - dt * 1.1);
    const turn = (I.right ? 1 : 0) - (I.left ? 1 : 0);

    if (pl.state === 'ground') {
      const face = Math.cos(pl.a) > 0 ? 1 : -1;
      pl.speed += (P.THRUST * pl.thr - 1.2 * pl.speed - (pl.thr < 0.05 ? 14 : 0)) * dt;
      if (pl.speed < 0) pl.speed = 0;
      pl.vx = face * pl.speed; pl.vy = 0;
      pl.x += pl.vx * dt; pl.y = GROUND - 5;
      const noseUp = face > 0 ? turn < 0 : turn > 0;
      if (noseUp && pl.speed > P.TAKEOFF) { pl.state = 'air'; pl.a += turn * 0.12; pl.y -= 1; }
      if (pl.pilot && pl.hp < 2 && pl.speed < 8 && Math.abs(wrapDx(pl.x, HANGAR[pl.team])) < 10) {
        pl.repair += dt;
        if (pl.repair > 1.2) { pl.hp = 2; pl.repair = 0; sfx('fix'); if (!players[pl.team].bot) toast('Самолёт починен'); }
      } else pl.repair = 0;
    } else if (pl.state === 'air') {
      pl.a += turn * P.TURN * dt;
      pl.speed += (P.THRUST * pl.thr - P.DRAG * pl.speed + P.GALONG * Math.sin(pl.a)) * dt;
      pl.speed = Math.min(pl.speed, P.MAXS);
      pl.vx = Math.cos(pl.a) * pl.speed; pl.vy = Math.sin(pl.a) * pl.speed;
      pl.x += pl.vx * dt; pl.y += pl.vy * dt;
      if (pl.speed < P.STALL || pl.y < 6) { pl.state = 'stall'; if (pl.y < 6) pl.vy = Math.max(pl.vy, 10); }
    } else if (pl.state === 'stall') {
      pl.vy += P.GRAV * dt; pl.vx *= 1 - 0.4 * dt;
      pl.vx += Math.cos(pl.a) * P.THRUST * pl.thr * 0.4 * dt;
      pl.vy += Math.sin(pl.a) * P.THRUST * pl.thr * 0.4 * dt;
      const target = Math.atan2(pl.vy, pl.vx);
      pl.a += clamp(angDiff(target, pl.a), -2.5 * dt, 2.5 * dt) + turn * P.TURN * 0.35 * dt;
      pl.x += pl.vx * dt; pl.y += pl.vy * dt;
      pl.speed = Math.hypot(pl.vx, pl.vy);
      if (pl.speed > P.STALL * 1.5 && Math.sin(pl.a) > 0.25 && pl.y > 12) pl.state = 'air';
    }
    pl.a = norm(pl.a); pl.x = wrapX(pl.x);

    if (I.fire && pl.pilot && pl.cd <= 0) {
      pl.cd = P.FIRECD;
      const ca = Math.cos(pl.a), sa = Math.sin(pl.a);
      bullets.push({ x: wrapX(pl.x + ca * 10), y: pl.y + sa * 10, vx: ca * P.BULLET + pl.vx, vy: sa * P.BULLET + pl.vy, t: P.BLIFE, team: pl.team });
      if (pl.team === 0) track('shots');
      sfx('shot');
    }

    if (pl.state !== 'ground' && pl.y + 4 >= GROUND) {
      const soft = pl.pilot && Math.abs(Math.sin(pl.a)) < 0.28 && pl.vy < 55 && pl.speed < 80;
      if (soft) { pl.state = 'ground'; pl.a = Math.cos(pl.a) > 0 ? 0 : Math.PI; pl.y = GROUND - 5; pl.speed = Math.abs(pl.vx); sfx('land'); }
      else destroyPlane(pl, null);
    }
    if (!pl.dead && hitBarn(pl.x, pl.y, 4)) destroyPlane(pl, null);

    if (!pl.dead && pl.hp < 2) {
      pl.smoke -= dt;
      if (pl.smoke <= 0) { pl.smoke = 0.07; part(pl.x - Math.cos(pl.a) * 6, pl.y, rnd(-4, 4), rnd(-14, -4), rnd(0.6, 1), Math.random() < 0.5 ? '#5d6068' : '#8a8f98', -4, 2); }
    }
  }

  function eject(p) {
    const pl = p.plane;
    if (!pl || !pl.pilot || pl.state === 'ground') return;
    pl.pilot = false;
    const pt = { team: p.team, x: pl.x, y: pl.y - 4, vx: pl.vx * 0.5, vy: pl.vy * 0.5 - 55, state: 'fall', t: 0, dir: 1, step: 0, dead: false };
    pilots.push(pt);
    p.plane = null; p.pilot = pt;
    if (p.team === 0) track('ejects');
    sfx('eject');
  }

  function updatePilot(p, I, dt) {
    const pt = p.pilot;
    pt.t += dt;
    const mv = (I.right ? 1 : 0) - (I.left ? 1 : 0);
    if (pt.state === 'fall') {
      pt.vy += P.GRAV * dt; pt.vx *= 1 - 0.5 * dt;
      if (I.eject && pt.t > 0.15) { pt.state = 'chute'; sfx('chute'); }
    } else if (pt.state === 'chute') {
      pt.vy += (22 - pt.vy) * 3 * dt; pt.vx += (mv * 28 - pt.vx) * 2 * dt;
    } else {
      pt.vx = mv * 18; pt.vy = 0; pt.y = GROUND - 3;
      if (mv) { pt.dir = mv; pt.step += dt; }
    }
    pt.x = wrapX(pt.x + pt.vx * dt); pt.y += pt.vy * dt;
    if (pt.y < 2) { pt.y = 2; pt.vy = Math.max(0, pt.vy); }
    if (pt.state !== 'walk' && pt.y + 2 >= GROUND) {
      if (pt.state === 'fall' && pt.vy > 75) { killPilot(pt, TEAM[pt.team].name + ': парашют не раскрылся'); return; }
      pt.state = 'walk'; pt.y = GROUND - 3; pt.vx = 0; sfx('land');
    }
    if (pt.state === 'walk' && Math.abs(wrapDx(pt.x, HANGAR[pt.team])) < 3) {
      pt.dead = true; p.pilot = null; p.respawn = 0.5;
      if (!p.bot) toast('Пилот добрался до ангара');
    }
  }

  // ---------- input ----------
  const keys = new Set(), tapped = new Set();
  const MAP = [
    { left: ['KeyA'], right: ['KeyD'], up: ['KeyW'], down: ['KeyS'], fire: ['Space'], eject: ['KeyE'] },
    { left: ['ArrowLeft'], right: ['ArrowRight'], up: ['ArrowUp'], down: ['ArrowDown'], fire: ['Slash', 'Enter', 'NumpadEnter'], eject: ['Period', 'ShiftRight'] },
  ];
  const GAME_KEYS = new Set(Object.values(MAP[0]).flat().concat(Object.values(MAP[1]).flat()));
  const touch = { left: false, right: false, up: false, down: false, fire: false }; let touchEject = false;

  addEventListener('keydown', (e) => {
    if (e.code === 'KeyP' || e.code === 'Escape') { if (game.mode === 'play' && !game.over) togglePause(); return; }
    if (game.mode === 'play' && !game.paused && GAME_KEYS.has(e.code)) { e.preventDefault(); audioOn(); }
    if (!e.repeat) tapped.add(e.code);
    keys.add(e.code);
  });
  addEventListener('keyup', (e) => keys.delete(e.code));
  addEventListener('blur', () => keys.clear());

  function humanInput(team) {
    const maps = team === 0 && game.vsBot ? [MAP[0], MAP[1]] : [MAP[team]];
    const any = (k, set) => maps.some((m) => m[k].some((c) => set.has(c)));
    const I = { left: any('left', keys), right: any('right', keys), up: any('up', keys), down: any('down', keys), fire: any('fire', keys), eject: any('eject', tapped) };
    if (team === 0) { for (const k in touch) I[k] = I[k] || touch[k]; I.eject = I.eject || touchEject; }
    return I;
  }

  function botInput(p, dt) {
    const I = {}, me = p.plane, en = players[1 - p.team];
    p.ai.t += dt;
    if (me) {
      I.up = true;
      const face = Math.cos(me.a) >= 0 ? 1 : -1;
      if (me.state === 'ground') {
        if (me.hp < 2 && Math.abs(wrapDx(me.x, HANGAR[p.team])) < 10) { I.up = false; I.down = true; return I; }
        if (me.speed > P.TAKEOFF + 5) { if (face > 0) I.left = true; else I.right = true; }
        return I;
      }
      if (me.state === 'stall') {
        const d = angDiff(Math.PI / 2, me.a);
        if (d < -0.1) I.left = true; else if (d > 0.1) I.right = true;
        return I;
      }
      let tgt = null;
      if (en.plane) tgt = en.plane; else if (en.pilot) tgt = en.pilot;
      let desired = face > 0 ? -0.2 : Math.PI + 0.2, aim = 0, dd = 999;
      if (tgt) {
        const dx0 = wrapDx(me.x, tgt.x), dy0 = tgt.y - me.y;
        dd = Math.hypot(dx0, dy0);
        const lead = dd / P.BULLET;
        aim = Math.atan2(dy0 + tgt.vy * lead, dx0 + tgt.vx * lead);
        desired = aim + Math.sin(p.ai.t * 1.7) * 0.2;
      }
      const alt = GROUND - me.y;
      if (alt < 45 && Math.sin(desired) > -0.1) desired = face > 0 ? -0.5 : Math.PI + 0.5;
      if (alt < 28) desired = face > 0 ? -0.9 : Math.PI + 0.9;
      if (me.y < 22) desired = face > 0 ? 0.35 : Math.PI - 0.35;
      const bdx = wrapDx(me.x, BARN_X) * face;
      if (bdx > 0 && bdx < 55 && me.y > 125) desired = face > 0 ? -0.8 : Math.PI + 0.8;
      if (me.speed < P.STALL + 10 && alt > 50) desired = face > 0 ? 0.6 : Math.PI - 0.6;
      const d = angDiff(desired, me.a);
      if (d < -0.06) I.left = true; else if (d > 0.06) I.right = true;
      if (tgt && Math.abs(angDiff(aim, me.a)) < 0.14 && dd < 140) I.fire = true;
      if (me.hp === 1 && alt > 70 && dd < 60 && Math.random() < 0.004) I.eject = true;
    } else if (p.pilot) {
      const pt = p.pilot;
      if (pt.state === 'fall' && pt.t > 0.45) I.eject = true;
      const dx = wrapDx(pt.x, HANGAR[p.team]);
      if (dx > 1) I.right = true; else if (dx < -1) I.left = true;
    }
    return I;
  }

  // ---------- step ----------
  function step(dt, inputs) {
    if (game.mode === 'play') game.playTime += dt;
    players.forEach((p, i) => {
      const I = inputs[i];
      if (p.plane && I.eject && p.plane.pilot && p.plane.state !== 'ground') { eject(p); I.eject = false; }
    });
    for (const pl of planes) {
      const owner = players[pl.team];
      updatePlane(pl, owner.plane === pl ? inputs[pl.team] : null, dt);
    }
    players.forEach((p, i) => { if (p.pilot && !p.pilot.dead) updatePilot(p, inputs[i], dt); });

    // bullets
    for (const b of bullets) {
      b.x = wrapX(b.x + b.vx * dt); b.y += b.vy * dt; b.t -= dt;
      if (b.y >= GROUND) { b.t = 0; dust(b.x, GROUND - 1); continue; }
      if (hitBarn(b.x, b.y, 0)) { b.t = 0; dust(b.x, b.y); continue; }
      for (const pl of planes) {
        if (pl.dead || pl.team === b.team) continue;
        if (Math.hypot(wrapDx(pl.x, b.x), b.y - pl.y) < 6) {
          b.t = 0; sparks(b.x, b.y); sfx('hit');
          if (b.team === 0) track('hits');
          if (--pl.hp <= 0) destroyPlane(pl, b.team);
          break;
        }
      }
      if (b.t <= 0) continue;
      for (const pt of pilots) {
        if (pt.dead || pt.team === b.team) continue;
        if (Math.hypot(wrapDx(pt.x, b.x), b.y - (pt.y - (pt.state === 'chute' ? 2 : 0))) < 3.5) {
          b.t = 0; if (b.team === 0) track('hits');
          killPilot(pt, TEAM[b.team].name + ' подстрелили пилота', b.team);
          break;
        }
      }
    }
    // plane vs plane, plane vs pilot
    for (let i = 0; i < planes.length; i++) for (let j = i + 1; j < planes.length; j++) {
      const a = planes[i], b = planes[j];
      if (a.dead || b.dead || (a.state === 'ground' && b.state === 'ground')) continue;
      if (dist(a, b) < 9) { destroyPlane(a, null); destroyPlane(b, null); }
    }
    for (const pl of planes) for (const pt of pilots) {
      if (pl.dead || pt.dead || pl.team === pt.team) continue;
      if (dist(pl, pt) < 6) killPilot(pt, TEAM[pl.team].name + ' задели пилота винтом', pl.team);
    }

    bullets = bullets.filter((b) => b.t > 0);
    planes = planes.filter((p) => !p.dead);
    pilots = pilots.filter((p) => !p.dead);
    players.forEach((p) => { if (p.plane && p.plane.dead) p.plane = null; if (p.pilot && p.pilot.dead) p.pilot = null; });

    // respawn
    players.forEach((p) => {
      if (p.plane || p.pilot) return;
      p.respawn -= dt;
      if (p.respawn <= 0) { const pl = newPlane(p.team); planes.push(pl); p.plane = pl; }
    });

    for (const q of parts) {
      q.vy += q.grav * dt; q.x += q.vx * dt; q.y += q.vy * dt; q.t -= dt;
      if (q.y > GROUND - 1 && q.grav > 0) { q.y = GROUND - 1; q.vy *= -0.3; q.vx *= 0.6; }
    }
    parts = parts.filter((q) => q.t > 0);
    for (const c of clouds) c.x = wrapX(c.x + c.v * dt);
  }

  // ---------- draw ----------
  function drawWrapped(x, margin, fn) {
    fn(x);
    if (x < margin) fn(x + W);
    if (x > W - margin) fn(x - W);
  }
  function drawCloud(c) {
    const w = c.img.width;
    const x = Math.round(c.x);
    ctx.drawImage(c.img, x, c.y);
    if (x + w > W) ctx.drawImage(c.img, x - W, c.y);
  }
  function drawPlane(pl) {
    const idx = ((Math.round(pl.a / TAU * DIRS) % DIRS) + DIRS) % DIRS;
    const flip = Math.cos(pl.a) < 0 ? 1 : 0;
    const prop = pl.thr > 0.02 || pl.speed > 5 ? (Math.floor(pl.prop) & 1) : 1;
    const spr = sprites[pl.team][pl.pilot ? 1 : 0][prop][flip][idx];
    drawWrapped(pl.x, 12, (x) => ctx.drawImage(spr, Math.round(x) - 12, Math.round(pl.y) - 12));
  }
  function drawPilot(pt) {
    const T = TEAM[pt.team];
    drawWrapped(pt.x, 8, (fx) => {
      const x = Math.round(fx), y = Math.round(pt.y);
      const R = (dx, dy, w, h, c) => { ctx.fillStyle = c; ctx.fillRect(x + dx, y + dy, w, h); };
      if (pt.state === 'chute') {
        for (let i = 0; i < 11; i++) R(i - 5, -11, 1, 1, i % 4 < 2 ? '#f4f1ea' : T.body);
        for (let i = 0; i < 13; i++) R(i - 6, -10, 1, 2, i % 4 < 2 ? '#f4f1ea' : T.body);
        R(-6, -8, 1, 1, '#c9c2b2'); R(6, -8, 1, 1, '#c9c2b2');
        for (let k = 1; k < 6; k++) { R(-6 + k, -8 + k, 1, 1, '#d8d2c4'); R(6 - k, -8 + k, 1, 1, '#d8d2c4'); }
      }
      R(0, -2, 1, 1, '#3b2a1e');
      R(-1, -1, 3, 1, T.body);
      R(0, 0, 1, 1, T.dark);
      if (pt.state === 'walk') {
        const f = Math.floor(pt.step * 8) & 1;
        R(f ? -1 : 0, 1, 1, 1, '#2b2b30'); R(f ? 1 : 0, 1, 1, 1, '#2b2b30');
      } else { R(-1, 1, 1, 1, '#2b2b30'); R(1, 1, 1, 1, '#2b2b30'); }
    });
  }
  function draw() {
    ctx.drawImage(bg, 0, 0);
    clouds.filter((c) => !c.fg).forEach(drawCloud);
    for (const pt of pilots) drawPilot(pt);
    for (const pl of planes) drawPlane(pl);
    ctx.fillStyle = '#1f232b';
    for (const b of bullets) ctx.fillRect(Math.round(b.x), Math.round(b.y), 2, 2);
    for (const q of parts) {
      ctx.globalAlpha = q.color === '#8a8f98' || q.color === '#5d6068' ? Math.min(1, q.t / q.life * 1.4) : 1;
      ctx.fillStyle = q.color; ctx.fillRect(Math.round(q.x), Math.round(q.y), q.size, q.size);
    }
    ctx.globalAlpha = 1;
    clouds.filter((c) => c.fg).forEach(drawCloud);
  }

  // ---------- HUD ----------
  const hudCache = [{}, {}];
  function stateText(p) {
    if (p.plane) {
      const pl = p.plane;
      if (pl.state === 'ground') return pl.repair > 0 ? 'ремонт…' : pl.speed > 1 ? 'разбег' : 'на земле';
      return pl.state === 'stall' ? 'сваливание!' : 'в воздухе';
    }
    if (p.pilot) return p.pilot.state === 'fall' ? 'падает — парашют!' : p.pilot.state === 'chute' ? 'парашют' : 'бежит в ангар';
    return 'новый самолёт…';
  }
  function updateHud() {
    players.forEach((p, i) => {
      const c = hudCache[i];
      const thr = p.plane ? Math.round(p.plane.thr * 100) : 0;
      const hp = p.plane ? p.plane.hp : 0;
      const st = stateText(p);
      if (c.score !== p.score) { $('s' + i).textContent = c.score = p.score; }
      if (c.thr !== thr) { $('t' + i).style.width = (c.thr = thr) + '%'; }
      if (c.hp !== hp) { c.hp = hp; [...$('h' + i).children].forEach((el, k) => el.classList.toggle('off', k >= hp)); }
      if (c.st !== st) { $('st' + i).textContent = c.st = st; }
    });
  }

  // ---------- screens ----------
  const overlay = $('overlay');
  function showOverlay(html) { overlay.innerHTML = html; overlay.hidden = false; }
  function menu() {
    game.mode = 'attract'; game.paused = false; game.over = false;
    resetWorld([true, true]);
    $('hud').hidden = true;
    const seg = [5, 10, 15].map((n) => `<button type="button" data-target="${n}" aria-pressed="${n === game.target}">${n}</button>`).join('');
    showOverlay(`<div class="card">
      <div class="logo"><span class="r">БИ</span>ПЛА<span class="b">НЫ</span></div>
      <p>Дуэль двух бипланов над одним полем. Сбей соперника, а если подбили тебя, прыгай с парашютом и беги в ангар за новым самолётом.</p>
      <div class="row">
        <button class="btn primary" type="button" data-act="bot">Против бота</button>
        <button class="btn" type="button" data-act="duo">Вдвоём</button>
      </div>
      <div class="seg">Играть до ${seg} очков</div>
    </div>`);
  }
  function startMatch(vsBot, target, scores) {
    game.mode = 'play'; game.vsBot = vsBot; game.target = target; game.paused = false; game.over = false;
    resetWorld([false, vsBot]);
    if (scores) players.forEach((p, i) => p.score = scores[i] || 0);
    game.playTime = 0;
    game.stats = { shots: 0, hits: 0, kills: 0, deaths: 0, ejects: 0 };
    hudCache[0] = {}; hudCache[1] = {};
    $('hud').hidden = false;
    overlay.hidden = true; overlay.innerHTML = '';
    if (document.activeElement && document.activeElement.blur) document.activeElement.blur();
    keys.clear();
  }
  function endMatch(team) {
    if (game.mode !== 'play' || game.over) return;
    game.over = true;
    const s = players.map((p) => p.score);
    const title = game.vsBot ? (team === 0 ? 'Победа!' : 'Бот победил') : 'Победили ' + TEAM[team].name.toLowerCase();
    showOverlay(`<div class="card">
      <div class="big" style="color:${team ? 'var(--blue)' : 'var(--red)'}">${title}</div>
      <div class="logo"><span class="r">${s[0]}</span> : <span class="b">${s[1]}</span></div>
      <div class="row">
        <button class="btn primary" type="button" data-act="again">Ещё раз</button>
        <button class="btn" type="button" data-act="menu">В меню</button>
      </div>
      <p class="rate" id="rateLine"></p>
    </div>`);
    if (window.bpAccount) {
      window.bpAccount.reportMatch({
        mode: game.vsBot ? 'bot' : 'duo', target: game.target, myScore: s[0], oppScore: s[1],
        durationMs: Math.round(game.playTime * 1000), ...game.stats,
      }, $('rateLine'));
    }
  }
  function togglePause() {
    if (game.mode !== 'play' || game.over) return;
    game.paused = !game.paused;
    if (game.paused) {
      showOverlay(`<div class="card"><div class="big">Пауза</div>
        <div class="row"><button class="btn primary" type="button" data-act="resume">Продолжить</button>
        <button class="btn" type="button" data-act="menu">В меню</button></div></div>`);
    } else { overlay.hidden = true; overlay.innerHTML = ''; keys.clear(); }
  }
  overlay.addEventListener('click', (e) => {
    const t = e.target.closest('button');
    if (!t) return;
    audioOn();
    if (t.dataset.target) {
      game.target = +t.dataset.target;
      overlay.querySelectorAll('[data-target]').forEach((b) => b.setAttribute('aria-pressed', String(b === t)));
      return;
    }
    const act = t.dataset.act;
    if ((act === 'bot' || act === 'duo' || act === 'again') && stage.classList.contains('touch')) setFull(true);
    if (act === 'bot') startMatch(true, game.target);
    else if (act === 'duo') startMatch(false, game.target);
    else if (act === 'again') startMatch(game.vsBot, game.target);
    else if (act === 'menu') menu();
    else if (act === 'resume') togglePause();
  });
  $('pauseBtn').addEventListener('click', (e) => { togglePause(); e.currentTarget.blur(); });
  $('muteBtn').addEventListener('click', (e) => {
    muted = !muted; audioOn();
    e.currentTarget.textContent = muted ? 'Звук: выкл' : 'Звук: вкл';
    e.currentTarget.blur();
  });

  // ---------- touch pads: multitouch, finger can slide between buttons ----------
  const stage = $('stage');
  const isTouch = () => matchMedia('(pointer: coarse)').matches || navigator.maxTouchPoints > 0;
  if (isTouch()) stage.classList.add('touch');
  addEventListener('touchstart', () => stage.classList.add('touch'), { once: true, passive: true });

  const padButtons = [...document.querySelectorAll('.pad button')];
  const fingers = new Map(); // pointerId -> key under the finger
  const keyAt = (x, y) => { const el = document.elementFromPoint(x, y); const b = el && el.closest('.pad button'); return b ? b.dataset.k : null; };
  function syncTouch() {
    for (const k in touch) touch[k] = false;
    const held = new Set(fingers.values());
    for (const k of held) if (k && k !== 'eject') touch[k] = true;
    padButtons.forEach((b) => b.classList.toggle('on', held.has(b.dataset.k)));
  }
  function press(id, k) {
    if (fingers.get(id) === k) return;
    fingers.set(id, k);
    if (k === 'eject') touchEject = true;
    syncTouch();
  }
  document.querySelectorAll('.pad').forEach((pad) => {
    pad.addEventListener('pointerdown', (e) => {
      e.preventDefault(); audioOn();
      try { pad.setPointerCapture(e.pointerId); } catch (err) { /* not critical */ }
      fingers.delete(e.pointerId);
      press(e.pointerId, keyAt(e.clientX, e.clientY));
    });
    pad.addEventListener('pointermove', (e) => { if (fingers.has(e.pointerId)) press(e.pointerId, keyAt(e.clientX, e.clientY)); });
    const lift = (e) => { fingers.delete(e.pointerId); syncTouch(); };
    pad.addEventListener('pointerup', lift);
    pad.addEventListener('pointercancel', lift);
    pad.addEventListener('contextmenu', (e) => e.preventDefault());
  });
  addEventListener('blur', () => { fingers.clear(); syncTouch(); });

  // ---------- full screen ----------
  function setFull(on) {
    if (on === stage.classList.contains('full')) return;
    stage.classList.toggle('full', on);
    document.body.classList.toggle('locked', on);
    $('fullBtn').textContent = on ? 'Свернуть' : 'Во весь экран';
    if (on) {
      const req = stage.requestFullscreen || stage.webkitRequestFullscreen;
      if (req) {
        try {
          const r = req.call(stage);
          if (r && r.then) r.then(() => screen.orientation && screen.orientation.lock && screen.orientation.lock('landscape').catch(() => {})).catch(() => {});
        } catch (err) { /* CSS full mode still works */ }
      }
    } else if (document.fullscreenElement || document.webkitFullscreenElement) {
      try { (document.exitFullscreen || document.webkitExitFullscreen).call(document); } catch (err) { /* ignore */ }
    }
  }
  const onFsChange = () => { if (!(document.fullscreenElement || document.webkitFullscreenElement)) setFull(false); };
  document.addEventListener('fullscreenchange', onFsChange);
  document.addEventListener('webkitfullscreenchange', onFsChange);
  $('fullBtn').addEventListener('click', (e) => { setFull(!stage.classList.contains('full')); e.currentTarget.blur(); });
  $('fbExit').addEventListener('click', () => setFull(false));
  $('fbPause').addEventListener('click', () => togglePause());

  // ---------- loop ----------
  const DT = 1 / 60;
  let acc = 0, last = 0;
  function frame(now) {
    const el = Math.min(0.1, (now - last) / 1000 || 0);
    last = now;
    if (!game.paused && !game.over) {
      acc += el;
      const inputs = players.map((p) => (p.bot ? botInput(p, el) : humanInput(p.team)));
      tapped.clear(); touchEject = false;
      while (acc >= DT) {
        step(DT, inputs);
        inputs.forEach((I) => { I.eject = false; });
        acc -= DT;
      }
    } else { tapped.clear(); touchEject = false; }
    if (toastTimer > 0) { toastTimer -= el; if (toastTimer <= 0) $('toast').classList.add('hide'); }
    draw();
    if (game.mode === 'play') updateHud();
    requestAnimationFrame(frame);
  }

  function boot(data) {
    data = data || {};
    if (data.running) startMatch(!!data.vsBot, data.target || 10, data.scores);
    else menu();
    requestAnimationFrame((t) => { last = t; requestAnimationFrame(frame); });
  }
  const hot = window.claude && window.claude.hot;
  if (hot && typeof hot.snapshot === 'function') {
    try { hot.snapshot(() => ({ running: game.mode === 'play' && !game.over, vsBot: game.vsBot, target: game.target, scores: players.map((p) => p.score) })); } catch (e) { /* optional */ }
  }
  if (hot && typeof hot.ready === 'function') hot.ready(boot); else boot(hot && hot.data ? hot.data : {});
})();
