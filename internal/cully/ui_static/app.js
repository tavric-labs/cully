(() => {
  'use strict';
  const $ = (id) => document.getElementById(id);

  // Build DOM without ever parsing agent text as markup.
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
    es: null, feed: null, tab: 'chat', draft: '', opts: { context: true, recall: false, scout: false, mode: 'edit', repo: '' },
    diffKey: '', panelCache: {},
  };
  const PANELS = {
    advisor: 'Advice for your agents', scout: 'What the scout searched and found', handoff: 'Where the work stands',
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
  const stateLabel = (s) => (s === 'waiting' ? 'needs you' : s);
  async function copy(text, what) {
    try { await navigator.clipboard.writeText(text); toast('Copied ' + what); } catch (_) { toast('Copy is blocked here; select and copy manually', true); }
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
  function markdown(text) {
    const frag = document.createDocumentFragment();
    text.split(/```/).forEach((seg, i) => {
      if (i % 2 === 1) {
        const body = seg.replace(/^[^\n]*\n/, '').replace(/\n$/, '');
        frag.append(h('pre', {}, h('code', {}, body)));
        return;
      }
      let list = null; let para = [];
      const flush = () => { if (para.length) { frag.append(h('p', {}, ...para.flatMap((l, j) => (j ? [h('br'), ...inline(l)] : inline(l))))); para = []; } };
      for (const line of seg.split('\n')) {
        const li = /^\s*(?:[-*]|\d+\.)\s+(.*)$/.exec(line);
        if (li) { flush(); if (!list) { list = h('ul'); frag.append(list); } list.append(h('li', {}, ...inline(li[1]))); continue; }
        list = null;
        if (!line.trim()) flush(); else para.push(line);
      }
      flush();
    });
    return frag;
  }

  // ---------- routing ----------
  function parseHash() {
    const p = location.hash.replace(/^#\/?/, '').split('/');
    if (p[0] === 'agent' && p[1]) return { name: 'agent', id: p[1] };
    if (p[0] === 'mcp') return { name: 'home', mcp: p[1] || 'claude' };
    if (p[0] === 'sessions') return { name: 'sessions' };
    if (p[0] === 'board') return { name: 'board' };
    if (p[0] === 'dashboard') return { name: 'dashboard', panel: p[1] || 'status' };
    return { name: 'home' };
  }
  function route() {
    S.route = parseHash();
    const section = S.route.name === 'board' ? 'board' : S.route.name === 'dashboard' ? 'dashboard' : S.route.name === 'sessions' ? 'sessions' : 'agents';
    for (const a of $('nav').querySelectorAll('a')) a.classList.toggle('active', a.dataset.route === section);
    for (const s of ['agents', 'board', 'dashboard', 'sessions']) $('view-' + s).hidden = s !== section;
    if (S.es) { S.es.close(); S.es = null; }
    S.feed = null;
    $('stage').className = 'stage';
    renderRail();
    if (S.route.name === 'agent') mountAgent(S.route.id);
    else if (S.route.name === 'home') mountHome();
    else if (S.route.name === 'board') renderBoard();
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
    if (!kids.length) kids.push(h('div', { class: 'hint', style: null }, 'No agents yet.'));
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
    const ta = h('textarea', { id: 'prompt', placeholder: 'Describe the task. Cully gathers context, then spawns an agent in its own worktree.', 'aria-label': 'Task' });
    ta.value = S.draft;
    ta.addEventListener('input', () => { S.draft = ta.value; });
    const err = h('div', { class: 'err', hidden: true });
    const start = h('button', { class: 'btn', type: 'submit' }, 'Start agent', icon('send'));
    const segBtn = (mode, label) => {
      const b = h('button', { type: 'button', class: S.opts.mode === mode ? 'on' : '' }, label);
      b.addEventListener('click', () => { S.opts.mode = mode; for (const x of seg.children) x.classList.toggle('on', x === b); });
      return b;
    };
    const seg = h('div', { class: 'seg', role: 'group', 'aria-label': 'Mode' });
    seg.append(segBtn('edit', 'Edit'), segBtn('plan', 'Plan only'));
    const repo = S.info && S.info.repos.length > 1 ? h('select', { 'aria-label': 'Repository' }, ...S.info.repos.map((r) => h('option', { value: r, ...(r === S.opts.repo ? { selected: true } : {}) }, r.split('/').pop()))) : null;
    if (repo) repo.addEventListener('change', () => { S.opts.repo = repo.value; });
    const form = h('form', { class: 'composer-card' }, ta,
      h('div', { class: 'row end' },
        h('div', { class: 'row' }, h('span', { class: 'opt-label' }, 'Include'), toggle('context', 'Context', "Start from this project's latest journal and git state"),
          toggle('recall', 'Recall', 'Ask Cully memory what is relevant (needs the Cully MCP signed in)'),
          toggle('scout', 'Scout', 'Search the web for current practice and pitfalls'), seg, repo),
        start), err);
    ta.addEventListener('keydown', (e) => { if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) form.requestSubmit(); });
    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      err.hidden = true; start.disabled = true;
      try {
        const view = await api('/api/agents', { method: 'POST', body: JSON.stringify({ prompt: ta.value, repo: S.opts.repo || (repo ? repo.value : ''), mode: S.opts.mode, context: S.opts.context, recall: S.opts.recall, scout: S.opts.scout }) });
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
      h('h1', {}, 'What should the agents work on?'),
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
        h('li', {}, h('b', {}, 'Worktree'), 'Each agent works on its own branch, so nothing touches your checkout.'),
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
    const ta = h('textarea', { rows: '1', placeholder: 'Message this agent…', 'aria-label': 'Message' });
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
        h('button', { class: 'btn ghost small', title: 'Start a read-only agent that critiques this change', onclick: () => startReview(a) }, icon('eye'), 'Review'),
        finished ? null : h('button', { class: 'btn ghost small', title: "Send this agent's reply or changes to another agent", onclick: () => openRelay(a) }, icon('send'), 'Send to agent'),
        h('button', { class: 'btn ghost small', onclick: () => continueFrom(a) }, icon('plus'), 'Continue in new agent'),
        finished ? null : stop));
    $('sub2').replaceChildren(
      h('button', { class: 'chip mono', title: 'Copy branch name', onclick: () => copy(a.branch, 'branch') }, icon('branch'), a.branch),
      a.token ? h('span', { class: 'chip' }, 'token ' + a.token) : h('span', { class: 'chip' }, 'ambient login'),
      h('span', { class: 'chip' }, 'started ' + ago(a.created)), h('span', { class: 'chip' }, 'active ' + ago(a.updated || a.created)),
      a.files && a.files.length ? h('span', { class: 'chip' }, a.files.length + (a.files.length === 1 ? ' file changed' : ' files changed')) : null,
      openAdviceFor(a.id) ? h('button', { class: 'chip warn', title: 'See and send the advisor\'s advice', onclick: () => { location.hash = '#/dashboard/advisor'; } }, icon('spark'), openAdviceFor(a.id) + (openAdviceFor(a.id) === 1 ? ' piece of advice' : ' pieces of advice')) : null,
      (a.mcp || []).some((m) => needsAuthStatus(m.status)) ? h('button', { class: 'chip warn', title: 'This agent started without Cully MCP access', onclick: () => openMCP('claude') }, icon('alert'), 'Cully MCP needs sign-in') : null);
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
    head.replaceChildren(h('span', {}, kind === 'changes' ? 'Full change against the commit this agent started from' : 'Where this agent left off'), refreshBtn);
    try {
      if (kind === 'handoff') {
        const sum = await api('/api/agents/' + a.id + '/summary');
        body.replaceChildren(h('div', { class: 'toolbar slim' }, h('div', { class: 'actions' }, copyBtn('Copy for another agent', () => api('/api/agents/' + a.id + '/handoff')))), renderSummary(sum, { agent: a }));
        return;
      }
      const d = parseDiff(await api('/api/agents/' + a.id + '/diff'));
      if (!d.files.length && !d.head) { body.replaceChildren(h('div', { class: 'empty' }, 'No changes yet.')); return; }
      const nodes = [];
      if (d.head && !d.files.length) nodes.push(h('pre', { class: 'stat' }, d.head));
      d.files.forEach((f, i) => nodes.push(h('details', { class: 'file', ...(i < 4 ? { open: true } : {}) },
        h('summary', {}, h('span', { class: 'nm' }, f.name), h('span', { class: 'plus' }, '+' + f.add), h('span', { class: 'minus' }, '−' + f.del)),
        h('div', {}, ...f.lines.slice(0, 3000).map((l) => {
          const cls = l.startsWith('+') && !l.startsWith('+++') ? 'add' : l.startsWith('-') && !l.startsWith('---') ? 'del' : l.startsWith('@@') ? 'hunk' : /^(index|new file|deleted file|---|\+\+\+|similarity|rename)/.test(l) ? 'meta' : '';
          return h('span', { class: 'dl ' + cls }, l || ' ');
        })))));
      body.replaceChildren(...nodes);
    } catch (e) { body.replaceChildren(h('div', { class: 'err' }, e.message)); }
  }

  // ---------- board ----------
  async function renderBoard() {
    try { S.overlaps = (await api('/api/board')).overlaps || []; } catch (_) { /* banner */ }
    const title = (id) => (S.agents.find((a) => a.id === id) || {}).title || id;
    const cols = [['preparing', 'Preparing'], ['working', 'Working'], ['waiting', 'Needs you'], ['finished', 'Finished']];
    const bucket = (a) => (a.state === 'preparing' ? 'preparing' : a.state === 'working' ? 'working' : a.state === 'waiting' ? 'waiting' : 'finished');
    const kids = S.overlaps.map((o) => h('div', { class: 'overlap' }, 'Overlap: ', h('b', {}, o.file), ' is edited by ' + joinNames(o.agents.map(title)) + '. Expect a merge conflict.'));
    kids.push(h('div', { class: 'cols' }, ...cols.map(([key, label]) => {
      const list = S.agents.filter((a) => bucket(a) === key);
      return h('div', { class: 'bcol' }, h('h3', {}, label, h('span', {}, list.length)),
        ...(list.length ? list.map((a) => h('a', { class: 'card' + (a.state === 'waiting' ? ' attn' : ''), href: '#/agent/' + a.id },
          h('div', { class: 'r1' }, dot(a.state), h('span', { class: 't' }, a.title)),
          h('div', { class: 'last' }, a.last || ''),
          h('div', { class: 'row' }, h('span', { class: 'chip mono' }, a.branch.replace(/^cully\//, '')), a.token ? h('span', { class: 'chip' }, 'token ' + a.token) : null),
          a.files && a.files.length ? h('ul', { class: 'files' }, ...a.files.slice(0, 5).map((f) => h('li', {}, f)), a.files.length > 5 ? h('li', {}, '+' + (a.files.length - 5) + ' more') : null) : null))
          : [h('div', { class: 'none' }, 'Nothing here.')]));
    })));
    $('board').replaceChildren(...kids);
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
        await navigator.clipboard.writeText(await getText());
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
        h('ul', { class: 'flist' }, ...sum.files.map((f) => h('li', {}, f)), sum.fileCount > sum.files.length ? h('li', { class: 'faint' }, 'and ' + (sum.fileCount - sum.files.length) + ' more') : null)]
      : [h('p', { class: 'faint' }, 'No changes yet.')];
    kids.push(h('section', {}, h('h3', {}, o.agent ? 'What changed' : 'Changes in ' + (sum.branch || 'this repository')), ...changed));
    if (sum.activity || o.agent) kids.push(h('section', {}, h('h3', {}, 'Checks'), h('p', {}, checksSentence(sum.checks, sum.editsSinceCheck))));
    if (sum.loops > 0) kids.push(h('div', { class: 'callout' }, h('b', {}, 'Heads up. '), 'The same command failed several times in a row. Look at it before continuing.'));
    if (!o.agent && !sum.activity) kids.push(h('div', { class: 'callout' }, 'Cully has no session recorded for this project yet. Start an agent from the Agents tab and what it does shows up here.'));
    if (o.agent) kids.push(h('div', { class: 'row' }, h('button', { class: 'btn small', onclick: () => continueFrom(o.agent) }, icon('plus'), 'Continue in new agent')));
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
      h('button', { class: 'btn small', disabled: !live(g.agentState), title: live(g.agentState) ? '' : 'This agent has finished', onclick: async () => {
        try { await api('/api/agents/' + g.agentId + '/advice/' + x.id + '/apply', { method: 'POST' }); toast('Sent to ' + g.agentTitle); drawPanel('advisor', true); } catch (e) { toast(e.message, true); }
      } }, icon('send'), 'Send to ' + g.agentTitle),
      h('button', { class: 'btn ghost small', onclick: async () => { try { await api('/api/agents/' + g.agentId + '/advice/' + x.id + '/dismiss', { method: 'POST' }); drawPanel('advisor', true); } catch (e) { toast(e.message, true); } } }, 'Dismiss')));
    return h('div', { class: 'advcard ' + x.level + ' ' + state }, ...kids);
  }
  function renderAdvisor(list) {
    if (!list.length) return emptyCard('No agents yet', 'The advisor reviews what a running agent is doing and suggests what to fix or check. Start an agent to get advice.', 'Start an agent', '#/');
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
  function renderScout(list) {
    const used = list.filter((v) => v.scout.status !== 'skipped' || v.recall.status !== 'skipped');
    if (!list.length) return emptyCard('No scout runs yet', 'When you start a task with Scout turned on, Cully searches the web for current practice and shows what it searched and found here, live.', 'Start a task', '#/');
    const kids = [];
    if (!used.length) kids.push(emptyCard('Scout was off for these tasks', 'Turn on Scout or Recall when you start a task. Cully then searches the web and your memory first, and you can see what it found here.', 'Start a task', '#/'));
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
    status: ['No sessions yet', 'Cully has not recorded a session for this project. Start an agent from the Agents tab and its health shows up here.'],
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
    if (!t) return emptyCard(etitle, ebody, 'Start an agent', '#/');
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
    if (dropped.length && (!blocks.length || name === 'rescue')) return emptyCard(etitle, ebody, 'Start an agent', '#/');
    return h('div', { class: 'plaintext' }, dropped.length ? emptyCard(etitle, ebody, null) : null, ...blocks);
  }

  async function drawPanel(name, force) {
    const out = $('panel');
    const repo = S.opts.repo || '';
    const head = (...actions) => toolbar(PANEL_TITLES[name] || name, PANELS[name], folderSelect(() => drawPanel(name, true)), ...actions);
    const again = () => drawPanel(name, true);
    try {
      let data; let view;
      if (name === 'advisor' || name === 'scout') {
        data = await api(name === 'advisor' ? '/api/advice' : '/api/scout');
        const sig = JSON.stringify(data);
        if (!force && sig === S.dashSig) return;
        S.dashSig = sig;
        view = name === 'advisor' ? renderAdvisor(data) : renderScout(data);
        if (S.route.name !== 'dashboard' || S.route.panel !== name) return;
        out.replaceChildren(head(refreshBtn(again)), view);
        return;
      }
      if (name === 'handoff') {
        data = await api('/api/handoff?repo=' + encodeURIComponent(repo));
        if (S.route.name !== 'dashboard' || S.route.panel !== name) return;
        out.replaceChildren(head(refreshBtn(again), copyBtn('Copy for another agent', () => api('/api/panel/handoff?repo=' + encodeURIComponent(repo)))), renderSummary(data));
        return;
      }
      const text = await api('/api/panel/' + name + '?repo=' + encodeURIComponent(repo));
      if (S.route.name !== 'dashboard' || S.route.panel !== name) return;
      out.replaceChildren(head(refreshBtn(again), copyBtn('Copy', () => text)), renderText(name, text));
    } catch (e) {
      if (S.route.name === 'dashboard' && S.route.panel === name) out.replaceChildren(head(), h('div', { class: 'err' }, e.message));
    }
  }
  function mountDashboard(name) {
    if (!S.info) return;
    $('panels').replaceChildren(...S.info.panels.map((p) => h('a', { class: p === name ? 'on' : '', href: '#/dashboard/' + p }, h('b', {}, PANEL_TITLES[p] || p), h('span', {}, PANELS[p] || ''))));
    S.dashSig = '';
    $('panel').replaceChildren(toolbar(PANEL_TITLES[name] || name, PANELS[name]), h('p', { class: 'faint' }, 'Loading…'));
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
    drawMCPChip();
    drawMCPBanner();
    if (S.modal) renderModal();
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
      if (!S.modal) { clearInterval(loginTimer); return; }
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
  function renderModal(force) {
    if (!S.modal) return;
    if (memoryMode() === 'local') { renderLocalModal(); return; }
    const agent = S.modal.agent;
    const hiddenAgents = S.mcp.filter((m) => !m.inUse);
    const cur = S.mcp.find((m) => m.agent === agent);
    if (cur && !cur.inUse) S.showOthers = true;
    const visible = S.mcp.filter((m) => m.inUse || S.showOthers);
    const st = S.mcp.find((m) => m.agent === agent) || { agent, state: 'unknown', canPaste: false };
    const login = S.login[agent] || st.login || { state: 'idle' };
    const sig = [agent, st.state, login.state, login.url, login.message, S.pasteErr, S.showOthers, S.mcp.map((m) => m.state + m.inUse).join()].join('|');
    if (!force && sig === modalSig) return;
    modalSig = sig;
    const root = $('modal-root');
    const focusedPaste = document.activeElement && document.activeElement.id === 'paste-in';

    const tabs = h('div', { class: 'mtabs', role: 'tablist' }, ...visible.map((m) =>
      h('button', { class: m.agent === agent ? 'on' : '', role: 'tab', onclick: () => { S.modal.agent = m.agent; S.pasteErr = ''; renderModal(true); } },
        m.agent, h('span', { class: 'pill ' + (m.state === 'needs-auth' ? 'waiting' : m.state === 'connected' ? 'working' : '') }, m.state.replace('-', ' ')))),
      !S.showOthers && hiddenAgents.length ? h('button', { class: 'linkbtn', onclick: () => { S.showOthers = true; renderModal(true); } }, 'Show other agents (' + hiddenAgents.map((m) => m.agent).join(', ') + ')') : null);

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
  async function addFolder(path) {
    const info = await api('/api/repos', { method: 'POST', body: JSON.stringify({ path }) });
    S.info = await api('/api/info');
    S.opts.repo = info.path;
    return info;
  }
  function openAddFolder() {
    let value = ''; const err = h('div', { class: 'err', hidden: true });
    const input = h('input', { type: 'text', placeholder: '/home/you/projects/my-repo', 'aria-label': 'Folder path', autocomplete: 'off', spellcheck: 'false' });
    const go = async () => {
      err.hidden = true;
      try { await addFolder(input.value.trim()); closeMCP(); toast('Folder added'); if (S.route.name === 'home') mountHome(); } catch (e) { err.textContent = e.message; err.hidden = false; }
    };
    input.addEventListener('input', () => { value = input.value; });
    input.addEventListener('keydown', (e) => { if (e.key === 'Enter') go(); });
    const modal = h('div', { class: 'modal', role: 'dialog', 'aria-modal': 'true', 'aria-label': 'Add a folder' },
      h('header', {}, h('h2', {}, 'Add a folder'), h('button', { class: 'btn ghost small sp', onclick: closeMCP }, 'Close')),
      h('div', { class: 'mbody' }, h('p', {}, 'Agents can work in any git repository on this machine. Paste the full path of the folder, or any folder inside the repository.'), input, err,
        h('div', { class: 'row' }, h('button', { class: 'btn', onclick: go }, icon('plus'), 'Add folder'))));
    const bg = h('div', { class: 'modal-bg', onclick: (e) => { if (e.target === bg) closeMCP(); } }, modal);
    $('modal-root').replaceChildren(bg);
    S.modal = { agent: 'folder' };
    document.removeEventListener('keydown', escClose);
    document.addEventListener('keydown', escClose);
    input.focus();
  }

  const AGENT_LABEL = { claude: 'Claude', codex: 'Codex', cursor: 'Cursor' };
  async function startHere(sess) {
    try { await addFolder(sess.repo); location.hash = '#/'; toast('Ready to start an agent in ' + sess.name); } catch (e) { toast(e.message, true); }
  }
  async function viewHistory(sess) {
    try { await addFolder(sess.repo); location.hash = '#/dashboard/status'; } catch (e) { toast(e.message, true); }
  }
  function renderSessions() {
    const box = $('sessions');
    const list = S.sessions || [];
    const sig = JSON.stringify(list.map((x) => [x.pid, x.cwd, x.branch, x.via]));
    if (sig === S.sessSig && box.childElementCount) return;
    S.sessSig = sig;
    const kids = [toolbar('Sessions on this machine', 'Every Claude, Codex and Cursor session open on this computer, in any folder. Start a new agent in any of these folders.', refreshBtn(async () => { S.sessions = await api('/api/sessions'); S.sessSig = ''; renderSessions(); }))];
    if (!list.length) {
      kids.push(emptyCard('No sessions are open right now', 'Open Claude, Codex or Cursor in a terminal and it appears here. Or start an agent from this page.', 'Start an agent', '#/'));
    } else {
      const groups = new Map();
      for (const x of list) { const k = x.repo || x.cwd; if (!groups.has(k)) groups.set(k, []); groups.get(k).push(x); }
      const cards = [];
      for (const [key, items] of groups) {
        const first = items[0];
        cards.push(h('section', { class: 'group' },
          h('header', {}, h('span', { class: 'gt' }, first.name),
            first.branch ? h('span', { class: 'chip mono' }, icon('branch'), first.branch) : null,
            first.remote ? h('span', { class: 'chip' }, first.remote) : null,
            h('span', { class: 'sp row' },
              h('button', { class: 'btn small', disabled: !first.repo, title: first.repo ? '' : 'This folder is not a git repository', onclick: () => startHere(first) }, icon('plus'), 'New agent here'),
              h('button', { class: 'btn ghost small', disabled: !first.repo, onclick: () => viewHistory(first) }, 'View history'))),
          h('p', { class: 'faint small path' }, key),
          h('ul', { class: 'srows' }, ...items.map((x) => h('li', { class: x.detached ? 'detached' : '' }, h('span', { class: 'chip' }, AGENT_LABEL[x.agent] || x.agent),
            x.detached ? h('span', { class: 'chip warn', title: 'The process is still running but the terminal window it was started in has been closed.' }, 'Terminal closed')
              : h('span', {}, x.via === 'cully' ? 'In the Cully terminal' : 'In a plain terminal'),
            h('span', { class: 'faint' }, x.started && !x.started.startsWith('0001') ? 'open for ' + ago(x.started).replace(' ago', '') : ''),
            h('span', { class: 'faint mono-s' }, 'pid ' + x.pid))))));
      }
      kids.push(h('div', { class: 'groups' }, ...cards));
    }
    const mine = S.agents.length;
    if (mine) kids.push(h('p', { class: 'faint small' }, mine + (mine === 1 ? ' agent was' : ' agents were') + ' started from this page. ', h('a', { class: 'ext', href: '#/' }, 'See them on the Agents tab')));
    box.replaceChildren(...kids);
  }

  // ---------- working together ----------
  async function startReview(a) {
    try {
      const view = await api('/api/agents/' + a.id + '/review', { method: 'POST' });
      await refresh();
      location.hash = '#/agent/' + view.id;
      toast('Review agent started');
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
        : h('div', { class: 'callout' }, 'No other agent is running. Start one first, then send to it.');
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
      const modal = h('div', { class: 'modal', role: 'dialog', 'aria-modal': 'true', 'aria-label': 'Send to another agent' },
        h('header', {}, h('h2', {}, 'Send to another agent'), h('button', { class: 'btn ghost small sp', onclick: closeMCP }, 'Close')),
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
    $('sbadge').hidden = !(S.sessions || []).length;
    $('sbadge').textContent = (S.sessions || []).length;
    if (S.route.name === 'sessions') renderSessions();
    const unread = (S.advice || []).reduce((n, g) => n + g.advice.filter((x) => !x.applied && !x.dismissed).length, 0);
    $('dbadge').hidden = unread === 0;
    $('dbadge').textContent = unread;
    renderRail();
    if (S.route.name === 'dashboard' && (S.route.panel === 'advisor' || S.route.panel === 'scout')) drawPanel(S.route.panel, false);
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
    try { S.info = await api('/api/info'); } catch (_) { return; }
    const auth = S.info.auth || { mode: 'login', accounts: 0 };
    $('meta').textContent = auth.mode === 'subscription'
      ? 'Claude subscription, ' + auth.accounts + (auth.accounts === 1 ? ' account' : ' accounts')
      : 'Claude login on this machine';
    $('meta').title = auth.mode === 'subscription'
      ? 'Agents started here use ' + auth.accounts + ' Claude subscription token' + (auth.accounts === 1 ? '' : 's') + ' in turn. Token values are never shown.'
      : "Agents started here use this machine's own Claude login. Add a tokens file to rotate between subscription accounts.";
    S.opts.repo = S.info.repos[0] || '';
    try { S.agents = await api('/api/agents'); } catch (_) { /* banner */ }
    route();
    if (S.route.mcp) { await refreshMCP(); openMCP(S.route.mcp); }
    setInterval(refresh, 2000);
    refreshMCP();
    setInterval(refreshMCP, 25000);
  }
  boot();
})();
