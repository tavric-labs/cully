(() => {
  'use strict';
  const $ = (id) => document.getElementById(id);

  // Build DOM without ever parsing agent text as markup.
  // The browser's own replaceChildren, append and prepend turn a null or false
  // argument into the text "null" or "false". Pages build lists with optional
  // parts, so drop empty arguments here once instead of in every call.
  for (const name of ['replaceChildren', 'append', 'prepend']) {
    const native = Element.prototype[name];
    Element.prototype[name] = function (...kids) { return native.apply(this, kids.filter((k) => k != null && k !== false)); };
  }

  function h(tag, props, ...kids) {
    const el = document.createElement(tag);
    for (const [k, v] of Object.entries(props || {})) {
      if (v == null || v === false) continue;
      if (k === 'class') el.className = v;
      else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
      else el.setAttribute(k, v === true ? '' : v);
    }
    for (const kid of kids.flat()) if (kid != null && kid !== false) el.append(kid.nodeType ? kid : document.createTextNode(String(kid)));
    return el;
  }

  // Static icon paths; elements are built with the DOM, never parsed from strings.
  const ICONS = {
    plug: 'M9 3v5M15 3v5M6 8h12v4a6 6 0 0 1-12 0zM12 18v3',
    mic: 'M12 3a3 3 0 0 0-3 3v6a3 3 0 0 0 6 0V6a3 3 0 0 0-3-3zM5 11a7 7 0 0 0 14 0M12 18v3',
    clock: 'M12 7v5l3 2M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18z',
    plus: 'M12 5v14M5 12h14', send: 'M12 19V5M5 12l7-7 7 7', stop: 'M7 7h10v10H7z', copy: 'M9 9h11v11H9zM5 15V4h11',
    refresh: 'M20 11a8 8 0 1 0-2.3 5.7M20 4v7h-7', check: 'M5 12l5 5L20 7', down: 'M12 5v14M5 12l7 7 7-7',
    lock: 'M7 11V8a5 5 0 0 1 10 0v3M5 11h14v10H5z', external: 'M14 4h6v6M20 4l-9 9M18 14v5H5V6h5',
    alert: 'M12 8v5M12 17h.01M10.3 4.2L2.6 18a2 2 0 0 0 1.7 3h15.4a2 2 0 0 0 1.7-3L13.7 4.2a2 2 0 0 0-3.4 0z',
    branch: 'M6 3v12M18 3a3 3 0 1 0 0 6a3 3 0 1 0 0-6M6 15a3 3 0 1 0 0 6a3 3 0 1 0 0-6M18 9a9 9 0 0 1-9 9',
    palette: 'M12 3a9 9 0 1 0 0 18a9 9 0 0 0 0-18zM12 3v18',
    eye: 'M2 12s4-7 10-7 10 7 10 7-4 7-10 7S2 12 2 12zM12 9a3 3 0 1 0 0 6a3 3 0 1 0 0-6',
    spark: 'M12 3l1.8 5.2L19 10l-5.2 1.8L12 17l-1.8-5.2L5 10l5.2-1.8zM19 16l.7 2.3L22 19l-2.3.7L19 22l-.7-2.3L16 19l2.3-.7z',
    bell: 'M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9M10.3 21a1.94 1.94 0 0 0 3.4 0',
    pencil: 'M4 20h4L20 8l-4-4L4 16z', term: 'M4 5l6 6-6 6M12 19h8', x: 'M6 6l12 12M18 6L6 18', folder: 'M3 6a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z',
    search: 'M11 4a7 7 0 1 0 0 14a7 7 0 1 0 0-14M21 21l-5-5',
    sliders: 'M4 7h9M17 7h3M4 17h3M11 17h9M13 4v6M7 14v6',
    globe: 'M12 3a9 9 0 1 0 0 18a9 9 0 1 0 0-18M3 12h18M12 3c3 3 3 15 0 18M12 3c-3 3-3 15 0 18',
  };
  function icon(name) {
    const NS = 'http://www.w3.org/2000/svg';
    const svg = document.createElementNS(NS, 'svg');
    for (const [k, v] of Object.entries({ viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': '2.2', 'stroke-linecap': 'round', 'stroke-linejoin': 'round', class: 'i', 'aria-hidden': 'true' })) svg.setAttribute(k, v);
    const path = document.createElementNS(NS, 'path');
    path.setAttribute('d', ICONS[name]);
    svg.append(path);
    return svg;
  }

  const S = {
    agents: [], overlaps: [], info: null, route: { name: 'home' },
    es: null, feed: null, tab: 'chat', draft: '', notes: {}, opts: { context: true, recall: false, scout: false, mode: 'edit', repo: '' },
    diffKey: '', panelCache: {},
  };
  const PANELS = {
    advisor: 'Advice for your tasks', scout: 'What the scout searched and found', handoff: 'Where the work stands',
    status: 'Session health and setup', timeline: 'What happened, step by step', replay: 'Files, commands and checks',
    suggestions: 'Tips from your terminal sessions', impact: 'What Cully saved and cost', jobs: 'Background jobs', rescue: 'Is a session stuck?',
  };
  const PANEL_TITLES = {
    advisor: 'Advisor', scout: 'Scout', handoff: 'Where it left off', status: 'Status', timeline: 'Timeline', replay: 'Replay',
    suggestions: 'Terminal tips', impact: 'Savings', jobs: 'Background jobs', rescue: 'Stuck session check',
  };
  const GROUPS = [['waiting', 'Needs you'], ['working', 'Working'], ['finished', 'Finished']];
  const groupOf = (a) => (a.state === 'waiting' ? 'waiting' : a.state === 'working' || a.state === 'preparing' ? 'working' : 'finished');

  // ---------- small helpers ----------
  function joinNames(list) {
    if (list.length <= 1) return list.join('');
    return list.slice(0, -1).join(', ') + ' and ' + list[list.length - 1];
  }
  function ago(iso) {
    if (!iso) return '';
    const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
    if (s < 10) return 'just now';
    if (s < 60) return Math.floor(s) + 's ago';
    if (s < 3600) return Math.floor(s / 60) + 'm ago';
    if (s < 86400) return Math.floor(s / 3600) + 'h ago';
    return Math.floor(s / 86400) + 'd ago';
  }
  function toast(msg, bad) {
    const t = h('div', { class: 'toast' + (bad ? ' bad' : '') }, msg);
    $('toasts').append(t);
    setTimeout(() => t.remove(), bad ? 6000 : 2800);
  }
  // The clipboard API only exists on secure pages (https or localhost); the page is
  // often opened over plain http on a VM, so fall back to a hidden selection.
  async function writeClip(text) {
    if (navigator.clipboard && window.isSecureContext) { await navigator.clipboard.writeText(text); return; }
    const ta = h('textarea', { 'aria-hidden': 'true', tabindex: '-1', readonly: 'true' });
    ta.value = text;
    ta.style.position = 'fixed'; ta.style.opacity = '0'; ta.style.top = '0';
    document.body.append(ta);
    ta.select();
    let ok = false;
    try { ok = document.execCommand('copy'); } catch (_) { ok = false; }
    ta.remove();
    if (!ok) throw new Error('copy is blocked');
  }
  // A task is shown by its phase (what it is doing now); the stored state stays as a class beside it.
  const STATE_WORDS = { preparing: 'Getting ready', working: 'Working', waiting: 'Needs you', done: 'Done', failed: 'Failed', stopped: 'Stopped', queued: 'Queued' };
  const stateLabel = (a) => (a && typeof a === 'object' ? a.phaseLabel || STATE_WORDS[a.state] || a.state || '' : STATE_WORDS[a] || a || '');
  const phaseCls = (a) => (a && typeof a === 'object' ? a.state + ' ph-' + (a.phase || a.state) : a + ' ph-' + a);
  // What to say a task was about, in one place so every list agrees. The summary comes first, then what you asked;
  // a last line such as "turn done" says nothing, and a raw error is clipped (the full text stays in the tooltip).
  const LOW_SIGNAL = /^(turn done|done|ok|okay|finished|complete|completed|stopped|started)\.?$/i;
  function taskBlurb(t) {
    const clean = (x) => String(x || '').replace(/\s+/g, ' ').trim();
    const sum = clean(t.summary); const ask = clean(t.prompt); const last = clean(t.last);
    if (sum) return { text: sum, full: sum };
    if (last && !LOW_SIGNAL.test(last) && (t.state === 'failed' || last.length >= 24)) return { text: last, full: last, weak: t.state === 'failed' };
    if (ask) return { text: 'Asked: ' + ask, full: ask, weak: true };
    return { text: 'No summary yet. It appears once a session of this task has been analysed.', full: '', weak: true };
  }
  const taskPair = (t) => {
    const ph = t.phase || t.state;
    const tone = { done: 'good', failed: 'bad', limit: 'bad', 'needs-you': 'wait', stuck: 'wait' }[ph] || 'info';
    return [ph === 'done' ? 'Finished' : stateLabel(t), tone];
  };
  async function copy(text, what) {
    try { await writeClip(text); toast('Copied ' + what); } catch (_) { toast('Copy is blocked here; select and copy manually', true); }
  }

  function showNewVersion() {
    S.stale = true;
    const b = $('banner');
    b.replaceChildren('A new version of Cully UI is available. ', h('button', { class: 'btn small', onclick: () => location.reload() }, 'Reload'));
    b.hidden = false;
  }

  async function api(path, opts = {}) {
    let res;
    try {
      res = await fetch(path, {
        credentials: 'same-origin', ...opts,
        headers: { 'X-Cully-UI': '1', ...(opts.body ? { 'Content-Type': 'application/json' } : {}) },
      });
    } catch (e) { $('live').classList.add('down'); throw e; }
    $('live').classList.remove('down');
    const build = res.headers.get('X-Cully-Build');
    if (build) {
      if (!S.build) S.build = build;
      else if (build !== S.build && !S.stale) showNewVersion();
    }
    if (res.status === 401) {
      const b = $('banner');
      b.textContent = 'Not signed in. Open the URL printed by `cully ui` (it ends in ?token=...).';
      b.hidden = false;
      throw new Error('unauthorized');
    }
    if (!S.stale) $('banner').hidden = true;
    if (!res.ok) {
      let msg = res.statusText;
      let paused = false;
      try { const j = await res.json(); msg = j.error || msg; paused = !!j.paused; } catch (_) { /* not JSON */ }
      if (paused && window.confirm(msg)) return api(path + (path.includes('?') ? '&' : '?') + 'anyway=1', opts);
      const err = new Error(msg); err.status = res.status; throw err;
    }
    if (res.status === 204) return null;
    return (res.headers.get('Content-Type') || '').includes('json') ? res.json() : res.text();
  }

  // ---------- markdown-lite (text nodes only) ----------
  function inline(text) {
    const out = [];
    for (const part of text.split(/(`[^`\n]+`|\*\*[^*\n]+\*\*|https?:\/\/[^\s<>"')\]]+)/)) {
      if (!part) continue;
      if (/^https?:\/\//.test(part)) {
        const m = /^(.*?)([.,;:!?]*)$/.exec(part);
        out.push(h('a', { href: m[1], target: '_blank', rel: 'noopener noreferrer', class: 'ext' }, m[1]), m[2]);
      } else if (part.startsWith('`') && part.endsWith('`') && part.length > 2) out.push(h('code', {}, part.slice(1, -1)));
      else if (part.startsWith('**') && part.endsWith('**') && part.length > 4) out.push(h('b', {}, part.slice(2, -2)));
      else out.push(part);
    }
    return out;
  }
  // Agent replies are Markdown; md.js builds them as DOM (never from HTML strings).
  function markdown(text) { return window.CullyMD.render(text); }

  // ---------- routing ----------
  function parseHash() {
    const p = location.hash.replace(/^#\/?/, '').split('/');
    if ((p[0] === 'task' || p[0] === 'agent') && p[1]) return { name: 'agent', id: p[1] }; // #/agent/ is the old address
    if (p[0] === 'mcp') return { name: 'home', mcp: p[1] || 'claude' };
    if (p[0] === 'tasks') return { name: 'tasks' };
    if (p[0] === 'sessions') {
      if (p[1] === 'live' && p[2]) return { name: 'session-live', id: p[2] };
      if (p[1]) return { name: 'session', id: p[1] };
      return { name: 'sessions' };
    }
    if (p[0] === 'board') return { name: 'board' };
    if (p[0] === 'agents') return { name: 'team' };
    if (p[0] === 'harness') return { name: 'harness', tab: p[1] || '' };
    if (p[0] === 'setup') return { name: 'setup' };
    if (p[0] === 'improve') return { name: 'improve', tab: p[1] || 'ideas' };
    if (p[0] === 'dashboard') return { name: 'dashboard', panel: p[1] || 'overview', kind: p[2] || '' };
    return { name: 'home' };
  }
  const REDIRECT = { status: 'savings', timeline: 'activity', replay: 'activity', handoff: 'activity', suggestions: 'advisor', impact: 'savings', rescue: 'overview' };
  function route() {
    if (location.hash === '#/dashboard/setup' || location.hash === '#/dashboard/status') { location.replace('#/setup'); return; }
    if (location.hash === '#/improve/rules') { location.replace('#/improve/rules'); return; }
    if (location.hash === '#/dashboard/permissions') { location.replace('#/harness/permissions'); return; }
    if (/^#\/agent\//.test(location.hash)) { location.replace(location.hash.replace('#/agent/', '#/task/')); return; }
    S.route = parseHash();
    if (S.route.name === 'dashboard' && REDIRECT[S.route.panel]) { location.replace('#/dashboard/' + REDIRECT[S.route.panel]); return; }
    const section = S.route.name === 'harness' ? 'harness' : S.route.name === 'team' ? 'team' : S.route.name === 'board' ? 'board' : S.route.name === 'setup' ? 'setup' : S.route.name === 'improve' ? 'improve' : S.route.name === 'dashboard' ? 'dashboard' : ['sessions', 'session', 'session-live'].includes(S.route.name) ? 'sessions' : 'agents';
    for (const a of $('nav').querySelectorAll('a')) a.classList.toggle('active', a.dataset.route === section);
    { // Agents, Harness and Dashboard live under More: it lights up when you are on one of them, and shows their badge
      const more = $('navmore');
      if (more) { more.classList.toggle('active', ['team', 'harness', 'dashboard', 'setup', 'improve'].includes(section)); more.open = false; const db = $('dbadge'); const mb = $('morebadge'); if (db && mb) { mb.hidden = db.hidden; mb.textContent = db.textContent; } }
    }
    for (const s of ['agents', 'board', 'dashboard', 'sessions', 'team', 'harness', 'setup', 'improve']) $('view-' + s).hidden = s !== section;
    if (S.es) { S.es.close(); S.es = null; }
    S.feed = null;
    $('stage').className = 'stage';
    renderRail();
    if (S.route.name !== 'agent') { drawTaskPanel(null); $('view-agents').classList.remove('rail-hidden'); } // hiding the panel applies to the task window only
    if (S.route.name === 'agent') mountAgent(S.route.id);
    else if (S.route.name === 'home') mountHome();
    else if (S.route.name === 'board') renderBoard();
    else if (S.route.name === 'tasks') mountTasks();
    else if (S.route.name === 'team') renderTeam();
    else if (S.route.name === 'harness') renderHarness();
    else if (S.route.name === 'setup') renderSetupPage();
    else if (S.route.name === 'improve') renderImprove();
    else if (S.route.name === 'sessions') { S.sessSig = ''; renderSessions(); }
    else if (S.route.name === 'session') renderSessionDetail(S.route.id);
    else if (S.route.name === 'session-live') renderLiveSession(S.route.id);
    else mountDashboard(S.route.panel);
  }
  window.CullyKit = { h, api, toast, icon, toolbar, emptyCard, confirmDialog, ago };
  window.addEventListener('hashchange', route);

  // ---------- rail ----------
  function queuedChip(a) { const n = (a.queue || []).length; return n ? h('span', { class: 'chip', title: n + (n === 1 ? ' message is' : ' messages are') + ' waiting to be sent to this task' }, n + ' queued') : null; }
  function dot(a) { return h('span', { class: 'dot ' + (typeof a === 'string' ? a : phaseCls(a)) }); }
  function renderRail() {
    const rail = $('rail');
    const kids = [];
    for (const [key, label] of GROUPS) {
      const list = S.agents.filter((a) => groupOf(a) === key);
      if (!list.length) continue;
      kids.push(h('div', { class: 'grp' + (key === 'waiting' ? ' attn' : '') }, label, h('span', { class: 'n' }, list.length)));
      for (const a of list) {
        kids.push(h('a', { class: 'item' + (a.state === 'waiting' ? ' attn' : '') + (S.route.name === 'agent' && S.route.id === a.id ? ' sel' : ''), href: '#/task/' + a.id },
          h('div', { class: 'r1' }, dot(a), h('span', { class: 't' }, a.title)),
          h('div', { class: 'r2' }, a.last || (a.state === 'preparing' ? 'Preparing…' : '')),
          h('div', { class: 'r3' }, a.token ? h('span', { class: 'chip' }, 'token ' + a.token) : null,
            a.files && a.files.length ? h('span', { class: 'chip' }, a.files.length + (a.files.length === 1 ? ' file' : ' files')) : null,
            queuedChip(a),
            h('span', { class: 'chip' }, ago(a.updated || a.created)))));
      }
    }
    if (!kids.length) kids.push(h('div', { class: 'hint', style: null }, 'No tasks yet.'));
    rail.replaceChildren(...kids);
    const waiting = S.agents.filter((a) => a.state === 'waiting' || (a.permissions || []).length).length;
    $('badge').hidden = waiting === 0;
    $('badge').textContent = waiting;
    document.title = (waiting ? '(' + waiting + ') ' : '') + 'Cully';
    mountInbox(); drawInbox();
  }

  // ---------- approvals from anywhere ----------
  // Every question a task is waiting on, across all tasks, answerable from the header or the home page, so
  // an approval never needs you to find the right task window first. A high-risk call is allowed once only.
  const pendingAsks = () => S.agents.flatMap((a) => (a.permissions || []).map((p) => ({ a, p })));
  async function answerAsk(a, p, decision) {
    try { await api('/api/agents/' + a.id + '/permissions/' + p.id, { method: 'POST', body: JSON.stringify({ decision, hash: p.hash }) }); await refresh(); } catch (e) { toast(e.message, true); }
  }
  function askCard(a, p) {
    return h('div', { class: 'askcard2 risk-b-' + p.risk },
      h('div', { class: 'row' }, h('b', {}, a.title), h('span', { class: 'chip risk-' + p.risk }, ({ low: 'Low risk', medium: 'Medium risk', high: 'High risk' })[p.risk] || p.risk), h('span', { class: 'sp' }), h('a', { class: 'linkbtn', href: '#/task/' + a.id, onclick: () => closeInbox() }, 'Open task')),
      h('p', { class: 'faint small' }, 'asks to use ' + p.tool + (p.riskWhy ? ', which ' + p.riskWhy : '')),
      h('pre', { class: 'permcmd' }, p.summary || p.input),
      h('div', { class: 'row' }, h('button', { class: 'btn small', onclick: () => answerAsk(a, p, 'allow') }, icon('check'), 'Allow once'), h('button', { class: 'btn ghost small', onclick: () => answerAsk(a, p, 'deny') }, 'Deny')));
  }
  let inboxPop = null;
  function closeInbox() { if (inboxPop) { inboxPop.remove(); inboxPop = null; const b = $('inboxbtn'); if (b) b.setAttribute('aria-expanded', 'false'); document.removeEventListener('click', inboxOutside, true); } }
  function inboxOutside(e) { if (inboxPop && !inboxPop.contains(e.target) && !$('inboxbtn').contains(e.target)) closeInbox(); }
  function drawInbox() {
    const btn = $('inboxbtn'); if (!btn) return;
    const asks = pendingAsks(); const waiting = S.agents.filter((a) => a.state === 'waiting' && !(a.permissions || []).length);
    const n = asks.length + waiting.length;
    btn.hidden = n === 0 && !inboxPop;
    btn.className = 'inboxbtn' + (asks.length ? ' ask' : '');
    btn.replaceChildren(icon(asks.length ? 'lock' : 'bell'), n + (n === 1 ? ' needs you' : ' need you'));
    if (!inboxPop) return;
    inboxPop.replaceChildren(h('div', { class: 'menu-note' }, n ? 'Answer from here; the task carries on at once.' : 'Nothing needs you right now.'),
      ...asks.map(({ a, p }) => askCard(a, p)),
      ...waiting.map((a) => h('div', { class: 'askcard2' }, h('div', { class: 'row' }, h('b', {}, a.title), h('span', { class: 'sp' }), h('a', { class: 'btn small', href: '#/task/' + a.id, onclick: () => closeInbox() }, 'Reply')), h('p', { class: 'faint small' }, a.last || 'Waiting for your reply.'))));
  }
  function mountInbox() {
    const btn = $('inboxbtn'); if (!btn || btn.dataset.ready) return; btn.dataset.ready = '1';
    btn.addEventListener('click', () => {
      if (inboxPop) { closeInbox(); return; }
      inboxPop = h('div', { class: 'inboxpop', role: 'dialog', 'aria-label': 'Needs you' });
      document.body.append(inboxPop); btn.setAttribute('aria-expanded', 'true');
      document.addEventListener('click', inboxOutside, true);
      document.addEventListener('keydown', (e) => { if (e.key === 'Escape') closeInbox(); }, { once: true });
      drawInbox();
    });
  }

  // ---------- home ----------
  function toggle(key, label, title) {
    const input = h('input', { type: 'checkbox', ...(S.opts[key] ? { checked: true } : {}) });
    const lab = h('label', { class: 'toggle' + (S.opts[key] ? ' on' : ''), title }, input, h('span', { class: 'tick' }, icon('check')), label);
    input.addEventListener('change', () => { S.opts[key] = input.checked; lab.classList.toggle('on', input.checked); });
    return lab;
  }
  // ---------- voice ----------
  // Speak a task. The page records, stops itself when you go quiet, sends the audio to this Cully
  // server, which turns it into text with a speech model on the same machine, and the text becomes
  // the task. The browser only lets a page use the microphone on https or localhost.
  const VOICE_LS = 'cully-voice-autostart';
  const voiceAuto = () => { try { return localStorage.getItem(VOICE_LS) !== '0'; } catch (_) { return true; } };
  const voiceAutoSet = (on) => { try { localStorage.setItem(VOICE_LS, on ? '1' : '0'); } catch (_) {} };
  function voiceBlocked() {
    if (navigator.mediaDevices && navigator.mediaDevices.getUserMedia && window.MediaRecorder) return null;
    return ['The browser blocks the microphone on this address.', 'Browsers allow the microphone only on https or localhost. To use it on this http address, allow it once in your browser. Chrome or Edge: open chrome://flags/#unsafely-treat-insecure-origin-as-secure (edge://flags/... in Edge), add ' + location.origin + ', set it to Enabled and relaunch. Firefox: in about:config set media.devices.insecure.enabled and media.getusermedia.insecure.enabled to true. Or open Cully through an SSH tunnel (ssh -L 7777:127.0.0.1:7777 HOST, then http://127.0.0.1:7777).'];
  }
  // Reports progress on the button while the speech model is installed, then says when it is ready.
  async function voiceWait(btn, rest) {
    btn.disabled = true;
    for (;;) {
      let st;
      try { st = await api('/api/voice'); } catch (e) { toast(e.message, true); rest(); return; }
      if (st.ready) { toast('Voice is ready. Click Speak.'); rest(); return; }
      if (st.state === 'failed') { toast('Voice setup failed: ' + (st.detail || 'unknown error'), true); rest(); return; }
      btn.replaceChildren(icon('mic'), 'Setting up: ' + (st.detail || 'starting'));
      await new Promise((r) => setTimeout(r, 3000));
    }
  }
  // Records until you stop talking (or press the button again). Resolves with the audio, or null.
  function voiceRecord(btn, onLevel) {
    return new Promise(async (resolve, reject) => {
      let stream;
      try { stream = await navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: true, noiseSuppression: true } }); } catch (e) { reject(new Error(e && e.name === 'NotAllowedError' ? 'Microphone access was refused. Allow it for this page and try again.' : 'No microphone found.')); return; }
      const mime = ['audio/webm;codecs=opus', 'audio/webm', 'audio/ogg;codecs=opus', 'audio/mp4'].find((m) => MediaRecorder.isTypeSupported(m)) || '';
      const rec = new MediaRecorder(stream, mime ? { mimeType: mime } : undefined);
      const chunks = [];
      rec.ondataavailable = (e) => { if (e.data.size) chunks.push(e.data); };
      const ctx = new (window.AudioContext || window.webkitAudioContext)();
      const an = ctx.createAnalyser(); an.fftSize = 1024;
      ctx.createMediaStreamSource(stream).connect(an);
      const buf = new Uint8Array(an.fftSize);
      const t0 = Date.now(); let spoke = false; let quietSince = 0; let done = false;
      const finish = (keep) => {
        if (done) return; done = true; clearInterval(timer); document.removeEventListener('keydown', onKey);
        rec.onstop = () => { stream.getTracks().forEach((t) => t.stop()); ctx.close().catch(() => {}); resolve(keep && chunks.length ? new Blob(chunks, { type: rec.mimeType || mime || 'audio/webm' }) : null); };
        if (rec.state !== 'inactive') rec.stop(); else rec.onstop();
      };
      const onKey = (e) => { if (e.key === 'Escape') finish(false); };
      document.addEventListener('keydown', onKey);
      btn.onclick = () => finish(true);
      const timer = setInterval(() => {
        an.getByteTimeDomainData(buf);
        let sum = 0; for (const v of buf) { const x = (v - 128) / 128; sum += x * x; }
        const rms = Math.sqrt(sum / buf.length);
        onLevel(Math.min(1, rms * 6));
        const now = Date.now();
        if (rms > 0.02) { spoke = true; quietSince = 0; } else if (spoke && !quietSince) quietSince = now;
        if (spoke && quietSince && now - quietSince > 1600) finish(true);   // you stopped talking
        else if (!spoke && now - t0 > 10000) finish(false);                 // nothing was said
        else if (now - t0 > 120000) finish(true);                           // two minutes is the longest
      }, 100);
      rec.start(250);
    });
  }
  // The microphone button and its "start straight away" switch, for a text box and its form.
  function voiceControls(ta, form, opts) {
    const compact = !!(opts && opts.compact);
    const word = (t) => (compact ? null : t);
    const btn = h('button', { type: 'button', class: compact ? 'btn ghost round mic' : 'btn ghost mic', title: 'Speak the task. It stops when you go quiet.', 'aria-label': 'Speak the task' }, icon('mic'), word('Speak'));
    const meter = h('span', { class: 'micmeter' }); const fillBar = h('i'); meter.append(fillBar);
    const auto = h('input', Object.assign({ type: 'checkbox', 'aria-label': 'Start as soon as I stop talking' }, voiceAuto() ? { checked: true } : {}));
    auto.addEventListener('change', () => voiceAutoSet(auto.checked));
    const base = compact ? 'btn ghost round mic' : 'btn ghost mic';
    const rest = () => { btn.className = base; btn.replaceChildren(icon('mic'), word('Speak')); btn.disabled = false; btn.onclick = begin; fillBar.style.width = '0%'; };
    async function begin() {
      const blocked = voiceBlocked();
      if (blocked) { confirmDialog({ title: blocked[0], body: [blocked[1]], confirmLabel: 'OK', onConfirm: () => {} }); return; }
      btn.disabled = true;
      let st;
      try { st = await api('/api/voice'); } catch (e) { toast(e.message, true); rest(); return; }
      if (!st.ready) {
        btn.disabled = false;
        if (st.state === 'installing') { voiceWait(btn, rest); return; }
        confirmDialog({ title: 'Set up voice?', body: ['Cully will install a speech model in its own folder on this machine (about 1.5 GB: faster-whisper and distil-large-v3). Your audio is turned into text here and is never sent anywhere or kept.', 'It takes a few minutes the first time; you can keep using the page.'], confirmLabel: 'Set it up',
          onConfirm: async () => { await api('/api/voice/setup', { method: 'POST', body: JSON.stringify({}) }); voiceWait(btn, rest); } });
        return;
      }
      btn.disabled = false; btn.className = compact ? 'btn round mic rec' : 'btn mic rec'; btn.title = 'Listening. Click to stop.'; btn.replaceChildren(icon('stop'), word('Listening… click to stop'));
      let blob;
      try { blob = await voiceRecord(btn, (l) => { fillBar.style.width = Math.round(l * 100) + '%'; }); } catch (e) { toast(e.message, true); rest(); return; }
      if (!blob) { toast('Nothing was recorded'); rest(); return; }
      btn.disabled = true; btn.className = base; btn.title = 'Turning it into text…'; btn.replaceChildren(icon('refresh'), word('Turning it into text…'));
      try {
        const res = await fetch('/api/voice/transcribe', { method: 'POST', credentials: 'same-origin', headers: { 'X-Cully-UI': '1', 'Content-Type': blob.type || 'audio/webm' }, body: blob });
        const j = await res.json();
        if (!res.ok) throw new Error(j.error || 'Could not transcribe that');
        if (!j.text) { toast('I did not catch anything. Try again.'); rest(); return; }
        ta.value = (ta.value.trim() ? ta.value.trim() + ' ' : '') + j.text; ta.dispatchEvent(new Event('input'));
        rest();
        if (auto.checked) form.requestSubmit(); else { ta.focus(); toast('Check it, then start the task'); }
      } catch (e) { toast(e.message, true); rest(); }
    }
    btn.onclick = begin;
    const autoLabel = h('label', { class: 'optline', title: 'Start the task as soon as the words are turned into text' }, auto, h('span', {}, 'Start as soon as I stop talking'));
    const el = h('div', { class: 'row voice' }, btn, meter, compact ? null : autoLabel);
    el.autoLabel = autoLabel;
    return el;
  }

  // The first minute. Before you have started anything, show what Cully already found on this machine:
  // your own sessions across the three harnesses, how many are analysed, and where to look. Nothing to set up.
  async function renderWelcome(box) {
    let dismissed = false;
    try { dismissed = localStorage.getItem('cully-welcome-off') === '1'; } catch (_) { /* optional */ }
    if (dismissed) return;
    let lib = null;
    try { lib = await api('/api/sessions/library'); } catch (_) { return; }
    if (!lib || !lib.sessions.length) return;
    const by = {}; for (const x of lib.sessions) by[x.agent] = (by[x.agent] || 0) + 1;
    const t = lib.totals; const total = t.analyzed + t.pending;
    const bad = lib.sessions.filter((x) => x.source === 'ai' && x.health != null && x.health < 55).length;
    box.replaceChildren(h('section', { class: 'lib-card welcome' },
      h('div', { class: 'row' }, h('b', {}, 'Cully found your sessions'), h('span', { class: 'sp' }), h('button', { class: 'linkbtn', onclick: () => { try { localStorage.setItem('cully-welcome-off', '1'); } catch (_) { /* optional */ } box.replaceChildren(); } }, 'Hide')),
      h('div', { class: 'row' }, ...['claude', 'codex', 'cursor'].filter((a) => by[a]).map((a) => h('span', { class: 'chip harness h-' + a }, AGENT_LABEL[a] + ' ' + by[a]))),
      h('p', { class: 'faint small' }, lib.sessions.length + ' sessions on this machine, ' + dur(t.activeSec) + ' of work' + (total ? '. ' + t.analyzed + ' of ' + total + ' analysed' + (lib.analysis.on ? ', a few more every hour.' : '. Analysis is off.') : '.') + (bad ? ' ' + bad + ' of the analysed ones need a second look.' : '')),
      h('div', { class: 'row' }, h('a', { class: 'btn small', href: '#/sessions' }, icon('eye'), 'See them'), S.info && S.info.setupDone === false ? h('a', { class: 'btn small', href: '#/setup' }, icon('spark'), 'Set up Cully') : null, h('a', { class: 'btn ghost small', href: '#/dashboard/health' }, 'Session health'), h('a', { class: 'btn ghost small', href: '#/dashboard/library' }, 'Library'))));
  }

  // ---------- tasks that outlast the page ----------
  // A task is kept in the database with every session it has had, from any harness, so today's and this
  // week's work is still here after a restart and can be continued: the same conversation when it can
  // be resumed, otherwise a new run that starts from a handoff.
  const agentOr = (id, state) => S.agents.find((x) => x.id === id) || state;
  function continueTaskDialog(t) {
    const ta = h('textarea', { rows: '3', 'aria-label': 'What to do next', placeholder: 'What should it do next? Leave empty to finish what is left.' });
    const err = h('div', { class: 'err', hidden: true });
    const go = h('button', { class: 'btn', onclick: async () => {
      go.disabled = true; err.hidden = true;
      try { const v = await api('/api/tasks/' + t.id + '/continue', { method: 'POST', body: JSON.stringify({ prompt: ta.value }) }); closeMCP(); await refresh(); location.hash = '#/task/' + v.id; }
      catch (e) { err.textContent = e.message; err.hidden = false; go.disabled = false; }
    } }, icon('send'), 'Continue');
    const harnesses = [...new Set((t.sessions || []).map((x) => x.agent).filter(Boolean))];
    const modal = h('div', { class: 'modal', role: 'dialog', 'aria-modal': 'true', 'aria-label': 'Continue the task' },
      h('header', {}, h('h2', {}, 'Continue this task'), h('button', { class: 'btn ghost small sp', onclick: closeMCP }, 'Cancel')),
      h('div', { class: 'mbody' }, h('p', {}, h('b', {}, t.title)), (t.summary || t.last) ? h('p', { class: 'faint small' }, t.summary || t.last) : null,
        (t.sessions || []).length ? h('div', { class: 'row' }, h('span', { class: 'faint small' }, (t.sessions.length === 1 ? 'One session' : t.sessions.length + ' sessions') + ':'), ...t.sessions.map((x) => harnessChip(x.agent))) : null,
        h('p', { class: 'faint' }, 'If its last Claude session can be resumed, that conversation carries on. Otherwise a new run starts from what the task has done so far' + (harnesses.some((x) => x !== 'claude') ? ', including the ' + harnesses.filter((x) => x !== 'claude').map((x) => AGENT_NAMES[x] || x).join(' and ') + ' work' : '') + '.'),
        ta, err, h('div', { class: 'row' }, go)));
    const bg = h('div', { class: 'modal-bg', onclick: (e) => { if (e.target === bg) closeMCP(); } }, modal);
    $('modal-root').replaceChildren(bg);
    S.modal = { agent: 'continue' };
    document.removeEventListener('keydown', escClose);
    document.addEventListener('keydown', escClose);
    ta.focus();
  }
  async function renderTaskHistory(box) {
    const period = S.taskPeriod || 'today';
    let list = [];
    try { list = (await api('/api/tasks?range=' + period)).filter((t) => !t.closed); } catch (_) { return; }
    const tab = ([k, l]) => h('button', { type: 'button', class: 'toggle' + (period === k ? ' on' : ''), onclick: () => { S.taskPeriod = k; renderTaskHistory(box); } }, l);
    const card = (t) => {
      const st = taskPair(t);
      const ag = [...new Set((t.sessions || []).map((x) => x.agent).filter(Boolean))];
      const blurb = taskBlurb(t);
      const folder = t.repo ? t.repo.split('/').filter(Boolean).pop() : '';
      const go = t.live ? h('a', { class: 'btn small', href: '#/task/' + t.live }, 'Open') : h('button', { class: 'btn small', onclick: () => continueTaskDialog(t) }, icon('send'), 'Continue');
      return h('article', { class: 'tcard2 tone-' + st[1] },
        h('div', { class: 'tc-top' }, h('span', { class: 'tc-dot', 'aria-hidden': 'true' }), h('span', { class: 'tc-status' }, st[0]), h('span', { class: 'sp' }), (t.sessions || []).length > 1 ? h('span', { class: 'tc-n', title: 'Sessions in this task, across harnesses' }, t.sessions.length + ' sessions') : null, h('span', { class: 'tc-time' }, ago(t.updated))),
        h('h4', { class: 'tc-title', title: t.title }, t.title),
        h('p', { class: 'tc-desc' + (blurb.weak ? ' faint' : ''), title: blurb.full }, blurb.text),
        h('div', { class: 'tc-foot' },
          h('div', { class: 'tc-meta' }, folder ? h('span', { class: 'chip tc-folder', title: t.repo }, icon('folder'), folder) : null, ...ag.map((x) => harnessChip(x)),
            t.files ? h('span', { class: 'chip' }, t.files + (t.files === 1 ? ' file' : ' files')) : null),
          go));
    };
    box.replaceChildren(h('div', { class: 'row' }, h('h3', { class: 'section-h' }, 'Your tasks'), h('a', { class: 'linkbtn', href: '#/tasks' }, 'All tasks and sessions'), h('span', { class: 'sp' }), ...[['today', 'Today'], ['week', 'This week'], ['all', 'Earlier']].map(tab)),
      list.length ? h('div', { class: 'tgrid' }, ...list.slice(0, 24).map(card)) : h('p', { class: 'faint small' }, period === 'today' ? 'No tasks today yet.' : 'No tasks in this period.'));
  }

  function mountHome() {
    S.homeGate = !claudeInUse();
    if (S.homeGate) {
      const used = mcpUsed();
      const name = AGENT_NAMES[(used[0] || {}).agent] || 'your agent';
      $('stage').className = 'stage';
      $('stage').replaceChildren(h('div', { class: 'home' }, h('h1', {}, 'Your ' + name + ' sessions'),
        h('p', { class: 'sub' }, 'This page was opened from ' + name + ". Starting new agents from the page supports Claude only so far, so here you see your " + name + ' sessions, setup and history.'),
        h('div', { class: 'row' }, h('a', { class: 'btn', href: '#/sessions' }, 'See sessions'), h('a', { class: 'btn ghost', href: '#/dashboard' }, 'Open the dashboard'))));
      return;
    }
    const ta = h('textarea', { id: 'prompt', placeholder: 'Describe the task… (Ctrl+Enter to start)', 'aria-label': 'Task' });
    ta.value = S.draft;
    ta.addEventListener('input', () => { S.draft = ta.value; });
    const err = h('div', { class: 'err', hidden: true });
    const start = h('button', { class: 'btn', type: 'submit' }, 'Start task', icon('send'));
    const later = h('button', { class: 'btn ghost', type: 'button', title: 'Hold this task until a slot is free' }, 'Queue for later');
    const segBtn = (mode, label, title) => {
      const b = h('button', { type: 'button', class: S.opts.mode === mode ? 'on' : '', title: title || '' }, label);
      b.addEventListener('click', () => { S.opts.mode = mode; for (const x of seg.children) x.classList.toggle('on', x === b); syncFactory(); });
      return b;
    };
    const seg = h('div', { class: 'seg', role: 'group', 'aria-label': 'Mode' });
    seg.append(segBtn('edit', 'Edit', 'The agent edits files as it works'), segBtn('plan', 'Plan only', 'Read only: the agent plans and changes nothing'), segBtn('factory', 'Factory', 'You state the goal and how to tell it is done. The agent specifies, plans, builds and verifies; a reviewer that holds your checks decides.'));
    // Which harness runs the task: one small menu, shown only when more than one is installed.
    const hName = h('span', {}, 'Claude Code');
    const hList = h('div', { class: 'popcol' });
    const hNote = h('p', { class: 'faint small hnote', hidden: true });
    const hPill = h('details', { class: 'menu pillpop', hidden: true }, h('summary', { class: 'pillbtn', title: 'Which tool runs this task' }, icon('spark'), hName, icon('down')), h('div', { class: 'menu-pop pop-card' }, hList, hNote));
    api('/api/harness/available').then((list) => {
      const installed = (list || []).filter((x) => x.installed);
      const shown = installed.filter((x) => !x.exhausted);
      const gone = installed.filter((x) => x.exhausted);
      hNote.replaceChildren(...gone.map((x) => h('span', {}, exhaustedLine(x))));
      hNote.hidden = !gone.length;
      if (shown.length < 2 && !gone.length) return;
      if (!shown.some((x) => x.id === (S.opts.harness || 'claude') && x.available)) S.opts.harness = (shown.find((x) => x.available) || { id: 'claude' }).id;
      const draw = () => {
        const cur = shown.find((x) => x.id === (S.opts.harness || 'claude'));
        hName.textContent = cur ? cur.name : 'Claude Code';
        hList.replaceChildren(...shown.map((x) => h('button', { type: 'button', class: 'folderrow' + (x.id === (S.opts.harness || 'claude') ? ' on' : ''), disabled: !x.available, title: x.available ? (x.version || x.name) : x.name + ': ' + x.reason,
          onclick: () => { S.opts.harness = x.id; hPill.open = false; draw(); } }, icon(x.id === (S.opts.harness || 'claude') ? 'check' : 'spark'), h('span', { class: 'fr-main' }, h('b', {}, x.name), x.available ? null : h('small', { class: 'faint' }, x.reason)))));
      };
      draw();
      hPill.hidden = shown.length < 2;
    }).catch(() => {});
    const repo = null; // the task works in the folder Cully was opened from; there is no folder to pick here
    const doneIn = h('textarea', { rows: '2', 'aria-label': 'Done when', placeholder: 'For example: the login test passes\nNo new lint errors' });
    doneIn.value = S.opts.doneWhen || '';
    doneIn.addEventListener('input', () => { S.opts.doneWhen = doneIn.value; });
    const doneLabel = h('span', { class: 'opt-label' }, 'Done when');
    const loopBox = h('div', { class: 'loopbox', hidden: !S.opts.loop && S.opts.mode !== 'factory' },
      doneLabel, doneIn,
      h('label', { class: 'optline', title: 'The agent is told what is missing, never the checks themselves, so it cannot write to them' }, (() => { const c = h('input', Object.assign({ type: 'checkbox', 'aria-label': 'Keep the checks from the agent' }, S.opts.holdout ? { checked: true } : {})); c.addEventListener('change', () => { S.opts.holdout = c.checked; }); return c; })(), h('span', {}, 'Keep these checks hidden from the agent')),
      h('p', { class: 'faint small' }, 'After each turn Cully makes sure the agent ran tests, steers it if it repeats the same call, and checks its work against these lines. It stops after 3 rounds. Hidden checks work like a holdout set: an agent cannot satisfy a check it never saw, only the real goal.'));
    const helperBox = h('div', { class: 'helperbox' });
    S.opts.helpers = S.opts.helpers || [];
    api('/api/identities').then((list) => {
      const ok = list.filter((x) => x.approved);
      if (!ok.length) { helperBox.replaceChildren(h('p', { class: 'faint small' }, 'Approved agents can review the finished work. Create one on the Agents page.')); return; }
      helperBox.replaceChildren(h('span', { class: 'opt-label' }, 'Ask these agents to review the finished work'), h('div', { class: 'row' }, ...ok.slice(0, 8).map((x) => { const cb = h('input', Object.assign({ type: 'checkbox', id: 'hp-' + x.id, onchange: (e) => { S.opts.helpers = e.target.checked ? [...S.opts.helpers, x.id].slice(0, 2) : S.opts.helpers.filter((i) => i !== x.id); } }, S.opts.helpers.includes(x.id) ? { checked: true } : {})); return h('label', { class: 'toggle' }, cb, h('span', { class: 'tick' }, icon('check')), x.name); })), h('p', { class: 'faint small' }, 'Up to two. A reviewer is read only; its concerns go back to the task.'));
    }).catch(() => {});
    loopBox.append(helperBox);
    const limitIn = (key, label, ph, step) => { const i = h('input', { type: 'number', min: '0', step, placeholder: ph, 'aria-label': label, class: 'limitin', value: S.opts[key] || '' }); i.addEventListener('input', () => { S.opts[key] = i.value; }); return h('label', { class: 'limit', title: label + ' (empty means no limit)' }, h('span', { class: 'faint small' }, label), i); };
    const limits = h('div', { class: 'row limits' }, limitIn('maxCost', 'Stop at $', 'no limit', '0.5'), limitIn('maxTurns', 'or after turns', 'no limit', '1'));
    const loopToggle = toggle('loop', 'Keep going until checked', 'Cully verifies the work, steers the agent when it loops, and judges it against your checks');
    loopToggle.querySelector('input').addEventListener('change', () => { loopBox.hidden = !S.opts.loop && S.opts.mode !== 'factory'; });
    // Factory mode: acceptance scenarios are required and held back from the agent, and the keep-going loop is on.
    const factoryNote = h('p', { class: 'faint small factorynote', hidden: true }, 'Factory mode: describe the goal above, and below say how a person could tell it is done. The agent writes a spec, plans small tasks, builds and verifies. It never sees your scenarios; a reviewer that holds them decides when it is done.');
    const syncFactory = () => {
      const on = S.opts.mode === 'factory';
      factoryNote.hidden = !on;
      doneLabel.textContent = on ? 'Acceptance scenarios (one per line, things a person could observe)' : 'Done when';
      doneIn.placeholder = on ? 'With an empty cart, checkout shows the empty message\nThe export file opens in a spreadsheet with a header row' : 'For example: the login test passes\nNo new lint errors';
      if (on) {
        if (S.opts.loopBeforeFactory === undefined) S.opts.loopBeforeFactory = !!S.opts.loop;
        S.opts.loop = true; S.opts.holdout = true;
        const li = loopToggle.querySelector('input'); li.checked = true; loopToggle.classList.add('on');
        const hc = loopBox.querySelector('input[aria-label="Keep the checks from the agent"]'); if (hc) hc.checked = true;
        loopBox.hidden = false;
      } else if (S.opts.loopBeforeFactory !== undefined) {
        S.opts.loop = S.opts.loopBeforeFactory; delete S.opts.loopBeforeFactory;
        const li = loopToggle.querySelector('input'); li.checked = !!S.opts.loop; loopToggle.classList.toggle('on', !!S.opts.loop);
        loopBox.hidden = !S.opts.loop;
      }
    };
    const pop = (label, ic, ...kids) => h('details', { class: 'menu pillpop' }, h('summary', { class: 'pillbtn' }, icon(ic), label, icon('down')), h('div', { class: 'menu-pop pop-card' }, ...kids));
    if (!S.opts.repo && S.info && (S.info.repos || []).length) S.opts.repo = S.info.repos[0];
    start.className = 'btn round send'; start.setAttribute('aria-label', 'Start task'); start.title = 'Start the task'; start.replaceChildren(icon('send'));
    const voiceRef = { form: null }; const voiceEl = voiceControls(ta, { requestSubmit: () => voiceRef.form && voiceRef.form.requestSubmit() }, { compact: true });
    // Everything optional lives in one menu, in plain sections. The prompt, the tool and the mode stay in view.
    const sect = (title, ...kids) => h('div', { class: 'optsect' }, h('div', { class: 'opt-label' }, title), ...kids);
    const optionsPill = h('details', { class: 'menu pillpop optionspill' }, h('summary', { class: 'pillbtn', title: 'Context, checks, limits and voice' }, icon('sliders'), 'Options', icon('down')),
      h('div', { class: 'menu-pop pop-card options-pop' },
        sect('Start with', toggle('context', 'Context', "This project's latest journal and git state"), toggle('recall', 'Recall', 'What Cully memory knows is relevant (needs the Cully MCP signed in)'), toggle('scout', 'Scout', 'Current practice and pitfalls from the web')),
        sect('Checks', loopToggle), sect('Limits', limits), sect('Voice', voiceEl.autoLabel)));
    // Send is the main action; Queue for later sits under its arrow so it does not compete with it.
    const qItem = h('button', { type: 'button', class: 'folderrow', title: 'Hold this task until a slot is free' }, icon('clock'), h('span', { class: 'fr-main' }, h('b', {}, 'Queue for later'), h('small', { class: 'faint' }, 'Starts when a slot is free')));
    qItem.addEventListener('click', () => { sendMenu.open = false; later.click(); });
    const sendMenu = h('details', { class: 'menu sendmenu' }, h('summary', { class: 'btn ghost round sendmore', 'aria-label': 'More ways to start', title: 'More ways to start' }, icon('down')), h('div', { class: 'menu-pop pop-card send-pop' }, qItem));
    later.hidden = true;
    const form = h('form', { class: 'composer-card hero-composer' }, ta, factoryNote, loopBox,
      h('div', { class: 'cbar' }, hPill, seg, optionsPill, h('span', { class: 'sp' }), voiceEl.querySelector('.mic'), voiceEl.querySelector('.micmeter'), later, start, sendMenu), err);
    voiceRef.form = form;
    syncFactory();
    later.addEventListener('click', async () => {
      err.hidden = true;
      if (S.opts.mode === 'factory') { err.textContent = 'Factory tasks start now; they cannot be queued yet.'; err.hidden = false; return; }
      try {
        await api('/api/queue', { method: 'POST', body: JSON.stringify({ prompt: ta.value, repo: S.opts.repo || (repo ? repo.value : ''), mode: S.opts.mode, context: S.opts.context, recall: S.opts.recall, scout: S.opts.scout }) });
        S.draft = ''; ta.value = ''; toast('Queued. It starts when a slot is free.'); location.hash = '#/board';
      } catch (x) { err.textContent = x.message; err.hidden = false; }
    });
    ta.addEventListener('keydown', (e) => { if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) form.requestSubmit(); });
    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      err.hidden = true; start.disabled = true;
      const factory = S.opts.mode === 'factory';
      if (factory && !doneIn.value.trim()) { err.textContent = 'Factory mode needs at least one acceptance scenario: something a person could observe that shows the work is done.'; err.hidden = false; start.disabled = false; doneIn.focus(); return; }
      try {
        const body = { prompt: ta.value, factory, repo: S.opts.repo || (repo ? repo.value : ''), mode: factory ? 'edit' : S.opts.mode, harness: S.opts.harness && S.opts.harness !== 'claude' ? S.opts.harness : '', context: S.opts.context, recall: S.opts.recall, scout: S.opts.scout, maxCost: Number(S.opts.maxCost) || 0, maxTurns: Math.floor(Number(S.opts.maxTurns)) || 0, loop: !!S.opts.loop || factory, doneWhen: S.opts.loop || factory ? (S.opts.doneWhen || '') : '', holdout: !!((S.opts.loop || factory) && S.opts.holdout), helpers: S.opts.loop ? (S.opts.helpers || []) : [] };
        const view = await api('/api/agents', { method: 'POST', body: JSON.stringify(body) });
        S.draft = '';
        await refresh();
        location.hash = '#/task/' + view.id;
      } catch (x) { err.textContent = x.message; err.hidden = false; } finally { start.disabled = false; }
    });
    const fleet = h('div', { id: 'fleet' });
    $('stage').className = 'stage home-bg';
    $('stage').replaceChildren(h('div', { class: 'home' },
      h('div', { class: 'herohead' }, h('span', { class: 'heromark', 'aria-hidden': 'true' }, icon('spark')), h('h1', {}, 'What should we do?')),
      h('p', { class: 'sub' }, 'Cully gathers context and starts an agent in its own worktree.'),
      h('div', { id: 'mcpbanner' }), form, fleet));
    renderFleet();
    drawMCPBanner();
    const welcomeBox = h('div', { class: 'welcomebox' }); const tasksBox = h('div', { class: 'taskhist' }); $('stage').firstChild.append(...[setupBanner(), welcomeBox, tasksBox].filter(Boolean)); renderWelcome(welcomeBox); renderTaskHistory(tasksBox);
    ta.focus();
  }
  function agentCard(a) {
    return h('a', { class: 'card' + (a.state === 'waiting' ? ' attn' : ''), href: '#/task/' + a.id },
      h('div', { class: 'r1' }, dot(a), h('span', { class: 't' }, a.title)),
      h('div', { class: 'last' }, a.last || (a.state === 'preparing' ? 'Preparing…' : '')),
      h('div', { class: 'row' }, h('span', { class: 'pill ' + phaseCls(a) }, stateLabel(a)),
        a.token ? h('span', { class: 'chip' }, 'token ' + a.token) : null, h('span', { class: 'chip' }, ago(a.updated || a.created))));
  }
  function renderFleet() {
    const box = $('fleet');
    if (!box) return;
    const live = S.agents.filter((a) => groupOf(a) !== 'finished');
    if (live.length) {
      box.replaceChildren(h('div', { class: 'section-h' }, 'Running now'), h('div', { class: 'cards' }, ...live.map(agentCard)));
    } else {
      box.replaceChildren(h('div', { class: 'section-h' }, 'What happens when you start'), h('ol', { class: 'how compact' },
        h('li', {}, h('b', {}, 'Context'), 'Journal and git state go to the agent first.'),
        h('li', {}, h('b', {}, 'Recall and scout'), 'Optional memory and web notes.'),
        h('li', {}, h('b', {}, 'Worktree'), 'Its own branch; your checkout stays untouched.'),
        h('li', {}, h('b', {}, 'Conversation'), 'Follow up while it works, then review the diff.')));
    }
  }

  // ---------- agent view ----------
  function mountAgent(id) {
    const a = S.agents.find((x) => x.id === id);
    if (!a) { $('stage').replaceChildren(h('div', { class: 'empty' }, S.agents.length ? 'That agent no longer exists.' : 'Loading…')); return; }
    S.tab = ['chat', 'changes', 'handoff', 'calls', 'graph'].includes(S.tab) ? S.tab : 'chat';
    const ahead = h('div', { class: 'ahead', id: 'ahead' });
    const sub = h('div', { class: 'sub2', id: 'sub2' });
    const ctxnote = h('div', { id: 'ctxnote' });
    const stepper = h('div', { class: 'stepper', id: 'stepper' });
    const tabs = h('div', { class: 'tabs narrow', id: 'tabs' });
    const body = h('div', { class: 'feed', id: 'feed' });
    const jump = h('button', { class: 'btn small jump', hidden: true, onclick: () => { S.feed.unseen = 0; body.scrollTo({ top: body.scrollHeight, behavior: 'smooth' }); } }, icon('down'), 'Latest');
    const quick = h('div', { class: 'quick', 'aria-label': 'Suggested replies' });
    const ta = h('textarea', { rows: '1', placeholder: 'Message this task…', 'aria-label': 'Message' });
    ta.addEventListener('input', () => { ta.style.height = 'auto'; ta.style.height = Math.min(ta.scrollHeight, 160) + 'px'; ta.style.overflowY = ta.scrollHeight > 160 ? 'auto' : 'hidden'; });
    const taskBusy = () => { const cur = S.agents.find((x) => x.id === a.id); return !!cur && (cur.state === 'working' || cur.state === 'preparing'); };
    const doSend = (mode) => { const t = ta.value; if (!t.trim()) return; ta.value = ''; ta.style.height = 'auto'; sendMessage(a.id, t, mode); };
    ta.addEventListener('keydown', (e) => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); doSend(taskBusy() ? (e.altKey ? 'followup' : 'steer') : ''); } });
    const sendBtn = h('button', { class: 'btn round', title: 'Send', 'aria-label': 'Send', onclick: () => doSend(taskBusy() ? 'steer' : '') }, icon('send'));
    const afterBtn = h('button', { class: 'btn ghost small afterbtn', hidden: true, title: 'Hold this message and send it when the current turn ends (Alt+Enter)', onclick: () => doSend('followup') }, 'After this turn');
    const hint = h('div', { class: 'composer-hint' }, 'Enter sends, Shift+Enter adds a line');
    const msgq = h('div', { class: 'msgq', id: 'msgq', hidden: true });
    const composer = h('div', { id: 'composer' }, h('div', { class: 'composer' }, h('div', { class: 'box' }, ta), afterBtn, sendBtn), hint, msgq);
    const statusline = h('div', { id: 'statusline', class: 'statusline', role: 'status' });
    const side = h('div', { class: 'col sidecol' }, h('div', { class: 'tabs side', id: 'tabs-side' }), h('div', { class: 'sidehead', id: 'sidehead' }), h('div', { class: 'sidebody', id: 'sidebody' }));
    const activity = h('div', { id: 'activity', class: 'activity off', role: 'status', 'aria-live': 'polite' });
    const permBox = h('div', { id: 'permbox' });
    const grid = h('div', { class: 'agrid', id: 'agrid', 'data-show': S.tab }, h('div', { class: 'col chatcol' }, body, activity, permBox, quick, composer, statusline), side);
    $('stage').className = 'stage agent-stage';
    $('stage').replaceChildren(ahead, sub, ctxnote, stepper, tabs, grid);
    installRailResizer(); installSideResizer(grid, side); applyPanelWidths();
    body.after(jump);
    body.addEventListener('scroll', () => { const atEnd = body.scrollTop + body.clientHeight >= body.scrollHeight - 60; if (atEnd && S.feed) S.feed.unseen = 0; paintJump(); });
    body.addEventListener('keydown', (e) => { if (e.key === 'End') { e.preventDefault(); body.scrollTo({ top: body.scrollHeight, behavior: 'smooth' }); } else if (e.key === 'Home') { e.preventDefault(); body.scrollTo({ top: 0, behavior: 'smooth' }); } });
    body.tabIndex = 0;
    S.feed = { body, jump, group: null, tools: 0, typing: null, quick, composer, ta, sendBtn, afterBtn, hint, msgq, loaded: false, calls: [], callLast: null, permKeys: [] };
    installPermKeys();
    setTimeout(() => { if (S.feed) S.feed.loaded = true; }, 900);
    drawAgentChrome(a);
    openFeed(a.id);
    loadSide(a, true);
  }
  function setTab(t) {
    S.tab = t;
    if (t !== 'chat') { S.sideTab = t; S.sideChosen = S.sideChosen || {}; S.sideChosen[S.route.id] = true; }
    const a = S.agents.find((x) => x.id === S.route.id);
    $('agrid').dataset.show = t;
    if (a) { drawAgentChrome(a); loadSide(a, true); }
  }
  // ---- the task panel: everything about the running task in one place, down the left side ----
  const gauge = (pct, size, label) => {
    const NS = 'http://www.w3.org/2000/svg'; const r = 15.9;
    const svg = document.createElementNS(NS, 'svg');
    for (const [k, v] of Object.entries({ viewBox: '0 0 36 36', width: size || 64, height: size || 64, class: 'gauge ' + (pct >= 80 ? 'hot' : pct >= 60 ? 'mid' : 'ok'), role: 'img', 'aria-label': (label || 'Used') + ' ' + pct + ' percent' })) svg.setAttribute(k, v);
    const mk = (cls, dash) => { const c = document.createElementNS(NS, 'circle'); for (const [k, v] of Object.entries({ cx: 18, cy: 18, r, fill: 'none', 'stroke-width': 3.4, class: cls })) c.setAttribute(k, v); if (dash != null) { c.setAttribute('stroke-dasharray', dash + ' 100'); c.setAttribute('stroke-linecap', 'round'); c.setAttribute('transform', 'rotate(-90 18 18)'); } return c; };
    const t = document.createElementNS(NS, 'text'); for (const [k, v] of Object.entries({ x: 18, y: 21, 'text-anchor': 'middle', class: 'gnum' })) t.setAttribute(k, v); t.textContent = pct + '%';
    svg.append(mk('gtrack'), mk('garc', Math.min(100, pct)), t);
    return svg;
  };
  const untilReset = (unix) => { const m = Math.max(0, Math.round((unix * 1000 - Date.now()) / 60000)); return m >= 1440 ? 'resets in ' + Math.floor(m / 1440) + ' d ' + Math.floor((m % 1440) / 60) + ' h' : m >= 60 ? 'resets in ' + Math.floor(m / 60) + ' h ' + (m % 60) + ' min' : 'resets in ' + m + ' min'; };
  // One usage window. Claude Code reports a window's use only some of the time, so a missing figure is
  // shown as "not reported", never as 0%, and a window whose reset time has passed says it reset.
  const limitBar = (label, w) => {
    const reset = w.resetsAt && w.resetsAt * 1000 < Date.now();
    if (!w.known || reset) return h('div', { class: 'lim' }, h('div', { class: 'row' }, h('span', {}, label), h('b', { class: 'faint' }, reset ? 'Window reset' : 'Not reported')), h('small', { class: 'faint' }, reset ? 'It started again from zero. Check now for the new figure.' : 'Claude Code tells a task this only some of the time. Check now asks for it.'));
    return h('div', { class: 'lim' }, h('div', { class: 'row' }, h('span', {}, label), h('b', { class: w.used >= 80 ? 'bad-t' : '' }, w.used + '% used')), h('span', { class: 'fbar ' + (w.used >= 80 ? 'hot' : w.used >= 60 ? 'mid' : '') }, fill(w.used)), w.resetsAt ? h('small', { class: 'faint' }, untilReset(w.resetsAt)) : null);
  };
  // Asks each account for its limits (a few tokens on the smallest model) and redraws.
  const limitsCheckBtn = (seen) => { const b = h('button', { class: 'btn ghost small', title: 'Ask for the current usage limits. Costs a few tokens.' }, icon('refresh'), 'Check now'); b.addEventListener('click', async () => { b.disabled = true; b.textContent = 'Checking…'; try { await api('/api/limits/refresh', { method: 'POST' }); S.panelAt = 0; await refresh(); toast('Usage limits updated'); } catch (e) { toast(e.message, true); } b.disabled = false; }); return h('div', { class: 'row' }, b, seen ? h('small', { class: 'faint' }, 'last heard ' + ago(new Date(seen * 1000).toISOString())) : null); };
  async function panelData() {
    if (S.panelData && Date.now() - S.panelAt < 20000) return S.panelData;
    const [day, week, limits] = await Promise.all([api('/api/usage?days=1').catch(() => null), api('/api/usage?days=7').catch(() => null), api('/api/limits').catch(() => null)]);
    S.panelData = { day, week, limits }; S.panelAt = Date.now();
    return S.panelData;
  }
  async function drawTaskPanel(a) {
    const box = $('taskpanel');
    if (!box) return;
    if (!a) { box.replaceChildren(); return; }
    const d = await panelData();
    if (!a || S.route.name !== 'agent' || S.route.id !== a.id) return;
    const c = a.context; const u = a.usage; const run = a.run;
    // Sections fold. Now, Context window and Usage limits start open; the rest start closed and stay as you leave them.
    const lsGet = () => { try { return JSON.parse(localStorage.getItem('cully-tp-open') || '{}'); } catch (_) { return {}; } };
    const lsSet = (o) => { try { localStorage.setItem('cully-tp-open', JSON.stringify(o)); } catch (_) { /* optional */ } };
    // Each section shows a one-line hint on its header and opens for the full detail. All start closed; the page
    // remembers what you open.
    const sec = (title, hint, ...kids) => {
      const saved = lsGet(); const open = title in saved ? saved[title] : false;
      const d = h('details', { class: 'tp-sec', ...(open ? { open: true } : {}) }, h('summary', { class: 'tp-h' }, h('span', {}, title), hint ? h('span', { class: 'tp-hint' }, hint) : null), ...kids);
      d.addEventListener('toggle', () => { const o = lsGet(); o[title] = d.open; lsSet(o); });
      return d;
    };
    const kv = (k, v) => h('div', { class: 'kv' }, h('span', { class: 'faint' }, k), h('b', {}, v));
    const elapsedS = Math.round((Date.now() - new Date(a.created).getTime()) / 1000);
    const lim = a.limits;
    // How long since the task last did anything. This, not the task's age, says whether a tool call may be stuck.
    const quietS = a.updated ? Math.max(0, Math.round((Date.now() - new Date(a.updated).getTime()) / 1000)) : 0;
    const quiet = quietS >= 60 ? dur(quietS) : '';
    const tool = (a.phase === 'tool' && a.phaseLabel && a.phaseLabel.includes(': ')) ? a.phaseLabel.split(': ').pop() : '';
    const nowHint = ['done', 'failed', 'stopped', 'limit', 'closed'].includes(a.phase) ? stateLabel(a)
      : (tool ? 'Running ' + tool : stateLabel(a)) + (quietS >= 300 ? ', no activity for ' + quiet : '');
    const parts = [
      sec('Now', nowHint, h('div', { class: 'row' }, h('span', { class: 'pill ' + phaseCls(a) }, dot(a), stateLabel(a)), h('span', { class: 'faint small' }, quiet ? 'Last activity ' + quiet + ' ago' : 'Active just now')),
        h('p', { class: 'faint small' }, 'Task started ' + dur(elapsedS) + ' ago'),
        a.last ? h('pre', { class: 'tp-lastfull' }, a.last) : h('p', { class: 'tp-last' }, a.state === 'preparing' ? 'Gathering context…' : 'No activity yet'),
        h('div', { class: 'row' }, h('button', { class: 'btn small', onclick: () => setTab('graph') }, icon('eye'), 'Open graph'), h('button', { class: 'btn ghost small', onclick: () => setTab('calls') }, 'Every tool call')),
        a.harness ? h('div', { class: 'chip ' + (a.harness.status === 'done' ? 'ok' : a.harness.status === 'stopped' ? 'warn' : '') }, icon('check'), 'Checking: round ' + a.harness.round + ' of ' + a.harness.max + ', ' + a.harness.status) : null),
      sec('Context window', c ? c.percent + '% used' : 'not yet', c ? h('div', { class: 'tp-gauge' }, gauge(c.percent, 68, 'Context'), h('div', {}, kv('Used', fmtTokens(c.used)), kv('Left', fmtTokens(c.left)), kv('Window', fmtTokens(c.window)))) : h('p', { class: 'faint small' }, 'Shows after its first reply.'),
        c && c.model ? h('div', { class: 'faint small' }, c.model) : null,
        c ? h('div', { class: 'faint small' }, c.compactions ? 'Compacted ' + c.compactions + (c.compactions === 1 ? ' time' : ' times') + ' by the harness.' : 'The harness compacts the history by itself before this fills.') : null,
        c && c.compactions >= 3 ? h('button', { class: 'btn small', onclick: () => startFromHandoff(a) }, icon('plus'), 'Start clean from a handoff') : null),
      sec('Usage limits', lim ? [lim.fiveHour && lim.fiveHour.known ? '5h ' + lim.fiveHour.used + '%' : null, lim.sevenDay && lim.sevenDay.known ? '7d ' + lim.sevenDay.used + '%' : null].filter(Boolean).join(', ') || 'not reported' : 'not reported', lim ? [limitBar('Last 5 hours', lim.fiveHour), limitBar('Last 7 days', lim.sevenDay), limitsCheckBtn(lim.seen), h('small', { class: 'faint' }, 'Account: ' + (a.token ? 'token ' + a.token : 'ambient login') + (lim.status && lim.status !== 'allowed' ? '. ' + lim.status.replace(/_/g, ' ') : ''))] : h('p', { class: 'faint small' }, 'The harness reports these after its first reply.')),
      sec('Tokens', u ? fmtTokens(u.tokens) : 'counting', u ? [kv('This task', fmtTokens(u.tokens)), kv('From cache', u.cachedShare + '%'), kv('Saved by cache', '~' + fmtTokens(u.saved))] : h('p', { class: 'faint small' }, 'Counted after each turn.'),
        d.day ? kv('Tasks today', fmtTokens(d.day.tokens)) : null, d.week ? kv('Tasks this week', fmtTokens(d.week.tokens)) : null, a.cost ? kv('List price so far', '$' + a.cost.toFixed(a.cost < 1 ? 3 : 2)) : null),
      sec('Harness', (AGENT_LABEL[a.agent || 'claude'] || 'Claude') + (run ? (run.permission === 'plan' ? ', read only' : ', can edit') : ''), h('div', { class: 'row' }, harnessChip(a.agent || 'claude'), run ? h('button', { class: 'linkbtn', onclick: () => openRunInfo(a) }, 'How it is set up') : null),
        run ? h('div', { class: 'faint small' }, (run.permission === 'plan' ? 'Read only' : 'Can edit') + ', ' + (run.mcpMode === 'chosen' ? run.mcpChosen.length + ' servers' : 'all servers') + ', ' + (run.isolation === 'worktree' ? 'own worktree' : 'in place')) : null),
      sec('Work so far', (a.files || []).length + (((a.files || []).length === 1) ? ' file' : ' files') + ', ' + (a.events || 0) + ' events', kv('Files changed', String((a.files || []).length)), kv('Tool calls', String(a.events || 0) + ' events'), a.run && a.run.loop ? kv('Tool budget', String(a.run.toolBudget)) : null),
    ];
    // Other harnesses: Codex limits when its plan reports them, and what Cursor cannot record.
    const other = [];
    if (d.limits && d.limits.codex && d.limits.codex.limits) other.push(h('div', {}, h('span', { class: 'chip harness h-codex' }, 'Codex'), limitBar('Primary window', d.limits.codex.limits.fiveHour), limitBar('Secondary window', d.limits.codex.limits.sevenDay)));
    other.push(h('p', { class: 'faint small' }, 'Cursor does not record usage limits or tokens.'));
    parts.push(sec('Other harnesses', '', ...other));
    box.replaceChildren(...parts.flat());
  }

  // When the context window is nearly full the task will start to forget or stop: offer a fresh task with a handoff.
  function drawContextNote(a) {
    const box = $('ctxnote');
    if (!box) return;
    const c = a.context;
    // The harness compacts its own history before the window fills, so only repeated compaction is worth a note.
    if (!c || c.compactions < 3 || ['done', 'failed', 'stopped'].includes(a.state)) { box.replaceChildren(); return; }
    box.replaceChildren(h('div', { class: 'ctxnote' }, icon('alert'), h('div', {}, h('b', {}, 'This task has been compacted ' + c.compactions + ' times'), h('div', { class: 'faint small' }, 'Each time the harness summarises the history to make room, detail from early on is lost. A new task can start clean from a handoff of what this one did.')),
      h('button', { class: 'btn small', onclick: () => startFromHandoff(a) }, icon('plus'), 'Start a new task with a handoff')));
  }
  async function startFromHandoff(a) {
    try {
      const text = await api('/api/agents/' + a.id + '/handoff');
      const v = await api('/api/agents', { method: 'POST', body: JSON.stringify({ prompt: text + '\n\nContinue from here: look at the current state of the work, finish what is left, and run the checks.', repo: a.repo, mode: 'edit', context: true, title: 'Continue: ' + a.title }) });
      await refresh(); location.hash = '#/task/' + v.id;
    } catch (e) { toast(e.message, true); }
  }
  // A plain account of what is running a task: the harness, its permissions and tools, the connected servers it loads, where it works, and what Cully adds.
  function openRunInfo(a) {
    const r = a.run;
    const item = (k, v) => h('div', { class: 'ri' }, h('span', { class: 'opt-label' }, k), v);
    const chips = (list, cls) => h('div', { class: 'row' }, ...(list.length ? list.map((x) => h('span', { class: 'chip ' + (cls || '') }, x)) : [h('span', { class: 'faint small' }, 'None')]));
    const servers = (a.mcp || []).length ? h('div', { class: 'row' }, ...a.mcp.map((m) => h('span', { class: 'chip ' + (needsAuthStatus(m.status) || m.status === 'failed' ? 'warn' : '') }, m.name + ': ' + m.status))) : null;
    const modal = h('div', { class: 'modal wide', role: 'dialog', 'aria-modal': 'true', 'aria-label': 'How this task runs' },
      h('header', {}, h('h2', {}, 'How this task runs'), h('button', { class: 'btn ghost small sp', onclick: closeMCP }, 'Close')),
      h('div', { class: 'mbody riwrap' },
        item('Harness', h('p', {}, h('b', {}, r.harness), ' (' + r.interface + '). Cully starts it and talks to it; it runs the model and carries out the tools.')),
        item('Model', h('p', {}, a.context && a.context.model ? a.context.model : r.model)),
        item('Permission mode', h('p', {}, r.permission === 'plan' ? 'Plan only: it can read and plan but not change files.' : 'It may edit files without asking; it cannot ask a person for anything else, so what is not allowed below is refused.')),
        item('Tools it may use', chips(r.groups)),
        (r.asks || []).length ? item('Asks you first', chips(r.asks, 'warn')) : null,
        item('Always blocked', chips(r.blocked, 'warn')),
        item('Connected servers', h('div', {}, h('p', {}, r.mcpMode === 'chosen' ? 'Limited to the servers you chose: ' + r.mcpChosen.join(', ') + '. Fewer servers means fewer tokens every turn.' : 'All of the servers connected to your harness. Limit them on the Harness permissions page to save tokens.'), servers)),
        item('Where it works', h('p', {}, r.isolation === 'worktree' ? 'In its own git worktree on branch ' + a.branch + ', so your checkout is untouched.' : 'Directly in the folder, not in a separate branch.' + (r.resume ? ' ' + r.resume + '.' : ''))),
        r.identity ? item('Agent', h('p', {}, 'Running as the agent ' + r.identity + ': its role, instructions and notes were added to the system prompt.')) : null,
        item('What Cully put in front of its first turn', chips(r.brief)),
        item('What Cully does around it', r.loop ? h('ul', { class: 'how' }, ...r.loopChecks.map((c) => h('li', {}, c))) : h('p', {}, 'Nothing. It is a plain run: Cully relays your messages and shows what it does.')),
        h('p', { class: 'faint small' }, 'Only tasks are tied to a harness: the one this page was opened through. Sessions, the library, the advisor and the scout cover Claude, Codex and Cursor alike.')));
    const bg = h('div', { class: 'modal-bg', onclick: (e) => { if (e.target === bg) closeMCP(); } }, modal);
    $('modal-root').replaceChildren(bg);
    S.modal = { agent: 'runinfo' };
    document.removeEventListener('keydown', escClose);
    document.addEventListener('keydown', escClose);
  }
  // A task asked to use something it is not allowed to yet. Nothing goes ahead until you answer; an
  // unanswered question is refused after ten minutes. "For this task" covers only that exact call.
  function drawPermissions(a) {
    const box = $('permbox');
    if (!box) return;
    const list = a.permissions || [];
    const sig = list.map((p) => p.id).join(',');
    if (box.dataset.sig === sig) return;
    box.dataset.sig = sig;
    const answer = async (p, decision) => { try { await api('/api/agents/' + a.id + '/permissions/' + p.id, { method: 'POST', body: JSON.stringify({ decision, hash: p.hash }) }); await refresh(); } catch (e) { toast(e.message, true); } };
    // a question counts for a shortcut key only after it has been on screen for a moment, so a key
    // pressed for something else, or a dialog that appears under your fingers, answers nothing
    if (S.feed) { const seen = S.feed.permShown = S.feed.permShown || {}; for (const p of list) if (!seen[p.id]) seen[p.id] = Date.now(); S.feed.permKeys = list.map((p) => ({ p, answer })); }
    const kb = (k) => h('kbd', {}, k);
    // A dialog in the terminal style. What you allow is exactly the input shown in full, and the answer
    // quotes the hash of that input, so a call that changed since you looked is refused.
    box.replaceChildren(...list.map((p, i) => h('section', { class: 'permcard tui-dialog risk-b-' + p.risk, role: 'alertdialog', 'aria-label': 'The task asks to use ' + p.tool },
      h('div', { class: 'dlg-title' }, icon('lock'), h('b', {}, 'Allow ' + toolLabel(p.tool) + '?'), h('span', { class: 'chip risk-' + p.risk, title: 'Worked out from the call by fixed rules, not by a model' }, ({ low: 'Low risk', medium: 'Medium risk', high: 'High risk' })[p.risk] || p.risk),
        h('span', { class: 'sp' }), h('span', { class: 'faint small' }, 'waiting since ' + clockTime(p.at))),
      h('p', { class: 'dlg-why' }, 'The task asks to use ' + p.tool + (p.riskWhy ? ', which ' + p.riskWhy : '') + '.'),
      p.summary ? h('pre', { class: 'permcmd' }, p.summary) : null,
      h('p', { class: 'faint small' }, 'Everything it sent, in full. What you allow is exactly this' + (typeof p.hash === 'string' && p.hash ? ' (input hash ' + p.hash.slice(0, 8) + '):' : ':')),
      h('pre', { class: 'permcmd' }, p.input),
      h('div', { class: 'dlg-keys' },
        h('button', { class: 'btn small', onclick: () => answer(p, 'allow') }, icon('check'), 'Allow once', i === 0 ? kb('y') : null),
        p.risk === 'high' ? null : h('button', { class: 'btn ghost small', title: 'Allow this exact call again without asking, for the rest of this task', onclick: () => answer(p, 'allow_always') }, 'Allow for this task'),
        h('button', { class: 'btn ghost small', onclick: () => answer(p, 'deny') }, 'Deny', i === 0 ? kb('n') : null)))));
  }
  // Single-key answers for the first question waiting, only when you are not typing in a field.
  function installPermKeys() {
    if (S.permKeysOn) return; S.permKeysOn = true;
    document.addEventListener('keydown', (e) => {
      const f = S.feed; if (!f || !S.route || S.route.name !== 'agent' || e.ctrlKey || e.metaKey || e.altKey || e.shiftKey) return;
      const t = e.target; if (t && (/^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName) || t.isContentEditable)) return;
      if (S.modal) return;
      if (e.repeat || S.permBusy) return;
      const first = f.permKeys && f.permKeys[0]; if (!first) return;
      const d = { y: 'allow', n: 'deny' }[e.key]; // "for this task" is never a single key
      if (!d) return;
      e.preventDefault();
      if (Date.now() - ((f.permShown || {})[first.p.id] || Date.now()) < 800) return;
      S.permBusy = true;
      Promise.resolve(first.answer(first.p, d)).finally(() => { S.permBusy = false; });
    });
  }
  document.addEventListener('click', (e) => { for (const d of document.querySelectorAll('details.menu[open]')) if (!d.contains(e.target)) d.open = false; });
  // Switching a task to another harness, and the quiet note when a harness is out of tokens.
  const clockOf = (unix) => new Date(unix * 1000).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
  const exhaustedLine = (x) => (x.id === 'claude' ? x.name + ' is out of tokens until ' + clockOf(x.until) : x.name + ' hit its limit; back about ' + clockOf(x.until));
  function switchHarnessDialog(a) {
    const err = h('div', { class: 'err', hidden: true });
    const body = h('div', { class: 'mbody' }, h('p', { class: 'faint small' }, 'Loading the harnesses'));
    const modal = h('div', { class: 'modal', role: 'dialog', 'aria-modal': 'true', 'aria-label': 'Switch harness' },
      h('header', {}, h('h2', {}, 'Switch harness'), h('button', { class: 'btn ghost small sp', onclick: closeMCP }, 'Cancel')), body);
    const bg = h('div', { class: 'modal-bg', onclick: (e) => { if (e.target === bg) closeMCP(); } }, modal);
    $('modal-root').replaceChildren(bg);
    S.modal = { agent: 'switch' };
    document.removeEventListener('keydown', escClose);
    document.addEventListener('keydown', escClose);
    const confirmStep = (x) => {
      const note = h('textarea', { rows: '2', 'aria-label': 'What to do next', placeholder: 'Anything it should do next? Optional.' });
      const go = h('button', { class: 'btn', onclick: async () => {
        go.disabled = true; err.hidden = true; go.replaceChildren('Switching, this can take up to a minute');
        try { const v = await api('/api/tasks/' + a.id + '/switch', { method: 'POST', body: JSON.stringify({ harness: x.id, note: note.value }) }); closeMCP(); await refresh(); location.hash = '#/task/' + v.id; }
        catch (e) { err.textContent = e.message; err.hidden = false; go.disabled = false; go.replaceChildren(icon('send'), 'Switch to ' + x.name); }
      } }, icon('send'), 'Switch to ' + x.name);
      body.replaceChildren(h('p', {}, 'Cully will wait for the current step to finish (up to one minute), save where the work stands, and continue this task in ' + x.name + ' in the same worktree. Everything stays in this task.'),
        note, err, h('div', { class: 'row' }, go, h('button', { class: 'btn ghost', onclick: () => listStep(S.switchList) }, 'Back')));
    };
    const listStep = (list) => {
      const cur = a.agent || 'claude';
      const rows = (list || []).filter((x) => x.installed && (x.id === cur || (x.available && !x.exhausted)));
      const gone = (list || []).filter((x) => x.installed && x.exhausted && x.id !== cur);
      body.replaceChildren(h('p', { class: 'faint small' }, 'Choose the harness that carries this task from here.'),
        h('div', { class: 'switchlist' }, ...rows.map((x) => h('button', { type: 'button', class: 'switchrow' + (x.id === cur ? ' on' : ''), disabled: x.id === cur, onclick: () => confirmStep(x) }, h('b', {}, x.name), h('span', { class: 'faint small' }, x.id === cur ? 'Running now' : (x.version || ''))))),
        rows.filter((x) => x.id !== cur).length ? null : h('p', { class: 'faint small' }, 'No other harness is available right now.'),
        gone.length ? h('p', { class: 'faint small hnote' }, ...gone.map((x) => h('span', {}, exhaustedLine(x)))) : null);
    };
    api('/api/harness/available').then((list) => { S.switchList = list; listStep(list); }).catch((e) => body.replaceChildren(h('div', { class: 'err' }, e.message)));
  }
  function drawAgentChrome(a) {
    drawPermissions(a);
    updateActivity();
    if (!$('ahead')) return;
    const finished = a.state === 'done' || a.state === 'failed' || a.state === 'stopped';
    const stop = h('button', { class: 'btn danger small', onclick: (e) => {
      const b = e.currentTarget;
      if (b.dataset.armed) { api('/api/agents/' + a.id + '/stop', { method: 'POST' }).then(refresh).catch((x) => toast(x.message, true)); return; }
      b.dataset.armed = '1'; b.replaceChildren(icon('stop'), 'Click again to stop'); setTimeout(() => { b.dataset.armed = ''; b.replaceChildren(icon('stop'), 'Stop'); }, 3000);
    } }, icon('stop'), 'Stop');
    // The side column (changes and summary) shows only when there is something to show, or when you
    // open it. With nothing in it, the chat takes the whole window.
    const nChanges = a.files ? a.files.length : 0;
    S.sidePref = S.sidePref || {};
    // When the task waits on you, open the right column on the calls once per question, so you see which
    // call is waiting. After that your own choice of tab and of open or closed stands.
    S.askOpened = S.askOpened || new Set();
    const fresh = (a.permissions || []).filter((p) => !S.askOpened.has(p.id));
    if (fresh.length) { fresh.forEach((p) => S.askOpened.add(p.id)); S.sidePref[a.id] = 'open'; S.sideTab = 'calls'; (S.sideChosen = S.sideChosen || {})[a.id] = true; if (S.tab !== 'chat') S.tab = 'calls'; S.diffKey = ''; }
    // The graph is the first thing in the right column while there are no changes to show, so it is open by default.
    S.sideDefault = S.sideDefault || {};
    if (!S.sideDefault[a.id]) S.sideDefault[a.id] = nChanges > 0 ? 'changes' : 'graph';
    const showSide = S.sidePref[a.id] ? S.sidePref[a.id] === 'open' : true;
    const grid = $('agrid');
    if (grid) grid.classList.toggle('no-side', !showSide);
    const sideBtn = h('button', { class: 'btn ghost small sidetoggle', title: showSide ? 'Hide the changes and summary' : 'Show the changes and summary', 'aria-pressed': String(showSide), onclick: () => { S.sidePref[a.id] = showSide ? 'closed' : 'open'; S.diffKey = ''; drawAgentChrome(a); loadSide(a, true); } }, icon('branch'), showSide ? 'Hide changes' : (nChanges ? 'Changes (' + nChanges + ')' : 'Summary'));
    const item = (ic, label, title, fn) => h('button', { type: 'button', class: 'menuitem', title, onclick: (e) => { const d = e.currentTarget.closest('details'); if (d) d.open = false; fn(); } }, icon(ic), label);
    const more = h('details', { class: 'menu' }, h('summary', { class: 'btn ghost small', 'aria-label': 'More actions' }, 'More'),
      h('div', { class: 'menu-pop' },
        item('spark', 'Ask advisor', "Ask the advisor to review this agent's work", () => askAdvisor(a)),
        item('eye', 'Review the change', 'Start a read-only review of this change', () => startReview(a)),
        finished ? null : item('send', 'Send to another task', "Send this agent's reply or changes to another agent", () => openRelay(a)),
        item('copy', 'Open a shareable report', 'A one-page summary: goal, plan, files, checks, cost. No file contents or secrets.', () => window.open('/api/agents/' + a.id + '/report', '_blank', 'noopener')),
        item('down', 'Download the report', 'Save the report as a single HTML file to attach to a pull request or send to a teammate', () => { location.href = '/api/agents/' + a.id + '/report?download=1'; }),
        item('branch', 'Switch harness', 'Continue this task in another harness, in the same worktree', () => switchHarnessDialog(a)),
        item('plus', 'Continue in a new task', 'Start a new task from where this one is', () => continueFrom(a))));
    const railHidden = (() => { try { return localStorage.getItem('cully-rail-hidden') === '1'; } catch (_) { return false; } })();
    $('view-agents').classList.toggle('rail-hidden', railHidden);
    const railBtn = h('button', { class: 'btn ghost small', title: railHidden ? 'Show the task list and usage panel' : 'Hide the task list and usage panel so the chat has the whole window', 'aria-pressed': String(!railHidden), onclick: () => { try { localStorage.setItem('cully-rail-hidden', railHidden ? '0' : '1'); } catch (_) { /* optional */ } drawAgentChrome(a); } }, icon('branch'), railHidden ? 'Show panel' : 'Hide panel');
    $('ahead').replaceChildren(railBtn, h('h2', {}, a.title), h('span', { class: 'pill ' + phaseCls(a) }, dot(a), stateLabel(a)),
      h('span', { class: 'sp' }, sideBtn, more, finished ? null : stop));
    drawContextNote(a);
    drawTaskPanel(a);
    $('sub2').replaceChildren(
      a.repo ? h('span', { class: 'chip', title: a.repo + (a.run && a.run.isolation === 'in place' ? ' (the task works directly in this folder)' : ' (the task works on a copy in its own worktree, so this folder is untouched until you merge)') }, icon('folder'), a.repo.split('/').filter(Boolean).pop() + (a.run && a.run.isolation === 'in place' ? ', in place' : ', own worktree')) : null,
      h('button', { class: 'chip mono', title: 'Copy branch name', onclick: () => copy(a.branch, 'branch') }, icon('branch'), a.branch),
      a.agent ? h('button', { class: 'chip harness h-' + a.agent, title: 'The harness this task runs in. Click to switch to another.', onclick: () => switchHarnessDialog(a) }, AGENT_LABEL[a.agent] || a.agent) : null,
      a.token ? h('span', { class: 'chip' }, 'token ' + a.token) : h('span', { class: 'chip' }, 'ambient login'),
      h('span', { class: 'chip' }, 'started ' + ago(a.created)), h('span', { class: 'chip' }, 'active ' + ago(a.updated || a.created)),
      a.run ? h('button', { class: 'chip', title: 'How the harness underneath this task is set up', onclick: () => openRunInfo(a) }, icon('term'), a.run.harness + ', ' + (a.run.permission === 'plan' ? 'read only' : 'can edit') + ', ' + (a.run.mcpMode === 'chosen' ? a.run.mcpChosen.length + ' servers' : 'all servers')) : null,
      a.context ? h('span', { class: 'chip ctx ' + (a.context.near ? 'warn' : ''), title: 'Context window: ' + fmtTokens(a.context.used) + ' of about ' + fmtTokens(a.context.window) + ' tokens used, about ' + fmtTokens(a.context.left) + ' left' + (a.context.model ? '. Model: ' + a.context.model : '') }, 'Context ' + a.context.percent + '%, ' + fmtTokens(a.context.left) + ' left' + (a.context.compactions ? ', compacted ' + a.context.compactions + '×' : '')) : null,
      a.usage ? h('span', { class: 'chip', title: 'Tokens this task has handled. Input served from the prompt cache costs about a tenth of fresh input, so a high share means tokens saved. About ' + fmtTokens(a.usage.saved) + ' tokens of cost saved so far.' }, icon('spark'), fmtTokens(a.usage.tokens) + ' tokens, ' + a.usage.cachedShare + '% from cache') : null,
      a.harness ? h('span', { class: 'chip ' + (a.harness.status === 'done' ? 'ok' : a.harness.status === 'stopped' ? 'warn' : ''), title: 'Cully is checking this task\'s work' }, icon('check'), 'Checking: round ' + a.harness.round + ' of ' + a.harness.max + ', ' + ({ working: 'working', verifying: 'asking it to verify', steering: 'steering it', judging: 'judging the work', done: 'checks done', stopped: 'stopped' }[a.harness.status] || a.harness.status)) : null,
      a.files && a.files.length ? h('span', { class: 'chip' }, a.files.length + (a.files.length === 1 ? ' file changed' : ' files changed')) : null,
      openAdviceFor(a.id) ? h('button', { class: 'chip warn', title: 'See and send the advisor\'s advice', onclick: () => { location.hash = '#/dashboard/advisor'; } }, icon('spark'), openAdviceFor(a.id) + (openAdviceFor(a.id) === 1 ? ' piece of advice' : ' pieces of advice')) : null,
      (a.mcp || []).some((m) => m.name === 'cully' && needsAuthStatus(m.status)) ? h('button', { class: 'chip warn', title: 'This task started without Cully MCP access', onclick: () => openMCP('claude') }, icon('alert'), 'Cully MCP needs sign-in') : null);
    const stagesMatter = (a.stages || []).some((s) => s.status === 'running' || s.status === 'failed') || a.state === 'preparing';
    $('stepper').hidden = !stagesMatter;
    $('stepper').replaceChildren(...(stagesMatter ? (a.stages || []) : []).map((s) => {
      const label = (({ ok: '✓ ', failed: '✗ ', skipped: '– ', running: '● ' })[s.status] || '') + s.name;
      const look = s.name === 'scout' || s.name === 'recall';
      return look && s.status !== 'skipped'
        ? h('button', { class: 'step ' + s.status, title: 'See what the ' + s.name + ' searched and found', onclick: () => { location.hash = '#/dashboard/scout'; } }, label)
        : h('span', { class: 'step ' + s.status, title: s.detail || '' }, label);
    }));
    const tabLabel = (k, label) => label + (k === 'changes' && a.files && a.files.length ? ' (' + a.files.length + ')' : k === 'calls' && S.feed && S.feed.calls.length ? ' (' + S.feed.calls.length + ')' : '');
    $('tabs').replaceChildren(...[['chat', 'Chat'], ['graph', 'Graph'], ['calls', 'Calls'], ['changes', 'Changes'], ['handoff', 'Summary']].map(([k, label]) =>
      h('button', { class: S.tab === k ? 'on' : '', onclick: () => setTab(k) }, tabLabel(k, label))));
    const sideKind = sideKindNow();
    $('tabs-side').replaceChildren(...[['graph', 'Graph'], ['calls', 'Calls'], ['changes', 'Changes'], ['handoff', 'Summary']].map(([k, label]) =>
      h('button', { class: sideKind === k ? 'on' : '', onclick: () => setTab(k) }, tabLabel(k, label))));
    if (S.feed) {
      S.feed.composer.hidden = finished;
      S.feed.quick.hidden = finished;
      updateQuick(a);
      drawSteerUI(a);
      setTyping(a.state === 'working' || a.state === 'preparing');
    }
    drawStatusline(a);
    if (sideKind === 'calls' || sideKindNow() === 'calls') loadSide(a, true);
  }
  const sideKindNow = () => {
    if (['calls', 'changes', 'handoff', 'graph'].includes(S.tab)) return S.tab;
    const id = S.route && S.route.id;
    if (id && !(S.sideChosen && S.sideChosen[id]) && S.sideDefault && S.sideDefault[id]) return S.sideDefault[id]; // your own choice always wins over the default
    return S.sideTab || 'changes';
  };
  // The bottom status line, like a terminal's: state, harness and model, context, tokens or cost, and the
  // time since the task last moved.
  function drawStatusline(a) {
    const el = $('statusline'); if (!el) return;
    const secs = Math.max(0, Math.floor((Date.now() - new Date(a.updated || a.created).getTime()) / 1000));
    const glyph = (a.permissions || []).length ? '!' : a.state === 'working' || a.state === 'preparing' ? '●' : a.state === 'failed' ? '✗' : a.state === 'done' ? '✓' : '○';
    const seg = (cls, ...kids) => h('span', { class: 'sl ' + cls }, ...kids);
    const model = (a.context && a.context.model) || '';
    const harness = a.run ? a.run.harness : '';
    el.replaceChildren(
      seg('st ' + ((a.permissions || []).length ? 'ask' : a.state) + ' ph-' + (a.phase || a.state), glyph + ' ' + stateLabel(a)),
      harness || model ? seg('', [harness, model].filter(Boolean).join(' ')) : null,
      a.context ? seg(a.context.near ? 'warn' : '', 'ctx ' + a.context.percent + '%') : null,
      a.usage ? seg('', fmtTokens(a.usage.tokens) + ' tokens') : null,
      typeof a.cost === 'number' && a.cost > 0 ? seg('', '$' + a.cost.toFixed(a.cost < 1 ? 3 : 2)) : null,
      seg('sp', secs < 5 ? 'updated just now' : 'updated ' + (secs < 60 ? secs + 's' : secs < 3600 ? Math.floor(secs / 60) + 'm' : Math.floor(secs / 3600) + 'h') + ' ago'));
  }
  // Replies worth sending next, worked out from this task: what its last message asked, whether files
  // changed, whether the tests ran and how they ended, and how full its context is. All local facts: no
  // model is asked, so suggesting costs nothing.
  function quickReplies(a, sum) {
    const out = []; const add = (label, text) => { if (out.length < 4 && !out.some((x) => x.label === label)) out.push({ label, text: text || label }); };
    const last = ((sum && sum.lastReply) || a.last || '').trim();
    const asks = /\?\s*$/.test(last) || /\b(should i|shall i|do you want|would you like|want me to|let me know|which (one|option))\b/i.test(last);
    const changed = (sum && sum.fileCount) || (a.files ? a.files.length : 0);
    const c = sum && sum.checks; const since = (sum && sum.editsSinceCheck) || 0;
    if (a.state === 'done' || a.state === 'failed' || a.state === 'stopped') return [];
    if (asks) { add('Yes, go ahead'); add('No, skip that', 'No, skip that and carry on with the rest.'); add('Explain first', 'Explain what you would change and why before doing it.'); }
    if (c && c.last === 'failed') add('Fix the failing check', 'Fix the failing check, then run it again and show me the result.');
    if (changed && (!c || c.last === 'none' || since > 0)) add('Run the tests', 'Run the tests and show me the output.');
    if (changed && c && c.last === 'passed' && since === 0) { add('Commit your work', 'Commit your work with a clear message. Do not push.'); }
    if (changed) add('Summarize what you changed', 'Summarize what you changed and why, file by file, briefly.');
    if (a.context && a.context.near) add('Wrap up and hand off', 'You are running low on room. Summarize where we are and what is left, so I can continue in a new task.');
    if (!changed && !asks) { add('Continue'); add('Summarize what you found', 'Summarize what you found so far, briefly.'); }
    if (!asks && out.length < 2) add('Continue');
    return out;
  }
  async function updateQuick(a) {
    const f = S.feed;
    if (!f || !f.quick) return;
    const finished = a.state === 'done' || a.state === 'failed' || a.state === 'stopped';
    if (finished || a.state === 'working' || a.state === 'preparing' || (a.permissions || []).length) { f.quick.replaceChildren(); f.quickKey = ''; return; }
    const key = a.id + '|' + a.state + '|' + (a.events || 0) + '|' + (a.files ? a.files.length : 0);
    if (f.quickKey === key) return;
    f.quickKey = key;
    let sum = null;
    try { sum = await api('/api/agents/' + a.id + '/summary'); } catch (_) { /* suggest from what the page already knows */ }
    if (S.feed !== f || f.quickKey !== key) return;
    f.quick.replaceChildren(...quickReplies(a, sum).map((r) => h('button', { type: 'button', title: r.text, onclick: () => sendMessage(a.id, r.text) }, r.label)));
  }
  // Steer and follow-up: while a task works, a message either goes in now (steer) or waits for the turn to end.
  function drawSteerUI(a) {
    const f = S.feed;
    if (!f || !f.sendBtn) return;
    const busy = a.state === 'working' || a.state === 'preparing';
    f.afterBtn.hidden = !busy;
    f.sendBtn.className = busy ? 'btn small steerbtn' : 'btn round';
    f.sendBtn.title = busy ? 'Deliver this message as soon as it is safe (Enter)' : 'Send';
    f.sendBtn.setAttribute('aria-label', busy ? 'Steer now' : 'Send');
    f.sendBtn.replaceChildren(...(busy ? ['Steer now'] : [icon('send')]));
    f.hint.textContent = busy ? 'Enter steers now, Alt+Enter sends after this turn, Shift+Enter adds a line' : 'Enter sends, Shift+Enter adds a line';
    const items = a.queue || [];
    f.msgq.hidden = !items.length;
    if (!items.length) { f.msgq.replaceChildren(); return; }
    const call = (path, opts) => api('/api/agents/' + a.id + path, opts).catch((x) => toast(x.message, true));
    const chips = items.map((m) => {
      const label = m.sent ? 'Sent, applies at the next step' : m.mode === 'steer' ? 'Steer' : 'After this turn';
      return h('span', { class: 'mchip ' + m.mode + (m.sent ? ' sent' : ''), title: m.note || m.text },
        h('span', { class: 'mtext' }, m.text.length > 48 ? m.text.slice(0, 48) + '…' : m.text),
        h('span', { class: 'mmode faint' }, label),
        !m.sent && m.mode === 'followup' ? h('button', { type: 'button', class: 'mbtn', title: 'Send it now instead of after this turn', onclick: () => call('/queue/' + m.id + '/promote', { method: 'POST' }) }, 'Send now') : null,
        !m.sent ? h('button', { type: 'button', class: 'mbtn', title: 'Remove this message', 'aria-label': 'Remove this message', onclick: () => call('/queue/' + m.id, { method: 'DELETE' }) }, icon('x')) : null);
    });
    const down = items.find((m) => m.downgraded);
    f.msgq.replaceChildren(h('div', { class: 'mrow' }, ...chips),
      h('div', { class: 'faint small' }, (down ? down.note + ' ' : '') + 'Stopping the task drops these messages.'));
  }
  async function sendMessage(id, text, mode) {
    text = (text || '').trim();
    if (!text) return;
    if (S.feed) S.feed.forceStick = true; // your own message always scrolls into view
    try {
      const r = await api('/api/agents/' + id + '/message', { method: 'POST', body: JSON.stringify(mode ? { text, mode } : { text }) });
      if (r && r.item && r.item.downgraded) toast(r.item.note);
    } catch (e) { toast(e.message, true); }
  }
  async function continueFrom(a) {
    try {
      const sum = await api('/api/agents/' + a.id + '/summary');
      S.draft = 'Continue the work from "' + a.title + '" (branch ' + a.branch + ').\n'
        + (sum.fileCount ? 'So far it changed ' + sum.fileCount + (sum.fileCount === 1 ? ' file' : ' files') + '. ' : '')
        + checksSentence(sum.checks, sum.editsSinceCheck) + '\n'
        + 'Review what was done, finish what is left, and run the checks.';
      location.hash = '#/';
    } catch (e) { toast(e.message, true); }
  }

  // When a reply names a session Cully knows (by its id), offer to carry on from it in a new task,
  // using a short handoff of what that session did. This works for sessions of any harness.
  async function sessionIndex() {
    if (S.libMap && Date.now() - S.libMapAt < 60000) return S.libMap;
    try { const lib = await api('/api/sessions/library'); S.libMap = new Map(lib.sessions.map((x) => [x.id, x])); S.libMapAt = Date.now(); } catch (_) { S.libMap = S.libMap || new Map(); }
    return S.libMap;
  }
  async function attachSessionActions(node, text) {
    const ids = [...new Set((text.match(/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/gi) || []).map((x) => x.toLowerCase()))].slice(0, 3);
    if (!ids.length) return;
    const map = await sessionIndex();
    for (const id of ids) {
      const x = map.get(id);
      if (!x) continue;
      const btn = h('button', { class: 'btn small' }, icon('plus'), 'Start a new task from its handoff');
      btn.addEventListener('click', async () => {
        btn.disabled = true;
        try { const v = await api('/api/sessions/' + id + '/continue', { method: 'POST' }); await refresh(); location.hash = '#/task/' + v.id; } catch (e) { toast(e.message, true); btn.disabled = false; }
      });
      node.append(h('div', { class: 'sessact' }, harnessChip(x.agent), h('div', {}, sessionLink(x), h('div', { class: 'faint small' }, x.cwd + ', ' + ago(x.updated))),
        h('span', { class: 'faint small' }, 'Out of room or closed? Carry on in a fresh task.'), btn));
    }
  }

  // ---------- chat feed ----------
  function openFeed(id) {
    const es = new EventSource('/api/agents/' + id + '/events?since=0');
    S.es = es;
    es.onmessage = (m) => appendEvent(JSON.parse(m.data));
  }
  function setTyping(on) {
    const f = S.feed;
    if (!f) return;
    if (!on) { if (f.typing) { f.typing.remove(); f.typing = null; } return; }
    if (!f.typing) f.typing = h('div', { class: 'typing', 'aria-label': 'Agent is working' }, h('i'), h('i'), h('i'));
    f.body.append(f.typing);
  }
  // The "Latest" button appears when you have scrolled up, and counts what arrived since.
  function paintJump() {
    const f = S.feed; if (!f || !f.jump) return;
    const b = f.body; const atEnd = b.scrollTop + b.clientHeight >= b.scrollHeight - 60;
    f.jump.hidden = atEnd;
    f.jump.replaceChildren(icon('down'), f.unseen ? f.unseen + (f.unseen === 1 ? ' new message' : ' new messages') : 'Latest');
  }
  // A long reply is shown in full only on request, so one big answer does not bury the conversation.
  function clampLong(node, text) {
    const lines = (text.match(/\n/g) || []).length;
    if (text.length < 2200 && lines < 32) return node;
    node.classList.add('clamped');
    const btn = h('button', { type: 'button', class: 'linkbtn showall', onclick: () => { const on = node.classList.toggle('clamped'); btn.textContent = on ? 'Show all (' + Math.max(lines, 1) + ' lines)' : 'Show less'; } }, 'Show all (' + Math.max(lines, 1) + ' lines)');
    node.append(btn);
    return node;
  }
  function appendEvent(ev) {
    const f = S.feed;
    if (!f) return;
    if (ev.kind === 'queue_update') {
      const ag = S.agents.find((x) => x.id === S.route.id);
      if (ag) { try { ag.queue = JSON.parse(ev.text); } catch (_) { return; } drawSteerUI(ag); if (S.route.name === 'board') renderBoard(); renderRail(); }
      return;
    }
    const b = f.body;
    const stick = b.scrollTop + b.clientHeight >= b.scrollHeight - 60;
    if (f.typing) f.typing.remove();
    if (ev.kind === 'plan') { renderPlan(ev.text); return; }
    if (ev.kind === 'tool' || ev.kind === 'tool_result') addToolRow(ev);
    else {
      f.group = null;
      if (ev.kind === 'text') { const node = clampLong(h('div', { class: 'msg bot' + (/\?\s*$/.test(ev.text.trim()) ? ' asks' : ''), title: new Date(ev.t).toLocaleTimeString() }, markdown(ev.text)), ev.text); b.append(node); attachSessionActions(node, ev.text); }
      else if (ev.kind === 'user') b.append(h('div', { class: 'msg you', title: new Date(ev.t).toLocaleTimeString() }, ev.text));
      else if (ev.kind === 'error') b.append(h('div', { class: 'msg error' }, ev.text));
      else if (ev.kind === 'result') b.append(h('div', { class: 'rule turnend ' + (ev.failed ? 'bad' : 'ok') }, h('span', { class: 'tglyph' }, ev.failed ? '✗' : '✓'), ev.failed ? 'Turn ended with an error' : 'Turn done', h('span', { class: 'faint' }, new Date(ev.t).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }))));
      else if (ev.kind === 'note') b.append(h('div', { class: 'note-card' }, ev.text));
      else if (ev.kind === 'stage' && /failed/.test(ev.text)) b.append(h('div', { class: 'note' }, ev.text));
    }
    if (f.loaded && b.lastElementChild) b.lastElementChild.classList.add('fresh');
    const a = S.agents.find((x) => x.id === S.route.id);
    if (a && (a.state === 'working' || a.state === 'preparing')) setTyping(true);
    if (stick || f.forceStick) { b.scrollTop = b.scrollHeight; f.forceStick = false; f.unseen = 0; } else if (f.loaded && (ev.kind === 'text' || ev.kind === 'error')) f.unseen = (f.unseen || 0) + 1;
    paintJump();
  }
  // Tool calls as typed rows: an icon for what kind of thing it did, a spinner while it runs, a tick or a
  // cross when it ends. Consecutive calls share one collapsible group so a long run stays readable.
  const TOOL_ICON = { Bash: 'term', Read: 'eye', Edit: 'pencil', Write: 'pencil', MultiEdit: 'pencil', NotebookEdit: 'pencil', Grep: 'search', Glob: 'search', WebSearch: 'globe', WebFetch: 'globe', Task: 'spark', Agent: 'spark', ToolSearch: 'search' };
  const toolIcon = (name) => TOOL_ICON[name] || (/^mcp__/.test(name) ? 'plug' : 'spark');
  const toolLabel = (name) => (/^mcp__([^_]+(?:_[^_]+)*?)__(.+)$/.exec(name) || []).slice(1).reverse().join(' on ') || name;
  function addToolRow(ev) {
    const f = S.feed;
    if (!f.group) {
      const rows = h('div', { class: 'rows' });
      const sum = h('summary', {});
      f.group = { el: h('details', { class: 'tools' }, sum, rows), rows, sum, names: {}, n: 0, bad: false, last: null };
      f.body.append(f.group.el);
    }
    const g = f.group;
    if (ev.kind === 'tool') {
      g.n += 1; g.names[ev.tool] = (g.names[ev.tool] || 0) + 1;
      const row = h('div', { class: 'trow2 run' }, h('span', { class: 'tspin' }, icon(toolIcon(ev.tool))), h('b', {}, toolLabel(ev.tool)), h('span', { class: 'tsum' }, ev.text || ''));
      g.rows.append(row); g.last = row; f.running = ev.tool + (ev.text ? ' ' + ev.text : '');
      f.callLast = { n: f.calls.length + 1, tool: ev.tool, input: ev.text || '', t0: new Date(ev.t).getTime() || Date.now(), status: 'run' };
      f.calls.push(f.callLast);
    } else {
      f.running = '';
      const row = g.last; g.last = null;
      if (row) { row.classList.remove('run'); row.classList.add(ev.failed ? 'bad' : 'ok'); }
      if (ev.failed) { g.bad = true; g.el.classList.add('bad'); g.el.open = true; }
      g.rows.append(h('div', { class: 'tres' + (ev.failed ? ' f' : '') }, (ev.text || 'ok').slice(0, 240)));
      const c = f.callLast; f.callLast = null;
      if (c) { c.status = ev.failed ? 'failed' : 'ok'; c.t1 = new Date(ev.t).getTime() || Date.now(); c.result = ev.text || 'ok'; }
    }
    const parts = Object.entries(g.names).map(([k, v]) => toolLabel(k) + (v > 1 ? ' ×' + v : '')).join(', ');
    if (sideKindNow() === 'calls' && S.agents.some((x) => x.id === S.route.id)) drawCalls(S.agents.find((x) => x.id === S.route.id));
    g.sum.textContent = 'Ran ' + g.n + (g.n === 1 ? ' tool' : ' tools') + (parts ? ' (' + parts + ')' : '') + (g.bad ? ', with a failure' : '');
    updateActivity();
  }
  // The agent's own plan, pinned above the chat and ticked off as it goes (the way Kiro shows its task
  // list): how far along it is, what it is on now, and what is left.
  function renderPlan(text) {
    const f = S.feed;
    if (!f) return;
    let items = [];
    try { items = JSON.parse(text); } catch (_) { return; }
    if (!items.length) return;
    f.plan = items;
    if (!f.planEl) { f.planEl = h('section', { class: 'plancard', 'aria-label': 'Plan' }); f.body.before(f.planEl); }
    const done = items.filter((x) => x.s === 'completed').length;
    const now = items.find((x) => x.s === 'in_progress');
    const open = f.planOpen != null ? f.planOpen : done < items.length;
    const bar = h('span', { class: 'fbar' }, fill(Math.round((done * 100) / items.length)));
    f.planEl.replaceChildren(h('button', { type: 'button', class: 'planhead', 'aria-expanded': String(open), onclick: () => { f.planOpen = !open; renderPlan(text); } },
      icon('check'), h('b', {}, 'Plan'), h('span', { class: 'faint small' }, done + ' of ' + items.length + ' done' + (now ? ', now: ' + now.c : '')), bar, h('span', { class: 'chev faint' }, open ? '⌄' : '›')),
      open ? h('ul', { class: 'planlist' }, ...items.map((x) => h('li', { class: 'p-' + x.s }, h('span', { class: 'pdot' }, x.s === 'completed' ? '✓' : x.s === 'in_progress' ? '●' : '○'), h('span', {}, x.c)))) : null);
    updateActivity();
  }
  // One line that always says what the task is doing right now, or that it is your turn.
  function updateActivity() {
    const f = S.feed; const el = $('activity');
    if (!f || !el) return;
    const a = S.agents.find((x) => x.id === S.route.id);
    if (!a) return;
    const asking = (a.permissions || []).length;
    const live = a.state === 'working' || a.state === 'preparing';
    el.className = 'activity ' + (asking ? 'ask' : live ? 'live' : a.state === 'waiting' ? 'turn' : 'off');
    if (!asking && !live && a.state !== 'waiting') { el.replaceChildren(); return; }
    const secs = Math.max(0, Math.floor((Date.now() - new Date(a.updated || a.created).getTime()) / 1000));
    const now = f.plan && f.plan.find((x) => x.s === 'in_progress');
    const what = asking ? 'Needs your answer: allow or deny the request below' : live ? (f.running ? 'Running ' + f.running : now ? now.c : a.state === 'preparing' ? 'Gathering context' : 'Thinking') : 'Your turn: reply below or pick a suggestion';
    if (a.stuck && !asking) {
      el.className = 'activity stuck';
      el.replaceChildren(h('span', { class: 'adot' }), h('span', { class: 'awhat' }, 'Looks stuck: ' + a.stuck.label.toLowerCase() + '. ' + a.stuck.detail),
        h('button', { class: 'btn small', title: 'Tell the task it is going in circles and ask it to try another way', onclick: () => sendMessage(a.id, 'You seem to be stuck: ' + a.stuck.detail + ' Stop repeating it, say what is blocking you, and try a different approach.') }, 'Nudge'));
      return;
    }
    el.replaceChildren(h('span', { class: 'adot' }), h('span', { class: 'awhat' }, uiClipText(what, 120)), live ? h('span', { class: 'faint small' }, secs < 5 ? 'just now' : secs + 's since the last update') : null);
  }
  const uiClipText = (t, n) => (t.length > n ? t.slice(0, n) + '…' : t);
  setInterval(() => { if (S.route && S.route.name === 'agent') { updateActivity(); const a = S.agents.find((x) => x.id === S.route.id); if (a) drawStatusline(a); } }, 1000);

  // ---------- changes / handoff ----------
  function parseDiff(text) {
    const parts = text.split(/^diff --git /m);
    const head = parts.shift().trim();
    const files = parts.map((p) => {
      const lines = p.split('\n');
      const m = /^a\/(.+?) b\/(.+)$/.exec(lines[0]);
      let add = 0; let del = 0;
      for (const l of lines) { if (l.startsWith('+') && !l.startsWith('+++')) add++; else if (l.startsWith('-') && !l.startsWith('---')) del++; }
      return { name: m ? m[2] : lines[0], lines: lines.slice(1), add, del };
    });
    return { head, files };
  }
  // ---- the calls tab: every tool call this task made, with the one waiting on you pinned on top ----
  const callTime = (ms) => { const s = Math.max(0, Math.round(ms / 1000)); return s < 60 ? s + 's' : Math.floor(s / 60) + 'm ' + (s % 60) + 's'; };
  function drawCalls(a) {
    const body = $('sidebody'); const f = S.feed;
    if (!body || !f || sideKindNow() !== 'calls') return;
    const perms = a.permissions || [];
    const now = Date.now();
    // The latest running call of the tool that asked is the one waiting for the answer.
    const claimed = new Map();
    for (const p of perms) { for (let i = f.calls.length - 1; i >= 0; i--) { const c = f.calls[i]; if (c.status === 'run' && c.tool === p.tool && ![...claimed.values()].includes(c)) { claimed.set(p.id, c); break; } } }
    const state = (c) => ([...claimed.values()].includes(c) ? 'wait' : c.status);
    const GLYPH = { run: '', wait: '!', ok: '✓', failed: '✗' };
    const glyph = (st) => h('span', { class: 'cglyph ' + st, 'aria-hidden': 'true' }, GLYPH[st]);
    const open = (f.callOpen = f.callOpen || new Set());
    const row = (c) => {
      const st = state(c);
      const dur = callTime((c.t1 || now) - c.t0);
      const isOpen = open.has(c.n);
      const label = { run: 'running', wait: 'waiting for you', ok: 'ok', failed: 'failed' }[st];
      return h('li', { class: 'crow ' + st },
        h('button', { type: 'button', class: 'chead', 'aria-expanded': String(isOpen), title: 'Show or hide the result', onclick: () => { if (open.has(c.n)) open.delete(c.n); else open.add(c.n); drawCalls(a); } },
          glyph(st), h('b', {}, toolLabel(c.tool)), h('span', { class: 'csum' }, c.input), h('span', { class: 'cdur faint' }, dur)),
        h('div', { class: 'cstate faint' }, label),
        isOpen ? h('pre', { class: 'cres' }, c.result || (st === 'run' ? 'No result yet.' : st === 'wait' ? 'Waiting for your answer before it runs.' : 'No output.')) : null);
    };
    const pinned = [];
    for (const p of perms) pinned.push(h('div', { class: 'cpin wait' }, glyph('wait'), h('div', { class: 'cpinbody' }, h('b', {}, 'Waiting for you to allow'), h('div', {}, toolLabel(p.tool) + (p.summary ? ': ' + uiClipText(p.summary, 160) : '')), h('div', { class: 'faint small' }, 'asked ' + ago(p.at) + '. Answer in the box under the chat.'))));
    for (const c of f.calls) if (c.status === 'run' && ![...claimed.values()].includes(c)) pinned.push(h('div', { class: 'cpin run' }, glyph('run'), h('div', { class: 'cpinbody' }, h('b', {}, 'Running'), h('div', {}, toolLabel(c.tool) + (c.input ? ': ' + uiClipText(c.input, 160) : '')), h('div', { class: 'faint small' }, callTime(now - c.t0) + ' so far'))));
    const failed = f.calls.filter((c) => c.status === 'failed').length;
    body.replaceChildren(
      ...pinned,
      f.calls.length ? h('div', { class: 'ccount faint small' }, f.calls.length + (f.calls.length === 1 ? ' call' : ' calls') + (failed ? ', ' + failed + ' failed' : '')) : h('div', { class: 'empty' }, 'No tool calls yet.'),
      h('ol', { class: 'calls' }, ...f.calls.map(row)));
  }
  setInterval(() => { if (S.route && S.route.name === 'agent' && S.feed && sideKindNow() === 'calls') { const a = S.agents.find((x) => x.id === S.route.id); if (a && S.feed.calls.some((c) => c.status === 'run')) drawCalls(a); } }, 1000);
  // ---- the graph tab: what the task did, as phases joined by the reason for each step ----
  // It is a picture of a trace Cully observed. The layout comes from graph.js; this part only draws it.
  const SVGNS = 'http://www.w3.org/2000/svg';
  function sv(tag, attrs, ...kids) {
    const el = document.createElementNS(SVGNS, tag);
    for (const [k, v] of Object.entries(attrs || {})) { if (v == null || v === false) continue; if (k.startsWith('on')) el.addEventListener(k.slice(2), v); else el.setAttribute(k, v); }
    for (const kid of kids.flat()) if (kid != null && kid !== false) el.append(kid.nodeType ? kid : document.createTextNode(String(kid)));
    return el;
  }
  const GRAPH_LEGEND = [['working', 'Working'], ['tool', 'Waiting on a tool'], ['needs-you', 'Needs you'], ['helper', 'Waiting on a helper'], ['checking', 'Checking'], ['stuck', 'Stuck'], ['done', 'Done'], ['failed', 'Failed or stopped']];
  const ANSWER_WORDS = { allowed: 'Allowed once', 'allowed-always': 'Allowed for the rest of the task', denied: 'Denied', expired: 'No answer in time, so it was not allowed' };
  const ACT_GLYPH = { tool: '>', said: '"', you: '@', plan: '#', note: '-', error: '!', turn: '|' };
  const ACT_WORD = { tool: 'Tool', said: 'It said', you: 'You said', plan: 'Plan', note: 'Note', error: 'Error', turn: 'Turn' };
  // One state as a card on the flow: what state it was, how long, what it did there, and what came of it.
  function graphStep(n, view, ui, now, a, extra) {
    const G = window.CullyGraph;
    const open = G.isOpen(n);
    const ms = Math.max(0, (open ? now : new Date(n.end).getTime()) - new Date(n.start).getTime());
    const sum = G.actSummary(n);
    const needs = n.phase === 'needs-you';
    const acts = n.acts || [];
    const head = h('div', { class: 'gf-head' },
      h('span', { class: 'pill ph-' + n.phase }, G.PHASE_LABEL[n.phase] || n.phase),
      h('b', { class: 'gf-title' }, n.phase === 'tool' && n.tool ? n.tool : n.phase === 'earlier' ? (n.summary || 'Earlier steps') : G.nodeLabel(n)),
      n.count > 1 && (n.phase === 'tool' || n.phase === 'working') ? h('span', { class: 'chip' }, n.count + ' times') : null,
      h('span', { class: 'sp' }),
      h('span', { class: 'faint small' }, (n.start ? new Date(n.start).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }) : '') + ', ' + (open ? G.dur(ms) + ' so far' : G.dur(ms))));
    const lines = [];
    if (needs) {
      lines.push(h('p', { class: 'gf-line' }, h('b', {}, 'Asked: '), n.summary || n.tool || 'permission'));
      lines.push(h('p', { class: 'gf-line' }, h('b', {}, 'You answered: '), ANSWER_WORDS[n.answer] || (open ? 'Not answered yet' : n.answer || 'No answer recorded'), open ? '' : ', after ' + G.dur(ms)));
    } else if (sum.sentence) lines.push(h('p', { class: 'gf-line' }, sum.sentence + '.'));
    else if (n.summary && n.phase !== 'earlier' && n.phase !== 'tool') lines.push(h('p', { class: 'gf-line' }, n.summary));
    if (n.changed) lines.push(h('p', { class: 'gf-line' }, h('b', {}, 'Files: '), n.changed + ' ', h('button', { type: 'button', class: 'linkbtn', onclick: () => setTab('changes') }, 'Open the Changes tab')));
    const chips = [];
    for (const t of n.tags || []) if (!t.startsWith('tool:')) chips.push(h('span', { class: 'chip' }, t));
    if (n.cost) chips.push(h('span', { class: 'chip' }, '$' + n.cost.toFixed(2)));
    if (n.tokens) chips.push(h('span', { class: 'chip' }, fmtTokens(n.tokens) + ' tokens'));
    if (n.ended === 'restart') chips.push(h('span', { class: 'chip warn' }, 'The server restarted here'));
    const more = acts.length ? h('details', { class: 'gf-acts', ...(ui.open.has(n.id) ? { open: true } : {}) },
      h('summary', {}, 'What it did here', h('span', { class: 'faint small' }, ' ' + (sum.total > sum.shown ? sum.shown + ' of ' + sum.total : String(sum.total)))),
      h('ol', { class: 'gf-list' }, ...acts.map((x) => h('li', { class: 'gf-act k-' + x.k + (x.ok === 'fail' ? ' bad' : '') },
        h('span', { class: 'gf-glyph', 'aria-hidden': 'true' }, ACT_GLYPH[x.k] || '-'),
        h('span', { class: 'gf-t faint small' }, x.t ? new Date(x.t).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }) : ''),
        h('span', { class: 'gf-l' }, h('span', { class: 'visually-hidden' }, (ACT_WORD[x.k] || '') + ': '), x.l,
          x.k === 'tool' ? h('span', { class: 'gf-r ' + (x.ok === 'fail' ? 'bad' : '') }, x.ok === 'fail' ? ' failed' + (x.r ? ': ' + x.r : '') : x.ok === 'ok' ? ' ok' + (x.r ? ': ' + x.r : '') : ' running') : null))))) : null;
    if (more) more.addEventListener('toggle', () => { if (more.open) ui.open.add(n.id); else ui.open.delete(n.id); });
    const notes = [];
    if (extra && extra.suspected) notes.push(h('p', { class: 'faint small' }, 'Suspected: this step came just before the failure. It is a hint and may not be the cause.'));
    if (n.phase === 'stuck') notes.push(h('p', { class: 'faint small' }, 'Cully thinks the task was going in circles. That is a guess from repeated calls.'));
    if (n.phase === 'failed') notes.push(h('p', { class: 'faint small' }, 'The task failed here. From outside, Cully cannot tell what caused it.'));
    if (n.phase === 'earlier') notes.push(h('p', { class: 'faint small' }, 'Older plain steps were folded into this one to keep the graph short. Questions to you, stuck spells, helpers and failures are kept.'));
    const show = h('button', { type: 'button', class: 'linkbtn gf-show', title: 'See this state on the graph, with what it did and how', onclick: () => openGraphWindow(a, n.id) }, 'Show on graph');
    return h('article', { class: 'gf-card ph-' + n.phase + (open ? ' open' : '') + (extra && extra.suspected ? ' suspected' : '') + (needs ? ' needs' : '') },
      head, ...lines, chips.length ? h('div', { class: 'row gf-chips' }, ...chips) : null, more, show, ...notes);
  }
  function renderGraph(body, a, view) {
    const G = window.CullyGraph;
    const now = Date.now();
    S.graphUi = S.graphUi || {};
    const ui = (S.graphUi[a.id] = S.graphUi[a.id] || { open: new Set() });
    if (!ui.open) ui.open = new Set();
    const live = ['preparing', 'working', 'waiting'].includes(a.state);
    const keep = body.scrollTop; const atEnd = body.scrollTop + body.clientHeight >= body.scrollHeight - 60;
    const all = (view && view.nodes) || [];
    if (!all.length) { body.replaceChildren(h('div', { class: 'empty' }, 'No steps recorded yet. They appear here as the task works.')); return; }
    const col = G.collapseLoops(all);
    const loops = new Map(col.loops.map((l) => [l.to, l]));
    const lanes = new Map(((view && view.lanes) || []).map((l) => [l.id, l]));
    // a step before a failure is only a suspect, never named as the cause
    const suspect = new Set();
    col.nodes.forEach((n, k) => { if (n.phase !== 'failed') return; for (let m = k - 1; m >= 0; m--) if (col.nodes[m].phase === 'tool' || col.nodes[m].phase === 'working') { suspect.add(col.nodes[m].id); break; } });
    const bar = h('div', { class: 'gf-strip', role: 'img', 'aria-label': 'The whole trace: one segment per state, as wide as it lasted' },
      ...G.strip(view, now).map((sg) => { const el = h('i', { class: 'gf-seg ph-' + sg.phase, title: (G.PHASE_LABEL[sg.phase] || sg.phase) + ', ' + G.dur(sg.ms) }); el.style.flexGrow = String(sg.grow); return el; }));
    const flow = h('div', { class: 'gf-flow' });
    let lastLane = null;
    col.nodes.forEach((n, k) => {
      const lane = n.session || 'run';
      if (lane !== lastLane) {
        const meta = lanes.get(lane) || { label: lane, agent: '' };
        flow.append(h('div', { class: 'gf-lane' }, meta.agent ? harnessChip(meta.agent) : null, h('b', {}, meta.label || lane), k ? h('span', { class: 'faint small' }, 'a new session') : null));
        lastLane = lane;
      }
      if (k > 0) flow.append(h('div', { class: 'gf-edge' + (col.nodes[k - 1].session !== n.session ? ' cross' : '') }, h('span', { class: 'gf-edgel' }, G.reasonLabel(n.cause) || '')));
      const row = h('div', { class: 'gf-row' + (n.phase === 'helper' ? ' helper' : '') }, h('span', { class: 'gf-node ph-' + n.phase + (G.isOpen(n) ? ' open' : '') + (n.phase === 'needs-you' ? ' ring' : '') }), graphStep(n, view, ui, now, a, { suspected: suspect.has(n.id) }));
      flow.append(row);
      const lp = loops.get(n.id);
      if (lp) flow.append(h('div', { class: 'gf-loop' }, h('span', { class: 'gf-loopchip' }, icon('refresh'), 'These two steps repeated ' + lp.count + ' times, shown once')));
    });
    const legend = h('div', { class: 'glegend', 'aria-label': 'Legend' }, ...GRAPH_LEGEND.map(([k, t]) => h('span', { class: 'gl' }, h('span', { class: 'gsw ph-' + k }), t)), h('span', { class: 'gl' }, h('span', { class: 'gsw dashed' }), 'Suspected, only a hint'));
    body.replaceChildren(
      h('div', { class: 'ghead' }, h('b', {}, G.headline(view, now)), live ? h('span', { class: 'faint small' }, 'updating live') : null, h('span', { class: 'sp' }), h('button', { type: 'button', class: 'btn small', title: 'Open the graph in a large window', onclick: () => openGraphWindow(a) }, icon('branch'), 'Open the graph')),
      bar, legend, flow,
      view.mermaid ? h('details', { class: 'gtext' }, h('summary', { class: 'faint small' }, 'Text version of the same trace'), h('pre', { class: 'gpre' }, view.mermaid)) : null,
      h('p', { class: 'faint small' }, view.note || ''));
    body.scrollTop = atEnd && live ? body.scrollHeight : keep;
  }
  // Drag handles that resize the task list on the left and the Graph/Calls/Changes/Summary panel on the right.
  // The widths are remembered in this browser.
  const PANEL_KEY = 'cully.panelw';
  function panelWidths() { try { return JSON.parse(localStorage.getItem(PANEL_KEY)) || {}; } catch (_) { return {}; } }
  function savePanelWidth(k, v) { const w = panelWidths(); w[k] = Math.round(v); try { localStorage.setItem(PANEL_KEY, JSON.stringify(w)); } catch (_) {} }
  function applyPanelWidths() {
    const w = panelWidths(); const rail = document.querySelector('.rail');
    if (rail && w.rail) rail.style.width = w.rail + 'px';
    const grid = $('agrid'); if (grid && w.side) grid.style.setProperty('--side-w', w.side + 'px');
  }
  function dragHandle(cls, label, getStart, onMove, onEnd) {
    const hd = h('div', { class: 'resizer ' + cls, role: 'separator', 'aria-orientation': 'vertical', 'aria-label': label, tabindex: '0', title: 'Drag to resize; double-click to reset' });
    hd.addEventListener('pointerdown', (e) => { e.preventDefault(); const x0 = e.clientX; const start = getStart(); hd.setPointerCapture(e.pointerId); hd.classList.add('on');
      const mv = (ev) => onMove(start, ev.clientX - x0); const up = () => { hd.classList.remove('on'); hd.removeEventListener('pointermove', mv); hd.removeEventListener('pointerup', up); hd.removeEventListener('pointercancel', up); onEnd(); };
      hd.addEventListener('pointermove', mv); hd.addEventListener('pointerup', up); hd.addEventListener('pointercancel', up); });
    return hd;
  }
  function installRailResizer() {
    const rail = document.querySelector('.rail'); if (!rail || rail.nextElementSibling && rail.nextElementSibling.classList.contains('rail-rz')) return;
    const hd = dragHandle('rail-rz', 'Resize the task list', () => rail.getBoundingClientRect().width,
      (w0, dx) => { rail.style.width = Math.min(640, Math.max(220, w0 + dx)) + 'px'; },
      () => savePanelWidth('rail', rail.getBoundingClientRect().width));
    hd.addEventListener('dblclick', () => { rail.style.width = ''; const w = panelWidths(); delete w.rail; try { localStorage.setItem(PANEL_KEY, JSON.stringify(w)); } catch (_) {} });
    rail.after(hd);
  }
  function installSideResizer(grid, side) {
    side.style.position = 'relative';
    side.append(dragHandle('side-rz', 'Resize the side panel', () => side.getBoundingClientRect().width,
      (w0, dx) => { const max = Math.max(360, grid.getBoundingClientRect().width - 320); grid.style.setProperty('--side-w', Math.min(max, Math.max(300, w0 - dx)) + 'px'); },
      () => savePanelWidth('side', side.getBoundingClientRect().width)));
    side.querySelector('.resizer').addEventListener('dblclick', () => { grid.style.removeProperty('--side-w'); const w = panelWidths(); delete w.side; try { localStorage.setItem(PANEL_KEY, JSON.stringify(w)); } catch (_) {} });
  }
  // ---- the graph window: a large canvas with every state as a node, and a panel for the one you pick ----
  function openGraphWindow(a, focusId) {
    const G = window.CullyGraph;
    const st = { view: null, sel: focusId || null, scale: 1, tx: 0, ty: 0, L: null, groups: new Map(), moved: false };
    const canvas = h('div', { class: 'gw-canvas', tabindex: '0', role: 'application', 'aria-label': 'Task graph. Drag to move, scroll to zoom, left and right arrows to step between states, plus and minus to zoom, 0 to fit.' });
    const panel = h('aside', { class: 'gw-panel', 'aria-live': 'polite' });
    const headline = h('span', { class: 'faint small' });
    const liveTag = h('span', { class: 'chip', hidden: true }, 'live');
    const stripBox = h('div', { class: 'gw-strip' });
    const clamp = (v, lo, hi) => Math.min(hi, Math.max(lo, v));
    const size = () => { const r = canvas.getBoundingClientRect(); return { w: Math.max(200, r.width), h: Math.max(200, r.height) }; };
    let vp = null;
    const hbar = h('input', { type: 'range', class: 'gw-hbar', min: '0', max: '1000', value: '0', 'aria-label': 'Scroll the graph left and right', title: 'Scroll left and right' });
    // The scroll range is the graph's width at the current zoom, with a margin so the ends stay reachable.
    const hrange = () => { const r = size(); const cw = st.L ? st.L.width * st.scale : 0; const margin = 40; return { max: margin, min: Math.min(margin, r.w - cw - margin) }; };
    const syncBar = () => { const g = hrange(); hbar.disabled = !(g.max - g.min > 1); hbar.value = String(g.max - g.min > 1 ? Math.round(((g.max - clamp(st.tx, g.min, g.max)) / (g.max - g.min)) * 1000) : 0); };
    hbar.addEventListener('input', () => { const g = hrange(); st.tx = g.max - (Number(hbar.value) / 1000) * (g.max - g.min); applyT(); });
    const applyT = () => { if (vp) vp.setAttribute('transform', 'translate(' + st.tx.toFixed(1) + ' ' + st.ty.toFixed(1) + ') scale(' + st.scale.toFixed(3) + ')'); canvas.classList.toggle('far', st.scale < 0.6); };
    const apply = () => { applyT(); syncBar(); };
    const fit = () => { if (!st.L) return; const r = size(); st.scale = clamp(Math.min((r.w - 40) / st.L.width, (r.h - 40) / st.L.height), 0.25, 1.3); st.tx = (r.w - st.L.width * st.scale) / 2; st.ty = (r.h - st.L.height * st.scale) / 2; apply(); };
    const centerOn = (id) => { const p = st.L && st.L.nodes.find((x) => x.id === id); if (!p) return; const r = size(); st.tx = r.w / 2 - (p.x + p.w / 2) * st.scale; st.ty = r.h / 2 - (p.y + p.h / 2) * st.scale; apply(); };
    const zoomAt = (f, mx, my) => { const r = size(); mx = mx == null ? r.w / 2 : mx; my = my == null ? r.h / 2 : my; const ns = clamp(st.scale * f, 0.2, 2.5); st.tx = mx - (mx - st.tx) * (ns / st.scale); st.ty = my - (my - st.ty) * (ns / st.scale); st.scale = ns; apply(); };
    const nodes = () => (st.L ? st.L.nodes : []);
    const select = (id, center) => {
      st.sel = id;
      for (const [k, g] of st.groups) { g.classList.toggle('sel', k === id); g.setAttribute('aria-pressed', String(k === id)); }
      drawPanel();
      if (center) centerOn(id);
    };
    const step = (d) => { const list = nodes(); if (!list.length) return; const i = list.findIndex((x) => x.id === st.sel); select(list[clamp((i < 0 ? list.length - 1 : i) + d, 0, list.length - 1)].id, true); };

    function drawPanel() {
      const list = nodes(); const p = list.find((x) => x.id === st.sel);
      if (!p) { panel.replaceChildren(h('div', { class: 'empty' }, list.length ? 'Pick a state on the graph to see what it did.' : 'No steps recorded yet.')); return; }
      const n = p.node; const open = G.isOpen(n); const now = Date.now();
      const ms = Math.max(0, (open ? now : new Date(n.end).getTime()) - new Date(n.start).getTime());
      const k = list.indexOf(p); const prev = list[k - 1]; const next = list[k + 1];
      const sum = G.actSummary(n); const acts = n.acts || [];
      const t0 = new Date(n.start).getTime();
      const kv = (a1, v) => (v ? h('div', { class: 'gkv' }, h('span', { class: 'faint' }, a1), h('span', {}, v)) : null);
      const nb = (q, label) => (q ? h('button', { type: 'button', class: 'gw-nb', onclick: () => select(q.id, true) }, h('span', { class: 'gf-node ph-' + q.node.phase }), h('span', {}, label + ': ' + q.label)) : null);
      panel.replaceChildren(
        h('div', { class: 'gw-ph' }, h('span', { class: 'pill ph-' + n.phase }, G.PHASE_LABEL[n.phase] || n.phase), open ? h('span', { class: 'chip' }, 'happening now') : null, p.suspected ? h('span', { class: 'chip warn' }, 'suspected, only a hint') : null),
        h('h3', { class: 'gw-h' }, n.phase === 'tool' && n.tool ? n.tool : p.label),
        h('div', { class: 'gw-kvs' },
          kv('How it began', G.reasonLabel(n.cause)),
          kv('Time', G.dur(ms) + (open ? ' so far' : '')),
          kv('Started', n.start ? new Date(n.start).toLocaleTimeString() : ''),
          n.count > 1 ? kv('Repeated', n.count + ' times in a row') : null,
          n.cost ? kv('Cost', '$' + n.cost.toFixed(2) + ' at list price') : null,
          n.tokens ? kv('Tokens', fmtTokens(n.tokens)) : null,
          n.phase === 'needs-you' ? kv('Asked', n.summary || n.tool) : (n.phase === 'tool' ? kv('Call', n.summary) : null),
          n.phase === 'needs-you' ? kv('You answered', ANSWER_WORDS[n.answer] || (open ? 'Not answered yet' : n.answer || 'No answer recorded')) : null,
          (n.tags || []).filter((t) => !t.startsWith('tool:')).length ? kv('Tags', (n.tags || []).filter((t) => !t.startsWith('tool:')).join(', ')) : null,
          n.ended === 'restart' ? kv('Ended', 'The server restarted while this was going') : null),
        sum.sentence ? h('p', { class: 'gw-sum' }, sum.sentence + '.') : null,
        acts.length ? h('div', { class: 'gw-sec' }, h('b', {}, 'What it did, and how'), h('span', { class: 'faint small' }, ' ' + (sum.total > sum.shown ? 'latest ' + sum.shown + ' of ' + sum.total : String(sum.total))),
          h('ol', { class: 'gw-acts' }, ...acts.map((x) => { const off = x.t ? Math.max(0, Math.round((new Date(x.t).getTime() - t0) / 1000)) : null; return h('li', { class: 'gw-act k-' + x.k + (x.ok === 'fail' ? ' bad' : '') },
            h('span', { class: 'gf-glyph', 'aria-hidden': 'true' }, ACT_GLYPH[x.k] || '-'),
            h('div', { class: 'gw-actb' },
              h('div', {}, h('span', { class: 'gw-actk' }, ACT_WORD[x.k] || ''), off != null ? h('span', { class: 'faint small' }, ' +' + (off < 60 ? off + ' s' : Math.floor(off / 60) + ' min ' + (off % 60) + ' s')) : null),
              h('div', { class: x.k === 'tool' ? 'gw-mono' : '' }, x.l),
              x.k === 'tool' ? h('div', { class: 'gw-res ' + (x.ok === 'fail' ? 'bad' : x.ok === 'ok' ? 'ok' : '') }, x.ok === 'fail' ? 'Failed' + (x.r ? ': ' + x.r : '') : x.ok === 'ok' ? 'Finished' + (x.r ? ': ' + x.r : '') : 'No result seen yet') : null)); }))) : h('p', { class: 'faint small' }, 'Nothing was recorded inside this state. Cully sees only the events the harness sends, so a quiet stretch can still hide work.'),
        n.changed ? h('div', { class: 'gw-sec' }, h('b', {}, 'Files'), h('p', { class: 'gf-line' }, n.changed), h('button', { type: 'button', class: 'btn ghost small', onclick: () => { closeGraphWindow(); setTab('changes'); } }, 'Open the Changes tab')) : null,
        n.phase === 'stuck' ? h('p', { class: 'faint small' }, 'Cully thinks the task was going in circles. That is a guess from repeated calls.') : null,
        n.phase === 'failed' ? h('p', { class: 'faint small' }, 'The task failed here. From outside, Cully cannot tell what caused it.') : null,
        n.phase === 'earlier' ? h('p', { class: 'faint small' }, 'Older plain steps were folded into this one to keep the graph short.') : null,
        h('div', { class: 'gw-sec gw-nbs' }, nb(prev, 'Came from'), nb(next, 'Led to')));
    }

    function draw() {
      const view = st.view; const now = Date.now();
      const all = (view && view.nodes) || [];
      headline.textContent = G.headline(view, now);
      liveTag.hidden = !['preparing', 'working', 'waiting'].includes((S.agents.find((x) => x.id === a.id) || a).state);
      if (!all.length) { st.L = null; canvas.replaceChildren(h('div', { class: 'empty' }, 'No steps recorded yet. They appear here as the task works.')); drawPanel(); return; }
      const L = G.layout(view, { nodeW: 168, nodeH: 54, gapX: 56, laneH: 112, labelW: 132, pad: 24, loopLift: 36, collapsed: new Set() });
      st.L = L; st.groups = new Map();
      const svg = sv('svg', { class: 'gw-svg', width: '100%', height: '100%' });
      svg.append(sv('defs', {}, sv('marker', { id: 'gw-arrow', viewBox: '0 0 10 10', refX: '9', refY: '5', markerWidth: '7', markerHeight: '7', orient: 'auto-start-reverse' }, sv('path', { d: 'M0 0 L10 5 L0 10 z', class: 'gw-arrowhead' }))));
      vp = sv('g', { class: 'gw-vp' });
      svg.append(vp);
      for (const r of L.rows) {
        vp.append(sv('rect', { class: 'gw-lanebg' + (r.helper ? ' sub' : ''), x: 0, y: r.y, width: L.width, height: L.opts.laneH, rx: 10 }));
        vp.append(sv('text', { class: 'gw-lanel', x: 14, y: r.y + 24 }, uiClipText(r.label, 16) + (r.agent ? ', ' + r.agent : '')));
      }
      const byId = new Map(L.nodes.map((q) => [q.id, q]));
      for (const e of L.edges) {
        const q1 = byId.get(e.from); const q2 = byId.get(e.to);
        vp.append(sv('path', { class: 'gw-edge' + (e.cross ? ' cross' : ''), d: e.d, 'marker-end': 'url(#gw-arrow)' }, sv('title', {}, e.reasonLabel || e.reason)));
        if (q1 && q2 && e.reasonLabel) vp.append(sv('text', { class: 'gw-edgel', x: (q1.x + q1.w + q2.x) / 2, y: (q1.y + q2.y + q1.h) / 2 - 8, 'text-anchor': 'middle' }, uiClipText(e.reasonLabel, 24)));
      }
      for (const b of L.back) vp.append(sv('g', { class: 'gw-back' }, sv('path', { d: b.d, 'marker-end': 'url(#gw-arrow)' }, sv('title', {}, 'The same two steps repeated ' + b.count + ' times')), sv('text', { x: b.lx, y: b.ly, 'text-anchor': 'middle' }, b.label)));
      if (!st.sel || !byId.has(st.sel)) st.sel = L.nodes[L.nodes.length - 1].id;
      for (const q of L.nodes) {
        const n = q.node; const open = G.isOpen(n); const needs = n.phase === 'needs-you';
        const ms = Math.max(0, (open ? now : new Date(n.end).getTime()) - new Date(n.start).getTime());
        const sm = G.actSummary(n);
        const sub = G.dur(ms) + (sm.tools ? ', ' + sm.tools + (sm.tools === 1 ? ' tool' : ' tools') : '') + (n.count > 1 && !sm.tools ? ', x' + n.count : '');
        const g = sv('g', { class: 'gw-node ph-' + n.phase + (open ? ' open' : '') + (q.suspected ? ' suspected' : '') + (q.id === st.sel ? ' sel' : ''), tabindex: '0', role: 'button', 'aria-pressed': String(q.id === st.sel), 'aria-label': q.label + ', ' + sub,
          onclick: () => { if (!st.moved) select(q.id, false); }, onkeydown: (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); select(q.id, false); } } },
        sv('title', {}, q.label + (n.summary && n.phase !== 'earlier' ? ': ' + n.summary : '')),
        needs ? sv('rect', { class: 'gw-ring', x: q.x - 4, y: q.y - 4, width: q.w + 8, height: q.h + 8, rx: 14 }) : null,
        sv('rect', { class: 'gw-body', x: q.x, y: q.y, width: q.w, height: q.h, rx: 11 }),
        sv('rect', { class: 'gw-stripe', x: q.x, y: q.y, width: 6, height: q.h, rx: 3 }),
        sv('text', { class: 'gw-nt', x: q.x + 16, y: q.y + 22 }, uiClipText(q.label, 20)),
        sv('text', { class: 'gw-ns', x: q.x + 16, y: q.y + 41 }, uiClipText(sub, 24)));
        st.groups.set(q.id, g);
        vp.append(g);
      }
      canvas.replaceChildren(svg);
      stripBox.replaceChildren(...G.strip(view, now).map((sg) => { const el = h('button', { type: 'button', class: 'gf-seg ph-' + sg.phase, title: (G.PHASE_LABEL[sg.phase] || sg.phase) + ', ' + G.dur(sg.ms), 'aria-label': 'Go to ' + (G.PHASE_LABEL[sg.phase] || sg.phase), onclick: () => { if (st.groups.has(sg.id)) select(sg.id, true); } }); el.style.flexGrow = String(sg.grow); return el; }));
      if (!st.fitted) { st.fitted = true; if (L.nodes.length > 6) { st.scale = 1; centerOn(st.sel); } else fit(); } else apply();
      drawPanel();
    }

    // drag to move, wheel to zoom
    let drag = null;
    canvas.addEventListener('pointerdown', (e) => { if (e.button !== 0) return; drag = { x: e.clientX, y: e.clientY, tx: st.tx, ty: st.ty }; st.moved = false; canvas.setPointerCapture(e.pointerId); canvas.classList.add('grab'); });
    canvas.addEventListener('pointermove', (e) => { if (!drag) return; const dx = e.clientX - drag.x; const dy = e.clientY - drag.y; if (Math.abs(dx) + Math.abs(dy) > 4) st.moved = true; st.tx = drag.tx + dx; st.ty = drag.ty + dy; apply(); });
    const endDrag = () => { drag = null; canvas.classList.remove('grab'); setTimeout(() => { st.moved = false; }, 0); };
    canvas.addEventListener('pointerup', endDrag); canvas.addEventListener('pointercancel', endDrag);
    canvas.addEventListener('wheel', (e) => { e.preventDefault(); if (e.shiftKey || Math.abs(e.deltaX) > Math.abs(e.deltaY)) { st.tx -= (e.deltaX || e.deltaY); apply(); return; } const r = canvas.getBoundingClientRect(); zoomAt(e.deltaY < 0 ? 1.12 : 1 / 1.12, e.clientX - r.left, e.clientY - r.top); }, { passive: false });
    canvas.addEventListener('keydown', (e) => {
      if (e.key === 'ArrowRight' || e.key === 'ArrowDown') { e.preventDefault(); step(1); }
      else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') { e.preventDefault(); step(-1); }
      else if (e.key === '+' || e.key === '=') zoomAt(1.2); else if (e.key === '-') zoomAt(1 / 1.2); else if (e.key === '0') fit();
    });

    const zbtn = (label, aria, fn) => h('button', { type: 'button', class: 'btn ghost small', 'aria-label': aria, title: aria, onclick: fn }, label);
    const modal = h('div', { class: 'modal gw-modal', role: 'dialog', 'aria-modal': 'true', 'aria-label': 'Task graph' },
      h('header', { class: 'gw-top' }, h('h2', {}, 'What this task did'), headline, liveTag, h('span', { class: 'sp' }),
        zbtn('-', 'Zoom out', () => zoomAt(1 / 1.2)), zbtn('+', 'Zoom in', () => zoomAt(1.2)), zbtn('Fit', 'Fit the whole graph', fit),
        zbtn('Latest', 'Go to the latest state', () => { const l = nodes(); if (l.length) { st.scale = 1; select(l[l.length - 1].id, true); } }),
        h('button', { class: 'btn ghost small', onclick: closeGraphWindow }, 'Close')),
      h('div', { class: 'gw-body-wrap' }, h('div', { class: 'gw-main' }, canvas, hbar, stripBox, h('p', { class: 'faint small gw-note' }, 'Observed by Cully, not a plan it follows. It cannot see inside the harness, so steps between tool calls are inferred.')), panel));
    const bg = h('div', { class: 'modal-bg', onclick: (e) => { if (e.target === bg) closeGraphWindow(); } }, modal);
    $('modal-root').replaceChildren(bg);
    S.modal = { agent: 'graphwin' };
    document.removeEventListener('keydown', escClose);
    document.addEventListener('keydown', escClose);
    function closeGraphWindow() { closeMCP(); }
    const load = async () => {
      try { st.view = await api('/api/agents/' + a.id + '/graph'); } catch (_) { if (!st.view) canvas.replaceChildren(h('div', { class: 'empty' }, 'The graph is not available right now.')); return; }
      if (!document.body.contains(canvas)) return;
      draw();
    };
    load().then(() => canvas.focus({ preventScroll: true }));
    const timer = setInterval(() => {
      if (!document.body.contains(canvas)) { clearInterval(timer); return; }
      const cur = S.agents.find((x) => x.id === a.id);
      if (cur && ['preparing', 'working', 'waiting'].includes(cur.state)) load();
    }, 3000);
  }
  async function drawGraph(a) {
    const body = $('sidebody'); if (!body || sideKindNow() !== 'graph') return;
    let view;
    try { view = await api('/api/agents/' + a.id + '/graph'); } catch (_) { if (!body.querySelector('.gwrap')) body.replaceChildren(h('div', { class: 'empty' }, 'The graph is not available right now.')); return; }
    if (sideKindNow() !== 'graph' || !S.route || S.route.id !== a.id) return;
    renderGraph(body, a, view);
  }
  setInterval(() => {
    if (!S.route || S.route.name !== 'agent' || sideKindNow() !== 'graph') return;
    const a = S.agents.find((x) => x.id === S.route.id);
    if (a && ['preparing', 'working', 'waiting'].includes(a.state)) drawGraph(a);
  }, 3000);
  async function loadSide(a, force) {
    const body = $('sidebody'); const head = $('sidehead');
    if (!body) return;
    const kind = sideKindNow();
    if (kind === 'graph') {
      S.diffKey = '';
      head.replaceChildren(h('span', {}, 'What this task did, step by step. Observed by Cully, not a plan it follows'));
      drawGraph(a);
      return;
    }
    if (kind === 'calls') {
      S.diffKey = '';
      head.replaceChildren(h('span', {}, 'Every tool call this task has made, newest last'));
      drawCalls(a);
      return;
    }
    const key = a.id + kind + (a.events || 0) + a.state;
    if (!force && key === S.diffKey) return;
    S.diffKey = key;
    const refreshBtn = h('button', { class: 'btn ghost small sp', onclick: () => loadSide(a, true) }, icon('refresh'), 'Refresh');
    head.replaceChildren(h('span', {}, kind === 'changes' ? 'Full change against the commit this task started from' : 'Where this task left off'), refreshBtn);
    try {
      if (kind === 'handoff') {
        const sum = await api('/api/agents/' + a.id + '/summary');
        const tsec = await taskSessionsSection(a);
        body.replaceChildren(h('div', { class: 'toolbar slim' }, h('div', { class: 'actions' }, copyBtn('Copy for another agent', () => api('/api/agents/' + a.id + '/handoff')))), tsec, renderSummary(sum, { agent: a }));
        return;
      }
      const cpNode = await checkpointSection(a);
      const d = parseDiff(await api('/api/agents/' + a.id + '/diff'));
      if (!d.files.length && !d.head) { body.replaceChildren(cpNode, h('div', { class: 'empty' }, 'No changes yet.')); return; }
      const nodes = [];
      const notes = (S.notes[a.id] = S.notes[a.id] || []);
      const bar = h('div', { class: 'notebar', hidden: !notes.length });
      const drawBar = () => {
        bar.hidden = !notes.length;
        bar.replaceChildren(h('span', {}, notes.length + (notes.length === 1 ? ' comment' : ' comments') + ' ready for the task'),
          h('button', { class: 'btn small', onclick: async () => {
            const text = 'Please address these review comments on your change:\n' + notes.map((n, i) => (i + 1) + '. ' + n.file + ' — "' + n.line + '": ' + n.text).join('\n');
            try { await api('/api/agents/' + a.id + '/message', { method: 'POST', body: JSON.stringify({ text }) }); notes.length = 0; drawBar(); toast('Comments sent to ' + a.title); S.tab = 'chat'; mountAgent(a.id); } catch (x) { toast(x.message, true); }
          } }, icon('send'), 'Send to task'),
          h('button', { class: 'btn ghost small', onclick: () => { notes.length = 0; drawBar(); } }, 'Clear'));
      };
      drawBar();
      nodes.push(bar);
      if (d.head && !d.files.length) nodes.push(h('pre', { class: 'stat' }, d.head));
      d.files.forEach((f, i) => nodes.push(h('details', { class: 'file', ...(i < 4 ? { open: true } : {}) },
        h('summary', {}, h('span', { class: 'nm' }, f.name), h('span', { class: 'plus' }, '+' + f.add), h('span', { class: 'minus' }, '−' + f.del)),
        h('div', {}, ...f.lines.slice(0, 3000).map((l) => {
          const cls = l.startsWith('+') && !l.startsWith('+++') ? 'add' : l.startsWith('-') && !l.startsWith('---') ? 'del' : l.startsWith('@@') ? 'hunk' : /^(index|new file|deleted file|---|\+\+\+|similarity|rename)/.test(l) ? 'meta' : '';
          const line = h('span', { class: 'dl ' + cls + (cls === 'add' || cls === 'del' || cls === '' ? ' commentable' : ''), title: cls === 'meta' || cls === 'hunk' ? '' : 'Click to comment on this line' }, l || ' ');
          if (cls === 'add' || cls === 'del' || cls === '') {
            line.addEventListener('click', () => {
              if (line.nextSibling && line.nextSibling.classList && line.nextSibling.classList.contains('notebox')) { line.nextSibling.remove(); return; }
              const ta = h('textarea', { 'aria-label': 'Comment', placeholder: 'What should change here?' });
              const box = h('div', { class: 'notebox' }, ta, h('div', { class: 'row' },
                h('button', { class: 'btn small', onclick: () => { if (!ta.value.trim()) return; notes.push({ file: f.name, line: l.replace(/^[+\- ]/, '').trim().slice(0, 120), text: ta.value.trim() }); box.remove(); line.classList.add('noted'); drawBar(); } }, 'Add comment'),
                h('button', { class: 'btn ghost small', onclick: () => box.remove() }, 'Cancel')));
              line.after(box); ta.focus();
            });
          }
          return line;
        })))));
      body.replaceChildren(cpNode, ...nodes);
    } catch (e) { body.replaceChildren(h('div', { class: 'err' }, e.message)); }
  }

  // ---------- checkpoints ----------
  // Cully's own snapshots of the task's files, one per turn. Newest first.
  async function checkpointSection(a) {
    let v;
    try { v = await api('/api/agents/' + a.id + '/checkpoints'); } catch (e) { return h('section', { class: 'cpsec' }, h('div', { class: 'err' }, e.message)); }
    const sec = h('section', { class: 'cpsec' }, h('h3', {}, 'Checkpoints'));
    if (!v.available) { sec.append(h('p', { class: 'faint small' }, v.reason || 'Checkpoints are not available for this task.')); return sec; }
    if (v.shadow) sec.append(h('p', { class: 'faint small' }, 'This folder has no git, so Cully keeps its snapshots in a private repository inside its own folder. Nothing is added to your folder.'));
    if (!v.checkpoints.length) { sec.append(h('p', { class: 'faint small' }, 'No checkpoints yet. One is taken before the first turn and after every turn that changes files.')); return sec; }
    const when = (t) => new Date(t).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    const list = h('div', { class: 'cptl' });
    v.checkpoints.slice().reverse().forEach((c) => {
      const what = c.safety ? 'Safety checkpoint' : c.turn === 0 ? 'Start' : 'Turn ' + c.turn;
      const head = what + ', ' + when(c.time) + ', ' + c.total + (c.total === 1 ? ' file' : ' files') + (c.label && !c.safety ? ', ' + c.label : '');
      const diffBox = h('pre', { class: 'cpdiff', hidden: true });
      const revert = h('button', { class: 'btn small', disabled: !v.canRevert, title: v.canRevert ? 'Put the task files back as they were at this checkpoint' : v.revertBlock, onclick: () => confirmDialog({
        title: 'Revert files to ' + what.toLowerCase(),
        body: ['Files in this task change back to how they were at ' + when(c.time) + '. Edits made after that are lost from the folder, and files created after that are deleted. The branch, your commits and ignored files are not touched.',
          'Cully takes a safety checkpoint of the current state first, so you can undo this revert from the list.'],
        confirmLabel: 'Revert files',
        onConfirm: async () => { const r = await api('/api/agents/' + a.id + '/checkpoints/' + c.n + '/revert', { method: 'POST' }); toast('Reverted: ' + r.restored + ' restored, ' + r.removed + ' removed.'); loadSide(a, true); },
      }) }, 'Revert files to here');
      const row = h('details', { class: 'cprow' },
        h('summary', {}, h('span', { class: 'cpdot' }), head),
        h('ul', { class: 'files' }, ...(c.files || []).map((f) => h('li', { class: 'mono' }, f.replace('\t', ' '))), c.total > (c.files || []).length ? h('li', {}, 'and ' + (c.total - c.files.length) + ' more') : null),
        diffBox, revert);
      row.addEventListener('toggle', async () => {
        if (!row.open || diffBox.dataset.loaded) return;
        diffBox.dataset.loaded = '1';
        try { diffBox.textContent = await api('/api/agents/' + a.id + '/checkpoints/' + c.n + '/diff'); diffBox.hidden = false; } catch (e) { diffBox.textContent = e.message; diffBox.hidden = false; }
      });
      list.append(row);
    });
    sec.append(list);
    if (!v.canRevert) sec.append(h('p', { class: 'faint small' }, v.revertBlock));
    return sec;
  }

  // ---------- board ----------
  const BOARD_KEY = 'cully-board';
  function boardPrefs() { try { return { q: '', repo: '', group: false, ...JSON.parse(localStorage.getItem(BOARD_KEY) || '{}') }; } catch (_) { return { q: '', repo: '', group: false }; } }
  function saveBoardPrefs(p) { try { localStorage.setItem(BOARD_KEY, JSON.stringify(p)); } catch (_) { /* storage blocked */ } }
  function elapsed(iso) {
    const sec = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000));
    if (sec < 60) return sec + 's';
    if (sec < 3600) return Math.floor(sec / 60) + 'm';
    return Math.floor(sec / 3600) + 'h ' + Math.floor((sec % 3600) / 60) + 'm';
  }
  function boardCard(a, overlaps) {
    const live = a.state === 'preparing' || a.state === 'working' || a.state === 'waiting';
    const clash = overlaps.filter((o) => o.agents.includes(a.id));
    const others = new Set();
    for (const o of clash) for (const id of o.agents) if (id !== a.id) others.add(id);
    const advice = openAdviceFor(a.id);
    const files = a.files || [];
    const card = (() => {
    const act = (label, ic, fn, cls) => h('button', { class: 'btn small ' + (cls || 'ghost'), onclick: (e) => { e.preventDefault(); e.stopPropagation(); fn(); } }, ic ? icon(ic) : null, label);
    const actions = [];
    if (a.state === 'waiting') actions.push(act('Reply', 'send', () => { location.hash = '#/task/' + a.id; }, ''));
    if (files.length) actions.push(act('Review changes', 'eye', () => { S.tab = 'changes'; location.hash = '#/task/' + a.id; }));
    if (!live) actions.push(act('Copy handoff', 'copy', async () => { try { await writeClip(await api('/api/agents/' + a.id + '/handoff')); toast('Handoff copied. Paste it into the session you are continuing.'); } catch (x) { toast(x.message, true); } }));
    if (live) actions.push(act('Stop', 'stop', () => api('/api/agents/' + a.id + '/stop', { method: 'POST' }).then(refresh).catch((x) => toast(x.message, true))));
    return h('a', { class: 'card bcard' + (a.state === 'waiting' ? ' attn' : ''), href: '#/task/' + a.id },
      h('div', { class: 'r1' }, dot(a), h('span', { class: 't' }, a.title), h('span', { class: 'sp faint small' }, live ? elapsed(a.created) : ago(a.updated || a.created))),
      h('div', { class: 'last' }, a.last || (a.state === 'preparing' ? 'Gathering context…' : 'No activity yet')),
      h('div', { class: 'row' },
        h('span', { class: 'chip mono', title: 'Branch' }, icon('branch'), a.branch.replace(/^cully\//, '')),
        a.option ? h('span', { class: 'chip' }, 'Option ' + a.option) : null,
        a.token ? h('span', { class: 'chip' }, 'token ' + a.token) : null,
        queuedChip(a),
        files.length ? h('span', { class: 'chip', title: files.slice(0, 8).join('\n') }, files.length + (files.length === 1 ? ' file changed' : ' files changed')) : null,
        a.stuck ? h('span', { class: 'chip warn', title: a.stuck.detail }, icon('alert'), 'Stuck: ' + a.stuck.label.toLowerCase()) : null,
        a.stopReason ? h('span', { class: 'chip warn' }, icon('stop'), a.stopReason === 'cost_limit' ? 'Stopped at the cost limit' : 'Stopped at the turn limit') : null,
        advice ? h('span', { class: 'chip warn' }, icon('spark'), advice + (advice === 1 ? ' piece of advice' : ' pieces of advice')) : null,
        clash.length ? h('span', { class: 'chip warn', title: clash.map((o) => o.file).join('\n') }, icon('alert'), 'Clashes with ' + joinNames([...others].map((id) => (S.agents.find((x) => x.id === id) || {}).title || id))) : null),
      actions.length ? h('div', { class: 'row bactions' }, ...actions) : null);
    })();
    card.draggable = true;
    card.addEventListener('dragstart', (e) => { e.dataTransfer.setData('text/plain', 'agent:' + a.id); e.dataTransfer.effectAllowed = 'move'; });
    return card;
  }
  function queueCard(q) {
    const card = h('div', { class: 'card bcard queued', draggable: 'true' },
      h('div', { class: 'r1' }, h('span', { class: 'dot queued ph-queued' }), h('span', { class: 't' }, q.title), h('span', { class: 'sp faint small' }, ago(q.added))),
      h('div', { class: 'last' }, q.error ? 'Cannot start: ' + q.error : q.prompt),
      q.option ? h('div', { class: 'row' }, h('span', { class: 'chip' }, 'Option ' + q.option)) : null,
      h('div', { class: 'row bactions' },
        h('button', { class: 'btn small', onclick: () => startQueued(q.id) }, icon('send'), 'Start now'),
        h('button', { class: 'btn ghost small', onclick: () => api('/api/queue/' + q.id, { method: 'DELETE' }).then(renderBoard).catch((x) => toast(x.message, true)) }, icon('x'), 'Remove')));
    card.addEventListener('dragstart', (e) => { e.dataTransfer.setData('text/plain', 'queue:' + q.id); e.dataTransfer.effectAllowed = 'move'; });
    return card;
  }
  async function startQueued(id) {
    try { await api('/api/queue/' + id + '/start', { method: 'POST' }); await refresh(); renderBoard(); } catch (e) { toast(e.message, true); }
  }
  // Drop a card on a column: a waiting task on Starting or Working starts now; a running agent on Finished stops.
  function dropOn(key, e) {
    e.preventDefault();
    const [kind, id] = (e.dataTransfer.getData('text/plain') || '').split(':');
    if (kind === 'queue' && (key === 'preparing' || key === 'working')) return startQueued(id);
    if (kind === 'queue' && key === 'queued') return;
    if (kind === 'agent' && key === 'finished') {
      const a = S.agents.find((x) => x.id === id);
      if (a && ['preparing', 'working', 'waiting'].includes(a.state) && window.confirm('Stop "' + a.title + '"? Its work so far stays on its branch.')) {
        return api('/api/agents/' + id + '/stop', { method: 'POST' }).then(refresh).then(renderBoard).catch((x) => toast(x.message, true));
      }
      return;
    }
    toast(kind === 'queue' ? 'Drop a waiting task on Starting or Working to start it.' : 'Drop a running task on Finished to stop it.');
  }
  function openCompare(group) {
    const opts = S.agents.filter((a) => a.group === group).sort((x, y) => (x.option || '').localeCompare(y.option || ''));
    const live = (a) => ['preparing', 'working', 'waiting'].includes(a.state);
    const cols = opts.map((a) => h('section', { class: 'cmp' },
      h('h3', {}, 'Option ' + a.option, h('span', { class: 'pill ' + phaseCls(a) }, stateLabel(a))),
      h('p', { class: 'last' }, a.last || 'No activity yet'),
      h('div', { class: 'row' }, h('span', { class: 'chip' }, (a.files || []).length + ' files changed')),
      h('ul', { class: 'files' }, ...(a.files || []).slice(0, 12).map((f) => h('li', {}, f))),
      h('div', { class: 'row' },
        h('a', { class: 'btn ghost small', href: '#/task/' + a.id, onclick: () => { S.tab = 'changes'; closeMCP(); } }, icon('eye'), 'Open'),
        h('button', { class: 'btn small', title: 'Stop the other options and keep this one', onclick: async () => {
          try { for (const o of opts) if (o.id !== a.id && live(o)) await api('/api/agents/' + o.id + '/stop', { method: 'POST' }); toast('Kept option ' + a.option + '. The others are stopped.'); closeMCP(); await refresh(); renderBoard(); } catch (x) { toast(x.message, true); }
        } }, icon('check'), 'Keep this one')))); 
    const modal = h('div', { class: 'modal wide', role: 'dialog', 'aria-modal': 'true', 'aria-label': 'Compare options' },
      h('header', {}, h('h2', {}, 'Compare options'), h('button', { class: 'btn ghost small sp', onclick: closeMCP }, 'Close')),
      h('div', { class: 'mbody cmpgrid' }, ...cols));
    const bg = h('div', { class: 'modal-bg', onclick: (e) => { if (e.target === bg) closeMCP(); } }, modal);
    $('modal-root').replaceChildren(bg);
    S.modal = { agent: 'compare' };
    document.removeEventListener('keydown', escClose);
    document.addEventListener('keydown', escClose);
  }
  // ---------- board ----------
  // The board answers two questions: what needs me, and what is Cully doing for me right now.
  // It redraws only when something it shows has changed, so it never flickers.
  function drawRunning(r) {
    const box = $('running');
    if (!box) return;
    const sig = JSON.stringify(r);
    if (box.dataset.sig === sig && box.childElementCount) return;
    box.dataset.sig = sig;
    if (!r) { box.replaceChildren(); return; }
    const work = [];
    const line = (ic, title, text, extra, href, label) => h('li', { class: 'bg-row' }, h('span', { class: 'bg-ic' }, icon(ic)), h('div', { class: 'bg-main' }, h('b', {}, title), h('span', { class: 'faint' }, text), extra),
      href ? h('a', { class: 'btn ghost small', href }, label) : null);
    for (const a of r.agents) work.push(line('spark', a.name, (a.scope === 'global' ? 'An agent for all your projects is' : 'An agent is') + ' doing its work now, for ' + ago(a.started).replace(' ago', ''), null, a.taskId ? '#/task/' + a.taskId : null, 'Open the task'));
    for (const e of r.evals) work.push(line('check', 'Eval running', e.task + (e.status === 'judging' ? ' (being judged)' : ''), null, e.agentId ? '#/task/' + e.agentId : '#/dashboard/evals', 'Open'));
    for (const c of r.scouts) work.push(line('search', 'Scout searching the web', c.query + ', ' + c.steps + (c.steps === 1 ? ' step' : ' steps') + ' so far', null, '#/dashboard/scout', 'Watch it'));
    for (const a of r.advisor) work.push(line('spark', 'Advisor reviewing', a.title, null, '#/dashboard/advisor', 'See advice'));
    if (r.naming.on && r.naming.pending > 0) {
      const total = r.naming.done + r.naming.pending; const bar = h('span', { class: 'fbar' }, fill(Math.round(r.naming.done * 100 / total)));
      work.push(line('pencil', 'Analysing your past sessions', r.naming.done + ' of ' + total + ' done, a few every hour', bar, '#/dashboard/library', 'Session library'));
    }
    const live = r.sessions.filter((x) => !x.detached); const idle = r.sessions.filter((x) => x.detached);
    const sessRow = (x) => h('tr', {}, h('td', {}, harnessChip(x.agent)), h('td', {}, h('b', {}, x.name)), h('td', { class: 'mono-s faint' }, x.branch || ''), h('td', { class: 'faint' }, x.via === 'cully' ? 'Cully terminal' : 'Terminal'),
      h('td', { class: 'faint' }, x.started && !x.started.startsWith('0001') ? ago(x.started).replace(' ago', '') : ''), h('td', {}, x.snapshot && x.snapshot.context ? h('span', { class: 'chip ctx ' + (x.snapshot.context.near ? 'warn' : '') }, x.snapshot.context.percent + '%') : h('span', { class: 'faint small' }, x.agent === 'cursor' ? 'not recorded' : '')));
    const table = (rows) => h('table', { class: 'bg-table' }, h('thead', {}, h('tr', {}, ...['Harness', 'Folder', 'Branch', 'Where', 'Open for', 'Context'].map((c) => h('th', {}, c)))), h('tbody', {}, ...rows.map(sessRow)));
    box.replaceChildren(
      h('section', { class: 'bsec' }, h('div', { class: 'bsec-h' }, h('h3', {}, 'Background work'), h('span', { class: 'faint small' }, 'Things Cully is doing for you that are not tasks')),
        work.length ? h('ul', { class: 'bg-list' }, ...work) : h('p', { class: 'faint small' }, 'Nothing in the background right now.')),
      h('section', { class: 'bsec' }, h('div', { class: 'bsec-h' }, h('h3', {}, 'Open in your terminals'), h('span', { class: 'faint small' }, 'Sessions of Claude, Codex and Cursor running on this computer')),
        live.length ? table(live) : h('p', { class: 'faint small' }, 'No session is open in a terminal right now.'),
        idle.length ? h('details', { class: 'bg-idle' }, h('summary', {}, idle.length + (idle.length === 1 ? ' idle process' : ' idle processes') + ' whose terminal window was closed'), h('p', { class: 'faint small' }, 'These are still running in the background, but nobody can type into them any more. They are not shown above.'), table(idle)) : null));
  }
  async function refreshRunning() { try { drawRunning(await api('/api/running')); } catch (_) { /* optional */ } }

  async function renderBoard() {
    try { S.overlaps = (await api('/api/board')).overlaps || []; } catch (_) { /* banner */ }
    let queue = [];
    try { queue = await api('/api/queue'); } catch (_) { /* optional */ }
    const prefs = boardPrefs();
    const sigOf = JSON.stringify([S.agents.map((a) => [a.id, a.state, (a.queue || []).length, a.stuck && a.stuck.kind, a.stopReason, a.last, (a.files || []).length, a.updated, a.context && a.context.percent, a.group, a.option, a.usage && a.usage.tokens, a.harness && a.harness.status]), queue.map((x) => x.id), S.overlaps, prefs, S.finishedAll]);
    const box = $('board');
    if (S.boardSig === sigOf && box.childElementCount) { refreshRunning(); return; }
    S.boardSig = sigOf;
    const repoName = (p) => p.split('/').filter(Boolean).pop() || p;
    const repos = [...new Set(S.agents.map((a) => a.repo))];
    const bucket = (a) => (a.state === 'preparing' || a.state === 'working' ? 'working' : a.state === 'waiting' ? 'waiting' : 'finished');
    const q = prefs.q.trim().toLowerCase();
    const shown = S.agents.filter((a) => (!prefs.repo || a.repo === prefs.repo) && (!q || (a.title + ' ' + a.branch + ' ' + (a.files || []).join(' ')).toLowerCase().includes(q)));
    const title = (id) => (S.agents.find((a) => a.id === id) || {}).title || id;
    const COLS = [['queued', 'Up next', 'Waits for a free slot'], ['working', 'Working', 'Busy right now'], ['waiting', 'Needs you', 'Waiting for your reply'], ['finished', 'Finished', 'Review it, then carry on']];
    const listOf = (k) => (k === 'queued' ? queue : shown.filter((a) => bucket(a) === k));
    const tiles = h('div', { class: 'btiles' }, ...COLS.map(([k, l]) => h('div', { class: 'btile ' + k }, h('b', {}, String(listOf(k).length)), h('span', {}, l))));
    const search = h('input', { type: 'search', class: 'bsearch', placeholder: 'Search tasks, branches or files', 'aria-label': 'Search the board', value: prefs.q, autocomplete: 'off' });
    search.addEventListener('input', () => { saveBoardPrefs({ ...boardPrefs(), q: search.value }); renderBoard().then(() => { const el = document.querySelector('.bsearch'); if (el) { el.focus(); el.setSelectionRange(el.value.length, el.value.length); } }); });
    const sel = repos.length > 1 ? h('select', { 'aria-label': 'Folder', onchange: (e) => { saveBoardPrefs({ ...boardPrefs(), repo: e.target.value }); renderBoard(); } },
      h('option', { value: '' }, 'All folders'), ...repos.map((r) => h('option', Object.assign({ value: r }, r === prefs.repo ? { selected: true } : {}), repoName(r)))) : null;
    const head = h('div', { class: 'bhead2' }, h('div', {}, h('h2', {}, 'Board'), h('p', { class: 'faint' }, 'Drag a task from Up next to Working to start it, and a running one to Finished to stop it. Below: what Cully is doing in the background and what is open in your terminals.')),
      h('div', { class: 'row btools' }, search, sel, h('a', { class: 'btn', href: '#/' }, icon('plus'), 'New task')));
    const overlapBanner = S.overlaps.map((o) => h('div', { class: 'overlap' }, icon('alert'), ' ', h('b', {}, o.file), ' is being edited by ' + joinNames(o.agents.map(title)) + '. Expect a merge conflict, or ask one of them to wait.'));
    const board = h('div', { class: 'cols' }, ...COLS.map(([key, label, hint]) => {
      const all = listOf(key);
      const list = key === 'finished' && !S.finishedAll ? all.slice(0, 6) : all;
      const groups = key !== 'queued' ? [...new Set(all.filter((a) => a.group).map((a) => a.group))].filter((g) => S.agents.filter((a) => a.group === g).length > 1) : [];
      const addBtn = key === 'queued' ? h('button', { class: 'iconbtn', title: 'Add a task to the queue', 'aria-label': 'Add a task to the queue', onclick: () => { S.boardAdd = !S.boardAdd; S.boardSig = ''; renderBoard(); } }, icon('plus')) : null;
      const addForm = key === 'queued' && S.boardAdd ? (() => {
        const ta2 = h('textarea', { rows: '3', placeholder: 'What should Cully do next?', 'aria-label': 'New queued task' });
        const err2 = h('div', { class: 'err', hidden: true });
        const go2 = h('button', { class: 'btn small', onclick: async () => { go2.disabled = true; err2.hidden = true; try { await api('/api/queue', { method: 'POST', body: JSON.stringify({ prompt: ta2.value, repo: S.opts.repo || '', mode: S.opts.mode, context: S.opts.context, recall: S.opts.recall, scout: S.opts.scout }) }); S.boardAdd = false; S.boardSig = ''; renderBoard(); } catch (x) { err2.textContent = x.message; err2.hidden = false; go2.disabled = false; } } }, icon('plus'), 'Add to the queue');
        setTimeout(() => ta2.focus(), 0);
        return h('div', { class: 'card addcard' }, ta2, err2, h('div', { class: 'row' }, go2, h('button', { class: 'btn ghost small', onclick: () => { S.boardAdd = false; S.boardSig = ''; renderBoard(); } }, 'Cancel')));
      })() : null;
      const sec = h('section', { class: 'bcol ' + key }, h('header', { class: 'bcol-h' }, h('h3', {}, label), h('span', { class: 'bcount' }, String(all.length)), h('span', { class: 'sp' }), addBtn),
        h('p', { class: 'bhint' }, hint),
        h('div', { class: 'bcol-body' }, addForm,
        ...groups.map((g) => h('button', { class: 'btn ghost small cmpbtn', onclick: () => openCompare(g) }, icon('eye'), 'Compare options: ' + ((S.agents.find((a) => a.group === g) || {}).title || '').replace(/ \(option .\)$/, ''))),
        ...(list.length ? list.map((x) => (key === 'queued' ? queueCard(x) : boardCard(x, S.overlaps))) : [h('div', { class: 'none' }, key === 'waiting' ? 'Nothing needs you.' : key === 'queued' ? 'Nothing queued. Press + to add a task.' : key === 'working' ? 'Nothing is running. Drag a queued task here to start it.' : 'Nothing finished yet.')]),
        key === 'finished' && all.length > 6 ? h('button', { class: 'linkbtn', onclick: () => { S.finishedAll = !S.finishedAll; renderBoard(); } }, S.finishedAll ? 'Show fewer' : 'Show all ' + all.length) : null));
      sec.addEventListener('dragover', (e) => { e.preventDefault(); sec.classList.add('over'); });
      sec.addEventListener('dragleave', () => sec.classList.remove('over'));
      sec.addEventListener('drop', (e) => { sec.classList.remove('over'); dropOn(key === 'working' ? 'preparing' : key, e); });
      return sec;
    }));
    const empty = !S.agents.length && !queue.length ? h('div', { class: 'empty-state' }, h('h2', {}, 'No tasks yet'), h('p', { class: 'faint' }, 'Give Cully a task and it shows up here, moving from Working to Finished.'), h('a', { class: 'btn', href: '#/' }, icon('plus'), 'Start a task')) : null;
    box.replaceChildren(head, ...overlapBanner, empty || board, h('div', { id: 'running' }));
    refreshRunning();
    clearInterval(S.runTimer);
    S.runTimer = setInterval(() => { if (S.route.name !== 'board') { clearInterval(S.runTimer); return; } refreshRunning(); }, 5000);
  }

  // A small score ring. Colour comes from classes, the arc from stroke-dasharray.
  function ring(score, size) {
    const NS = 'http://www.w3.org/2000/svg'; const r = 15.9;
    const svg = document.createElementNS(NS, 'svg');
    for (const [k, v] of Object.entries({ viewBox: '0 0 36 36', width: size || 44, height: size || 44, class: 'ring ' + (score >= 80 ? 'good' : score >= 55 ? 'mid' : 'bad'), role: 'img', 'aria-label': 'Health ' + score + ' out of 100' })) svg.setAttribute(k, v);
    const mk = (cls, dash) => { const c = document.createElementNS(NS, 'circle'); for (const [k, v] of Object.entries({ cx: 18, cy: 18, r, fill: 'none', 'stroke-width': 3.6, class: cls })) c.setAttribute(k, v); if (dash) { c.setAttribute('stroke-dasharray', dash + ' 100'); c.setAttribute('stroke-linecap', 'round'); c.setAttribute('transform', 'rotate(-90 18 18)'); } return c; };
    const t = document.createElementNS(NS, 'text'); for (const [k, v] of Object.entries({ x: 18, y: 21.5, 'text-anchor': 'middle', class: 'ringnum' })) t.setAttribute(k, v); t.textContent = String(score);
    svg.append(mk('rtrack'), mk('rarc', score), t);
    return svg;
  }
  const COACH_KIND = { job: 'Background job', workflow: 'Workflow', skill: 'Skill', mcp: 'MCP server', habit: 'Habit', eval: 'Eval' };
  async function coachCard(again, actions) {
    let c = { items: [], at: '' };
    try { c = await api('/api/coach'); } catch (_) { /* optional */ }
    const run = async (btn) => { btn.disabled = true; try { await api('/api/coach/run', { method: 'POST' }); toast('The coach has new suggestions'); again(); } catch (e) { toast(e.message, true); btn.disabled = false; } };
    const btn = h('button', { class: 'btn small sp' }, icon('refresh'), c.items.length ? 'Ask again' : 'Ask the coach');
    btn.addEventListener('click', () => run(btn));
    return h('section', { class: 'lib-card coach' },
      h('div', { class: 'row' }, h('b', {}, 'Ways to work better'), c.at ? h('span', { class: 'faint small' }, 'from ' + c.based + ' sessions, ' + ago(c.at)) : null, btn),
      c.items.length ? h('ul', { class: 'coach-list' }, ...c.items.map((it) => h('li', { class: 'coach-item k-' + it.kind },
        h('span', { class: 'kind' }, COACH_KIND[it.kind] || it.kind), h('div', {}, h('b', {}, it.title), h('p', { class: 'faint small' }, it.why), h('p', { class: 'how' }, it.how),
          actions ? h('div', { class: 'row' }, h('button', { class: 'btn small', onclick: () => pushSuggestion(it) }, icon('send'), 'Apply'), h('button', { class: 'btn ghost small', onclick: () => writeClip('Suggestion from Cully: ' + it.title + '. ' + it.how).then(() => toast('Copied. Paste it into a session in a terminal.')).catch(() => showCommand('Suggestion from Cully: ' + it.title + '. ' + it.how)) }, icon('copy'), 'Copy message')) : null))))
        : h('p', { class: 'faint small' }, 'Once a few sessions are analysed, the coach looks for patterns across them and suggests a job, skill, MCP server, habit or eval to set up. It also looks again about once an hour while naming is on.'));
  }

  // The hourly scout switch, and the full history of what scouts found, kept on disk.
  async function scoutAuto(again) {
    let lib = null;
    try { lib = await api('/api/sessions/library'); } catch (_) { return null; }
    const on = lib.analysis.scout;
    const flip = async () => { try { await api('/api/scout/auto', { method: 'POST', body: JSON.stringify({ on: !on }) }); toast(on ? 'The hourly scout is off' : 'The hourly scout is on'); again(); } catch (e) { toast(e.message, true); } };
    return h('section', { class: 'lib-card' }, h('div', { class: 'row' }, h('b', {}, 'Scout the web while you work'), on && lib.analysis.lastScout && !lib.analysis.lastScout.startsWith('0001') ? h('span', { class: 'faint small' }, 'last ran ' + ago(lib.analysis.lastScout)) : null,
      h('button', { class: 'btn small sp' + (on ? ' ghost' : ''), onclick: flip }, icon(on ? 'x' : 'globe'), on ? 'Turn off' : 'Turn on')),
      h('p', { class: 'faint small' }, 'While you are working (a session changed in the last 90 minutes), at most once an hour the advisor gives one search; when you are away, every 12 hours the scouts found so far are merged into at most three suggestions, with repeats of the last week dropped. The advisor reads what you have been working on and picks a specific search that would help, such as a library, an error or a practice you keep running into. The scout runs it and the findings are kept below. Searches use general technical terms only: never a folder, project, path, link or ticket. It needs at least three named sessions, so start naming sessions on the Session library page first.'));
  }
  async function scoutHistory() {
    let list = [];
    let digests = [];
    try { list = await api('/api/research?kind=scout'); digests = await api('/api/research?kind=digest'); } catch (_) { return null; }
    return h('section', { class: 'lib-card' }, h('b', {}, 'Scout history'),
      ...digests.slice(0, 3).map((r) => h('details', { class: 'rl', open: true }, h('summary', {}, h('span', { class: 'rq' }, 'Suggestions'), h('span', { class: 'faint small' }, ago(r.time)), h('span', { class: 'chip' }, r.source)), h('div', { class: 'rbody' }, markdown(r.text)))), h('p', { class: 'faint small' }, list.length ? list.length + ' scouts kept on this computer.' : 'Nothing yet. Scouts you start, the ones the advisor starts and the ones run for an agent are all kept here.'),
      ...list.map((r) => h('details', { class: 'rl' }, h('summary', {}, h('span', { class: 'rq' }, r.query || 'Scout'), h('span', { class: 'faint small' }, ago(r.time)), h('span', { class: 'chip' }, r.source)), h('div', { class: 'rbody' }, markdown(r.text)))));
  }

  // Apply a suggestion: message an agent started from Cully, or start one for it. A session open
  // in a terminal or an app cannot be written to from here, so the text can be copied instead.
  function pushSuggestion(it) {
    const live = S.agents.filter((a) => ['preparing', 'working', 'waiting'].includes(a.state));
    const ta = h('textarea', { rows: '4', 'aria-label': 'Message' });
    ta.value = 'Suggestion from Cully: ' + it.title + '. ' + it.why + ' ' + it.how + ' Apply it to what you are doing now if it fits, and say what you changed.';
    const err = h('div', { class: 'err', hidden: true });
    const run = async (fn) => { err.hidden = true; try { await fn(); } catch (e) { err.textContent = e.message; err.hidden = false; } };
    const modal = h('div', { class: 'modal wide', role: 'dialog', 'aria-modal': 'true', 'aria-label': 'Apply this suggestion' },
      h('header', {}, h('h2', {}, 'Apply this suggestion'), h('button', { class: 'btn ghost small sp', onclick: closeMCP }, 'Close')),
      h('div', { class: 'mbody' }, h('p', {}, h('b', {}, it.title)), ta,
        live.length ? h('div', {}, h('span', { class: 'opt-label' }, 'Send it to a task that is running'),
          h('div', { class: 'row' }, ...live.map((a) => h('button', { class: 'btn small', onclick: () => run(async () => { await api('/api/agents/' + a.id + '/message', { method: 'POST', body: JSON.stringify({ text: ta.value }) }); closeMCP(); toast('Sent to ' + a.title); location.hash = '#/task/' + a.id; }) }, icon('send'), a.title))))
          : h('p', { class: 'faint small' }, 'No task started from Cully is running right now.'),
        h('div', { class: 'row' },
          h('button', { class: 'btn', onclick: () => run(async () => { const v = await api('/api/agents', { method: 'POST', body: JSON.stringify({ prompt: ta.value, repo: S.opts.repo || '', mode: 'edit', context: true }) }); closeMCP(); await refresh(); location.hash = '#/task/' + v.id; }) }, icon('plus'), 'Start a task for this'),
          h('button', { class: 'btn ghost', onclick: () => writeClip(ta.value).then(() => toast('Copied')).catch(() => showCommand(ta.value)) }, icon('copy'), 'Copy for a session in a terminal')),
        h('p', { class: 'faint small' }, 'Sessions open in a terminal, an editor or a desktop app cannot be written to from this page, so copy the text and paste it there.'), err));
    const bg = h('div', { class: 'modal-bg', onclick: (e) => { if (e.target === bg) closeMCP(); } }, modal);
    $('modal-root').replaceChildren(bg);
    S.modal = { agent: 'suggest' };
    document.removeEventListener('keydown', escClose);
    document.addEventListener('keydown', escClose);
  }

  // ---------- evals ----------
  // What evals are, in plain words. The ideas come from how coding-agent research tests agents:
  // a fixed set of small tasks with checks a reader can judge, rerun whenever the agent changes,
  // including a case where it should refuse, so a better-looking agent is not just a bolder one.
  function evalsExplainer() {
    return h('section', { class: 'lib-card' }, h('b', {}, 'What evals are'),
      h('p', { class: 'faint small' }, 'An eval is a small test for an agent. It has a task, a few checks written in plain words, and a judge that reads the answer and says pass, partly or fail. You run the same set again after you change an agent\'s instructions or tools, to see whether it got better or worse, instead of guessing.'),
      h('ul', { class: 'files' },
        h('li', {}, h('b', {}, 'Do'), ': it does what its role says.'),
        h('li', {}, h('b', {}, 'Refuse'), ': it declines something outside its role. An agent that never says no is not safe to schedule.'),
        h('li', {}, h('b', {}, 'Edge'), ': an awkward request, such as missing information.')),
      h('p', { class: 'faint small' }, 'Cully drafts the cases for each of your agents from its role and rules. You approve the ones worth keeping; only approved cases run. Eval runs only plan and answer, they never change files, and they use the agent\'s own tools limits. Cases made from your finished sessions are further down.'));
  }
  const CASE_KIND = { do: 'Do', refuse: 'Refuse', edge: 'Edge' };
  async function agentEvalCards(again) {
    let list = [];
    try { list = (await api('/api/identities')).filter((a) => a.approved !== false); } catch (_) { return { nodes: [], busy: false }; }
    if (!list.length) return { nodes: [emptyCard('No agents yet', 'Evals for an agent appear here once you have one. Create an agent first.', 'Open Agents', '#/dashboard/agents')], busy: false };
    let busy = false;
    const nodes = [];
    for (const a of list) {
      let v = null;
      try { v = await api('/api/identities/' + a.id + '/evals'); } catch (_) { continue; }
      const run = v.runs && v.runs[0];
      if (run && !run.done) busy = true;
      const resFor = (cid) => run && run.results.find((r) => r.caseId === cid);
      const approved = v.cases.filter((c) => c.approved);
      const act = (label, ic, path, opts, ok) => h('button', { class: 'btn small' + (opts && opts.ghost ? ' ghost' : ''), disabled: !!(opts && opts.off), onclick: async (e) => { e.target.disabled = true; try { const r = await api(path, { method: opts && opts.method || 'POST' }); if (ok) toast(typeof ok === 'function' ? ok(r) : ok); again(); } catch (x) { toast(x.message, true); e.target.disabled = false; } } }, icon(ic), label);
      const pass = run ? run.results.filter((r) => r.status === 'pass').length : 0;
      nodes.push(h('section', { class: 'lib-card' },
        h('div', { class: 'row' }, h('b', {}, a.name),
          run ? h('span', { class: 'chip ' + (run.done ? (pass === run.total ? 'run-pass' : 'run-partial') : 'run-running') }, run.done ? pass + ' of ' + run.total + ' passed' : 'Running ' + run.results.length + ' of ' + run.total) : h('span', { class: 'chip' }, 'Never run'),
          v.changed ? h('span', { class: 'chip run-partial', title: 'Its instructions or tools changed after the last run' }, 'Changed since last run') : null,
          h('span', { class: 'sp' }), act('Draft cases', 'plus', '/api/identities/' + a.id + '/evals/suggest', { ghost: true, off: v.cases.length >= 8 }, (r) => (r && r.added ? r.added + ' drafted. Approve the ones you want.' : 'Nothing new to add.')),
          act('Run ' + approved.length, 'send', '/api/identities/' + a.id + '/evals/run', { off: !approved.length || (run && !run.done) }, 'Started. Each case is a short answer-only run.')),
        v.cases.length ? h('ul', { class: 'hrows' }, ...v.cases.map((c) => { const r = resFor(c.id); return h('li', { class: 'hrow' },
          h('div', { class: 'hmain' }, h('div', { class: 'ht' }, h('span', { class: 'chip' }, CASE_KIND[c.kind] || c.kind), ' ' + c.name, r ? h('span', { class: 'chip run-' + r.status }, RUN_LABEL[r.status] || r.status) : null, c.approved ? null : h('span', { class: 'chip' }, 'Draft')),
            h('p', { class: 'hdesc' }, c.task),
            c.checks.length ? h('ul', { class: 'files' }, ...c.checks.map((x) => h('li', {}, x))) : null,
            r && r.reason ? h('p', { class: 'faint small' }, r.reason) : null),
          h('div', { class: 'row hact' }, c.approved ? null : act('Approve', 'check', '/api/identities/' + a.id + '/evals/' + c.id + '/approve', {}), r && r.taskId ? h('a', { class: 'btn ghost small', href: '#/task/' + r.taskId }, 'Open') : null, act('Remove', 'x', '/api/identities/' + a.id + '/evals/' + c.id, { ghost: true, method: 'DELETE' }))); }))
          : h('p', { class: 'faint small' }, 'No cases yet. Draft some from this agent\'s role and rules.')));
    }
    return { nodes, busy };
  }
  const RUN_LABEL = { running: 'Running', judging: 'Judging', pass: 'Passed', partial: 'Partly', fail: 'Failed', error: 'No result' };
  async function renderEvals(out, head) {
    const [cases, runs] = await Promise.all([api('/api/sessions/evals').catch(() => []), api('/api/evals/runs').catch(() => [])]);
    const live = () => S.route.name === 'dashboard' && S.route.panel === 'evals';
    const again = () => renderEvals(out, head);
    const active = runs.some((r) => r.status === 'running' || r.status === 'judging');
    const start = async (c, btn) => { btn.disabled = true; try { await api('/api/evals/' + c.id + '/run', { method: 'POST' }); toast('Started. A fresh agent is working on this goal.'); again(); } catch (e) { toast(e.message, true); btn.disabled = false; } };
    const lastFor = (id) => runs.find((r) => r.evalId === id);
    const passed = runs.filter((r) => r.status === 'pass').length; const failed = runs.filter((r) => r.status === 'fail').length;
    const agentCards = await agentEvalCards(again);
    const agentBusy = agentCards.busy;
    out.replaceChildren(head(refreshBtn(again)),
      evalsExplainer(),
      h('h3', { class: 'section-h' }, 'Your agents'),
      ...agentCards.nodes,
      h('h3', { class: 'section-h' }, 'From your sessions'),
      h('div', { class: 'kpis' }, ...[['Eval cases', String(cases.length)], ['Runs', String(runs.length)], ['Passed', String(passed)], ['Failed', String(failed)], ['Running now', String(runs.filter((r) => r.status === 'running' || r.status === 'judging').length)]].map(([k, v]) => h('div', { class: 'kpi' }, h('b', {}, v), h('span', {}, k)))),
      h('h3', { class: 'section-h' }, 'Runs'),
      runs.length ? h('ul', { class: 'hrows' }, ...runs.slice(0, 30).map((r) => h('li', { class: 'hrow' },
        h('div', { class: 'hmain' }, h('div', { class: 'ht' }, r.task, h('span', { class: 'chip run-' + r.status }, RUN_LABEL[r.status] || r.status)),
          r.reason ? h('p', { class: 'hdesc' }, r.reason) : null,
          h('div', { class: 'row' }, h('span', { class: 'chip' }, ago(r.started)), r.seconds ? h('span', { class: 'chip' }, dur(r.seconds)) : null, r.files ? h('span', { class: 'chip' }, r.files + ' files changed') : null)),
        r.agentId ? h('div', { class: 'row hact' }, h('a', { class: 'btn ghost small', href: '#/task/' + r.agentId }, 'Open the task')) : null)))
        : emptyCard('No eval runs yet', 'Run a case below: a fresh agent gets the goal, then a judge checks its work against the checks.', null, null),
      h('h3', { class: 'section-h' }, 'Eval cases'),
      cases.length ? h('ul', { class: 'hrows' }, ...cases.map((c) => { const last = lastFor(c.id); const btn = h('button', { class: 'btn small' }, icon('send'), last ? 'Run again' : 'Run');
        btn.addEventListener('click', () => start(c, btn));
        return h('li', { class: 'hrow' }, h('div', { class: 'hmain' }, h('div', { class: 'ht' }, c.task, last ? h('span', { class: 'chip run-' + last.status }, RUN_LABEL[last.status] || last.status) : null),
          c.criteria.length ? h('ul', { class: 'files' }, ...c.criteria.map((x) => h('li', {}, x))) : null,
          h('div', { class: 'row' }, h('span', { class: 'chip' }, icon('folder'), c.folder), h('span', { class: 'chip' }, 'made ' + ago(c.created)))), h('div', { class: 'row hact' }, btn)); }))
        : emptyCard('No eval cases yet', 'Open Session health, pick a session that finished its job and choose Make an eval.', 'Open Session health', '#/dashboard/health'));
    if (active || agentBusy) setTimeout(() => { if (live()) again(); }, 4000);
  }

  // What a good session looks like, and how the user's own sessions compare. The practices come from
  // reading coding-agent harnesses: a clear goal with checks, a brief instead of a dump, a plan only
  // when the task is big, tests after changes, no repeated calls, and a summary at the end.
  function optimalCard(lib) {
    const all = lib.sessions;
    if (all.length < 3) return null;
    const pct = (n, d) => (d ? Math.round((n * 100) / d) : null);
    const edited = all.filter((x) => x.edits > 0); const judged = all.filter((x) => x.source === 'ai' && x.completed);
    const rows = [
      ['Ran tests after changing files', pct(edited.filter((x) => !x.unverified).length, edited.length), 90, 'Run the repository\'s tests after each batch of edits and before calling the task done.'],
      ['Never repeated a call 3 times in a row', pct(all.filter((x) => (x.maxRepeat || 0) < 3).length, all.length), 95, 'When something fails twice, read the error and change approach instead of rerunning it.'],
      ['Stayed on the original task', pct(judged.filter((x) => x.onTask !== 'drifted').length, judged.length), 90, 'State the goal and how you will know it is done, and check against it at each milestone.'],
      ['Finished the job', pct(judged.filter((x) => x.completed === 'yes').length, judged.length), 75, 'Break big work into a numbered plan and finish one step before starting the next.'],
      ['Stayed a manageable size (100 tool calls or fewer)', pct(all.filter((x) => (x.tools || 0) <= 100).length, all.length), 85, 'Split long work into separate sessions and hand off with a short summary of what is done and what is next.'],
    ].filter((r) => r[1] != null);
    return h('section', { class: 'lib-card' }, h('b', {}, 'What a good session looks like'),
      h('p', { class: 'faint small' }, 'Compared with your own ' + all.length + ' sessions. A good session starts with a clear goal and checks, brings only the context it needs, plans only when the task is big, runs tests after changes, and ends with a short summary.'),
      h('ul', { class: 'optimal' }, ...rows.map(([label, v, target, tip]) => h('li', {}, h('div', { class: 'row' }, h('span', { class: 'ol' }, label), h('b', { class: v >= target ? 'ok-t' : 'bad-t' }, v + '%'), h('span', { class: 'faint small' }, 'aim for ' + target + '%')),
        h('span', { class: 'fbar' }, fill(v)), v < target ? h('p', { class: 'faint small' }, tip) : null))));
  }

  // What a proposal is drawn from: how many sessions, over how many days, and which ones, so it can be
  // checked. A suggestion with no evidence is never shown.
  function evidenceBlock(x) {
    const ev = x.evidence || [];
    if (!ev.length) return null;
    return h('details', { class: 'evid' }, h('summary', {}, h('span', { class: 'rq' }, 'Based on ' + (x.count || ev.length) + ' sessions' + (x.days ? ' over ' + x.days + ' days' : '')), h('span', { class: 'faint small' }, 'see which')),
      h('ul', { class: 'evlist' }, ...ev.map((e) => h('li', {}, harnessChip(e.agent), h('span', {}, e.title || 'Session'), h('span', { class: 'faint small' }, new Date(e.at).toLocaleDateString([], { month: 'short', day: 'numeric' }))))));
  }
  // ---------- agents (named, persistent workers) ----------
  const AGENT_TEMPLATES = [
    ['Pull request watcher', 'Checks the open pull requests of this repository and reports which need a person.', 'Look at the open pull requests with the available tools. Report, for each: its title, whether checks pass, and what is blocking it. Never merge, comment or push.', { read: true, commands: true }],
    ['Test guardian', 'Runs the test suite and reports new failures with the likely cause.', 'Run the repository\'s documented test command. If tests fail, find the likely cause by reading the code and recent changes, and report it. Do not change files.', { read: true, commands: true }],
    ['Dependency reviewer', 'Looks for outdated or risky dependencies and suggests safe upgrades.', 'List the dependencies, check which are far behind or have known problems, and suggest the smallest safe upgrades. Do not change files.', { read: true, commands: true, web: true }],
    ['Docs keeper', 'Finds documentation that no longer matches the code.', 'Compare the docs with the code they describe and list the places that are out of date, with the file and what is wrong. Do not change files.', { read: true }],
  ];
  async function renderTeam() {
    const box = $('team');
    box.replaceChildren(h('p', { class: 'faint' }, 'Loading…'));
    let list = []; let tools = null;
    let proposals = [];
    try { [list, tools, proposals] = await Promise.all([api('/api/identities'), api('/api/admin/tools'), api('/api/agent-suggestions').catch(() => [])]); } catch (e) { box.replaceChildren(h('div', { class: 'err' }, e.message)); return; }
    const again = () => renderTeam();
    const live = list.some((a) => a.runs.some((r) => r.status === 'running'));
    const hue = (name) => { let n = 0; for (const c of name) n = (n * 31 + c.codePointAt(0)) % 360; return n; };
    const initials = (name) => name.split(/[\s._-]+/).filter(Boolean).slice(0, 2).map((w) => w[0].toUpperCase()).join('') || '?';
    const stateOf = (a) => { const last = a.runs[0]; if (last && last.status === 'running') return 'running'; if (!a.approved) return 'draft'; if (a.enabled) return 'scheduled'; return 'idle'; };
    const STATE_WORD = { running: 'Running now', draft: 'Draft', scheduled: 'Scheduled', idle: 'On demand' };
    const avatar = (a) => { const el = h('div', { class: 'avatar st-' + stateOf(a), 'aria-hidden': 'true' }, h('span', {}, initials(a.name)), h('i', { class: 'pulse' })); el.style.setProperty('--hue', String(hue(a.name))); return el; };
    const runBars = (a) => { const rs = a.runs.slice(0, 12).reverse(); if (!rs.length) return h('div', { class: 'runbars empty' }, 'No runs yet'); return h('div', { class: 'runbars', role: 'img', 'aria-label': 'The last ' + rs.length + ' runs, oldest first' }, ...rs.map((r) => { const b = h('i', { class: 'rb rb-' + r.status, title: r.status + ', ' + ago(r.started) }); b.style.height = (r.status === 'running' ? 60 : 40 + Math.min(60, Math.round((r.seconds || 0) / 6))) + '%'; return b; })); };
    const streak = (a) => { let n = 0; for (const r of a.runs) { if (r.status === 'failed') n++; else if (r.status !== 'running') break; } return n; };
    const okRate = (a) => { const done = a.runs.filter((r) => r.status !== 'running'); return done.length ? Math.round((100 * done.filter((r) => r.status === 'done').length) / done.length) : null; };
    const runNow = async (a) => { try { await api('/api/identities/' + a.id + '/run', { method: 'POST' }); toast(a.name + ' is running. See it on the Tasks tab.'); again(); } catch (e) { toast(e.message, true); } };
    const togglePause = async (a) => { try { await api('/api/identities/' + a.id, { method: 'PUT', body: JSON.stringify({ name: a.name, role: a.role, instructions: a.instructions, scope: a.scope, folder: a.folder, groups: a.groups, mode: a.mode, schedule: a.schedule, enabled: !a.enabled, approve: a.approved }) }); toast(a.enabled ? a.name + ' is paused' : a.name + ' will run ' + a.schedule); again(); } catch (e) { toast(e.message, true); } };
    const approve = async (a) => { try { await api('/api/identities/' + a.id + '/approve', { method: 'POST' }); toast(a.name + ' is now one of your agents'); again(); } catch (e) { toast(e.message, true); } };
    const remove = (a) => confirmDialog({ title: 'Remove ' + a.name + '?', body: ['Its notes and run history are deleted. Tasks it already ran stay on the Tasks tab.'], confirmLabel: 'Remove', onConfirm: async () => { await api('/api/identities/' + a.id, { method: 'DELETE' }); again(); } });
    const card = (a) => {
      const last = a.runs[0]; const groups = tools.presets.filter((p) => a.groups[p.id]).map((p) => p.label); const st = stateOf(a); const rate = okRate(a);
      const card = h('section', { class: 'agentcard2 st-' + st },
        h('div', { class: 'ac-head' }, avatar(a),
          h('div', { class: 'ac-id' }, h('div', { class: 'ac-name' }, a.name, h('span', { class: 'ac-state st-' + st }, STATE_WORD[st]), ...window.CullyLedger.agentChips(a)),
            h('div', { class: 'ac-meta' }, h('span', {}, icon('folder'), a.folder ? a.folder.split('/').pop() : 'Own workspace'), h('span', {}, a.scope === 'global' ? 'All projects' : 'One project'), h('span', {}, a.mode === 'plan' ? 'Read only' : 'Can edit'), a.approved && a.schedule ? h('span', {}, icon('clock'), (a.enabled ? 'Runs ' : 'Paused, ') + a.schedule) : null))),
        h('p', { class: 'ac-role' }, a.role),
        groups.length ? h('div', { class: 'ac-tools' }, ...groups.map((g) => h('span', { class: 'tg' }, g))) : null,
        h('div', { class: 'ac-runs' }, runBars(a),
          h('div', { class: 'ac-runtext' },
            last ? h('div', { class: 'row' }, h('span', { class: 'chip run-' + (last.status === 'done' ? 'pass' : last.status === 'failed' ? 'fail' : 'running') }, last.status === 'done' ? 'Last run finished' : last.status === 'failed' ? 'Last run failed' : 'Running now'), h('span', { class: 'faint small' }, ago(last.started) + (last.seconds ? ', ' + dur(last.seconds) : '')), rate != null && a.runs.length > 1 ? h('span', { class: 'faint small' }, rate + '% finished') : null, streak(a) > 1 ? h('span', { class: 'chip run-fail' }, streak(a) + ' failed in a row') : null) : h('span', { class: 'faint small' }, 'Has not run yet.'))),
        last && last.summary ? h('div', { class: 'quote ac-quote' }, markdown(last.summary)) : null,
        !a.approved ? h('p', { class: 'faint small' }, 'A draft does not run and is not scheduled. Approve it to make it one of your agents.') : null,
        h('div', { class: 'ac-foot' },
          a.approved
            ? h('button', { class: 'btn small', title: 'Start one run of this agent now', disabled: st === 'running', onclick: () => runNow(a) }, icon('send'), st === 'running' ? 'Running' : 'Run once')
            : h('button', { class: 'btn small', title: 'Make this part of your agents, so it can run and be scheduled', onclick: () => approve(a) }, icon('check'), 'Approve as agent'),
          last && last.taskId ? h('a', { class: 'btn ghost small', href: '#/task/' + last.taskId }, 'Open the task') : null,
          h('span', { class: 'sp' }),
          a.approved && a.schedule ? h('button', { class: 'btn ghost small', title: a.enabled ? 'Stop the schedule but keep the agent' : 'Start the schedule again', onclick: () => togglePause(a) }, a.enabled ? 'Pause' : 'Resume') : null,
          h('a', { class: 'btn ghost small', href: '/api/identities/' + a.id + '/export', download: '', title: 'Save this agent as a Markdown file: its settings and instructions, with no notes or paths' }, 'Export'),
          ...window.CullyLedger.agentButtons(a, again),
          h('button', { class: 'btn ghost small', onclick: () => openAgentForm(a, tools, again) }, 'Edit'),
          h('button', { class: 'btn ghost small', 'aria-label': 'Remove ' + a.name, onclick: () => remove(a) }, icon('x'))),
        a.memory ? h('details', { class: 'rl' }, h('summary', {}, h('span', { class: 'rq' }, 'Its notes'), h('span', { class: 'faint small' }, a.memory.length + ' of 2200 characters')), h('div', { class: 'rbody' }, markdown(a.memory))) : null,
        a.runs.length > 1 ? h('details', { class: 'rl' }, h('summary', {}, h('span', { class: 'rq' }, 'Earlier runs'), h('span', { class: 'faint small' }, String(a.runs.length - 1))), h('ul', { class: 'hrows' }, ...a.runs.slice(1, 8).map((r) => h('li', { class: 'hrow' }, h('div', { class: 'hmain' }, h('div', { class: 'row' }, h('span', { class: 'chip run-' + (r.status === 'done' ? 'pass' : 'fail') }, r.status), h('span', { class: 'faint small' }, ago(r.started))), r.summary ? h('p', { class: 'hdesc' }, uiShort(r.summary, 220)) : null))))) : null);
      card.dataset.state = st;
      return card;
    };
    const counts = { running: 0, scheduled: 0, draft: 0, idle: 0 }; for (const a of list) counts[stateOf(a)]++;
    const weekAgo = Date.now() - 7 * 864e5; const weekRuns = list.flatMap((a) => a.runs).filter((r) => new Date(r.started).getTime() > weekAgo);
    const weekOk = weekRuns.filter((r) => r.status === 'done').length;
    const stat = (n, label, tone) => h('div', { class: 'astat' + (tone ? ' ' + tone : '') }, h('b', {}, String(n)), h('span', {}, label));
    const filters = [['all', 'All', list.length], ['running', 'Running', counts.running], ['scheduled', 'Scheduled', counts.scheduled], ['idle', 'On demand', counts.idle], ['draft', 'Drafts', counts.draft]];
    const grid = h('div', { class: 'agentgrid' }, ...list.map(card));
    let want = 'all';
    try { want = sessionStorage.getItem('cully.agentfilter') || 'all'; } catch (_) { /* private window */ }
    const pills = h('div', { class: 'pills', role: 'tablist', 'aria-label': 'Filter agents' });
    const applyFilter = (f) => { want = f; try { sessionStorage.setItem('cully.agentfilter', f); } catch (_) { /* ignore */ } for (const b of pills.children) b.setAttribute('aria-selected', b.dataset.f === f ? 'true' : 'false'); for (const c of grid.children) c.hidden = f !== 'all' && c.dataset.state !== f; };
    for (const [f, label, n] of filters) { if (f !== 'all' && !n) continue; pills.append(h('button', { class: 'pill', role: 'tab', 'data-f': f, onclick: () => applyFilter(f) }, label, h('span', { class: 'pn' }, String(n)))); }
    if (![...pills.children].some((b) => b.dataset.f === want)) want = 'all';
    const descIn = h('input', { type: 'text', class: 'desc-in', autocomplete: 'off', 'aria-label': 'Describe an agent', placeholder: 'Describe a job in a sentence, for example: every weekday morning, check my open pull requests and say which need me' });
    const descBtn = h('button', { class: 'btn' }, icon('spark'), 'Draft it');
    const draftIt = async () => {
      const text = descIn.value.trim(); if (!text) return;
      descBtn.disabled = true; descBtn.textContent = 'Drafting…';
      try { const d = await api('/api/identities/draft', { method: 'POST', body: JSON.stringify({ text }) }); descIn.value = ''; openAgentForm({ ...d, id: null, scope: 'project', enabled: !!d.schedule, folder: S.opts.repo || (S.info && S.info.repos[0]) || '' }, tools, again); }
      catch (e) { toast(e.message, true); }
      descBtn.disabled = false; descBtn.replaceChildren(icon('spark'), 'Draft it');
    };
    descBtn.addEventListener('click', draftIt);
    descIn.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); draftIt(); } });
    const describeBar = h('div', { class: 'describe' }, descIn, descBtn);
    const askNow = async (btn) => { btn.disabled = true; try { const r = await api('/api/agent-suggestions/run', { method: 'POST' }); toast(r.added ? r.added + ' new ' + (r.added === 1 ? 'proposal' : 'proposals') : 'Nothing new to propose right now'); again(); } catch (e) { toast(e.message, true); btn.disabled = false; } };
    const askBtn = h('button', { class: 'btn ghost small sp' }, icon('spark'), proposals.length ? 'Look again' : 'Ask Cully for ideas');
    askBtn.addEventListener('click', () => askNow(askBtn));
    const proposalCard = (p) => { const cur = p.replaces ? list.find((x) => x.id === p.replaces) : null; return h('li', { class: 'hrow' },
      h('div', { class: 'hmain' }, h('div', { class: 'ht' }, p.name, cur ? h('span', { class: 'chip run-partial' }, 'Updates an agent you have') : h('span', { class: 'chip' }, 'New'), p.schedule ? h('span', { class: 'chip' }, 'Runs ' + p.schedule) : h('span', { class: 'chip' }, 'On demand'), h('span', { class: 'chip' }, icon('folder'), p.folder.split('/').pop())),
        h('p', { class: 'hdesc' }, p.role), h('p', { class: 'faint small' }, h('b', {}, 'Why: '), p.why), evidenceBlock(p), window.CullyLedger.preview(p.preview),
        h('div', { class: 'row' }, ...tools.presets.filter((g) => p.groups[g.id]).map((g) => h('span', { class: 'chip' }, g.label)))),
      h('div', { class: 'row hact' },
        h('button', { class: 'btn small', title: cur ? 'Review the changes to ' + cur.name + ' and save them' : 'Review it, change anything, and create the agent', onclick: () => openAgentForm(cur ? { ...cur, role: p.role, instructions: p.instructions, groups: p.groups, schedule: p.schedule, enabled: !!p.schedule, fromSuggestion: p.id } : { ...p, id: null, enabled: !!p.schedule, mode: 'edit', scope: 'project', fromSuggestion: p.id }, tools, again) }, icon('check'), cur ? 'Review the update' : 'Review and approve'),
        h('button', { class: 'btn ghost small', onclick: async () => { try { await api('/api/agent-suggestions/' + p.id + '/dismissed', { method: 'POST' }); again(); } catch (e) { toast(e.message, true); } } }, 'Dismiss'),
        ...window.CullyLedger.proposalButtons(p, again, false))); };
    box.replaceChildren(
      toolbar('Agents', 'Named workers with a role of their own. Each keeps notes between runs and can run on a schedule.', window.CullyLedger.link('agent'), h('button', { class: 'btn', onclick: () => openAgentForm(null, tools, again) }, icon('plus'), 'New agent')),
      h('details', { class: 'lib-card', ...(list.length ? {} : { open: true }) }, h('summary', {}, h('b', {}, 'What an agent is and how it works')),
        h('ol', { class: 'how' },
          h('li', {}, h('b', {}, 'Identity'), 'A name, a one-line role and instructions on how it works. Cully adds them to the start of every run, so they stay the same and the prompt cache can reuse them.'),
          h('li', {}, h('b', {}, 'Tools'), 'Only the tool groups you pick. The always-blocked list from Harness permissions still applies.'),
          h('li', {}, h('b', {}, 'A folder'), 'It runs as a task in its own worktree of that folder, so your checkout is untouched. Runs show on the Tasks tab.'),
          h('li', {}, h('b', {}, 'Notes'), 'At the end of a run it saves up to three short facts. The next run starts by reading them. The notes are capped at 2,200 characters and the newest win.'),
          h('li', {}, h('b', {}, 'Schedule'), 'Run it now, every hour, or every day while the Cully page server is running. Each run keeps a short summary.'))),
      h('section', { class: 'lib-card' }, h('div', { class: 'row' }, h('b', {}, 'Suggested for you'), askBtn),
        proposals.length ? h('ul', { class: 'hrows' }, ...proposals.map(proposalCard))
          : h('p', { class: 'faint small' }, 'Cully reads your analysed sessions and proposes agents for work you repeat or skip, such as watching pull requests or running the tests. You review each one before it exists. It looks again about once an hour while naming is on.')),
      describeBar,
      list.length ? h('div', { class: 'astats' }, stat(list.length, list.length === 1 ? 'agent' : 'agents'), stat(counts.running, 'running now', counts.running ? 'hot' : ''), stat(counts.scheduled, 'on a schedule'), stat(weekRuns.length, 'runs this week'), stat(weekRuns.length ? Math.round((100 * weekOk) / weekRuns.length) + '%' : 'none', 'finished well')) : null,
      list.length ? pills : null,
      list.length ? grid : emptyCard('No agents yet', 'Create one for work you repeat: watching pull requests, guarding the tests, checking dependencies.', null, null));
    if (list.length) applyFilter(want);
    window.CullyLedger.archivedAgents(again).then((sec) => { if (sec && S.route.name === 'team') box.append(sec); });
    if (live) setTimeout(() => { if (S.route.name === 'team') again(); }, 4000);
  }
  const uiShort = (t, n) => (t.length > n ? t.slice(0, n) + '…' : t);
  function openAgentForm(a, tools, again) {
    const f = a || { name: '', role: '', instructions: '', scope: 'project', folder: S.opts.repo || (S.info && S.info.repos[0]) || '', groups: Object.fromEntries(tools.presets.map((p) => [p.id, p.on])), mode: 'edit', schedule: '', enabled: false };
    const val = { ...f, groups: { ...f.groups } };
    const err = h('div', { class: 'err', hidden: true });
    const text = (key, label, hint, rows) => { const el = rows ? h('textarea', { rows: String(rows), 'aria-label': label }) : h('input', { type: 'text', 'aria-label': label, autocomplete: 'off' }); el.value = val[key] || ''; el.addEventListener('input', () => { val[key] = el.value; }); return h('label', { class: 'field' }, h('span', { class: 'opt-label' }, label), el, hint ? h('span', { class: 'faint small' }, hint) : null); };
    const folders = [...new Set([val.folder, ...((S.info && S.info.repos) || [])].filter(Boolean))];
    const folderSel = h('select', { 'aria-label': 'Folder' }, h('option', Object.assign({ value: '' }, !val.folder ? { selected: true } : {}), 'No folder: its own workspace'), ...folders.map((p) => h('option', Object.assign({ value: p }, p === val.folder ? { selected: true } : {}), p)));
    folderSel.addEventListener('change', () => { val.folder = folderSel.value; });
    const folderField = h('label', { class: 'field' }, h('span', { class: 'opt-label' }, 'Folder it works in'), folderSel, h('span', { class: 'faint small' }, 'Any folder you can write in. It does not need git; inside a git repository the agent works on a copy of it.'));
    const scopeSel = h('select', { 'aria-label': 'Kind of agent' }, h('option', Object.assign({ value: 'project' }, val.scope !== 'global' ? { selected: true } : {}), 'For one project or folder'), h('option', Object.assign({ value: 'global' }, val.scope === 'global' ? { selected: true } : {}), 'Global: for all my projects'));
    const syncScope = () => { folderField.hidden = val.scope === 'global'; };
    scopeSel.addEventListener('change', () => { val.scope = scopeSel.value; syncScope(); });
    syncScope();
    const roleField = text('role', 'What it does', 'One sentence.'); const instrField = text('instructions', 'How it works', 'Its identity: how it should behave, what it must never do. Added to every run.', 6); const nameField = text('name', 'Name');
    const tpl = a ? null : h('div', { class: 'row' }, h('span', { class: 'opt-label' }, 'Start from'), ...AGENT_TEMPLATES.map(([n, r, i, g]) => h('button', { type: 'button', class: 'chip', onclick: () => { val.name = n; val.role = r; val.instructions = i; for (const p of tools.presets) val.groups[p.id] = !!g[p.id]; val.mode = 'plan' === val.mode ? 'plan' : val.mode; closeMCP(); openAgentForm({ ...val, id: null }, tools, again); } }, n)));
    const groups = h('div', { class: 'toolgroups' }, ...tools.presets.map((p) => { const cb = h('input', Object.assign({ type: 'checkbox', id: 'ag-' + p.id, onchange: (e) => { val.groups[p.id] = e.target.checked; } }, val.groups[p.id] ? { checked: true } : {})); return h('label', { class: 'toolgroup', for: 'ag-' + p.id }, cb, h('span', {}, h('b', {}, p.label), h('small', {}, p.description))); }));
    const mode = h('select', { 'aria-label': 'Mode' }, h('option', Object.assign({ value: 'edit' }, val.mode === 'edit' ? { selected: true } : {}), 'Can edit files'), h('option', Object.assign({ value: 'plan' }, val.mode === 'plan' ? { selected: true } : {}), 'Read only (plan mode)'));
    mode.addEventListener('change', () => { val.mode = mode.value; });
    const sched = h('select', { 'aria-label': 'Schedule' }, ...[['', 'Only when I run it'], ['every 30m', 'Every 30 minutes'], ['hourly', 'Every hour'], ['every 6h', 'Every 6 hours'], ['daily', 'Every day'], ['weekdays', 'Every weekday'], ...(val.schedule && !['', 'every 30m', 'hourly', 'every 6h', 'daily', 'weekdays'].includes(val.schedule) ? [[val.schedule, 'Every ' + val.schedule.replace(/^every /, '')]] : [])].map(([v, l]) => h('option', Object.assign({ value: v }, val.schedule === v ? { selected: true } : {}), l)));
    sched.addEventListener('change', () => { val.schedule = sched.value; val.enabled = !!sched.value; });
    const clear = a && a.memory ? h('label', { class: 'check' }, h('input', { type: 'checkbox', onchange: (e) => { val.clearMemory = e.target.checked; } }), 'Forget its notes') : null;
    const doSave = async (approve, btn) => {
      err.hidden = true; btn.disabled = true;
      try {
        const body = { name: val.name, role: val.role, instructions: val.instructions, scope: val.scope || 'project', folder: val.scope === 'global' ? '' : val.folder, groups: val.groups, mode: val.mode, schedule: val.schedule, enabled: !!val.schedule, proposal: a && a.fromSuggestion ? a.fromSuggestion : undefined, approve, clearMemory: !!val.clearMemory };
        if (a && a.id) await api('/api/identities/' + a.id, { method: 'PUT', body: JSON.stringify(body) }); else await api('/api/identities', { method: 'POST', body: JSON.stringify(body) });
        if (a && a.fromSuggestion) await api('/api/agent-suggestions/' + a.fromSuggestion + '/accepted', { method: 'POST' }).catch(() => {});
        closeMCP(); toast(approve ? val.name + ' is now one of your agents' : 'Saved ' + val.name + ' as a draft'); again();
      } catch (e) { err.textContent = e.message; err.hidden = false; btn.disabled = false; }
    };
    const draftBtn = h('button', { class: 'btn ghost' }, a && a.approved ? 'Save' : 'Save as draft');
    draftBtn.addEventListener('click', () => doSave(false, draftBtn));
    const approveBtn = h('button', { class: 'btn' }, icon('check'), a && a.approved ? 'Save and keep approved' : 'Approve as agent');
    approveBtn.addEventListener('click', () => doSave(true, approveBtn));
    const modal = h('div', { class: 'modal wide', role: 'dialog', 'aria-modal': 'true', 'aria-label': a && a.id ? 'Edit agent' : 'New agent' },
      h('header', {}, h('h2', {}, a && a.id ? 'Edit ' + f.name : 'New agent'), h('button', { class: 'btn ghost small sp', onclick: closeMCP }, 'Cancel')),
      h('div', { class: 'mbody' }, tpl, nameField, roleField, instrField,
        h('label', { class: 'field' }, h('span', { class: 'opt-label' }, 'Kind of agent'), scopeSel), folderField,
        h('div', {}, h('span', { class: 'opt-label' }, 'Tools it may use'), groups),
        h('div', { class: 'row' }, h('label', { class: 'field' }, h('span', { class: 'opt-label' }, 'Mode'), mode), h('label', { class: 'field' }, h('span', { class: 'opt-label' }, 'Schedule'), sched)),
        clear, err, h('div', { class: 'row' }, approveBtn, draftBtn)));
    const bg = h('div', { class: 'modal-bg', onclick: (e) => { if (e.target === bg) closeMCP(); } }, modal);
    $('modal-root').replaceChildren(bg);
    S.modal = { agent: 'agentform' };
    document.removeEventListener('keydown', escClose);
    document.addEventListener('keydown', escClose);
  }

  // ---------- workflow rules ----------
  async function renderRules(out, head) {
    const v = await api('/api/rules');
    const again = () => renderRules(out, head);
    S.rules = v; S.rulesAt = Date.now();
    const p = v.proposal;
    const ask = async (btn) => { btn.disabled = true; try { const r = await api('/api/rules/propose', { method: 'POST' }); toast(r.added ? 'A draft is ready for your review' : 'Nothing to change right now'); again(); } catch (e) { toast(e.message, true); btn.disabled = false; } };
    const askBtn = h('button', { class: 'btn small sp' }, icon('spark'), v.exists ? 'Look for changes' : 'Draft it from my sessions');
    askBtn.addEventListener('click', () => ask(askBtn));
    const proposal = p ? h('section', { class: 'lib-card coach' }, h('div', { class: 'row' }, h('b', {}, v.exists ? 'Proposed update' : 'First draft'), h('span', { class: 'faint small' }, 'written from ' + p.based + ' named sessions, ' + ago(p.created))),
      h('ul', { class: 'how' }, ...p.changes.map((c) => h('li', {}, c))),
      h('div', { class: 'difflist', role: 'region', 'aria-label': 'What would change' }, ...v.diff.map((d) => h('div', { class: 'dline ' + (d.op === '+' ? 'add' : d.op === '-' ? 'del' : '') }, h('span', { class: 'dop' }, d.op === ' ' ? '' : d.op), d.text || ' '))),
      h('p', { class: 'faint small' }, 'Approving writes it to: ' + v.targets.map((t) => AGENT_LABEL[t.agent]).join(', ') + '. The file it replaces is kept beside it as SKILL.md.prev.'),
      h('div', { class: 'row' },
        h('button', { class: 'btn', onclick: async () => { try { await api('/api/rules/' + p.id + '/approve', { method: 'POST' }); toast('Saved. Your harnesses will read it from now on.'); S.rulesAt = 0; again(); } catch (e) { toast(e.message, true); } } }, icon('check'), 'Approve and save'),
        h('button', { class: 'btn ghost', onclick: async () => { try { await api('/api/rules/' + p.id + '/dismiss', { method: 'POST' }); S.rulesAt = 0; again(); } catch (e) { toast(e.message, true); } } }, 'Dismiss'))) : null;
    out.replaceChildren(head(refreshBtn(again)),
      h('p', { class: 'faint' }, 'One skill, dev-workflow-rules, that tells every harness how you work: the projects you are on, the harnesses you use, habits that help and things to avoid. Cully drafts it from your sessions and keeps it up to date, but never saves anything until you approve it. It looks about once a day, and a notification asks you to review.'),
      h('section', { class: 'lib-card' }, h('div', { class: 'row' }, h('b', {}, 'Where it lives'), askBtn),
        h('ul', { class: 'rules' }, ...v.targets.map((t) => h('li', {}, harnessChip(t.agent), h('code', {}, t.path), h('span', { class: 'chip ' + (t.exists ? 'ok' : '') }, t.exists ? 'Saved' : 'Not created yet')))),
        v.named < v.needed ? h('p', { class: 'faint small' }, 'Cully needs ' + v.needed + ' analysed sessions to write it; ' + v.named + ' so far. Start naming sessions on the Session library page.') : null),
      proposal,
      v.exists ? h('section', { class: 'lib-card' }, h('b', {}, 'Your current rules'), h('div', { class: 'quote' }, markdown(v.current))) : null);
  }

  // ---------- session health ----------
  const DONE_LABEL = { yes: 'Completed', partly: 'Partly done', no: 'Not completed', unclear: 'Unclear' };
  async function renderHealth(out, head) {
    const [lib, evals] = await Promise.all([api('/api/sessions/library'), api('/api/sessions/evals').catch(() => [])]);
    const again = () => renderHealth(out, head);
    const judged = lib.sessions.filter((x) => x.source === 'ai' && x.completed);
    const count = (f) => judged.filter(f).length;
    const done = count((x) => x.completed === 'yes'); const partly = count((x) => x.completed === 'partly'); const notDone = count((x) => x.completed === 'no');
    const drifted = count((x) => x.onTask === 'drifted');
    const withErrors = lib.sessions.filter((x) => x.errors > 0).length;
    const avgTools = lib.sessions.length ? Math.round(lib.sessions.reduce((n, x) => n + (x.tools || 0), 0) / lib.sessions.length) : 0;
    const evalIDs = new Set(evals.map((e) => e.id));
    const risk = (x) => (x.completed === 'no' ? 3 : 0) + (x.onTask === 'drifted' ? 2 : 0) + (x.completed === 'partly' ? 1 : 0) + (x.errors > 5 ? 1 : 0);
    const rows = [...judged].sort((a, b) => risk(b) - risk(a) || new Date(b.updated) - new Date(a.updated)).slice(0, S.healthShown || 25);
    const makeEval = async (x) => {
      try { await api('/api/sessions/' + x.id + '/eval', { method: 'POST' }); toast('Eval case saved for "' + x.title + '"'); again(); } catch (e) { toast(e.message, true); }
    };
    const copyEval = async (x) => { try { await writeClip(JSON.stringify(await api('/api/sessions/' + x.id + '/eval'), null, 2)); toast('Eval case copied'); } catch (e) { toast(e.message, true); } };
    const bar = (parts) => { const total = parts.reduce((n, p) => n + p[1], 0) || 1; const b = h('div', { class: 'stackbar', role: 'img', 'aria-label': parts.map((p) => p[0] + ' ' + p[1]).join(', ') }); for (const [label, n, cls] of parts) { if (!n) continue; const i = h('i', { class: cls, title: label + ': ' + n }); i.style.width = (n / total * 100) + '%'; b.append(i); } return b; };
    out.replaceChildren(head(refreshBtn(again)),
      optimalCard(lib),
      judged.length ? null : h('section', { class: 'lib-card' }, h('b', {}, 'No sessions have been checked yet'), h('p', { class: 'faint small' }, 'When Cully names your sessions it also judges whether each one finished its job and stayed on task. Start that on the Session library page.'), h('a', { class: 'btn', href: '#/dashboard/library' }, icon('spark'), 'Open the session library')),
      h('div', { class: 'kpis' }, ...[['Completed', done + ' of ' + judged.length], ['Drifted from the task', String(drifted)], ['Had tool errors', String(withErrors)], ['Tools per session', String(avgTools)], ['Eval cases', String(evals.length)]].map(([k, v]) => h('div', { class: 'kpi' }, h('b', {}, v), h('span', {}, k)))),
      judged.length ? h('section', { class: 'lib-card' }, h('b', {}, 'How sessions ended'), bar([['Completed', done, 'good'], ['Partly done', partly, 'mid'], ['Not completed', notDone, 'bad'], ['Unclear', judged.length - done - partly - notDone, 'dim']]),
        h('div', { class: 'row legend' }, h('span', { class: 'chip ok' }, 'Completed ' + done), h('span', { class: 'chip' }, 'Partly ' + partly), h('span', { class: 'chip warn' }, 'Not completed ' + notDone))) : null,
      judged.length ? h('h3', { class: 'section-h' }, 'Sessions that need a look first') : null,
      rows.length ? h('ul', { class: 'hrows' }, ...rows.map((x) => {
        return h('li', { class: 'hrow' },
          ring(x.health, 46),
          h('div', { class: 'hmain' },
            h('div', { class: 'ht' }, harnessChip(x.agent), sessionLink(x), h('span', { class: 'chip ' + (x.completed === 'yes' ? 'ok' : x.completed === 'no' ? 'warn' : '') }, DONE_LABEL[x.completed] || 'Unclear'), x.onTask === 'drifted' ? h('span', { class: 'chip warn' }, 'Drifted') : null),
            x.signals && x.signals.length ? h('p', { class: 'hdesc' }, x.signals[0]) : null,
            h('div', { class: 'row' }, h('span', { class: 'chip' }, icon('folder'), x.name), h('span', { class: 'chip' }, ago(x.updated)),
              h('span', { class: 'chip' }, (x.tools || 0) + ' tool calls'), x.errors ? h('span', { class: 'chip warn' }, x.errors + ' errors') : null)),
          h('div', { class: 'row hact' },
            evalIDs.has(x.id) ? h('button', { class: 'btn ghost small', onclick: () => copyEval(x) }, icon('copy'), 'Copy eval case')
              : h('button', { class: 'btn small', disabled: x.completed !== 'yes' || !x.task, title: x.completed === 'yes' ? 'Save this session as a test case for future agents' : 'Only completed sessions can become an eval', onclick: () => makeEval(x) }, icon('check'), 'Make an eval')));
      })) : null,
      judged.length > rows.length ? h('div', { class: 'row' }, h('button', { class: 'btn ghost', onclick: () => { S.healthShown = (S.healthShown || 25) + 25; again(); } }, 'Show more')) : null,
      h('section', { class: 'lib-card' }, h('b', {}, 'About evals'), h('p', { class: 'faint small' }, 'An eval case turns a session that finished its job into a test: the goal in one sentence, how to tell it was met, and what the session actually did. It holds no prompts or replies. Cases are kept in the local database.')));
  }

  // ---------- session library ----------
  // The page's content security policy forbids inline styles, so widths are set through the CSSOM.
  const fill = (pct) => { const i = h('i', {}); i.style.width = pct + '%'; return i; };
  const dur = (sec) => { const m = Math.round(sec / 60); if (m < 1) return 'under a minute'; if (m < 60) return m + ' min'; return Math.floor(m / 60) + ' h ' + (m % 60) + ' min'; };
  // Everything here is worked out from the session list the page already has.
  function libraryInsights(lib) {
    const all = lib.sessions;
    const judged = all.filter((x) => x.source === 'ai' && x.completed);
    const day = (iso) => new Date(iso).toISOString().slice(0, 10);
    // The last 14 days: sessions and hours worked per day.
    const days = [];
    for (let i = 13; i >= 0; i--) { const d = new Date(Date.now() - i * 864e5); days.push({ key: d.toISOString().slice(0, 10), label: d.toLocaleDateString(undefined, { weekday: 'short' }), n: 0, sec: 0 }); }
    for (const x of all) { const d = days.find((e) => e.key === day(x.updated)); if (d) { d.n += 1; d.sec += x.activeSec; } }
    const maxSec = Math.max(1, ...days.map((d) => d.sec));
    const week = days.slice(7).reduce((n, d) => n + d.sec, 0); const prior = days.slice(0, 7).reduce((n, d) => n + d.sec, 0);
    const chart = h('div', { class: 'daychart', role: 'img', 'aria-label': 'Time worked per day over the last 14 days' }, ...days.map((d) => {
      const bar = h('i', { title: d.label + ': ' + d.n + (d.n === 1 ? ' session, ' : ' sessions, ') + dur(d.sec) }); bar.style.height = Math.max(d.sec ? 6 : 2, Math.round((d.sec / maxSec) * 100)) + '%';
      return h('span', { class: 'dc' + (d.sec ? '' : ' none') }, bar, h('small', {}, d.label.slice(0, 2)));
    }));
    // Outcomes, health, tools, where sessions started.
    const done = judged.filter((x) => x.completed === 'yes').length; const partly = judged.filter((x) => x.completed === 'partly').length; const notDone = judged.filter((x) => x.completed === 'no').length;
    const avgHealth = judged.length ? Math.round(judged.reduce((n, x) => n + x.health, 0) / judged.length) : 0;
    const tools = {}; for (const x of all) for (const [k, v] of Object.entries(x.toolNames || {})) tools[k] = (tools[k] || 0) + v;
    const topTools = Object.entries(tools).sort((a, b) => b[1] - a[1]).slice(0, 6); const maxTool = Math.max(1, ...topTools.map((x) => x[1]));
    const origins = {}; for (const x of all) { const k = x.origin || 'Unknown'; origins[k] = (origins[k] || 0) + 1; }
    const attention = judged.filter((x) => x.health < 70).sort((a, b) => a.health - b.health).slice(0, 4);
    const toolCalls = all.reduce((n, x) => n + (x.tools || 0), 0); const errors = all.reduce((n, x) => n + (x.errors || 0), 0);
    const unverified = all.filter((x) => x.unverified).length; const loops = all.filter((x) => (x.maxRepeat || 0) >= 3).length;
    const trend = prior ? Math.round(((week - prior) / prior) * 100) : null;
    const kpis = [['This week', dur(week) + (trend == null ? '' : (trend >= 0 ? ', up ' : ', down ') + Math.abs(trend) + '%')], ['Tool calls', fmtTokens(toolCalls) + (errors ? ', ' + Math.round(errors * 100 / Math.max(1, toolCalls)) + '% failed' : '')]];
    if (judged.length) kpis.push(['Finished the job', Math.round(done * 100 / judged.length) + '%'], ['Average health', String(avgHealth)]);
    if (unverified) kpis.push(['Changed files, no tests', String(unverified)]);
    if (loops) kpis.push(['Repeated a call 3+ times', String(loops)]);
    const card = h('section', { class: 'lib-card' }, h('b', {}, 'Your last two weeks'), chart,
      h('div', { class: 'insight-grid' },
        h('div', {}, h('span', { class: 'opt-label' }, 'Most used tools'), h('ul', { class: 'fbars' }, ...topTools.map(([k, v]) => h('li', {}, h('span', { class: 'fname mono' }, k), h('span', { class: 'fbar' }, fill(Math.round(v / maxTool * 100))), h('span', { class: 'faint small' }, fmtTokens(v)))))),
        h('div', {}, h('span', { class: 'opt-label' }, 'Where sessions start'), h('ul', { class: 'fbars' }, ...Object.entries(origins).sort((a, b) => b[1] - a[1]).slice(0, 6).map(([k, v]) => h('li', {}, h('span', { class: 'fname' }, k), h('span', { class: 'fbar' }, fill(Math.round(v / all.length * 100))), h('span', { class: 'faint small' }, String(v)))))),
        judged.length ? h('div', {}, h('span', { class: 'opt-label' }, 'How sessions ended'), h('div', { class: 'stackbar' }, ...[['Completed', done, 'good'], ['Partly', partly, 'mid'], ['Not completed', notDone, 'bad'], ['Unclear', judged.length - done - partly - notDone, 'dim']].filter((p) => p[1]).map(([l, n, c]) => { const i = h('i', { class: c, title: l + ': ' + n }); i.style.width = (n / judged.length * 100) + '%'; return i; })),
          h('div', { class: 'row legend' }, h('span', { class: 'chip ok' }, 'Completed ' + done), h('span', { class: 'chip' }, 'Partly ' + partly), h('span', { class: 'chip warn' }, 'Not completed ' + notDone))) : null),
      attention.length ? h('div', {}, h('span', { class: 'opt-label' }, 'Look at these first'), h('ul', { class: 'attn-list' }, ...attention.map((x) => h('li', {}, ring(x.health, 34), h('div', {}, sessionLink(x), harnessChip(x.agent), h('div', { class: 'faint small' }, (x.signals || []).join('. ') || 'Low health'))))), h('a', { class: 'linkbtn', href: '#/dashboard/health' }, 'See all in Session health')) : null);
    return { kpis, card };
  }
  async function renderLibrary(out, head) {
    const lib = await api('/api/sessions/library');
    const t = lib.totals;
    const live = () => S.route.name === 'dashboard' && S.route.panel === 'library';
    const again = () => renderLibrary(out, head);
    const total = t.analyzed + t.pending;
    const pct = total ? Math.round((t.analyzed / total) * 100) : 0;
    const toggle = async (on) => { try { await api('/api/sessions/analysis', { method: 'POST', body: JSON.stringify({ on }) }); again(); } catch (e) { toast(e.message, true); } };
    const analysis = lib.analysis.on
      ? h('section', { class: 'lib-card' }, h('div', { class: 'row' }, h('b', {}, 'Analysing your sessions'), h('span', { class: 'faint' }, t.analyzed + ' of ' + total + ' done' + (t.open ? ', ' + t.open + (t.open === 1 ? ' is' : ' are') + ' open now' : '')), h('button', { class: 'btn ghost small sp', onclick: () => toggle(false) }, 'Pause')),
        h('div', { class: 'bar' }, fill(pct)),
        h('p', { class: 'faint small' }, 'Claude reads each finished session in the background and works out its goal, whether it was finished and stayed on task, where it looped or went unchecked, and what could go better, and gives it a title. It does up to ' + lib.analysis.perHour + ' an hour, newest first, so the first pass takes a while. A session still in use is left alone and analysed 15 minutes after its last activity' + (t.open ? ', which is why ' + t.open + (t.open === 1 ? ' is' : ' are') + ' waiting now (this page\'s own session counts)' : '') + '.' + (t.failed ? ' ' + t.failed + ' could not be analysed after two tries.' : '') + ' It uses your Claude plan.'))
      : h('section', { class: 'lib-card' }, h('b', {}, t.pending ? 'Analyse your ' + t.pending + ' past sessions' : 'All sessions are analysed'),
        h('p', { class: 'faint small' }, 'Claude can read each finished session in the background and work out its goal, whether it was finished and stayed on task, where it looped or went unchecked, and what could go better. Nothing is sent anywhere except to your own Claude, and folders you hide are skipped. It takes a while and uses your Claude plan.'),
        t.pending ? h('button', { class: 'btn', onclick: () => toggle(true) }, icon('spark'), 'Start analysing sessions') : null);
    const maxT = Math.max(1, ...lib.folders.map((f) => f.activeSec));
    const folders = h('section', { class: 'lib-card' }, h('b', {}, 'Where the time went'),
      h('ul', { class: 'fbars' }, ...lib.folders.slice(0, 12).map((f) => h('li', { title: f.cwd }, h('span', { class: 'fname' }, f.name), h('span', { class: 'fbar' }, fill(Math.max(2, Math.round((f.activeSec / maxT) * 100)))),
        h('span', { class: 'faint small' }, f.count + (f.count === 1 ? ' session, ' : ' sessions, ') + dur(f.activeSec))))));
    const shown = S.libShown || 30;
    const rows = lib.sessions.slice(0, shown).map((x) => h('li', { class: 'hrow' },
      h('div', { class: 'hmain' },
        h('div', { class: 'ht' }, harnessChip(x.agent), sessionLink(x)),
        x.description ? h('p', { class: 'hdesc' }, x.description) : null,
        h('div', { class: 'row' }, h('span', { class: 'chip' }, icon('folder'), x.name), x.origin ? h('span', { class: 'chip' }, x.origin) : null, x.branch ? h('span', { class: 'chip mono' }, icon('branch'), x.branch) : null,
          h('span', { class: 'chip' }, ago(x.updated)), h('span', { class: 'chip' }, dur(x.activeSec) + ' worked'),
          x.files ? h('span', { class: 'chip' }, x.files + (x.files === 1 ? ' file changed' : ' files changed')) : null,
          x.checks ? h('span', { class: 'chip' }, x.checks + (x.checks === 1 ? ' test run' : ' test runs')) : null,
          x.errors ? h('span', { class: 'chip warn' }, x.errors + (x.errors === 1 ? ' tool error' : ' tool errors')) : null),
        x.different && x.different !== 'Nothing notable.' ? h('p', { class: 'hdiff' }, h('b', {}, 'Could go better: '), x.different) : null)));
    const insights = libraryInsights(lib);
    out.replaceChildren(head(refreshBtn(again)),
      h('div', { class: 'kpis' }, ...[['Sessions', String(t.sessions)], ['Time worked', dur(t.activeSec)], ['Analysed', t.analyzed + ' of ' + total], ...insights.kpis].map(([k, v]) => h('div', { class: 'kpi' }, h('b', {}, v), h('span', {}, k)))),
      analysis, insights.card, await coachCard(again, true), folders,
      h('h3', { class: 'section-h' }, 'Sessions'),
      rows.length ? h('ul', { class: 'hrows' }, ...rows) : emptyCard('No sessions yet', 'Sessions you run appear here, with how long you worked and what you did.', 'Start a task', '#/'),
      lib.sessions.length > shown ? h('div', { class: 'row' }, h('button', { class: 'btn ghost', onclick: () => { S.libShown = shown + 30; again(); } }, 'Show more')) : null);
    if (lib.analysis.on) setTimeout(() => { if (live()) again(); }, 30000);
  }

  // ---------- the task overview ----------
  // Every task, open and closed, with how many sessions it has spawned and on which harnesses, whether it
  // has been saved to Cully memory, and what to do with it: continue it or close it. Closing keeps it.
  const TASK_LANES = [
    ['attention', 'Needs attention', 'Waiting for you, stuck, failed, or stopped at a limit or by a restart.'],
    ['running', 'Running', 'Working now, or starting.'],
    ['finished', 'Finished', 'Done or closed. Nothing is deleted.'],
  ];
  async function mountTasks() {
    const stage = $('stage'); stage.className = 'stage';
    const view = { period: S.taskPeriodAll || 'all', q: '' };
    S.taskLaneShut = S.taskLaneShut || {};
    const wrap = h('div', { class: 'home tasks-page' }); stage.replaceChildren(wrap);
    const shortDate = (iso) => new Date(iso).toLocaleString([], { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' });
    const sessionRow = (x) => {
      const st = taskPair(x);
      const when = x.ended && !x.ended.startsWith('0001') ? 'Started ' + shortDate(x.at) + ', ended ' + shortDate(x.ended) : 'Started ' + shortDate(x.at);
      const known = x.run && S.agents.some((a) => a.id === x.run);
      return h('li', { class: 'tsess' }, harnessChip(x.agent), x.role === 'handoff' ? h('span', { class: 'chip' }, 'Handoff') : null, h('code', { title: x.id }, (x.id || '').slice(0, 8)),
        x.state ? h('span', { class: 'chip fs-' + st[1] }, st[0]) : null, h('span', { class: 'faint small' }, when),
        x.turns ? h('span', { class: 'faint small' }, x.turns + (x.turns === 1 ? ' turn' : ' turns')) : null,
        x.cost ? h('span', { class: 'faint small' }, '$' + x.cost.toFixed(2)) : null,
        x.reason ? h('span', { class: 'faint small' }, 'Ended: ' + x.reason) : null,
        known ? h('a', { class: 'btn ghost small', href: '#/task/' + x.run }, 'Open') : null);
    };
    async function draw() {
      let all = [];
      try { all = await api('/api/tasks?range=' + view.period); } catch (e) { wrap.replaceChildren(h('div', { class: 'err' }, e.message)); return; }
      const q = view.q.trim().toLowerCase();
      const shown = all.filter((t) => !q || (t.title + ' ' + (t.summary || '') + ' ' + (t.last || '')).toLowerCase().includes(q));
      const by = (k) => shown.filter((t) => t.status === k);
      const sessions = shown.reduce((n, t) => n + (t.sessions || []).length, 0);
      const mem = memoryMode() === 'mcp';
      const per = ([k, l]) => h('button', { type: 'button', class: 'toggle' + (view.period === k ? ' on' : ''), onclick: () => { view.period = k; S.taskPeriodAll = k; draw(); } }, l);
      const search = h('input', { type: 'search', class: 'bsearch', placeholder: 'Search tasks', 'aria-label': 'Search tasks', value: view.q, autocomplete: 'off' });
      search.addEventListener('input', () => { view.q = search.value; const pos = search.selectionStart; draw().then(() => { const el = wrap.querySelector('.bsearch'); if (el) { el.focus(); el.setSelectionRange(pos, pos); } }); });
      const closeTask = async (t, closed) => { try { await api('/api/tasks/' + t.id + '/close', { method: 'POST', body: JSON.stringify({ closed }) }); await refresh(); draw(); } catch (e) { toast(e.message, true); } };
      const card = (t) => {
        const st = taskPair(t);
        const ss = t.sessions || [];
        const det = h('details', { class: 'rl tsessions' }, h('summary', {}, h('span', { class: 'rq' }, 'Sessions (' + ss.length + ')'), h('span', { class: 'faint small' }, 'see each')), h('ul', { class: 'evlist' }, ...ss.map(sessionRow)));
        const memChip = t.entries ? h('span', { class: 'chip fs-good', title: 'Summarised and saved to Cully memory (PostgreSQL, indexed by Mem0)' }, 'In memory' + (t.entries > 1 ? ' x' + t.entries : '')) : (mem ? h('span', { class: 'chip', title: 'It is summarised and saved once it has settled' }, 'Not saved yet') : h('span', { class: 'chip', title: 'Connect the Cully MCP to keep tasks in memory across machines' }, 'Local only'));
        return h('article', { class: 'tcard' + (t.closed ? ' closed' : '') },
          h('div', { class: 'row' }, h('b', { class: 'ttitle' }, t.title), t.closed ? h('span', { class: 'chip' }, 'Closed') : h('span', { class: 'chip fs-' + (st[1] === 'good' ? 'good' : st[1] === 'bad' ? 'bad' : st[1] === 'wait' ? 'wait' : 'info') }, st[0]), h('span', { class: 'sp' }), h('span', { class: 'faint small' }, ago(t.updated))),
          t.why ? h('p', { class: 'twhy' }, t.why) : null,
          h('p', { class: 'fprev' }, plainPreview(taskBlurb(t).text)),
          h('div', { class: 'row' }, t.repo ? h('span', { class: 'chip', title: t.repo }, icon('folder'), t.repo.split('/').filter(Boolean).pop()) : null, ...[...new Set(ss.map((x) => x.agent).filter(Boolean))].map((x) => harnessChip(x)),
            t.files ? h('span', { class: 'chip' }, t.files + (t.files === 1 ? ' file' : ' files')) : null, t.runs ? h('span', { class: 'chip' }, t.runs + (t.runs === 1 ? ' run' : ' runs')) : null, memChip),
          ss.length ? det : null,
          h('div', { class: 'row' },
            t.live ? h('a', { class: 'btn small', href: '#/task/' + t.live }, 'Open') : h('button', { class: 'btn small', onclick: () => continueTaskDialog(t) }, icon('send'), t.closed ? 'Reopen and continue' : 'Continue'),
            t.closed ? h('button', { class: 'btn ghost small', onclick: () => closeTask(t, false) }, 'Reopen')
              : h('button', { class: 'btn ghost small', title: 'Done with it: stops any run and moves it to Finished. Nothing is deleted.', onclick: () => closeTask(t, true) }, icon('check'), 'Close')));
      };
      const lane = ([k, label, hint]) => {
        const items = by(k);
        const d = h('details', { class: 'tlane tlane-' + k, open: !S.taskLaneShut[k] }, h('summary', {}, h('b', {}, label), h('span', { class: 'chip' }, String(items.length)), h('span', { class: 'faint small' }, hint)),
          items.length ? h('div', { class: 'tgrid' }, ...items.map(card)) : h('p', { class: 'faint small tempty' }, 'Nothing here.'));
        d.addEventListener('toggle', () => { S.taskLaneShut[k] = !d.open; });
        return d;
      };
      const tile = ([k, label]) => h('div', { class: 'kpi tkpi-' + k }, h('b', {}, String(by(k).length)), h('span', {}, label));
      const order = by('attention').length ? TASK_LANES : [TASK_LANES[1], TASK_LANES[0], TASK_LANES[2]];
      wrap.replaceChildren(h('h1', {}, 'Tasks'), h('p', { class: 'sub' }, 'Every task, grouped by where it stands. A task is made of sessions, on any harness. Continue one, or close it when you are done.'),
        h('div', { class: 'kpis tsummary' }, ...order.map(tile), h('div', { class: 'kpi' }, h('b', {}, String(sessions)), h('span', {}, 'Sessions in all'))),
        h('div', { class: 'row filters' }, ...[['today', 'Today'], ['week', 'This week'], ['all', 'All time']].map(per), h('span', { class: 'sp' }), search),
        shown.length ? h('div', { class: 'tlanes' }, ...order.map(lane)) : emptyCard('No tasks here', 'Tasks you start appear here, with every session they spawn.', 'Start a task', '#/'));
    }
    await draw();
  }

  // ---------- the harness page: everything about the program that runs the model ----------
  // What a server's state means, in words, and what to do about it.
  const SRV_WORD = { ok: 'Connected', auth: 'Needs sign-in', bad: 'Failed to connect', unchecked: 'Not checked yet' };
  const srvState = (st) => (!st ? 'unchecked' : /^connected/i.test(st) ? 'ok' : st === 'needs-auth' ? 'auth' : st === 'pending' ? 'unchecked' : 'bad');
  const SRV_FIX = { auth: 'Needs you to sign in.', bad: 'Claude could not connect just now.' };
  // What is wrong with a server and what to do, in steps you can follow. A server with no header
  // credentials set signs in with OAuth; one that has headers had them refused.
  function srvHelp(m) {
    const st = srvState(m.status);
    const cmd = (c) => h('div', { class: 'row' }, h('code', { class: 'cmdline' }, c), copyBtn('Copy', () => c));
    if (st === 'auth' && m.kind === 'connector') return h('div', { class: 'shelp' }, h('p', {}, 'This connector was added in claude.ai and has not been connected yet. Connect it in claude.ai under Settings, Connectors, or ignore it: tasks do not need it.'));
    if (st === 'auth' && !(m.headers || []).length) return h('div', { class: 'shelp' }, h('p', {}, h('b', {}, 'Why: '), 'no header credentials are set for this server in the Claude config, so it uses OAuth sign-in. Cully cannot do that sign-in for you; Claude Code does it once and remembers it.'),
      h('ol', {}, h('li', {}, 'In a terminal where Claude Code is installed, run:', cmd('claude')), h('li', {}, 'Type ', h('code', {}, '/mcp'), ', choose ', h('b', {}, m.name), ', then Authenticate, and finish in the browser.'), h('li', {}, 'Come back here and press Check again.')),
      h('p', { class: 'faint small' }, 'If this server takes a token instead of a browser sign-in, add it as a header and there will be no sign-in step:'), cmd('claude mcp add --transport http ' + m.name + ' ' + (m.kind === 'http' && m.target ? 'https://' + m.target : '<url>') + ' --scope user --header "Authorization: Bearer <token>"'));
    if (st === 'auth') return h('div', { class: 'shelp' }, h('p', {}, h('b', {}, 'Why: '), 'the server refused the credentials Claude sent in these headers: ', h('b', {}, (m.headers || []).join(', ')), '. The token may have expired, or the account may no longer have access. Replace the header value and check again.'), cmd('claude mcp remove ' + m.name + ' --scope user'), h('p', { class: 'faint small' }, 'then add it again with the new token (claude mcp add ... --header "Name: value").'));
    if (st === 'bad') return h('div', { class: 'shelp' }, h('p', {}, h('b', {}, 'Why: '), 'Claude could not connect when it last started. That is often temporary (the server was restarting, or the network dropped). Check again before changing anything.'), m.kind === 'stdio' ? h('p', { class: 'faint small' }, 'For a program, run it by hand in a terminal to see why it does not start.') : h('p', { class: 'faint small' }, 'If it keeps failing, check the address and that your network reaches it.'));
    return null;
  }
  function srvHost(m) {
    if (m.kind === 'http') { try { const u = new URL(m.target); return u.host + (u.pathname.length > 1 ? u.pathname.replace(/\/$/, '') : ''); } catch (_) { /* show it as it is */ } }
    return m.target || '';
  }
  // Whether a tool only reads, changes something, or deletes, from the words in its name. A guess to help
  // you scan a long list: the server's own description is the truth.
  const TOOL_DEL = new Set(['delete', 'remove', 'erase', 'drop', 'destroy', 'stop', 'cancel', 'purge', 'clean', 'unshare', 'uncertify', 'revoke']);
  const TOOL_CHG = new Set(['create', 'add', 'update', 'edit', 'move', 'transition', 'assign', 'upload', 'send', 'merge', 'play', 'retry', 'trigger', 'promote', 'resolve', 'approve', 'set', 'link', 'copy', 'publish', 'run', 'write', 'save', 'log', 'start', 'push', 'rebase', 'submit', 'commit', 'reply', 'complete', 'authenticate', 'share', 'certify', 'take', 'checkout', 'mirror', 'migrate', 'clone', 'upload']);
  const TOOL_READ = new Set(['get', 'list', 'search', 'read', 'describe', 'find', 'check', 'download', 'whoami', 'health', 'help', 'show', 'view', 'query', 'sample', 'validate', 'discover', 'wait', 'my', 'lookup', 'parse', 'collect', 'branchdiff', 'recall', 'recent', 'context', 'projects', 'readonly']);
  function toolKind(t) {
    if (t.readOnly) return 'read';
    const parts = t.name.toLowerCase().split(/[_\-.]/);
    if (parts.includes('readonly')) return 'read';
    for (const w of parts) { if (TOOL_DEL.has(w)) return 'delete'; if (TOOL_CHG.has(w)) return 'change'; if (TOOL_READ.has(w)) return 'read'; }
    return 'other';
  }
  const TOOL_KIND_WORD = { read: 'Reads', change: 'Changes things', delete: 'Deletes', other: 'Other' };
  // Group a long tool list by its service prefix (jira_, confluence_) when it has one, otherwise by what the tools do.
  function groupTools(tools) {
    if (tools.length <= 12) return [{ name: '', tools }];
    const first = {};
    for (const t of tools) { const w = t.name.split(/[_\-]/)[0]; first[w] = (first[w] || 0) + 1; }
    const verbs = (w) => TOOL_DEL.has(w) || TOOL_CHG.has(w) || TOOL_READ.has(w);
    const prefixes = Object.keys(first).filter((w) => first[w] >= 3 && !verbs(w));
    const groups = new Map();
    for (const t of tools) {
      const w = t.name.split(/[_\-]/)[0];
      const key = prefixes.includes(w) ? w : (prefixes.length ? 'other' : TOOL_KIND_WORD[toolKind(t)]);
      if (!groups.has(key)) groups.set(key, []);
      groups.get(key).push(t);
    }
    return [...groups.entries()].map(([name, ts]) => ({ name, tools: ts })).sort((a, b) => b.tools.length - a.tools.length);
  }

  const HARNESS_SECTIONS = [['overview', 'Overview'], ['permissions', 'Permissions'], ['servers', 'Connected servers'], ['tools', 'Tools'], ['skills', 'Skills'], ['hooks', 'Hooks'], ['more', 'Plugins and commands']];
  async function renderHarness() {
    const box = $('harness');
    const tasksAgent = (mcpUsed()[0] || { agent: 'claude' }).agent;
    const agent = S.harnessAgent || tasksAgent;
    const section = S.route.tab && HARNESS_SECTIONS.some((t) => t[0] === S.route.tab) ? S.route.tab : 'overview';
    const go = (a, t) => { S.harnessAgent = a; if (location.hash === '#/harness/' + t) renderHarness(); else location.hash = '#/harness/' + t; };
    const infos = {};
    try { await Promise.all(['claude', 'codex', 'cursor'].map(async (a) => { infos[a] = await api('/api/harness?agent=' + a); })); } catch (e) { box.replaceChildren(h('div', { class: 'err' }, e.message)); return; }
    if (S.route.name !== 'harness') return;
    const info = infos[agent];
    // 1. choose a harness
    const picker = h('div', { class: 'hpick', role: 'group', 'aria-label': 'Harness' }, ...['claude', 'codex', 'cursor'].map((a) => { const i = infos[a];
      return h('button', { type: 'button', class: 'hcard' + (a === agent ? ' on' : ''), 'aria-pressed': String(a === agent), onclick: () => go(a, a !== tasksAgent && section === 'permissions' ? 'overview' : section) },
        h('span', { class: 'hc-name' }, h('span', { class: 'dot ' + (i.installed ? 'working' : '') }), AGENT_LABEL[a]),
        h('span', { class: 'hc-ver faint small' }, i.installed ? i.version : 'Not found'),
        h('span', { class: 'hc-meta faint small' }, i.mcp.length + ' servers, ' + i.skills.length + ' skills'),
        a === tasksAgent ? h('span', { class: 'chip ok' }, 'Tasks run here') : null); }));
    // 2. choose a section
    const counts = { servers: info.mcp.length, skills: info.skills.length, hooks: info.hooks.length };
    const nav = h('nav', { class: 'hnav', 'aria-label': 'Harness sections' }, ...HARNESS_SECTIONS.filter((t) => (t[0] !== 'permissions' && t[0] !== 'tools') || info.tasksUseIt).map(([k, l]) => h('a', { href: '#/harness/' + k, class: k === section ? 'on' : '', 'aria-current': k === section ? 'page' : null, onclick: (e) => { e.preventDefault(); go(agent, k); } }, l, counts[k] != null ? h('span', { class: 'n' }, String(counts[k])) : null)));
    const body = h('div', { class: 'hbody' });
    const card = (title, sub, ...kids) => h('section', { class: 'lib-card' }, h('div', { class: 'hc-title' }, h('h3', {}, title), sub ? h('p', { class: 'faint small' }, sub) : null), ...kids);
    const tile = (n, l) => h('div', { class: 'btile' }, h('b', {}, n), h('span', {}, l));
    if (section === 'overview') {
      // What to look at first: things the page can see are wrong or wasteful, each with where to fix it.
      const finds = [];
      const need = info.mcp.filter((m) => srvState(m.status) === 'auth'); const bad = info.mcp.filter((m) => srvState(m.status) === 'bad');
      const link = (t) => (e) => { e.preventDefault(); go(agent, t); };
      if (!info.installed) finds.push(['alert', info.name + ' was not found on this machine.', null]);
      if (need.length) finds.push(['lock', need.length + (need.length === 1 ? ' server needs' : ' servers need') + ' you to sign in: ' + need.map((m) => m.name).join(', ') + '.', 'servers']);
      if (bad.length) finds.push(['alert', bad.length + (bad.length === 1 ? ' server' : ' servers') + ' failed to connect: ' + bad.map((m) => m.name).join(', ') + '.', 'servers']);
      const toolTotal = info.mcp.reduce((n, m) => n + (m.tools || 0), 0);
      if (info.tasksUseIt && toolTotal > 150) finds.push(['spark', 'Tasks load ' + info.mcp.length + ' servers with ' + toolTotal + ' tools. Each tool description is sent every turn, so choosing only the servers you need saves tokens.', 'servers']);
      if (info.mcp.length && info.mcp.every((m) => !m.status) && info.tasksUseIt) finds.push(['refresh', 'The servers have not been checked yet, so their state and tools are unknown.', 'servers']);
      if (!info.instructions.length) finds.push(['pencil', 'No instruction file (such as CLAUDE.md) was found for ' + info.name + '.', 'more']);
      body.append(card('What to look at', finds.length ? null : 'Nothing needs attention.',
        finds.length ? h('ul', { class: 'hrows' }, ...finds.map(([ic, text, sec]) => h('li', { class: 'hrow' }, h('div', { class: 'hmain row' }, icon(ic), h('span', {}, text)), sec ? h('div', { class: 'row hact' }, h('a', { class: 'btn ghost small', href: '#/harness/' + sec, onclick: link(sec) }, 'Open')) : null))) : null));
      // The three harnesses side by side.
      const cell = (a, f) => { try { return f(infos[a]); } catch (_) { return ''; } };
      const rows = [['Model', (i) => i.model || 'Default'], ['Permission mode', (i) => i.mode || 'Not set'], ['Servers connected', (i) => i.mcp.filter((m) => srvState(m.status) === 'ok').length + ' of ' + i.mcp.length], ['Skills', (i) => String(i.skills.length)], ['Hook events', (i) => String(i.hooks.length)], ['Plugins', (i) => String(i.plugins.length)], ['Instruction files', (i) => i.instructions.join(', ') || 'None']];
      body.append(card('The three harnesses side by side', 'Tasks run in ' + AGENT_LABEL[tasksAgent] + '. Sessions, the library and the advisor cover all three.',
        h('table', { class: 'ptable cmp' }, h('thead', {}, h('tr', {}, h('th', {}, ''), ...['claude', 'codex', 'cursor'].map((a) => h('th', { class: a === agent ? 'on' : '' }, AGENT_LABEL[a], infos[a].installed ? '' : ' (not found)')))),
          h('tbody', {}, ...rows.map(([label, f]) => h('tr', {}, h('td', { class: 'faint' }, label), ...['claude', 'codex', 'cursor'].map((a) => h('td', { class: a === agent ? 'on' : '' }, cell(a, f)))))))));
      // What Cully can do with each harness, and a warning when the installed Claude Code has dropped a flag Cully relies on.
      try {
        const cv = await api('/api/harness/capabilities');
        const pr = await api('/api/harness/probe').catch(() => null);
        const drift = (a) => ((pr && pr.harnesses.find((x) => x.agent === a)) || { drift: [] }).drift;
        const mark = (v, why, a, label) => h('div', {}, h('span', { class: 'cap cap-' + v, title: v === 'yes' ? 'Supported' : v === 'partial' ? 'Partly' : 'Not available' }, v === 'yes' ? 'Yes' : v === 'partial' ? 'Partly' : 'No'),
          ...drift(a).filter((d) => d.cap === label).map((d) => h('span', { class: 'cap cap-drift', title: 'The installed program no longer lists ' + d.flag + ', which Cully uses to ' + d.for }, 'Drift: ' + d.flag)),
          why ? h('div', { class: 'faint small' }, why) : null);
        const probeBar = h('div', { class: 'row probebar' }, h('span', { class: 'faint small' }, pr ? 'Checked ' + ago(new Date(pr.at * 1000).toISOString()) + '. ' + (pr.harnesses.some((x) => x.drift.length) ? 'Some installed programs have dropped a flag Cully relies on.' : 'Every flag Cully relies on is still there.') : 'Not checked yet.'),
          h('button', { class: 'btn ghost small', onclick: (e) => { e.target.disabled = true; e.target.textContent = 'Checking'; api('/api/harness/probe/refresh', { method: 'POST' }).then(() => route(), () => { e.target.disabled = false; e.target.textContent = 'Check now'; toast('The check failed', true); }); } }, 'Check now'));
        body.append(card('What Cully can do with each harness', 'Each feature works through what the harness offers, so it is checked against the installed program, not assumed.',
          cv.missing.length ? h('div', { class: 'ctxnote' }, icon('alert'), h('div', {}, h('b', {}, 'Your Claude Code is missing ' + (cv.missing.length === 1 ? 'a flag' : cv.missing.length + ' flags') + ' Cully relies on'), h('div', { class: 'faint small' }, cv.missing.join('; ') + '. The features that need them are switched off until it is updated.'))) : (cv.flagsChecked ? h('p', { class: 'faint small' }, 'Every flag Cully uses is present in this Claude Code' + (cv.version ? ' (' + cv.version + ')' : '') + '.') : null),
          probeBar,
          h('table', { class: 'ptable cmp' }, h('thead', {}, h('tr', {}, h('th', {}, 'Feature'), ...['claude', 'codex', 'cursor'].map((a) => h('th', {}, AGENT_LABEL[a])), h('th', {}, ''))),
            h('tbody', {}, ...cv.capabilities.map((c) => h('tr', {}, h('td', {}, h('b', {}, c.label), h('div', { class: 'faint small' }, c.why)), h('td', {}, mark(c.claude, c.claudeWhy, 'claude', c.label)), h('td', {}, mark(c.codex, c.codexWhy, 'codex', c.label)), h('td', {}, mark(c.cursor, c.cursorWhy, 'cursor', c.label)), h('td', { class: 'faint small' }, c.note || '')))))));
      } catch (_) { /* optional */ }
      body.append(card(info.name + (info.installed ? ' ' + info.version : ''), info.tasksUseIt ? 'Every task Cully starts runs in this harness: it carries out the work, uses the servers and skills below, and obeys the permissions.' : 'Cully reads this harness\'s setup so you can compare it with the others. Tasks started here run in ' + AGENT_LABEL[tasksAgent] + '.',
        h('div', { class: 'btiles' }, tile(info.model || 'Default', 'Model'), tile(info.mode || 'Not set', 'Its permission mode'), tile(info.allow + ' / ' + info.deny, 'Allow and deny rules'), tile(String(info.mcp.length), 'Connected servers'), tile(String(info.skills.length), 'Skills'), tile(String(info.hooks.length), 'Hook events'), tile(String(info.plugins.length), 'Plugins'), tile(String(info.commands.length), 'Commands')),
        h('div', { class: 'row' }, ...info.instructions.map((x) => h('span', { class: 'chip mono' }, x))), ...info.notes.map((n) => h('p', { class: 'faint small' }, n))));
    } else if (section === 'permissions') {
      const holder = h('div', {}); body.append(holder); renderPermissions(holder, () => document.createTextNode(''));
    } else if (section === 'servers') {
      const tools = info.tasksUseIt ? await api('/api/admin/tools').catch(() => null) : null;
      const chosen = new Set(tools ? tools.mcp.only : []);
      const initial = [...chosen].sort().join(',');
      const bar = h('div', { class: 'savebar', hidden: true });
      const summary = h('p', { class: 'faint small' });
      const syncBar = () => {
        const changed = [...chosen].sort().join(',') !== initial;
        summary.textContent = info.tasksUseIt ? (chosen.size ? 'Tasks load ' + chosen.size + ' of ' + own.length + ' servers.' : 'Tasks load all ' + own.length + ' servers.') + ' Each server adds its tool descriptions to every turn, so loading fewer saves tokens.' : 'Servers this harness has set up.';
        bar.hidden = !changed;
        if (info.tasksUseIt && toolsLoaded()) summary.textContent += ' That is ' + toolsLoaded() + ' tools in every turn.';
        for (const sw of body.querySelectorAll('[role=switch]')) sw.setAttribute('aria-checked', String(!chosen.size || chosen.has(sw.dataset.name)));
        for (const c of body.querySelectorAll('.srow')) c.classList.toggle('off', chosen.size > 0 && !chosen.has(c.dataset.name) && !c.querySelector('.sdot.connector'));
      };
      bar.append(h('span', {}, 'You have unsaved changes.'), h('button', { class: 'btn', onclick: async () => {
        try { await api('/api/admin/tools', { method: 'PUT', body: JSON.stringify({ presets: Object.fromEntries(tools.presets.map((p) => [p.id, p.on])), allow: tools.allow, deny: tools.deny, mcpOnly: [...chosen], askRisky: tools.askRisky }) }); toast('Saved. New tasks load ' + (chosen.size ? chosen.size + ' servers' : 'all servers') + '.'); renderHarness(); } catch (e) { toast(e.message, true); }
      } }, icon('check'), 'Save'), h('button', { class: 'btn ghost', onclick: () => renderHarness() }, 'Discard'));
      const order = { auth: 0, bad: 1, ok: 2, unchecked: 3 };
      const own = info.mcp.filter((m) => m.kind !== 'connector'); const conns = info.mcp.filter((m) => m.kind === 'connector');
      const byState = (x, y) => order[srvState(x.status)] - order[srvState(y.status)] || x.name.localeCompare(y.name);
      const grid = h('div', { class: 'srows' });
      const sfilter = { q: '', st: S.srvFilter || '' };
      S.srvOpen = S.srvOpen || new Set();
      const rowFor = (m) => {
        const loaded = () => !chosen.size || chosen.has(m.name);
        const st = srvState(m.status);
        const sw = info.tasksUseIt && m.kind !== 'connector' ? h('button', { type: 'button', class: 'switch', role: 'switch', 'aria-checked': String(loaded()), 'aria-label': 'Tasks load ' + m.name, 'data-name': m.name, title: 'Whether tasks load this server', onclick: (e) => { e.stopPropagation(); if (!chosen.size) for (const x of own) chosen.add(x.name); if (chosen.has(m.name)) chosen.delete(m.name); else chosen.add(m.name); if (chosen.size === own.length) chosen.clear(); syncBar(); } }, h('span', { class: 'knob' })) : null;
        const help = srvHelp(m);
        const detail = h('div', { class: 'sdetail', hidden: !S.srvOpen.has(m.name) },
          h('div', { class: 'row' }, h('span', { class: 'faint small' }, (m.kind === 'http' ? 'Web address ' : m.kind === 'connector' ? '' : 'Program ') + (m.target || '')), m.headers && m.headers.length ? h('span', { class: 'faint small' }, 'Headers set: ' + m.headers.join(', ')) : null),
          help, m.tools ? h('p', { class: 'faint small' }, m.tools + ' tools' + (m.prompts ? ' and ' + m.prompts + ' prompts' : '') + ' reported at the last check.') : null,
          h('div', { class: 'row' }, m.kind !== 'connector' && m.tools ? h('button', { class: 'btn small', onclick: () => openServerModal(agent, m, info) }, icon('term'), 'See its tools') : null,
            m.kind !== 'connector' && m.tools && info.tasksUseIt ? h('a', { class: 'btn ghost small', href: '#/harness/tools', onclick: (e) => { e.preventDefault(); S.toolsServer = m.name; go(agent, 'tools'); } }, 'Set Ask, Allow or Block') : null));
        const action = st === 'auth' ? h('button', { class: 'btn small', onclick: (e) => { e.stopPropagation(); S.srvOpen.add(m.name); drawGrid(); } }, icon('lock'), 'How to sign in') : st === 'bad' || st === 'unchecked' ? h('button', { class: 'btn ghost small', onclick: async (e) => { e.stopPropagation(); e.currentTarget.disabled = true; e.currentTarget.textContent = 'Checking…'; try { await api('/api/harness/probe', { method: 'POST' }); renderHarness(); } catch (x) { toast(x.message, true); renderHarness(); } } }, icon('refresh'), st === 'bad' ? 'Check again' : 'Check') : null;
        const toggle = () => { if (S.srvOpen.has(m.name)) S.srvOpen.delete(m.name); else S.srvOpen.add(m.name); detail.hidden = !S.srvOpen.has(m.name); row.classList.toggle('open', S.srvOpen.has(m.name)); };
        const row = h('div', { class: 'srow st-' + st + (S.srvOpen.has(m.name) ? ' open' : ''), 'data-name': m.name },
          h('div', { class: 'srow-main', role: 'button', tabindex: '0', 'aria-expanded': String(S.srvOpen.has(m.name)), onclick: toggle, onkeydown: (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); toggle(); } } },
            h('span', { class: 'sdot ' + st, title: SRV_WORD[st] }), h('div', { class: 'sname' }, h('b', {}, m.name), h('span', { class: 'faint small', title: m.target || '' }, srvHost(m))),
            h('span', { class: 'chip fs-' + (st === 'ok' ? 'good' : st === 'bad' ? 'bad' : st === 'auth' ? 'wait' : 'info') }, SRV_WORD[st]),
            m.tools ? h('span', { class: 'chip' }, m.tools + (m.tools === 1 ? ' tool' : ' tools')) : h('span', { class: 'chip faint' }, ''), h('span', { class: 'sp' }), action, sw, h('span', { class: 'chev faint' }, '›')),
          detail);
        return row;
      };
      const drawGrid = () => {
        const t = sfilter.q.trim().toLowerCase();
        const ok = (m) => (!sfilter.st || (sfilter.st === 'attention' ? ['auth', 'bad'].includes(srvState(m.status)) : srvState(m.status) === sfilter.st)) && (!t || (m.name + ' ' + srvHost(m)).toLowerCase().includes(t));
        const mine = own.filter(ok).sort(byState);
        const need = mine.filter((m) => ['auth', 'bad'].includes(srvState(m.status))); const rest = mine.filter((m) => !need.includes(m));
        const cs = conns.filter(ok).sort(byState);
        const grp = (title, sub, items, open) => items.length ? h('section', { class: 'sgroup' }, h('div', { class: 'sgroup-h' }, h('b', {}, title), h('span', { class: 'faint small' }, sub)), ...items.map(rowFor)) : null;
        const connNeed = cs.filter((m) => srvState(m.status) === 'auth').length;
        const connBlock = cs.length ? h('details', { class: 'sgroup', ...(t || sfilter.st ? { open: true } : {}) }, h('summary', { class: 'sgroup-h' }, h('b', {}, 'Connectors from claude.ai'), h('span', { class: 'faint small' }, cs.length + ' connectors' + (connNeed ? ', ' + connNeed + ' not connected (tasks do not need them)' : ''))), ...cs.map(rowFor)) : null;
        const nodes = [grp('Needs you', need.length + (need.length === 1 ? ' server' : ' servers'), need), grp(need.length ? 'Working' : 'Your servers', rest.length + (rest.length === 1 ? ' server' : ' servers'), rest), connBlock].filter(Boolean);
        grid.replaceChildren(...(nodes.length ? nodes : [h('p', { class: 'faint small' }, 'No server matches.')]));
        syncBar();
      };
      const cnt = (f) => own.filter((m) => f(srvState(m.status))).length;
      const sq = h('input', { type: 'search', class: 'bsearch', placeholder: 'Search servers', 'aria-label': 'Search servers', autocomplete: 'off' });
      sq.addEventListener('input', () => { sfilter.q = sq.value; drawGrid(); });
      const fchips = h('div', { class: 'row filters' });
      const drawChips = () => fchips.replaceChildren(...[['', 'All', own.length], ['ok', 'Connected', cnt((x) => x === 'ok')], ['attention', 'Need a look', cnt((x) => x === 'auth' || x === 'bad')], ['unchecked', 'Not checked', cnt((x) => x === 'unchecked')]].filter(([k, , n]) => !k || n).map(([k, l, n]) => h('button', { type: 'button', class: 'toggle' + (sfilter.st === k ? ' on' : ''), onclick: () => { sfilter.st = k; S.srvFilter = k; drawChips(); drawGrid(); } }, l + ' ' + n)));
      drawChips();
      const toolsLoaded = () => own.filter((m) => !chosen.size || chosen.has(m.name)).reduce((n, m) => n + (m.tools || 0), 0);
      const unchecked = info.tasksUseIt && info.mcp.length && info.mcp.every((m) => !m.status);
      const check = unchecked ? h('div', { class: 'ctxnote' }, icon('alert'), h('div', {}, h('b', {}, 'Cully has not checked these servers yet'), h('div', { class: 'faint small' }, 'Claude reports which servers connected, and what each offers, when it starts. Cully can start it once for a moment to find out; it costs a fraction of a cent.')),
        h('button', { class: 'btn small', onclick: async (e) => { e.currentTarget.disabled = true; e.currentTarget.textContent = 'Checking…'; try { const r = await api('/api/harness/probe', { method: 'POST' }); toast('Checked ' + r.servers + ' servers'); renderHarness(); } catch (x) { toast(x.message, true); renderHarness(); } } }, 'Check now')) : null;
      body.append(card('Connected servers', 'Click a server to see its tools, prompts and resources. Ones that need you come first.', check, summary, info.mcp.length ? h('div', { class: 'row' }, sq, fchips) : null, info.mcp.length ? grid : h('p', { class: 'faint small' }, 'No servers are connected to ' + info.name + '.'), bar));
      drawGrid();
    } else if (section === 'tools') {
      const [tv, pol] = await Promise.all([api('/api/harness/tools?agent=' + agent), api('/api/admin/tools')]);
      const ruleId = (srv, t) => 'mcp__' + srv + '__' + t;
      const cur = new Map(); const orig = new Map();
      for (const sv of tv.servers) for (const t of sv.tools) { const id = ruleId(sv.name, t.name); const v = pol.deny.includes(id) ? 'block' : pol.allow.includes(id) ? 'allow' : ''; cur.set(id, v); orig.set(id, v); }
      const view = { q: '', kind: '', acc: '', server: S.toolsServer || '' };
      S.toolsServer = '';
      const changed = () => [...cur].filter(([id, v]) => orig.get(id) !== v).length;
      const tiles = h('div', { class: 'kpis' });
      const bar = h('div', { class: 'savebar', hidden: true });
      const list = h('div', { class: 'tlist' });
      const drawTiles = () => {
        const all = [...cur.values()]; const n = (v) => all.filter((x) => x === v).length;
        tiles.replaceChildren(...[[all.length, 'Tools on connected servers'], [n('allow'), 'Allowed'], [n(''), 'Ask me'], [n('block'), 'Blocked']].map(([v, l]) => h('div', { class: 'kpi' }, h('b', {}, String(v)), h('span', {}, l))));
        const c = changed(); bar.hidden = !c;
        bar.replaceChildren(h('span', {}, c + (c === 1 ? ' change' : ' changes') + ' not saved.'), h('button', { class: 'btn', onclick: save }, icon('check'), 'Save'), h('button', { class: 'btn ghost', onclick: () => { for (const [id, v] of orig) cur.set(id, v); drawTiles(); drawList(); } }, 'Discard'));
      };
      const save = async () => {
        const managed = new Set(cur.keys());
        const allow = [...pol.allow.filter((r) => !managed.has(r)), ...[...cur].filter(([, v]) => v === 'allow').map(([id]) => id)];
        const deny = [...pol.deny.filter((r) => !managed.has(r)), ...[...cur].filter(([, v]) => v === 'block').map(([id]) => id)];
        try { await api('/api/admin/tools', { method: 'PUT', body: JSON.stringify({ presets: Object.fromEntries(pol.presets.map((p) => [p.id, p.on])), allow, deny, mcpOnly: pol.mcp.only, askRisky: pol.askRisky }) }); toast('Saved. New tasks use these settings.'); renderHarness(); } catch (e) { toast(e.message, true); }
      };
      const setOne = (id, v) => { cur.set(id, v); drawTiles(); };
      const seg = (id) => { const row = h('div', { class: 'seg acc', role: 'group', 'aria-label': 'Access' });
        const paint = () => { for (const b of row.children) b.className = (b.dataset.v === cur.get(id) ? 'on ' : '') + 'acc-' + (b.dataset.v || 'ask'); };
        for (const [v, l, tip] of [['', 'Ask', 'The task pauses and asks you in its window'], ['allow', 'Allow', 'The task may use it without asking'], ['block', 'Block', 'The task can never use it']]) { const b = h('button', { type: 'button', 'data-v': v, title: tip, onclick: () => { setOne(id, v); paint(); } }, l); row.append(b); }
        paint(); return row; };
      const toolRow = (sv, t) => { const k = toolKind(t); const id = ruleId(sv.name, t.name);
        return h('div', { class: 'trow' }, h('details', { class: 'tool' }, h('summary', {}, h('code', {}, t.name), h('span', { class: 'chip tk-' + k }, TOOL_KIND_WORD[k]), t.description ? h('span', { class: 'td faint small' }, t.description.length > 120 ? t.description.slice(0, 120) + '…' : t.description) : null),
          t.description ? h('p', { class: 'tdesc' }, t.description) : h('p', { class: 'faint small' }, 'No description yet. Use Load descriptions on the server.'),
          t.params && t.params.length ? h('table', { class: 'ptable' }, h('thead', {}, h('tr', {}, ...['Parameter', 'Type', 'Needed', 'What it is'].map((c) => h('th', {}, c)))), h('tbody', {}, ...t.params.map((p) => h('tr', {}, h('td', {}, h('code', {}, p.name)), h('td', { class: 'faint' }, p.type || ''), h('td', {}, p.required ? 'Required' : 'Optional'), h('td', { class: 'faint' }, p.description || ''))))) : null),
          seg(id)); };
      const loadDesc = async (sv, btn) => { btn.disabled = true; btn.textContent = 'Reading ' + sv.name + '…'; try { await api('/api/harness/server/details', { method: 'POST', body: JSON.stringify({ agent, name: sv.name }) }); toast('Loaded ' + sv.name); renderHarness(); } catch (e) { toast(e.message, true); btn.disabled = false; btn.textContent = 'Load descriptions'; } };
      const drawList = () => {
        const t = view.q.trim().toLowerCase();
        const cards = tv.servers.filter((sv) => !view.server || sv.name === view.server).map((sv) => {
          const rows = sv.tools.filter((x) => (!view.kind || toolKind(x) === view.kind) && (!view.acc || (cur.get(ruleId(sv.name, x.name)) || 'ask') === view.acc) && (!t || (sv.name + ' ' + x.name + ' ' + (x.description || '')).toLowerCase().includes(t)));
          if (!rows.length) return null;
          const bulk = (label, pick, v) => h('button', { type: 'button', class: 'btn ghost small', onclick: () => { for (const x of sv.tools) if (pick(x)) cur.set(ruleId(sv.name, x.name), v); drawTiles(); drawList(); } }, label);
          const lb = sv.canLoad && !sv.detailsFrom ? h('button', { class: 'btn ghost small', title: 'Asks your Claude once, for this server only; uses a few thousand tokens' }, icon('spark'), 'Load descriptions') : null;
          if (lb) lb.addEventListener('click', () => loadDesc(sv, lb));
          const st = srvState(sv.status);
          return h('details', { class: 'tgroup', ...(t || view.kind || view.acc || tv.servers.length <= 3 ? { open: true } : {}) },
            h('summary', {}, h('b', {}, sv.name), h('span', { class: 'chip fs-' + (st === 'ok' ? 'good' : st === 'bad' ? 'bad' : st === 'auth' ? 'wait' : 'info') }, SRV_WORD[st]), h('span', { class: 'faint small' }, rows.length + (rows.length === sv.tools.length ? '' : ' of ' + sv.tools.length) + ' tools')),
            h('div', { class: 'row tbulk' }, bulk('Allow all that read', (x) => toolKind(x) === 'read', 'allow'), bulk('Block all that delete', (x) => toolKind(x) === 'delete', 'block'), bulk('Ask for everything', () => true, ''), lb),
            ...rows.map((x) => toolRow(sv, x)));
        }).filter(Boolean);
        list.replaceChildren(...(cards.length ? cards : [h('p', { class: 'faint small' }, tv.servers.length ? 'No tool matches.' : 'No servers are connected.')]));
      };
      const q = h('input', { type: 'search', class: 'bsearch', placeholder: 'Search tools by name or what they do', 'aria-label': 'Search tools', autocomplete: 'off' });
      q.addEventListener('input', () => { view.q = q.value; drawList(); });
      const chips = h('div', { class: 'row filters' });
      const drawChips = () => chips.replaceChildren(...[['kind', '', 'All'], ['kind', 'read', 'Reads'], ['kind', 'change', 'Changes things'], ['kind', 'delete', 'Deletes'], ['acc', 'allow', 'Allowed'], ['acc', 'ask', 'Ask me'], ['acc', 'block', 'Blocked']].map(([f, v, l]) => h('button', { type: 'button', class: 'toggle' + ((f === 'kind' ? view.kind : view.acc) === v && (v || (f === 'kind' && !view.acc && !view.kind)) ? ' on' : ''), onclick: () => { if (!v) { view.kind = ''; view.acc = ''; } else view[f] = view[f] === v ? '' : v; drawChips(); drawList(); } }, l)));
      const srvSel = h('select', { 'aria-label': 'Server' }, h('option', { value: '' }, 'All servers'), ...tv.servers.map((sv) => h('option', Object.assign({ value: sv.name }, view.server === sv.name ? { selected: true } : {}), sv.name + ' (' + sv.tools.length + ')')));
      srvSel.addEventListener('change', () => { view.server = srvSel.value; drawList(); });
      drawChips();
      const needDesc = tv.servers.filter((sv) => sv.canLoad && !sv.detailsFrom);
      const allBtn = needDesc.length ? h('button', { class: 'btn small', title: 'Reads every server\'s tool definitions through your Claude: a few thousand tokens per server' }, icon('spark'), 'Load all descriptions (' + needDesc.length + ' servers)') : null;
      if (allBtn) allBtn.addEventListener('click', async () => { allBtn.disabled = true; for (let i = 0; i < needDesc.length; i++) { allBtn.textContent = 'Reading ' + needDesc[i].name + ' (' + (i + 1) + ' of ' + needDesc.length + ')…'; try { await api('/api/harness/server/details', { method: 'POST', body: JSON.stringify({ agent, name: needDesc[i].name }) }); } catch (e) { toast(needDesc[i].name + ': ' + e.message, true); } } renderHarness(); });
      body.append(card('Tools', 'Every tool on the servers tasks connect to. Choose, tool by tool, whether a task may use it freely, must ask you first, or never gets it.',
        h('div', { class: 'ctxnote' }, icon('lock'), h('div', {}, h('b', {}, 'Ask, Allow or Block'), h('div', { class: 'faint small' }, 'Ask is the default: the task stops and a card in its window shows exactly what it wants to do. Allow lets it through without asking. Block is final: a blocked tool is never offered to the task.'))),
        !tv.known ? h('p', { class: 'faint small' }, 'Tool names appear after a task has started or the servers have been checked. Check them on the Connected servers page.') : null,
        tiles, h('div', { class: 'row' }, q, srvSel, allBtn), chips, list, bar,
        tv.builtin.length ? h('details', { class: 'rl' }, h('summary', {}, h('span', { class: 'rq' }, 'Built-in tools'), h('span', { class: 'faint small' }, tv.builtin.length + ' from the harness itself')), h('div', { class: 'rbody' }, h('p', { class: 'faint small' }, 'These come with the harness. The tool groups on the Permissions page decide which of them tasks may use.'), h('div', { class: 'row' }, ...tv.builtin.map((x) => h('span', { class: 'chip mono' }, x))))) : null));
      drawTiles(); drawList();
    } else if (section === 'skills') {
      const q = h('input', { type: 'search', class: 'bsearch', placeholder: 'Search skills', 'aria-label': 'Search skills', autocomplete: 'off' });
      const list = h('div', { class: 'kgrid' });
      const draw = () => { const t = q.value.trim().toLowerCase(); const rows = info.skills.filter((k) => !t || (k.name + ' ' + (k.description || '')).toLowerCase().includes(t)); list.replaceChildren(...(rows.length ? rows.map((k) => h('div', { class: 'kcard' }, h('b', {}, k.name), k.description ? h('p', { class: 'faint small' }, k.description) : h('p', { class: 'faint small' }, 'No description.'))) : [h('p', { class: 'faint small' }, 'No skills match.')])); };
      q.addEventListener('input', draw); draw();
      body.append(card('Skills', 'Instructions the harness loads when they apply. Only the name and description are shown.', info.skills.length ? q : null, info.skills.length ? list : h('p', { class: 'faint small' }, 'None found in ' + info.name + '\'s own skills folder.')));
    } else if (section === 'hooks') {
      body.append(card('Hooks', 'Programs the harness runs on events, such as before a tool or when a session ends. Only program names are shown, never arguments.',
        info.hooks.length ? h('div', { class: 'kgrid' }, ...info.hooks.map((k) => h('div', { class: 'kcard' }, h('div', { class: 'row' }, h('b', {}, k.event), h('span', { class: 'chip' }, k.count + (k.count === 1 ? ' hook' : ' hooks'))), k.runs.length ? h('div', { class: 'row' }, ...k.runs.map((r) => h('span', { class: 'chip mono' }, r))) : null))) : h('p', { class: 'faint small' }, 'No hooks are set up.')));
    } else {
      const list = (title, items) => card(title, null, items.length ? h('div', { class: 'row' }, ...items.map((x) => h('span', { class: 'chip mono' }, x))) : h('p', { class: 'faint small' }, 'None.'));
      body.append(list('Plugins', info.plugins), list('Slash commands', info.commands), list('Subagents', info.subagents), list('Instruction files', info.instructions));
    }
    // How fresh the server states are, with a way to check now. States older than ten minutes are
    // checked once when the page opens (a few tokens on the smallest model), so a stale "failed" does not linger.
    const age = info.statusAt ? Math.floor(Date.now() / 1000) - info.statusAt : null;
    const stale = info.tasksUseIt && (age == null || age > 600);
    if (stale && info.installed && !(S.probeTried && Date.now() - S.probeTried < 600000)) {
      S.probeTried = Date.now();
      api('/api/harness/probe', { method: 'POST' }).then(() => { if (S.route.name === 'harness') renderHarness(); }).catch(() => {});
    }
    const freshness = info.tasksUseIt ? h('div', { class: 'row fresh' }, h('span', { class: 'faint small' }, age == null ? 'Servers not checked yet' : 'Servers checked ' + (age < 60 ? 'just now' : Math.floor(age / 60) + ' min ago')),
      (() => { const b = h('button', { class: 'btn ghost small', title: 'Start Claude once to see which servers connect. A few tokens on the smallest model.' }, icon('refresh'), 'Check now'); b.addEventListener('click', async () => { b.disabled = true; b.textContent = 'Checking…'; try { await api('/api/harness/probe', { method: 'POST' }); } catch (e) { toast(e.message, true); } renderHarness(); }); return b; })()) : null;
    box.replaceChildren(h('div', { class: 'bhead2' }, h('div', {}, h('h2', {}, 'Harness'), h('p', { class: 'faint' }, 'The program that runs the model: its servers, tools, skills and permissions. Tasks run in the one this page was opened through.')), freshness), picker, h('div', { class: 'hlayout' }, nav, body));
  }

  // A server in detail: what state it is in and what to do about it, then its tools (grouped, searchable,
  // marked by what they do), prompts and resources.
  async function openServerModal(agent, m, info) {
    const st = srvState(m.status);
    const body = h('div', { class: 'mbody smodal' }, h('p', { class: 'faint' }, 'Loading…'));
    const modal = h('div', { class: 'modal wide', role: 'dialog', 'aria-modal': 'true', 'aria-label': m.name },
      h('header', {}, h('h2', {}, m.name), h('span', { class: 'chip fs-' + (st === 'ok' ? 'good' : st === 'bad' ? 'bad' : st === 'auth' ? 'wait' : 'info') }, SRV_WORD[st]), h('button', { class: 'btn ghost small sp', onclick: closeMCP }, 'Close')), body);
    const bg = h('div', { class: 'modal-bg', onclick: (e) => { if (e.target === bg) closeMCP(); } }, modal);
    $('modal-root').replaceChildren(bg);
    S.modal = { agent: 'server' };
    document.removeEventListener('keydown', escClose);
    document.addEventListener('keydown', escClose);
    let tab = 'tools';
    const only = { kind: '' }; // filter by what a tool does
    async function draw(d) {
      const q = h('input', { type: 'search', class: 'bsearch', placeholder: 'Search tools by name or what they do', 'aria-label': 'Search this server', autocomplete: 'off', value: S.srvQ || '' });
      const list = h('div', { class: 'slist', tabindex: '0', 'aria-label': 'Scrollable list' });
      const kinds = h('div', { class: 'row filters' });
      const toolRow = (x) => { const k = toolKind(x); return h('details', { class: 'tool' }, h('summary', {}, h('code', {}, x.name), h('span', { class: 'chip tk-' + k }, TOOL_KIND_WORD[k]), x.description ? h('span', { class: 'td faint small' }, x.description.length > 110 ? x.description.slice(0, 110) + '…' : x.description) : null),
        x.description ? h('p', { class: 'tdesc' }, x.description) : h('p', { class: 'faint small' }, 'No description yet. Load descriptions above.'),
        h('div', { class: 'row' }, h('button', { type: 'button', class: 'btn ghost small', title: 'The name to use in the allowed or blocked list on the Permissions page', onclick: () => copy('mcp__' + m.name + '__' + x.name, 'the tool name') }, icon('copy'), 'Copy tool name')),
        x.params && x.params.length ? h('table', { class: 'ptable' }, h('thead', {}, h('tr', {}, ...['Parameter', 'Type', 'Needed', 'What it is'].map((c) => h('th', {}, c)))), h('tbody', {}, ...x.params.map((p) => h('tr', {}, h('td', {}, h('code', {}, p.name)), h('td', { class: 'faint' }, p.type || ''), h('td', {}, p.required ? 'Required' : 'Optional'), h('td', { class: 'faint' }, p.description || ''))))) : null); };
      const fill2 = () => {
        const t = q.value.trim().toLowerCase(); S.srvQ = q.value;
        if (tab === 'tools') {
          const rows = d.tools.filter((x) => (!only.kind || toolKind(x) === only.kind) && (!t || (x.name + ' ' + (x.description || '')).toLowerCase().includes(t)));
          if (!rows.length) { list.replaceChildren(h('p', { class: 'faint small' }, d.known ? 'No tool matches.' : 'Tool names appear after a task has started. Check the servers from the Connected servers page, or start a task.')); return; }
          const groups = groupTools(rows);
          list.replaceChildren(...groups.map((g) => g.name ? h('details', { class: 'tgroup', ...(groups.length === 1 || t ? { open: true } : {}) }, h('summary', {}, h('b', {}, g.name), h('span', { class: 'faint small' }, g.tools.length + (g.tools.length === 1 ? ' tool' : ' tools'))), ...g.tools.map(toolRow)) : h('div', {}, ...g.tools.map(toolRow))));
        } else if (tab === 'prompts') {
          const rows = d.prompts.filter((x) => !t || x.toLowerCase().includes(t));
          list.replaceChildren(...(rows.length ? rows.map((x) => h('div', { class: 'kcard' }, h('code', {}, '/' + x))) : [h('p', { class: 'faint small' }, d.known ? 'This server offers no prompts.' : 'Shown after a task has started.')]));
        } else list.replaceChildren(h('p', { class: 'faint small' }, d.note || 'Resources are not listed.'), h('p', { class: 'faint small' }, 'Resources are files or records a server can hand to the harness. Claude Code does not report them, so Cully cannot show them here.'));
      };
      const drawKinds = () => {
        const counts = {}; for (const x of d.tools) counts[toolKind(x)] = (counts[toolKind(x)] || 0) + 1;
        kinds.replaceChildren(...[['', 'All'], ['read', 'Reads'], ['change', 'Changes things'], ['delete', 'Deletes']].filter(([k]) => !k || counts[k]).map(([k, l]) => h('button', { type: 'button', class: 'toggle' + (only.kind === k ? ' on' : ''), onclick: () => { only.kind = k; drawKinds(); fill2(); } }, l + (k ? ' ' + counts[k] : ' ' + d.tools.length))));
      };
      q.addEventListener('input', fill2);
      const tabs = h('div', { class: 'seg tabs-seg', role: 'tablist' }, ...[['tools', 'Tools ' + d.tools.length], ['prompts', 'Prompts ' + d.prompts.length], ['resources', 'Resources']].map(([k, l]) => h('button', { type: 'button', role: 'tab', class: k === tab ? 'on' : '', 'aria-selected': String(k === tab), onclick: () => { tab = k; draw(d); } }, l)));
      const loadBtn = d.canLoad && !d.detailsFrom ? h('button', { class: 'btn small' }, icon('spark'), 'Load descriptions and parameters') : null;
      if (loadBtn) loadBtn.addEventListener('click', async () => { loadBtn.disabled = true; loadBtn.textContent = 'Reading the server…'; try { draw(await api('/api/harness/server/details', { method: 'POST', body: JSON.stringify({ agent, name: m.name }) })); } catch (e) { toast(e.message, true); loadBtn.disabled = false; loadBtn.textContent = 'Load descriptions and parameters'; } });
      const checkBtn = st === 'unchecked' || st === 'bad' ? h('button', { class: 'btn small', onclick: async (e) => { e.currentTarget.disabled = true; e.currentTarget.textContent = 'Checking…'; try { await api('/api/harness/probe', { method: 'POST' }); closeMCP(); renderHarness(); toast('Checked the servers'); } catch (x) { toast(x.message, true); } } }, icon('refresh'), 'Check now') : null;
      body.replaceChildren(
        h('div', { class: 'row' }, h('span', { class: 'chip' }, d.kind === 'http' ? 'Web address' : 'Program'), h('code', { class: 'srvaddr' }, d.target || ''), d.target ? copyBtn('Copy', () => d.target) : null),
        SRV_FIX[st] ? h('div', { class: 'ctxnote' }, icon('alert'), h('div', {}, h('b', {}, SRV_WORD[st]), h('div', { class: 'faint small' }, SRV_FIX[st])), checkBtn) : (checkBtn ? h('div', { class: 'ctxnote' }, icon('alert'), h('div', {}, h('b', {}, 'Not checked yet'), h('div', { class: 'faint small' }, 'Claude reports a server\'s state when it starts. Cully can start it once to find out.')), checkBtn) : null),
        tabs, tab === 'tools' ? kinds : null, tab !== 'resources' ? h('div', { class: 'row' }, q, loadBtn) : null,
        loadBtn ? h('p', { class: 'faint small' }, 'Claude Code lists a server\'s tool names but not what they do. Loading asks your Claude once, for this server only, to read the tool definitions; it uses a few thousand tokens.') : null,
        d.detailsFrom ? h('p', { class: 'faint small' }, 'Descriptions: ' + d.detailsFrom + '. "Reads", "Changes things" and "Deletes" are guessed from the tool\'s name to help you scan; read the description to be sure.') : (d.tools.length ? h('p', { class: 'faint small' }, '"Reads", "Changes things" and "Deletes" are guessed from the tool\'s name to help you scan.') : null), list);
      if (tab === 'tools') drawKinds();
      fill2();
    }
    try { draw(await api('/api/harness/server?agent=' + agent + '&name=' + encodeURIComponent(m.name))); } catch (e) { body.replaceChildren(h('div', { class: 'err' }, e.message)); }
  }

  // ---------- harness permissions ----------
  async function renderPermissions(out, head) {
    const v = await api('/api/admin/tools');
    const tv = await api('/api/harness/tools?agent=claude').catch(() => ({ servers: [] }));
    const state = { presets: Object.fromEntries(v.presets.map((p) => [p.id, p.on])), allow: [...v.allow], deny: [...v.deny], mcp: [...v.mcp.only], askRisky: v.askRisky !== false };
    const orig = JSON.stringify(state);
    const bar = h('div', { class: 'savebar', hidden: true });
    const save = async () => {
      try { await api('/api/admin/tools', { method: 'PUT', body: JSON.stringify({ presets: state.presets, allow: state.allow, deny: state.deny, mcpOnly: state.mcp, askRisky: state.askRisky }) }); toast('Saved. New tasks use these settings.'); renderPermissions(out, head); } catch (e) { toast(e.message, true); }
    };
    const summary = h('div', { class: 'permsum' });
    const touch = () => {
      const changed = JSON.stringify(state) !== orig;
      bar.hidden = !changed;
      bar.replaceChildren(h('span', {}, 'You have unsaved changes.'), h('button', { class: 'btn', onclick: save }, icon('check'), 'Save'), h('button', { class: 'btn ghost', onclick: () => renderPermissions(out, head) }, 'Discard'));
      const on = v.presets.filter((p) => state.presets[p.id]).map((p) => p.label.toLowerCase());
      summary.replaceChildren(h('p', {}, h('b', {}, 'In plain words: '), 'a task may ' + (on.length ? on.join(', ') : 'do nothing on its own') + (state.allow.length ? ', and use ' + state.allow.length + ' more ' + (state.allow.length === 1 ? 'thing' : 'things') + ' you allowed' : '') + '. For anything else it stops and asks you in its window. It can never use the ' + state.deny.length + ' ' + (state.deny.length === 1 ? 'thing' : 'things') + ' blocked below.'));
    };
    // A rule in words: Bash(git push:*) is "any command starting with git push".
    const ruleWords = (r) => {
      let m = /^Bash\((.+?)(?::\*)?\)$/.exec(r); if (m) return 'Commands starting with ' + m[1];
      m = /^mcp__([^_]+(?:_[^_]+)*?)__(.+)$/.exec(r); if (m) return 'The ' + m[2] + ' tool on ' + m[1];
      m = /^mcp__(.+)$/.exec(r); if (m) return 'Every tool on ' + m[1];
      return r;
    };
    const RISK = { read: ['Safe', 'ok'], edit: ['Changes files', 'warn'], bash: ['Runs commands', 'warn'], web: ['Reaches the web', 'warn'], memory: ['Safe', 'ok'], memsave: ['Writes memory', 'warn'] };
    const risk = (p) => RISK[p.id] || (/web/i.test(p.id + p.label) ? RISK.web : /edit|write/i.test(p.id + p.label) ? RISK.edit : /command|bash/i.test(p.id + p.label) ? RISK.bash : null);
    // 1. what a task may do on its own
    const cards = h('div', { class: 'pcards' }, ...v.presets.map((p) => {
      const r = risk(p);
      const sw = h('button', { type: 'button', class: 'switch', role: 'switch', 'aria-checked': String(!!state.presets[p.id]), 'aria-label': p.label, onclick: (e) => { e.stopPropagation(); state.presets[p.id] = !state.presets[p.id]; sw.setAttribute('aria-checked', String(state.presets[p.id])); card.classList.toggle('on', !!state.presets[p.id]); touch(); } }, h('span', { class: 'knob' }));
      const card = h('div', { class: 'pcard' + (state.presets[p.id] ? ' on' : ''), onclick: () => sw.click() }, h('div', { class: 'row' }, h('b', {}, p.label), r ? h('span', { class: 'chip ' + (r[1] === 'ok' ? 'fs-good' : 'fs-wait') }, r[0]) : null, h('span', { class: 'sp' }), sw), h('p', { class: 'faint small' }, p.description));
      return card;
    }));
    // a list of rules as chips, with a box to add one and ready-made suggestions
    const chipList = (kind, items, placeholder, suggestions) => {
      const wrap = h('div', {}); const chips = h('div', { class: 'rchips' });
      const input = h('input', { type: 'text', placeholder, 'aria-label': 'Add to the ' + kind + ' list', autocomplete: 'off', spellcheck: 'false' });
      const sug = h('div', { class: 'row filters' });
      const draw = () => {
        chips.replaceChildren(...(items.length ? items.map((t, i) => h('span', { class: 'rchip', title: t }, h('span', {}, ruleWords(t)), h('code', { class: 'faint' }, t), h('button', { class: 'iconbtn', title: 'Remove', 'aria-label': 'Remove ' + t, onclick: () => { items.splice(i, 1); draw(); touch(); } }, icon('x')))) : [h('p', { class: 'faint small' }, 'Nothing here.')]));
        sug.replaceChildren(...suggestions.filter((x) => !items.includes(x[0])).slice(0, 6).map(([rule, words]) => h('button', { type: 'button', class: 'toggle', title: rule, onclick: () => { items.push(rule); draw(); touch(); } }, icon('plus'), words)));
      };
      const add = () => { const t = input.value.trim(); if (t && !items.includes(t)) { items.push(t); input.value = ''; draw(); touch(); } };
      input.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); add(); } });
      draw();
      return h('div', {}, chips, suggestions.length ? h('div', {}, h('span', { class: 'opt-label' }, 'Quick add'), sug) : null, h('div', { class: 'row' }, input, h('button', { class: 'btn ghost small', onclick: add }, icon('plus'), 'Add')));
    };
    const allowSug = [['Bash(go test:*)', 'Run go test'], ['Bash(npm test:*)', 'Run npm test'], ['Bash(make test:*)', 'Run make test'], ['Bash(pytest:*)', 'Run pytest'], ['Bash(git status:*)', 'git status'], ['Bash(git diff:*)', 'git diff'], ['Bash(git log:*)', 'git log']];
    const denySug = [['Bash(rm -rf:*)', 'Delete recursively'], ['Bash(sudo:*)', 'Use sudo'], ['Bash(docker:*)', 'Run docker'], ['Bash(chmod:*)', 'Change permissions'], ['WebFetch', 'Fetch web pages']];
    const denyBox = h('div', {}); const drawDeny = () => denyBox.replaceChildren(chipList('blocked', state.deny, 'For example Bash(rm:*)', denySug));
    drawDeny();
    out.replaceChildren(head(),
      h('p', { class: 'faint' }, 'Tasks Cully starts run on their own. A task may use what is allowed here without asking. For anything else it stops, and a card in its window shows exactly what it wants to do so you can allow or deny it. What is blocked here is never offered at all.'),
      summary,
      (() => {
        const sw = h('button', { type: 'button', class: 'switch', role: 'switch', 'aria-checked': String(state.askRisky), 'aria-label': 'Ask me about risky commands', onclick: () => { state.askRisky = !state.askRisky; sw.setAttribute('aria-checked', String(state.askRisky)); touch(); } }, h('span', { class: 'knob' }));
        return h('section', { class: 'group askcard' }, h('div', { class: 'row' }, h('b', {}, 'Ask me about risky commands'), h('span', { class: 'sp' }), sw),
          h('p', { class: 'faint small' }, 'On (recommended): a command such as git push, curl, ssh or kubectl stops the task and a card in its window shows exactly what it wants to do, with a risk label. You allow it once, or deny it. Off: those are blocked outright and the task is told no. A high-risk call is only ever allowed one at a time.'));
      })(),
      h('section', { class: 'group' }, h('header', {}, h('span', { class: 'gt' }, 'What a task may do on its own')), cards),
      h('section', { class: 'group' }, h('header', {}, h('span', { class: 'gt' }, 'Allowed without asking'), h('a', { class: 'linkbtn sp', href: '#/harness/tools' }, 'Choose connected tools one by one')),
        h('p', { class: 'faint small' }, 'Extra things a task may use freely, such as one kind of command. For the tools of a connected server, the Tools page lets you pick Ask, Allow or Block for each.'), chipList('allowed', state.allow, 'Bash(go test:*)', allowSug)),
      h('section', { class: 'group' }, h('header', {}, h('span', { class: 'gt' }, 'Never allowed'), h('button', { class: 'linkbtn sp', onclick: () => { state.deny = [...v.defaultDeny]; drawDeny(); touch(); } }, 'Reset to defaults')),
        h('p', { class: 'faint small' }, 'Blocked even when allowed above, and never asked about. While "Ask me about risky commands" is on, the default entries (git push, curl, wget, ssh, scp, nc, kubectl) ask instead; anything else you add here is blocked outright.'), denyBox),
      bar);
    touch();
  }

  // ---------- dashboard ----------
  // Shared building blocks for the dashboard.
  function toolbar(title, sub, ...actions) {
    return h('div', { class: 'toolbar' }, h('div', { class: 'tt' }, h('h2', {}, title), sub ? h('p', { class: 'sub' }, sub) : null), h('div', { class: 'actions' }, ...actions));
  }
  function copyBtn(label, getText) {
    const b = h('button', { class: 'btn ghost small' }, icon('copy'), label);
    b.addEventListener('click', async () => {
      try {
        await writeClip(await getText());
        b.replaceChildren(icon('check'), 'Copied');
        b.classList.add('done');
        setTimeout(() => { b.replaceChildren(icon('copy'), label); b.classList.remove('done'); }, 1600);
      } catch (_) { toast('Copy is blocked here; select the text and copy it by hand', true); }
    });
    return b;
  }
  function refreshBtn(fn) {
    const b = h('button', { class: 'btn ghost small' }, icon('refresh'), 'Refresh');
    b.addEventListener('click', async () => { b.disabled = true; b.classList.add('spin'); try { await fn(); } finally { b.disabled = false; b.classList.remove('spin'); } });
    return b;
  }
  function emptyCard(title, body, cta, href) {
    return h('div', { class: 'emptycard' }, h('h3', {}, title), h('p', {}, body), cta ? h('a', { class: 'btn small', href }, cta) : null);
  }
  const live = (state) => state === 'working' || state === 'waiting' || state === 'preparing';
  const ago2 = (iso) => (iso && !iso.startsWith('0001') ? ago(iso) : '');

  function checksSentence(c, since) {
    if (!c || c.last === 'none') return 'No tests or builds have run yet.';
    return (c.last === 'passed' ? 'The last check passed.' : 'The last check failed.') + (since > 0 ? ' ' + since + (since === 1 ? ' edit' : ' edits') + ' since then.' : '');
  }

  // Where the work stands, in plain words. The text meant for another agent stays behind Copy.
  function renderSummary(sum, opts) {
    const o = opts || {};
    const kids = [];
    if (o.agent && sum.task) kids.push(h('section', {}, h('h3', {}, 'What was asked'), h('p', {}, sum.task)));
    if (sum.lastReply) kids.push(h('section', {}, h('h3', {}, 'Where it stopped'), h('div', { class: 'quote' }, markdown(sum.lastReply))));
    const changed = sum.fileCount
      ? [h('p', {}, sum.fileCount + (sum.fileCount === 1 ? ' file' : ' files') + ' changed', sum.added || sum.removed ? h('span', {}, '  ', h('span', { class: 'plus' }, '+' + sum.added), ' ', h('span', { class: 'minus' }, '−' + sum.removed)) : null),
        groupBars(sum),
        h('details', { class: 'more' }, h('summary', {}, 'Show files'), h('ul', { class: 'flist' }, ...sum.files.map((f) => h('li', {}, f)), sum.fileCount > sum.files.length ? h('li', { class: 'faint' }, 'and ' + (sum.fileCount - sum.files.length) + ' more') : null))]
      : [h('p', { class: 'faint' }, 'No changes yet.')];
    kids.push(h('section', {}, h('h3', {}, o.agent ? 'What changed' : 'Changes in ' + (sum.branch || 'this repository')), ...changed));
    if (sum.activity || o.agent) kids.push(h('section', {}, h('h3', {}, 'Checks'), h('p', {}, checksSentence(sum.checks, sum.editsSinceCheck))));
    if (sum.loops > 0) kids.push(h('div', { class: 'callout' }, h('b', {}, 'Heads up. '), 'The same command failed several times in a row. Look at it before continuing.'));
    if (!o.agent && !sum.activity) kids.push(h('div', { class: 'callout' }, 'Cully has no session recorded for this project yet. Start a task from the Tasks tab and what it does shows up here.'));
    if (o.agent) kids.push(h('div', { class: 'row' }, h('button', { class: 'btn small', onclick: () => continueFrom(o.agent) }, icon('plus'), 'Continue in a new task')));
    return h('div', { class: 'summary' }, ...kids);
  }

  // ----- advisor -----
  const LEVEL = { warn: 'Warning', caution: 'Caution', advice: 'Suggestion' };
  async function askAdvisor(a) {
    try { await api('/api/agents/' + a.id + '/advise', { method: 'POST' }); toast('The advisor is reviewing ' + a.title); location.hash = '#/dashboard/advisor'; } catch (e) { toast(e.message, true); }
  }
  const openAdviceFor = (id) => ((S.advice || []).find((g) => g.agentId === id) || { advice: [] }).advice.filter((x) => !x.applied && !x.dismissed).length;

  function adviceCard(g, x) {
    const state = x.applied ? 'applied' : '';
    const kids = [h('div', { class: 'row' }, h('span', { class: 'lvl ' + x.level }, LEVEL[x.level] || 'Suggestion'), h('span', { class: 'faint' }, 'for ' + g.agentTitle)), h('p', { class: 'advtext' }, x.text)];
    if (x.applied) kids.push(h('div', { class: 'sent' }, icon('check'), 'Sent to ' + g.agentTitle));
    else kids.push(h('div', { class: 'row' },
      x.action === 'handoff' ? h('button', { class: 'btn small', onclick: async () => {
        try { await writeClip(await api('/api/agents/' + g.agentId + '/handoff')); toast('Handoff copied. Paste it into the session you are continuing.'); } catch (e) { toast('Could not copy: ' + e.message, true); }
      } }, icon('copy'), 'Copy handoff from this session') : null,
      h('button', { class: x.action === 'handoff' ? 'btn ghost small' : 'btn small', disabled: !live(g.agentState), title: live(g.agentState) ? '' : 'This task has finished', onclick: async () => {
        try { await api('/api/agents/' + g.agentId + '/advice/' + x.id + '/apply', { method: 'POST' }); toast('Sent to ' + g.agentTitle); drawPanel('advisor', true); } catch (e) { toast(e.message, true); }
      } }, icon('send'), 'Send to ' + g.agentTitle),
      h('button', { class: 'btn ghost small', onclick: async () => { try { await api('/api/agents/' + g.agentId + '/advice/' + x.id + '/dismiss', { method: 'POST' }); drawPanel('advisor', true); } catch (e) { toast(e.message, true); } } }, 'Dismiss')));
    return h('div', { class: 'advcard ' + x.level + ' ' + state }, ...kids);
  }
  // The Advisor's inbox: everything Cully suggests, in one place. The Advisor decides what to
  // suggest; Agents, Background jobs and Workflow rules only show what you approved. A background
  // job is one fixed command at a set time (no model, no tokens); an agent reads and judges.
  const SUG_LABEL = { agent: 'Agent', job: 'Background job', coach: 'Tip', rules: 'Workflow rules', digest: 'Scouts' };
  async function suggestionInbox(again) {
    let list = [];
    const archived = !!S.inboxArchived;
    try { list = await api('/api/suggestions' + (archived ? '?archived=1' : '')); } catch (_) { return null; }
    const ask = async (btn) => { btn.disabled = true; try { const r = await api('/api/job-suggestions/run', { method: 'POST' }); toast(r.added ? r.added + ' background job ' + (r.added === 1 ? 'proposal' : 'proposals') : 'Nothing repeats enough to need a background job yet.'); again(); } catch (e) { toast(e.message, true); btn.disabled = false; } };
    const card = (x) => {
      const acts = [];
      if (x.kind === 'job' || x.kind === 'agent') acts.push(h('a', { class: 'btn small', href: x.href }, icon('check'), x.kind === 'job' ? 'Review in Background jobs' : 'Review in Agents'));
      else if (x.href && x.href !== '#/dashboard/advisor') acts.push(h('a', { class: 'btn ghost small', href: x.href }, 'Open ' + ({ coach: 'the page', rules: 'Workflow rules', digest: 'Scout history' }[x.kind] || '')));
      return h('li', { class: 'hrow' }, h('div', { class: 'hmain' }, h('div', { class: 'ht' }, h('span', { class: 'chip' }, SUG_LABEL[x.kind] || x.kind), ' ' + x.title),
        x.why ? h('p', { class: 'hdesc' }, x.why) : null, evidenceBlock(x),
        x.detail ? h('p', { class: 'faint small' }, x.detail) : null,
        x.command ? h('p', { class: 'small' }, h('span', { class: 'faint' }, 'It will run: '), h('code', {}, x.command)) : null, window.CullyLedger.preview(x.preview)),
        h('div', { class: 'row hact' }, ...acts, ...(x.kind === 'job' || x.kind === 'agent' ? window.CullyLedger.proposalButtons(x, again, archived) : [])));
    };
    return h('section', { class: 'lib-card' }, h('div', { class: 'row' }, h('b', {}, archived ? 'Archived suggestions' : 'Suggestions'), h('button', { class: 'btn ghost small', onclick: () => { S.inboxArchived = !archived; again(); } }, archived ? 'Show waiting' : 'Archived'), window.CullyLedger.link(''), h('span', { class: 'faint small' }, list.length ? list.length + (archived ? ' archived' : ' waiting for you') : (archived ? 'Nothing archived' : 'Nothing waiting')),
      h('button', { class: 'btn ghost small sp', title: 'Ask the advisor whether a chore you repeat should be a background job', onclick: (e) => ask(e.target) }, icon('spark'), 'Look for background jobs')),
      h('p', { class: 'faint small' }, 'The Advisor makes every suggestion, from your analysed sessions and what you ask Claude to do again and again, and sends each to where it belongs: agents to Agents, background jobs to Background jobs, rules to Workflow rules. A background job is one fixed command that runs by itself at a time you choose, with no model and no tokens, for chores like running the tests overnight. An agent is for work that needs reading and judgement. Nothing starts until you approve it.'),
      list.length ? h('ul', { class: 'hrows' }, ...list.map(card)) : null);
  }
  // The Advisor board: one list of everything Cully suggests, tips and proposals together, with a filter,
  // short rows that open for the detail, and a button that says what it does.
  async function adviceBoard(again) {
    let list = []; let c = { items: [], at: '', based: 0 };
    const archived = !!S.inboxArchived;
    try { list = await api('/api/suggestions' + (archived ? '?archived=1' : '')); } catch (_) { return null; }
    try { c = await api('/api/coach'); } catch (_) { /* optional */ }
    const coachOf = (x) => { const m = /^coach-(\d+)$/.exec(x.id); return m ? c.items[Number(m[1])] : null; };
    const labelOf = (x) => { const it = coachOf(x); return it ? (COACH_KIND[it.kind] || it.kind) : (SUG_LABEL[x.kind] || x.kind); };
    // Proposals you can act on come first, then tips, then the scouts' digest.
    const rank = (x) => (x.kind === 'agent' || x.kind === 'job' || x.kind === 'rules' ? 0 : x.kind === 'coach' ? 1 : 2);
    const sorted = list.slice().sort((a, b) => rank(a) - rank(b));
    const labels = [...new Set(sorted.map(labelOf))];
    const filter = labels.includes(S.adviceFilter) ? S.adviceFilter : '';
    const shown = filter ? sorted.filter((x) => labelOf(x) === filter) : sorted;
    const run = async (btn, path, done) => { btn.disabled = true; try { const r = await api(path, { method: 'POST' }); toast(done(r)); again(); } catch (e) { toast(e.message, true); btn.disabled = false; } };
    const more = h('details', { class: 'menu advmenu' }, h('summary', { class: 'btn ghost small', 'aria-label': 'More actions' }, 'More', icon('down')),
      h('div', { class: 'menu-pop pop-card' }, h('div', { class: 'popcol' },
        h('button', { type: 'button', class: 'folderrow', onclick: (e) => run(e.currentTarget, '/api/coach/run', () => 'The coach has new suggestions') }, icon('refresh'), h('span', { class: 'fr-main' }, h('b', {}, 'Ask the coach again'), h('small', { class: 'faint' }, c.at ? 'Last read ' + c.based + ' sessions, ' + ago(c.at) : 'Reads your analysed sessions'))),
        h('button', { type: 'button', class: 'folderrow', onclick: (e) => run(e.currentTarget, '/api/job-suggestions/run', (r) => (r.added ? r.added + ' background job ' + (r.added === 1 ? 'proposal' : 'proposals') : 'Nothing repeats enough to need a background job yet.')) }, icon('spark'), h('span', { class: 'fr-main' }, h('b', {}, 'Look for background jobs'), h('small', { class: 'faint' }, 'Chores one fixed command can do'))),
        h('button', { type: 'button', class: 'folderrow', onclick: () => { S.inboxArchived = !archived; again(); } }, icon('folder'), h('span', { class: 'fr-main' }, h('b', {}, archived ? 'Show waiting' : 'Show archived')),),
        h('a', { class: 'folderrow', href: '#/dashboard/history' }, icon('clock'), h('span', { class: 'fr-main' }, h('b', {}, 'History of changes'), h('small', { class: 'faint' }, 'Every change, with undo'))))));
    const chip = (v, label, n) => h('button', { type: 'button', class: 'ssf' + (filter === v ? ' on' : ''), 'aria-pressed': String(filter === v), onclick: () => { S.adviceFilter = v; again(); } }, label, h('span', { class: 'ssn' }, String(n)));
    const row = (x) => {
      const it = coachOf(x);
      const acts = [];
      if (x.kind === 'job' || x.kind === 'agent') acts.push(h('a', { class: 'btn small', href: x.href }, icon('check'), x.kind === 'job' ? 'Review job' : 'Review agent'));
      else if (it) acts.push(h('button', { class: 'btn small', onclick: () => pushSuggestion(it) }, icon('send'), 'Apply'));
      else if (x.href && x.href !== '#/dashboard/advisor') acts.push(h('a', { class: 'btn small', href: x.href }, x.kind === 'rules' ? 'Review rules' : 'Open'));
      const extra = [];
      if (it) extra.push(h('button', { class: 'btn ghost small', onclick: () => writeClip('Suggestion from Cully: ' + it.title + '. ' + it.how).then(() => toast('Copied. Paste it into a session.')).catch(() => showCommand('Suggestion from Cully: ' + it.title + '. ' + it.how)) }, icon('copy'), 'Copy'));
      const proposalBtns = x.kind === 'job' || x.kind === 'agent' ? window.CullyLedger.proposalButtons(x, again, archived) : [];
      const doThis = it ? it.how : x.detail;
      const hasDetail = !!(doThis || x.command || (x.evidence && x.evidence.length) || x.preview);
      return h('li', { class: 'advrow' },
        h('div', { class: 'advmain' }, h('div', { class: 'row' }, h('span', { class: 'chip advkind k-' + (it ? it.kind : x.kind) }, labelOf(x)), h('b', { class: 'advtitle' }, x.title)),
          x.why ? h('p', { class: 'advwhy' }, x.why) : null,
          hasDetail ? h('details', { class: 'advmore' }, h('summary', {}, 'What to do and where it came from'),
            doThis ? h('p', { class: 'advhow' }, h('b', {}, 'What to do: '), doThis) : null,
            x.command ? h('p', { class: 'small' }, h('span', { class: 'faint' }, 'It will run: '), h('code', {}, x.command)) : null,
            evidenceBlock(x), window.CullyLedger.preview(x.preview)) : null),
        h('div', { class: 'advact' }, ...acts, ...extra, ...proposalBtns));
    };
    const head = h('div', { class: 'advhead' }, h('div', { class: 'row' }, h('b', {}, archived ? 'Archived' : sorted.length ? sorted.length + (sorted.length === 1 ? ' suggestion' : ' suggestions') + ' waiting for you' : 'Nothing waiting'), h('span', { class: 'sp' }), more),
      sorted.length > 1 ? h('div', { class: 'ssgroup', role: 'group', 'aria-label': 'Filter suggestions' }, chip('', 'All', sorted.length), ...labels.map((l) => chip(l, l, sorted.filter((x) => labelOf(x) === l).length))) : null);
    const how = h('details', { class: 'advhowto' }, h('summary', {}, 'How the Advisor works'),
      h('p', { class: 'faint small' }, 'It reads your analysed sessions and what you keep asking your agents to do, then sends each idea where it belongs: agents to Agents, background jobs to Background jobs, rules to Rules. A background job is one fixed command that runs on its own at a time you choose, with no model and no tokens. Nothing starts until you approve it.'));
    return h('section', { class: 'lib-card advboard' }, head, shown.length ? h('ul', { class: 'advlist' }, ...shown.map(row)) : emptyCard(archived ? 'No archived suggestions' : 'Nothing to suggest yet', archived ? 'Dismissed and approved suggestions appear here.' : 'Once a few sessions are analysed, Cully looks for patterns worth acting on. You can also ask it now from More.', '', ''), how);
  }
  // Background job proposals, shown on the Background jobs page where they are approved. Each shows
  // the exact command, and the time can be changed before approving.
  async function jobProposals(again) {
    let list = [];
    try { list = await api('/api/job-suggestions'); } catch (_) { return null; }
    const ask = async (btn) => { btn.disabled = true; try { const r = await api('/api/job-suggestions/run', { method: 'POST' }); toast(r.added ? r.added + ' background job ' + (r.added === 1 ? 'proposal' : 'proposals') : 'Nothing repeats enough to need a background job yet.'); again(); } catch (e) { toast(e.message, true); btn.disabled = false; } };
    const card = (x) => {
      const time = h('input', { type: 'time', value: x.after || '02:00', 'aria-label': 'Time of day', class: 'timein' });
      return h('li', { class: 'hrow' }, h('div', { class: 'hmain' }, h('div', { class: 'ht' }, x.name, x.updates ? h('span', { class: 'chip run-partial' }, 'Updates a job you have') : h('span', { class: 'chip' }, 'New')),
        h('p', { class: 'hdesc' }, x.why), evidenceBlock(x),
        x.updates && x.current ? h('p', { class: 'small' }, h('span', { class: 'faint' }, 'Now: '), h('code', {}, x.current)) : null,
        h('p', { class: 'small' }, h('span', { class: 'faint' }, x.updates ? 'Would become: ' : 'It will run: '), h('code', {}, x.display)),
        window.CullyLedger.preview(x.preview),
        h('div', { class: 'row' }, h('span', { class: 'faint small' }, 'Run after'), time, h('span', { class: 'faint small' }, 'while Cully is running'))),
        h('div', { class: 'row hact' },
          h('button', { class: 'btn small', onclick: async (e) => { e.target.disabled = true; try { await api('/api/job-suggestions/' + x.id + '/accept', { method: 'POST', body: JSON.stringify({ after: time.value }) }); toast((x.updates ? 'Updated. It runs after ' : 'Added. It runs after ') + time.value + '.'); again(); } catch (err) { toast(err.message, true); e.target.disabled = false; } } }, icon('check'), 'Approve'),
          h('button', { class: 'btn ghost small', onclick: async () => { try { await api('/api/job-suggestions/' + x.id + '/dismiss', { method: 'POST' }); again(); } catch (err) { toast(err.message, true); } } }, 'Reject')));
    };
    return h('section', { class: 'lib-card' }, h('div', { class: 'row' }, h('b', {}, 'Proposed by the Advisor'), h('span', { class: 'faint small' }, list.length ? list.length + ' waiting for you' : 'Nothing waiting'),
      h('button', { class: 'btn ghost small sp', onclick: (e) => ask(e.target) }, icon('spark'), 'Look for background jobs')),
      h('p', { class: 'faint small' }, 'A background job is one fixed command that runs by itself at a time you choose, with no model and no tokens. The Advisor proposes one for a chore you repeat that a single command can do. Nothing is added until you approve it.'),
      list.length ? h('ul', { class: 'hrows' }, ...list.map(card)) : null);
  }
  // The Background jobs page: cards in the look of the agents page, a box that turns a sentence into
  // an unsaved job, and the Advisor's proposals. Nothing is saved until you approve it.
  const JOB_STATE_WORD = { running: 'Running now', scheduled: 'Scheduled', paused: 'Paused', done: 'Finished' };
  const JOB_FORMS = [['daily', 'Every day'], ['weekdays', 'Weekdays'], ['hourly', 'Every hour'], ['every', 'Every so often'], ['in', 'Once, from now']];
  // A control for when a job runs. value() gives { schedule, after, maxRuns } as the server takes them.
  function scheduleEditor(init) {
    const m = /^(every|in) (\d+)(m|h)$/.exec(init.schedule || '');
    const kind0 = !init.schedule ? 'daily' : m ? m[1] : init.schedule;
    const kind = h('select', { 'aria-label': 'How often', class: 'jsel' }, ...JOB_FORMS.map(([k, l]) => h('option', { value: k }, l)));
    kind.value = kind0;
    const time = h('input', { type: 'time', value: init.after || '02:00', 'aria-label': 'Time of day', class: 'timein' });
    const num = h('input', { type: 'number', min: '1', max: '999', value: m ? m[2] : '6', 'aria-label': 'How many', class: 'jnum' });
    const unit = h('select', { 'aria-label': 'Minutes or hours', class: 'jsel' }, h('option', { value: 'm' }, 'minutes'), h('option', { value: 'h' }, 'hours'));
    unit.value = m ? m[3] : 'h';
    const limit = h('input', { type: 'number', min: '1', max: '1000', value: init.maxRuns ? String(init.maxRuns) : '', placeholder: 'No limit', 'aria-label': 'Stop after this many runs', class: 'jnum' });
    const dayRow = h('span', { class: 'row' }, h('span', { class: 'faint small' }, 'after'), time);
    const everyLabel = h('span', { class: 'faint small' }, 'every');
    const everyRow = h('span', { class: 'row' }, everyLabel, num, unit);
    const limitRow = h('span', { class: 'row' }, h('span', { class: 'faint small' }, 'Stop after'), limit, h('span', { class: 'faint small' }, 'runs'));
    const sync = () => {
      const k = kind.value;
      dayRow.hidden = !(k === 'daily' || k === 'weekdays');
      everyRow.hidden = !(k === 'every' || k === 'in');
      limitRow.hidden = k === 'in';
      everyLabel.textContent = k === 'in' ? 'in' : 'every';
    };
    kind.addEventListener('change', sync); sync();
    const value = () => {
      const k = kind.value;
      const schedule = k === 'daily' ? '' : k === 'every' || k === 'in' ? k + ' ' + num.value + unit.value : k;
      return { schedule, after: k === 'daily' || k === 'weekdays' ? time.value : '', maxRuns: k === 'in' || !limit.value ? 0 : Number(limit.value) };
    };
    return { el: h('div', { class: 'row jsched' }, kind, dayRow, everyRow, limitRow), value };
  }
  async function renderJobs(out, head, again, live) {
    const [jobs, props] = await Promise.all([api('/api/jobs').catch(() => []), jobProposals(again)]);
    if (!live()) return;
    const hue = (name) => { let n = 0; for (const c of name) n = (n * 31 + c.codePointAt(0)) % 360; return n; };
    const initials = (name) => name.split(/[\s._-]+/).filter(Boolean).slice(0, 2).map((w) => w[0].toUpperCase()).join('') || '?';
    const when = (iso) => (iso ? new Date(iso).toLocaleString([], { weekday: 'short', hour: '2-digit', minute: '2-digit' }) : '');
    const secs = (n) => (n < 90 ? n + ' s' : Math.round(n / 60) + ' min');
    const act = async (fn, msg) => { try { await fn(); if (msg) toast(msg); again(); } catch (e) { toast(e.message, true); } };
    const avatar = (name, st) => { const el = h('div', { class: 'avatar st-' + st, 'aria-hidden': 'true' }, h('span', {}, initials(name)), h('i', { class: 'pulse' })); el.style.setProperty('--hue', String(hue(name))); return el; };
    const bars = (j) => {
      const rs = j.history.slice(0, 20).reverse();
      if (!rs.length) return h('div', { class: 'runbars empty' }, 'No runs yet');
      return h('div', { class: 'runbars', role: 'img', 'aria-label': 'The last ' + rs.length + ' runs, oldest first' }, ...rs.map((r) => {
        const b = h('i', { class: 'rb rb-' + (r.status === 'ok' ? 'done' : 'failed'), title: (r.status === 'ok' ? 'Finished' : r.status === 'timeout' ? 'Timed out' : 'Failed') + ', ' + ago(r.start) });
        b.style.height = (40 + Math.min(60, Math.round(r.seconds / 6))) + '%';
        return b;
      }));
    };
    const editor = (j) => {
      const ed = scheduleEditor({ schedule: j.schedule, after: j.after, maxRuns: j.maxRuns });
      const box = h('div', { class: 'jedit', hidden: true },
        h('p', { class: 'faint small' }, 'Change when it runs. The command stays as it is; to change the command, remove the job and describe it again.'), ed.el,
        h('div', { class: 'row' },
          h('button', { class: 'btn small', onclick: () => act(() => api('/api/jobs/' + encodeURIComponent(j.name), { method: 'PUT', body: JSON.stringify(ed.value()) }), 'Saved ' + j.name) }, icon('check'), 'Save'),
          h('button', { class: 'btn ghost small', onclick: () => { box.hidden = true; } }, 'Cancel')));
      return box;
    };
    const remove = (j) => confirmDialog({ title: 'Remove ' + j.name + '?', body: ['It stops running and its run history is deleted. Its log file stays in ~/.cully/jobs.'], confirmLabel: 'Remove', onConfirm: async () => { await api('/api/jobs/' + encodeURIComponent(j.name), { method: 'DELETE' }); again(); } });
    const card = (j) => {
      const last = j.history[0]; const ed = editor(j);
      return h('section', { class: 'agentcard2 st-' + j.state },
        h('div', { class: 'ac-head' }, avatar(j.name, j.state),
          h('div', { class: 'ac-id' }, h('div', { class: 'ac-name' }, j.name, h('span', { class: 'ac-state st-' + j.state }, JOB_STATE_WORD[j.state] || j.state)),
            h('div', { class: 'ac-meta' }, h('span', {}, icon('clock'), j.when), j.folder ? h('span', {}, icon('folder'), j.folder) : null, h('span', {}, 'Runs ' + j.program),
              j.maxRuns ? h('span', {}, 'Run ' + Math.min(j.runs, j.maxRuns) + ' of ' + j.maxRuns) : null,
              j.next && j.state === 'scheduled' ? h('span', {}, 'Next ' + when(j.next)) : null))),
        h('div', { class: 'ac-runs' }, bars(j),
          h('div', { class: 'ac-runtext' }, last
            ? h('div', { class: 'row' }, h('span', { class: 'chip run-' + (last.status === 'ok' ? 'pass' : 'fail') }, last.status === 'ok' ? 'Last run finished' : last.status === 'timeout' ? 'Last run timed out' : 'Last run failed'),
              h('span', { class: 'faint small' }, ago(last.start) + ', ' + secs(last.seconds)),
              j.streak > 1 ? h('span', { class: 'chip run-fail' }, j.streak + ' failures in a row') : null)
            : h('span', { class: 'faint small' }, 'Has not run yet.'))),
        last && last.tail ? h('div', { class: 'quote ac-quote jtail', title: 'The end of the last run output, secrets removed' }, last.tail) : null,
        h('div', { class: 'ac-foot' },
          h('button', { class: 'btn small', disabled: j.state === 'running', title: 'Start one run now, even when paused', onclick: () => act(() => api('/api/jobs/' + encodeURIComponent(j.name) + '/run', { method: 'POST' }), 'Started ' + j.name) }, icon('send'), j.state === 'running' ? 'Running' : 'Run now'),
          h('button', { class: 'btn ghost small', onclick: () => act(() => api('/api/jobs/' + encodeURIComponent(j.name) + '/' + (j.paused ? 'resume' : 'pause'), { method: 'POST' }), j.paused ? 'Resumed ' + j.name : 'Paused ' + j.name) }, j.paused ? 'Resume' : 'Pause'),
          h('span', { class: 'sp' }),
          h('button', { class: 'btn ghost small', onclick: () => { ed.hidden = !ed.hidden; } }, 'Edit'),
          h('button', { class: 'btn ghost small', 'aria-label': 'Remove ' + j.name, onclick: () => remove(j) }, icon('x'))),
        ed,
        j.history.length > 1 ? h('details', { class: 'rl' }, h('summary', {}, h('span', { class: 'rq' }, 'Run history'), h('span', { class: 'faint small' }, 'the last ' + j.history.length)),
          h('ul', { class: 'hrows' }, ...j.history.map((r) => h('li', { class: 'hrow' }, h('div', { class: 'hmain' },
            h('div', { class: 'row' }, h('span', { class: 'chip run-' + (r.status === 'ok' ? 'pass' : 'fail') }, r.status === 'ok' ? 'finished' : r.status), h('span', { class: 'faint small' }, ago(r.start) + ', ' + secs(r.seconds) + (r.manual ? ', started by you' : ''))),
            r.tail ? h('p', { class: 'hdesc jtail' }, r.tail) : null))))) : null);
    };
    // Describe a job: the advisor writes a draft, shown here for review. Nothing is saved yet.
    const text = h('textarea', { rows: '2', maxlength: '1000', 'aria-label': 'Describe the job', placeholder: 'Run the tests in my api folder every weekday at 7am', class: 'jtext' });
    const draftBox = h('div', {});
    const draftCard = (d) => {
      const ed = scheduleEditor({ schedule: d.schedule, after: d.after, maxRuns: d.maxRuns });
      return h('section', { class: 'agentcard2 st-draft' },
        h('div', { class: 'ac-head' }, avatar(d.name, 'draft'),
          h('div', { class: 'ac-id' }, h('div', { class: 'ac-name' }, d.name, h('span', { class: 'ac-state' }, 'Draft, not saved')), h('div', { class: 'ac-meta' }, h('span', {}, icon('folder'), d.folder)))),
        h('p', { class: 'ac-role' }, d.why),
        h('p', { class: 'small' }, h('span', { class: 'faint' }, 'It will run: '), h('code', {}, d.display)),
        ed.el,
        h('p', { class: 'faint small' }, 'A draft does not run. Check the command and the schedule, then approve it, or change the sentence and draft again.'),
        h('div', { class: 'ac-foot' },
          h('button', { class: 'btn small', onclick: () => act(() => api('/api/jobs', { method: 'POST', body: JSON.stringify({ name: d.name, folder: d.folder, command: d.command, ...ed.value() }) }), 'Added ' + d.name) }, icon('check'), 'Approve and add'),
          h('button', { class: 'btn ghost small', onclick: () => draftBox.replaceChildren() }, 'Discard')));
    };
    const draftBtn = h('button', { class: 'btn', onclick: async () => {
      draftBtn.disabled = true;
      try { const d = await api('/api/jobs/draft', { method: 'POST', body: JSON.stringify({ text: text.value }) }); draftBox.replaceChildren(draftCard(d)); } catch (e) { toast(e.message, true); }
      draftBtn.disabled = false;
    } }, icon('spark'), 'Draft a job');
    const describe = h('section', { class: 'lib-card' }, h('b', {}, 'Describe a job'),
      h('p', { class: 'faint small' }, 'Say what should run and when, in a sentence. Cully turns it into one fixed command in a folder it knows, such as running the tests or fetching git remotes, and shows it here for you to review. Schedules can be daily, weekdays, hourly, every 15 minutes to 168 hours, or once from now, and a job can stop after a number of runs.'),
      text, h('div', { class: 'row' }, draftBtn), draftBox);
    const count = (st) => jobs.filter((j) => j.state === st).length;
    const stat = (n, label, tone) => h('div', { class: 'astat' + (tone ? ' ' + tone : '') }, h('b', {}, String(n)), h('span', {}, label));
    const failing = jobs.filter((j) => j.streak > 0).length;
    out.replaceChildren(head(window.CullyLedger.link('job'), refreshBtn(again)), describe, props,
      jobs.length ? h('div', { class: 'astats' }, stat(jobs.length, jobs.length === 1 ? 'job' : 'jobs'), stat(count('running'), 'running now', count('running') ? 'hot' : ''), stat(count('paused'), 'paused'), stat(failing, 'failing')) : null,
      jobs.length ? h('div', { class: 'agentgrid' }, ...jobs.map(card)) : emptyCard(EMPTY_TEXT.jobs[0], EMPTY_TEXT.jobs[1], null, null));
  }
  function renderAdvisor(list) {
    if (!list.length) return emptyCard('No tasks yet', 'The advisor reviews what a running agent is doing and suggests what to fix or check. Start a task to get advice.', 'Start a task', '#/');
    return h('div', { class: 'groups' }, ...list.map((g) => {
      const open = g.advice.filter((x) => !x.dismissed);
      const asking = g.status === 'asking';
      return h('section', { class: 'group' },
        h('header', {}, dot(agentOr(g.agentId, g.agentState)), h('a', { class: 'gt', href: '#/task/' + g.agentId }, g.agentTitle), h('span', { class: 'pill ' + phaseCls(agentOr(g.agentId, g.agentState)) }, stateLabel(agentOr(g.agentId, g.agentState))),
          h('button', { class: 'btn ghost small sp', disabled: asking, onclick: async () => { try { await api('/api/agents/' + g.agentId + '/advise', { method: 'POST' }); drawPanel('advisor', true); } catch (e) { toast(e.message, true); } } }, icon('spark'), asking ? 'Reviewing…' : 'Ask advisor')),
        asking ? h('p', { class: 'status pulse' }, "The advisor is reviewing this agent's work…") : null,
        g.status === 'failed' ? h('p', { class: 'err' }, g.error) : null,
        open.length ? h('div', { class: 'advlist' }, ...open.map((x) => adviceCard(g, x))) : (asking ? null : h('p', { class: 'faint' }, "No advice yet. Ask the advisor to review this agent's work.")),
        g.asked && !g.asked.startsWith('0001') ? h('p', { class: 'faint small' }, 'Last asked ' + ago(g.asked)) : null);
    }));
  }

  // ----- scout -----
  const STAGE_WORDS = { running: 'Scouting…', ok: 'Done', failed: 'Could not finish', skipped: 'Not used for this task', pending: 'Waiting' };
  function stageBlock(title, st, icn) {
    const kids = [h('div', { class: 'row' }, icon(icn), h('b', {}, title), h('span', { class: 'step ' + st.status }, STAGE_WORDS[st.status] || st.status))];
    if (st.status === 'failed' && st.detail) kids.push(h('p', { class: 'err' }, st.detail));
    if (st.log && st.log.length) {
      kids.push(h('ul', { class: 'steps2' }, ...st.log.map((l) => {
        const m = /^(Searching|Reading):\s*(.*)$/.exec(l);
        if (!m) return h('li', {}, l);
        return h('li', {}, icon(m[1] === 'Searching' ? 'search' : 'globe'), h('span', { class: 'faint' }, m[1] === 'Searching' ? 'Searched for ' : 'Read '),
          /^https?:\/\//.test(m[2]) ? h('a', { href: m[2], target: '_blank', rel: 'noopener noreferrer', class: 'ext' }, m[2]) : h('span', {}, m[2]));
      })));
    }
    if (st.status === 'running' && !(st.log && st.log.length)) kids.push(h('p', { class: 'status pulse' }, 'Starting up…'));
    if (st.output) kids.push(h('div', { class: 'findings' }, markdown(st.output)), h('div', { class: 'row' }, copyBtn('Copy findings', () => st.output)));
    return h('div', { class: 'stage-block' }, ...kids);
  }
  function scoutStarter() {
    const input = h('input', { type: 'text', placeholder: 'What should the scout look into? For example: retry patterns for flaky HTTP calls in Go', 'aria-label': 'Scout question' });
    const go = h('button', { class: 'btn', type: 'submit' }, icon('search'), 'Start a scout');
    const form = h('form', { class: 'scout-form' }, input, go);
    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      go.disabled = true;
      try { await api('/api/scout/run', { method: 'POST', body: JSON.stringify({ query: input.value }) }); input.value = ''; S.dashSig = ''; drawPanel('scout', true); } catch (x) { toast(x.message, true); } finally { go.disabled = false; }
    });
    return form;
  }
  function scoutRunCard(r) {
    const st = { status: r.status === 'ok' ? 'ok' : r.status === 'failed' ? 'failed' : 'running', detail: r.detail, log: r.log, output: r.output };
    return h('section', { class: 'group' }, h('header', {}, h('span', { class: 'gt' }, r.query), h('span', { class: 'faint small' }, ago(r.started))),
      stageBlock('Web research', st, 'search'),
      r.output ? h('div', { class: 'row' }, h('button', { class: 'btn small', onclick: () => { S.draft = 'Use these findings:\n\n' + r.output + '\n\nTask: '; location.hash = '#/'; } }, icon('plus'), 'Use in a new task')) : null);
  }
  function renderScout(list, runs) {
    const used = list.filter((v) => v.scout.status !== 'skipped' || v.recall.status !== 'skipped');
    const kids = [scoutStarter(), ...(runs || []).map(scoutRunCard)];
    if (!list.length && !(runs || []).length) kids.push(emptyCard('No scout runs yet', 'Ask a question above and the scout searches the web, showing each search as it happens. It also runs when you start a task with Scout turned on.', null));
    if (!list.length) return h('div', { class: 'groups' }, ...kids);
    if (!used.length && !(runs || []).length) kids.push(emptyCard('Scout was off for these tasks', 'Turn on Scout or Recall when you start a task. Cully then searches the web and your memory first, and you can see what it found here.', 'Start a task', '#/'));
    for (const v of used) {
      kids.push(h('section', { class: 'group' },
        h('header', {}, dot(agentOr(v.agentId, v.state)), h('a', { class: 'gt', href: '#/task/' + v.agentId }, v.title)),
        h('p', { class: 'faint' }, 'Looking into: ' + v.task),
        v.scout.status !== 'skipped' ? stageBlock('Web research', v.scout, 'search') : null,
        v.recall.status !== 'skipped' ? stageBlock('Memory recall', v.recall, 'eye') : null));
    }
    return h('div', { class: 'groups' }, ...kids);
  }

  // ----- plain text panels, formatted instead of dumped -----
  const HEADINGS = { Evidence: 'What Cully saw', 'Recovery steps': 'What to try next' };
  const EMPTY_RE = /nothing recorded|nothing to show|no cully suggestions|no sessions|no scheduled jobs|no jobs/i;
  const EMPTY_TEXT = {
    status: ['No sessions yet', 'Cully has not recorded a session for this project. Start a task from the Tasks tab and its health shows up here.'],
    rescue: ['Nothing to check yet', 'There is no session history for this project, so there is nothing to diagnose. If an agent gets stuck, the check appears here.'],
    suggestions: ['No tips right now', 'Cully has no suggestions from your terminal sessions.'],
    impact: ['Nothing to measure yet', 'Savings appear once Cully has recorded some sessions.'],
    jobs: ['No background jobs', 'A background job is one fixed command that runs at a time you choose, with no model and no tokens. None are set up. The Advisor proposes them from the chores you repeat; approve one on the Advisor page.'],
    timeline: ['No session to show', 'The timeline needs a recorded session.'],
    replay: ['No session to show', 'Replay needs a recorded session.'],
  };
  function shortenItem(t) {
    const m = /^([^:]{3,40}):\s*(.+)$/.exec(t);
    if (m && m[2].includes(', ')) {
      const parts = m[2].split(', ');
      if (parts.length > 6) return m[1] + ': ' + parts.slice(0, 6).join(', ') + ' and ' + (parts.length - 6) + ' more';
    }
    return t.length > 220 ? t.slice(0, 220) + '…' : t;
  }
  function renderText(name, text) {
    const t = (text || '').trim();
    const [etitle, ebody] = EMPTY_TEXT[name] || ['Nothing to show yet', 'There is nothing here yet.'];
    if (!t) return emptyCard(etitle, ebody, 'Start a task', '#/');
    if (name === 'timeline' || name === 'replay') return h('pre', { class: 'pre' }, t);
    const lines = t.split('\n').filter((l) => l.trim() && l.trim() !== 'CULLY');
    const dropped = lines.filter((l) => EMPTY_RE.test(l));
    const keep = lines.filter((l) => !EMPTY_RE.test(l));
    const blocks = [];
    let list = null;
    for (let i = 0; i < keep.length; i++) {
      const line = keep[i];
      const item = /^\s*(?:[-*]|\d+\.)\s+(.*)$/.exec(line);
      if (item) {
        const ordered = /^\s*\d+\./.test(line);
        if (!list || list.ordered !== ordered) { list = { ordered, el: h(ordered ? 'ol' : 'ul', { class: 'plain' }) }; blocks.push(list.el); }
        list.el.append(h('li', {}, ...inline(shortenItem(item[1]))));
        continue;
      }
      list = null;
      const next = keep[i + 1] || '';
      const isHead = /^\S/.test(line) && (/^\s/.test(next) || /^\s*(?:[-*]|\d+\.)\s/.test(next)) && line.length < 60;
      blocks.push(isHead ? h('h3', {}, HEADINGS[line.trim()] || line.trim()) : h('p', {}, ...inline(shortenItem(line.trim()))));
    }
    if (dropped.length && (!blocks.length || name === 'rescue')) return emptyCard(etitle, ebody, 'Start a task', '#/');
    return h('div', { class: 'plaintext' }, dropped.length ? emptyCard(etitle, ebody, null) : null, ...blocks);
  }

  // ----- structure -----
  const DASH = [
    [null, [['overview', 'Overview', 'What needs you']]],
    ['Work', [['advisor', 'Advisor', 'Advice for your tasks'], ['library', 'Session library', 'Every past session, analysed'], ['health', 'Session health', 'Did sessions finish the job?'], ['evals', 'Evals', 'Test cases and their runs'], ['scout', 'Scout', 'Live searches and findings'], ['history', 'History of changes', 'Every change, with undo'], ['activity', 'Activity', 'What was done, step by step']]],
    ['Health', [['savings', 'Savings', 'What Cully saved and cost']]],
    ['More', [['jobs', 'Background jobs', 'Runs on a schedule']]],
  ];
  const DASH_TITLES = Object.fromEntries(DASH.flatMap(([, items]) => items.map(([k, t, d]) => [k, [t, d]])));
  const fmtTokens = (n) => {
    const v = Math.abs(n);
    const out = v >= 1e6 ? (v / 1e6).toFixed(1).replace(/\.0$/, '') + 'M' : v >= 1e3 ? (v / 1e3).toFixed(1).replace(/\.0$/, '') + 'k' : String(v);
    return n < 0 ? '−' + out : out;
  };
  function sparkline(values) {
    const NS = 'http://www.w3.org/2000/svg';
    const w = 140; const hgt = 30; const max = Math.max(1, ...values);
    const svg = document.createElementNS(NS, 'svg');
    for (const [k, v] of Object.entries({ viewBox: '0 0 ' + w + ' ' + hgt, width: w, height: hgt, class: 'spark', 'aria-hidden': 'true' })) svg.setAttribute(k, v);
    const pts = values.map((v, i) => (values.length > 1 ? (i / (values.length - 1)) * w : 0).toFixed(1) + ',' + (hgt - 3 - (v / max) * (hgt - 6)).toFixed(1)).join(' ');
    const line = document.createElementNS(NS, 'polyline');
    for (const [k, v] of Object.entries({ points: pts, fill: 'none', stroke: 'currentColor', 'stroke-width': '2', 'stroke-linecap': 'round', 'stroke-linejoin': 'round' })) line.setAttribute(k, v);
    svg.append(line);
    return svg;
  }
  const loadingCard = () => h('div', { class: 'skeleton', 'aria-busy': 'true', 'aria-label': 'Loading' }, h('i'), h('i'), h('i'));

  // ----- overview -----
  function needsList() {
    const rows = [];
    for (const a of S.agents.filter((x) => x.state === 'waiting')) rows.push({ icon: 'alert', text: a.title + ' is waiting for you', label: 'Open', href: '#/task/' + a.id });
    for (const g of S.advice || []) {
      const n = g.advice.filter((x) => !x.applied && !x.dismissed).length;
      if (n) rows.push({ icon: 'spark', text: n + (n === 1 ? ' piece of advice' : ' pieces of advice') + ' for ' + g.agentTitle, label: 'Review', href: '#/dashboard/advisor' });
    }
    if (memoryMode() === 'mcp') for (const m of mcpNeeding()) rows.push({ icon: 'lock', text: 'Cully MCP needs sign-in for ' + AGENT_NAMES[m.agent], label: 'Sign in', onclick: () => openMCP(m.agent) });
    if (S.overview && S.overview.activity && S.overview.activity.facts.loops > 0) rows.push({ icon: 'alert', text: 'A recent session repeated a failing command', label: 'Look', href: '#/dashboard/activity' });
    if (S.overview && S.overview.setup && !S.overview.setup.allReady) rows.push({ icon: 'alert', text: 'Cully setup needs attention', label: 'See what', href: '#/setup' });
    return rows;
  }
  function renderNeeds() {
    const box = $('needs');
    if (!box) return;
    const rows = [...needsList(), ...(S.extraNeeds || [])];
    const asks = pendingAsks();
    const total = rows.length + asks.length;
    box.replaceChildren(
      h('h2', { class: 'hello' }, total ? (total === 1 ? 'One thing needs you' : total + ' things need you') : 'Nothing needs you right now'),
      ...asks.map(({ a, p }) => askCard(a, p)),
      rows.length ? h('ul', { class: 'needs' }, ...rows.map((r) => h('li', {}, icon(r.icon), h('span', {}, r.text),
        r.href ? h('a', { class: 'btn small', href: r.href }, r.label) : h('button', { class: 'btn small', onclick: r.onclick }, r.label))))
        : h('p', { class: 'faint' }, 'Tasks are running smoothly, or none are running. Start one from the Tasks tab.'));
  }
  async function renderOverview(out, head) {
    const repo = encodeURIComponent(S.opts.repo || '');
    const [impact, setup, activity, lib, usage, running, agents, proposals, coach, limitsAll] = await Promise.all([api('/api/impact?days=14').catch(() => null), api('/api/setup').catch(() => null), api('/api/activity?repo=' + repo).catch(() => null),
      api('/api/sessions/library').catch(() => null), api('/api/usage?days=7').catch(() => null), api('/api/running').catch(() => null), api('/api/identities').catch(() => []), api('/api/agent-suggestions').catch(() => []), api('/api/coach').catch(() => null), api('/api/limits').catch(() => null)]);
    S.overview = { impact, setup, activity };
    // Things that need a decision, beyond tasks waiting for a reply.
    const extra = [];
    try { S.rules = await api('/api/rules'); S.rulesAt = Date.now(); } catch (_) { /* optional */ }
    if (S.rules && S.rules.proposal) extra.push({ icon: 'spark', text: 'Cully drafted an update to your workflow rules and is waiting for your approval', label: 'Review', href: '#/improve/rules' });
    const drafts = agents.filter((x) => !x.approved);
    if (drafts.length) extra.push({ icon: 'check', text: drafts.length + (drafts.length === 1 ? ' agent is a draft waiting for your approval' : ' agents are drafts waiting for your approval'), label: 'Review', href: '#/agents' });
    if (proposals.length) extra.push({ icon: 'spark', text: 'Cully noticed work you repeat and proposed ' + proposals.length + (proposals.length === 1 ? ' agent' : ' agents'), label: 'Look', href: '#/agents' });
    // The harness compacts its own history near the limit, so a full window is not a problem. Many compactions are: detail from early on is gone.
    const worn = S.agents.filter((x) => x.context && x.context.compactions >= 3 && ['working', 'waiting'].includes(x.state));
    for (const x of worn) extra.push({ icon: 'alert', text: '"' + x.title + '" has been compacted ' + x.context.compactions + ' times, so early details may be lost', label: 'Look', href: '#/task/' + x.id });
    S.extraNeeds = extra;
    const sessions = lib ? lib.sessions : [];
    const dayAgo = (n) => Date.now() - n * 864e5;
    const week = sessions.filter((x) => new Date(x.updated).getTime() >= dayAgo(7)); const prior = sessions.filter((x) => { const t = new Date(x.updated).getTime(); return t < dayAgo(7) && t >= dayAgo(14); });
    const sec = (l) => l.reduce((n, x) => n + x.activeSec, 0);
    const trend = sec(prior) ? Math.round(((sec(week) - sec(prior)) * 100) / sec(prior)) : null;
    const judged = week.filter((x) => x.source === 'ai' && x.completed);
    const avgHealth = judged.length ? Math.round(judged.reduce((n, x) => n + x.health, 0) / judged.length) : null;
    const byHarness = {}; for (const x of week) byHarness[x.agent] = (byHarness[x.agent] || 0) + 1;
    const kpi = (n, l, sub) => h('div', { class: 'kpi' }, h('b', {}, n), h('span', {}, l), sub ? h('small', { class: 'faint' }, sub) : null);
    const kpis = h('div', { class: 'kpis' },
      kpi(dur(sec(week)), 'Time worked, 7 days', trend == null ? null : (trend >= 0 ? 'Up ' : 'Down ') + Math.abs(trend) + '% on the week before'),
      kpi(String(week.length), 'Sessions, 7 days', Object.entries(byHarness).map(([k, v]) => (AGENT_LABEL[k] || k) + ' ' + v).join(', ') || null),
      usage && usage.tasks ? kpi(fmtTokens(usage.tokens), 'Task tokens, 7 days', usage.cachedShare + '% from cache, about ' + fmtTokens(usage.saved) + ' saved') : kpi('None yet', 'Task tokens, 7 days', 'Shows once a task finishes'),
      avgHealth == null ? kpi('Not judged', 'Session health', 'Start analysing sessions') : kpi(String(avgHealth), 'Session health', judged.filter((x) => x.completed === 'yes').length + ' of ' + judged.length + ' finished the job'));
    // Right now.
    const live = S.agents.filter((x) => ['preparing', 'working', 'waiting'].includes(x.state));
    const taskRow = (x) => h('li', {}, dot(x), h('a', { class: 'rt', href: '#/task/' + x.id }, x.title),
      x.context ? h('span', { class: 'chip ' + (x.context.near ? 'warn' : ''), title: 'How full this task\'s context window is' }, x.context.percent + '% of context') : null, h('span', { class: 'faint small' }, stateLabel(x)));
    const open = running ? running.sessions.filter((x) => !x.detached) : [];
    const nowCard = h('section', { class: 'lib-card' }, h('div', { class: 'row' }, h('b', {}, 'Right now'), h('a', { class: 'linkbtn sp', href: '#/board' }, 'Open the board')),
      h('ul', { class: 'rn-list' },
        ...live.slice(0, 5).map(taskRow),
        ...(running ? running.agents.map((a) => h('li', {}, h('span', { class: 'dot working' }), h('a', { class: 'rt', href: '#/task/' + a.taskId }, a.name), h('span', { class: 'chip' }, 'Agent at work'))) : []),
        ...open.slice(0, 4).map((x) => h('li', {}, harnessChip(x.agent), h('b', {}, x.name), h('span', { class: 'faint small' }, 'open in a terminal')))),
      !live.length && !open.length && !(running && running.agents.length) ? h('p', { class: 'faint small' }, 'Nothing is running. Start a task, or open Claude, Codex or Cursor in a terminal and it shows here.') : null);
    const attention = judged.filter((x) => x.health < 70).sort((x, y) => x.health - y.health).slice(0, 3);
    const lookCard = h('section', { class: 'lib-card' }, h('div', { class: 'row' }, h('b', {}, 'Worth a look'), h('a', { class: 'linkbtn sp', href: '#/dashboard/health' }, 'Session health')),
      attention.length ? h('ul', { class: 'attn-list' }, ...attention.map((x) => h('li', {}, ring(x.health, 34), h('div', {}, sessionLink(x), harnessChip(x.agent), h('div', { class: 'faint small' }, (x.signals || []).join('. ') || 'Low health')))))
        : h('p', { class: 'faint small' }, judged.length ? 'Every session this week looks healthy.' : 'Once your sessions are analysed, the ones that went wrong show here.'),
      coach && coach.items.length ? h('div', { class: 'coachline' }, h('span', { class: 'kind' }, COACH_KIND[coach.items[0].kind] || coach.items[0].kind), h('div', {}, h('b', {}, coach.items[0].title), h('div', { class: 'faint small' }, coach.items[0].how)), h('button', { class: 'btn small', onclick: () => pushSuggestion(coach.items[0]) }, 'Apply')) : null);
    // Harness mix and agents.
    const mixTotal = Object.values(byHarness).reduce((n, v) => n + v, 0);
    const mix = mixTotal ? h('div', {}, h('span', { class: 'opt-label' }, 'Sessions by harness, 7 days'), h('div', { class: 'stackbar' }, ...Object.entries(byHarness).map(([k, v]) => { const i = h('i', { class: 'seg-' + k, title: (AGENT_LABEL[k] || k) + ': ' + v }); i.style.width = (v / mixTotal * 100) + '%'; return i; })),
      h('div', { class: 'row legend' }, ...Object.entries(byHarness).map(([k, v]) => h('span', { class: 'chip harness h-' + k }, (AGENT_LABEL[k] || k) + ' ' + v)))) : null;
    const limitsCard = h('section', { class: 'lib-card' }, h('b', {}, 'Usage limits'),
      ...((limitsAll && limitsAll.claude.length) ? limitsAll.claude.map((l) => h('div', { class: 'lim-acct' }, h('span', { class: 'chip harness h-claude' }, 'Claude'), h('span', { class: 'faint small' }, l.account), limitBar('Last 5 hours', l.limits.fiveHour), limitBar('Last 7 days', l.limits.sevenDay), limitsCheckBtn(l.limits.seen))) : [h('p', { class: 'faint small' }, 'Claude reports its limits when a task runs; none has run since this page started.'), limitsCheckBtn(0)]),
      limitsAll && limitsAll.codex && limitsAll.codex.limits ? h('div', { class: 'lim-acct' }, h('span', { class: 'chip harness h-codex' }, 'Codex'), limitBar('Primary window', limitsAll.codex.limits.fiveHour), limitBar('Secondary window', limitsAll.codex.limits.sevenDay)) : h('p', { class: 'faint small' }, 'Codex has not reported limits in its recent sessions.'),
      h('p', { class: 'faint small' }, 'Cursor does not record usage limits.'));
    const approved = agents.filter((x) => x.approved);
    const teamCard = h('section', { class: 'lib-card' }, h('div', { class: 'row' }, h('b', {}, 'Your agents'), h('a', { class: 'linkbtn sp', href: '#/agents' }, 'Manage')),
      agents.length || proposals.length ? h('p', { class: 'faint small' }, approved.length + (approved.length === 1 ? ' approved' : ' approved') + (drafts.length ? ', ' + drafts.length + ' draft' + (drafts.length === 1 ? '' : 's') : '') + (proposals.length ? ', ' + proposals.length + ' proposed by Cully' : '') + '. ' + approved.filter((x) => x.enabled).length + ' on a schedule.')
        : h('p', { class: 'faint small' }, 'No agents yet. Cully proposes one when it sees work you repeat, or you can create your own.'),
      mix);
    out.replaceChildren(head(refreshBtn(() => drawPanel('overview', true))),
      h('div', { id: 'needs', class: 'overview-needs' }),
      kpis,
      h('div', { class: 'insight-grid' }, nowCard, lookCard, teamCard, limitsCard),
      h('div', { class: 'glance' },
        h('a', { class: 'glance-row', href: '#/dashboard/savings' }, h('span', { class: 'gl' }, 'Savings'), h('span', { class: 'gv' }, impact && impact.net > 0 ? 'Cully saved about ' + fmtTokens(impact.net) + ' tokens in 14 days' : 'Nothing saved yet'), h('span', { class: 'ga' }, 'Details')),
        h('a', { class: 'glance-row', href: '#/setup' }, h('span', { class: 'gl' }, 'Setup'), h('span', { class: 'gv' }, !setup ? 'Not checked' : setup.allReady ? 'Everything is connected' : 'Needs attention'), h('span', { class: 'ga' }, 'Details')),
        h('a', { class: 'glance-row', href: '#/dashboard/library' }, h('span', { class: 'gl' }, 'Library'), h('span', { class: 'gv' }, lib ? lib.totals.sessions + ' sessions, ' + lib.totals.analyzed + ' named' : 'Not available'), h('span', { class: 'ga' }, 'Details')),
        h('a', { class: 'glance-row', href: '#/dashboard/activity' }, h('span', { class: 'gl' }, 'This folder'), h('span', { class: 'gv' }, activity && activity.recorded ? activity.facts.edits + (activity.facts.edits === 1 ? ' edit' : ' edits') + ' recorded' : 'Nothing recorded yet'), h('span', { class: 'ga' }, 'Details'))),
      h('section', { class: 'start-else' }, h('h3', {}, 'Start somewhere else'), h('p', { class: 'faint' }, 'Choose any folder on this machine and start a new task there.'),
        h('div', { class: 'row' }, h('button', { class: 'btn', onclick: () => openFolderChooser() }, icon('folder'), 'Choose a folder'),
          ...recentFolders(5).filter((r) => r.path !== S.opts.repo).map((r) => h('button', { class: 'chip', title: r.path, onclick: () => startIn(r.path) }, icon('folder'), r.name)))));
    renderNeeds();
  }

  // ----- setup -----

  // ----- setup page (#/setup) -----
  // A guided, never blocking stepper. Every step can be skipped and revisited; Finish always works.
  const SETUP_DOCS = 'https://docs.cully.net/choose-a-mode';
  const SETUP_STEPS = [['check', 'Welcome and check'], ['mode', 'Choose a mode'], ['rules', 'Rules interview'], ['review', 'Review and approve'], ['plan', 'What Cully could do for you'], ['done', 'Done']];
  const SETUP_HARNESS = ['claude', 'codex', 'cursor'];
  const SU = { step: 'check', state: null, interview: null, ans: {}, qi: 0, proposal: null, approved: null, sugg: null, mcpUrl: '', busy: '', err: '', added: [] };
  const suPost = (path, body) => api(path, { method: 'POST', body: JSON.stringify(body || {}) });

  function copyBox(text) {
    return h('div', { class: 'su-copy' }, h('code', {}, text), h('button', { class: 'btn ghost small', onclick: async () => {
      try { await navigator.clipboard.writeText(text); toast('Copied'); } catch (_) { toast('Select the command and copy it', true); }
    } }, icon('copy'), 'Copy'));
  }
  async function renderSetupPage() {
    const box = $('setup');
    if (!SU.state) box.replaceChildren(h('p', { class: 'faint' }, 'Loading…'));
    try { SU.state = await api('/api/setup/state'); } catch (e) { if (!SU.state) { box.replaceChildren(h('div', { class: 'err' }, e.message)); return; } }
    if (SU.state.mode && !SU.mcpUrl) SU.mcpUrl = SU.state.mcpUrl || '';
    drawSetupPage();
    if (!SU.interview) loadInterview();
  }
  async function loadInterview(path, post) {
    try { SU.interview = post ? await suPost(path, {}) : await api('/api/setup/interview'); } catch (_) { SU.interview = SU.interview || { questions: [], answers: {} }; }
    if (S.route.name === 'setup') drawSetupPage();
  }
  function goStep(id) { SU.step = id; SU.err = ''; drawSetupPage(); window.scrollTo(0, 0); if (id === 'plan' && !SU.sugg) loadSuggestions(); }
  async function loadSuggestions() {
    try { SU.sugg = await api('/api/setup/suggestions'); } catch (e) { SU.sugg = { basis: {}, agents: [], jobs: [], note: 'Suggestions are not available yet: ' + e.message }; }
    if (S.route.name === 'setup') drawSetupPage();
  }
  function drawSetupPage() {
    const box = $('setup');
    if (!box || S.route.name !== 'setup') return;
    const st = SU.state || {};
    const idx = SETUP_STEPS.findIndex((x) => x[0] === SU.step);
    const doneIds = { check: SETUP_HARNESS.length && (st.harnesses || []).some((x) => x.linked), mode: !!st.mode, rules: !!(st.rules && st.rules.exists) || !!SU.approved, review: !!SU.approved || !!(st.rules && st.rules.exists), plan: SU.added.length > 0, done: !!st.done };
    const rail = h('ol', { class: 'su-rail', 'aria-label': 'Setup steps' }, ...SETUP_STEPS.map(([id, title], i) => {
      const done = !!(doneIds[id] || window.CullySetup.stepDone(id, st));
      return h('li', {}, h('button', { class: 'su-step' + (id === SU.step ? ' on' : '') + (done ? ' done' : ''), 'aria-current': id === SU.step ? 'step' : null, onclick: () => goStep(id) },
        h('span', { class: 'su-num' }, done ? icon('check') : String(i + 1)), h('span', { class: 'su-st' }, title)));
    }));
    const body = { check: suCheck, mode: suMode, rules: suRules, review: suReview, plan: suPlan, done: suDone }[SU.step]();
    const nav = h('div', { class: 'su-nav' },
      idx > 0 ? h('button', { class: 'btn ghost', onclick: () => goStep(SETUP_STEPS[idx - 1][0]) }, 'Back') : null, h('span', { class: 'sp' }),
      SU.step !== 'done' ? h('button', { class: 'btn ghost', onclick: () => goStep(SETUP_STEPS[idx + 1][0]) }, 'Skip this step') : null,
      SU.step !== 'done' ? h('button', { class: 'btn ghost', onclick: suFinish }, 'Finish') : null);
    box.replaceChildren(h('div', { class: 'su' },
      h('header', { class: 'su-head' }, h('h1', {}, 'Set up Cully'), h('p', { class: 'faint' }, 'Take the steps in any order, skip any of them, and come back later. Nothing here blocks you.'), st.done ? h('span', { class: 'pill working' }, 'Setup finished') : h('span', { class: 'pill waiting' }, 'Not finished')),
      h('div', { class: 'su-grid' }, rail, h('section', { class: 'su-body' }, SU.err ? h('div', { class: 'err' }, SU.err) : null, body, nav))));
  }
  async function suFinish() {
    try { SU.state = await suPost('/api/setup/complete'); S.info = await api('/api/info').catch(() => S.info); goStep('done'); } catch (e) { SU.err = e.message; drawSetupPage(); }
  }

  // 1. Welcome and check
  function suCheck() {
    const st = SU.state || {};
    const byId = Object.fromEntries((st.harnesses || []).map((x) => [x.id, x]));
    const cards = SETUP_HARNESS.map((id) => {
      const x = byId[id] || { id, name: AGENT_LABEL[id] || id, installed: false };
      return h('div', { class: 'su-card' }, h('div', { class: 'row' }, h('b', {}, x.name || AGENT_LABEL[id]), h('span', { class: 'sp' }), h('span', { class: 'pill ' + (x.installed ? 'working' : '') }, x.installed ? 'Installed' : 'Not installed')),
        h('p', { class: 'faint small' }, x.installed ? (x.version ? 'Version ' + x.version : 'Version not reported') : 'Not found on this machine.'),
        x.installed ? h('span', { class: 'pill ' + (x.linked ? 'working' : 'waiting') }, x.linked ? 'Linked to the shared skill' : 'Not linked yet') : null,
        x.linked && x.skillPath ? h('p', { class: 'faint small su-path' }, x.skillPath) : null);
    });
    return h('div', {}, h('h2', {}, 'Welcome'), h('p', { class: 'su-lead' }, 'Cully keeps your rules and skills in one place and links them into each coding agent you use. Rules and skills live in ~/.cully and are linked into each harness.'),
      h('div', { class: 'su-cards' }, ...cards),
      h('div', { class: 'su-card su-term' }, h('b', {}, 'Prefer the terminal?'), h('p', { class: 'faint small' }, 'Run this. Both do the same thing.'), copyBox('cully setup')),
      h('div', { class: 'row' }, h('button', { class: 'btn', onclick: () => goStep('mode') }, 'Continue')));
  }

  // 2. Mode
  function suMode() {
    const st = SU.state || {}; const modes = st.modes || {};
    const url = h('input', { type: 'text', 'aria-label': 'MCP URL', placeholder: 'https://your-cully-server/mcp', autocomplete: 'off', spellcheck: 'false' });
    url.value = SU.mcpUrl; url.addEventListener('input', () => { SU.mcpUrl = url.value; });
    const pick = async (mode) => {
      SU.busy = mode; SU.err = '';
      try { SU.state = await suPost('/api/setup/mode', mode === 'mcp' ? { mode, mcpUrl: SU.mcpUrl.trim() } : { mode }); toast(mode === 'mcp' ? 'MCP mode saved' : 'Local mode saved'); } catch (e) { SU.err = e.message; }
      SU.busy = ''; drawSetupPage();
    };
    const card = (mode, title, tag, extra) => h('div', { class: 'su-mode' + (st.mode === mode ? ' on' : '') },
      h('div', { class: 'row' }, h('b', {}, title), st.mode === mode ? h('span', { class: 'pill working' }, 'Chosen') : null, tag ? h('span', { class: 'pill' }, tag) : null),
      h('p', {}, modes[mode] || ''), extra,
      h('div', { class: 'row' }, h('button', { class: mode === 'local' ? 'btn' : 'btn ghost', disabled: SU.busy === mode, onclick: () => pick(mode) }, mode === 'local' ? 'Use local mode' : 'Use MCP mode')));
    return h('div', {}, h('h2', {}, 'Choose a mode'), h('p', { class: 'su-lead' }, 'You can change this later.'),
      h('div', { class: 'su-modes' },
        card('local', 'One machine: Local mode', 'Recommended for one computer', h('p', { class: 'faint small' }, 'Memory and recall are stored on this machine. Nothing to deploy.')),
        card('mcp', 'Many machines, VMs or shared hosts: MCP mode', 'Preferred if you can deploy it', h('div', {},
          h('p', { class: 'faint small' }, 'You deploy the MCP service, the Cully service, Postgres and Mem0. One memory is shared by every machine. It is more efficient and the preferred way if you can deploy it.'),
          h('label', { class: 'field' }, h('span', { class: 'opt-label' }, 'MCP URL'), url),
          h('a', { class: 'linkbtn', href: SETUP_DOCS, target: '_blank', rel: 'noopener' }, icon('external'), 'How to deploy it')))));
  }

  // 3. Rules interview
  function suQs() { return (SU.interview && SU.interview.questions) || []; }
  function suValue(q) {
    if (SU.ans[q.id] !== undefined) return SU.ans[q.id] === null ? '' : SU.ans[q.id];
    const saved = ((SU.interview || {}).answers || {})[q.id];
    return saved != null ? saved : (q.suggested || '');
  }
  function suRules() {
    const W = window.CullySetup; const qs = suQs();
    if (!SU.interview) return h('p', { class: 'faint' }, 'Loading the questions…');
    const analyse = h('button', { class: 'btn ghost', onclick: async () => {
      analyse.disabled = true; toast('Reading your sessions…');
      await loadInterview('/api/setup/interview/analyse', true);
    } }, icon('spark'), 'Improve with my sessions');
    if (!qs.length) return h('div', {}, h('h2', {}, 'Rules interview'), h('p', { class: 'faint' }, 'There are no questions yet. You can skip this step.'), h('div', { class: 'row' }, analyse));
    SU.qi = Math.min(Math.max(SU.qi, 0), qs.length - 1);
    const q = qs[SU.qi];
    const t = W.tally(qs, SU.ans);
    let field;
    if (q.kind === 'choice' && (q.choices || []).length) { field = h('select', { 'aria-label': q.text }, ...q.choices.map((c) => h('option', { value: c }, c))); }
    else field = h('textarea', { rows: q.kind === 'list' ? '5' : '3', 'aria-label': q.text, placeholder: q.kind === 'list' ? 'One per line' : '' });
    field.value = suValue(q);
    const go = (v) => { if (v !== undefined) SU.ans[q.id] = v; if (SU.qi < qs.length - 1) { SU.qi++; drawSetupPage(); } else drawSetupPage(); };
    const last = SU.qi === qs.length - 1;
    return h('div', {}, h('h2', {}, 'Rules interview'),
      h('p', { class: 'su-lead' }, 'Answer, edit or skip each question. Cully writes your dev-workflow rules from the answers.'),
      h('div', { class: 'row su-prog' }, h('span', { class: 'pill' }, W.progress(SU.qi, qs.length)), h('span', { class: 'faint small' }, t.answered + ' answered, ' + t.skipped + ' skipped'), h('span', { class: 'sp' }), analyse),
      h('div', { class: 'su-bar', role: 'progressbar', 'aria-valuemin': '0', 'aria-valuemax': String(qs.length), 'aria-valuenow': String(SU.qi + 1) }, (() => { const i = h('i', {}); i.style.width = Math.round(((SU.qi + 1) / qs.length) * 100) + '%'; return i; })()),
      h('div', { class: 'su-card su-q' },
        q.group ? h('div', { class: 'su-group' }, q.group) : null, h('h3', {}, q.text), q.hint ? h('p', { class: 'faint small' }, q.hint) : null,
        h('label', { class: 'field' }, h('span', { class: 'opt-label' }, 'Your answer'), field),
        q.evidence ? h('p', { class: 'su-ev' }, icon('search'), q.evidence) : h('p', { class: 'su-ev faint' }, 'No evidence found in your sessions for this one.'),
        h('div', { class: 'row' },
          h('button', { class: 'btn', onclick: () => go(field.value) }, icon('check'), 'Use this'),
          h('button', { class: 'btn ghost', onclick: () => field.focus() }, icon('pencil'), 'Edit'),
          h('button', { class: 'btn ghost', onclick: () => go(null) }, 'Skip'),
          h('span', { class: 'sp' }),
          h('button', { class: 'btn ghost', disabled: SU.qi === 0, onclick: () => { SU.qi--; drawSetupPage(); } }, 'Back'))),
      h('div', { class: 'su-dots' }, ...qs.map((x, i) => { const a = SU.ans[x.id]; const b = h('button', { class: 'su-dot' + (i === SU.qi ? ' on' : '') + (a === null ? ' skip' : typeof a === 'string' ? ' got' : ''), title: x.text, 'aria-label': 'Question ' + (i + 1), onclick: () => { SU.qi = i; drawSetupPage(); } }); return b; })),
      h('div', { class: 'row' }, h('button', { class: last ? 'btn' : 'btn ghost', disabled: SU.busy === 'propose', onclick: suPropose }, 'Build my rules from these answers')));
  }
  async function suPropose() {
    SU.busy = 'propose'; SU.err = '';
    const answers = {};
    for (const q of suQs()) { const a = SU.ans[q.id]; answers[q.id] = a === null ? null : a !== undefined ? a : suValue(q) || null; }
    try { const r = await suPost('/api/setup/interview', { answers }); SU.proposal = r.proposal || null; SU.approved = null; SU.busy = ''; goStep('review'); } catch (e) { SU.err = e.message; SU.busy = ''; drawSetupPage(); }
  }

  // 4. Review and approve
  function suReview() {
    const W = window.CullySetup;
    if (SU.approved) {
      const a = SU.approved;
      return h('div', {}, h('h2', {}, 'Rules approved'), h('div', { class: 'su-card' }, h('p', {}, 'Your dev-workflow rules were saved' + ((a.linked || []).length ? ' and linked to ' + a.linked.join(', ') + '.' : '.')),
        a.path ? h('p', { class: 'faint small su-path' }, 'Source file: ' + a.path) : null,
        (a.linked || []).length ? h('div', { class: 'row' }, ...a.linked.map((x) => h('span', { class: 'pill working' }, icon('check'), x))) : h('p', { class: 'faint small' }, 'No harness was linked. Install one and run cully setup again.')),
        h('div', { class: 'row' }, h('button', { class: 'btn', onclick: () => goStep('plan') }, 'Continue')));
    }
    const p = SU.proposal;
    if (!p) return h('div', {}, h('h2', {}, 'Review and approve'), h('p', { class: 'su-lead' }, 'There is no proposal yet. Answer or skip the interview questions, then build your rules from them.'),
      h('div', { class: 'row' }, h('button', { class: 'btn', onclick: () => goStep('rules') }, 'Go to the interview')));
    const rows = W.collapseDiff(p.diff, 2);
    const view = h('div', { class: 'su-diff', role: 'table', 'aria-label': 'Proposed changes to your rules' }, ...rows.map((r) => r.t === 'fold'
      ? h('div', { class: 'su-dl fold', role: 'row' }, h('span', { class: 'su-dm' }, ''), h('span', {}, r.n + ' unchanged ' + (r.n === 1 ? 'line' : 'lines')))
      : h('div', { class: 'su-dl ' + r.t, role: 'row' }, h('span', { class: 'su-dm' }, r.t === 'add' ? '+' : r.t === 'del' ? '-' : ''), h('span', {}, r.l || ' '))));
    const approve = h('button', { class: 'btn', onclick: async () => {
      approve.disabled = true;
      try { SU.approved = await suPost('/api/setup/rules/approve', { id: p.id }); SU.state = await api('/api/setup/state').catch(() => SU.state); drawSetupPage(); } catch (e) { SU.err = e.message; drawSetupPage(); }
    } }, icon('check'), 'Approve and link to my harnesses');
    const discard = h('button', { class: 'btn ghost', onclick: async () => { try { await suPost('/api/setup/rules/discard', { id: p.id }); } catch (_) { /* nothing saved either way */ } SU.proposal = null; toast('Discarded. Nothing was changed.'); drawSetupPage(); } }, 'Discard');
    return h('div', {}, h('h2', {}, 'Review and approve'),
      h('p', { class: 'su-lead' }, p.summary || W.diffSentence(p.diff)), h('p', { class: 'faint small' }, W.diffSentence(p.diff) + '. Nothing is saved until you approve.'),
      view, h('div', { class: 'row' }, approve, discard));
  }

  // 5. What Cully could do for you
  function suPlan() {
    const W = window.CullySetup; const sg = SU.sugg; const st = SU.state || {};
    const basis = (sg && sg.basis) || st.sessions || {};
    const by = (st.sessions && st.sessions.byHarness) || {};
    const head = h('h2', {}, 'What Cully could do for you');
    if (!sg) return h('div', {}, head, h('p', { class: 'faint' }, 'Reading your sessions…'));
    const tile = (n, l) => h('div', { class: 'su-tile' }, h('b', {}, n), h('span', {}, l));
    const tiles = h('div', { class: 'su-tiles' }, tile(String(basis.sessions || 0), 'sessions'), tile(W.hours(basis.hours), 'of work'), tile(W.tokens(basis.tokens), 'tokens'), tile(String(basis.days || 0), 'days'));
    const bars = (key, label, fmt) => h('div', { class: 'su-bars' }, h('div', { class: 'faint small' }, label), ...W.barScale(by, key, SETUP_HARNESS).map((b) => {
      const i = h('i', {}); i.style.width = b.pct + '%';
      return h('div', { class: 'su-brow' }, h('span', { class: 'su-bl' }, AGENT_LABEL[b.id] || b.id), h('span', { class: 'su-bt' }, i), h('span', { class: 'su-bv' }, b.value ? fmt(b.value) : 'none'));
    }));
    const ev = (x) => [W.evidenceWord(x.count), W.saves(x.estMinutesPerWeek), W.costs(x.estTokensPerRun)].filter(Boolean);
    const sources = (x) => ((x.sources || []).length ? h('p', { class: 'faint small su-src' }, 'Backed by: ', ...x.sources.flatMap((r, i) => [i ? ', ' : '', h('a', { class: 'ext', href: r.url, target: '_blank', rel: 'noopener', title: r.kind === 'paper' ? 'A paper' : r.kind === 'vendor' ? 'A vendor engineering post, not independent evidence' : 'A practitioner write-up, not independent evidence' }, r.title)])) : null);
    const sCard = (x, kind) => h('div', { class: 'su-card su-sug' }, h('div', { class: 'row' }, h('b', {}, x.name), h('span', { class: 'pill' }, kind === 'agent' ? (x.replaces ? 'Update to your agent' : 'Agent') : 'Background job')),
      h('p', {}, x.role && x.why && x.role !== x.why && !x.replaces ? x.role : (x.why || x.role || '')), x.role && x.why && !x.replaces && x.role !== x.why ? h('p', { class: 'faint small' }, x.why) : null, sources(x), kind === 'job' && x.command ? h('p', {}, h('code', {}, x.command), x.schedule ? h('span', { class: 'faint small' }, '  ' + x.schedule) : null) : null,
      h('div', { class: 'row' }, ...ev(x).map((t) => h('span', { class: 'chip' }, t))),
      (x.evidence || []).length ? h('ul', { class: 'su-evl' }, ...x.evidence.slice(0, 3).map((e) => h('li', { class: 'faint small' }, String(e)))) : null,
      h('div', { class: 'row' }, h('button', { class: 'btn small', onclick: () => (kind === 'agent' ? suAddAgent(x) : suAddJob(x)) }, icon('plus'), kind === 'agent' && x.replaces ? 'Review update' : 'Review and add')));
    const readyAgents = sg.ready || []; const readyJobs = sg.readyJobs || [];
    const ready = readyAgents.length || readyJobs.length ? h('div', {}, h('h3', {}, 'Ready-made, from research'),
      h('p', { class: 'faint small' }, 'These need no history. Each is short, read only where it can be, and tied to a documented failure. Nothing is created until you review and approve it.'),
      h('div', { class: 'su-cards' }, ...readyAgents.map((x) => sCard(x, 'agent')), ...readyJobs.map((x) => sCard(x, 'job')))) : null;
    let cards;
    if (!W.enough(basis)) cards = h('div', { class: 'su-card' }, h('p', {}, sg.note || 'Cully needs about ' + W.MIN_SESSIONS + ' analysed sessions before it can suggest agents and jobs. Come back after a few more days of work.'));
    else cards = h('div', {}, (sg.agents || []).length ? h('h3', {}, 'Agents') : null, h('div', { class: 'su-cards' }, ...(sg.agents || []).map((x) => sCard(x, 'agent'))),
      (sg.jobs || []).length ? h('h3', {}, 'Background jobs') : null, h('div', { class: 'su-cards' }, ...(sg.jobs || []).map((x) => sCard(x, 'job'))),
      !(sg.agents || []).length && !(sg.jobs || []).length ? h('p', { class: 'faint' }, sg.note || 'Nothing to suggest yet.') : null);
    return h('div', {}, head, h('p', { class: 'su-lead' }, W.basisSentence(basis) + '.'), tiles,
      Object.keys(by).length ? h('div', { class: 'su-two' }, bars('hours', 'Hours of work', W.hours), bars('tokens', 'Tokens used', W.tokens)) : null,
      h('p', { class: 'faint small' }, 'All figures are estimates from your own sessions, so they say about.'), ready, cards);
  }
  async function suAddAgent(x) {
    let tools; try { tools = await api('/api/admin/tools'); } catch (e) { toast(e.message, true); return; }
    if (x.replaces) {
      // An agent you already have covers this: open it with the suggested role, so saving updates it.
      let cur = null; try { cur = (await api('/api/identities')).find((a) => a.id === x.replaces); } catch (_) {}
      if (cur) { openAgentForm({ ...cur, role: x.role || cur.role }, tools, () => { SU.added.push(x.id || x.name); drawSetupPage(); }); return; }
    }
    openAgentForm({ id: null, name: x.name, role: x.role || x.why || '', instructions: x.instructions || '', scope: 'project', folder: x.folder || S.opts.repo || (S.info && S.info.repos[0]) || '', groups: Object.fromEntries(tools.presets.map((p) => [p.id, p.on])), mode: x.mode || 'edit', schedule: '', enabled: false },
      tools, () => { SU.added.push(x.id || x.name); drawSetupPage(); });
  }
  function suAddJob(x) {
    const ed = scheduleEditor({ schedule: x.schedule || '', after: x.after, maxRuns: x.maxRuns });
    const name = h('input', { type: 'text', 'aria-label': 'Name', value: x.name, autocomplete: 'off' });
    const cmd = h('input', { type: 'text', 'aria-label': 'Command', value: x.command || '', autocomplete: 'off', spellcheck: 'false' });
    const err = h('div', { class: 'err', hidden: true });
    const save = h('button', { class: 'btn', onclick: async () => {
      save.disabled = true; err.hidden = true;
      try { await suPost('/api/jobs', { name: name.value, folder: x.folder || S.opts.repo || '', command: cmd.value, ...ed.value() }); closeMCP(); toast('Added ' + name.value); SU.added.push(x.id || x.name); drawSetupPage(); } catch (e) { err.textContent = e.message; err.hidden = false; save.disabled = false; }
    } }, icon('check'), 'Approve and add');
    const modal = h('div', { class: 'modal', role: 'dialog', 'aria-modal': 'true', 'aria-label': 'New background job' },
      h('header', {}, h('h2', {}, 'New background job'), h('button', { class: 'btn ghost small sp', onclick: closeMCP }, 'Cancel')),
      h('div', { class: 'mbody' }, h('p', { class: 'faint small' }, x.why || ''), h('label', { class: 'field' }, h('span', { class: 'opt-label' }, 'Name'), name), h('label', { class: 'field' }, h('span', { class: 'opt-label' }, 'Command'), cmd), ed.el, err, h('div', { class: 'row' }, save)));
    const bg = h('div', { class: 'modal-bg', onclick: (e) => { if (e.target === bg) closeMCP(); } }, modal);
    $('modal-root').replaceChildren(bg);
    S.modal = { agent: 'confirm' };
    document.removeEventListener('keydown', escClose); document.addEventListener('keydown', escClose);
  }

  // 6. Done
  function suDone() {
    const st = SU.state || {}; const rules = st.rules || {};
    const linked = (st.harnesses || []).filter((x) => x.linked).map((x) => x.name);
    const line = (ok, text) => h('li', { class: ok ? 'ok' : 'bad' }, icon(ok ? 'check' : 'x'), h('span', { class: 'sl' }, text));
    return h('div', {}, h('h2', {}, st.done ? 'You are set up' : 'Almost there'),
      h('p', { class: 'su-lead' }, 'Here is where things stand. You can reopen this page from More, then Setup, at any time.'),
      h('ul', { class: 'checks' }, line(!!st.mode, st.mode ? (st.mode === 'mcp' ? 'MCP mode' : 'Local mode') + ' chosen' : 'No mode chosen yet'),
        line(!!rules.exists, rules.exists ? 'Dev-workflow rules saved' + (rules.lines ? ' (' + rules.lines + ' lines)' : '') : 'No dev-workflow rules yet'),
        line(linked.length > 0, linked.length ? 'Linked to ' + linked.join(', ') : 'Not linked to a harness yet'),
        line(SU.added.length > 0, SU.added.length ? SU.added.length + ' suggested ' + (SU.added.length === 1 ? 'item' : 'items') + ' added' : 'No agents or jobs added from suggestions')),
      rules.path ? h('p', { class: 'faint small su-path' }, 'Rules file: ' + rules.path) : null,
      h('div', { class: 'row' }, h('a', { class: 'btn ghost', href: '#/agents' }, 'Agents'), h('a', { class: 'btn ghost', href: '#/dashboard/jobs' }, 'Background jobs'), h('a', { class: 'btn ghost', href: '#/improve/rules' }, 'Rules'), h('span', { class: 'sp' }),
        h('button', { class: 'btn', onclick: async () => { if (!st.done) await suFinish().catch(() => {}); location.hash = '#/'; } }, 'Start a task')));
  }

  // A calm banner on the new-task page until setup is finished.
  function setupBanner() {
    if (!S.info || S.info.setupDone !== false) return null;
    let off = false; try { off = sessionStorage.getItem('cully-setup-banner-off') === '1'; } catch (_) { /* optional */ }
    if (off) return null;
    const box = h('div', { class: 'su-banner', role: 'note' }, icon('spark'), h('span', {}, 'Setup is not finished. Set it up in the UI or run ', h('code', {}, 'cully setup'), ' in a terminal.'),
      h('a', { class: 'btn small', href: '#/setup' }, 'Open setup'),
      h('button', { class: 'btn ghost small', 'aria-label': 'Dismiss', onclick: () => { try { sessionStorage.setItem('cully-setup-banner-off', '1'); } catch (_) { /* optional */ } box.remove(); } }, 'Dismiss'));
    return box;
  }

  // ----- savings -----
  async function renderSavings(out, head, days) {
    const v = await api('/api/impact?days=' + days);
    const u = await api('/api/usage?days=' + days).catch(() => null);
    const seg = h('div', { class: 'seg', role: 'group', 'aria-label': 'Period' }, ...[7, 14, 30].map((d) => h('button', { type: 'button', class: d === days ? 'on' : '', onclick: () => { S.dashSig = ''; drawPanel('savings', true, d); } }, d + ' days')));
    const total = v.saved + v.cost;
    const bar = h('div', { class: 'balance', role: 'img', 'aria-label': 'Saved versus spent' }, h('span', { class: 'saved' }), v.cost > 0 ? h('span', { class: 'cost' }) : null);
    bar.querySelector('.saved').style.flexGrow = String(Math.max(v.saved, total ? 0 : 1));
    if (v.cost > 0) bar.querySelector('.cost').style.flexGrow = String(v.cost);
    const max = Math.max(1, ...v.daily.map((d) => d.saved));
    const bars = h('div', { class: 'bars', role: 'img', 'aria-label': 'Tokens saved per day' }, ...v.daily.map((d) => { const i = h('i', { class: d.saved ? '' : 'zero', title: d.date + ': ' + fmtTokens(d.saved) + ' saved' }); i.style.height = Math.max(4, Math.round((d.saved / max) * 100)) + '%'; return i; }));
    const stat = (n, label, note) => h('div', { class: 'kpi' }, h('b', {}, n), h('span', {}, label), note ? h('small', { class: 'faint' }, note) : null);
    out.replaceChildren(head(seg, refreshBtn(() => drawPanel('savings', true, days))),
      h('div', { class: 'hero-sav' },
        h('p', { class: 'big' }, v.net > 0 ? ['Cully saved about ', h('b', {}, fmtTokens(v.net)), ' tokens in the last ' + v.days + ' days.'] : 'Cully has not saved anything yet in the last ' + v.days + ' days.'),
        v.sessions ? h('p', { class: 'faint' }, 'Across ' + v.sessions + (v.sessions === 1 ? ' recorded session.' : ' recorded sessions.')) : h('p', { class: 'faint' }, 'No sessions recorded in this period.'),
        bar, h('div', { class: 'legend' }, h('span', {}, h('i', { class: 'k saved' }), 'Saved ' + fmtTokens(v.saved)), h('span', {}, h('i', { class: 'k cost' }), 'Spent ' + fmtTokens(v.cost))),
        bars, h('div', { class: 'axis faint small' }, h('span', {}, v.daily[0].date), h('span', {}, 'today'))),
      h('div', { class: 'stats' }, stat(String(v.memoryHits), 'Memory lookups', 'times prior work was found'), stat(String(v.analysesSkipped), 'Analyses avoided', 'estimated tokens not sent'), stat(String(v.loopsCaught), 'Loops caught', 'repeated failing commands'), stat(String(v.wasteSessions), 'Oversized output', v.wasteTokens ? fmtTokens(v.wasteTokens) + ' tokens flagged' : 'sessions flagged')),
      tasksTokenCard(u),
      h('details', { class: 'more' }, h('summary', {}, 'How this is measured'),
        h('p', {}, 'Saved is an estimate of tokens Cully avoided sending, such as analyses it skipped. Spent is what its memory lookups cost, measured. Memory savings are only counted when there is a trustworthy with-and-without comparison.'),
        h('p', {}, 'Tokens used by your tasks are counted from what each task reports when it finishes. Input served from the prompt cache costs about a tenth of fresh input, so about nine tenths of the cached tokens are counted as saved. Limiting the connected servers is compared by the input each turn carries, for tasks that loaded every server and tasks limited to the ones you chose; it needs a few of each before it shows a number. Only counts are stored.'),
        v.basis ? h('p', { class: 'faint' }, v.basis) : null));
  }

  // Where the tokens went, by kind of work (edits, reads, shell, each MCP server, replies) instead of by folder.
  // Fresh input and output are split across the tools each turn used, so these are estimates; the cache line is exact.
  function kindBars(u) {
    if (!u.kinds || !u.kinds.length) return null;
    const total = u.kinds.reduce((n, k) => n + k.tokens, 0) || 1; const max = Math.max(1, ...u.kinds.map((k) => k.tokens));
    return h('div', {}, h('span', { class: 'opt-label' }, 'Where the tokens went'),
      h('ul', { class: 'fbars' }, ...u.kinds.map((k) => h('li', {}, h('span', { class: 'fname', title: k.name }, k.name), h('span', { class: 'fbar' }, fill(Math.round(k.tokens / max * 100))), h('span', { class: 'faint small' }, fmtTokens(k.tokens) + ' (' + Math.round(k.tokens * 100 / total) + '%)')))),
      h('p', { class: 'faint small' }, 'Estimated: each turn\'s fresh input and output are shared across the tools it called. Only tasks finished since this was added are counted.'));
  }
  // What the tasks started from Cully really used, and what the cache and the connected-tools choice saved.
  function tasksTokenCard(u) {
    if (!u) return null;
    if (!u.tasks) return h('section', { class: 'lib-card' }, h('b', {}, 'Tokens used by your tasks'), h('p', { class: 'faint small' }, 'No finished tasks in this period yet. When a task started from Cully finishes, what it used shows up here: how much came from the prompt cache, and where the rest went.'));
    const max = Math.max(1, ...u.daily.map((d) => d.tokens));
    const chart = h('div', { class: 'daychart', role: 'img', 'aria-label': 'Tokens used per day' }, ...u.daily.map((d) => { const bar = h('i', { title: d.date + ': ' + fmtTokens(d.tokens) + ' tokens, ' + d.tasks + (d.tasks === 1 ? ' task' : ' tasks') }); bar.style.height = Math.max(d.tokens ? 6 : 2, Math.round((d.tokens / max) * 100)) + '%'; return h('span', { class: 'dc' + (d.tokens ? '' : ' none') }, bar, h('small', {}, d.date.slice(8))); }));
    const maxF = Math.max(1, ...u.folders.map((f) => f.tokens));
    const mcp = u.mcpSaved > 0
      ? h('p', {}, 'Limiting connected servers saved about ', h('b', {}, fmtTokens(u.mcpSaved)), ' tokens: tasks that loaded every server carried about ' + fmtTokens(u.allPerTurn) + ' input tokens a turn, and those limited to your choice about ' + fmtTokens(u.filteredPerTurn) + '.')
      : h('p', { class: 'faint small' }, u.filteredTasks ? 'Limiting connected servers is on for ' + u.filteredTasks + ' ' + (u.filteredTasks === 1 ? 'task' : 'tasks') + '. A comparison shows once there are tasks of both kinds.' : 'Every task loaded all your connected servers. Choosing only the ones tasks need, on the Harness permissions page, usually cuts the input each turn carries.');
    return h('section', { class: 'lib-card' }, h('b', {}, 'Tokens used by your tasks'),
      h('div', { class: 'kpis' }, ...[[fmtTokens(u.tokens), 'Tokens handled'], [u.cachedShare + '%', 'Input from the cache'], [fmtTokens(u.saved), 'Saved by the cache'], [String(u.tasks), u.tasks === 1 ? 'Task' : 'Tasks']].map(([n, l]) => h('div', { class: 'kpi' }, h('b', {}, n), h('span', {}, l)))),
      chart, mcp,
      kindBars(u),
      u.folders.length && !(u.kinds && u.kinds.length) ? h('div', {}, h('span', { class: 'opt-label' }, 'Where the tokens went'), h('ul', { class: 'fbars' }, ...u.folders.map((f) => h('li', {}, h('span', { class: 'fname' }, f.name), h('span', { class: 'fbar' }, fill(Math.round(f.tokens / maxF * 100))), h('span', { class: 'faint small' }, fmtTokens(f.tokens)))))) : null,
      u.top.length ? h('div', {}, h('span', { class: 'opt-label' }, 'Biggest tasks'), h('ul', { class: 'hrows' }, ...u.top.map((t) => h('li', { class: 'hrow' }, h('div', { class: 'hmain' }, h('div', { class: 'ht' }, t.title), h('div', { class: 'row' }, h('span', { class: 'chip' }, fmtTokens(t.tokens) + ' tokens'), h('span', { class: 'chip' }, t.cachedShare + '% from cache'), h('span', { class: 'chip' }, ago(t.time)))), h('div', { class: 'row hact' }, h('a', { class: 'btn ghost small', href: '#/task/' + t.taskId }, 'Open')))))) : null);
  }

  // ----- activity -----
  const KIND_ICON = { edit: 'pencil', read: 'eye', search: 'search', check: 'check', command: 'term', memory: 'spark', note: 'alert', start: 'plus', end: 'check' };
  const KIND_WORD = { edit: 'Edited', read: 'Read', search: 'Searched', check: 'Ran check', command: 'Ran', memory: 'Used', note: '', start: 'Started', end: 'Ended' };
  function groupBars(sum) {
    if (!sum.groups || !sum.groups.length) return null;
    const max = Math.max(...sum.groups.map((g) => g.count));
    return h('ul', { class: 'gbars' }, ...sum.groups.map((g) => { const bar = h('i'); bar.style.width = Math.max(4, Math.round((g.count / max) * 100)) + '%'; return h('li', {}, h('span', { class: 'gn' }, g.name), h('span', { class: 'gb' }, bar), h('span', { class: 'gc' }, String(g.count))); }));
  }
  function renderTimeline(data, filter) {
    const keep = (e) => filter === 'all' || (filter === 'files' && ['edit', 'read', 'search'].includes(e.kind)) || (filter === 'commands' && ['command', 'check'].includes(e.kind)) || (filter === 'problems' && (e.failed || e.kind === 'note'));
    const list = data.events.filter((e) => keep(e) && e.kind !== 'end');
    if (!list.length) return h('p', { class: 'faint' }, 'Nothing matches this filter.');
    const blocks = []; let cur = null; let last = 0;
    for (const e of list) {
      const t = new Date(e.t).getTime();
      if (!cur || t - last > 10 * 60 * 1000) { cur = { at: e.t, items: [] }; blocks.push(cur); }
      cur.items.push(e); last = t;
    }
    return h('div', { class: 'timeline' }, ...blocks.map((b) => h('section', { class: 'tl-block' }, h('div', { class: 'tl-when' }, new Date(b.at).toLocaleString([], { weekday: 'short', hour: '2-digit', minute: '2-digit' })),
      h('ul', {}, ...b.items.map((e) => h('li', { class: (e.failed ? 'bad ' : '') + e.kind }, icon(e.failed ? 'x' : KIND_ICON[e.kind] || 'spark'), h('span', { class: 'tw' }, KIND_WORD[e.kind] || ''), h('span', { class: 'tl' }, e.label || ''), h('span', { class: 'tt faint small' }, new Date(e.t).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }))))))));
  }
  // The feed: what happened across Cully, as cards grouped by day. A card shows what it was, how it went
  // and a preview; opening it shows the full text and the facts behind it.
  const FEED_KINDS = [['', 'Everything'], ['task', 'Tasks'], ['agent', 'Agents'], ['eval', 'Evals'], ['scout', 'Scout'], ['advice', 'Advice']];
  const FEED_ICON = { task: 'term', agent: 'spark', eval: 'check', scout: 'globe', advice: 'alert' };
  const FEED_WORD = { task: 'Task', agent: 'Agent', eval: 'Eval', scout: 'Scout', advice: 'Advice' };
  const FEED_STATUS = { good: ['ok', 'Went well'], bad: ['problem', 'Problem'], wait: ['needs you', 'Needs you'], info: ['info', 'For your information'] };
  const plainPreview = (t) => (t || '').replace(/\[([^\]]+)\]\([^)]*\)/g, '$1').replace(/\*\*|__|`/g, '').replace(/^\s*[-*#]+\s+/gm, '').replace(/\s+/g, ' ').trim();
  const clockTime = (iso) => new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  function feedChip(st) { const [cls, word] = FEED_STATUS[st] || FEED_STATUS.info; return h('span', { class: 'chip fs-' + st, title: word }, cls); }
  function openFeedItem(it) {
    const meta = (it.meta || []).length ? h('dl', { class: 'meta' }, ...it.meta.flatMap((m) => [h('dt', {}, m.label), h('dd', {}, m.value)])) : null;
    const modal = h('div', { class: 'modal wide', role: 'dialog', 'aria-modal': 'true', 'aria-label': it.title },
      h('header', {}, h('h2', {}, it.title), h('button', { class: 'btn ghost small sp', onclick: closeMCP }, 'Close')),
      h('div', { class: 'mbody' },
        h('div', { class: 'row' }, h('span', { class: 'chip' }, icon(FEED_ICON[it.kind] || 'spark'), FEED_WORD[it.kind] || it.kind), feedChip(it.status), h('span', { class: 'faint small' }, new Date(it.at).toLocaleString([], { weekday: 'long', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }))),
        meta,
        it.body ? h('div', { class: 'findings' }, markdown(it.body)) : h('p', { class: 'faint' }, 'Nothing more was recorded for this.'),
        h('div', { class: 'row' }, it.href ? h('a', { class: 'btn', href: it.href, onclick: closeMCP }, 'Open where it lives') : null, it.body ? copyBtn('Copy text', () => it.body) : null)));
    const bg = h('div', { class: 'modal-bg', onclick: (e) => { if (e.target === bg) closeMCP(); } }, modal);
    $('modal-root').replaceChildren(bg);
    S.modal = { agent: 'feed' };
    document.removeEventListener('keydown', escClose);
    document.addEventListener('keydown', escClose);
  }
  function feedCard(it) {
    const folder = it.repo ? it.repo.split('/').filter(Boolean).pop() : '';
    const preview = plainPreview(it.detail || it.body);
    const card = h('article', { class: 'fcard f-' + it.status + ' k-' + it.kind, tabindex: '0', role: 'button', 'aria-label': it.title + '. Open for details.' },
      h('div', { class: 'row fhead' }, h('span', { class: 'fkind' }, icon(FEED_ICON[it.kind] || 'spark'), FEED_WORD[it.kind] || it.kind), feedChip(it.status), h('span', { class: 'sp' }), h('span', { class: 'faint small' }, clockTime(it.at))),
      h('h4', { class: 'ftitle' }, it.title),
      preview ? h('p', { class: 'fprev' }, preview) : null,
      h('div', { class: 'row ffoot' }, folder ? h('span', { class: 'chip', title: it.repo }, icon('folder'), folder) : null, ...(it.meta || []).slice(0, 2).filter((m) => m.label !== 'Folder' && m.value.length < 40).map((m) => h('span', { class: 'chip', title: m.label }, m.value)), h('span', { class: 'sp' }), h('span', { class: 'faint small' }, 'Open ›')));
    card.addEventListener('click', () => openFeedItem(it));
    card.addEventListener('keydown', (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); openFeedItem(it); } });
    return card;
  }
  // What Cully did, by period. The periods sit in a column on the right with their counts, and the
  // day-by-day feed fills the left. Everything comes from one call for the last 30 days.
  const FEED_PERIODS = [['today', 'Today'], ['yesterday', 'Yesterday'], ['7', '7 days'], ['15', '15 days'], ['30', '30 days']];
  function feedWindow(key) {
    const now = new Date(); const mid = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
    if (key === 'today') return [mid, Infinity];
    if (key === 'yesterday') return [mid - 86400000, mid];
    return [Date.now() - Number(key) * 86400000, Infinity];
  }
  async function renderFeed(out, head) {
    const kind = S.feedKind || ''; const period = S.feedPeriod || 'today';
    let all = [];
    try { all = await api('/api/feed?days=30'); } catch (e) { out.replaceChildren(head(), h('div', { class: 'callout' }, e.message)); return; }
    const again = () => renderFeed(out, head);
    const inWin = (key) => { const [from, to] = feedWindow(key); return all.filter((i) => { const t = new Date(i.at).getTime(); return t >= from && t < to; }); };
    const inPeriod = inWin(period);
    const list = kind ? inPeriod.filter((i) => i.kind === kind) : inPeriod;
    const count = (f) => inPeriod.filter(f).length;
    const tasks = inPeriod.filter((i) => i.kind === 'task');
    const kpis = [
      [count((i) => i.kind === 'task' && i.status === 'good'), 'Tasks finished'], [tasks.length, 'Tasks worked on'], [count((i) => i.kind === 'agent'), 'Agent runs'],
      [count((i) => i.kind === 'eval' && i.status === 'good'), 'Evals passed'], [count((i) => i.kind === 'eval' && i.status === 'bad'), 'Evals failed'],
      [count((i) => i.kind === 'scout'), 'Scouts'], [count((i) => i.status === 'wait' || i.status === 'bad'), 'Need a look'],
    ];
    const label = (FEED_PERIODS.find(([k]) => k === period) || FEED_PERIODS[0])[1].toLowerCase();
    const said = [];
    if (tasks.length) said.push(tasks.length + (tasks.length === 1 ? ' task' : ' tasks'));
    const runs = count((i) => i.kind === 'agent'); if (runs) said.push(runs + (runs === 1 ? ' agent run' : ' agent runs'));
    const evs = count((i) => i.kind === 'eval'); if (evs) said.push(evs + (evs === 1 ? ' eval' : ' evals'));
    const sc = count((i) => i.kind === 'scout'); if (sc) said.push(sc + (sc === 1 ? ' scout' : ' scouts'));
    const summary = h('p', { class: 'actsum' }, said.length ? (period === 'today' || period === 'yesterday' ? 'Cully handled ' : 'Over the last ' + label + ', Cully handled ') + said.join(', ') + (period === 'today' ? ' today.' : period === 'yesterday' ? ' yesterday.' : '.') : (period === 'today' ? 'Nothing has happened today yet.' : 'Nothing happened in this period.'));
    const side = h('aside', { class: 'actside', 'aria-label': 'Period' },
      h('div', { class: 'actcard' }, h('div', { class: 'opt-label' }, 'Period'),
        h('div', { class: 'actperiods', role: 'tablist' }, ...FEED_PERIODS.map(([k, l]) => h('button', { type: 'button', role: 'tab', class: 'actp' + (k === period ? ' on' : ''), 'aria-selected': String(k === period), onclick: () => { S.feedPeriod = k; again(); } }, h('span', {}, l), h('span', { class: 'pn' }, String(inWin(k).length)))))),
      h('div', { class: 'actcard' }, h('div', { class: 'opt-label' }, 'In this period'), h('div', { class: 'actkpis' }, ...kpis.map(([n, l]) => h('div', { class: 'actk' }, h('b', {}, String(n)), h('span', {}, l))))),
      h('div', { class: 'actcard' }, h('div', { class: 'opt-label' }, 'Show'), h('div', { class: 'actkinds' }, ...FEED_KINDS.map(([k, l]) => { const n = k ? inPeriod.filter((i) => i.kind === k).length : inPeriod.length; return h('button', { type: 'button', class: 'actp' + (kind === k ? ' on' : ''), onclick: () => { S.feedKind = k; again(); } }, h('span', {}, l), h('span', { class: 'pn' }, String(n))); }))));
    const attention = list.filter((i) => i.status === 'wait' || i.status === 'bad').slice(0, 6);
    const now = new Date(); const dayName = (iso) => { const d = new Date(iso); const mid = (x) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime(); const diff = Math.round((mid(now) - mid(d)) / 86400000); return diff === 0 ? 'Today' : diff === 1 ? 'Yesterday' : d.toLocaleDateString([], { weekday: 'long', month: 'short', day: 'numeric' }); };
    const days = [];
    for (const it of list) { const d = dayName(it.at); if (!days.length || days[days.length - 1].d !== d) days.push({ d, items: [] }); days[days.length - 1].items.push(it); }
    const main = h('div', { class: 'actmain' }, summary,
      attention.length ? h('section', { class: 'fsec' }, h('h3', { class: 'section-h' }, 'Needs a look'), h('div', { class: 'fgrid' }, ...attention.map(feedCard))) : null,
      days.length ? h('div', {}, ...days.map((g) => h('section', { class: 'fsec' }, h('h3', { class: 'section-h' }, g.d, h('span', { class: 'faint small' }, '  ' + g.items.length + (g.items.length === 1 ? ' item' : ' items'))),
        h('div', { class: 'fgrid' }, ...g.items.map(feedCard)))))
        : emptyCard(period === 'today' ? 'Nothing yet today' : 'Nothing in this period', 'Tasks, agent runs, evals, scouts and advice show up here as they happen. Try a longer period.', 'Start a task', '#/'));
    out.replaceChildren(head(refreshBtn(again)), activityTabs('feed'), h('div', { class: 'actwrap' }, main, side));
  }
  const activityTabs = (on) => h('div', { class: 'row filters' }, h('button', { type: 'button', class: 'toggle' + (on === 'feed' ? ' on' : ''), onclick: () => { S.activityTab = 'feed'; drawPanel('activity', true); } }, 'Feed'), h('button', { type: 'button', class: 'toggle' + (on === 'folder' ? ' on' : ''), onclick: () => { S.activityTab = 'folder'; drawPanel('activity', true); } }, 'This folder'));
  async function renderActivity(out, head, filter) {
    if ((S.activityTab || 'feed') === 'feed') { await renderFeed(out, head); return; }
    const repo = encodeURIComponent(S.opts.repo || '');
    const [sum, data] = await Promise.all([api('/api/handoff?repo=' + repo), api('/api/activity?repo=' + repo)]);
    const chips = h('div', { class: 'row filters' }, ...[['all', 'Everything'], ['files', 'Files'], ['commands', 'Commands and checks'], ['problems', 'Problems']].map(([k, l]) => h('button', { type: 'button', class: 'toggle' + (k === filter ? ' on' : ''), onclick: () => { S.activityFilter = k; drawPanel('activity', true); } }, l)));
    const state = sum.fileCount ? [h('p', { class: 'lead' }, sum.fileCount + (sum.fileCount === 1 ? ' file' : ' files') + ' changed', sum.added || sum.removed ? h('span', {}, '  ', h('span', { class: 'plus' }, '+' + sum.added), ' ', h('span', { class: 'minus' }, '−' + sum.removed)) : null, sum.branch ? h('span', { class: 'faint' }, ' on ' + sum.branch) : null), groupBars(sum),
      h('details', { class: 'more' }, h('summary', {}, 'Show files'), h('ul', { class: 'flist' }, ...sum.files.map((f) => h('li', {}, f)), sum.fileCount > sum.files.length ? h('li', { class: 'faint' }, 'and ' + (sum.fileCount - sum.files.length) + ' more') : null))]
      : [h('p', { class: 'faint' }, 'No uncommitted changes in this folder.')];
    out.replaceChildren(head(copyBtn('Copy for another agent', () => api('/api/panel/handoff?repo=' + repo)), refreshBtn(() => drawPanel('activity', true))),
      activityTabs('folder'),
      h('section', { class: 'group' }, h('header', {}, h('span', { class: 'gt' }, 'Where it left off')), ...state,
        data.recorded ? h('p', { class: 'faint' }, checksSentence(data.facts.checks, data.facts.editsSinceCheck)) : null),
      data.recorded
        ? h('section', { class: 'group' }, h('header', {}, h('span', { class: 'gt' }, 'What was done'), h('span', { class: 'faint small' }, data.sessions + (data.sessions === 1 ? ' recorded session' : ' recorded sessions') + ', latest shown')), chips, renderTimeline(data, filter),
          h('p', { class: 'faint small' }, 'Shows what the agent did: files, commands and checks. It never shows prompts or replies.'))
        : emptyCard('No activity recorded yet', 'Activity appears here after an agent runs in the Cully terminal or from this page.', 'Start a task', '#/'));
  }

  // ----- the drawing switch -----
  async function drawPanel(name, force, arg) {
    const out = $('panel');
    const [title, sub] = DASH_TITLES[name] || [name, ''];
    const head = (...actions) => toolbar(title, sub, ...actions);
    const again = () => drawPanel(name, true);
    const live = () => S.route.name === 'dashboard' && S.route.panel === name;
    try {
      if (name === 'history') { const body = await window.CullyLedger.page(S.route.kind, () => drawPanel('history', true)); if (live()) out.replaceChildren(head(), body); return; }
      if (name === 'advisor' || name === 'scout') {
        const data = await api(name === 'advisor' ? '/api/advice' : '/api/scout');
        const runs = name === 'scout' ? await api('/api/scout/runs').catch(() => []) : null;
        const sig = JSON.stringify(data) + JSON.stringify(runs) + (S.advisorTab || '');
        if (!force && sig === S.dashSig) return;
        S.dashSig = sig;
        if (!live()) return;
        if (name === 'advisor') {
          const tab = S.advisorTab || 'agents';
          const tabs = h('div', { class: 'seg', role: 'group', 'aria-label': 'Source' }, ...[['agents', 'For your tasks'], ['terminal', 'From terminal sessions']].map(([k, l]) => h('button', { type: 'button', class: k === tab ? 'on' : '', onclick: () => { S.advisorTab = k; drawPanel('advisor', true); } }, l)));
          let body;
          if (tab === 'terminal') { const text = await api('/api/panel/suggestions?repo=' + encodeURIComponent(S.opts.repo || '')); body = renderText('suggestions', text); } else body = renderAdvisor(data);
          if (!live()) return;
          out.replaceChildren(head(tabs, refreshBtn(again)), tab === 'agents' ? await adviceBoard(again) : null, tab === 'agents' && !data.length ? null : body);
        } else out.replaceChildren(head(refreshBtn(again)), await scoutAuto(again), renderScout(data, runs), await scoutHistory());
        return;
      }
      out.replaceChildren(head(), loadingCard());
      if (name === 'overview') { await renderOverview(out, head); return; }
      if (name === 'evals') { if (live()) await renderEvals(out, head); return; }
      if (name === 'health') { if (live()) await renderHealth(out, head); return; }
      if (name === 'library') { if (live()) await renderLibrary(out, head); return; }
      if (name === 'permissions') { if (live()) await renderPermissions(out, head); return; }
      if (name === 'savings') { const d = arg || S.savingsDays || 14; S.savingsDays = d; if (live()) await renderSavings(out, head, d); return; }
      if (name === 'activity') { await renderActivity(out, head, S.activityFilter || 'all'); return; }
      if (name === 'jobs') { await renderJobs(out, head, again, live); return; }
      const text = await api('/api/panel/' + name + '?repo=' + encodeURIComponent(S.opts.repo || ''));
      if (live()) out.replaceChildren(head(refreshBtn(again), copyBtn('Copy', () => text)), renderText(name, text));
    } catch (e) {
      if (live()) out.replaceChildren(head(), h('div', { class: 'emptycard' }, h('h3', {}, 'Could not load this'), h('p', {}, e.message), h('button', { class: 'btn small', onclick: again }, icon('refresh'), 'Try again')));
    }
  }
  function mountDashboard(name) {
    $('panels').replaceChildren(...DASH.flatMap(([group, items]) => [
      group ? h('div', { class: 'navgroup' }, group) : null,
      ...items.map(([k, t, d]) => h('a', { class: k === name ? 'on' : '', href: '#/dashboard/' + k }, h('b', {}, t), h('span', {}, d))),
    ]).filter(Boolean));
    S.dashSig = '';
    drawPanel(name, true);
  }

  // ---------- MCP sign-in ----------
  const AGENT_NAMES = { claude: 'Claude', codex: 'Codex', cursor: 'Cursor' };
  const needsAuthStatus = (st) => { const l = String(st || '').toLowerCase(); return l.includes('auth') && /need|requir|unauth/.test(l); };
  // Agents you actually launch Cully with. Others are tucked away.
  const mcpUsed = () => (S.mcp || []).filter((m) => m.inUse);
  const mcpNeeding = () => mcpUsed().filter((m) => m.state === 'needs-auth');
  // Local disk mode: no Cully MCP is registered for the agents in use (or the
  // user pinned it), so Cully keeps history in files on this machine.
  function memoryMode() {
    const o = S.info && S.info.memoryOverride;
    if (o === 'local' || o === 'mcp') return o;
    const used = mcpUsed();
    return used.length && used.every((m) => m.state === 'not-configured' || m.state === 'not-installed') ? 'local' : 'mcp';
  }
  S.mcp = []; S.login = {}; S.modal = null; S.paste = ''; S.pasteErr = '';

  async function refreshMCP() {
    try { S.mcp = await api('/api/mcp'); } catch (_) { return; }
    drawAuth();
    if (S.route.name === 'home' && S.homeGate !== !claudeInUse()) mountHome();
    drawMCPChip();
    drawMCPBanner();
    if (mcpModalOpen()) renderModal();
  }
  function drawMCPChip() {
    const chip = $('mcpchip');
    if (!S.mcp.length) { chip.hidden = true; return; }
    const local = memoryMode() === 'local';
    const need = local ? [] : mcpNeeding();
    const ok = mcpUsed().some((m) => m.state === 'connected');
    chip.hidden = false;
    chip.className = 'mcpchip ' + (need.length ? 'warn' : ok || local ? 'ok' : '');
    chip.title = local ? 'Cully keeps your session history in files on this machine.' : '';
    chip.replaceChildren(icon(local ? 'check' : 'lock'), h('span', { class: 'lbl' }, local ? 'Local mode (SQLite)' : need.length ? 'Cully MCP needs sign-in' : ok ? 'Cully MCP connected' : 'Cully MCP not connected'));
    chip.onclick = () => openMCP((need[0] || mcpUsed()[0] || S.mcp.find((m) => m.state !== 'not-installed') || S.mcp[0]).agent);
  }
  function drawMCPBanner() {
    const box = $('mcpbanner');
    if (!box) return;
    const need = memoryMode() === 'local' ? [] : mcpNeeding();
    if (!need.length) { box.replaceChildren(); return; }
    box.replaceChildren(h('div', { class: 'mcpbanner' }, icon('alert'),
      h('span', {}, h('b', {}, 'Cully MCP needs sign-in'), ' for ' + need.map((m) => AGENT_NAMES[m.agent]).join(', ') + '. Until then agents run without Cully memory or recall.'),
      h('button', { class: 'btn small sp', onclick: () => openMCP(need[0].agent) }, icon('lock'), 'Authenticate MCP')));
  }

  function openMCP(agent) {
    S.modal = { agent }; S.paste = ''; S.pasteErr = '';
    renderModal(true);
    pollLogin();
  }
  function closeMCP() {
    S.modal = null;
    $('modal-root').replaceChildren();
    document.removeEventListener('keydown', escClose);
  }
  const escClose = (e) => { if (e.key === 'Escape') closeMCP(); };
  let loginTimer = null;
  function pollLogin() {
    clearInterval(loginTimer);
    loginTimer = setInterval(async () => {
      if (!mcpModalOpen()) { clearInterval(loginTimer); return; }
      const agent = S.modal.agent;
      const cur = S.login[agent];
      if (!cur || !['starting', 'awaiting'].includes(cur.state)) return;
      try {
        const v = await api('/api/mcp/' + agent + '/login');
        const was = cur.state;
        S.login[agent] = v;
        if (was !== v.state) {
          if (v.state === 'done') { toast(AGENT_NAMES[agent] + ' signed in to Cully MCP'); refreshMCP(); }
          renderModal();
        }
      } catch (_) { /* banner */ }
    }, 1500);
  }

  let modalSig = '';
  function renderLocalModal() {
    const used = mcpUsed().map((m) => m.agent);
    const modal = h('div', { class: 'modal', role: 'dialog', 'aria-modal': 'true', 'aria-label': 'Local mode' },
      h('header', {}, h('h2', {}, 'Local mode'), h('button', { class: 'btn ghost small sp', onclick: closeMCP }, 'Close')),
      h('div', { class: 'mbody' },
        h('p', {}, 'Cully keeps your session history, agents, evals and advice in one small SQLite database on this machine (cully.db). Nothing is sent to a server, and nothing needs Postgres or Mem0 installed.'),
        h('div', { class: 'callout' }, h('b', {}, 'Local mode is less efficient. '), 'It recalls past work by keyword only, so agents may be given more context than needed and use more tokens. The Cully MCP (Postgres and Mem0) finds the few relevant records by meaning, keeps memory across machines and shares it with a team.'),
        h('p', {}, 'To set it up, connect the Cully MCP:'),
        h('div', { class: 'callout' }, h('code', { class: 'k' }, 'cully mcp add --agent ' + (used[0] || 'claude') + ' --url <your Cully MCP URL> --oauth'), ', then sign in from here.')));
    const bg = h('div', { class: 'modal-bg', onclick: (e) => { if (e.target === bg) closeMCP(); } }, modal);
    $('modal-root').replaceChildren(bg);
    document.removeEventListener('keydown', escClose);
    document.addEventListener('keydown', escClose);
  }
  // Other dialogs (folder chooser, resume, confirm...) reuse S.modal with a label in place of an
  // agent. Only a real agent name means the sign-in dialog is open.
  const mcpModalOpen = () => !!S.modal && Object.prototype.hasOwnProperty.call(AGENT_NAMES, S.modal.agent);
  function renderModal(force) {
    if (!mcpModalOpen()) return;
    if (memoryMode() === 'local') { renderLocalModal(); return; }
    const agent = S.modal.agent;
    const visible = S.mcp;
    const st = S.mcp.find((m) => m.agent === agent) || { agent, state: 'unknown', canPaste: false };
    const login = S.login[agent] || st.login || { state: 'idle' };
    const sig = [agent, st.state, login.state, login.url, login.message, S.pasteErr, S.mcp.map((m) => m.state).join()].join('|');
    if (!force && sig === modalSig) return;
    modalSig = sig;
    const root = $('modal-root');
    const focusedPaste = document.activeElement && document.activeElement.id === 'paste-in';

    const tabs = visible.length > 1 ? h('div', { class: 'mtabs', role: 'tablist' }, ...visible.map((m) =>
      h('button', { class: m.agent === agent ? 'on' : '', role: 'tab', onclick: () => { S.modal.agent = m.agent; S.pasteErr = ''; renderModal(true); } },
        m.agent, h('span', { class: 'pill ' + (m.state === 'needs-auth' ? 'waiting' : m.state === 'connected' ? 'working' : '') }, m.state.replace('-', ' '))))) : null;

    const body = h('div', { class: 'mbody' });
    const recheck = h('button', { class: 'btn ghost small', onclick: async () => { await api('/api/mcp').then((x) => { S.mcp = x; }).catch(() => {}); drawMCPChip(); drawMCPBanner(); renderModal(true); } }, 'Re-check');
    const name = AGENT_NAMES[agent];

    if (st.state === 'connected') {
      body.append(h('p', {}, name + ' is connected to the Cully MCP. Nothing to do.'), h('div', { class: 'row' }, recheck));
    } else if (st.state === 'not-installed') {
      body.append(h('p', {}, name + "'s command-line tool was not found on the machine running Cully, so it cannot be signed in here."));
    } else if (st.state === 'not-configured') {
      body.append(h('p', {}, 'No Cully MCP server is configured for ' + name + '.'),
        h('div', { class: 'callout' }, 'Add it with ', h('code', { class: 'k' }, 'cully mcp add --agent ' + agent + ' --url <your Cully MCP URL> --oauth'), ', then come back here to sign in.'), h('div', { class: 'row' }, recheck));
    } else if (st.state === 'unhealthy') {
      body.append(h('p', {}, name + ' could not reach the Cully MCP server. That is a connection problem, not a sign-in problem.'),
        h('div', { class: 'callout' }, 'Check the server URL and that the server is up, then re-check. If it keeps failing, run ', h('code', { class: 'k' }, agent === 'claude' ? 'claude mcp list' : agent === 'codex' ? 'codex mcp list' : 'cursor-agent mcp list'), ' in a terminal to see the error.'), h('div', { class: 'row' }, recheck));
    } else {
      drawSignIn(body, agent, name, st, login);
    }
    const modal = h('div', { class: 'modal', role: 'dialog', 'aria-modal': 'true', 'aria-label': 'Authenticate MCP' },
      h('header', {}, h('h2', {}, 'Authenticate Cully MCP'), h('button', { class: 'btn ghost small sp', onclick: closeMCP }, 'Close')), tabs, body);
    const bg = h('div', { class: 'modal-bg', onclick: (e) => { if (e.target === bg) closeMCP(); } }, modal);
    root.replaceChildren(bg);
    document.removeEventListener('keydown', escClose);
    document.addEventListener('keydown', escClose);
    const input = $('paste-in');
    if (input && focusedPaste) input.focus();
  }

  function drawSignIn(body, agent, name, st, login) {
    const active = login.state === 'starting' || login.state === 'awaiting';
    if (login.state === 'done') {
      body.append(h('p', {}, h('b', {}, 'Signed in.'), ' Checking the connection…'), h('div', { class: 'row' }, h('button', { class: 'btn ghost small', onclick: refreshMCP }, 'Re-check')));
      return;
    }
    const steps = h('ol', { class: 'steps' });
    const start = h('button', { class: 'btn', onclick: async () => {
      S.pasteErr = '';
      try { S.login[agent] = await api('/api/mcp/' + agent + '/login', { method: 'POST' }); pollLogin(); } catch (e) { toast(e.message, true); }
      renderModal(true);
    } }, active ? 'Restart' : login.state === 'failed' || login.state === 'cancelled' ? 'Try again' : 'Start sign-in');
    if (active) start.onclick = async () => { await api('/api/mcp/' + agent + '/login/cancel', { method: 'POST' }).catch(() => {}); S.login[agent] = { state: 'idle' }; renderModal(true); };
    if (active) start.textContent = 'Cancel';
    body.append(h('p', {}, name + ' needs you to sign in to the Cully MCP before it can use Cully memory and tasks.'));
    if (login.state === 'failed') body.append(h('div', { class: 'callout' }, h('b', {}, 'Sign-in failed. '), login.message || 'The login command stopped with an error.'));
    if (login.state === 'cancelled') body.append(h('div', { class: 'callout' }, 'Sign-in cancelled.'));
    steps.append(h('li', {}, h('div', { class: 'row' }, 'Start the sign-in on the machine running Cully. ', start)));
    if (active && login.state === 'starting') steps.append(h('li', {}, 'Waiting for ' + name + ' to print the sign-in link…'));
    if (login.url) {
      steps.append(h('li', {}, 'Open this link, sign in, and approve access:',
        h('div', { class: 'linkbox' }, h('a', { href: login.url, target: '_blank', rel: 'noopener noreferrer' }, login.url), h('button', { class: 'btn ghost small', onclick: () => copy(login.url, 'link') }, icon('copy'), 'Copy'))));
      if (st.canPaste) {
        const input = h('input', { type: 'text', id: 'paste-in', placeholder: 'http://localhost:…/callback?code=…', autocomplete: 'off', spellcheck: 'false', 'aria-label': 'Redirect address' });
        input.value = S.paste;
        input.addEventListener('input', () => { S.paste = input.value; });
        const submit = async () => {
          try { await api('/api/mcp/' + agent + '/login/paste', { method: 'POST', body: JSON.stringify({ url: S.paste }) }); S.pasteErr = ''; S.paste = ''; toast('Address sent to ' + name); } catch (e) { S.pasteErr = e.message; }
          renderModal(true);
        };
        input.addEventListener('keydown', (e) => { if (e.key === 'Enter') submit(); });
        steps.append(h('li', {}, h('b', {}, 'Where is your browser?'),
          h('div', { class: 'callout' }, h('b', {}, 'On the same machine as Cully'), ' (or you forwarded the port): after you approve, the page finishes by itself and this window turns green.'),
          h('div', { class: 'callout' }, h('b', {}, "On your laptop while Cully runs on a VM:"), " after you approve, the browser lands on a page that can't load (its address starts with ", h('code', { class: 'k' }, 'http://localhost'), '). That is expected. Copy the ', h('b', {}, 'full address'), ' from the address bar and paste it here:'),
          h('div', { class: 'row' }, input, h('button', { class: 'btn small', onclick: submit }, 'Finish sign-in')),
          S.pasteErr ? h('div', { class: 'err' }, S.pasteErr) : null,
          agent === 'codex' ? h('div', { class: 'hint' }, 'Codex can also pin the callback port with mcp_oauth_callback_port in ~/.codex/config.toml, so one SSH port forward works every time.') : null));
      } else {
        steps.append(h('li', {}, h('b', {}, "Cursor can't take a pasted address."),
          h('div', { class: 'callout' }, "Its login only finishes where its own browser callback lands. If Cursor runs on this machine and you browse from here, approve in the link above. If Cully runs on a VM, run ", h('code', { class: 'k' }, 'cursor-agent mcp login cully'),
            ' in a terminal on the machine with your browser, or forward the callback port with ', h('code', { class: 'k' }, 'ssh -L PORT:localhost:PORT'), '. Cursor-side OAuth for remote servers is also reported unreliable by Cursor users.')));
      }
    }
    body.append(steps);
  }

  // ---------- folders and sessions ----------
  function folderSelect(onChange) {
    const list = (S.info && S.info.repoInfo) || [];
    if (list.length < 2) return null;
    const sel = h('select', { class: 'folder-select', 'aria-label': 'Folder', title: 'Folder shown' }, ...list.map((r) => h('option', { value: r.path, ...(r.path === S.opts.repo ? { selected: true } : {}) }, r.name)));
    sel.addEventListener('change', () => { S.opts.repo = sel.value; onChange(); });
    return sel;
  }
  async function addFolder(path, init) {
    const info = await api('/api/repos', { method: 'POST', body: JSON.stringify({ path, init: !!init }) });
    S.info = await api('/api/info');
    S.opts.repo = info.path;
    drawFolderChip();
    return info;
  }
  // Choose any folder on this machine and start there.
  function recentFolders(limit) {
    const seen = new Map();
    for (const r of (S.info && S.info.repoInfo) || []) seen.set(r.path, { path: r.path, name: r.name, git: r.git !== false });
    for (const x of S.sessions || []) { const p = x.repo || x.cwd; if (p && !seen.has(p)) seen.set(p, { path: p, name: x.name, git: !!x.repo }); }
    return [...seen.values()].slice(0, limit || 6);
  }
  async function startIn(path, init) {
    try { const info = await addFolder(path, init); closeMCP(); location.hash = '#/'; if (S.route.name === 'home') mountHome(); toast('Ready to start a task in ' + info.name); } catch (e) { toast(e.message, true); }
  }
  // Any folder works. With git a task gets its own branch and worktree; without it the task works in
  // the folder directly. Cully works out which when the task starts.
  function openFolderChooser(start) {
    const state = { path: start || '~', showDot: false, data: null, err: '' };
    const body = h('div', { class: 'mbody chooser' });
    const modal = h('div', { class: 'modal wide', role: 'dialog', 'aria-modal': 'true', 'aria-label': 'Choose a folder' },
      h('header', {}, h('h2', {}, 'Choose a folder'), h('button', { class: 'btn ghost small sp', onclick: closeMCP }, 'Close')), body);
    const bg = h('div', { class: 'modal-bg', onclick: (e) => { if (e.target === bg) closeMCP(); } }, modal);
    $('modal-root').replaceChildren(bg);
    S.modal = { agent: 'folder' };
    document.removeEventListener('keydown', escClose);
    document.addEventListener('keydown', escClose);
    async function load(path) {
      try { state.data = await api('/api/fs?path=' + encodeURIComponent(path) + (state.showDot ? '&hidden=1' : '')); state.path = state.data.path; state.err = ''; } catch (e) { state.err = e.message; }
      draw();
    }
    function draw() {
      const d = state.data;
      const recent = recentFolders(6);
      const segs = d ? d.path.split('/').filter(Boolean) : [];
      const crumbs = h('nav', { class: 'crumbs', 'aria-label': 'Path' }, h('button', { class: 'linkbtn', onclick: () => load('/') }, '/'),
        ...segs.map((seg, i) => h('button', { class: 'linkbtn', onclick: () => load('/' + segs.slice(0, i + 1).join('/')) }, seg)));
      const pathIn = h('input', { type: 'text', placeholder: 'Or type a path, such as ~/work or /srv/app', 'aria-label': 'Folder path', autocomplete: 'off', spellcheck: 'false' });
      pathIn.addEventListener('keydown', (e) => { if (e.key === 'Enter' && pathIn.value.trim()) { const v = pathIn.value.trim(); load(v.startsWith('/') || v.startsWith('~') || !state.path ? v : state.path.replace(/\/$/, '') + '/' + v); } });
      body.replaceChildren(
        recent.length ? h('div', { class: 'row' }, h('span', { class: 'opt-label' }, 'Recent'), ...recent.map((r) => h('button', { class: 'chip', title: r.path, onclick: () => (r.git ? startIn(r.path) : load(r.path)) }, icon('folder'), r.name))) : null,
        h('div', { class: 'row crumbrow' }, crumbs, h('span', { class: 'sp row' },
          d && d.parent ? h('button', { class: 'btn ghost small', onclick: () => load(d.parent) }, 'Up') : null,
          h('button', { class: 'btn ghost small', onclick: () => { load(d.home || '~'); } }, 'Home'),
          h('label', { class: 'check' }, h('input', Object.assign({ type: 'checkbox', onchange: (e) => { state.showDot = e.target.checked; load(state.path); } }, state.showDot ? { checked: true } : {})), 'Show hidden folders'))),
        state.err ? h('div', { class: 'err' }, state.err) : null,
        d ? h('ul', { class: 'frows' }, ...(d.entries.length ? d.entries.map((e) => h('li', {},
          h('button', { class: 'frow', onclick: () => load(e.path) }, icon('folder'), h('span', { class: 'fn' }, e.name), e.isGit ? h('span', { class: 'chip mono' }, icon('branch'), 'git repository') : null, e.writable ? null : h('span', { class: 'chip' }, icon('lock'), 'read only')),
          e.writable ? h('button', { class: 'btn small', onclick: () => startIn(e.path) }, 'Start here') : null)) : [h('li', { class: 'faint' }, 'No folders here.')])) : h('p', { class: 'faint' }, 'Loading…'),
        d && d.truncated ? h('p', { class: 'faint small' }, 'Showing the first folders only. Paste a path to go further.') : null,
        h('div', { class: 'row foot' }, pathIn,
          d && d.writable ? h('button', { class: 'btn', onclick: () => startIn(d.path) }, icon('plus'), 'Use this folder')
            : h('span', { class: 'faint small' }, d ? 'You do not have write access to this folder, so Cully cannot work here.' : '')));
    }
    draw();
    load(state.path);
  }
  function openAddFolder() { openFolderChooser(); }

  const AGENT_LABEL = { claude: 'Claude', codex: 'Codex', cursor: 'Cursor' };
  // Which harness (Claude Code, Codex, Cursor) a session belongs to. Only tasks started here are tied to one.
  const harnessChip = (agent) => (agent ? h('span', { class: 'chip harness h-' + agent, title: 'The harness this session ran in' }, AGENT_LABEL[agent] || agent) : null);
  async function startHere(sess) {
    try { await addFolder(sess.repo || sess.cwd); location.hash = '#/'; toast('Ready to start a task in ' + sess.name); } catch (e) { toast(e.message, true); }
  }
  // ---- session search: several words at once, filters, highlights, a count, and keys (/ to focus, Esc to clear) ----
  const OUTCOMES = [['', 'Any outcome'], ['yes', 'Finished'], ['partly', 'Partly'], ['no', 'Not finished'], ['none', 'Not summarised']];
  const norm = (t) => String(t || '').toLowerCase();
  function searchTerms(q) { return (q.match(/"[^"]+"|\S+/g) || []).map((w) => norm(w.replace(/^"|"$/g, ''))).filter(Boolean); }
  // The text a session can be found by, split into fields so a match in the title ranks above one in a step.
  function sessionFields(x) {
    const lib = x.library || {}; const steps = lib.steps || [];
    return { title: norm([x.title, x.name].join(' ')), what: norm([x.description, lib.task].join(' ')), steps: norm(steps.map((t) => t.title + ' ' + t.story).join(' ') + ' ' + (lib.drift || '')),
      where: norm([x.cwd, x.branch, x.agent, AGENT_LABEL[x.agent], x.origin].join(' ')), id: norm(x.id) };
  }
  // 0 = no match. Every word has to match somewhere; the best field a word is found in sets the score.
  function sessionScore(x, terms) {
    if (!terms.length) return 1;
    const f = sessionFields(x); let total = 0;
    for (const t of terms) {
      const w = f.title.includes(t) ? 8 : f.what.includes(t) ? 5 : f.steps.includes(t) ? 3 : f.where.includes(t) ? 2 : f.id.startsWith(t) || (t.length >= 6 && f.id.includes(t)) ? 4 : 0;
      if (!w) return 0;
      total += w;
    }
    return total;
  }
  function sessionOutcomeOf(x) { const c = (x.library && x.library.completed) || x.completed; return c === 'yes' || c === 'partly' || c === 'no' ? c : 'none'; }
  // Wraps each matched word in <mark>, building nodes so session text is never parsed as HTML.
  function highlight(text, terms) {
    text = String(text || '');
    if (!terms.length) return text;
    const re = new RegExp('(' + terms.map((t) => t.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('|') + ')', 'ig');
    return text.split(re).map((part, i) => (i % 2 ? h('mark', {}, part) : part));
  }
  // When the words matched only inside a step or the goal, show that line so the result explains itself.
  function matchSnippet(x, terms) {
    if (!terms.length) return null;
    const lib = x.library || {};
    const cand = [['Goal', lib.task]].concat((lib.steps || []).map((t) => ['Step: ' + t.title, t.story]), [['Drift', lib.drift]]);
    const visible = norm([x.title, x.name, x.description].join(' '));
    if (terms.every((t) => visible.includes(t))) return null;
    for (const [label, body] of cand) { if (body && terms.some((t) => norm(label + ' ' + body).includes(t))) return h('p', { class: 'hsnip faint small' }, h('b', {}, label + ': '), highlight(uiClipText ? uiClipText(body, 180) : body, terms)); }
    return null;
  }
  function sessionSearchBar() {
    const input = h('input', { type: 'search', class: 'session-search', id: 'session-search', placeholder: 'Search title, summary, goal, steps, folder, branch or ID', 'aria-label': 'Search sessions', autocomplete: 'off', spellcheck: 'false' });
    input.value = S.sessionQuery || '';
    const clear = h('button', { type: 'button', class: 'ssclear', 'aria-label': 'Clear the search', title: 'Clear (Esc)', hidden: !input.value, onclick: () => { input.value = ''; S.sessionQuery = ''; clear.hidden = true; drawHistory(); input.focus(); } }, '×');
    let timer = 0;
    input.addEventListener('input', () => { clear.hidden = !input.value; clearTimeout(timer); timer = setTimeout(() => { S.sessionQuery = input.value; S.histShown = {}; drawHistory(); }, 120); });
    input.addEventListener('keydown', (e) => { if (e.key === 'Escape' && input.value) { e.preventDefault(); clear.click(); } });
    S.searchFocusKey = S.searchFocusKey || document.addEventListener('keydown', (e) => {
      const el = document.getElementById('session-search'); if (!el || e.key !== '/' || e.ctrlKey || e.metaKey || e.altKey) return;
      const t = document.activeElement; if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)) return;
      e.preventDefault(); el.focus(); el.select();
    }) || true;
    S.searchCount = h('span', { class: 'sscount faint small', 'aria-live': 'polite' });
    S.searchFilters = h('div', { class: 'ssfilters' });
    return h('div', { class: 'ssbar' }, h('div', { class: 'ssrow' }, h('span', { class: 'ssicon', 'aria-hidden': 'true' }, icon('search') || ''), input, clear, S.searchCount), S.searchFilters);
  }
  function drawSearchFilters(sessions) {
    if (!S.searchFilters) return;
    const harnessCount = (a) => sessions.filter((x) => !a || x.agent === a).length;
    const hs = [['', 'All']].concat(['claude', 'codex', 'cursor'].filter((a) => harnessCount(a)).map((a) => [a, AGENT_LABEL[a]]));
    const hbtn = ([v, label]) => h('button', { type: 'button', class: 'ssf' + ((S.sessionHarness || '') === v ? ' on' : ''), 'aria-pressed': String((S.sessionHarness || '') === v), onclick: () => { S.sessionHarness = v; S.histShown = {}; drawHistory(); } }, label, h('span', { class: 'ssn' }, String(harnessCount(v))));
    const sel = h('select', { class: 'jsel ssout', 'aria-label': 'Outcome' }, ...OUTCOMES.map(([v, l]) => h('option', { value: v }, l + (v ? ' (' + sessions.filter((x) => sessionOutcomeOf(x) === v).length + ')' : ''))));
    sel.value = S.sessionOutcome || '';
    sel.addEventListener('change', () => { S.sessionOutcome = sel.value; S.histShown = {}; drawHistory(); });
    S.searchFilters.replaceChildren(h('div', { class: 'ssgroup', role: 'group', 'aria-label': 'Harness' }, ...hs.map(hbtn)), sel);
  }
  function renderSessions() {
    const box = $('sessions');
    const all = S.sessions || [];
    const list = all.filter((x) => S.showClosedSessions || !x.detached);
    const hiddenClosed = all.filter((x) => x.detached).length;
    const sig = JSON.stringify([list.map((x) => [x.pid, x.cwd, x.branch, x.via, x.detached, x.sessionId]), hiddenClosed, !!S.showClosedSessions]);
    if (sig === S.sessSig && box.childElementCount) return;
    S.sessSig = sig;
    const sub = 'Every session on this computer, from every harness.';
    const kids = [toolbar('Sessions', sub, refreshBtn(async () => { S.sessions = await api('/api/sessions'); S.sessSig = ''; S.hist = null; renderSessions(); }))];
    kids.push(h('div', { id: 'active' }));
    const toggles = [];
    if (hiddenClosed || S.showClosedSessions) toggles.push(h('button', { class: 'linkbtn', onclick: () => { S.showClosedSessions = !S.showClosedSessions; S.sessSig = ''; renderSessions(); } }, S.showClosedSessions ? 'Hide sessions whose terminal was closed' : 'Show ' + hiddenClosed + (hiddenClosed === 1 ? ' session' : ' sessions') + ' whose terminal was closed'));
    if (toggles.length) kids.push(h('div', { class: 'row' }, ...toggles));
    const mine = S.agents.length;
    if (mine) kids.push(h('p', { class: 'faint small' }, mine + (mine === 1 ? ' agent was' : ' agents were') + ' started from this page. ', h('a', { class: 'ext', href: '#/' }, 'See them on the Tasks tab')));
    kids.push(sessionSearchBar(), h('div', { id: 'history' }));
    box.replaceChildren(...kids);
    drawActive();
    drawHistory();
    if (!S.hist) loadHistory(0);
  }

  // Shown when the browser will not let the page copy: the command is selectable here.
  function showCommand(cmd) {
    const inp = h('textarea', { class: 'cmdbox', readonly: 'true', 'aria-label': 'Command', rows: '3' });
    inp.value = cmd;
    const modal = h('div', { class: 'modal', role: 'dialog', 'aria-modal': 'true', 'aria-label': 'Copy this command' },
      h('header', {}, h('h2', {}, 'Copy this command'), h('button', { class: 'btn ghost small sp', onclick: closeMCP }, 'Close')),
      h('div', { class: 'mbody' }, h('p', { class: 'faint' }, 'This browser does not let the page use the clipboard on an http address. Select the command and copy it, then paste it in a terminal.'), inp));
    const bg = h('div', { class: 'modal-bg', onclick: (e) => { if (e.target === bg) closeMCP(); } }, modal);
    $('modal-root').replaceChildren(bg);
    S.modal = { agent: 'command' };
    document.removeEventListener('keydown', escClose);
    document.addEventListener('keydown', escClose);
    inp.focus(); inp.select();
  }
  const TASK_STATUS = { done: ['Done', 'ok'], partly: ['Partly done', ''], open: ['Not finished', 'warn'] };
  // One card per job the session covered: its title, how it ended and what happened.
  function taskStories(tasks, empty) {
    if (!tasks || !tasks.length) return h('p', { class: 'faint small' }, empty);
    return h('ol', { class: 'taskstories' }, ...tasks.map((t) => { const st = TASK_STATUS[t.status] || TASK_STATUS.open;
      return h('li', {}, h('div', { class: 'row' }, h('b', {}, t.title), h('span', { class: 'chip ' + st[1] }, st[0])), h('p', { class: 'hdesc' }, t.story)); }));
  }
  // A task can span several sessions, across harnesses. This lists each one with its own story.
  async function taskSessionsSection(a) {
    let rec = null;
    try { rec = (await api('/api/tasks?range=all')).find((t) => t.id === a.id); } catch (_) {}
    const sess = (rec && rec.sessions) || [];
    if (!sess.length) return null;
    const lib = await sessionIndex();
    const harnesses = new Set(sess.map((x) => x.agent).filter(Boolean));
    const cards = sess.map((x, i) => { const k = lib.get(x.id); const named = k && k.source === 'ai';
      return h('li', {}, h('div', { class: 'row' }, h('span', { class: 'faint small' }, (i + 1) + '.'), harnessChip(x.agent), h('b', {}, (k && (k.title || k.name)) || 'Session ' + x.id.slice(0, 8)),
          k && k.completed ? h('span', { class: 'chip ' + (k.completed === 'yes' ? 'ok' : k.completed === 'no' ? 'warn' : '') }, DONE_LABEL[k.completed] || k.completed) : null,
          x.reason ? h('span', { class: 'chip' }, 'Switched: ' + x.reason) : null),
        h('p', { class: 'hdesc' }, named && k.description ? k.description : 'Not summarised yet. Open the session and use Summarize.'),
        k && k.steps && k.steps.length ? h('details', {}, h('summary', {}, k.steps.length + (k.steps.length === 1 ? ' step' : ' steps')), taskStories(k.steps)) : null,
        h('a', { class: 'btn ghost small', href: '#/sessions/' + x.id }, 'Open session')); });
    return h('div', { class: 'tsessions' }, h('h3', { class: 'section-h' }, 'Sessions in this task (' + sess.length + (harnesses.size > 1 ? ', ' + harnesses.size + ' harnesses' : '') + ')'), h('ol', { class: 'taskstories' }, ...cards));
  }
  // What this session did, shown before it is resumed.
  function sessionSummary(x) {
    const lib = x.library; const kids = [];
    if (lib && lib.task) kids.push(h('p', {}, h('b', {}, 'Goal: '), lib.task));
    if (x.description) kids.push(h('p', {}, h('b', {}, 'What it did: '), x.description));
    if (lib && lib.completed) kids.push(h('p', {}, h('b', {}, 'Where it ended: '), h('span', { class: 'chip ' + (lib.completed === 'yes' ? 'ok' : lib.completed === 'no' ? 'warn' : '') }, DONE_LABEL[lib.completed] || lib.completed), lib.onTask === 'drifted' ? h('span', { class: 'chip warn' }, 'Drifted') : null));
    if (lib && lib.steps && lib.steps.length) kids.push(h('div', {}, h('b', {}, 'Steps (' + lib.steps.length + ')'), taskStories(lib.steps)));
    if (lib && lib.drift) kids.push(h('p', { class: 'hdiff' }, h('b', {}, 'Drift: '), lib.drift));
    if (x.different && x.different !== 'Nothing notable.') kids.push(h('p', { class: 'hdiff' }, h('b', {}, 'Could go better: '), x.different));
    if (lib) kids.push(h('div', { class: 'row' }, x.activeSec ? h('span', { class: 'chip' }, dur(x.activeSec) + ' worked') : null, h('span', { class: 'chip' }, lib.turns + (lib.turns === 1 ? ' request' : ' requests')), h('span', { class: 'chip' }, lib.tools + ' tool calls'),
      lib.files ? h('span', { class: 'chip' }, lib.files + ' files changed') : null, lib.checks ? h('span', { class: 'chip' }, lib.checks + ' test runs') : null, lib.errors ? h('span', { class: 'chip warn' }, lib.errors + ' errors') : null));
    if (!lib || !lib.named) {
      const btn = h('button', { class: 'btn ghost small' }, icon('spark'), 'Summarize this session');
      btn.addEventListener('click', async () => {
        btn.disabled = true; btn.textContent = 'Reading the session…';
        try { await api('/api/sessions/' + x.id + '/summarize', { method: 'POST' }); await loadHistory(S.hist ? S.hist.days : 7); const fresh = ((S.hist && S.hist.sessions) || []).find((r) => r.id === x.id); continueSession(fresh || x, fork); }
        catch (e) { toast(e.message, true); btn.disabled = false; btn.textContent = 'Summarize this session'; }
      });
      kids.push(h('div', { class: 'row' }, btn, h('span', { class: 'faint small' }, 'Asks your Claude to read it. Takes a moment.')));
    }
    return h('div', { class: 'sumbox' }, ...kids);
  }
  // Continue any session, from any harness, as a new task. The task runs in the harness this page was
  // opened with; the session's handoff (goal, what it did, where it ended, what went wrong, and where its
  // full record is) is its first message, so work started in Cursor or Codex carries on in Claude.
  async function continueAsTask(x) {
    const ta = h('textarea', { 'aria-label': 'What to do next', rows: '3', placeholder: 'What should it do next? Leave empty to finish what is left.' });
    const err = h('div', { class: 'err', hidden: true });
    const pre = h('pre', { class: 'handoff' }, 'Loading the handoff…');
    const named = !!(x.library && x.library.named) || x.source === 'ai';
    const load = async () => { try { const r = await fetch('/api/sessions/' + x.id + '/handoff', { credentials: 'same-origin', headers: { 'X-Cully-UI': '1' } }); pre.textContent = r.ok ? await r.text() : 'The handoff is not available.'; } catch (_) { pre.textContent = 'The handoff is not available.'; } };
    load();
    const go = h('button', { class: 'btn', onclick: async () => {
      go.disabled = true; err.hidden = true;
      try { const v = await api('/api/sessions/' + x.id + '/continue', { method: 'POST', body: JSON.stringify({ prompt: ta.value }) }); closeMCP(); await refresh(); location.hash = '#/task/' + v.id; }
      catch (e) { err.textContent = e.message; err.hidden = false; go.disabled = false; }
    } }, icon('plus'), 'Start the task');
    const target = AGENT_NAMES[(S.info && S.info.agent) || 'claude'] || 'Claude';
    const modal = h('div', { class: 'modal', role: 'dialog', 'aria-modal': 'true', 'aria-label': 'Continue as a task' },
      h('header', {}, h('h2', {}, 'Continue as a task'), h('button', { class: 'btn ghost small sp', onclick: closeMCP }, 'Cancel')),
      h('div', { class: 'mbody' },
        h('p', {}, harnessChip(x.agent), ' ', h('b', {}, x.title || x.name)),
        h('p', { class: 'faint small path' }, x.cwd),
        h('p', { class: 'faint' }, 'A new task starts in ' + target + ', Cully\'s harness, with this handoff as its first message and the original session record available to read. ' + (x.agent === target.toLowerCase() ? '' : 'The session stays in ' + (AGENT_NAMES[x.agent] || x.agent) + ' as it was; the work carries on here.')),
        named ? null : h('p', { class: 'callout' }, 'Generate a summary from the session page to include the goal, outcome, and remaining work in the handoff.'),
        h('details', { class: 'rl', open: true }, h('summary', {}, h('span', { class: 'rq' }, 'The handoff the task starts with')), h('div', { class: 'rbody' }, pre)),
        ta, err, h('div', { class: 'row' }, go)));
    const bg = h('div', { class: 'modal-bg', onclick: (e) => { if (e.target === bg) closeMCP(); } }, modal);
    $('modal-root').replaceChildren(bg);
    S.modal = { agent: 'continue' };
    document.removeEventListener('keydown', escClose);
    document.addEventListener('keydown', escClose);
    ta.focus();
  }
  // Continue or fork a past Claude session as an agent this page runs, in the folder it started in.
  function continueSession(x, fork) {
    const ta = h('textarea', { 'aria-label': 'What to ask first', rows: '3', placeholder: 'What should it do next? Leave empty to continue where it left off.' });
    const err = h('div', { class: 'err', hidden: true });
    const go = h('button', { class: 'btn', onclick: async () => {
      go.disabled = true; err.hidden = true;
      try {
        const v = await api('/api/history/' + x.id + '/resume', { method: 'POST', body: JSON.stringify({ prompt: ta.value, fork }) });
        closeMCP(); await refresh(); location.hash = '#/task/' + v.id;
      } catch (e) { err.textContent = e.message; err.hidden = false; go.disabled = false; }
    } }, icon('send'), fork ? 'Fork and start' : 'Resume');
    const modal = h('div', { class: 'modal', role: 'dialog', 'aria-modal': 'true', 'aria-label': fork ? 'Fork session' : 'Resume session' },
      h('header', {}, h('h2', {}, fork ? 'Fork this session' : 'Resume this session'), h('button', { class: 'btn ghost small sp', onclick: closeMCP }, 'Cancel')),
      h('div', { class: 'mbody' },
        h('p', {}, h('b', {}, x.title || x.name)),
        h('p', { class: 'faint small path' }, x.cwd),
        sessionSummary(x),
        h('p', { class: 'faint' }, fork ? 'The agent starts with the history of this session and works as a new session. The original is left as it is.' : 'The agent carries on this same session.'),
        h('p', { class: 'faint' }, 'It works in this folder directly, not in a separate branch, so changes land in your files.'),
        ta, err, h('div', { class: 'row' }, go)));
    const bg = h('div', { class: 'modal-bg', onclick: (e) => { if (e.target === bg) closeMCP(); } }, modal);
    $('modal-root').replaceChildren(bg);
    S.modal = { agent: 'continue' };
    document.removeEventListener('keydown', escClose);
    document.addEventListener('keydown', escClose);
    ta.focus();
  }

  // Past sessions the agent recorded on this machine, with a way to continue them.
  async function loadHistory(days) {
    try { S.hist = await api('/api/history?agent=all&days=0'); S.histErr = ''; } catch (e) { S.histErr = e.message; }
    drawHistory();
  }
  // Sessions open right now, drawn as cards like the recent ones. When the agent
  // has recorded the session, its title and the Fork button come from there.
  // What the harness recorded about a session's context, or plainly that it records none.
  function snapChip(sn) {
    if (!sn) return null;
    if (sn.context) return h('span', { class: 'chip ctx ' + (sn.context.near ? 'warn' : ''), title: fmtTokens(sn.context.used) + ' of ' + (sn.context.estimated ? 'about ' : '') + fmtTokens(sn.context.window) + ' tokens in the context' }, 'Context ' + sn.context.percent + '%');
    return h('span', { class: 'chip', title: sn.note || '' }, sn.agent === 'cursor' ? 'No usage recorded' : 'Context not recorded yet');
  }
  function drawActive() {
    const box = $('active');
    if (!box) return;
    const list = (S.sessions || []).filter((x) => S.showClosedSessions || !x.detached);
    if (!list.length) { box.replaceChildren(h('div', { class: 'opennow-empty' }, h('span', { class: 'tc-dot', 'aria-hidden': 'true' }), h('b', {}, 'Open now'), h('span', { class: 'faint' }, 'Nothing is open. Open Claude, Codex or Cursor in a terminal and it appears here.'), h('a', { class: 'linkbtn', href: '#/' }, 'Start a task'))); return; }
    box.replaceChildren(h('h3', { class: 'section-h' }, 'Open now'), h('ul', { class: 'hrows active' }, ...list.map((x) => {
      const rec = S.hist && S.hist.sessions.find((r) => r.id === x.sessionId && r.agent === x.agent && r.cwd === x.cwd);
      return h('li', { class: 'hrow' + (x.detached ? ' detached' : '') },
        h('div', { class: 'hmain' },
          h('div', { class: 'ht' }, harnessChip(x.agent), rec ? sessionLink(rec) : h('a', { class: 'session-link', href: '#/sessions/live/' + x.pid }, x.name), h('span', { class: 'chip ok' }, x.detached ? 'Terminal closed' : 'Active')),
          rec && rec.description ? h('p', { class: 'hdesc' }, rec.description) : null,
          h('div', { class: 'faint small path' }, x.cwd),
          h('div', { class: 'row' }, x.branch ? h('span', { class: 'chip mono' }, icon('branch'), x.branch) : null,
            h('span', { class: 'chip' }, x.via === 'cully' ? 'In the Cully terminal' : 'In a plain terminal'),
            x.started && !x.started.startsWith('0001') ? h('span', { class: 'chip' }, 'open for ' + ago(x.started).replace(' ago', '')) : null,
            snapChip(x.snapshot))),
        h('div', { class: 'row hact' },
          rec && rec.agent === 'claude' ? h('button', { class: 'btn small', title: 'Start a new session from this one; the original stays as it is', onclick: () => continueSession(rec, true) }, icon('branch'), 'Fork') : null,
          h('button', { class: rec ? 'btn ghost small' : 'btn small', disabled: !(x.repo || x.cwd), onclick: () => startHere(x) }, icon('plus'), 'New task here'),
          h('a', { class: 'btn ghost small', href: rec ? sessionHref(rec.id) : '#/sessions/live/' + x.pid }, 'Open session')));
    })));
  }
  function drawHistory() {
    drawActive();
    const box = $('history');
    if (!box) return;
    if (S.histErr) { box.replaceChildren(h('div', { class: 'err' }, S.histErr)); return; }
    const hist = S.hist;
    if (!hist) { box.replaceChildren(h('p', { class: 'faint' }, 'Loading recent sessions…')); return; }
    // Tabs by how long ago a session was last active: today, the last 7, 15 and 30 days, and older.
    const startOfToday = new Date(); startOfToday.setHours(0, 0, 0, 0);
    const ageDays = (iso) => Math.floor((startOfToday.getTime() - new Date(iso).getTime()) / 864e5) + 1;
    const BUCKETS = [['today', 'Today', (x) => new Date(x.updated) >= startOfToday], ['d7', 'Last 7 days', (x) => new Date(x.updated) < startOfToday && ageDays(x.updated) <= 7], ['d15', 'Last 15 days', (x) => ageDays(x.updated) > 7 && ageDays(x.updated) <= 15],
      ['d30', 'Last 30 days', (x) => ageDays(x.updated) > 15 && ageDays(x.updated) <= 30], ['older', 'Older', (x) => ageDays(x.updated) > 30]];
    const recent = hist.sessions.filter((x) => !x.open);
    const inBucket = (k) => recent.filter(BUCKETS.find((b) => b[0] === k)[2]);
    const terms = searchTerms(S.sessionQuery || '');
    const filtering = !!(S.sessionHarness || S.sessionOutcome);
    const query = terms.length || filtering;
    drawSearchFilters(hist.sessions);
    if (!S.histPicked || !S.histBucket) S.histBucket = (BUCKETS.find((b) => inBucket(b[0]).length) || BUCKETS[0])[0];
    const bucket = query ? 'search' : S.histBucket;
    const scored = query ? hist.sessions.filter((x) => (!S.sessionHarness || x.agent === S.sessionHarness) && (!S.sessionOutcome || sessionOutcomeOf(x) === S.sessionOutcome))
      .map((x) => [x, sessionScore(x, terms)]).filter((p) => p[1] > 0).sort((a, b) => b[1] - a[1] || new Date(b[0].updated) - new Date(a[0].updated)) : null;
    const items = query ? scored.map((p) => p[0]) : inBucket(bucket);
    if (S.searchCount) S.searchCount.textContent = query ? items.length + (items.length === 1 ? ' session' : ' sessions') + ' of ' + hist.sessions.length : '';
    const shown = (S.histShown && S.histShown[bucket]) || 20;
    const parts = [];
    if (hist.note) parts.push(h('p', { class: 'faint' }, hist.note));
    const headRow = h('div', { class: 'sx-head' }, h('h3', { class: 'section-h' }, query ? 'Search results' : 'Recent'));
    parts.push(headRow);
    if (!query) headRow.append(h('div', { class: 'seg tabs-seg', role: 'tablist', 'aria-label': 'How long ago' }, ...BUCKETS.map(([k, label]) => h('button', { type: 'button', role: 'tab', class: k === bucket ? 'on' : '', 'aria-selected': String(k === bucket), onclick: () => { S.histBucket = k; S.histPicked = true; drawHistory(); } }, label + ' ' + inBucket(k).length))));
    if (query && !items.length) parts.push(h('div', { class: 'card ssempty' }, h('b', {}, terms.length ? 'No session matches "' + (S.sessionQuery || '').trim() + '"' : 'No session matches these filters'),
      h('p', { class: 'faint small' }, 'Every word has to match. Try fewer words, or search by a folder, a branch, or part of the session ID. Use quotes for an exact phrase.'),
      h('div', { class: 'row' }, h('button', { class: 'btn ghost small', onclick: () => { S.sessionQuery = ''; S.sessionHarness = ''; S.sessionOutcome = ''; const el = $('session-search'); if (el) el.value = ''; drawHistory(); } }, 'Clear search and filters'))));
    else if (!items.length) parts.push(emptyCard('Nothing in this period', recent.length ? 'Pick another period above.' : 'Sessions you run appear here so you can continue them.', 'Start a task', '#/'));
    const copyCmd = (cmd, what) => async () => {
      try { await writeClip(cmd); toast('Copied. Paste it in a terminal to ' + what + '.'); } catch (_) { showCommand(cmd); }
    };
    if (items.length) {
      const row = (x) => {
        const runnable = x.agent === 'claude';
        const folder = (x.cwd || '').split('/').filter(Boolean).pop() || x.cwd || '';
        const oc = sessionOutcomeOf(x);
        const ocChip = oc === 'yes' ? h('span', { class: 'chip ok' }, 'Finished') : oc === 'partly' ? h('span', { class: 'chip' }, 'Partly') : oc === 'no' ? h('span', { class: 'chip warn' }, 'Not finished') : null;
        const item = (ic, label, sub, fn, disabled) => h('button', { type: 'button', class: 'folderrow', disabled: !!disabled, onclick: (e) => { const d = e.currentTarget.closest('details'); if (d) d.open = false; fn(); } }, icon(ic), h('span', { class: 'fr-main' }, h('b', {}, label), sub ? h('small', { class: 'faint' }, sub) : null));
        const menu = h('details', { class: 'menu sx-menu' }, h('summary', { class: 'btn ghost small', 'aria-label': 'More actions', title: 'More actions' }, '⋯'),
          h('div', { class: 'menu-pop pop-card' }, h('div', { class: 'popcol' },
            runnable ? item('branch', 'Fork', 'A new session from this one; the original stays', () => continueSession(x, true), !x.exists) : item('branch', 'Copy fork command', 'Paste it in a terminal', copyCmd(x.fork || x.resume, 'fork it'), !x.exists),
            runnable ? item('send', 'Resume this session', 'Continue the same conversation', () => continueSession(x, false), !x.exists) : null,
            runnable ? item('plus', 'Continue as a task', 'Start a task from its handoff', () => continueAsTask(x), !x.exists) : null,
            item('copy', runnable ? 'Copy command' : 'Copy resume command', 'Resume it in a terminal', copyCmd(x.resume, 'continue it')))));
        const primary = runnable
          ? h('button', { type: 'button', class: 'btn small', disabled: !x.exists, title: 'Continue this same session here', onclick: () => continueSession(x, false) }, icon('send'), 'Resume')
          : h('button', { type: 'button', class: 'btn small', disabled: !x.exists, title: 'Start a new task from this session\'s handoff', onclick: () => continueAsTask(x) }, icon('plus'), 'Continue');
        const li = h('li', { class: 'sx-row' },
          h('div', { class: 'sx-main' },
            h('div', { class: 'sx-top' }, harnessChip(x.agent), h('span', { class: 'sx-title' }, sessionLink(x, terms)), ocChip, h('span', { class: 'sp' }), h('span', { class: 'sx-time', title: x.updated ? new Date(x.updated).toLocaleString() : '' }, ago(x.updated))),
            h('p', { class: 'sx-desc' + (x.description ? '' : ' faint') }, x.description ? highlight(x.description, terms) : 'Not summarised yet. Open it and use Summarize.'),
            matchSnippet(x, terms),
            h('div', { class: 'sx-meta' }, folder ? h('span', { class: 'chip', title: x.cwd }, icon('folder'), highlight(folder, terms)) : null,
              x.branch && x.branch !== 'HEAD' ? h('span', { class: 'chip mono', title: x.branch }, icon('branch'), x.branch) : null,
              x.activeSec ? h('span', { class: 'faint small', title: 'Time spent working in this session' }, dur(x.activeSec) + ' worked') : null)),
          h('div', { class: 'sx-act' }, primary, menu));
        li.addEventListener('click', (e) => { if (e.target.closest('a, button, summary, details, input, select')) return; location.hash = sessionHref(x.id); });
        return li;
      };
      parts.push(h('ul', { class: 'sx-rows' }, ...items.slice(0, shown).map(row)));
      if (items.length > shown) parts.push(h('div', { class: 'row' }, h('button', { class: 'btn ghost', onclick: () => { S.histShown = { ...(S.histShown || {}), [bucket]: shown + 20 }; drawHistory(); } }, 'Show ' + Math.min(20, items.length - shown) + ' more of ' + (items.length - shown))));
    }
    if (hist.older) parts.push(h('p', { class: 'faint small' }, 'Showing the most recent sessions of each harness. Older ones are kept by the agents themselves.'));
    box.replaceChildren(...parts);
  }

  const sessionHref = (id) => '#/sessions/' + encodeURIComponent(id);
  const sessionLink = (x, terms) => h('a', { class: 'session-link', href: sessionHref(x.id) }, terms && terms.length ? highlight(x.title || x.name, terms) : (x.title || x.name));
  const sessionDate = (d) => d && !d.startsWith('0001') ? new Date(d).toLocaleString() : 'Unavailable';

  function sessionSection(title, ...children) {
    return h('section', { class: 'lib-card session-section' }, h('h2', {}, title), ...children);
  }

  function sessionUsage(snapshot) {
    if (!snapshot || !snapshot.available) return h('p', { class: 'faint' }, (snapshot && snapshot.note) || 'Usage is unavailable.');
    return h('div', { class: 'row' }, snapshot.model ? h('span', { class: 'chip' }, snapshot.model) : null,
      snapChip(snapshot), snapshot.tokens ? h('span', { class: 'chip' }, fmtTokens(snapshot.tokens) + ' tokens') : null);
  }

  async function renderLiveSession(pid) {
    const current = S.route;
    const box = $('sessions');
    const back = h('a', { class: 'linkbtn', href: '#/sessions' }, 'All sessions');
    box.replaceChildren(back, toolbar('Live session', 'Loading…'));
    try {
      const [live, hist] = await Promise.all([api('/api/sessions'), api('/api/history?agent=all&days=7')]);
      if (S.route !== current) return;
      const x = live.find((v) => String(v.pid) === pid);
      if (!x) { box.replaceChildren(back, emptyCard('Session closed', 'Look for its record in Sessions.', 'Open Sessions', '#/sessions')); return; }
      const related = hist.sessions.filter((v) => v.agent === x.agent && v.cwd === x.cwd);
      const exact = related.find((v) => v.id === x.sessionId);
      if (exact) { location.replace(sessionHref(exact.id)); return; }
      box.replaceChildren(back, toolbar(x.name, x.cwd, refreshBtn(() => renderLiveSession(pid))),
        sessionSection('Live session', h('div', { class: 'row' }, harnessChip(x.agent), h('span', { class: 'chip' }, x.detached ? 'Terminal closed' : 'Active'),
          x.branch ? h('span', { class: 'chip mono' }, x.branch) : null, h('code', {}, 'PID ' + x.pid)),
          h('p', {}, 'Started ' + sessionDate(x.started))),
        sessionSection('Recorded sessions in this folder',
          h('p', { class: 'faint' }, 'Cully could not confirm which record belongs to this terminal.'),
          related.length ? h('ul', { class: 'rules' }, ...related.slice(0, 12).map((v) => h('li', {}, sessionLink(v), h('span', { class: 'faint' }, ago(v.updated)))))
            : h('p', { class: 'faint' }, 'No recent records for this harness and folder.')));
    } catch (e) {
      if (S.route === current) box.replaceChildren(back, h('p', { class: 'err' }, e.message));
    }
  }

  // ---- The journey: every tool call of a session as a graph you can play. One lane per kind of tool, one node per
  // call in order, failures and repeated calls marked, with play, step, speed and a scrubber, and the call's
  // parameters beside it. It is drawn from the harness's own record, so Cursor (no timestamps, no results) shows
  // order only. ----
  const JOURNEY_LANES = [['edit', 'File edits'], ['read', 'Reading and searching'], ['shell', 'Shell commands'], ['web', 'Web'], ['agent', 'Subagents'], ['mcp', 'MCP tools'], ['other', 'Other tools']];
  function journeyKind(name) {
    const n = name || '';
    if (n.startsWith('mcp__')) return 'mcp';
    if (/^(Edit|Write|MultiEdit|NotebookEdit|apply_patch|StrReplace|ApplyPatch)$/.test(n)) return 'edit';
    if (/^(Read|Glob|Grep|LS|NotebookRead|Search|ReadFile)$/.test(n)) return 'read';
    if (/^(Bash|BashOutput|KillShell|exec_command|shell|Shell)$/.test(n)) return 'shell';
    if (/^(WebSearch|WebFetch)$/.test(n)) return 'web';
    if (/^(Task|Agent)$/.test(n)) return 'agent';
    return 'other';
  }
  function journeyView(calls) {
    const NS = 'http://www.w3.org/2000/svg';
    const el = (tag, attrs, ...kids) => { const n = document.createElementNS(NS, tag); for (const [k, v] of Object.entries(attrs || {})) n.setAttribute(k, v); for (const c of kids) if (c) n.append(c); return n; };
    const n = calls.length;
    const step = 20, padX = 14, laneH = 34, padY = 14;
    const lanes = JOURNEY_LANES.filter(([k]) => calls.some((c) => journeyKind(c.name) === k));
    const laneAt = Object.fromEntries(lanes.map(([k], i) => [k, i]));
    const W = padX * 2 + n * step, H = padY * 2 + lanes.length * laneH;
    const X = (i) => padX + i * step + step / 2;
    const Y = (c) => padY + laneAt[journeyKind(c.name)] * laneH + laneH / 2;
    // A call repeated three or more times in a row, with the same arguments, is the loop the research warns about.
    const sig = calls.map((c) => (c.name || '') + '|' + (c.params || ''));
    const rep = new Array(n).fill(false);
    for (let i = 0; i < n;) { let j = i; while (j + 1 < n && sig[j + 1] === sig[i]) j++; if (j - i >= 2) for (let k = i; k <= j; k++) rep[k] = true; i = j + 1; }
    const svg = el('svg', { class: 'jsvg', width: W, height: H, viewBox: '0 0 ' + W + ' ' + H, role: 'img', 'aria-label': 'The session as a graph: one node per tool call, in order' });
    lanes.forEach((_, i) => svg.append(el('rect', { class: 'jlane' + (i % 2 ? ' alt' : ''), x: 0, y: padY + i * laneH, width: W, height: laneH })));
    const edges = [], nodes = [];
    for (let i = 0; i + 1 < n; i++) {
      const x1 = X(i), y1 = Y(calls[i]), x2 = X(i + 1), y2 = Y(calls[i + 1]); const xm = (x1 + x2) / 2;
      const e = el('path', { class: 'jedge', d: 'M' + x1 + ' ' + y1 + ' C' + xm + ' ' + y1 + ' ' + xm + ' ' + y2 + ' ' + x2 + ' ' + y2 }); edges.push(e); svg.append(e);
    }
    const marker = el('line', { class: 'jmark', x1: 0, x2: 0, y1: 0, y2: H });
    svg.append(marker);
    let cur = n - 1; let timer = 0;
    calls.forEach((c, i) => {
      const k = journeyKind(c.name);
      const g = el('g', { class: 'jnode jk-' + k + (c.result === 'failed' ? ' failed' : '') + (rep[i] ? ' rep' : '') },
        el('title', {}, '#' + c.number + ' ' + (c.name || 'tool') + (c.result === 'failed' ? ', failed' : '') + (rep[i] ? ', repeated' : '')),
        rep[i] ? el('circle', { class: 'jrep', cx: X(i), cy: Y(c), r: 9 }) : null,
        el('circle', { class: 'jdot', cx: X(i), cy: Y(c), r: 5.5 }));
      g.addEventListener('click', () => { pause(); go(i); });
      nodes.push(g); svg.append(g);
    });
    const labels = h('div', { class: 'jlabels', 'aria-hidden': 'true' }, ...lanes.map(([k, l]) => h('div', { class: 'jlab jk-' + k }, h('i', {}), l)));
    labels.style.paddingTop = padY + 'px';
    for (const lab of labels.children) lab.style.height = laneH + 'px'; // set as a property: the h() helper does not take a style string
    const scroller = h('div', { class: 'jscroll', tabindex: '0', 'aria-label': 'Scroll the journey left and right' }, svg);
    const detail = h('div', { class: 'jdetail', 'aria-live': 'polite' });
    const counter = h('span', { class: 'jcount faint small' });
    const bar = h('input', { type: 'range', class: 'jbar', min: '0', max: String(Math.max(0, n - 1)), value: String(cur), 'aria-label': 'Position in the session' });
    const playBtn = h('button', { type: 'button', class: 'btn small', title: 'Play the journey from the start (Space)' }, icon('send'), 'Play');
    const prev = h('button', { type: 'button', class: 'btn ghost small', 'aria-label': 'Previous call', title: 'Previous call (Left arrow)' }, '‹');
    const next = h('button', { type: 'button', class: 'btn ghost small', 'aria-label': 'Next call', title: 'Next call (Right arrow)' }, '›');
    const speed = h('select', { 'aria-label': 'Playback speed', class: 'jsel' }, ...[['1', 'Slow'], ['3', 'Normal'], ['8', 'Fast'], ['25', 'Very fast']].map(([v, l]) => h('option', { value: v }, l)));
    speed.value = '3';
    const paint = (i) => { nodes[i].classList.toggle('future', i > cur); nodes[i].classList.toggle('now', i === cur); if (i < edges.length) edges[i].classList.toggle('future', i >= cur); };
    const showDetail = () => {
      const c = calls[cur]; if (!c) { detail.replaceChildren(); return; }
      const k = journeyKind(c.name); const lane = (JOURNEY_LANES.find((l) => l[0] === k) || [])[1] || '';
      detail.replaceChildren(h('div', { class: 'row' }, h('span', { class: 'chip jchip jk-' + k }, lane), h('b', { class: 'mono' }, c.name || 'Unnamed tool'), h('span', { class: 'faint small' }, '#' + c.number + ' of ' + n),
        c.at ? h('time', { datetime: c.at, class: 'faint small' }, sessionDate(c.at)) : null,
        h('span', { class: 'chip ' + (c.result === 'failed' ? 'warn' : '') }, c.result === 'failed' ? 'Failed' : c.result === 'returned' ? 'Returned' : 'Result not recorded'),
        rep[cur] ? h('span', { class: 'chip warn', title: 'The same call with the same arguments, three or more times in a row' }, 'Repeated') : null),
        h('pre', { class: 'pre jparams' }, c.params || 'No parameters were recorded for this call.'));
    };
    function go(i, scroll = true) {
      const old = cur; cur = Math.max(0, Math.min(n - 1, i));
      for (let k = Math.max(0, Math.min(old, cur) - 1); k <= Math.min(n - 1, Math.max(old, cur) + 1); k++) paint(k);
      marker.setAttribute('x1', X(cur)); marker.setAttribute('x2', X(cur));
      bar.value = String(cur); counter.textContent = (cur + 1) + ' of ' + n; showDetail();
      if (scroll) { const x = X(cur), vw = scroller.clientWidth; if (x < scroller.scrollLeft + 30 || x > scroller.scrollLeft + vw - 30) scroller.scrollLeft = Math.max(0, x - vw * 0.55); }
    }
    function pause() { clearInterval(timer); timer = 0; playBtn.replaceChildren(icon('send'), 'Play'); playBtn.title = 'Play the journey from the start (Space)'; }
    function play() {
      if (cur >= n - 1) go(0);
      playBtn.replaceChildren(icon('stop'), 'Pause'); playBtn.title = 'Pause (Space)';
      clearInterval(timer);
      timer = setInterval(() => { if (!document.body.contains(scroller)) { clearInterval(timer); return; } if (cur >= n - 1) { pause(); return; } go(cur + 1); }, Math.max(20, 1000 / Number(speed.value)));
    }
    const toggle = () => (timer ? pause() : play());
    playBtn.addEventListener('click', toggle);
    prev.addEventListener('click', () => { pause(); go(cur - 1); }); next.addEventListener('click', () => { pause(); go(cur + 1); });
    speed.addEventListener('change', () => { if (timer) play(); });
    bar.addEventListener('input', () => { pause(); go(Number(bar.value)); });
    const wrap = h('div', { class: 'journeybox' });
    wrap.addEventListener('keydown', (e) => { if (e.target.tagName === 'SELECT' || e.target.tagName === 'INPUT') return; if (e.key === ' ') { e.preventDefault(); toggle(); } else if (e.key === 'ArrowRight') { e.preventDefault(); pause(); go(cur + 1); } else if (e.key === 'ArrowLeft') { e.preventDefault(); pause(); go(cur - 1); } });
    scroller.addEventListener('wheel', (e) => { if (Math.abs(e.deltaY) > Math.abs(e.deltaX) && !e.shiftKey) return; e.preventDefault(); scroller.scrollLeft += (e.deltaX || e.deltaY); }, { passive: false });
    const legend = h('div', { class: 'jlegend faint small' }, h('span', { class: 'jl ok' }, h('i', {}), 'returned'), h('span', { class: 'jl bad' }, h('i', {}), 'failed'), h('span', { class: 'jl rep' }, h('i', {}), 'repeated 3 or more times'), h('span', {}, 'Click a node, or press Space to play. Scroll sideways for more.'));
    wrap.append(h('div', { class: 'jcontrols' }, playBtn, prev, next, speed, bar, counter), h('div', { class: 'jstage' }, labels, scroller), legend, detail);
    for (let k = 0; k < n; k++) paint(k);
    go(n - 1, false);
    requestAnimationFrame(() => { scroller.scrollLeft = scroller.scrollWidth; });
    return wrap;
  }
  async function renderSessionDetail(id) {
    const current = S.route;
    const box = $('sessions');
    const back = h('a', { class: 'linkbtn', href: '#/sessions' }, 'All sessions');
    const path = '/api/sessions/' + encodeURIComponent(id);
    box.replaceChildren(back, toolbar('Session', 'Loading…'));
    try {
      const { session: x, activity, snapshot, exists } = await api(path);
      if (S.route !== current) return;
      const summary = h('button', { class: 'btn ghost small', disabled: !x.turns, onclick: async () => {
        summary.disabled = true;
        summary.textContent = 'Reading the session…';
        try {
          await api(path + '/summarize', { method: 'POST' });
          S.hist = null; S.libMapAt = 0;
          if (S.route === current) renderSessionDetail(id);
        } catch (e) { toast(e.message, true); summary.disabled = false; summary.textContent = summaryLabel; }
      } }, icon('spark'), x.source === 'ai' ? 'Refresh summary' : 'Generate summary');
      const summaryLabel = summary.textContent;
      const tools = Object.entries(x.toolNames || {}).sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]));
      const callRows = h('ol', { class: 'session-calls' });
      const drawCalls = (calls) => {
        callRows.replaceChildren(...calls.map((v) => h('li', { value: v.number },
          h('details', { class: 'callparams' },
            h('summary', {},
              h('code', {}, v.name || 'Unnamed tool'),
              v.kind ? h('span', { class: 'faint small' }, v.kind === 'verification' ? 'Verification' : 'File edit') : null,
              v.at ? h('time', { datetime: v.at, class: 'faint small' }, sessionDate(v.at)) : null,
              h('span', { class: 'chip ' + (v.result === 'failed' ? 'warn' : '') }, v.result === 'failed' ? 'Failed' : v.result === 'returned' ? 'Returned' : 'Result unavailable')),
            h('pre', { class: 'pre' }, v.params || 'No parameters were recorded for this call.')))));
        pageLabel.textContent = calls.length ? calls[0].number + '–' + calls[calls.length - 1].number + ' of ' + callCount : 'No calls on this page';
        previous.disabled = offset === 0;
        next.disabled = offset + calls.length >= callCount || calls.length === 0;
      };
      let offset = 0, callCount = x.tools;
      const pageLabel = h('span', { 'aria-live': 'polite', class: 'faint small' });
      const loadCalls = async (target) => {
        previous.disabled = next.disabled = true;
        try {
          const page = await api(path + '?offset=' + target);
          if (S.route !== current) return;
          offset = target; callCount = page.session.tools;
          drawCalls(page.activity.calls);
        } catch (e) { toast(e.message, true); previous.disabled = offset === 0; next.disabled = false; }
      };
      // Every call with its parameters as one JSON file, for review outside the page.
      const exportBtn = h('button', { class: 'btn ghost small', title: 'Download every tool call with its parameters (secrets redacted)' }, 'Export calls');
      exportBtn.addEventListener('click', async () => {
        exportBtn.disabled = true;
        try {
          const all = []; let off = 0;
          for (;;) { const pg = await api(path + '?offset=' + off); all.push(...pg.activity.calls); off += pg.activity.calls.length; if (!pg.activity.calls.length || off >= pg.session.tools) break; }
          const blob = new Blob([JSON.stringify({ session: id, agent: x.agent, title: x.title, calls: all }, null, 2)], { type: 'application/json' });
          const a = h('a', { href: URL.createObjectURL(blob), download: 'session-' + id.slice(0, 8) + '-calls.json' }); document.body.append(a); a.click(); a.remove();
        } catch (e) { toast(e.message, true); }
        exportBtn.disabled = false;
      });
      const previous = h('button', { class: 'btn ghost small', onclick: () => loadCalls(Math.max(0, offset - activity.calls.length)) }, 'Previous calls');
      const next = h('button', { class: 'btn ghost small', onclick: () => loadCalls(offset + activity.calls.length) }, 'Next calls');
      drawCalls(activity.calls);
      // The journey is the default view; the list keeps the same calls, each expandable, with paging.
      const journeyBox = h('div', { class: 'journey' }, h('p', { class: 'faint small' }, 'Loading the journal…'));
      const listBox = h('div', { hidden: true }, callRows, h('div', { class: 'row' }, x.tools > activity.calls.length ? previous : null, x.tools > activity.calls.length ? pageLabel : null, x.tools > activity.calls.length ? next : null));
      const viewBtn = (label, which) => h('button', { type: 'button', class: which === 'graph' ? 'on' : '', onclick: (e) => { journeyBox.hidden = which !== 'graph'; listBox.hidden = which !== 'list'; for (const b of e.currentTarget.parentNode.children) b.classList.toggle('on', b === e.currentTarget); } }, label);
      const viewSeg = h('div', { class: 'seg', role: 'group', 'aria-label': 'View' }, viewBtn('Graph', 'graph'), viewBtn('List', 'list'));
      (async () => {
        const all = [...activity.calls]; let off = activity.calls.length;
        try {
          while (all.length < x.tools && off > 0) {
            journeyBox.replaceChildren(h('p', { class: 'faint small' }, 'Loading the journal: ' + all.length + ' of ' + x.tools + ' calls…'));
            const pg = await api(path + '?offset=' + off);
            if (S.route !== current) return;
            if (!pg.activity.calls.length) break;
            all.push(...pg.activity.calls); off += pg.activity.calls.length;
          }
          if (S.route !== current) return;
          journeyBox.replaceChildren(all.length ? journeyView(all) : h('p', { class: 'faint' }, 'No tool calls were recorded.'));
        } catch (e) { journeyBox.replaceChildren(h('p', { class: 'err' }, 'Could not load the whole journal: ' + e.message)); }
      })();
      box.replaceChildren(back, toolbar(x.title || x.name, x.cwd,
        refreshBtn(() => renderSessionDetail(id)),
        h('button', { class: 'btn', disabled: !exists, onclick: () => continueAsTask(x) }, icon('plus'), 'Continue as a task')),
        h('div', { class: 'row' }, harnessChip(x.agent),
          x.branch && x.branch !== 'HEAD' ? h('span', { class: 'chip mono' }, icon('branch'), x.branch) : null,
          exists ? null : h('span', { class: 'chip warn' }, 'Folder is gone')),
        sessionSection('What happened',
          x.task ? h('p', {}, h('b', {}, 'Goal '), x.task) : null,
          h('p', {}, x.description || 'No activity summary is available yet.'),
          x.criteria && x.criteria.length ? h('details', {}, h('summary', {}, 'How to verify the goal'), h('ul', {}, ...x.criteria.map((v) => h('li', {}, v)))) : null,
          h('div', { class: 'row' }, summary)),
        sessionSection('Steps in this session' + (x.steps && x.steps.length ? ' (' + x.steps.length + ')' : ''),
          taskStories(x.steps, 'No step breakdown yet. Use Summarize or let background analysis name this session again.')),
        sessionSection('Session health',
          h('div', { class: 'row' }, x.turns || x.tools ? ring(x.health, 48) : null,
            h('b', {}, DONE_LABEL[x.completed] || 'Outcome unassessed'),
            x.onTask === 'drifted' ? h('span', { class: 'chip warn' }, 'Drifted') : null),
          x.drift ? h('p', {}, x.drift) : null,
          x.signals && x.signals.length ? h('ul', { class: 'signals' }, ...x.signals.map((v) => h('li', {}, icon('alert'), v))) : h('p', { class: 'faint' }, 'No health flags in the recorded activity.'),
          x.different && x.different !== 'Nothing notable.' ? h('p', {}, h('b', {}, 'What could go better '), x.different) : null),
        h('div', { class: 'kpis' }, ...[['Requests', x.turns], ['Tool calls', x.tools], ['Tool errors', x.agent === 'cursor' ? 'Unavailable' : x.errors], ['Verification calls', x.checks], ['Files with edits', x.files]].map(([label, n]) => h('div', { class: 'kpi' }, h('b', {}, n), h('span', {}, label)))),
        sessionSection('Journey',
          tools.length ? h('details', { class: 'toolcounts' }, h('summary', {}, 'Tools used (' + tools.length + ' kinds, ' + x.tools + ' calls)'), h('div', { class: 'row' }, ...tools.map(([name, n]) => h('span', { class: 'chip mono' }, name + ' × ' + n)))) : null,
          x.tools ? h('div', { class: 'row' }, viewSeg, exportBtn) : null,
          x.tools ? journeyBox : h('p', { class: 'faint' }, 'No tool calls were recorded.'),
          x.tools ? listBox : null),
        sessionSection('Files with recorded edits',
          activity.files.length ? h('ul', { class: 'session-files' }, ...activity.files.map((v) => h('li', {}, h('code', {}, v)))) : h('p', { class: 'faint' }, 'No file paths were recorded for edits.')),
        sessionSection('Session information',
          h('dl', { class: 'session-meta' },
            h('dt', {}, 'Session ID'), h('dd', {}, h('code', {}, x.id)),
            h('dt', {}, 'Started'), h('dd', {}, sessionDate(x.started)),
            h('dt', {}, 'Last activity'), h('dd', {}, sessionDate(x.updated)),
            h('dt', {}, 'Active time'), h('dd', {}, x.agent === 'cursor' ? 'Unavailable' : dur(x.activeSec))),
          sessionUsage(snapshot)));
    } catch (e) {
      if (S.route === current) box.replaceChildren(back, toolbar('Session unavailable', ''), h('p', { class: 'err' }, e.message));
    }
  }

  // ---------- working together ----------
  async function startReview(a) {
    try {
      const view = await api('/api/agents/' + a.id + '/review', { method: 'POST' });
      await refresh();
      location.hash = '#/task/' + view.id;
      toast('Review started');
    } catch (e) { toast(e.message, true); }
  }

  function openRelay(a) {
    const targets = S.agents.filter((x) => x.id !== a.id && (x.state === 'working' || x.state === 'waiting' || x.state === 'preparing'));
    let what = 'reply'; let to = targets[0] ? targets[0].id : ''; let note = '';
    const root = $('modal-root');
    const err = h('div', { class: 'err', hidden: true });
    function draw() {
      const kindBtn = (k, label) => h('button', { type: 'button', class: what === k ? 'on' : '', onclick: () => { what = k; draw(); } }, label);
      const list = targets.length
        ? h('div', { class: 'targets', role: 'radiogroup', 'aria-label': 'Send to' }, ...targets.map((x) =>
          h('button', { type: 'button', role: 'radio', 'aria-checked': to === x.id ? 'true' : 'false', class: 'target' + (to === x.id ? ' on' : ''), onclick: () => { to = x.id; draw(); } },
            dot(x), h('span', { class: 't' }, x.title), h('span', { class: 'chip mono' }, x.branch.replace(/^cully\//, '')))))
        : h('div', { class: 'callout' }, 'No other task is running. Start one first, then send to it.');
      const noteIn = h('input', { type: 'text', placeholder: 'Optional note, for example: please check the error handling', 'aria-label': 'Note' });
      noteIn.value = note;
      noteIn.addEventListener('input', () => { note = noteIn.value; });
      const send = h('button', { class: 'btn', disabled: !to, onclick: async () => {
        err.hidden = true;
        try {
          await api('/api/agents/' + a.id + '/relay', { method: 'POST', body: JSON.stringify({ to, what, note }) });
          closeMCP();
          toast('Sent to ' + (targets.find((x) => x.id === to) || {}).title);
        } catch (e) { err.textContent = e.message; err.hidden = false; }
      } }, icon('send'), 'Send');
      const modal = h('div', { class: 'modal', role: 'dialog', 'aria-modal': 'true', 'aria-label': 'Send to another task' },
        h('header', {}, h('h2', {}, 'Send to another task'), h('button', { class: 'btn ghost small sp', onclick: closeMCP }, 'Close')),
        h('div', { class: 'mbody' },
          h('p', {}, 'Cully relays this to the other agent and shows it in both feeds. The other agent will respond to it.'),
          h('div', {}, h('div', { class: 'opt-label' }, 'What to send'), h('div', { class: 'seg' }, kindBtn('reply', 'Latest reply'), kindBtn('diff', 'Changes (diff)'))),
          h('div', {}, h('div', { class: 'opt-label' }, 'Send to'), list),
          noteIn, err, h('div', { class: 'row' }, send)));
      const bg = h('div', { class: 'modal-bg', onclick: (e) => { if (e.target === bg) closeMCP(); } }, modal);
      root.replaceChildren(bg);
      S.modal = { agent: 'relay' };
      document.removeEventListener('keydown', escClose);
      document.addEventListener('keydown', escClose);
    }
    draw();
  }

  // ---------- notifications ----------
  const NOTIFY_KEY = 'cully-notify';
  function notifyPrefs() {
    const base = { desktop: false, sound: false, flash: true, approvals: true };
    try { return { ...base, ...JSON.parse(localStorage.getItem(NOTIFY_KEY) || '{}') }; } catch (_) { return base; }
  }
  function saveNotifyPrefs(p) { try { localStorage.setItem(NOTIFY_KEY, JSON.stringify(p)); } catch (_) { /* storage blocked */ } }
  // Browsers only allow desktop notifications on HTTPS or localhost.
  function desktopProblem() {
    if (!('Notification' in window)) return 'This browser does not support desktop notifications.';
    if (!window.isSecureContext) return 'Desktop notifications need HTTPS or localhost. Open the UI through an SSH tunnel to localhost, or start it with --tls-cert and --tls-key.';
    if (Notification.permission === 'denied') return 'Desktop notifications are blocked for this site in your browser settings.';
    return '';
  }
  function beep() {
    try {
      const ctx = S.audio || (S.audio = new (window.AudioContext || window.webkitAudioContext)());
      const o = ctx.createOscillator(); const g = ctx.createGain();
      o.frequency.value = 880; g.gain.value = 0.05; o.connect(g); g.connect(ctx.destination);
      o.start(); o.stop(ctx.currentTime + 0.18);
    } catch (_) { /* no audio available */ }
  }
  let flashTimer = null;
  function startFlash(msg) {
    if (flashTimer) return;
    let on = false; const base = document.title.replace(/^•\s*/, '');
    flashTimer = setInterval(() => { on = !on; document.title = on ? '• ' + msg : base; }, 1000);
  }
  document.addEventListener('visibilitychange', () => { if (!document.hidden && flashTimer) { clearInterval(flashTimer); flashTimer = null; renderRail(); } });
  // urgent is a request to approve a tool call. It is told even with the tab open, with a tone unless
  // you switched approval alerts off, and it stays on screen until it is answered.
  function notifyUser(title, body, href, urgent) {
    const p = notifyPrefs();
    if (urgent && p.approvals) {
      const t = h('div', { class: 'toast urgent', role: 'alert' }, h('b', {}, title), body ? h('div', { class: 'small' }, body) : null,
        h('button', { class: 'btn small', onclick: () => { if (href) location.hash = href; t.remove(); } }, 'Review'),
        h('button', { class: 'btn ghost small', onclick: () => t.remove() }, 'Later'));
      t.dataset.urgent = '1'; $('toasts').append(t);
      beep();
    } else toast(body ? title + ': ' + body : title);
    if (p.sound && !(urgent && p.approvals)) beep();
    if ((document.hidden || urgent) && p.desktop && !desktopProblem() && Notification.permission === 'granted') {
      const n = new Notification(title, { body, tag: 'cully', requireInteraction: !!urgent });
      n.onclick = () => { window.focus(); if (href) location.hash = href; n.close(); };
    }
    if (document.hidden && p.flash) startFlash(title);
  }
  function attentionKeys() {
    const keys = new Map();
    for (const a of S.agents.filter((x) => x.state === 'waiting')) keys.set('wait:' + a.id, [a.title + ' needs you', '', '#/task/' + a.id]);
    for (const { a, p } of pendingAsks()) keys.set('perm:' + p.id, [a.title + ' asks to use ' + p.tool, (p.risk === 'high' ? 'High risk: ' : '') + (p.summary || '').slice(0, 120), '#/task/' + a.id]);
    for (const g of S.advice || []) for (const x of g.advice) if (!x.applied && !x.dismissed) keys.set('adv:' + x.id, ['New advice for ' + g.agentTitle, x.text, '#/dashboard/advisor']);
    if (memoryMode() === 'mcp') for (const m of mcpNeeding()) keys.set('auth:' + m.agent, ['Cully MCP needs sign-in', 'for ' + AGENT_NAMES[m.agent], '#/setup']);
    if (S.rules && S.rules.proposal) keys.set('rules:' + S.rules.proposal.id, ['Review your workflow rules', 'Cully drafted an update to dev-workflow-rules from your recent sessions. Nothing is saved until you approve it.', '#/improve/rules']);
    return keys;
  }
  function checkAttention() {
    const keys = attentionKeys();
    if (!S.attnSeen) S.attnSeen = new Set();
    if (!S.permSince) S.permSince = new Map();
    for (const [k, [t, b, href]] of keys) {
      const urgent = k.startsWith('perm:');
      if (S.attnSeen.has(k)) {
        // an unanswered approval is refused after ten minutes, so say so again every two minutes
        if (urgent && S.attnReady && Date.now() - (S.permSince.get(k) || 0) >= 120000) { S.permSince.set(k, Date.now()); notifyUser('Still waiting for your approval', t.replace(/^.* asks to use /, 'Use ') + ' (refused if unanswered for ten minutes)', href, true); }
        continue;
      }
      S.attnSeen.add(k);
      if (urgent) S.permSince.set(k, Date.now());
      if (S.attnReady) notifyUser(t, b, href, urgent);
    }
    for (const k of [...S.attnSeen]) if (!keys.has(k)) { S.attnSeen.delete(k); S.permSince.delete(k); }
    if (![...keys.keys()].some((k) => k.startsWith('perm:'))) for (const t of document.querySelectorAll('#toasts .toast.urgent')) t.remove();
    maybeNudge(keys.size);
  }
  function toastAction(msg, label, fn) {
    const t = h('div', { class: 'toast' }, msg + ' ', h('button', { class: 'btn small', onclick: () => { fn(); t.remove(); } }, label));
    $('toasts').append(t);
    setTimeout(() => t.remove(), 12000);
  }
  function maybeNudge(count) {
    if (!S.attnReady || !count) return;
    let done = false;
    try { done = !!localStorage.getItem('cully-notify-nudged') || !!localStorage.getItem(NOTIFY_KEY); } catch (_) { done = true; }
    if (done) return;
    try { localStorage.setItem('cully-notify-nudged', '1'); } catch (_) { return; }
    toastAction('Get a heads-up when a task needs you?', 'Notification settings', () => $('notifybtn').click());
  }
  // The pause switch: stops Cully starting anything new on its own. Work that is running carries on.
  async function mountPause() {
    const btn = $('pausebtn');
    const draw = (on) => {
      btn.classList.toggle('on', on);
      btn.setAttribute('aria-pressed', on ? 'true' : 'false');
      btn.replaceChildren(icon(on ? 'alert' : 'check'), h('span', {}, on ? 'Paused' : 'Pause new work'));
      btn.title = on ? 'New work is paused: queued tasks, scheduled agents and jobs, and automatic scouts will not start. Running tasks carry on. Click to resume.' : 'Stop Cully from starting anything new on its own. Tasks already running are not touched.';
    };
    btn.addEventListener('click', async () => {
      try { const r = await api('/api/pause', { method: 'POST', body: JSON.stringify({ paused: !btn.classList.contains('on') }) }); draw(r.paused); } catch (e) { toast(e.message, true); }
    });
    try { draw((await api('/api/pause')).paused); } catch (_) { draw(false); }
    btn.hidden = false;
  }
  function mountNotifyMenu() {
    const btn = $('notifybtn');
    btn.replaceChildren(icon('bell'));
    let menu = null;
    const outside = (e) => { if (menu && !menu.contains(e.target) && !btn.contains(e.target)) close(); };
    function close() { if (menu) { menu.remove(); menu = null; document.removeEventListener('click', outside, true); } }
    function draw() {
      const p = notifyPrefs();
      const problem = desktopProblem();
      const row = (key, name, desc, disabled) => h('button', { role: 'menuitemcheckbox', 'aria-checked': p[key] ? 'true' : 'false', class: p[key] ? 'on' : '', disabled: disabled || undefined, onclick: async () => {
        if (key === 'desktop' && !p.desktop && Notification.permission === 'default') {
          const perm = await Notification.requestPermission();
          if (perm !== 'granted') { toast('Desktop notifications were not allowed', true); draw(); return; }
        }
        saveNotifyPrefs({ ...p, [key]: !p[key] });
        if (key === 'sound' && !p.sound) beep();
        draw();
      } }, icon(p[key] ? 'check' : 'bell'), h('span', {}, h('b', {}, name), h('small', {}, desc)));
      const el = h('div', { class: 'thememenu notifymenu', role: 'menu' },
        h('div', { class: 'menu-note' }, 'Get told when a task needs you, advice arrives, or sign-in is needed.'),
        row('approvals', 'Alert for approval requests', 'A tone and a message that stays until you answer, even with this tab open'),
        row('flash', 'Flash the tab title', 'Works everywhere'),
        row('sound', 'Play a sound', 'A short tone'),
        row('desktop', 'Desktop notifications', problem ? 'Not available here' : 'Alerts when this tab is in the background', !!problem && Notification && Notification.permission !== 'default'),
        problem ? h('div', { class: 'menu-note faint' }, problem) : null);
      if (menu) menu.replaceWith(el);
      menu = el;
      if (!el.isConnected) $('notifywrap').append(el);
    }
    btn.addEventListener('click', () => { if (menu) { close(); return; } draw(); document.addEventListener('click', outside, true); });
    document.addEventListener('keydown', (e) => { if (e.key === 'Escape') close(); });
  }

  // ---------- things you can do here as well as in the terminal ----------
  function confirmDialog(opts) {
    const err = h('div', { class: 'err', hidden: true });
    const go = h('button', { class: 'btn', onclick: async () => {
      go.disabled = true; err.hidden = true;
      try { await opts.onConfirm(); closeMCP(); } catch (e) { err.textContent = e.message; err.hidden = false; go.disabled = false; }
    } }, opts.confirmLabel);
    const modal = h('div', { class: 'modal', role: 'dialog', 'aria-modal': 'true', 'aria-label': opts.title },
      h('header', {}, h('h2', {}, opts.title), h('button', { class: 'btn ghost small sp', onclick: closeMCP }, 'Cancel')),
      h('div', { class: 'mbody' }, ...opts.body.map((t) => h('p', {}, t)), err, h('div', { class: 'row' }, go)));
    const bg = h('div', { class: 'modal-bg', onclick: (e) => { if (e.target === bg) closeMCP(); } }, modal);
    $('modal-root').replaceChildren(bg);
    S.modal = { agent: 'confirm' };
    document.removeEventListener('keydown', escClose);
    document.addEventListener('keydown', escClose);
  }
  function enableAgentDialog(agent) {
    const name = AGENT_NAMES[agent];
    confirmDialog({
      title: 'Set up ' + name + ' for Cully',
      body: ['Cully will add its session-tracking entries and its skill to ' + name + "'s user settings, and leave your other settings as they are.", 'Open ' + name + ' sessions need a restart to pick this up.'],
      confirmLabel: 'Set up ' + name,
      onConfirm: async () => { const r = await api('/api/setup/' + agent + '/enable', { method: 'POST' }); toast(r.message); S.dashSig = ''; await refreshMCP(); if (S.route.name === 'dashboard') drawPanel(S.route.panel, true); },
    });
  }
  async function startAdvisorNow() {
    try { const r = await api('/api/setup/advisor/start', { method: 'POST' }); toast(r.message); await refresh(); } catch (e) { toast(e.message, true); }
  }
  function connectMCPDialog(agent) {
    const input = h('input', { type: 'text', placeholder: 'https://your-cully-server/mcp', 'aria-label': 'Cully MCP address', autocomplete: 'off', spellcheck: 'false' });
    const err = h('div', { class: 'err', hidden: true });
    const go = async () => {
      err.hidden = true;
      try {
        await api('/api/mcp/' + agent + '/connect', { method: 'POST', body: JSON.stringify({ url: input.value }) });
        closeMCP(); toast('Connected ' + AGENT_NAMES[agent] + ' to the Cully MCP. Sign in next.');
        await refreshMCP(); openMCP(agent);
      } catch (e) { err.textContent = e.message; err.hidden = false; }
    };
    input.addEventListener('keydown', (e) => { if (e.key === 'Enter') go(); });
    const modal = h('div', { class: 'modal', role: 'dialog', 'aria-modal': 'true', 'aria-label': 'Connect the Cully MCP' },
      h('header', {}, h('h2', {}, 'Connect the Cully MCP'), h('button', { class: 'btn ghost small sp', onclick: closeMCP }, 'Close')),
      h('div', { class: 'mbody' }, h('p', {}, 'Paste the address of your Cully MCP server. Cully adds it to ' + AGENT_NAMES[agent] + "'s settings and leaves the rest as it is. You sign in afterwards."), input, err, h('div', { class: 'row' }, h('button', { class: 'btn', onclick: go }, icon('lock'), 'Connect'))));
    const bg = h('div', { class: 'modal-bg', onclick: (e) => { if (e.target === bg) closeMCP(); } }, modal);
    $('modal-root').replaceChildren(bg);
    S.modal = { agent: 'connect' };
    document.removeEventListener('keydown', escClose);
    document.addEventListener('keydown', escClose);
    input.focus();
  }

  // ---------- where: the machine and the full folder path ----------
  // The bar under the header names only the machine. Tasks pick their own folder, so no folder is shown or chosen here.
  function drawWhere() {
    const box = $('where');
    if (!S.info || !S.info.host) { box.hidden = true; return; }
    const who = (S.info.user ? S.info.user + '@' : '') + S.info.host;
    box.hidden = false;
    box.replaceChildren(h('span', { class: 'w-item', title: 'The machine this page is working on' }, icon('term'), h('b', {}, who)));
  }
  // Opened through one agent, the page is about that agent only.
  const claudeInUse = () => !(S.mcp || []).length || mcpUsed().some((m) => m.agent === 'claude');
  function drawAuth() { $('meta').hidden = !claudeInUse(); }


  // ---- Improve Cully: where Cully itself gets better. Ideas it has, the rules it keeps for every harness,
  // the skills your harnesses load, and the MCP servers worth adding. ----
  const IMPROVE_TABS = [['ideas', 'Ideas', 'What Cully suggests from your sessions'], ['rules', 'Rules', 'How you work, for every harness'], ['skills', 'Skills', 'What each harness can already do'], ['mcp', 'MCP servers', 'Tools worth adding']];
  // Servers many developers add. Cully shows the command; you run it, so nothing is installed without you.
  const MCP_CATALOG = [
    { name: 'Context7', why: 'Current library documentation in the prompt, so the agent stops guessing at APIs.', claude: 'claude mcp add --transport http context7 https://mcp.context7.com/mcp', codex: 'codex mcp add context7 --url https://mcp.context7.com/mcp' },
    { name: 'Playwright', why: 'Lets the agent open pages and click through them, to check a UI change itself.', claude: 'claude mcp add playwright -- npx @playwright/mcp@latest', codex: 'codex mcp add playwright -- npx @playwright/mcp@latest' },
    { name: 'GitHub', why: 'Read issues and pull requests and comment on them without leaving the task (needs a token).', claude: 'claude mcp add --transport http github https://api.githubcopilot.com/mcp/', codex: '' },
  ];
  async function renderImprove() {
    const box = $('improve');
    const tab = IMPROVE_TABS.some((t) => t[0] === S.route.tab) ? S.route.tab : 'ideas';
    const seg = h('div', { class: 'seg', role: 'tablist', 'aria-label': 'Improve Cully' }, ...IMPROVE_TABS.map(([k, l]) => h('button', { type: 'button', role: 'tab', class: k === tab ? 'on' : '', 'aria-selected': String(k === tab), onclick: () => { location.hash = '#/improve/' + k; } }, l)));
    const sub = IMPROVE_TABS.find((t) => t[0] === tab)[2];
    const body = h('div', { class: 'improvebody' }, loadingCard());
    box.replaceChildren(toolbar('Improve Cully', 'Make Cully, and the way you work with your agents, better over time.'), seg, h('p', { class: 'faint small' }, sub), body);
    const live = () => S.route.name === 'improve' && (S.route.tab || 'ideas') === tab;
    const again = () => renderImprove();
    try {
      if (tab === 'ideas') { const a = await adviceBoard(again); if (live()) body.replaceChildren(...[a].filter(Boolean)); }
      else if (tab === 'rules') { await renderRules(body, (...x) => h('div', { class: 'row' }, ...x)); }
      else if (tab === 'skills') {
        const infos = await Promise.all(['claude', 'codex', 'cursor'].map((a) => api('/api/harness?agent=' + a).catch(() => null)));
        if (!live()) return;
        const cards = ['claude', 'codex', 'cursor'].map((a, i) => { const info = infos[i]; if (!info || !info.installed) return null; const skills = info.skills || [];
          return h('div', { class: 'card' }, h('div', { class: 'row' }, harnessChip(a), h('b', {}, skills.length + (skills.length === 1 ? ' skill' : ' skills')), h('span', { class: 'sp' }), h('a', { class: 'linkbtn', href: '#/harness/skills' }, 'Open in Harness')),
            skills.length ? h('ul', { class: 'files' }, ...skills.slice(0, 12).map((k) => h('li', {}, h('b', {}, k.name), k.description ? h('span', { class: 'faint small' }, ' ' + uiClipText(k.description, 120)) : null))) : h('p', { class: 'faint small' }, 'No skills installed yet.'),
            skills.length > 12 ? h('p', { class: 'faint small' }, 'and ' + (skills.length - 12) + ' more in Harness') : null); }).filter(Boolean);
        body.replaceChildren(...cards, h('p', { class: 'faint small' }, 'Drafting a new skill from your repeated work is not built yet; today Cully drafts agents, jobs and workflow rules (see Ideas).'));
      } else if (tab === 'mcp') {
        const infos = await Promise.all(['claude', 'codex'].map((a) => api('/api/harness?agent=' + a).catch(() => null)));
        if (!live()) return;
        const have = new Set(infos.flatMap((i) => ((i && i.mcp) || []).map((m) => String(m.name || '').toLowerCase())));
        const card = (m) => { const on = have.has(m.name.toLowerCase());
          const cmd = (label, text) => (text ? h('div', { class: 'row' }, h('span', { class: 'faint small' }, label), h('code', { class: 'ic' }, text), h('button', { class: 'btn ghost small', onclick: async () => { try { await writeClip(text); toast('Copied. Run it in a terminal.'); } catch (_) { showCommand(text); } } }, icon('copy'), 'Copy')) : null);
          return h('div', { class: 'card' }, h('div', { class: 'row' }, h('b', {}, m.name), on ? h('span', { class: 'chip ok' }, 'Already added') : null), h('p', { class: 'faint small' }, m.why), on ? null : cmd('Claude', m.claude), on ? null : cmd('Codex', m.codex)); };
        body.replaceChildren(...MCP_CATALOG.map(card), h('p', { class: 'faint small' }, 'Cully shows the command and never runs it. Check what a server can do before you add it. Servers you already have are in ', h('a', { class: 'ext', href: '#/harness/servers' }, 'Harness'), '.'));
      }
    } catch (e) { if (live()) body.replaceChildren(h('div', { class: 'emptycard' }, h('h3', {}, 'Could not load this'), h('p', {}, e.message))); }
  }

  // ---------- folder switcher ----------
  function drawFolderChip() {
    const btn = $('folderbtn');
    if (!btn) { drawWhere(); return; } // the folder lives in the bar below the header, not in the navigation
    const list = (S.info && S.info.repoInfo) || [];
    const cur = list.find((r) => r.path === S.opts.repo) || list[0];
    btn.replaceChildren(icon('folder'), h('span', { class: 'lbl' }, cur ? cur.name : 'Add a folder'));
    btn.title = cur ? 'Folder shown: ' + cur.path : 'Add a git folder';
    drawWhere();
  }
  function mountFolderMenu() {
    const btn = $('folderbtn');
    if (!btn) return;
    let menu = null;
    const outside = (e) => { if (menu && !menu.contains(e.target) && !btn.contains(e.target)) close(); };
    function close() { if (menu) { menu.remove(); menu = null; document.removeEventListener('click', outside, true); } }
    btn.addEventListener('click', () => {
      if (menu) { close(); return; }
      const list = (S.info && S.info.repoInfo) || [];
      menu = h('div', { class: 'thememenu foldermenu', role: 'menu' },
        ...list.map((r) => h('button', { class: r.path === S.opts.repo ? 'on' : '', role: 'menuitemradio', onclick: () => { S.opts.repo = r.path; close(); drawFolderChip(); route(); } },
          icon('folder'), h('span', {}, h('b', {}, r.name), h('small', {}, r.remote || r.path)))),
        h('button', { onclick: () => { close(); openAddFolder(); } }, icon('plus'), h('span', {}, h('b', {}, 'Add a folder'), h('small', {}, 'Any git repository on this machine'))));
      $('folderwrap').append(menu);
      document.addEventListener('click', outside, true);
    });
    document.addEventListener('keydown', (e) => { if (e.key === 'Escape') close(); });
  }

  // ---------- themes ----------
  const THEMES = [
    ['cully', 'Cully', 'Forest and cream. Follows your system light or dark.', ['#174c38', '#f6f4ec', '#eeb96e']],
    ['harbor', 'Harbor', 'Cool slate and teal, light.', ['#1d6a86', '#eef2f5', '#e2733f']],
    ['graphite', 'Graphite', 'Neutral dark with an ice-blue accent.', ['#2d6a9f', '#121417', '#f0b35a']],
  ];
  const currentTheme = () => document.documentElement.getAttribute('data-theme') || 'cully';
  function setTheme(name) {
    if (name === 'cully') document.documentElement.removeAttribute('data-theme'); else document.documentElement.setAttribute('data-theme', name);
    try { if (name === 'cully') localStorage.removeItem('cully-theme'); else localStorage.setItem('cully-theme', name); } catch (_) { /* storage blocked */ }
  }
  function mountThemeMenu() {
    const btn = $('themebtn');
    btn.replaceChildren(icon('palette'));
    let menu = null;
    const outside = (e) => { if (menu && !menu.contains(e.target) && !btn.contains(e.target)) close(); };
    function close() { if (menu) { menu.remove(); menu = null; document.removeEventListener('click', outside, true); } }
    btn.addEventListener('click', () => {
      if (menu) { close(); return; }
      menu = h('div', { class: 'thememenu', role: 'menu' }, ...THEMES.map(([id, name, desc, cols]) => {
        const sw = h('span', { class: 'swatch' }, ...cols.map((c) => { const i = h('i'); i.style.background = c; return i; }));
        const on = currentTheme() === id;
        return h('button', { class: on ? 'on' : '', role: 'menuitemradio', 'aria-checked': on ? 'true' : 'false', onclick: () => { setTheme(id); close(); } },
          sw, h('span', {}, h('b', {}, name), h('small', {}, desc)));
      }));
      $('themewrap').append(menu);
      document.addEventListener('click', outside, true);
    });
    document.addEventListener('keydown', (e) => { if (e.key === 'Escape') close(); });
  }

  // ---------- polling ----------
  let lastStates = '';
  async function refresh() {
    try { S.agents = await api('/api/agents'); } catch (_) { return; }
    try { S.advice = await api('/api/advice'); } catch (_) { /* older server */ }
    try { S.sessions = await api('/api/sessions'); } catch (_) { /* older server */ }
    if (!S.rulesAt || Date.now() - S.rulesAt > 60000) { S.rulesAt = Date.now(); try { S.rules = await api('/api/rules'); } catch (_) { /* older server */ } }
    const mineLive = (S.sessions || []).filter((x) => !x.detached).length;
    $('sbadge').hidden = mineLive === 0;
    $('sbadge').textContent = mineLive;
    if (S.route.name === 'sessions') renderSessions();
    const unread = (S.advice || []).reduce((n, g) => n + g.advice.filter((x) => !x.applied && !x.dismissed).length, 0) + (S.rules && S.rules.proposal ? 1 : 0);
    $('dbadge').hidden = unread === 0;
    $('dbadge').textContent = unread;
    { const mb = $('morebadge'); if (mb) { mb.hidden = unread === 0; mb.textContent = unread; } }
    renderRail();
    checkAttention();
    if (S.route.name === 'dashboard' && (S.route.panel === 'advisor' || S.route.panel === 'scout')) drawPanel(S.route.panel, false);
    if (S.route.name === 'dashboard' && S.route.panel === 'overview') renderNeeds();
    if (S.route.name === 'home') renderFleet();
    if (S.route.name === 'board') renderBoard();
    if (S.route.name === 'agent') {
      const a = S.agents.find((x) => x.id === S.route.id);
      if (a) {
        drawAgentChrome(a);
        const sig = a.id + a.state + a.events;
        if (sig !== lastStates && (S.tab !== 'chat' || window.matchMedia('(min-width:1180px)').matches)) loadSide(a, false);
        lastStates = sig;
      } else if (!$('stage').querySelector('.empty')) route();
    }
  }

  async function boot() {
    mountThemeMenu();
    mountNotifyMenu();
    mountPause();
    mountFolderMenu();
    setTimeout(() => { S.attnReady = true; }, 4000);
    try { S.info = await api('/api/info'); } catch (_) { return; }
    const auth = S.info.auth || { mode: 'login', accounts: 0 };
    S.authInfo = auth;
    $('meta').textContent = auth.mode === 'subscription'
      ? 'Claude subscription, ' + auth.accounts + (auth.accounts === 1 ? ' account' : ' accounts')
      : 'Claude login on this machine';
    $('meta').title = auth.mode === 'subscription'
      ? 'Agents started here use ' + auth.accounts + ' Claude subscription token' + (auth.accounts === 1 ? '' : 's') + ' in turn. Token values are never shown.'
      : "Agents started here use this machine's own Claude login. Add a tokens file to rotate between subscription accounts.";
    S.opts.repo = S.info.repos[0] || '';
    drawFolderChip();
    setInterval(async () => { try { S.info = await api('/api/info'); drawFolderChip(); } catch (_) { /* banner */ } }, 30000);
    try { S.agents = await api('/api/agents'); } catch (_) { /* banner */ }
    route();
    if (S.route.mcp) { await refreshMCP(); openMCP(S.route.mcp); }
    setInterval(refresh, 2000);
    refreshMCP();
    setInterval(refreshMCP, 25000);
  }
  boot();
})();
