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
  const stateLabel = (s) => (s === 'waiting' ? 'needs you' : s);
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
      try { msg = (await res.json()).error || msg; } catch (_) { /* not JSON */ }
      throw new Error(msg);
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
    if (p[0] === 'agent' && p[1]) return { name: 'agent', id: p[1] };
    if (p[0] === 'mcp') return { name: 'home', mcp: p[1] || 'claude' };
    if (p[0] === 'sessions') return { name: 'sessions' };
    if (p[0] === 'board') return { name: 'board' };
    if (p[0] === 'agents') return { name: 'team' };
    if (p[0] === 'dashboard') return { name: 'dashboard', panel: p[1] || 'overview' };
    return { name: 'home' };
  }
  const REDIRECT = { status: 'setup', timeline: 'activity', replay: 'activity', handoff: 'activity', suggestions: 'advisor', impact: 'savings', rescue: 'overview' };
  function route() {
    S.route = parseHash();
    if (S.route.name === 'dashboard' && REDIRECT[S.route.panel]) { location.replace('#/dashboard/' + REDIRECT[S.route.panel]); return; }
    const section = S.route.name === 'team' ? 'team' : S.route.name === 'board' ? 'board' : S.route.name === 'dashboard' ? 'dashboard' : S.route.name === 'sessions' ? 'sessions' : 'agents';
    for (const a of $('nav').querySelectorAll('a')) a.classList.toggle('active', a.dataset.route === section);
    for (const s of ['agents', 'board', 'dashboard', 'sessions', 'team']) $('view-' + s).hidden = s !== section;
    if (S.es) { S.es.close(); S.es = null; }
    S.feed = null;
    $('stage').className = 'stage';
    renderRail();
    if (S.route.name === 'agent') mountAgent(S.route.id);
    else if (S.route.name === 'home') mountHome();
    else if (S.route.name === 'board') renderBoard();
    else if (S.route.name === 'team') renderTeam();
    else if (S.route.name === 'sessions') { S.sessSig = ''; renderSessions(); }
    else mountDashboard(S.route.panel);
  }
  window.addEventListener('hashchange', route);

  // ---------- rail ----------
  function dot(state) { return h('span', { class: 'dot ' + state }); }
  function renderRail() {
    const rail = $('rail');
    const kids = [];
    for (const [key, label] of GROUPS) {
      const list = S.agents.filter((a) => groupOf(a) === key);
      if (!list.length) continue;
      kids.push(h('div', { class: 'grp' + (key === 'waiting' ? ' attn' : '') }, label, h('span', { class: 'n' }, list.length)));
      for (const a of list) {
        kids.push(h('a', { class: 'item' + (a.state === 'waiting' ? ' attn' : '') + (S.route.name === 'agent' && S.route.id === a.id ? ' sel' : ''), href: '#/agent/' + a.id },
          h('div', { class: 'r1' }, dot(a.state), h('span', { class: 't' }, a.title)),
          h('div', { class: 'r2' }, a.last || (a.state === 'preparing' ? 'Preparing…' : '')),
          h('div', { class: 'r3' }, a.token ? h('span', { class: 'chip' }, 'token ' + a.token) : null,
            a.files && a.files.length ? h('span', { class: 'chip' }, a.files.length + (a.files.length === 1 ? ' file' : ' files')) : null,
            h('span', { class: 'chip' }, ago(a.updated || a.created)))));
      }
    }
    if (!kids.length) kids.push(h('div', { class: 'hint', style: null }, 'No tasks yet.'));
    rail.replaceChildren(...kids);
    const waiting = S.agents.filter((a) => a.state === 'waiting').length;
    $('badge').hidden = waiting === 0;
    $('badge').textContent = waiting;
    document.title = (waiting ? '(' + waiting + ') ' : '') + 'Cully';
  }

  // ---------- home ----------
  function toggle(key, label, title) {
    const input = h('input', { type: 'checkbox', ...(S.opts[key] ? { checked: true } : {}) });
    const lab = h('label', { class: 'toggle' + (S.opts[key] ? ' on' : ''), title }, input, h('span', { class: 'tick' }, icon('check')), label);
    input.addEventListener('change', () => { S.opts[key] = input.checked; lab.classList.toggle('on', input.checked); });
    return lab;
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
    const ta = h('textarea', { id: 'prompt', placeholder: 'Describe the task. Cully gathers context, then starts it in its own worktree.', 'aria-label': 'Task' });
    ta.value = S.draft;
    ta.addEventListener('input', () => { S.draft = ta.value; });
    const err = h('div', { class: 'err', hidden: true });
    const start = h('button', { class: 'btn', type: 'submit' }, 'Start task', icon('send'));
    const later = h('button', { class: 'btn ghost', type: 'button', title: 'Hold this task until a slot is free' }, 'Queue for later');
    const segBtn = (mode, label) => {
      const b = h('button', { type: 'button', class: S.opts.mode === mode ? 'on' : '' }, label);
      b.addEventListener('click', () => { S.opts.mode = mode; for (const x of seg.children) x.classList.toggle('on', x === b); });
      return b;
    };
    const seg = h('div', { class: 'seg', role: 'group', 'aria-label': 'Mode' });
    seg.append(segBtn('edit', 'Edit'), segBtn('plan', 'Plan only'));
    const repo = S.info && S.info.repos.length > 1 ? h('select', { 'aria-label': 'Repository' }, ...S.info.repos.map((r) => h('option', { value: r, ...(r === S.opts.repo ? { selected: true } : {}) }, r.split('/').pop()))) : null;
    if (repo) repo.addEventListener('change', () => { S.opts.repo = repo.value; });
    const doneIn = h('textarea', { rows: '2', 'aria-label': 'Done when', placeholder: 'For example: the login test passes\nNo new lint errors' });
    doneIn.value = S.opts.doneWhen || '';
    doneIn.addEventListener('input', () => { S.opts.doneWhen = doneIn.value; });
    const loopBox = h('div', { class: 'loopbox', hidden: !S.opts.loop },
      h('span', { class: 'opt-label' }, 'Done when'), doneIn,
      h('p', { class: 'faint small' }, 'After each turn Cully makes sure the agent ran tests, steers it if it repeats the same call, and checks its work against these lines. It stops after 3 rounds.'));
    const loopToggle = toggle('loop', 'Keep going until checked', 'Cully verifies the work, steers the agent when it loops, and judges it against your checks');
    loopToggle.querySelector('input').addEventListener('change', () => { loopBox.hidden = !S.opts.loop; });
    const form = h('form', { class: 'composer-card' }, ta, loopBox,
      h('div', { class: 'row end' },
        h('div', { class: 'row' }, h('span', { class: 'opt-label' }, 'Include'), toggle('context', 'Context', "Start from this project's latest journal and git state"),
          toggle('recall', 'Recall', 'Ask Cully memory what is relevant (needs the Cully MCP signed in)'),
          toggle('scout', 'Scout', 'Search the web for current practice and pitfalls'), loopToggle, seg, repo),
        h('div', { class: 'row' }, later, start)), err);
    later.addEventListener('click', async () => {
      err.hidden = true;
      try {
        await api('/api/queue', { method: 'POST', body: JSON.stringify({ prompt: ta.value, repo: S.opts.repo || (repo ? repo.value : ''), mode: S.opts.mode, context: S.opts.context, recall: S.opts.recall, scout: S.opts.scout }) });
        S.draft = ''; ta.value = ''; toast('Queued. It starts when a slot is free.'); location.hash = '#/board';
      } catch (x) { err.textContent = x.message; err.hidden = false; }
    });
    ta.addEventListener('keydown', (e) => { if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) form.requestSubmit(); });
    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      err.hidden = true; start.disabled = true;
      try {
        const body = { prompt: ta.value, repo: S.opts.repo || (repo ? repo.value : ''), mode: S.opts.mode, context: S.opts.context, recall: S.opts.recall, scout: S.opts.scout, loop: !!S.opts.loop, doneWhen: S.opts.loop ? (S.opts.doneWhen || '') : '' };
        const view = await api('/api/agents', { method: 'POST', body: JSON.stringify(body) });
        S.draft = '';
        await refresh();
        location.hash = '#/agent/' + view.id;
      } catch (x) { err.textContent = x.message; err.hidden = false; } finally { start.disabled = false; }
    });
    const ri = (S.info.repoInfo || []).find((r) => r.path === (S.opts.repo || S.info.repos[0])) || (S.info.repoInfo || [])[0];
    const repoLine = h('div', { class: 'repo-line' }, ...(ri ? [icon('branch'), 'Agents work in ', h('b', {}, ri.name), ri.remote ? h('span', { class: 'faint' }, ' (' + ri.remote + ')') : null] : ['No folder yet.']),
      h('button', { class: 'linkbtn', onclick: () => openAddFolder() }, ri ? 'Use another folder' : 'Add a git folder to start agents'));
    const fleet = h('div', { id: 'fleet' });
    $('stage').className = 'stage home-bg';
    $('stage').replaceChildren(h('div', { class: 'home' },
      h('h1', {}, 'What should Cully work on?'),
      h('p', { class: 'sub' }, 'Give a task. Cully prepares context, starts an agent on the next token, and you can keep talking to it. ⌘/Ctrl+Enter to start.'),
      h('div', { id: 'mcpbanner' }), form, repoLine, fleet));
    renderFleet();
    drawMCPBanner();
    ta.focus();
  }
  function agentCard(a) {
    return h('a', { class: 'card' + (a.state === 'waiting' ? ' attn' : ''), href: '#/agent/' + a.id },
      h('div', { class: 'r1' }, dot(a.state), h('span', { class: 't' }, a.title)),
      h('div', { class: 'last' }, a.last || (a.state === 'preparing' ? 'Preparing…' : '')),
      h('div', { class: 'row' }, h('span', { class: 'pill ' + a.state }, stateLabel(a.state)),
        a.token ? h('span', { class: 'chip' }, 'token ' + a.token) : null, h('span', { class: 'chip' }, ago(a.updated || a.created))));
  }
  function renderFleet() {
    const box = $('fleet');
    if (!box) return;
    const live = S.agents.filter((a) => groupOf(a) !== 'finished');
    if (live.length) {
      box.replaceChildren(h('div', { class: 'section-h' }, 'Running now'), h('div', { class: 'cards' }, ...live.map(agentCard)));
    } else {
      box.replaceChildren(h('div', { class: 'section-h' }, 'What happens when you start'), h('ol', { class: 'how' },
        h('li', {}, h('b', {}, 'Context'), "The project's latest journal and live git state go to the agent first."),
        h('li', {}, h('b', {}, 'Recall and scout'), 'Optionally pull memory from Cully and fresh notes from the web.'),
        h('li', {}, h('b', {}, 'Worktree'), 'Each task works on its own branch, so nothing touches your checkout.'),
        h('li', {}, h('b', {}, 'Conversation'), 'Send follow-ups while it works, review the diff, then continue elsewhere.')));
    }
  }

  // ---------- agent view ----------
  function mountAgent(id) {
    const a = S.agents.find((x) => x.id === id);
    if (!a) { $('stage').replaceChildren(h('div', { class: 'empty' }, S.agents.length ? 'That agent no longer exists.' : 'Loading…')); return; }
    S.tab = S.tab === 'chat' || S.tab === 'changes' || S.tab === 'handoff' ? S.tab : 'chat';
    const ahead = h('div', { class: 'ahead', id: 'ahead' });
    const sub = h('div', { class: 'sub2', id: 'sub2' });
    const stepper = h('div', { class: 'stepper', id: 'stepper' });
    const tabs = h('div', { class: 'tabs narrow', id: 'tabs' });
    const body = h('div', { class: 'feed', id: 'feed' });
    const jump = h('button', { class: 'btn ghost small jump', hidden: true, onclick: () => { body.scrollTop = body.scrollHeight; } }, icon('down'), 'Latest');
    const quick = h('div', { class: 'quick' }, ...['Continue', 'Run the tests', 'Summarize what you changed', 'Commit your work'].map((t) =>
      h('button', { type: 'button', onclick: () => sendMessage(a.id, t) }, t)));
    const ta = h('textarea', { rows: '1', placeholder: 'Message this task…', 'aria-label': 'Message' });
    ta.addEventListener('input', () => { ta.style.height = 'auto'; ta.style.height = Math.min(ta.scrollHeight, 160) + 'px'; ta.style.overflowY = ta.scrollHeight > 160 ? 'auto' : 'hidden'; });
    ta.addEventListener('keydown', (e) => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); const t = ta.value; ta.value = ''; ta.style.height = 'auto'; sendMessage(a.id, t); } });
    const composer = h('div', { id: 'composer' }, h('div', { class: 'composer' }, h('div', { class: 'box' }, ta),
      h('button', { class: 'btn round', title: 'Send', 'aria-label': 'Send', onclick: () => { const t = ta.value; ta.value = ''; ta.style.height = 'auto'; sendMessage(a.id, t); } }, icon('send'))),
      h('div', { class: 'composer-hint' }, 'Enter sends, Shift+Enter adds a line'));
    const side = h('div', { class: 'col sidecol' }, h('div', { class: 'tabs side', id: 'tabs-side' }), h('div', { class: 'sidehead', id: 'sidehead' }), h('div', { class: 'sidebody', id: 'sidebody' }));
    const grid = h('div', { class: 'agrid', id: 'agrid', 'data-show': S.tab }, h('div', { class: 'col chatcol' }, body, quick, composer), side);
    $('stage').replaceChildren(ahead, sub, stepper, tabs, grid);
    body.after(jump);
    body.addEventListener('scroll', () => { jump.hidden = body.scrollTop + body.clientHeight >= body.scrollHeight - 60; });
    S.feed = { body, jump, group: null, tools: 0, typing: null, quick, composer, ta, loaded: false };
    setTimeout(() => { if (S.feed) S.feed.loaded = true; }, 900);
    drawAgentChrome(a);
    openFeed(a.id);
    loadSide(a, true);
  }
  function setTab(t) {
    S.tab = t;
    const a = S.agents.find((x) => x.id === S.route.id);
    $('agrid').dataset.show = t;
    if (a) { drawAgentChrome(a); loadSide(a, true); }
  }
  function drawAgentChrome(a) {
    if (!$('ahead')) return;
    const finished = a.state === 'done' || a.state === 'failed' || a.state === 'stopped';
    const stop = h('button', { class: 'btn danger small', onclick: (e) => {
      const b = e.currentTarget;
      if (b.dataset.armed) { api('/api/agents/' + a.id + '/stop', { method: 'POST' }).then(refresh).catch((x) => toast(x.message, true)); return; }
      b.dataset.armed = '1'; b.replaceChildren(icon('stop'), 'Click again to stop'); setTimeout(() => { b.dataset.armed = ''; b.replaceChildren(icon('stop'), 'Stop'); }, 3000);
    } }, icon('stop'), 'Stop');
    $('ahead').replaceChildren(h('h2', {}, a.title), h('span', { class: 'pill ' + a.state }, dot(a.state), stateLabel(a.state)),
      h('span', { class: 'sp' },
        h('button', { class: 'btn ghost small', title: "Ask the advisor to review this agent's work", onclick: () => askAdvisor(a) }, icon('spark'), 'Ask advisor'),
        h('button', { class: 'btn ghost small', title: 'Start a read-only review of this change', onclick: () => startReview(a) }, icon('eye'), 'Review'),
        finished ? null : h('button', { class: 'btn ghost small', title: "Send this agent's reply or changes to another agent", onclick: () => openRelay(a) }, icon('send'), 'Send to task'),
        h('button', { class: 'btn ghost small', onclick: () => continueFrom(a) }, icon('plus'), 'Continue in a new task'),
        finished ? null : stop));
    $('sub2').replaceChildren(
      h('button', { class: 'chip mono', title: 'Copy branch name', onclick: () => copy(a.branch, 'branch') }, icon('branch'), a.branch),
      a.token ? h('span', { class: 'chip' }, 'token ' + a.token) : h('span', { class: 'chip' }, 'ambient login'),
      h('span', { class: 'chip' }, 'started ' + ago(a.created)), h('span', { class: 'chip' }, 'active ' + ago(a.updated || a.created)),
      a.usage ? h('span', { class: 'chip', title: 'Tokens this task has handled. Input served from the prompt cache costs about a tenth of fresh input, so a high share means tokens saved. About ' + fmtTokens(a.usage.saved) + ' tokens of cost saved so far.' }, icon('spark'), fmtTokens(a.usage.tokens) + ' tokens, ' + a.usage.cachedShare + '% from cache') : null,
      a.harness ? h('span', { class: 'chip ' + (a.harness.status === 'done' ? 'ok' : a.harness.status === 'stopped' ? 'warn' : ''), title: 'Cully is checking this task\'s work' }, icon('check'), 'Checking: round ' + a.harness.round + ' of ' + a.harness.max + ', ' + ({ working: 'working', verifying: 'asking it to verify', steering: 'steering it', judging: 'judging the work', done: 'checks done', stopped: 'stopped' }[a.harness.status] || a.harness.status)) : null,
      a.files && a.files.length ? h('span', { class: 'chip' }, a.files.length + (a.files.length === 1 ? ' file changed' : ' files changed')) : null,
      openAdviceFor(a.id) ? h('button', { class: 'chip warn', title: 'See and send the advisor\'s advice', onclick: () => { location.hash = '#/dashboard/advisor'; } }, icon('spark'), openAdviceFor(a.id) + (openAdviceFor(a.id) === 1 ? ' piece of advice' : ' pieces of advice')) : null,
      (a.mcp || []).some((m) => m.name === 'cully' && needsAuthStatus(m.status)) ? h('button', { class: 'chip warn', title: 'This task started without Cully MCP access', onclick: () => openMCP('claude') }, icon('alert'), 'Cully MCP needs sign-in') : null);
    $('stepper').replaceChildren(...(a.stages || []).map((s) => {
      const label = (({ ok: '✓ ', failed: '✗ ', skipped: '– ', running: '● ' })[s.status] || '') + s.name;
      const look = s.name === 'scout' || s.name === 'recall';
      return look && s.status !== 'skipped'
        ? h('button', { class: 'step ' + s.status, title: 'See what the ' + s.name + ' searched and found', onclick: () => { location.hash = '#/dashboard/scout'; } }, label)
        : h('span', { class: 'step ' + s.status, title: s.detail || '' }, label);
    }));
    const tabLabel = (k, label) => label + (k === 'changes' && a.files && a.files.length ? ' (' + a.files.length + ')' : '');
    $('tabs').replaceChildren(...[['chat', 'Chat'], ['changes', 'Changes'], ['handoff', 'Summary']].map(([k, label]) =>
      h('button', { class: S.tab === k ? 'on' : '', onclick: () => setTab(k) }, tabLabel(k, label))));
    const sideKind = S.tab === 'handoff' ? 'handoff' : 'changes';
    $('tabs-side').replaceChildren(...[['changes', 'Changes'], ['handoff', 'Summary']].map(([k, label]) =>
      h('button', { class: sideKind === k ? 'on' : '', onclick: () => setTab(k) }, tabLabel(k, label))));
    if (S.feed) {
      S.feed.composer.hidden = finished;
      S.feed.quick.hidden = finished;
      setTyping(a.state === 'working' || a.state === 'preparing');
    }
  }
  async function sendMessage(id, text) {
    text = (text || '').trim();
    if (!text) return;
    try { await api('/api/agents/' + id + '/message', { method: 'POST', body: JSON.stringify({ text }) }); } catch (e) { toast(e.message, true); }
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
  function appendEvent(ev) {
    const f = S.feed;
    if (!f) return;
    const b = f.body;
    const stick = b.scrollTop + b.clientHeight >= b.scrollHeight - 60;
    if (f.typing) f.typing.remove();
    if (ev.kind === 'tool' || ev.kind === 'tool_result') addToolRow(ev);
    else {
      f.group = null;
      if (ev.kind === 'text') b.append(h('div', { class: 'msg bot', title: new Date(ev.t).toLocaleTimeString() }, markdown(ev.text)));
      else if (ev.kind === 'user') b.append(h('div', { class: 'msg you', title: new Date(ev.t).toLocaleTimeString() }, ev.text));
      else if (ev.kind === 'error') b.append(h('div', { class: 'msg error' }, ev.text));
      else if (ev.kind === 'result') b.append(h('div', { class: 'rule' }, ev.failed ? 'turn ended with an error' : 'turn done'));
      else if (ev.kind === 'note') b.append(h('div', { class: 'note-card' }, ev.text));
      else if (ev.kind === 'stage' && /failed/.test(ev.text)) b.append(h('div', { class: 'note' }, ev.text));
    }
    if (f.loaded && b.lastElementChild) b.lastElementChild.classList.add('fresh');
    const a = S.agents.find((x) => x.id === S.route.id);
    if (a && (a.state === 'working' || a.state === 'preparing')) setTyping(true);
    if (stick) b.scrollTop = b.scrollHeight;
  }
  function addToolRow(ev) {
    const f = S.feed;
    if (!f.group) {
      const rows = h('div', { class: 'rows' });
      const sum = h('summary', {});
      f.group = { el: h('details', { class: 'tools' }, sum, rows), rows, sum, names: {}, n: 0, bad: false };
      f.body.append(f.group.el);
    }
    const g = f.group;
    if (ev.kind === 'tool') {
      g.n += 1; g.names[ev.tool] = (g.names[ev.tool] || 0) + 1;
      g.rows.append(h('div', {}, h('b', {}, ev.tool), '  ', ev.text));
    } else {
      if (ev.failed) { g.bad = true; g.el.classList.add('bad'); g.el.open = true; }
      g.rows.append(h('div', { class: ev.failed ? 'f' : '' }, (ev.failed ? '✗ ' : '↳ ') + ((ev.text || 'ok').slice(0, 240))));
    }
    const parts = Object.entries(g.names).map(([k, v]) => k + (v > 1 ? ' ×' + v : '')).join(', ');
    g.sum.textContent = 'Ran ' + g.n + (g.n === 1 ? ' tool' : ' tools') + (parts ? ' (' + parts + ')' : '') + (g.bad ? ', with a failure' : '');
  }

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
  async function loadSide(a, force) {
    const body = $('sidebody'); const head = $('sidehead');
    if (!body) return;
    const kind = S.tab === 'handoff' ? 'handoff' : 'changes';
    const key = a.id + kind + (a.events || 0) + a.state;
    if (!force && key === S.diffKey) return;
    S.diffKey = key;
    const refreshBtn = h('button', { class: 'btn ghost small sp', onclick: () => loadSide(a, true) }, icon('refresh'), 'Refresh');
    head.replaceChildren(h('span', {}, kind === 'changes' ? 'Full change against the commit this task started from' : 'Where this task left off'), refreshBtn);
    try {
      if (kind === 'handoff') {
        const sum = await api('/api/agents/' + a.id + '/summary');
        body.replaceChildren(h('div', { class: 'toolbar slim' }, h('div', { class: 'actions' }, copyBtn('Copy for another agent', () => api('/api/agents/' + a.id + '/handoff')))), renderSummary(sum, { agent: a }));
        return;
      }
      const d = parseDiff(await api('/api/agents/' + a.id + '/diff'));
      if (!d.files.length && !d.head) { body.replaceChildren(h('div', { class: 'empty' }, 'No changes yet.')); return; }
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
      body.replaceChildren(...nodes);
    } catch (e) { body.replaceChildren(h('div', { class: 'err' }, e.message)); }
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
    if (a.state === 'waiting') actions.push(act('Reply', 'send', () => { location.hash = '#/agent/' + a.id; }, ''));
    if (files.length) actions.push(act('Review changes', 'eye', () => { S.tab = 'changes'; location.hash = '#/agent/' + a.id; }));
    if (!live) actions.push(act('Copy handoff', 'copy', async () => { try { await writeClip(await api('/api/agents/' + a.id + '/handoff')); toast('Handoff copied. Paste it into the session you are continuing.'); } catch (x) { toast(x.message, true); } }));
    if (live) actions.push(act('Stop', 'stop', () => api('/api/agents/' + a.id + '/stop', { method: 'POST' }).then(refresh).catch((x) => toast(x.message, true))));
    return h('a', { class: 'card bcard' + (a.state === 'waiting' ? ' attn' : ''), href: '#/agent/' + a.id },
      h('div', { class: 'r1' }, dot(a.state), h('span', { class: 't' }, a.title), h('span', { class: 'sp faint small' }, live ? elapsed(a.created) : ago(a.updated || a.created))),
      h('div', { class: 'last' }, a.last || (a.state === 'preparing' ? 'Gathering context…' : 'No activity yet')),
      h('div', { class: 'row' },
        h('span', { class: 'chip mono', title: 'Branch' }, icon('branch'), a.branch.replace(/^cully\//, '')),
        a.option ? h('span', { class: 'chip' }, 'Option ' + a.option) : null,
        a.token ? h('span', { class: 'chip' }, 'token ' + a.token) : null,
        files.length ? h('span', { class: 'chip', title: files.slice(0, 8).join('\n') }, files.length + (files.length === 1 ? ' file changed' : ' files changed')) : null,
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
      h('div', { class: 'r1' }, h('span', { class: 'dot queued' }), h('span', { class: 't' }, q.title), h('span', { class: 'sp faint small' }, ago(q.added))),
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
      h('h3', {}, 'Option ' + a.option, h('span', { class: 'pill ' + a.state }, stateLabel(a.state))),
      h('p', { class: 'last' }, a.last || 'No activity yet'),
      h('div', { class: 'row' }, h('span', { class: 'chip' }, (a.files || []).length + ' files changed')),
      h('ul', { class: 'files' }, ...(a.files || []).slice(0, 12).map((f) => h('li', {}, f))),
      h('div', { class: 'row' },
        h('a', { class: 'btn ghost small', href: '#/agent/' + a.id, onclick: () => { S.tab = 'changes'; closeMCP(); } }, icon('eye'), 'Open'),
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
  async function renderBoard() {
    try { S.overlaps = (await api('/api/board')).overlaps || []; } catch (_) { /* banner */ }
    let queue = [];
    try { queue = await api('/api/queue'); } catch (_) { /* optional */ }
    const prefs = boardPrefs();
    const repoName = (p) => p.split('/').filter(Boolean).pop() || p;
    const repos = [...new Set(S.agents.map((a) => a.repo))];
    const cols = [['queued', 'Up next', 'Waiting for a free slot'], ['preparing', 'Starting', 'Gathering context and a clean branch'], ['working', 'Working', 'Agents busy right now'], ['waiting', 'Needs you', 'Waiting for your reply'], ['finished', 'Finished', 'Review, then continue elsewhere']];
    const bucket = (a) => (a.state === 'preparing' ? 'preparing' : a.state === 'working' ? 'working' : a.state === 'waiting' ? 'waiting' : 'finished');
    const q = prefs.q.trim().toLowerCase();
    const shown = S.agents.filter((a) => (!prefs.repo || a.repo === prefs.repo) && (!q || (a.title + ' ' + a.branch + ' ' + (a.files || []).join(' ')).toLowerCase().includes(q)));
    const title = (id) => (S.agents.find((a) => a.id === id) || {}).title || id;
    if (!S.agents.length && !queue.length) {
      $('board').replaceChildren(h('div', { class: 'empty-state' }, h('h2', {}, 'No tasks yet'), h('p', { class: 'faint' }, 'Give Cully a task and it shows up here, moving from Starting to Finished.'), h('a', { class: 'btn', href: '#/' }, icon('plus'), 'Start a task')));
      return;
    }
    const count = (k) => (k === 'queued' ? queue.length : shown.filter((a) => bucket(a) === k).length);
    const strip = h('div', { class: 'bstrip' }, ...cols.map(([k, l]) => h('div', { class: 'bstat ' + k }, h('b', {}, String(count(k))), h('span', {}, l))));
    const search = h('input', { type: 'search', class: 'bsearch', placeholder: 'Search tasks, branches or files', 'aria-label': 'Search the board', value: prefs.q, autocomplete: 'off' });
    search.addEventListener('input', () => { saveBoardPrefs({ ...boardPrefs(), q: search.value }); renderBoard().then(() => { const el = document.querySelector('.bsearch'); if (el) { el.focus(); el.setSelectionRange(el.value.length, el.value.length); } }); });
    const sel = repos.length > 1 ? h('select', { 'aria-label': 'Folder', onchange: (e) => { saveBoardPrefs({ ...boardPrefs(), repo: e.target.value }); renderBoard(); } },
      h('option', { value: '' }, 'All folders'), ...repos.map((r) => h('option', Object.assign({ value: r }, r === prefs.repo ? { selected: true } : {}), repoName(r)))) : null;
    const tools = h('div', { class: 'row btools' }, search, sel, h('a', { class: 'btn', href: '#/' }, icon('plus'), 'New task'));
    const overlapBanner = S.overlaps.map((o) => h('div', { class: 'overlap' }, icon('alert'), ' ', h('b', {}, o.file), ' is being edited by ' + joinNames(o.agents.map(title)) + '. Expect a merge conflict, or ask one of them to wait.'));
    const board = h('div', { class: 'cols' }, ...cols.map(([key, label, hint]) => {
      const list = key === 'queued' ? queue : shown.filter((a) => bucket(a) === key);
      const groups = key === 'finished' || key === 'working' || key === 'waiting' ? [...new Set(list.filter((a) => a.group).map((a) => a.group))].filter((g) => S.agents.filter((a) => a.group === g).length > 1) : [];
      const sec = h('section', { class: 'bcol ' + key }, h('h3', {}, label, h('span', {}, String(list.length))), h('p', { class: 'bhint' }, hint),
        ...groups.map((g) => h('button', { class: 'btn ghost small cmpbtn', onclick: () => openCompare(g) }, icon('eye'), 'Compare options: ' + (S.agents.find((a) => a.group === g) || {}).title.replace(/ \(option .\)$/, ''))),
        ...(list.length ? list.map((x) => (key === 'queued' ? queueCard(x) : boardCard(x, S.overlaps))) : [h('div', { class: 'none' }, key === 'waiting' ? 'Nothing needs you.' : key === 'queued' ? 'No queued tasks. Use Queue for later when you start one.' : 'Nothing here.')]));
      sec.addEventListener('dragover', (e) => { e.preventDefault(); sec.classList.add('over'); });
      sec.addEventListener('dragleave', () => sec.classList.remove('over'));
      sec.addEventListener('drop', (e) => { sec.classList.remove('over'); dropOn(key, e); });
      return sec;
    }));
    $('board').replaceChildren(h('div', { class: 'bhead' }, strip, tools), ...overlapBanner, board);
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
        : h('p', { class: 'faint small' }, 'Once a few sessions are named, the coach looks for patterns across them and suggests a job, skill, MCP server, habit or eval to set up. It also looks again about once an hour while naming is on.'));
  }

  // The hourly scout switch, and the full history of what scouts found, kept on disk.
  async function scoutAuto(again) {
    let lib = null;
    try { lib = await api('/api/sessions/library'); } catch (_) { return null; }
    const on = lib.analysis.scout;
    const flip = async () => { try { await api('/api/scout/auto', { method: 'POST', body: JSON.stringify({ on: !on }) }); toast(on ? 'The hourly scout is off' : 'The hourly scout is on'); again(); } catch (e) { toast(e.message, true); } };
    return h('section', { class: 'lib-card' }, h('div', { class: 'row' }, h('b', {}, 'Scout the web every hour'), on && lib.analysis.lastScout && !lib.analysis.lastScout.startsWith('0001') ? h('span', { class: 'faint small' }, 'last ran ' + ago(lib.analysis.lastScout)) : null,
      h('button', { class: 'btn small sp' + (on ? ' ghost' : ''), onclick: flip }, icon(on ? 'x' : 'globe'), on ? 'Turn off' : 'Turn on')),
      h('p', { class: 'faint small' }, 'Once an hour the advisor reads what you have been working on and picks a specific search that would help, such as a library, an error or a practice you keep running into. The scout runs it and the findings are kept below. Searches use general technical terms only: never a folder, project, path, link or ticket. It needs at least three named sessions, so start naming sessions on the Session library page first.'));
  }
  async function scoutHistory() {
    let list = [];
    try { list = await api('/api/research?kind=scout'); } catch (_) { return null; }
    return h('section', { class: 'lib-card' }, h('b', {}, 'Scout history'), h('p', { class: 'faint small' }, list.length ? list.length + ' scouts kept on this computer.' : 'Nothing yet. Scouts you start, the ones the advisor starts and the ones run for an agent are all kept here.'),
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
          h('div', { class: 'row' }, ...live.map((a) => h('button', { class: 'btn small', onclick: () => run(async () => { await api('/api/agents/' + a.id + '/message', { method: 'POST', body: JSON.stringify({ text: ta.value }) }); closeMCP(); toast('Sent to ' + a.title); location.hash = '#/agent/' + a.id; }) }, icon('send'), a.title))))
          : h('p', { class: 'faint small' }, 'No task started from Cully is running right now.'),
        h('div', { class: 'row' },
          h('button', { class: 'btn', onclick: () => run(async () => { const v = await api('/api/agents', { method: 'POST', body: JSON.stringify({ prompt: ta.value, repo: S.opts.repo || '', mode: 'edit', context: true }) }); closeMCP(); await refresh(); location.hash = '#/agent/' + v.id; }) }, icon('plus'), 'Start a task for this'),
          h('button', { class: 'btn ghost', onclick: () => writeClip(ta.value).then(() => toast('Copied')).catch(() => showCommand(ta.value)) }, icon('copy'), 'Copy for a session in a terminal')),
        h('p', { class: 'faint small' }, 'Sessions open in a terminal, an editor or a desktop app cannot be written to from this page, so copy the text and paste it there.'), err));
    const bg = h('div', { class: 'modal-bg', onclick: (e) => { if (e.target === bg) closeMCP(); } }, modal);
    $('modal-root').replaceChildren(bg);
    S.modal = { agent: 'suggest' };
    document.removeEventListener('keydown', escClose);
    document.addEventListener('keydown', escClose);
  }

  // ---------- evals ----------
  const RUN_LABEL = { running: 'Running', judging: 'Judging', pass: 'Passed', partial: 'Partly', fail: 'Failed', error: 'No result' };
  async function renderEvals(out, head) {
    const [cases, runs] = await Promise.all([api('/api/sessions/evals').catch(() => []), api('/api/evals/runs').catch(() => [])]);
    const live = () => S.route.name === 'dashboard' && S.route.panel === 'evals';
    const again = () => renderEvals(out, head);
    const active = runs.some((r) => r.status === 'running' || r.status === 'judging');
    const start = async (c, btn) => { btn.disabled = true; try { await api('/api/evals/' + c.id + '/run', { method: 'POST' }); toast('Started. A fresh agent is working on this goal.'); again(); } catch (e) { toast(e.message, true); btn.disabled = false; } };
    const lastFor = (id) => runs.find((r) => r.evalId === id);
    const passed = runs.filter((r) => r.status === 'pass').length; const failed = runs.filter((r) => r.status === 'fail').length;
    out.replaceChildren(head(refreshBtn(again)),
      h('div', { class: 'kpis' }, ...[['Eval cases', String(cases.length)], ['Runs', String(runs.length)], ['Passed', String(passed)], ['Failed', String(failed)], ['Running now', String(runs.filter((r) => r.status === 'running' || r.status === 'judging').length)]].map(([k, v]) => h('div', { class: 'kpi' }, h('b', {}, v), h('span', {}, k)))),
      h('h3', { class: 'section-h' }, 'Runs'),
      runs.length ? h('ul', { class: 'hrows' }, ...runs.slice(0, 30).map((r) => h('li', { class: 'hrow' },
        h('div', { class: 'hmain' }, h('div', { class: 'ht' }, r.task, h('span', { class: 'chip run-' + r.status }, RUN_LABEL[r.status] || r.status)),
          r.reason ? h('p', { class: 'hdesc' }, r.reason) : null,
          h('div', { class: 'row' }, h('span', { class: 'chip' }, ago(r.started)), r.seconds ? h('span', { class: 'chip' }, dur(r.seconds)) : null, r.files ? h('span', { class: 'chip' }, r.files + ' files changed') : null)),
        r.agentId ? h('div', { class: 'row hact' }, h('a', { class: 'btn ghost small', href: '#/agent/' + r.agentId }, 'Open the task')) : null)))
        : emptyCard('No eval runs yet', 'Run a case below: a fresh agent gets the goal, then a judge checks its work against the checks.', null, null),
      h('h3', { class: 'section-h' }, 'Eval cases'),
      cases.length ? h('ul', { class: 'hrows' }, ...cases.map((c) => { const last = lastFor(c.id); const btn = h('button', { class: 'btn small' }, icon('send'), last ? 'Run again' : 'Run');
        btn.addEventListener('click', () => start(c, btn));
        return h('li', { class: 'hrow' }, h('div', { class: 'hmain' }, h('div', { class: 'ht' }, c.task, last ? h('span', { class: 'chip run-' + last.status }, RUN_LABEL[last.status] || last.status) : null),
          c.criteria.length ? h('ul', { class: 'files' }, ...c.criteria.map((x) => h('li', {}, x))) : null,
          h('div', { class: 'row' }, h('span', { class: 'chip' }, icon('folder'), c.folder), h('span', { class: 'chip' }, 'made ' + ago(c.created)))), h('div', { class: 'row hact' }, btn)); }))
        : emptyCard('No eval cases yet', 'Open Session health, pick a session that finished its job and choose Make an eval.', 'Open Session health', '#/dashboard/health'));
    if (active) setTimeout(() => { if (live()) again(); }, 4000);
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
    try { [list, tools] = await Promise.all([api('/api/identities'), api('/api/admin/tools')]); } catch (e) { box.replaceChildren(h('div', { class: 'err' }, e.message)); return; }
    const again = () => renderTeam();
    const live = list.some((a) => a.runs.some((r) => r.status === 'running'));
    const card = (a) => {
      const last = a.runs[0]; const groups = tools.presets.filter((p) => a.groups[p.id]).map((p) => p.label);
      return h('section', { class: 'lib-card agentcard' },
        h('div', { class: 'row' }, h('b', { class: 'an' }, a.name), a.enabled ? h('span', { class: 'chip ok' }, 'Runs ' + a.schedule) : h('span', { class: 'chip' }, a.schedule ? 'Schedule paused' : 'On demand'), h('span', { class: 'chip' }, icon('folder'), a.folder.split('/').pop()), h('span', { class: 'chip' }, a.mode === 'plan' ? 'Read only' : 'Can edit'),
          h('span', { class: 'sp row' },
            h('button', { class: 'btn small', disabled: !!(last && last.status === 'running'), onclick: async () => { try { await api('/api/identities/' + a.id + '/run', { method: 'POST' }); toast(a.name + ' is running. See it on the Tasks tab.'); again(); } catch (e) { toast(e.message, true); } } }, icon('send'), last && last.status === 'running' ? 'Running' : 'Run now'),
            h('button', { class: 'btn ghost small', onclick: () => openAgentForm(a, tools, again) }, 'Edit'),
            h('button', { class: 'btn ghost small', onclick: () => confirmDialog({ title: 'Remove ' + a.name + '?', body: ['Its notes and run history are deleted. Tasks it already ran stay on the Tasks tab.'], confirmLabel: 'Remove', onConfirm: async () => { await api('/api/identities/' + a.id, { method: 'DELETE' }); again(); } }) }, icon('x'), 'Remove'))),
        h('p', { class: 'hdesc' }, a.role),
        h('div', { class: 'row' }, ...groups.map((g) => h('span', { class: 'chip' }, g))),
        last ? h('div', { class: 'agentlast' }, h('div', { class: 'row' }, h('span', { class: 'chip run-' + (last.status === 'done' ? 'pass' : last.status === 'failed' ? 'fail' : 'running') }, last.status === 'done' ? 'Last run finished' : last.status === 'failed' ? 'Last run failed' : 'Running now'), h('span', { class: 'faint small' }, ago(last.started)), last.seconds ? h('span', { class: 'faint small' }, dur(last.seconds)) : null, last.taskId ? h('a', { class: 'linkbtn', href: '#/agent/' + last.taskId }, 'Open the task') : null),
          last.summary ? h('div', { class: 'quote' }, markdown(last.summary)) : null) : h('p', { class: 'faint small' }, 'Has not run yet.'),
        a.memory ? h('details', { class: 'rl' }, h('summary', {}, h('span', { class: 'rq' }, 'Its notes'), h('span', { class: 'faint small' }, a.memory.length + ' of 2200 characters')), h('div', { class: 'rbody' }, markdown(a.memory))) : null,
        a.runs.length > 1 ? h('details', { class: 'rl' }, h('summary', {}, h('span', { class: 'rq' }, 'Earlier runs'), h('span', { class: 'faint small' }, String(a.runs.length - 1))), h('ul', { class: 'hrows' }, ...a.runs.slice(1, 8).map((r) => h('li', { class: 'hrow' }, h('div', { class: 'hmain' }, h('div', { class: 'row' }, h('span', { class: 'chip run-' + (r.status === 'done' ? 'pass' : 'fail') }, r.status), h('span', { class: 'faint small' }, ago(r.started))), r.summary ? h('p', { class: 'hdesc' }, uiShort(r.summary, 220)) : null)))) ) : null);
    };
    box.replaceChildren(
      toolbar('Agents', 'Named workers with a role of their own. Each keeps notes between runs and can run on a schedule.', h('button', { class: 'btn', onclick: () => openAgentForm(null, tools, again) }, icon('plus'), 'New agent')),
      h('details', { class: 'lib-card', ...(list.length ? {} : { open: true }) }, h('summary', {}, h('b', {}, 'What an agent is and how it works')),
        h('ol', { class: 'how' },
          h('li', {}, h('b', {}, 'Identity'), 'A name, a one-line role and instructions on how it works. Cully adds them to the start of every run, so they stay the same and the prompt cache can reuse them.'),
          h('li', {}, h('b', {}, 'Tools'), 'Only the tool groups you pick. The always-blocked list from Agent permissions still applies.'),
          h('li', {}, h('b', {}, 'A folder'), 'It runs as a task in its own worktree of that folder, so your checkout is untouched. Runs show on the Tasks tab.'),
          h('li', {}, h('b', {}, 'Notes'), 'At the end of a run it saves up to three short facts. The next run starts by reading them. The notes are capped at 2,200 characters and the newest win.'),
          h('li', {}, h('b', {}, 'Schedule'), 'Run it now, every hour, or every day while the Cully page server is running. Each run keeps a short summary.'))),
      list.length ? h('div', { class: 'groups' }, ...list.map(card)) : emptyCard('No agents yet', 'Create one for work you repeat: watching pull requests, guarding the tests, checking dependencies.', null, null));
    if (live) setTimeout(() => { if (S.route.name === 'team') again(); }, 4000);
  }
  const uiShort = (t, n) => (t.length > n ? t.slice(0, n) + '…' : t);
  function openAgentForm(a, tools, again) {
    const f = a || { name: '', role: '', instructions: '', folder: S.opts.repo || (S.info && S.info.repos[0]) || '', groups: Object.fromEntries(tools.presets.map((p) => [p.id, p.on])), mode: 'edit', schedule: '', enabled: false };
    const val = { ...f, groups: { ...f.groups } };
    const err = h('div', { class: 'err', hidden: true });
    const text = (key, label, hint, rows) => { const el = rows ? h('textarea', { rows: String(rows), 'aria-label': label }) : h('input', { type: 'text', 'aria-label': label, autocomplete: 'off' }); el.value = val[key] || ''; el.addEventListener('input', () => { val[key] = el.value; }); return h('label', { class: 'field' }, h('span', { class: 'opt-label' }, label), el, hint ? h('span', { class: 'faint small' }, hint) : null); };
    const folders = [...new Set([val.folder, ...((S.info && S.info.repos) || [])].filter(Boolean))];
    const folderSel = h('select', { 'aria-label': 'Folder' }, ...folders.map((p) => h('option', Object.assign({ value: p }, p === val.folder ? { selected: true } : {}), p)));
    folderSel.addEventListener('change', () => { val.folder = folderSel.value; });
    const roleField = text('role', 'What it does', 'One sentence.'); const instrField = text('instructions', 'How it works', 'Its identity: how it should behave, what it must never do. Added to every run.', 6); const nameField = text('name', 'Name');
    const tpl = a ? null : h('div', { class: 'row' }, h('span', { class: 'opt-label' }, 'Start from'), ...AGENT_TEMPLATES.map(([n, r, i, g]) => h('button', { type: 'button', class: 'chip', onclick: () => { val.name = n; val.role = r; val.instructions = i; for (const p of tools.presets) val.groups[p.id] = !!g[p.id]; val.mode = 'plan' === val.mode ? 'plan' : val.mode; closeMCP(); openAgentForm({ ...val, id: null }, tools, again); } }, n)));
    const groups = h('div', { class: 'toolgroups' }, ...tools.presets.map((p) => { const cb = h('input', Object.assign({ type: 'checkbox', id: 'ag-' + p.id, onchange: (e) => { val.groups[p.id] = e.target.checked; } }, val.groups[p.id] ? { checked: true } : {})); return h('label', { class: 'toolgroup', for: 'ag-' + p.id }, cb, h('span', {}, h('b', {}, p.label), h('small', {}, p.description))); }));
    const mode = h('select', { 'aria-label': 'Mode' }, h('option', Object.assign({ value: 'edit' }, val.mode === 'edit' ? { selected: true } : {}), 'Can edit files'), h('option', Object.assign({ value: 'plan' }, val.mode === 'plan' ? { selected: true } : {}), 'Read only (plan mode)'));
    mode.addEventListener('change', () => { val.mode = mode.value; });
    const sched = h('select', { 'aria-label': 'Schedule' }, ...[['', 'Only when I run it'], ['hourly', 'Every hour'], ['daily', 'Every day']].map(([v, l]) => h('option', Object.assign({ value: v }, val.schedule === v ? { selected: true } : {}), l)));
    sched.addEventListener('change', () => { val.schedule = sched.value; val.enabled = !!sched.value; });
    const clear = a && a.memory ? h('label', { class: 'check' }, h('input', { type: 'checkbox', onchange: (e) => { val.clearMemory = e.target.checked; } }), 'Forget its notes') : null;
    const save = h('button', { class: 'btn', onclick: async () => {
      err.hidden = true; save.disabled = true;
      try {
        const body = { name: val.name, role: val.role, instructions: val.instructions, folder: val.folder, groups: val.groups, mode: val.mode, schedule: val.schedule, enabled: !!val.schedule, clearMemory: !!val.clearMemory };
        if (a && a.id) await api('/api/identities/' + a.id, { method: 'PUT', body: JSON.stringify(body) }); else await api('/api/identities', { method: 'POST', body: JSON.stringify(body) });
        closeMCP(); toast('Saved ' + val.name); again();
      } catch (e) { err.textContent = e.message; err.hidden = false; save.disabled = false; }
    } }, icon('check'), a && a.id ? 'Save' : 'Create agent');
    const modal = h('div', { class: 'modal wide', role: 'dialog', 'aria-modal': 'true', 'aria-label': a && a.id ? 'Edit agent' : 'New agent' },
      h('header', {}, h('h2', {}, a && a.id ? 'Edit ' + f.name : 'New agent'), h('button', { class: 'btn ghost small sp', onclick: closeMCP }, 'Cancel')),
      h('div', { class: 'mbody' }, tpl, nameField, roleField, instrField,
        h('label', { class: 'field' }, h('span', { class: 'opt-label' }, 'Folder it works in'), folderSel),
        h('div', {}, h('span', { class: 'opt-label' }, 'Tools it may use'), groups),
        h('div', { class: 'row' }, h('label', { class: 'field' }, h('span', { class: 'opt-label' }, 'Mode'), mode), h('label', { class: 'field' }, h('span', { class: 'opt-label' }, 'Schedule'), sched)),
        clear, err, h('div', { class: 'row' }, save)));
    const bg = h('div', { class: 'modal-bg', onclick: (e) => { if (e.target === bg) closeMCP(); } }, modal);
    $('modal-root').replaceChildren(bg);
    S.modal = { agent: 'agentform' };
    document.removeEventListener('keydown', escClose);
    document.addEventListener('keydown', escClose);
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
        const tools = Object.entries(x.toolNames || {}).sort((a, b) => b[1] - a[1]).slice(0, 5);
        return h('li', { class: 'hrow' },
          ring(x.health, 46),
          h('div', { class: 'hmain' },
            h('div', { class: 'ht' }, x.title || x.name, h('span', { class: 'chip ' + (x.completed === 'yes' ? 'ok' : x.completed === 'no' ? 'warn' : '') }, DONE_LABEL[x.completed] || 'Unclear'), x.onTask === 'drifted' ? h('span', { class: 'chip warn' }, 'Drifted') : null),
            x.task ? h('p', { class: 'hdesc' }, h('b', {}, 'Goal: '), x.task) : null,
            x.description ? h('p', { class: 'hdesc' }, h('b', {}, 'What it did: '), x.description) : null,
            x.drift ? h('p', { class: 'hdiff' }, h('b', {}, 'Drift: '), x.drift) : null,
            (x.signals || []).length ? h('ul', { class: 'signals' }, ...x.signals.map((g) => h('li', {}, icon('alert'), g))) : null,
            x.different && x.different !== 'Nothing notable.' ? h('p', { class: 'hdiff' }, h('b', {}, 'Could go better: '), x.different) : null,
            h('div', { class: 'row' }, h('span', { class: 'chip' }, icon('folder'), x.name), h('span', { class: 'chip' }, ago(x.updated)), h('span', { class: 'chip' }, dur(x.activeSec) + ' worked'),
              h('span', { class: 'chip' }, (x.tools || 0) + ' tool calls'), x.errors ? h('span', { class: 'chip warn' }, x.errors + ' errors') : null, x.checks ? h('span', { class: 'chip' }, x.checks + ' test runs') : null),
            tools.length ? h('div', { class: 'row' }, ...tools.map(([n, c]) => h('span', { class: 'chip mono', title: 'Calls to ' + n }, n + ' ' + c))) : null),
          h('div', { class: 'row hact' },
            evalIDs.has(x.id) ? h('button', { class: 'btn ghost small', onclick: () => copyEval(x) }, icon('copy'), 'Copy eval case')
              : h('button', { class: 'btn small', disabled: x.completed !== 'yes' || !x.task, title: x.completed === 'yes' ? 'Save this session as a test case for future agents' : 'Only completed sessions can become an eval', onclick: () => makeEval(x) }, icon('check'), 'Make an eval')));
      })) : null,
      judged.length > rows.length ? h('div', { class: 'row' }, h('button', { class: 'btn ghost', onclick: () => { S.healthShown = (S.healthShown || 25) + 25; again(); } }, 'Show more')) : null,
      h('section', { class: 'lib-card' }, h('b', {}, 'About evals'), h('p', { class: 'faint small' }, 'An eval case turns a session that finished its job into a test: the goal in one sentence, how to tell it was met, and what the session actually did. It holds no prompts or replies. Cases are saved as files in your Cully folder under evals, one per session.')));
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
      attention.length ? h('div', {}, h('span', { class: 'opt-label' }, 'Look at these first'), h('ul', { class: 'attn-list' }, ...attention.map((x) => h('li', {}, ring(x.health, 34), h('div', {}, h('b', {}, x.title || x.name), h('div', { class: 'faint small' }, (x.signals || []).join('. ') || 'Low health'))))), h('a', { class: 'linkbtn', href: '#/dashboard/health' }, 'See all in Session health')) : null);
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
      ? h('section', { class: 'lib-card' }, h('div', { class: 'row' }, h('b', {}, 'Naming your sessions'), h('span', { class: 'faint' }, t.analyzed + ' of ' + total + ' done'), h('button', { class: 'btn ghost small sp', onclick: () => toggle(false) }, 'Pause')),
        h('div', { class: 'bar' }, fill(pct)),
        h('p', { class: 'faint small' }, 'Claude reads each finished session in the background and writes a title, a short description and what could go better. It does up to ' + lib.analysis.perHour + ' an hour, newest first, so the first pass takes a while. It uses your Claude plan.'))
      : h('section', { class: 'lib-card' }, h('b', {}, t.pending ? 'Name your ' + t.pending + ' past sessions' : 'All sessions are named'),
        h('p', { class: 'faint small' }, 'Claude can read each finished session in the background and write a title, a short description and what could go better. Nothing is sent anywhere except to your own Claude, and folders you hide are skipped. It takes a while and uses your Claude plan.'),
        t.pending ? h('button', { class: 'btn', onclick: () => toggle(true) }, icon('spark'), 'Start naming sessions') : null);
    const maxT = Math.max(1, ...lib.folders.map((f) => f.activeSec));
    const folders = h('section', { class: 'lib-card' }, h('b', {}, 'Where the time went'),
      h('ul', { class: 'fbars' }, ...lib.folders.slice(0, 12).map((f) => h('li', { title: f.cwd }, h('span', { class: 'fname' }, f.name), h('span', { class: 'fbar' }, fill(Math.max(2, Math.round((f.activeSec / maxT) * 100)))),
        h('span', { class: 'faint small' }, f.count + (f.count === 1 ? ' session, ' : ' sessions, ') + dur(f.activeSec))))));
    const shown = S.libShown || 30;
    const rows = lib.sessions.slice(0, shown).map((x) => h('li', { class: 'hrow' },
      h('div', { class: 'hmain' },
        h('div', { class: 'ht' }, x.title || x.name, x.source === 'ai' ? null : h('span', { class: 'chip' }, 'Not named yet')),
        x.description ? h('p', { class: 'hdesc' }, x.description) : null,
        h('div', { class: 'row' }, h('span', { class: 'chip' }, icon('folder'), x.name), x.origin ? h('span', { class: 'chip' }, x.origin) : null, x.branch ? h('span', { class: 'chip mono' }, icon('branch'), x.branch) : null,
          h('span', { class: 'chip' }, ago(x.updated)), h('span', { class: 'chip' }, dur(x.activeSec) + ' worked'),
          x.files ? h('span', { class: 'chip' }, x.files + (x.files === 1 ? ' file changed' : ' files changed')) : null,
          x.checks ? h('span', { class: 'chip' }, x.checks + (x.checks === 1 ? ' test run' : ' test runs')) : null,
          x.errors ? h('span', { class: 'chip warn' }, x.errors + (x.errors === 1 ? ' tool error' : ' tool errors')) : null),
        x.different && x.different !== 'Nothing notable.' ? h('p', { class: 'hdiff' }, h('b', {}, 'Could go better: '), x.different) : null)));
    const insights = libraryInsights(lib);
    out.replaceChildren(head(refreshBtn(again)),
      h('div', { class: 'kpis' }, ...[['Sessions', String(t.sessions)], ['Time worked', dur(t.activeSec)], ['Named', t.analyzed + ' of ' + total], ...insights.kpis].map(([k, v]) => h('div', { class: 'kpi' }, h('b', {}, v), h('span', {}, k)))),
      analysis, insights.card, await coachCard(again, true), folders,
      h('h3', { class: 'section-h' }, 'Sessions'),
      rows.length ? h('ul', { class: 'hrows' }, ...rows) : emptyCard('No sessions yet', 'Sessions you run appear here, with how long you worked and what you did.', 'Start a task', '#/'),
      lib.sessions.length > shown ? h('div', { class: 'row' }, h('button', { class: 'btn ghost', onclick: () => { S.libShown = shown + 30; again(); } }, 'Show more')) : null);
    if (lib.analysis.on) setTimeout(() => { if (live()) again(); }, 30000);
  }

  // ---------- agent permissions ----------
  async function renderPermissions(out, head) {
    const v = await api('/api/admin/tools');
    const state = { presets: Object.fromEntries(v.presets.map((p) => [p.id, p.on])), allow: [...v.allow], deny: [...v.deny], mcp: [...v.mcp.only] };
    const save = async (deny) => {
      try {
        const r = await api('/api/admin/tools', { method: 'PUT', body: JSON.stringify({ presets: state.presets, allow: state.allow, deny, mcpOnly: state.mcp }) });
        toast('Saved. New agents use these settings.');
        return r;
      } catch (e) { toast(e.message, true); return null; }
    };
    const listEditor = (kind, items, placeholder) => {
      const ul = h('ul', { class: 'rules' });
      const input = h('input', { type: 'text', placeholder, 'aria-label': 'Add to the ' + kind + ' list', autocomplete: 'off', spellcheck: 'false' });
      const draw = () => ul.replaceChildren(...(items.length ? items.map((t, i) => h('li', {}, h('code', {}, t), h('button', { class: 'iconbtn', title: 'Remove', 'aria-label': 'Remove ' + t, onclick: () => { items.splice(i, 1); draw(); } }, icon('x')))) : [h('li', { class: 'faint' }, 'Nothing here.')]));
      const add = () => { const t = input.value.trim(); if (t && !items.includes(t)) { items.push(t); input.value = ''; draw(); } };
      input.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); add(); } });
      draw();
      return h('div', {}, ul, h('div', { class: 'row' }, input, h('button', { class: 'btn ghost small', onclick: add }, icon('plus'), 'Add')));
    };
    const groups = h('div', { class: 'toolgroups' }, ...v.presets.map((p) => {
      const cb = h('input', Object.assign({ type: 'checkbox', id: 'tg-' + p.id, onchange: (e) => { state.presets[p.id] = e.target.checked; } }, p.on ? { checked: true } : {}));
      return h('label', { class: 'toolgroup', for: 'tg-' + p.id }, cb, h('span', {}, h('b', {}, p.label), h('small', {}, p.description)));
    }));
    const denyBox = h('div', {});
    const drawDeny = () => denyBox.replaceChildren(listEditor('blocked', state.deny, 'For example Bash(rm:*)'));
    drawDeny();
    out.replaceChildren(head(),
      h('p', { class: 'faint' }, 'Agents Cully starts run on their own, so nothing can answer a permission question. These settings decide what they may do. They apply to agents you start after saving.'),
      h('section', { class: 'group' }, h('header', {}, h('span', { class: 'gt' }, 'What agents may do')), groups),
      h('section', { class: 'group' }, h('header', {}, h('span', { class: 'gt' }, 'Connected tools to load')),
        h('p', { class: 'faint small' }, 'Every connected server adds its tool descriptions to every turn the task takes, so loading fewer saves tokens. Pick the ones tasks need, or none to load them all. Servers found in your Claude settings are listed.'),
        v.mcp.available.length ? h('div', { class: 'toolgroups' }, ...v.mcp.available.map((n) => {
          const cb = h('input', Object.assign({ type: 'checkbox', id: 'mcp-' + n, onchange: (e) => { state.mcp = e.target.checked ? [...state.mcp, n] : state.mcp.filter((x) => x !== n); } }, state.mcp.includes(n) ? { checked: true } : {}));
          return h('label', { class: 'toolgroup', for: 'mcp-' + n }, cb, h('span', {}, h('b', { class: 'mono' }, n)));
        })) : h('p', { class: 'faint small' }, 'No connected servers were found in your Claude settings.')),
      h('section', { class: 'group' }, h('header', {}, h('span', { class: 'gt' }, 'Extra tools to allow')),
        h('p', { class: 'faint small' }, 'Add a tool by name, for example mcp__gitlab__get_issue for one tool of a connected server, or Bash(go test:*) for one kind of command.'), listEditor('allowed', state.allow, 'mcp__server__tool')),
      h('section', { class: 'group' }, h('header', {}, h('span', { class: 'gt' }, 'Always blocked'), h('button', { class: 'linkbtn sp', onclick: () => { state.deny = [...v.defaultDeny]; drawDeny(); } }, 'Reset to defaults')),
        h('p', { class: 'faint small' }, 'Blocked tools stay blocked even when allowed above. The defaults stop agents from pushing code or sending data out.'), denyBox),
      h('div', { class: 'row' }, h('button', { class: 'btn', onclick: async () => { const r = await save(state.deny); if (r) renderPermissions(out, head); } }, icon('check'), 'Save'),
        h('button', { class: 'btn ghost', onclick: () => renderPermissions(out, head) }, 'Discard changes')));
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
  function renderAdvisor(list) {
    if (!list.length) return emptyCard('No tasks yet', 'The advisor reviews what a running agent is doing and suggests what to fix or check. Start a task to get advice.', 'Start a task', '#/');
    return h('div', { class: 'groups' }, ...list.map((g) => {
      const open = g.advice.filter((x) => !x.dismissed);
      const asking = g.status === 'asking';
      return h('section', { class: 'group' },
        h('header', {}, dot(g.agentState), h('a', { class: 'gt', href: '#/agent/' + g.agentId }, g.agentTitle), h('span', { class: 'pill ' + g.agentState }, g.agentState === 'waiting' ? 'needs you' : g.agentState),
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
        h('header', {}, dot(v.state), h('a', { class: 'gt', href: '#/agent/' + v.agentId }, v.title)),
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
    jobs: ['No background jobs', 'Background jobs run on a schedule you set. None are set up.'],
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
    ['Work', [['advisor', 'Advisor', 'Advice for your tasks'], ['library', 'Session library', 'Every past session, named'], ['health', 'Session health', 'Did sessions finish the job?'], ['evals', 'Evals', 'Test cases and their runs'], ['scout', 'Scout', 'Live searches and findings'], ['activity', 'Activity', 'What was done, step by step']]],
    ['Health', [['setup', 'Setup', 'Is everything connected?'], ['savings', 'Savings', 'What Cully saved and cost']]],
    ['More', [['jobs', 'Background jobs', 'Runs on a schedule'], ['permissions', 'Agent permissions', 'What agents may do']]],
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
    for (const a of S.agents.filter((x) => x.state === 'waiting')) rows.push({ icon: 'alert', text: a.title + ' is waiting for you', label: 'Open', href: '#/agent/' + a.id });
    for (const g of S.advice || []) {
      const n = g.advice.filter((x) => !x.applied && !x.dismissed).length;
      if (n) rows.push({ icon: 'spark', text: n + (n === 1 ? ' piece of advice' : ' pieces of advice') + ' for ' + g.agentTitle, label: 'Review', href: '#/dashboard/advisor' });
    }
    if (memoryMode() === 'mcp') for (const m of mcpNeeding()) rows.push({ icon: 'lock', text: 'Cully MCP needs sign-in for ' + AGENT_NAMES[m.agent], label: 'Sign in', onclick: () => openMCP(m.agent) });
    if (S.overview && S.overview.activity && S.overview.activity.facts.loops > 0) rows.push({ icon: 'alert', text: 'A recent session repeated a failing command', label: 'Look', href: '#/dashboard/activity' });
    if (S.overview && S.overview.setup && !S.overview.setup.allReady) rows.push({ icon: 'alert', text: 'Cully setup needs attention', label: 'See what', href: '#/dashboard/setup' });
    return rows;
  }
  function renderNeeds() {
    const box = $('needs');
    if (!box) return;
    const rows = needsList();
    box.replaceChildren(
      h('h2', { class: 'hello' }, rows.length ? (rows.length === 1 ? 'One thing needs you' : rows.length + ' things need you') : 'Nothing needs you right now'),
      rows.length ? h('ul', { class: 'needs' }, ...rows.map((r) => h('li', {}, icon(r.icon), h('span', {}, r.text),
        r.href ? h('a', { class: 'btn small', href: r.href }, r.label) : h('button', { class: 'btn small', onclick: r.onclick }, r.label))))
        : h('p', { class: 'faint' }, 'Agents are running smoothly, or none are running. Start one from the Agents tab.'));
  }
  async function renderOverview(out, head) {
    const repo = encodeURIComponent(S.opts.repo || '');
    const [impact, setup, activity] = await Promise.all([api('/api/impact?days=14').catch(() => null), api('/api/setup').catch(() => null), api('/api/activity?repo=' + repo).catch(() => null)]);
    S.overview = { impact, setup, activity };
    const mine = (S.sessions || []).filter((x) => !x.detached);
    const folders = new Set(mine.map((x) => x.repo || x.cwd)).size;
    const sess = mine.length;
    const daily = impact ? impact.daily.map((d) => d.saved) : [];
    out.replaceChildren(head(refreshBtn(() => drawPanel('overview', true))),
      h('div', { id: 'needs', class: 'overview-needs' }),
      h('div', { class: 'glance' },
        h('a', { class: 'glance-row', href: '#/sessions' }, h('span', { class: 'gl' }, 'Open now'), h('span', { class: 'gv' }, sess ? sess + (sess === 1 ? ' session' : ' sessions') + ' in ' + folders + (folders === 1 ? ' folder' : ' folders') : 'No sessions open'), h('span', { class: 'ga' }, 'See sessions')),
        h('a', { class: 'glance-row', href: '#/dashboard/savings' }, h('span', { class: 'gl' }, 'Last 14 days'),
          h('span', { class: 'gv' }, impact && impact.net > 0 ? 'Saved about ' + fmtTokens(impact.net) + ' tokens' : 'Nothing saved yet', daily.some((v) => v > 0) ? h('span', { class: 'sparkwrap' }, sparkline(daily)) : null), h('span', { class: 'ga' }, 'Details')),
        h('a', { class: 'glance-row', href: '#/dashboard/setup' }, h('span', { class: 'gl' }, 'Setup'),
          h('span', { class: 'gv' }, !setup ? 'Not checked' : setup.allReady ? 'All set' : 'Needs attention'), h('span', { class: 'ga' }, 'Details')),
        h('a', { class: 'glance-row', href: '#/dashboard/activity' }, h('span', { class: 'gl' }, 'Activity'),
          h('span', { class: 'gv' }, activity && activity.recorded ? activity.facts.edits + (activity.facts.edits === 1 ? ' edit' : ' edits') + ' recorded in this folder' : 'Nothing recorded in this folder yet'), h('span', { class: 'ga' }, 'Details'))),
      h('section', { class: 'start-else' }, h('h3', {}, 'Start somewhere else'), h('p', { class: 'faint' }, 'Choose any folder on this machine and start a new task there.'),
        h('div', { class: 'row' }, h('button', { class: 'btn', onclick: () => openFolderChooser() }, icon('folder'), 'Choose a folder'),
          ...recentFolders(5).filter((r) => r.git && r.path !== S.opts.repo).map((r) => h('button', { class: 'chip', title: r.path, onclick: () => startIn(r.path) }, icon('folder'), r.name)))));
    renderNeeds();
  }

  // ----- setup -----
  function setupRow(ok, label, hint, action) {
    return h('li', { class: ok ? 'ok' : 'bad' }, icon(ok ? 'check' : 'x'), h('span', { class: 'sl' }, label), !ok && hint ? h('span', { class: 'faint small' }, hint) : null, action || null);
  }
  function renderSetup(data) {
    const order = [...data.agents].sort((a, b) => (mcpUsed().some((m) => m.agent === b.agent) ? 1 : 0) - (mcpUsed().some((m) => m.agent === a.agent) ? 1 : 0));
    const local = memoryMode() === 'local';
    const kids = order.map((a) => {
      const mcp = (S.mcp || []).find((m) => m.agent === a.agent);
      const pill = !a.installed ? ['', 'Not installed'] : a.ready ? ['working', 'Ready'] : ['waiting', 'Needs attention'];
      const rows = [];
      if (a.installed) {
        for (const it of a.items) rows.push(setupRow(it.OK === undefined ? it.ok : it.ok, it.label, it.hint));
        if (mcp) {
          if (local || mcp.state === 'not-configured') rows.push(h('li', { class: 'ok' }, icon('check'), h('span', { class: 'sl' }, 'Keeping history on this machine (local disk mode)'), h('button', { class: 'btn ghost small', onclick: () => connectMCPDialog(a.agent) }, icon('lock'), 'Connect the Cully MCP')));
          else if (mcp.state === 'connected') rows.push(setupRow(true, 'Cully memory connected'));
          else if (mcp.state === 'needs-auth') rows.push(setupRow(false, 'Cully memory needs sign-in', '', h('button', { class: 'btn small', onclick: () => openMCP(a.agent) }, icon('lock'), 'Sign in')));
          else if (mcp.state === 'unhealthy') rows.push(setupRow(false, "Cully memory can't be reached", 'Check that the Cully server is up.'));
        }
      } else rows.push(h('li', { class: 'faint' }, AGENT_NAMES[a.agent] + ' is not installed on this machine.'));
      const gaps = a.installed && a.items.some((i) => !i.ok);
      return h('section', { class: 'group' }, h('header', {}, h('span', { class: 'gt' }, AGENT_NAMES[a.agent]), h('span', { class: 'pill ' + pill[0] }, pill[1]),
        gaps ? h('button', { class: 'btn small sp', onclick: () => enableAgentDialog(a.agent) }, icon('plus'), 'Set up ' + AGENT_NAMES[a.agent]) : null), h('ul', { class: 'checks' }, ...rows));
    });
    kids.push(h('section', { class: 'group' }, h('header', {}, h('span', { class: 'gt' }, 'Advisor for terminal sessions'), h('span', { class: 'pill ' + (data.advisorRunning ? 'working' : 'waiting') }, data.advisorRunning ? 'Running' : 'Not running'),
      data.advisorRunning ? null : h('button', { class: 'btn small sp', onclick: startAdvisorNow }, icon('spark'), 'Start the advisor')),
      data.advisorRunning ? null : h('p', { class: 'faint' }, data.advisorHint),
      h('details', { class: 'more' }, h('summary', {}, 'Details'), h('p', { class: 'faint' }, 'Connected tools: ' + data.tools.MCP + '. Skills: ' + data.tools.Skills + '. Helper agents: ' + data.tools.Agents + '. Names are hidden here on purpose.'))));
    return h('div', { class: 'groups' }, ...kids);
  }

  // ----- savings -----
  async function renderSavings(out, head, days) {
    const v = await api('/api/impact?days=' + days);
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
      h('details', { class: 'more' }, h('summary', {}, 'How this is measured'),
        h('p', {}, 'Saved is an estimate of tokens Cully avoided sending, such as analyses it skipped. Spent is what its memory lookups cost, measured. Memory savings are only counted when there is a trustworthy with-and-without comparison.'),
        v.basis ? h('p', { class: 'faint' }, v.basis) : null));
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
  async function renderActivity(out, head, filter) {
    const repo = encodeURIComponent(S.opts.repo || '');
    const [sum, data] = await Promise.all([api('/api/handoff?repo=' + repo), api('/api/activity?repo=' + repo)]);
    const chips = h('div', { class: 'row filters' }, ...[['all', 'Everything'], ['files', 'Files'], ['commands', 'Commands and checks'], ['problems', 'Problems']].map(([k, l]) => h('button', { type: 'button', class: 'toggle' + (k === filter ? ' on' : ''), onclick: () => { S.activityFilter = k; drawPanel('activity', true); } }, l)));
    const state = sum.fileCount ? [h('p', { class: 'lead' }, sum.fileCount + (sum.fileCount === 1 ? ' file' : ' files') + ' changed', sum.added || sum.removed ? h('span', {}, '  ', h('span', { class: 'plus' }, '+' + sum.added), ' ', h('span', { class: 'minus' }, '−' + sum.removed)) : null, sum.branch ? h('span', { class: 'faint' }, ' on ' + sum.branch) : null), groupBars(sum),
      h('details', { class: 'more' }, h('summary', {}, 'Show files'), h('ul', { class: 'flist' }, ...sum.files.map((f) => h('li', {}, f)), sum.fileCount > sum.files.length ? h('li', { class: 'faint' }, 'and ' + (sum.fileCount - sum.files.length) + ' more') : null))]
      : [h('p', { class: 'faint' }, 'No uncommitted changes in this folder.')];
    out.replaceChildren(head(copyBtn('Copy for another agent', () => api('/api/panel/handoff?repo=' + repo)), refreshBtn(() => drawPanel('activity', true))),
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
          out.replaceChildren(head(tabs, refreshBtn(again)), tab === 'agents' ? await coachCard(again, true) : null, body);
        } else out.replaceChildren(head(refreshBtn(again)), await scoutAuto(again), renderScout(data, runs), await scoutHistory());
        return;
      }
      out.replaceChildren(head(), loadingCard());
      if (name === 'overview') { await renderOverview(out, head); return; }
      if (name === 'setup') { const data = await api('/api/setup'); if (live()) out.replaceChildren(head(refreshBtn(again)), renderSetup(data)); return; }
      if (name === 'evals') { if (live()) await renderEvals(out, head); return; }
      if (name === 'health') { if (live()) await renderHealth(out, head); return; }
      if (name === 'library') { if (live()) await renderLibrary(out, head); return; }
      if (name === 'permissions') { if (live()) await renderPermissions(out, head); return; }
      if (name === 'savings') { const d = arg || S.savingsDays || 14; S.savingsDays = d; if (live()) await renderSavings(out, head, d); return; }
      if (name === 'activity') { await renderActivity(out, head, S.activityFilter || 'all'); return; }
      if (name === 'jobs') {
        const jobs = await api('/api/jobs').catch(() => []);
        if (jobs.length) {
          if (live()) out.replaceChildren(head(refreshBtn(again)), h('div', { class: 'groups' }, ...jobs.map((j) => h('section', { class: 'group' },
            h('header', {}, h('span', { class: 'gt' }, j.name), j.running ? h('span', { class: 'pill working' }, 'Running') : null, h('span', { class: 'faint small' }, 'runs after ' + j.after + ' each day, with ' + j.program),
              h('button', { class: 'btn small sp', disabled: j.running, onclick: async () => { try { await api('/api/jobs/' + encodeURIComponent(j.name) + '/run', { method: 'POST' }); toast('Started ' + j.name); setTimeout(again, 1500); } catch (e) { toast(e.message, true); } } }, icon('refresh'), 'Run now'))))));
          return;
        }
      }
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
    chip.replaceChildren(icon(local ? 'check' : 'lock'), h('span', { class: 'lbl' }, local ? 'Local disk mode' : need.length ? 'Cully MCP needs sign-in' : ok ? 'Cully MCP connected' : 'Cully MCP not connected'));
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
    const modal = h('div', { class: 'modal', role: 'dialog', 'aria-modal': 'true', 'aria-label': 'Local disk mode' },
      h('header', {}, h('h2', {}, 'Local disk mode'), h('button', { class: 'btn ghost small sp', onclick: closeMCP }, 'Close')),
      h('div', { class: 'mbody' },
        h('p', {}, 'Cully keeps your session history, handoffs and advice in files on this machine. Nothing is sent to a server.'),
        h('p', {}, 'To keep memory across machines and share it with a team, connect the Cully MCP:'),
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
    for (const r of (S.info && S.info.repoInfo) || []) seen.set(r.path, { path: r.path, name: r.name, git: true });
    for (const x of S.sessions || []) { const p = x.repo || x.cwd; if (p && !seen.has(p)) seen.set(p, { path: p, name: x.name, git: !!x.repo }); }
    return [...seen.values()].slice(0, limit || 6);
  }
  async function startIn(path, init) {
    try { const info = await addFolder(path, init); closeMCP(); location.hash = '#/'; if (S.route.name === 'home') mountHome(); toast('Ready to start a task in ' + info.name); } catch (e) { toast(e.message, true); }
  }
  // A folder that is not a git repository needs one first: agents work on their own branch.
  function startHere(path, isGit) {
    if (isGit) return startIn(path);
    confirmDialog({ title: 'Set up git here?', body: [path + ' is not a git repository.', 'Agents work on their own branch, so Cully will run git init and make an empty first commit. Your files are not changed.'], confirmLabel: 'Set up git and start', onConfirm: () => startIn(path, true) });
  }
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
      const pathIn = h('input', { type: 'text', placeholder: 'Or paste a full path', 'aria-label': 'Folder path', autocomplete: 'off', spellcheck: 'false' });
      pathIn.addEventListener('keydown', (e) => { if (e.key === 'Enter' && pathIn.value.trim()) load(pathIn.value.trim()); });
      body.replaceChildren(
        recent.length ? h('div', { class: 'row' }, h('span', { class: 'opt-label' }, 'Recent'), ...recent.map((r) => h('button', { class: 'chip', title: r.path, onclick: () => (r.git ? startIn(r.path) : load(r.path)) }, icon('folder'), r.name))) : null,
        h('div', { class: 'row crumbrow' }, crumbs, h('span', { class: 'sp row' },
          d && d.parent ? h('button', { class: 'btn ghost small', onclick: () => load(d.parent) }, 'Up') : null,
          h('button', { class: 'btn ghost small', onclick: () => { load(d.home || '~'); } }, 'Home'),
          h('label', { class: 'check' }, h('input', Object.assign({ type: 'checkbox', onchange: (e) => { state.showDot = e.target.checked; load(state.path); } }, state.showDot ? { checked: true } : {})), 'Show hidden folders'))),
        state.err ? h('div', { class: 'err' }, state.err) : null,
        d ? h('ul', { class: 'frows' }, ...(d.entries.length ? d.entries.map((e) => h('li', {},
          h('button', { class: 'frow', onclick: () => load(e.path) }, icon('folder'), h('span', { class: 'fn' }, e.name), e.isGit ? h('span', { class: 'chip mono' }, icon('branch'), 'git repository') : null, e.writable ? null : h('span', { class: 'chip' }, icon('lock'), 'read only')),
          e.writable ? h('button', { class: 'btn small', onclick: () => startHere(e.path, e.isGit) }, 'Start here') : null)) : [h('li', { class: 'faint' }, 'No folders here.')])) : h('p', { class: 'faint' }, 'Loading…'),
        d && d.truncated ? h('p', { class: 'faint small' }, 'Showing the first folders only. Paste a path to go further.') : null,
        h('div', { class: 'row foot' }, pathIn,
          d && d.writable ? h('button', { class: 'btn', onclick: () => startHere(d.path, d.isGit) }, icon('plus'), 'Use this folder')
            : h('span', { class: 'faint small' }, d ? 'You do not have write access to this folder, so Cully cannot work here.' : '')));
    }
    draw();
    load(state.path);
  }
  function openAddFolder() { openFolderChooser(); }

  const AGENT_LABEL = { claude: 'Claude', codex: 'Codex', cursor: 'Cursor' };
  async function startHere(sess) {
    try { await addFolder(sess.repo); location.hash = '#/'; toast('Ready to start a task in ' + sess.name); } catch (e) { toast(e.message, true); }
  }
  async function viewHistory(sess) {
    try { await addFolder(sess.repo); location.hash = '#/dashboard/status'; } catch (e) { toast(e.message, true); }
  }
  function renderSessions() {
    const box = $('sessions');
    const all = S.sessions || [];
    const list = all.filter((x) => S.showClosedSessions || !x.detached);
    const hiddenClosed = all.filter((x) => x.detached).length;
    const sig = JSON.stringify([list.map((x) => [x.pid, x.cwd, x.branch, x.via, x.detached]), hiddenClosed, !!S.showClosedSessions]);
    if (sig === S.sessSig && box.childElementCount) return;
    S.sessSig = sig;
    const names = [...new Set(mcpUsed().map((m) => AGENT_LABEL[m.agent]))].join(' and ') || 'Claude';
    const sub = 'Your ' + names + ' sessions on this computer: the ones open now first, then recent ones.';
    const kids = [toolbar('Sessions', sub, refreshBtn(async () => { S.sessions = await api('/api/sessions'); S.sessSig = ''; S.hist = null; renderSessions(); }))];
    kids.push(h('div', { id: 'active' }));
    const toggles = [];
    if (hiddenClosed || S.showClosedSessions) toggles.push(h('button', { class: 'linkbtn', onclick: () => { S.showClosedSessions = !S.showClosedSessions; S.sessSig = ''; renderSessions(); } }, S.showClosedSessions ? 'Hide sessions whose terminal was closed' : 'Show ' + hiddenClosed + (hiddenClosed === 1 ? ' session' : ' sessions') + ' whose terminal was closed'));
    if (toggles.length) kids.push(h('div', { class: 'row' }, ...toggles));
    const mine = S.agents.length;
    if (mine) kids.push(h('p', { class: 'faint small' }, mine + (mine === 1 ? ' agent was' : ' agents were') + ' started from this page. ', h('a', { class: 'ext', href: '#/' }, 'See them on the Tasks tab')));
    kids.push(h('div', { id: 'history' }));
    box.replaceChildren(...kids);
    drawActive();
    drawHistory();
    if (!S.hist) loadHistory(7);
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
  // What this session did, shown before it is resumed.
  function sessionSummary(x) {
    const lib = x.library; const kids = [];
    if (lib && lib.task) kids.push(h('p', {}, h('b', {}, 'Goal: '), lib.task));
    if (x.description) kids.push(h('p', {}, h('b', {}, 'What it did: '), x.description));
    if (lib && lib.completed) kids.push(h('p', {}, h('b', {}, 'Where it ended: '), h('span', { class: 'chip ' + (lib.completed === 'yes' ? 'ok' : lib.completed === 'no' ? 'warn' : '') }, DONE_LABEL[lib.completed] || lib.completed), lib.onTask === 'drifted' ? h('span', { class: 'chip warn' }, 'Drifted') : null));
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
  // Continue or fork a past Claude session as an agent this page runs, in the folder it started in.
  function continueSession(x, fork) {
    const ta = h('textarea', { 'aria-label': 'What to ask first', rows: '3', placeholder: 'What should it do next? Leave empty to continue where it left off.' });
    const err = h('div', { class: 'err', hidden: true });
    const go = h('button', { class: 'btn', onclick: async () => {
      go.disabled = true; err.hidden = true;
      try {
        const v = await api('/api/history/' + x.id + '/resume', { method: 'POST', body: JSON.stringify({ prompt: ta.value, fork }) });
        closeMCP(); await refresh(); location.hash = '#/agent/' + v.id;
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
    try { S.hist = await api('/api/history?days=' + days); S.histErr = ''; } catch (e) { S.histErr = e.message; }
    drawHistory();
  }
  // Sessions open right now, drawn as cards like the recent ones. When the agent
  // has recorded the session, its title and the Fork button come from there.
  function drawActive() {
    const box = $('active');
    if (!box) return;
    const list = (S.sessions || []).filter((x) => S.showClosedSessions || !x.detached);
    if (!list.length) { box.replaceChildren(h('h3', { class: 'section-h' }, 'Open now'), emptyCard('Nothing is open right now', 'Open ' + (mcpUsed().map((m) => AGENT_LABEL[m.agent]).join(' or ') || 'Claude') + ' in a terminal and it appears here. Or start an agent from this page.', 'Start a task', '#/')); return; }
    const recorded = (S.hist && S.hist.sessions) || [];
    box.replaceChildren(h('h3', { class: 'section-h' }, 'Open now'), h('ul', { class: 'hrows active' }, ...list.map((x) => {
      const rec = recorded.find((r) => r.open && r.cwd === x.cwd);
      return h('li', { class: 'hrow' + (x.detached ? ' detached' : '') },
        h('div', { class: 'hmain' },
          h('div', { class: 'ht' }, (rec && rec.title) || x.name, h('span', { class: 'chip ok' }, x.detached ? 'Terminal closed' : 'Active')),
          rec && rec.description ? h('p', { class: 'hdesc' }, rec.description) : null,
          h('div', { class: 'faint small path' }, x.cwd),
          h('div', { class: 'row' }, h('span', { class: 'chip' }, AGENT_LABEL[x.agent] || x.agent), x.branch ? h('span', { class: 'chip mono' }, icon('branch'), x.branch) : null,
            h('span', { class: 'chip' }, x.via === 'cully' ? 'In the Cully terminal' : 'In a plain terminal'),
            x.started && !x.started.startsWith('0001') ? h('span', { class: 'chip' }, 'open for ' + ago(x.started).replace(' ago', '')) : null)),
        h('div', { class: 'row hact' },
          rec && S.hist && S.hist.agent === 'claude' ? h('button', { class: 'btn small', title: 'Start a new session from this one; the original stays as it is', onclick: () => continueSession(rec, true) }, icon('branch'), 'Fork') : null,
          h('button', { class: rec ? 'btn ghost small' : 'btn small', disabled: !x.repo, title: x.repo ? '' : 'This folder is not a git repository', onclick: () => startHere(x) }, icon('plus'), 'New task here'),
          h('button', { class: 'btn ghost small', disabled: !x.repo, onclick: () => viewHistory(x) }, 'View history')));
    })));
  }
  function drawHistory() {
    drawActive();
    const box = $('history');
    if (!box) return;
    if (S.histErr) { box.replaceChildren(h('div', { class: 'err' }, S.histErr)); return; }
    const hist = S.hist;
    if (!hist) { box.replaceChildren(h('p', { class: 'faint' }, 'Loading recent sessions…')); return; }
    const label = hist.days === 0 ? 'of all time' : hist.days === 7 ? 'in the last 7 days' : 'in the last ' + hist.days + ' days';
    const parts = [h('h3', { class: 'section-h' }, 'Recent'), h('p', { class: 'faint small' }, (AGENT_LABEL[hist.agent] || hist.agent) + ' sessions ' + label + ', named from what each one did.')];
    if (hist.note) parts.push(h('p', { class: 'faint' }, hist.note));
    if (!hist.sessions.filter((x) => !x.open).length && !hist.note) parts.push(emptyCard('No recent sessions', 'Sessions you run appear here so you can continue them.', 'Start a task', '#/'));
    const copyCmd = (cmd, what) => async () => {
      try { await writeClip(cmd); toast('Copied. Paste it in a terminal to ' + what + '.'); } catch (_) { showCommand(cmd); }
    };
    const runnable = hist.agent === 'claude';
    const recent = hist.sessions.filter((x) => !x.open);
    if (recent.length) {
      parts.push(h('ul', { class: 'hrows' }, ...recent.map((x) => h('li', { class: 'hrow' },
        h('div', { class: 'hmain' },
          h('div', { class: 'ht' }, x.title || x.name, x.open ? h('span', { class: 'chip warn', title: 'Active in a terminal moments ago' }, 'Open now') : null),
          x.description ? h('p', { class: 'hdesc' }, x.description) : null,
          h('div', { class: 'faint small path' }, x.cwd),
          h('div', { class: 'row' }, x.origin ? h('span', { class: 'chip', title: 'Where this session was started' }, x.origin) : null, x.activeSec ? h('span', { class: 'chip', title: 'Time spent working in this session' }, dur(x.activeSec) + ' worked') : null, x.branch ? h('span', { class: 'chip mono' }, icon('branch'), x.branch) : null, h('span', { class: 'chip' }, ago(x.updated)), x.exists ? null : h('span', { class: 'chip warn' }, 'Folder is gone'))),
        h('div', { class: 'row hact' },
          runnable
            ? [h('button', { class: x.open ? 'btn small' : 'btn ghost small', disabled: !x.exists, title: 'Start a new session from this one here; the original stays as it is', onclick: () => continueSession(x, true) }, icon('branch'), 'Fork'),
              h('button', { class: x.open ? 'btn ghost small' : 'btn small', disabled: !x.exists || x.open, title: x.open ? 'It is open in a terminal. Fork it instead.' : 'Continue this same session here', onclick: () => continueSession(x, false) }, icon('send'), 'Resume')]
            : [h('button', { class: 'btn small', disabled: !x.exists, onclick: copyCmd(x.fork || x.resume, 'fork it') }, icon('branch'), 'Copy fork command'),
              h('button', { class: 'btn ghost small', disabled: !x.exists || x.open, onclick: copyCmd(x.resume, 'continue it') }, icon('copy'), 'Copy resume command')],
          runnable ? h('button', { class: 'linkbtn', title: 'Copy the terminal command instead', onclick: copyCmd(x.open ? x.fork : x.resume, 'continue it') }, 'Copy command') : null)))));
    }
    if (hist.older || hist.days !== 7) {
      parts.push(h('div', { class: 'row' }, hist.older ? h('button', { class: 'btn ghost', onclick: () => loadHistory(hist.days === 7 ? 30 : 0) }, 'Show older sessions') : null,
        hist.days !== 7 ? h('button', { class: 'linkbtn', onclick: () => loadHistory(7) }, 'Back to the last 7 days') : null));
    }
    box.replaceChildren(...parts);
  }

  // ---------- working together ----------
  async function startReview(a) {
    try {
      const view = await api('/api/agents/' + a.id + '/review', { method: 'POST' });
      await refresh();
      location.hash = '#/agent/' + view.id;
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
            dot(x.state), h('span', { class: 't' }, x.title), h('span', { class: 'chip mono' }, x.branch.replace(/^cully\//, '')))))
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
    const base = { desktop: false, sound: false, flash: true };
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
  function notifyUser(title, body, href) {
    toast(body ? title + ': ' + body : title);
    const p = notifyPrefs();
    if (p.sound) beep();
    if (document.hidden && p.desktop && !desktopProblem() && Notification.permission === 'granted') {
      const n = new Notification(title, { body, tag: 'cully' });
      n.onclick = () => { window.focus(); if (href) location.hash = href; n.close(); };
    }
    if (document.hidden && p.flash) startFlash(title);
  }
  function attentionKeys() {
    const keys = new Map();
    for (const a of S.agents.filter((x) => x.state === 'waiting')) keys.set('wait:' + a.id, [a.title + ' needs you', '', '#/agent/' + a.id]);
    for (const g of S.advice || []) for (const x of g.advice) if (!x.applied && !x.dismissed) keys.set('adv:' + x.id, ['New advice for ' + g.agentTitle, x.text, '#/dashboard/advisor']);
    if (memoryMode() === 'mcp') for (const m of mcpNeeding()) keys.set('auth:' + m.agent, ['Cully MCP needs sign-in', 'for ' + AGENT_NAMES[m.agent], '#/dashboard/setup']);
    return keys;
  }
  function checkAttention() {
    const keys = attentionKeys();
    if (!S.attnSeen) S.attnSeen = new Set();
    for (const [k, [t, b, href]] of keys) {
      if (S.attnSeen.has(k)) continue;
      S.attnSeen.add(k);
      if (S.attnReady) notifyUser(t, b, href);
    }
    for (const k of [...S.attnSeen]) if (!keys.has(k)) S.attnSeen.delete(k);
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
    toastAction('Get a heads-up when an agent needs you?', 'Notification settings', () => $('notifybtn').click());
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
        h('div', { class: 'menu-note' }, 'Get told when an agent needs you, advice arrives, or sign-in is needed.'),
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
    try { const r = await api('/api/setup/advisor/start', { method: 'POST' }); toast(r.message); drawPanel('setup', true); } catch (e) { toast(e.message, true); }
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
  function drawWhere() {
    const box = $('where');
    if (!S.info || !S.info.host) { box.hidden = true; return; }
    const list = S.info.repoInfo || [];
    const cur = list.find((r) => r.path === S.opts.repo) || list[0];
    const who = (S.info.user ? S.info.user + '@' : '') + S.info.host;
    box.hidden = false;
    box.replaceChildren(
      h('span', { class: 'w-item', title: 'The machine this page is working on' }, icon('term'), h('b', {}, who)),
      cur ? h('span', { class: 'w-item w-path' }, icon('folder'), h('span', { class: 'wpath' }, cur.path),
        h('button', { class: 'iconbtn', title: 'Copy the full path', 'aria-label': 'Copy the full path', onclick: () => copy(cur.path, 'path') }, icon('copy')),
        h('button', { class: 'linkbtn', onclick: () => openFolderChooser() }, 'Change'))
        : h('span', { class: 'w-item faint' }, icon('folder'), 'No folder yet'),
      cur && cur.branch ? h('span', { class: 'w-item mono' }, icon('branch'), cur.branch) : null,
      cur && cur.remote ? h('span', { class: 'w-item faint' }, cur.remote) : null);
  }
  // Opened through one agent, the page is about that agent only.
  const claudeInUse = () => !(S.mcp || []).length || mcpUsed().some((m) => m.agent === 'claude');
  function drawAuth() { $('meta').hidden = !claudeInUse(); }

  // ---------- folder switcher ----------
  function drawFolderChip() {
    const btn = $('folderbtn');
    const list = (S.info && S.info.repoInfo) || [];
    const cur = list.find((r) => r.path === S.opts.repo) || list[0];
    btn.replaceChildren(icon('folder'), h('span', { class: 'lbl' }, cur ? cur.name : 'Add a folder'));
    btn.title = cur ? 'Folder shown: ' + cur.path : 'Add a git folder';
    drawWhere();
  }
  function mountFolderMenu() {
    const btn = $('folderbtn');
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
    const mineLive = (S.sessions || []).filter((x) => !x.detached).length;
    $('sbadge').hidden = mineLive === 0;
    $('sbadge').textContent = mineLive;
    if (S.route.name === 'sessions') renderSessions();
    const unread = (S.advice || []).reduce((n, g) => n + g.advice.filter((x) => !x.applied && !x.dismissed).length, 0);
    $('dbadge').hidden = unread === 0;
    $('dbadge').textContent = unread;
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
