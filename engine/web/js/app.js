// MFROAD 화면 — made by. Nevertheless_D
// 엔진(127.0.0.1)에 1초마다 시세를 묻고, 계좌별 매수 기록을 종목별로 합쳐서 보여 줘요.
'use strict';

const TOKEN = new URLSearchParams(location.search).get('t') || '';
const S = {
  settings: { appearance: 'system', refresh: 'live', autoUpdate: true, autoImport: true, ui: {} },
  pf: { accounts: [], lots: [], rev: 0 },
  quotes: {}, qrev: '', rev: null,
  nh: { configured: false }, market: {}, update: {}, open: false, master: false,
  hist: [], tab: 'all', expanded: new Set(),
  prev: {}, flash: {}, lastQuoteAt: 0, fails: 0, build: '',
};

// ---------------------------------------------------------------- 도우미

const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => [...el.querySelectorAll(s)];
const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

async function api(path, body, opts = {}) {
  const init = { method: body === undefined ? 'GET' : 'POST', headers: { 'x-token': TOKEN }, cache: 'no-store' };
  if (body !== undefined) { init.headers['content-type'] = 'application/json'; init.body = JSON.stringify(body); }
  let res;
  for (let i = 0; i < (opts.retry ?? 2); i++) {
    try { res = await fetch(path, init); break; } catch (e) { if (i + 1 >= (opts.retry ?? 2)) throw new Error('엔진에 연결하지 못했어요'); await sleep(300); }
  }
  let data = {};
  try { data = await res.json(); } catch (e) { /* 빈 응답 */ }
  if (!res.ok || data.ok === false) throw new Error(data.error || `요청을 처리하지 못했어요 (${res.status})`);
  return data;
}
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const nf0 = new Intl.NumberFormat('ko-KR', { maximumFractionDigits: 0 });
const won = (n) => nf0.format(Math.round(n));
const sign = (n) => (n > 0 ? '+' : n < 0 ? '−' : '');
const sWon = (n) => sign(n) + won(Math.abs(n));
const sPct = (p) => sign(p) + Math.abs(p).toFixed(2) + '%';
const tone = (n) => (n > 0 ? 'up' : n < 0 ? 'down' : 'flat');
const arrow = (n) => (n > 0 ? '▲' : n < 0 ? '▼' : '–');
function price(p, kind) {
  if (p == null || !isFinite(p)) return '—';
  if (kind === 'coin' && Math.abs(p) < 100) {
    const d = Math.abs(p) < 1 ? 4 : 2;
    return new Intl.NumberFormat('ko-KR', { maximumFractionDigits: d }).format(p);
  }
  return won(p);
}
function qty(q, kind, code) {
  const s = new Intl.NumberFormat('ko-KR', { maximumFractionDigits: kind === 'coin' ? 8 : 6 }).format(q);
  return kind === 'coin' ? `${s} ${code}` : `${s}주`;
}
function clock(ms) {
  const d = new Date(ms);
  return [d.getHours(), d.getMinutes(), d.getSeconds()].map((x) => String(x).padStart(2, '0')).join(':');
}
function badgeOf(name, kind, code) {
  if (kind === 'coin') return code.slice(0, 4);
  const n = (name || code).trim();
  const m = n.match(/^[A-Z]+/);
  if (m) return m[0].length === 2 ? m[0] : m[0][0];
  return n[0] || '?';
}
function toast(msg, ms = 2600) {
  const t = $('#toast');
  t.textContent = msg;
  t.classList.add('on');
  clearTimeout(toast.t);
  toast.t = setTimeout(() => t.classList.remove('on'), ms);
}
function clientLog(msg) { api('/api/clientlog', { items: [String(msg).slice(0, 500)] }, { retry: 1 }).catch(() => {}); }
window.addEventListener('error', (e) => clientLog(`${e.message} @${e.filename}:${e.lineno}`));
window.addEventListener('unhandledrejection', (e) => clientLog(`promise: ${e.reason && e.reason.message || e.reason}`));

const ICON = {
  chev: '<svg viewBox="0 0 24 24"><path d="M9 6l6 6-6 6"></path></svg>',
  x: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M6 6l12 12M18 6L6 18"></path></svg>',
  search: '<svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="11" cy="11" r="7"></circle><path d="M20 20l-3.5-3.5"></path></svg>',
  check: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 12.5l4.5 4.5L19 7.5"></path></svg>',
  plus: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v14M5 12h14"></path></svg>',
  down: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 4v11M7 10l5 5 5-5M5 20h14"></path></svg>',
  lock: '<svg viewBox="0 0 24 24" aria-hidden="true"><rect x="5" y="11" width="14" height="9" rx="2"></rect><path d="M8 11V8a4 4 0 0 1 8 0v3"></path></svg>',
  back: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M15 5l-7 7 7 7"></path></svg>',
  mark: '<svg viewBox="0 0 24 24" fill="none" aria-hidden="true"><rect x="4" y="15" width="3" height="5" rx="0.8" class="bar"></rect><rect x="10.5" y="12.5" width="3" height="7.5" rx="0.8" class="bar"></rect><rect x="17" y="10" width="3" height="10" rx="0.8" class="bar"></rect><path d="M3 13 L8.5 8.5 L12.5 11 L20 4" class="line"></path><path d="M15 4 H20 V9" class="line"></path></svg>',
};

// ---------------------------------------------------------------- 테마

function applyTheme() {
  const a = S.settings.appearance;
  if (a === 'light' || a === 'dark') document.documentElement.dataset.theme = a;
  else delete document.documentElement.dataset.theme;
}

async function saveSettings(patch) {
  Object.assign(S.settings, patch);
  applyTheme();
  try { S.settings = Object.assign(S.settings, await api('/api/settings', patch)); } catch (e) { toast(e.message); }
}
function rememberUI(k, v) {
  S.settings.ui[k] = v;
  api('/api/settings', { ui: { [k]: v } }).catch(() => {});
}

// ---------------------------------------------------------------- 계산

const acctById = (id) => S.pf.accounts.find((a) => a.id === id);
const quoteOf = (kind, code) => S.quotes[`${kind}:${code}`];

function groupsOf(kind) {
  const by = new Map();
  for (const l of S.pf.lots) {
    if (l.kind !== kind) continue;
    if (!by.has(l.code)) by.set(l.code, { kind, code: l.code, name: l.name, market: l.market, lots: [] });
    const g = by.get(l.code);
    g.lots.push(l);
    if (!g.name && l.name) g.name = l.name;
    if (!g.market && l.market) g.market = l.market;
  }
  const out = [];
  for (const g of by.values()) {
    const q = quoteOf(kind, g.code);
    if (q && q.name) g.name = q.name;
    g.q = q;
    g.qty = g.lots.reduce((s, l) => s + l.qty, 0);
    g.cost = g.lots.reduce((s, l) => s + l.qty * l.avg, 0);
    g.avg = g.qty > 0 ? g.cost / g.qty : 0;
    g.value = q ? g.qty * q.price : null;
    g.pl = q ? g.value - g.cost : null;
    g.plp = q && g.cost > 0 ? (g.pl / g.cost) * 100 : null;
    g.today = q ? g.qty * (q.change || 0) : 0;
    g.lots.sort((a, b) => b.qty * b.avg - a.qty * a.avg);
    out.push(g);
  }
  out.sort((a, b) => (b.value ?? b.cost) - (a.value ?? a.cost));
  return out;
}

function totalsOf(groups) {
  let value = 0, cost = 0, costPriced = 0, today = 0, missing = 0;
  for (const g of groups) {
    cost += g.cost;
    if (g.value == null) { missing++; continue; }
    value += g.value; costPriced += g.cost; today += g.today;
  }
  const pl = value - costPriced;
  return { value, cost, pl, plp: costPriced > 0 ? (pl / costPriced) * 100 : 0, today, todayP: value - today > 0 ? (today / (value - today)) * 100 : 0, missing, n: groups.length };
}

// ---------------------------------------------------------------- 그리기

function render() {
  $$('#tabs button').forEach((b) => b.setAttribute('aria-pressed', String(b.dataset.tab === S.tab)));
  renderNotices();
  const main = $('#main');
  if (!S.pf.lots.length) { main.innerHTML = emptyHTML(); return; }
  const stocks = groupsOf('stock'), coins = groupsOf('coin');
  const ts = totalsOf(stocks), tc = totalsOf(coins);
  const show = (k) => S.tab === 'all' || S.tab === k;
  const visible = [...(show('stock') ? stocks : []), ...(show('coin') ? coins : [])];
  const t = totalsOf(visible);
  let html = `<section class="summary" aria-label="자산 요약">${totalHTML(t, ts, tc)}<div class="side">${miniHTML('stock', ts)}${miniHTML('coin', tc)}</div></section>`;
  html += `<section class="card holdings" aria-label="보유 종목">`;
  if (show('stock')) html += groupHTML('stock', stocks);
  if (show('coin')) html += groupHTML('coin', coins);
  html += `</section>`;
  main.innerHTML = html;
}

function emptyHTML() {
  return `<section class="card empty">
    <span class="mark">${ICON.mark}</span>
    <h2>아직 등록한 자산이 없어요</h2>
    <p>국내주식이나 코인을 검색해서 보유 수량과 평균 매수가를 넣으면, 지금 가격으로 평가 금액과 손익을 계속 계산해 드려요.<br>
    나무증권을 연결하면 나무증권 계좌의 주식은 자동으로 불러와요.</p>
    <div class="row2">
      <button type="button" class="btn primary" data-act="add">${ICON.plus}종목 추가</button>
      ${S.nh.configured ? '' : '<button type="button" class="btn" data-act="settings">나무증권 연결</button>'}
    </div>
  </section>`;
}

function sparkHTML() {
  const pts = S.hist || [];
  if (pts.length < 2) {
    return `<svg class="spark" viewBox="0 0 600 72" preserveAspectRatio="none" aria-hidden="true"><path class="ln wait" d="M0,40 L600,40"></path></svg>
      <div class="spark-cap">오늘 그래프는 1분마다 한 점씩 쌓여요</div>`;
  }
  const vs = pts.map((p) => p.v);
  let lo = Math.min(...vs), hi = Math.max(...vs);
  if (hi - lo < hi * 0.0005) { const m = (hi + lo) / 2; lo = m * 0.9995; hi = m * 1.0005; }
  const t0 = pts[0].t, t1 = pts[pts.length - 1].t || t0 + 1;
  const x = (t) => ((t - t0) / Math.max(1, t1 - t0)) * 600;
  const y = (v) => 64 - ((v - lo) / (hi - lo)) * 56;
  const d = pts.map((p, i) => `${i ? 'L' : 'M'}${x(p.t).toFixed(1)},${y(p.v).toFixed(1)}`).join(' ');
  return `<svg class="spark" viewBox="0 0 600 72" preserveAspectRatio="none" role="img" aria-label="오늘 총자산 그래프"><path class="area" d="${d} L600,72 L0,72 Z"></path><path class="ln" d="${d}"></path></svg>`;
}

function totalHTML(t, ts, tc) {
  const sum = ts.value + tc.value;
  const sp = sum > 0 ? (ts.value / sum) * 100 : 0;
  const allocBar = sum > 0 && ts.n && tc.n ? `<div class="alloc">
      <div class="bar"><span class="sw stock" style="width:${sp.toFixed(1)}%"></span><span class="sw coin" style="flex:1"></span></div>
      <div class="legend"><span><i class="sw stock"></i>국내주식 <b>${sp.toFixed(1)}%</b></span><span><i class="sw coin"></i>코인 <b>${(100 - sp).toFixed(1)}%</b></span></div>
    </div>` : '';
  const label = S.tab === 'stock' ? '국내주식 자산' : S.tab === 'coin' ? '코인 자산' : '총 자산';
  const miss = t.missing ? `<span class="chip"><span class="k">시세 기다리는 종목</span>${t.missing}개</span>` : '';
  return `<div class="card total">
    <div style="display:flex;flex-direction:column;gap:6px">
      <span class="label">${label}</span>
      <div class="amount"><span class="v">${won(t.value)}</span><span class="u">원</span></div>
      <span class="base" title="산 가격(평균 단가) × 보유 수량을 모두 더한 금액이에요. 시세가 바뀌어도 그대로예요.">평균 단가 기준 <b>${won(t.cost)}원</b></span>
    </div>
    <div class="chips">
      <span class="chip tint-${tone(t.pl) === 'down' ? 'down' : 'up'} ${tone(t.pl)}"><span class="k">총 손익</span>${sWon(t.pl)}원 (${sPct(t.plp)})</span>
      <span class="chip ${tone(t.today)}"><span class="k">오늘</span>${sWon(t.today)}원 (${sPct(t.todayP)})</span>
      ${miss}
    </div>
    ${S.tab === 'all' ? sparkHTML() : ''}
    ${S.tab === 'all' ? allocBar : ''}
  </div>`;
}

function miniHTML(kind, t) {
  const accts = new Set(S.pf.lots.filter((l) => l.kind === kind).map((l) => l.account));
  const where = accts.size > 1 ? `계좌 ${accts.size}개`
    : accts.size === 1 ? ((acctById([...accts][0]) || {}).name || '')
    : (kind === 'stock' ? '나무증권' : '업비트');
  const src = `${where} · ${t.n}${kind === 'stock' ? '종목' : '개'}`;
  return `<div class="card mini">
    <div class="head"><span class="title"><i class="sw ${kind}"></i>${kind === 'stock' ? '국내주식' : '코인'}</span><span class="tag">${esc(src)}</span></div>
    <div class="val"><span class="v">${won(t.value)}</span><span class="u">원</span></div>
    <span class="pl ${tone(t.pl)}">${t.n ? `${sWon(t.pl)}원 · ${sPct(t.plp)}` : '아직 없어요'}</span>
  </div>`;
}

function groupHTML(kind, groups) {
  const title = kind === 'stock' ? '국내주식' : '코인';
  let sub;
  if (kind === 'stock') sub = S.nh.configured ? (S.open ? '나무증권 실시간 시세' : '장 마감 · 나무증권 마지막 시세') : '전일 종가 기준 · 나무증권을 연결하면 실시간';
  else sub = '업비트 원화 시세';
  const btn = kind === 'stock' && S.nh.configured
    ? `<button type="button" class="btn small" data-act="sync" ${S.nh.syncing ? 'disabled' : ''}>${S.nh.syncing ? '불러오는 중…' : '계좌 다시 불러오기'}</button>` : '';
  let html = `<div class="group"><div class="ghead"><div class="t"><h2>${title}</h2><span class="sub">${esc(sub)}</span></div>${btn}</div>`;
  if (!groups.length) {
    html += `<div class="hint" style="padding:0 20px 20px">아직 ${title}이 없어요. 오른쪽 위 '종목 추가'로 넣어 보세요.</div></div>`;
    return html;
  }
  html += `<div class="scroll"><div class="tbl"><div class="row thead"><span>${kind === 'stock' ? '종목' : '코인'}</span><span>현재가 (원)</span><span>보유 수량</span><span>평균 단가 (원)</span><span>평가 금액 (원)</span><span>손익</span></div>`;
  for (const g of groups) html += itemHTML(g);
  html += `</div></div></div>`;
  return html;
}

function flashClass(key) {
  const f = S.flash[key];
  return f && Date.now() - f.at < 1200 ? (f.dir > 0 ? 'f-up' : 'f-down') : '';
}

function itemHTML(g) {
  const key = `${g.kind}:${g.code}`;
  const multi = g.lots.length > 1;
  const open = multi && S.expanded.has(key);
  const q = g.q;
  const acctNames = [...new Set(g.lots.map((l) => (acctById(l.account) || {}).name || '계좌'))];
  const mk = g.kind === 'coin' ? 'KRW' : (g.market || '');
  const sub = [g.code, multi ? `계좌 ${g.lots.length}개` : acctNames[0], !multi && mk ? mk : ''].filter(Boolean).join(' · ');
  const chg = q ? `<span class="s12 ${tone(q.rate)}">${arrow(q.rate)} ${Math.abs(q.rate).toFixed(2)}%</span>` : '<span class="s12 nodata">시세 기다리는 중</span>';
  let html = `<button type="button" class="row item ${open ? 'open' : ''}" data-key="${esc(key)}" data-act="${multi ? 'toggle' : 'edit'}" ${multi ? '' : `data-lot="${esc(g.lots[0].id)}"`} aria-expanded="${multi ? String(open) : 'false'}">
    <span class="name">${multi ? `<span class="chev">${ICON.chev}</span>` : ''}<span class="badge ${g.kind === 'coin' ? 'coin' : ''}">${esc(badgeOf(g.name, g.kind, g.code))}</span>
      <span class="nm"><b>${esc(g.name || g.code)}</b><span>${esc(sub)}</span></span></span>
    <span class="c"><span class="v15 b price ${flashClass(key)}">${q ? price(q.price, g.kind) : '—'}</span>${chg}</span>
    <span class="c"><span class="v15">${qty(g.qty, g.kind, g.code)}</span></span>
    <span class="c"><span class="v15 muted">${price(g.avg, g.kind)}</span></span>
    <span class="c"><span class="v15 b">${g.value == null ? '—' : won(g.value)}</span></span>
    <span class="c ${g.pl == null ? 'nodata' : tone(g.pl)}"><span class="v15 b">${g.pl == null ? '—' : sWon(g.pl)}</span><span class="s12">${g.plp == null ? '' : sPct(g.plp)}</span></span>
  </button>`;
  if (open) {
    html += `<div class="lots">`;
    for (const l of g.lots) {
      const a = acctById(l.account) || { name: '계좌' };
      const v = q ? l.qty * q.price : null, pl = q ? v - l.qty * l.avg : null, plp = q && l.avg > 0 ? (pl / (l.qty * l.avg)) * 100 : null;
      html += `<button type="button" class="row lot" data-act="edit" data-lot="${esc(l.id)}">
        <span class="acc"><span class="tag ${l.source === 'nh' ? 'auto' : ''}">${esc(a.name)}</span>${l.source === 'nh' ? '<span class="muted">자동</span>' : ''}</span>
        <span class="c"></span>
        <span class="c"><span class="v15">${qty(l.qty, l.kind, l.code)}</span></span>
        <span class="c"><span class="v15 muted">${price(l.avg, l.kind)}</span></span>
        <span class="c"><span class="v15">${v == null ? '—' : won(v)}</span></span>
        <span class="c ${pl == null ? 'nodata' : tone(pl)}"><span class="v15">${pl == null ? '—' : sWon(pl)}</span><span class="s12">${plp == null ? '' : sPct(plp)}</span></span>
      </button>`;
    }
    html += `</div>`;
  }
  return html;
}

function renderNotices() {
  const out = [];
  const u = S.update || {};
  if (u.status === 'ready' && u.installed) {
    out.push(`<div class="notice"><span class="grow"><b>새 버전 ${esc(u.installed)}을 받았어요.</b> 다시 시작하면 새 버전으로 열려요.</span><button type="button" class="btn small primary" data-act="restart">다시 시작</button></div>`);
  } else if (u.status === 'available' && !S.settings.autoUpdate) {
    out.push(`<div class="notice"><span class="grow"><b>새 버전 ${esc(u.latest && u.latest.version)}이 나왔어요.</b></span><button type="button" class="btn small" data-act="update">자세히</button></div>`);
  }
  const hasStock = S.pf.lots.some((l) => l.kind === 'stock');
  if (S.nh.configured && S.nh.state === 'error' && S.nh.error) {
    out.push(`<div class="notice warn"><span class="grow"><b>나무증권</b> · ${esc(S.nh.error)}</span><button type="button" class="btn small" data-act="settings">설정 열기</button></div>`);
  } else if (S.nh.configured && S.nh.syncError) {
    out.push(`<div class="notice warn"><span class="grow"><b>계좌 불러오기</b> · ${esc(S.nh.syncError)}</span><button type="button" class="btn small" data-act="sync">다시 시도</button></div>`);
  } else if (hasStock && !S.nh.configured && !S.settings.ui.hideNHTip) {
    out.push(`<div class="notice"><span class="grow">국내주식은 지금 <b>전일 종가</b>로 보여요. 나무증권을 연결하면 실시간 시세로 바뀌어요.</span><button type="button" class="btn small primary" data-act="settings">연결하기</button><button type="button" class="btn small ghost" data-act="hideNHTip">닫기</button></div>`);
  }
  if (S.market && S.market.coinError && S.pf.lots.some((l) => l.kind === 'coin')) {
    out.push(`<div class="notice warn"><span class="grow"><b>업비트</b> · ${esc(S.market.coinError)}</span></div>`);
  }
  const html = out.join('');
  const el = $('#notices');
  if (el.innerHTML !== html) el.innerHTML = html;
}

function renderLive() {
  const el = $('#live'), txt = $('#liveText');
  const bad = S.fails > 0 || (S.market && S.market.coinError && S.pf.lots.some((l) => l.kind === 'coin')) || (S.nh.configured && S.nh.state === 'error');
  const at = S.lastQuoteAt;
  const every = { live: '실시간', '5s': '5초마다', '30s': '30초마다', '60s': '1분마다' }[S.settings.refresh] || '실시간';
  el.classList.toggle('bad', !!bad);
  el.classList.toggle('idle', !bad && !S.pf.lots.length);
  if (!S.pf.lots.length) txt.textContent = '대기 중';
  else txt.textContent = `${bad ? '연결 확인 필요' : every}${at ? ' · ' + clock(at) : ''}`;
  $('#btnVersion').textContent = 'v' + (S.build || '—');
}

// ---------------------------------------------------------------- 엔진과 주고받기

async function loadPortfolio() {
  S.pf = await api('/api/portfolio');
  S.rev = S.pf.rev;
}
async function loadHistory() {
  try { S.hist = (await api('/api/history')).today || []; } catch (e) { /* 다음에 */ }
}

let polling = false;
async function poll() {
  if (polling) return;
  polling = true;
  try {
    const d = await api(`/api/poll?q=${encodeURIComponent(S.qrev)}`, undefined, { retry: 1 });
    S.fails = 0;
    $('#offline').hidden = true;
    S.build = d.build; S.nh = d.nh || {}; S.market = d.market || {}; S.update = d.update || {}; S.open = d.open; S.master = d.master;
    let changed = false;
    if (d.rev !== S.rev) { await loadPortfolio(); changed = true; }
    if (d.quotes) {
      const now = Date.now();
      for (const [k, q] of Object.entries(d.quotes)) {
        const old = S.prev[k];
        if (old != null && q.price !== old) S.flash[k] = { at: now, dir: q.price > old ? 1 : -1 };
        S.prev[k] = q.price;
        if (q.at > S.lastQuoteAt) S.lastQuoteAt = q.at;
      }
      S.quotes = d.quotes;
      S.qrev = String(d.qrev);
      changed = true;
      $('#live').classList.remove('pulse'); void $('#live').offsetWidth; $('#live').classList.add('pulse');
    }
    if (changed || renderNotices.lastNH !== JSON.stringify([S.nh, S.update.status, S.market.coinError])) {
      renderNotices.lastNH = JSON.stringify([S.nh, S.update.status, S.market.coinError]);
      render();
    }
    renderLive();
    if (UPD.open) UPD.refresh();
    if (SET.open) SET.refreshStatus();
  } catch (e) {
    S.fails++;
    if (S.fails >= 4) $('#offline').hidden = false;
    renderLive();
  } finally {
    polling = false;
  }
}

// ---------------------------------------------------------------- 창(모달)

const modal = {
  stack: [],
  open(html, cls, onClose) {
    const ov = document.createElement('div');
    ov.className = 'overlay';
    ov.innerHTML = `<div class="dialog ${cls || ''}" role="dialog" aria-modal="true">${html}</div>`;
    ov.addEventListener('mousedown', (e) => { if (e.target === ov) ov._downOnOverlay = true; });
    ov.addEventListener('click', (e) => { if (e.target === ov && ov._downOnOverlay) modal.close(); ov._downOnOverlay = false; });
    $('#modalRoot').appendChild(ov);
    modal.stack.push({ ov, onClose });
    return ov.firstElementChild;
  },
  close() {
    const top = modal.stack.pop();
    if (!top) return;
    top.ov.remove();
    if (top.onClose) top.onClose();
  },
  closeAll() { while (modal.stack.length) modal.close(); },
};
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape' && modal.stack.length) { e.preventDefault(); modal.close(); }
});

function confirmBox(title, text, okLabel = '확인', danger = false) {
  return new Promise((resolve) => {
    let done = false;
    const el = modal.open(`<div class="dhead"><h1 style="font-size:19px">${esc(title)}</h1></div>
      <div class="hint" style="font-size:14px">${text}</div>
      <div class="dfoot"><button type="button" class="btn" data-x>취소</button><button type="button" class="btn ${danger ? 'danger' : 'primary'}" data-ok>${esc(okLabel)}</button></div>`,
    'small', () => { if (!done) resolve(false); });
    $('[data-x]', el).onclick = () => modal.close();
    $('[data-ok]', el).onclick = () => { done = true; modal.close(); resolve(true); };
    $('[data-ok]', el).focus();
  });
}

// ---------------------------------------------------------------- 종목 추가 · 고치기

const STOCK_BROKERS = ['나무증권', '한국투자', '키움', '미래에셋', '삼성증권', '토스증권', 'KB증권', 'NH투자'];
const COIN_EXCHANGES = ['업비트', '빗썸', '코인원', '코빗'];

function parseNum(s) {
  const n = parseFloat(String(s).replace(/[^0-9.]/g, ''));
  return isFinite(n) ? n : 0;
}
function fmtInput(raw) {
  const s = String(raw).replace(/[^0-9.]/g, '');
  const [h, ...rest] = s.split('.');
  const head = h ? nf0.format(Number(h)) : (rest.length ? '0' : '');
  return rest.length ? `${head}.${rest.join('').slice(0, 8)}` : head;
}

function openLot(opts = {}) {
  const editing = opts.lot || null;
  const D = {
    kind: editing ? editing.kind : (opts.kind || (S.tab === 'coin' ? 'coin' : 'stock')),
    q: '', items: [], loading: false, err: '',
    sel: editing ? { kind: editing.kind, code: editing.code, name: editing.name, market: editing.market } : null,
    quote: null, account: editing ? editing.account : null, newAcct: false,
    qty: editing ? String(editing.qty) : '', avg: editing ? String(editing.avg) : '',
  };
  const auto = editing && editing.source === 'nh';
  const el = modal.open(`
    <div class="dhead"><h1>${editing ? (auto ? '자동으로 불러온 기록' : '기록 고치기') : '종목 추가'}</h1><button type="button" class="x" data-x aria-label="닫기">${ICON.x}</button></div>
    <div id="lotBody" style="display:contents"></div>`, '');
  const body = $('#lotBody', el);
  $('[data-x]', el).onclick = () => modal.close();

  const manualAccounts = () => S.pf.accounts.filter((a) => a.kind === D.kind && a.source !== 'nh');
  if (!D.account) {
    const last = S.settings.ui['lastAccount_' + D.kind];
    const list = manualAccounts();
    D.account = (list.find((a) => a.id === last) || list[0] || {}).id || null;
  }
  const existing = () => D.sel && D.account && S.pf.lots.find((l) => l.kind === D.kind && l.code === D.sel.code && l.account === D.account && (!editing || l.id !== editing.id));

  function curPrice() {
    const q = D.quote || (D.sel && quoteOf(D.kind, D.sel.code));
    return q ? q.price : (D.sel && D.sel.price) || 0;
  }

  function draw() {
    const accts = manualAccounts();
    const suggestions = (D.kind === 'stock' ? STOCK_BROKERS : COIN_EXCHANGES).filter((n) => !accts.some((a) => a.name === n));
    let h = '';
    if (auto) {
      const a = acctById(editing.account) || {};
      h += `<div class="picked"><span class="badge">${esc(badgeOf(editing.name, editing.kind, editing.code))}</span><span class="nm"><b>${esc(editing.name || editing.code)}</b><span>${esc(editing.code)} · ${esc(a.name || '나무증권')}</span></span></div>
        <div class="sumbox"><div class="r"><span>보유 수량</span><span class="num b">${qty(editing.qty, editing.kind, editing.code)}</span></div>
        <div class="r"><span>평균 단가</span><span class="num b">${price(editing.avg, editing.kind)}원</span></div></div>
        <div class="hint">나무증권 계좌에서 자동으로 불러온 기록이에요. 나무증권에서 사고팔면 다음에 불러올 때 저절로 바뀌어요. 다른 증권사에서 산 같은 종목은 '종목 추가'로 따로 넣으면 합쳐서 보여 드려요.</div>
        <div class="dfoot"><button type="button" class="btn primary" data-x2>닫기</button></div>`;
      body.innerHTML = h;
      $('[data-x2]', body).onclick = () => modal.close();
      return;
    }
    if (!editing) {
      h += `<div class="seg wide" role="group" aria-label="자산 종류">
        <button type="button" data-kind="stock" aria-pressed="${D.kind === 'stock'}">국내주식</button>
        <button type="button" data-kind="coin" aria-pressed="${D.kind === 'coin'}">코인</button></div>
        <div class="field"><label for="lotQ">${D.kind === 'stock' ? '종목 검색' : '코인 검색'}</label>
        <div class="search">${ICON.search}<input id="lotQ" type="search" autocomplete="off" spellcheck="false" placeholder="${D.kind === 'stock' ? '종목명 · 초성 · 코드 (예: 삼성전자, ㅅㅅㅈㅈ, 005930)' : '코인 이름 또는 기호 (예: 비트코인, BTC)'}" value="${esc(D.q)}"></div></div>
        <div class="results" id="lotRes">${resultsHTML()}</div>`;
    } else {
      h += `<div class="picked"><span class="badge ${D.kind === 'coin' ? 'coin' : ''}">${esc(badgeOf(D.sel.name, D.kind, D.sel.code))}</span><span class="nm"><b>${esc(D.sel.name || D.sel.code)}</b><span>${esc(D.sel.code)}${D.sel.market ? ' · ' + esc(D.sel.market) : ''}</span></span></div>`;
    }
    // 계좌
    h += `<div class="field"><span class="flabel">${D.kind === 'stock' ? '어느 증권사 계좌에 있나요?' : '어느 거래소에 있나요?'}</span><div class="accts" id="acctPills">`;
    for (const a of accts) h += `<button type="button" class="pill" data-acct="${esc(a.id)}" aria-pressed="${a.id === D.account}">${esc(a.name)}</button>`;
    h += `<button type="button" class="pill add" data-newacct>${ICON.plus}${accts.length ? '새 계좌' : (D.kind === 'stock' ? '증권사 계좌 만들기' : '거래소 만들기')}</button></div>`;
    if (D.newAcct || !accts.length) {
      h += `<div class="newacct"><div class="accts">${suggestions.map((n) => `<button type="button" class="pill" data-suggest="${esc(n)}">${esc(n)}</button>`).join('')}</div>
        <div class="line"><input class="inp" id="acctName" maxlength="24" placeholder="${D.kind === 'stock' ? '직접 입력 (예: 한국투자 ISA)' : '직접 입력 (예: 바이낸스)'}"><button type="button" class="btn" data-mkacct>만들기</button></div></div>`;
    }
    h += `</div>`;
    const unit = D.kind === 'stock' ? '주' : (D.sel ? D.sel.code : '개');
    h += `<div class="two">
      <div class="field"><label for="lotQty">보유 수량</label><div class="box"><input id="lotQty" inputmode="decimal" autocomplete="off" value="${esc(fmtInput(D.qty))}" placeholder="0"><span class="u">${esc(unit)}</span></div></div>
      <div class="field wider"><label for="lotAvg" style="display:flex;justify-content:space-between;align-items:center">평균 매수가<button type="button" class="mini-link" data-fillprice ${D.sel ? '' : 'hidden'}>현재가로 채우기</button></label><div class="box"><input id="lotAvg" inputmode="decimal" autocomplete="off" value="${esc(fmtInput(D.avg))}" placeholder="0"><span class="u">원</span></div></div>
    </div>
    <div class="sumbox" id="lotSum"></div>
    <div class="hint" id="lotMerge" hidden></div>`;
    if (!editing && D.kind === 'stock') {
      h += S.nh.configured
        ? `<button type="button" class="btn dashed" data-nhsync>${ICON.down}나무증권 계좌에서 한 번에 불러오기</button>`
        : `<button type="button" class="btn dashed" data-nhsetup>${ICON.down}나무증권을 연결하면 계좌의 주식을 한 번에 불러와요</button>`;
    }
    h += `<div class="dfoot">${editing ? '<button type="button" class="btn danger left" data-del>이 기록 지우기</button>' : ''}<button type="button" class="btn" data-cancel style="min-width:96px">취소</button><button type="button" class="btn primary" data-save style="min-width:120px">${editing ? '저장' : '추가하기'}</button></div>`;
    body.innerHTML = h;
    bind();
    updateSum();
  }

  function resultsHTML() {
    if (D.err) return `<div class="msg">${esc(D.err)}</div>`;
    if (!D.q.trim()) return `<div class="msg">${D.kind === 'stock' ? '찾을 종목의 이름이나 코드를 넣어 주세요.<br>초성(ㅅㅅㅈㅈ)으로도 찾을 수 있어요.' : '찾을 코인의 이름이나 기호를 넣어 주세요.'}</div>`;
    if (D.loading && !D.items.length) return `<div class="msg">찾는 중…</div>`;
    if (!D.items.length) return `<div class="msg">검색 결과가 없어요. 이름이나 코드로 다시 찾아 보세요.</div>`;
    return D.items.map((it, i) => {
      const on = D.sel && D.sel.code === it.code;
      const note = it.kind === 'stock' && it.source === 'close' ? '<span class="s12 muted">전일 종가</span>' : (it.rate ? `<span class="s12 ${tone(it.rate)}">${arrow(it.rate)} ${Math.abs(it.rate).toFixed(2)}%</span>` : '');
      return `<button type="button" class="res" data-i="${i}" aria-pressed="${!!on}">
        <span class="badge ${it.kind === 'coin' ? 'coin' : ''}">${esc(badgeOf(it.name, it.kind, it.code))}</span>
        <span class="nm"><b>${esc(it.name)}</b><span>${esc(it.code)} · ${esc(it.market)}</span></span>
        <span class="pr"><span class="v15 b">${it.price ? price(it.price, it.kind) + '원' : '—'}</span>${note}</span>
        <span class="check">${ICON.check}</span></button>`;
    }).join('');
  }

  let searchTimer = 0, searchSeq = 0;
  async function doSearch() {
    const seq = ++searchSeq;
    const q = D.q.trim();
    if (!q) { D.items = []; D.err = ''; $('#lotRes', body).innerHTML = resultsHTML(); return; }
    D.loading = true;
    try {
      const r = await api(`/api/search?kind=${D.kind}&q=${encodeURIComponent(q)}`);
      if (seq !== searchSeq) return;
      D.items = r.items || []; D.err = '';
      if (D.items.length && (!D.sel || !D.items.some((x) => x.code === D.sel.code))) pick(D.items[0], false);
    } catch (e) {
      if (seq !== searchSeq) return;
      D.items = []; D.err = e.message;
    }
    D.loading = false;
    const box = $('#lotRes', body);
    if (box) box.innerHTML = resultsHTML();
    updateSum();
  }

  async function pick(it, redraw = true) {
    D.sel = { kind: it.kind, code: it.code, name: it.name, market: it.market, price: it.price };
    D.quote = null;
    if (redraw) { $('#lotRes', body).innerHTML = resultsHTML(); }
    const u = $('#lotQty', body) && $('#lotQty', body).nextElementSibling;
    if (u && D.kind === 'coin') u.textContent = it.code;
    const fp = $('[data-fillprice]', body); if (fp) fp.hidden = false;
    updateSum();
    try {
      const r = await api(`/api/quote?kind=${it.kind}&code=${encodeURIComponent(it.code)}`);
      if (D.sel && D.sel.code === it.code) { D.quote = r.quote; updateSum(); }
    } catch (e) { /* 검색 결과의 값을 그대로 써요 */ }
  }

  function updateSum() {
    const sum = $('#lotSum', body);
    if (!sum) return;
    const p = curPrice();
    const nq = parseNum(D.qty), na = parseNum(D.avg);
    const pl = nq * (p - na), cost = nq * na;
    const src = D.quote ? D.quote.source : (D.sel && D.sel.price ? (D.kind === 'stock' ? 'close' : 'upbit') : '');
    const srcLabel = src === 'close' ? ' (전일 종가)' : '';
    sum.innerHTML = `<div class="r"><span>현재가 · ${esc(D.sel ? D.sel.name : '종목을 골라 주세요')}${srcLabel}</span><span class="num b">${p ? price(p, D.kind) + '원' : '—'}</span></div>
      <div class="r"><span>${editing ? '평가 금액' : '예상 평가 금액'}</span><span class="num big">${p && nq ? won(nq * p) + '원' : '—'}</span></div>
      <div class="r"><span>${editing ? '손익' : '예상 손익'}</span><span class="num b ${tone(pl)}">${p && nq && na ? `${sWon(pl)}원 (${sPct(cost > 0 ? (pl / cost) * 100 : 0)})` : '—'}</span></div>`;
    const ex = existing();
    const m = $('#lotMerge', body);
    if (m) {
      if (ex && !editing) {
        m.hidden = false;
        m.innerHTML = `이 계좌에 이미 <b>${qty(ex.qty, ex.kind, ex.code)}</b>(평단 ${price(ex.avg, ex.kind)}원)이 있어요. 추가하면 합쳐서 평균 단가를 다시 계산해요.`;
      } else if (ex && editing) {
        m.hidden = false; m.className = 'hint warn';
        m.textContent = '고른 계좌에 이 종목 기록이 이미 있어요. 그 기록을 고치거나 다른 계좌를 골라 주세요.';
      } else m.hidden = true;
    }
    const ok = D.sel && D.account && nq > 0 && !(ex && editing);
    const sv = $('[data-save]', body); if (sv) sv.disabled = !ok;
  }

  async function makeAccount(name) {
    name = (name || '').trim();
    if (!name) { toast('계좌 이름을 넣어 주세요'); return; }
    try {
      const r = await api('/api/account/save', { name, kind: D.kind });
      await loadPortfolio();
      D.account = r.account.id; D.newAcct = false;
      draw();
    } catch (e) { toast(e.message); }
  }

  function bind() {
    $$('[data-kind]', body).forEach((b) => b.onclick = () => {
      if (D.kind === b.dataset.kind) return;
      D.kind = b.dataset.kind; D.sel = null; D.quote = null; D.items = []; D.q = ''; D.err = ''; D.newAcct = false;
      const list = manualAccounts(); const last = S.settings.ui['lastAccount_' + D.kind];
      D.account = (list.find((a) => a.id === last) || list[0] || {}).id || null;
      draw(); $('#lotQ', body).focus();
    });
    const qi = $('#lotQ', body);
    if (qi) {
      qi.oninput = () => { D.q = qi.value; clearTimeout(searchTimer); searchTimer = setTimeout(doSearch, 160); };
      qi.onkeydown = (e) => {
        if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
          e.preventDefault();
          if (!D.items.length) return;
          let i = D.items.findIndex((x) => D.sel && x.code === D.sel.code);
          i = Math.max(0, Math.min(D.items.length - 1, i + (e.key === 'ArrowDown' ? 1 : -1)));
          pick(D.items[i]);
          const r = $(`.res[data-i="${i}"]`, body); if (r) r.scrollIntoView({ block: 'nearest' });
        } else if (e.key === 'Enter') { e.preventDefault(); $('#lotQty', body).focus(); }
      };
    }
    const rs = $('#lotRes', body);
    if (rs) rs.onclick = (e) => { const b = e.target.closest('.res'); if (b) pick(D.items[+b.dataset.i]); };
    $$('[data-acct]', body).forEach((b) => b.onclick = () => { D.account = b.dataset.acct; D.newAcct = false; $$('[data-acct]', body).forEach((x) => x.setAttribute('aria-pressed', String(x === b))); updateSum(); });
    const na = $('[data-newacct]', body); if (na) na.onclick = () => { D.newAcct = !D.newAcct; draw(); const i = $('#acctName', body); if (i) i.focus(); };
    $$('[data-suggest]', body).forEach((b) => b.onclick = () => makeAccount(b.dataset.suggest));
    const mk = $('[data-mkacct]', body); if (mk) mk.onclick = () => makeAccount($('#acctName', body).value);
    const an = $('#acctName', body); if (an) an.onkeydown = (e) => { if (e.key === 'Enter') { e.preventDefault(); makeAccount(an.value); } };
    for (const id of ['lotQty', 'lotAvg']) {
      const inp = $('#' + id, body);
      inp.oninput = () => {
        const raw = inp.value.replace(/[^0-9.]/g, '');
        const fromEnd = inp.value.length - inp.selectionStart;
        if (id === 'lotQty') D.qty = raw; else D.avg = raw;
        inp.value = fmtInput(raw);
        const pos = Math.max(0, inp.value.length - fromEnd);
        inp.setSelectionRange(pos, pos);
        updateSum();
      };
      inp.onkeydown = (e) => { if (e.key === 'Enter') { e.preventDefault(); if (id === 'lotQty') $('#lotAvg', body).focus(); else save(); } };
    }
    const fp = $('[data-fillprice]', body);
    if (fp) fp.onclick = () => { const p = curPrice(); if (p) { D.avg = String(p); $('#lotAvg', body).value = fmtInput(D.avg); updateSum(); } };
    $('[data-cancel]', body).onclick = () => modal.close();
    $('[data-save]', body).onclick = save;
    const del = $('[data-del]', body);
    if (del) del.onclick = async () => {
      const a = acctById(editing.account) || {};
      if (!(await confirmBox('이 기록을 지울까요?', `${esc(a.name || '')} 계좌의 ${esc(editing.name || editing.code)} ${esc(qty(editing.qty, editing.kind, editing.code))} 기록을 지워요.`, '지우기', true))) return;
      try { await api('/api/lot/delete', { id: editing.id }); await loadPortfolio(); render(); modal.close(); toast('기록을 지웠어요'); } catch (e) { toast(e.message); }
    };
    const sync = $('[data-nhsync]', body);
    if (sync) sync.onclick = async () => {
      sync.disabled = true; sync.textContent = '나무증권에서 불러오는 중…';
      try { const r = await api('/api/nh/sync', {}); await loadPortfolio(); render(); modal.close(); toast(`나무증권에서 ${r.imported}개 종목을 불러왔어요`); }
      catch (e) { toast(e.message, 4000); sync.disabled = false; sync.innerHTML = `${ICON.down}나무증권 계좌에서 한 번에 불러오기`; }
    };
    const setup = $('[data-nhsetup]', body);
    if (setup) setup.onclick = () => { modal.close(); openSettings('nh'); };
  }

  async function save() {
    const nq = parseNum(D.qty), na = parseNum(D.avg);
    if (!D.sel) { toast('종목을 골라 주세요'); return; }
    if (!D.account) { toast(D.kind === 'stock' ? '증권사 계좌를 골라 주세요' : '거래소를 골라 주세요'); return; }
    if (!(nq > 0)) { toast('보유 수량을 넣어 주세요'); $('#lotQty', body).focus(); return; }
    const btn = $('[data-save]', body); btn.disabled = true;
    try {
      const r = await api('/api/lot/save', { id: editing ? editing.id : '', kind: D.kind, code: D.sel.code, name: D.sel.name, market: D.sel.market || '', account: D.account, qty: nq, avg: na });
      rememberUI('lastAccount_' + D.kind, D.account);
      if (D.quote) { S.quotes[`${D.kind}:${D.sel.code}`] = D.quote; }
      await loadPortfolio();
      render();
      modal.close();
      toast(editing ? '저장했어요' : r.merged ? '같은 계좌의 기록과 합쳤어요' : `${D.sel.name}을(를) 추가했어요`);
    } catch (e) { toast(e.message, 3500); btn.disabled = false; }
  }

  draw();
  const qi = $('#lotQ', body);
  if (qi) qi.focus(); else { const i = $('#lotQty', body); if (i) i.focus(); }
}

// ---------------------------------------------------------------- 설정

const SET = { open: false, el: null, editKeys: false };

function openSettings(focus) {
  SET.open = true;
  SET.editKeys = false;
  SET.el = modal.open(`<div class="dhead"><div style="display:flex;align-items:center;gap:12px">
      <button type="button" class="x" style="margin:0 0 0 -10px" data-x aria-label="닫기">${ICON.back}</button><h1>설정</h1></div>
      <span class="logo" style="font-size:14px;color:var(--muted)">MFROAD</span></div>
    <div id="setBody" style="display:contents"></div>`, 'settings', () => { SET.open = false; SET.el = null; });
  $('[data-x]', SET.el).onclick = () => modal.close();
  drawSettings();
  if (focus === 'nh') { const k = $('#nhKey', SET.el); if (k) k.focus(); }
}

function segHTML(name, cur, opts) {
  return `<div class="seg wide" role="group" aria-label="${esc(name)}">${opts.map(([v, l]) => `<button type="button" data-${name}="${v}" aria-pressed="${v === cur}">${l}</button>`).join('')}</div>`;
}
function switchHTML(id, on, label) {
  return `<button type="button" class="switch" role="switch" id="${id}" aria-checked="${!!on}" aria-label="${esc(label)}"><span></span></button>`;
}
function ago(ms) {
  if (!ms) return '';
  const s = Math.round((Date.now() - ms) / 1000);
  if (s < 60) return '방금';
  if (s < 3600) return `${Math.floor(s / 60)}분 전`;
  return clock(ms).slice(0, 5);
}

function nhSectionHTML() {
  const nh = S.nh || {};
  const st = !nh.configured ? '<span class="status off"><span class="dot"></span>연결 안 됨</span>'
    : nh.state === 'error' ? '<span class="status bad"><span class="dot"></span>확인 필요</span>'
      : '<span class="status"><span class="dot"></span>연결됨</span>';
  let h = `<section class="sec" aria-labelledby="sNH"><div class="hd"><div><h2 id="sNH">나무증권 연결</h2><span class="d">국내주식 실시간 시세와 계좌 잔고에 쓰여요</span></div>${st}</div>`;
  if (!nh.configured || SET.editKeys) {
    h += `<div class="hint">nhplug.com 에서 'API 사용신청'으로 받은 <b>App Key</b> 와 <b>App Secret</b> 을 붙여 넣어 주세요.</div>
      <div class="field"><label for="nhKey">App Key</label><input class="inp" id="nhKey" autocomplete="off" spellcheck="false" placeholder="App Key"></div>
      <div class="field"><label for="nhSecret">App Secret</label><input class="inp" id="nhSecret" type="password" autocomplete="off" spellcheck="false" placeholder="App Secret"></div>
      <div class="between"><div><span class="flabel">계정 종류</span></div>${segHTML('brand', nh.brand === 'n2plug' ? 'n2plug' : 'nhplug', [['nhplug', '나무증권'], ['n2plug', 'NH투자증권(N2)']])}</div>
      <div class="hint warn" id="nhErr" hidden></div>
      <div class="dfoot">${SET.editKeys ? '<button type="button" class="btn" data-keycancel>취소</button>' : ''}<button type="button" class="btn primary" data-keysave>저장하고 연결</button></div>`;
  } else {
    const left = nh.tokenLeft || 0;
    const tl = left > 0 ? `${Math.floor(left / 3600)}시간 ${Math.floor((left % 3600) / 60)}분 남음 · 자동으로 다시 받아요` : '필요할 때 자동으로 받아요';
    h += `<div style="display:flex;flex-direction:column;gap:8px">
        <div class="keyrow"><span class="k">App Key</span><span class="v">•••••••••••••••• ${esc(nh.keyTail || '')}</span><button type="button" class="btn small" data-keyedit>변경</button></div>
        <div class="keyrow"><span class="k">App Secret</span><span class="v">•••••••••••••••• ••••</span><button type="button" class="btn small" data-keyedit>변경</button></div></div>`;
    if (nh.state === 'error' && nh.error) h += `<div class="hint warn">${esc(nh.error)}</div>`;
    h += `<div class="between"><span class="d" id="nhToken">접근 토큰 · ${esc(tl)}</span><button type="button" class="btn small ghost" data-nhtest>연결 확인</button></div>`;
  }
  h += `<div class="safe">${ICON.lock}<span>키는 이 Mac의 키체인에만 저장돼요. 다른 곳으로 보내지 않아요. MFROAD는 조회만 하고 주문은 하지 않아요.</span></div>`;
  if (nh.configured && !SET.editKeys) {
    const last = nh.lastSync ? `마지막 불러오기 ${ago(nh.lastSync)} · ${nh.synced}종목` : '아직 불러오지 않았어요';
    h += `<div class="hr"></div>
      <div class="between"><div><span class="t">보유 종목을 계좌에서 자동으로 불러오기</span><span class="d">켤 때와 10분마다 · 다른 증권사 주식은 직접 추가해요</span></div>${switchHTML('swImport', S.settings.autoImport, '보유 종목 자동 불러오기')}</div>
      <div class="between"><span class="d" id="nhSync">${esc(nh.syncing ? '불러오는 중…' : last)}${nh.syncError ? ' · ' + esc(nh.syncError) : ''}</span><button type="button" class="btn small" data-sync ${nh.syncing ? 'disabled' : ''}>지금 불러오기</button></div>
      <div><button type="button" class="link d" data-forget>나무증권 연결 해제</button></div>`;
  }
  return h + '</section>';
}

function drawSettings() {
  if (!SET.el) return;
  const s = S.settings, u = S.update || {};
  const coinErr = S.market && S.market.coinError;
  let h = nhSectionHTML();
  h += `<section class="sec" aria-labelledby="sUp"><div class="hd"><div><h2 id="sUp">코인 · 업비트</h2><span class="d">공개 시세라 키 없이 바로 써요</span></div>
    ${coinErr ? '<span class="status bad"><span class="dot"></span>확인 필요</span>' : '<span class="status"><span class="dot"></span>사용 중</span>'}</div>
    ${coinErr ? `<div class="hint warn">${esc(coinErr)}</div>` : ''}</section>`;
  h += `<section class="sec" aria-labelledby="sRf"><div class="hd"><div><h2 id="sRf">시세 갱신</h2><span class="d">장이 닫혀 있을 때는 마지막 가격을 보여줘요</span></div></div>
    ${segHTML('refresh', s.refresh, [['live', '실시간 (권장)'], ['5s', '5초'], ['30s', '30초'], ['60s', '1분']])}</section>`;
  h += `<section class="sec" aria-labelledby="sTh"><h2 id="sTh">화면</h2>
    ${segHTML('appearance', s.appearance, [['system', '맥 설정 따름'], ['light', '라이트'], ['dark', '다크']])}</section>`;
  // 계좌 관리
  const accts = S.pf.accounts;
  h += `<section class="sec" aria-labelledby="sAc"><div class="hd"><div><h2 id="sAc">계좌 관리</h2><span class="d">같은 종목을 여러 계좌에 넣으면 합쳐서 보여 드려요</span></div></div>`;
  if (!accts.length) h += `<div class="hint">아직 계좌가 없어요. '종목 추가'에서 만들 수 있어요.</div>`;
  else {
    h += `<div class="acctlist">`;
    for (const kind of ['stock', 'coin']) {
      for (const a of accts.filter((x) => x.kind === kind)) {
        const n = S.pf.lots.filter((l) => l.account === a.id).length;
        h += `<div class="acctrow" data-acctrow="${esc(a.id)}"><span class="n"><span class="tag ${a.source === 'nh' ? 'auto' : ''}">${kind === 'stock' ? '주식' : '코인'}</span>${esc(a.name)}<span class="muted">${n}종목${a.source === 'nh' ? ' · 자동' : ''}</span></span>
          <button type="button" class="btn small ghost" data-rename="${esc(a.id)}">이름 바꾸기</button><button type="button" class="btn small ghost danger" data-delacct="${esc(a.id)}">지우기</button></div>`;
      }
    }
    h += `</div>`;
  }
  h += `</section>`;
  // 업데이트
  const lc = u.lastCheck ? `마지막 확인 ${ago(u.lastCheck)}` : '';
  h += `<section class="sec" aria-labelledby="sUd"><div class="hd"><div><h2 id="sUd">업데이트</h2><span class="d">현재 버전 ${esc(S.build)}${lc ? ' · ' + esc(lc) : ''}</span></div>
      <button type="button" class="btn primary" data-upd>업데이트 확인</button></div>
    <div class="hr"></div>
    <div class="between"><div><span class="t">새 버전 자동으로 받기</span><span class="d">새 버전이 나오면 앱이 알아서 받아요</span></div>${switchHTML('swUpdate', s.autoUpdate, '자동 업데이트')}</div>
    ${u.canRollback ? `<div><button type="button" class="link d" data-rollback>이전 버전(${esc(u.backupVersion)})으로 되돌리기</button></div>` : ''}</section>`;
  h += `<div class="sfoot"><a class="link" href="/diag" target="_blank" rel="noopener" style="font-size:13px;font-weight:600">진단 정보 보기</a><span>made by. Nevertheless_D</span></div>`;
  const body = $('#setBody', SET.el);
  const keep = document.activeElement && document.activeElement.id;
  const keyVals = { k: ($('#nhKey', body) || {}).value, s: ($('#nhSecret', body) || {}).value };
  body.innerHTML = h;
  if (keyVals.k && $('#nhKey', body)) $('#nhKey', body).value = keyVals.k;
  if (keyVals.s && $('#nhSecret', body)) $('#nhSecret', body).value = keyVals.s;
  if (keep && $('#' + keep, body)) $('#' + keep, body).focus();
  bindSettings(body);
  SET.sig = settingsSig();
}

function settingsSig() {
  const nh = S.nh || {};
  return JSON.stringify([nh.configured, nh.state, nh.error, nh.syncing, nh.syncError, nh.keyTail, S.market && S.market.coinError, S.update && S.update.canRollback, S.pf.rev]);
}

SET.refreshStatus = () => {
  if (!SET.el) return;
  if (settingsSig() !== SET.sig && !($('#nhKey', SET.el) && ($('#nhKey', SET.el).value || $('#nhSecret', SET.el).value))) { drawSettings(); return; }
  const nh = S.nh || {};
  const t = $('#nhToken', SET.el);
  if (t && nh.tokenLeft > 0) t.textContent = `접근 토큰 · ${Math.floor(nh.tokenLeft / 3600)}시간 ${Math.floor((nh.tokenLeft % 3600) / 60)}분 남음 · 자동으로 다시 받아요`;
};

function bindSettings(body) {
  $$('[data-refresh]', body).forEach((b) => b.onclick = async () => { await saveSettings({ refresh: b.dataset.refresh }); drawSettings(); renderLive(); });
  $$('[data-appearance]', body).forEach((b) => b.onclick = async () => { await saveSettings({ appearance: b.dataset.appearance }); drawSettings(); });
  $$('[data-brand]', body).forEach((b) => b.onclick = () => { $$('[data-brand]', body).forEach((x) => x.setAttribute('aria-pressed', String(x === b))); });
  const sw = (id, key) => { const el = $('#' + id, body); if (el) el.onclick = async () => { const v = el.getAttribute('aria-checked') !== 'true'; el.setAttribute('aria-checked', String(v)); await saveSettings({ [key]: v }); }; };
  sw('swImport', 'autoImport'); sw('swUpdate', 'autoUpdate');
  $$('[data-keyedit]', body).forEach((b) => b.onclick = () => { SET.editKeys = true; drawSettings(); $('#nhKey', SET.el).focus(); });
  const kc = $('[data-keycancel]', body); if (kc) kc.onclick = () => { SET.editKeys = false; drawSettings(); };
  const ks = $('[data-keysave]', body);
  if (ks) ks.onclick = async () => {
    const key = $('#nhKey', body).value.trim(), secret = $('#nhSecret', body).value.trim();
    const brand = ($('[data-brand][aria-pressed="true"]', body) || {}).dataset?.brand || 'nhplug';
    const err = $('#nhErr', body);
    if (!key || !secret) { err.hidden = false; err.textContent = 'App Key 와 App Secret 을 모두 넣어 주세요'; return; }
    ks.disabled = true; ks.textContent = '확인하는 중…'; err.hidden = true;
    try {
      const r = await api('/api/nh/keys', { appKey: key, appSecret: secret, brand });
      $('#nhKey', body).value = ''; $('#nhSecret', body).value = '';
      S.nh = r.nh; SET.editKeys = false;
      drawSettings(); render();
      toast(r.warning ? `연결했어요 · ${r.warning}` : `나무증권을 연결했어요 (계좌 ${r.accounts}개)`, 3500);
    } catch (e) {
      err.hidden = false; err.textContent = e.message; ks.disabled = false; ks.textContent = '저장하고 연결';
    }
  };
  const test = $('[data-nhtest]', body);
  if (test) test.onclick = async () => {
    test.disabled = true; test.textContent = '확인 중…';
    try { const r = await api('/api/nh/test', {}); S.nh = r.nh; toast(`잘 연결되어 있어요 (계좌 ${r.accounts}개)`); }
    catch (e) { toast(e.message, 4000); }
    test.disabled = false; test.textContent = '연결 확인'; drawSettings();
  };
  const sy = $('[data-sync]', body);
  if (sy) sy.onclick = async () => {
    sy.disabled = true; sy.textContent = '불러오는 중…';
    try { const r = await api('/api/nh/sync', {}); S.nh = r.nh; await loadPortfolio(); render(); toast(`나무증권에서 ${r.imported}개 종목을 불러왔어요`); }
    catch (e) { toast(e.message, 4000); }
    drawSettings();
  };
  const fg = $('[data-forget]', body);
  if (fg) fg.onclick = async () => {
    if (!(await confirmBox('나무증권 연결을 해제할까요?', '키체인에 저장한 키를 지워요. 이미 불러온 나무증권 계좌의 기록은 남아 있고, 필요 없으면 아래 「계좌 관리」에서 지울 수 있어요.', '연결 해제', true))) return;
    try { const r = await api('/api/nh/forget', {}); S.nh = r.nh; drawSettings(); render(); toast('나무증권 연결을 해제했어요'); } catch (e) { toast(e.message); }
  };
  $$('[data-rename]', body).forEach((b) => b.onclick = () => {
    const id = b.dataset.rename, a = acctById(id); if (!a) return;
    const row = $(`[data-acctrow="${id}"]`, body);
    row.innerHTML = `<input class="inp" style="height:38px" maxlength="24" value="${esc(a.name)}"><button type="button" class="btn small" data-ok>저장</button><button type="button" class="btn small ghost" data-no>취소</button>`;
    const inp = $('input', row); inp.focus(); inp.select();
    const ok = async () => { try { await api('/api/account/save', { id, name: inp.value }); await loadPortfolio(); render(); drawSettings(); } catch (e) { toast(e.message); } };
    $('[data-ok]', row).onclick = ok; $('[data-no]', row).onclick = () => drawSettings();
    inp.onkeydown = (e) => { if (e.key === 'Enter') ok(); if (e.key === 'Escape') { e.stopPropagation(); drawSettings(); } };
  });
  $$('[data-delacct]', body).forEach((b) => b.onclick = async () => {
    const a = acctById(b.dataset.delacct); if (!a) return;
    const n = S.pf.lots.filter((l) => l.account === a.id).length;
    const more = a.source === 'nh' && S.nh.configured && S.settings.autoImport ? '<br>나무증권 자동 불러오기가 켜져 있으면 다음에 다시 불러와져요.' : '';
    if (!(await confirmBox(`'${a.name}' 계좌를 지울까요?`, `${n ? `이 계좌의 기록 ${n}개도 함께 지워져요.` : '이 계좌에는 기록이 없어요.'}${more}`, '지우기', true))) return;
    try { await api('/api/account/delete', { id: a.id }); await loadPortfolio(); render(); drawSettings(); toast('계좌를 지웠어요'); } catch (e) { toast(e.message); }
  });
  const up = $('[data-upd]', body); if (up) up.onclick = () => openUpdate(true);
  const rb = $('[data-rollback]', body);
  if (rb) rb.onclick = async () => {
    if (!(await confirmBox('이전 버전으로 되돌릴까요?', `${esc(S.update.backupVersion)} 버전으로 바꾸고, 자동 업데이트는 꺼 둘게요. 바꾼 뒤 다시 시작해 주세요.`, '되돌리기'))) return;
    try { await api('/api/update/rollback', {}); toast('되돌렸어요. 다시 시작하면 이전 버전으로 열려요'); S.settings.autoUpdate = false; drawSettings(); } catch (e) { toast(e.message, 4000); }
  };
}

// ---------------------------------------------------------------- 업데이트 창

const UPD = { open: false, el: null };
async function openUpdate(check) {
  if (!UPD.open) {
    UPD.open = true;
    UPD.el = modal.open('<div id="updBody" style="display:contents"></div>', 'small', () => { UPD.open = false; UPD.el = null; });
  }
  UPD.refresh();
  if (check) {
    try { S.update = await api('/api/update/check', {}); } catch (e) { /* 상태에 담겨 와요 */ }
    UPD.refresh();
  }
}
UPD.refresh = () => {
  if (!UPD.el) return;
  const u = S.update || {}, rel = u.latest || {};
  let title = '업데이트', sub = `지금 쓰는 버전 ${esc(u.current || S.build)}`, mid = '', buttons = '<button type="button" class="btn" data-close>닫기</button>';
  switch (u.status) {
    case 'checking': title = '새 버전을 확인하고 있어요'; break;
    case 'available':
      title = '새 버전이 나왔어요'; sub = `MFROAD <b class="num">${esc(rel.version)}</b> · 지금 쓰는 버전 ${esc(u.current)}`;
      buttons = '<button type="button" class="btn" data-close>나중에</button><button type="button" class="btn primary" data-install style="min-width:140px">지금 업데이트</button>';
      break;
    case 'downloading': case 'installing':
      title = '새 버전을 받고 있어요'; sub = `MFROAD <b class="num">${esc(rel.version)}</b>`;
      mid = `<div style="display:flex;flex-direction:column;gap:8px"><div class="between"><span class="d">${u.status === 'installing' ? '설치하는 중이에요' : '받는 중이에요. 끝나면 다시 시작하면 돼요.'}</span><span class="num b">${Math.round((u.progress || 0) * 100)}%</span></div>
        <div class="progress" role="progressbar" aria-label="업데이트 받는 중" aria-valuenow="${Math.round((u.progress || 0) * 100)}" aria-valuemin="0" aria-valuemax="100"><div style="width:${Math.round((u.progress || 0) * 100)}%"></div></div></div>`;
      buttons = '<button type="button" class="btn" data-close>닫기</button>';
      break;
    case 'ready':
      title = '새 버전이 준비됐어요'; sub = `다시 시작하면 MFROAD <b class="num">${esc(u.installed)}</b>로 열려요`;
      buttons = '<button type="button" class="btn" data-close>나중에</button><button type="button" class="btn primary" data-restart style="min-width:140px">다시 시작</button>';
      break;
    case 'uptodate': title = '최신 버전이에요'; break;
    case 'error': title = '업데이트를 확인하지 못했어요'; mid = `<div class="hint warn">${esc(u.error)}</div>`; buttons = '<button type="button" class="btn" data-close>닫기</button><button type="button" class="btn primary" data-recheck>다시 확인</button>'; break;
  }
  const notes = (rel.notes && rel.notes.length && ['available', 'downloading', 'installing', 'ready'].includes(u.status))
    ? `<div class="notes"><span class="flabel">바뀐 점</span>${rel.notes.map((n) => `<span>• ${esc(n)}</span>`).join('')}</div>` : '';
  const html = `<div class="updhead"><span class="mark">${ICON.mark}</span><div style="display:flex;flex-direction:column;gap:3px"><h1>${title}</h1><span class="d" style="font-size:14px;color:var(--muted)">${sub}</span></div></div>
    ${notes}${mid}<div class="dfoot">${buttons}</div>`;
  const b = $('#updBody', UPD.el);
  if (b.innerHTML === html) return;
  b.innerHTML = html;
  const on = (sel, fn) => { const x = $(sel, b); if (x) x.onclick = fn; };
  on('[data-close]', () => modal.close());
  on('[data-install]', async () => { try { await api('/api/update/install', {}); S.update.status = 'downloading'; UPD.refresh(); } catch (e) { toast(e.message); } });
  on('[data-restart]', restartApp);
  on('[data-recheck]', () => openUpdate(true));
};

async function restartApp() {
  try { await api('/api/update/restart', {}); toast('새 버전으로 다시 여는 중…', 6000); } catch (e) { toast(e.message, 4000); }
}

// ---------------------------------------------------------------- 이벤트

$('#tabs').addEventListener('click', (e) => {
  const b = e.target.closest('button[data-tab]');
  if (!b) return;
  S.tab = b.dataset.tab;
  rememberUI('tab', S.tab);
  render();
});
$('#btnAdd').onclick = () => openLot();
$('#btnSettings').onclick = () => openSettings();
$('#btnVersion').onclick = () => openUpdate(false);
$('#btnRefresh').onclick = async () => {
  const b = $('#btnRefresh');
  b.classList.add('spin');
  try {
    if (S.nh.configured && S.settings.autoImport) await api('/api/nh/sync', {}).catch((e) => toast(e.message, 3500));
    await api('/api/settings', {}); // 시세 반복을 깨워요
    S.qrev = '';
    await loadPortfolio(); await poll(); await loadHistory(); render();
  } finally { setTimeout(() => b.classList.remove('spin'), 400); }
};

document.addEventListener('click', async (e) => {
  const t = e.target.closest('[data-act]');
  if (!t || t.closest('.dialog')) return;
  const act = t.dataset.act;
  if (act === 'toggle') {
    const k = t.dataset.key;
    if (S.expanded.has(k)) S.expanded.delete(k); else S.expanded.add(k);
    render();
  } else if (act === 'edit') {
    const lot = S.pf.lots.find((l) => l.id === t.dataset.lot);
    if (lot) openLot({ lot });
  } else if (act === 'add') openLot();
  else if (act === 'settings') openSettings('nh');
  else if (act === 'update') openUpdate(false);
  else if (act === 'restart') restartApp();
  else if (act === 'hideNHTip') { rememberUI('hideNHTip', true); render(); }
  else if (act === 'sync') {
    t.disabled = true;
    try { const r = await api('/api/nh/sync', {}); await loadPortfolio(); render(); toast(`나무증권에서 ${r.imported}개 종목을 불러왔어요`); }
    catch (err) { toast(err.message, 4000); t.disabled = false; }
  }
});

// 창이 다시 보이면 바로 새로 묻기
document.addEventListener('visibilitychange', () => { if (!document.hidden) poll(); });

// ---------------------------------------------------------------- 시작

async function start() {
  if (!TOKEN) { document.body.innerHTML = '<p style="padding:40px">MFROAD 앱을 다시 실행해 주세요.</p>'; return; }
  try {
    S.settings = await api('/api/settings');
    S.build = S.settings.build;
    S.tab = ['all', 'stock', 'coin'].includes(S.settings.ui.tab) ? S.settings.ui.tab : 'all';
    applyTheme();
    await loadPortfolio();
  } catch (e) { $('#offline').hidden = false; }
  render();
  await poll();
  await loadHistory();
  render();
  const loop = async () => {
    await poll();
    setTimeout(loop, S.settings.refresh === 'live' ? 1000 : 2000);
  };
  setTimeout(loop, 1000);
  setInterval(loadHistory, 60_000);
  setInterval(() => { if (Object.keys(S.flash).length) { const now = Date.now(); let any = false; for (const k in S.flash) if (now - S.flash[k].at > 1300) { delete S.flash[k]; any = true; } if (any) $$('.price.f-up, .price.f-down').forEach((p) => p.classList.remove('f-up', 'f-down')); } }, 400);
}
start();
