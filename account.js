// Аккаунт: вход через Telegram (бот или Mini App), отправка матчей, рейтинг.
(() => {
  'use strict';
  const API = '/api';
  const TOKEN_KEY = 'bp_token';
  const tg = window.Telegram && window.Telegram.WebApp;
  const inMiniApp = !!(tg && tg.initData);

  let token = null;
  let me = null; // ответ /api/me
  let pending = null; // результат матча, сыгранного до входа
  let login = null; // { nonce, secret, url, timer, deadline }
  let boardKind = 'bot';
  let markReady;
  const ready = new Promise((r) => { markReady = r; });

  try { token = localStorage.getItem(TOKEN_KEY); } catch (e) { token = null; }
  const saveToken = (t) => { token = t; try { t ? localStorage.setItem(TOKEN_KEY, t) : localStorage.removeItem(TOKEN_KEY); } catch (e) { /* private mode */ } };

  const $ = (id) => document.getElementById(id);
  function h(tag, attrs, ...kids) {
    const el = document.createElement(tag);
    for (const [k, v] of Object.entries(attrs || {})) {
      if (v == null || v === false) continue;
      if (k === 'class') el.className = v;
      else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
      else el.setAttribute(k, v === true ? '' : v);
    }
    for (const k of kids.flat()) if (k != null && k !== false) el.append(k instanceof Node ? k : String(k));
    return el;
  }

  // replaceChildren превращает null в текст «null» — пропускаем пустые значения, как h().
  const fill = (el, ...kids) => el.replaceChildren(...kids.flat().filter((k) => k != null && k !== false));

  async function api(path, { method = 'GET', body } = {}) {
    const headers = {};
    if (body) headers['Content-Type'] = 'application/json';
    if (token) headers.Authorization = 'Bearer ' + token;
    let res;
    try {
      res = await fetch(API + path, { method, headers, body: body ? JSON.stringify(body) : undefined });
    } catch (e) {
      return { status: 0, data: { error: 'Нет связи с сервером' } };
    }
    let data = null;
    try { data = await res.json(); } catch (e) { data = null; }
    if (res.status === 401 && token && path !== '/auth/webapp') { saveToken(null); me = null; render(); }
    return { status: res.status, data };
  }

  const name = (u) => [u.firstName, u.lastName].filter(Boolean).join(' ') || (u.username ? '@' + u.username : 'Пилот');
  const signed = (n) => (n > 0 ? '+' : n < 0 ? '−' : '±') + Math.abs(n);

  // ---------- session ----------
  async function refreshMe() {
    if (!token) { me = null; return; }
    const r = await api('/me');
    me = r.status === 200 ? r.data : null;
  }
  async function onLoggedIn(data) {
    saveToken(data.token);
    await refreshMe();
    render();
    loadBoard();
    dispatchEvent(new Event('bp:login'));
    if (pending) {
      const { result, el } = pending; pending = null;
      reportMatch(result, el && el.isConnected ? el : null);
    }
  }
  async function logout() {
    await api('/auth/logout', { method: 'POST' });
    saveToken(null); me = null;
    render(); loadBoard();
  }

  // ---------- login through the bot ----------
  function closeLogin() {
    if (login) clearInterval(login.timer);
    login = null;
    $('loginModal').hidden = true;
  }
  async function startLogin() {
    if (inMiniApp) return;
    const modal = $('loginModal');
    modal.hidden = false;
    $('loginStatus').textContent = 'Готовлю ссылку…';
    $('loginLink').removeAttribute('href');
    const r = await api('/auth/login', { method: 'POST' });
    if (r.status !== 200) { $('loginStatus').textContent = 'Не получилось начать вход. Попробуй ещё раз через минуту.'; return; }
    const { nonce, secret, url, bot, expiresIn } = r.data;
    $('loginLink').href = url;
    $('loginLink').textContent = 'Открыть @' + bot;
    login = { nonce, secret, deadline: Date.now() + expiresIn * 1000, busy: false };
    const tick = async () => {
      if (!login || login.busy) return;
      const left = Math.max(0, Math.round((login.deadline - Date.now()) / 1000));
      if (left === 0) { $('loginStatus').textContent = 'Ссылка устарела. Закрой окно и нажми «Войти» ещё раз.'; clearInterval(login.timer); return; }
      $('loginStatus').textContent = `Жду подтверждения в Telegram… ${Math.floor(left / 60)}:${String(left % 60).padStart(2, '0')}`;
      login.busy = true;
      const p = await api('/auth/login/poll', { method: 'POST', body: { nonce, secret } });
      if (!login) return;
      login.busy = false;
      if (p.status === 200) { closeLogin(); onLoggedIn(p.data); }
      else if (p.status === 410) { $('loginStatus').textContent = 'Ссылка устарела. Закрой окно и нажми «Войти» ещё раз.'; clearInterval(login.timer); }
    };
    login.timer = setInterval(tick, 2000);
    tick();
  }

  // ---------- matches ----------
  async function reportMatch(result, el) {
    const say = (...kids) => { if (el) fill(el, ...kids); };
    if (!token) {
      pending = { result, el };
      if (inMiniApp) { say('Не получилось войти через Telegram, матч не сохранён. Перезапусти мини-приложение.'); return; }
      say('Матч не попадёт в рейтинг без входа. ',
        h('button', { type: 'button', class: 'linkbtn', onclick: startLogin }, 'Войти через Telegram'));
      return;
    }
    say('Сохраняю результат…');
    const r = await api('/matches', { method: 'POST', body: result });
    if (r.status === 401) { pending = { result, el }; say('Сессия истекла. ', h('button', { type: 'button', class: 'linkbtn', onclick: startLogin }, 'Войти снова')); return; }
    if (r.status !== 200) { say('Не удалось сохранить матч: ' + ((r.data && r.data.error) || 'ошибка сервера')); return; }
    const d = r.data;
    if (d.rated) {
      say('Рейтинг ', h('b', null, d.ratingAfter), ' ',
        h('span', { class: d.delta >= 0 ? 'up' : 'down' }, '(' + signed(d.delta) + ')'),
        d.rank ? ' · место ' + d.rank : '');
    } else say('Матч сохранён в историю. Рейтинг считается только за игры против бота.');
    await refreshMe(); render(); loadBoard();
  }

  // ---------- rendering ----------
  function renderHeader() {
    const box = $('acct');
    if (me) {
      fill(box, 
        h('span', { class: 'who-chip' },
          h('span', { class: 'nm' }, name(me.user)),
          h('span', { class: 'rt' }, 'рейтинг ', h('b', null, me.user.rating)),
          me.pvp.matches ? h('span', { class: 'rt' }, 'онлайн ', h('b', null, me.pvp.rating)) : null),
        inMiniApp ? null : h('button', { type: 'button', class: 'btn', onclick: logout }, 'Выйти'));
    } else if (!inMiniApp) {
      fill(box, h('button', { type: 'button', class: 'btn tg', onclick: startLogin }, 'Войти через Telegram'));
    } else fill(box);
  }

  function stat(label, value) { return h('div', { class: 'stat' }, h('span', null, label), h('b', null, value)); }

  function renderProfile() {
    const box = $('profile');
    if (!me) {
      fill(box, 
        h('h2', null, 'Мой профиль'),
        h('p', { class: 'muted' }, 'Войди через Telegram, и каждый матч против бота будет менять твой рейтинг. Бот играет на 1200, новичок стартует с 1000.'),
        inMiniApp ? h('p', { class: 'muted' }, 'Вход через Telegram не прошёл. Перезапусти мини-приложение.')
          : h('button', { type: 'button', class: 'btn tg', onclick: startLogin }, 'Войти через Telegram'));
      return;
    }
    const s = me.stats;
    const acc = s.shots ? Math.round(s.hits / s.shots * 100) + '%' : '—';
    const kd = s.deaths ? (s.kills / s.deaths).toFixed(2) : (s.kills ? s.kills.toFixed(2) : '—');
    const p = me.pvp;
    const recentPvp = me.recentPvp.length
      ? h('ol', { class: 'recent' }, me.recentPvp.map((m) => h('li', null,
        h('span', { class: m.won ? 'up' : 'down' }, m.won ? 'Победа' : 'Поражение'),
        h('span', { class: 'sc' }, m.myScore + ' : ' + m.oppScore),
        h('span', { class: 'muted opp' }, 'vs ' + m.opponent),
        h('span', { class: 'dl' }, signed(m.ratingDelta)))))
      : h('p', { class: 'muted' }, 'Онлайн-боёв пока нет. Жми «Онлайн» в меню игры.');
    const recent = me.recent.length
      ? h('ol', { class: 'recent' }, me.recent.map((m) => h('li', null,
        h('span', { class: m.won ? 'up' : 'down' }, m.won ? 'Победа' : 'Поражение'),
        h('span', { class: 'sc' }, m.myScore + ' : ' + m.oppScore),
        h('span', { class: 'muted' }, m.mode === 'bot' ? 'бот' : 'вдвоём'),
        h('span', { class: 'dl' }, m.mode === 'bot' ? signed(m.ratingDelta) : ''))))
      : h('p', { class: 'muted' }, 'Пока ни одного матча. Сыграй против бота.');
    fill(box, 
      h('h2', null, 'Мой профиль'),
      h('div', { class: 'stats' },
        stat('Рейтинг', me.user.rating),
        stat('Место', me.rank || '—'),
        stat('Матчи', s.matches),
        stat('Победы над ботом', s.botWins + ' из ' + s.botMatches),
        stat('Сбито / потеряно', s.kills + ' / ' + s.deaths),
        stat('Точность', acc + (kd !== '—' ? ' · K/D ' + kd : '')),
        stat('Онлайн-рейтинг', p.matches ? p.rating : '—'),
        stat('Онлайн: место · победы', p.matches ? (p.rank || '—') + ' · ' + p.wins + ' из ' + p.matches : '—')),
      h('h3', null, 'Онлайн-бои'),
      recentPvp,
      h('h3', null, 'Против бота'),
      recent);
  }

  async function loadBoard(kind) {
    if (kind) boardKind = kind;
    document.querySelectorAll('#boardTabs button').forEach((b) => b.setAttribute('aria-pressed', String(b.dataset.kind === boardKind)));
    const r = await api('/leaderboard' + (boardKind === 'pvp' ? '?kind=pvp' : ''));
    const box = $('board');
    if (r.status !== 200) { fill(box, h('p', { class: 'muted' }, 'Рейтинг сейчас недоступен.')); return; }
    const rows = r.data.rows;
    if (!rows.length) { fill(box, h('p', { class: 'muted' }, boardKind === 'pvp' ? 'Онлайн-боёв ещё не было. Сыграй первым!' : 'Здесь пока пусто. Выиграй у бота и стань первым.')); return; }
    fill(box, h('table', null,
      h('thead', null, h('tr', null, h('th', null, '#'), h('th', null, 'Пилот'), h('th', { class: 'n' }, 'Рейтинг'), h('th', { class: 'n' }, 'Матчи'), h('th', { class: 'n' }, 'Победы'))),
      h('tbody', null, rows.map((x) => h('tr', { class: x.me ? 'me' : null },
        h('td', null, x.rank),
        h('td', null, x.name, x.username ? h('span', { class: 'muted' }, ' @' + x.username) : null),
        h('td', { class: 'n' }, x.rating),
        h('td', { class: 'n' }, x.matches),
        h('td', { class: 'n' }, x.wins))))));
  }

  function render() { renderHeader(); renderProfile(); }

  // ---------- boot ----------
  async function boot() {
    $('loginClose').addEventListener('click', closeLogin);
    $('boardTabs').addEventListener('click', (e) => { const b = e.target.closest('button'); if (b) loadBoard(b.dataset.kind); });
    $('loginModal').addEventListener('click', (e) => { if (e.target === e.currentTarget) closeLogin(); });
    addEventListener('keydown', (e) => { if (e.key === 'Escape' && login) closeLogin(); });

    if (inMiniApp) {
      try {
        tg.ready(); tg.expand();
        if (tg.disableVerticalSwipes) tg.disableVerticalSwipes();
        if (tg.setHeaderColor) tg.setHeaderColor('#16202e');
        if (tg.setBackgroundColor) tg.setBackgroundColor('#16202e');
      } catch (e) { /* старые клиенты Telegram */ }
    }
    if (inMiniApp) {
      const r = await api('/auth/webapp', { method: 'POST', body: { initData: tg.initData } });
      if (r.status === 200) { await onLoggedIn(r.data); markReady(); return; }
    }
    await refreshMe();
    render();
    loadBoard();
    markReady();
  }

  async function refresh() { await refreshMe(); render(); loadBoard(); }

  window.bpAccount = { reportMatch, login: startLogin, isLoggedIn: () => !!me, token: () => token, ready, refresh };
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', boot); else boot();
})();
