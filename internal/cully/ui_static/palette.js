// Command palette (Ctrl+K or Cmd+K) and keyboard help (?). Built with DOM calls only. It drives the
// page through location.hash and by clicking existing elements by id, and reads GET /api/agents
// for recent tasks.
(function () {
  'use strict';
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
  const typing = (t) => !!t && (/^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName) || t.isContentEditable);
  let open = null; // { bg, restore }

  function close() {
    if (!open) return;
    const { bg, restore, onKey } = open;
    document.removeEventListener('keydown', onKey, true);
    bg.remove(); open = null;
    if (restore && restore.focus && document.contains(restore)) restore.focus();
  }

  function setTheme(name) {
    if (name === 'cully') document.documentElement.removeAttribute('data-theme'); else document.documentElement.setAttribute('data-theme', name);
    try { if (name === 'cully') localStorage.removeItem('cully-theme'); else localStorage.setItem('cully-theme', name); } catch (_) { /* storage blocked */ }
  }
  const go = (hash) => () => { location.hash = hash; };
  const clickId = (id) => () => { const el = document.getElementById(id); if (el) el.click(); };

  async function recentTasks() {
    try {
      const r = await fetch('/api/agents', { credentials: 'same-origin' });
      if (!r.ok) return [];
      const list = await r.json();
      if (!Array.isArray(list)) return [];
      return list.filter((a) => a && a.id && a.title).slice(0, 12).map((a) => ({ group: 'Recent tasks', label: String(a.title), hint: a.state || '', run: go('#/task/' + a.id) }));
    } catch (_) { return []; }
  }

  function staticItems() {
    return [
      { group: 'Go to', label: 'Tasks', run: go('#/tasks') },
      { group: 'Go to', label: 'Board', run: go('#/board') },
      { group: 'Go to', label: 'Sessions', run: go('#/sessions') },
      { group: 'Go to', label: 'Agents', run: go('#/agents') },
      { group: 'Go to', label: 'Harness', run: go('#/harness') },
      { group: 'Go to', label: 'Dashboard', run: go('#/dashboard') },
      { group: 'Actions', label: 'New task', run: go('#/') },
      { group: 'Actions', label: 'Notification settings', run: clickId('notifybtn') },
      { group: 'Actions', label: 'Keyboard shortcuts', hint: '?', run: () => setTimeout(showHelp, 0) },
      { group: 'Theme', label: 'Theme: Cully (follows system light or dark)', run: () => setTheme('cully') },
      { group: 'Theme', label: 'Theme: Harbor (cool slate, light)', run: () => setTheme('harbor') },
      { group: 'Theme', label: 'Theme: Graphite (neutral, dark)', run: () => setTheme('graphite') },
    ];
  }

  function trapTab(bg) {
    return (e) => {
      if (e.key !== 'Tab') return;
      const f = [...bg.querySelectorAll('button, input, [tabindex="0"]')].filter((x) => !x.disabled);
      if (!f.length) { e.preventDefault(); return; }
      const first = f[0], last = f[f.length - 1];
      if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
      else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
    };
  }

  function showPalette() {
    if (open) { close(); return; }
    let items = staticItems();
    let shown = [], sel = 0;
    const input = h('input', { type: 'text', class: 'cmdk-in', placeholder: 'Type a page, task or action', role: 'combobox', 'aria-expanded': 'true', 'aria-controls': 'cmdk-list', 'aria-label': 'Command palette', autocomplete: 'off', spellcheck: 'false' });
    const list = h('ul', { class: 'cmdk-list', id: 'cmdk-list', role: 'listbox', 'aria-label': 'Commands' });
    const box = h('div', { class: 'cmdk', role: 'dialog', 'aria-modal': 'true', 'aria-label': 'Command palette' }, input, list,
      h('div', { class: 'cmdk-foot' }, h('span', {}, h('kbd', {}, 'Up'), ' ', h('kbd', {}, 'Down'), ' to move'), h('span', {}, h('kbd', {}, 'Enter'), ' to choose'), h('span', {}, h('kbd', {}, 'Esc'), ' to close')));
    const bg = h('div', { class: 'cmdk-bg', onclick: (e) => { if (e.target === bg) close(); } }, box);
    const choose = (it) => { close(); if (it) it.run(); };
    function draw() {
      const q = input.value.trim().toLowerCase();
      shown = items.filter((it) => !q || (it.label + ' ' + it.group).toLowerCase().includes(q));
      if (sel >= shown.length) sel = Math.max(0, shown.length - 1);
      const kids = []; let g = '';
      shown.forEach((it, i) => {
        if (it.group !== g) { g = it.group; kids.push(h('li', { class: 'cmdk-grp', role: 'presentation' }, g)); }
        kids.push(h('li', { class: 'cmdk-it', id: 'cmdk-' + i, role: 'option', 'aria-selected': i === sel ? 'true' : 'false', onclick: () => choose(it), onmousemove: () => { if (sel !== i) { sel = i; mark(); } } },
          h('span', {}, it.label), it.hint ? h('span', { class: 'hint' }, it.hint) : null));
      });
      if (!shown.length) kids.push(h('li', { class: 'cmdk-none', role: 'presentation' }, 'Nothing matches. Try a page name such as Board.'));
      list.replaceChildren(...kids);
      mark(true);
    }
    function mark(scroll) {
      for (const el of list.querySelectorAll('.cmdk-it')) el.setAttribute('aria-selected', el.id === 'cmdk-' + sel ? 'true' : 'false');
      const cur = document.getElementById('cmdk-' + sel);
      if (cur) { input.setAttribute('aria-activedescendant', cur.id); if (scroll !== false && cur.scrollIntoView) cur.scrollIntoView({ block: 'nearest' }); } else input.removeAttribute('aria-activedescendant');
    }
    const tabs = trapTab(bg);
    const onKey = (e) => {
      if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); close(); }
      else if (e.key === 'ArrowDown') { e.preventDefault(); if (shown.length) { sel = (sel + 1) % shown.length; mark(); } }
      else if (e.key === 'ArrowUp') { e.preventDefault(); if (shown.length) { sel = (sel - 1 + shown.length) % shown.length; mark(); } }
      else if (e.key === 'Enter') { e.preventDefault(); choose(shown[sel]); }
      else tabs(e);
    };
    open = { bg, restore: document.activeElement, onKey };
    document.addEventListener('keydown', onKey, true);
    input.addEventListener('input', () => { sel = 0; draw(); });
    document.body.append(bg);
    draw(); input.focus();
    recentTasks().then((r) => { if (open && open.bg === bg) { items = items.concat(r); draw(); } });
  }

  function showHelp() {
    if (open) close();
    const rows = [
      ['Ctrl+K or Cmd+K', 'Open the command palette'],
      ['?', 'Show this list'],
      ['Esc', 'Close a dialog, menu or the palette'],
      ['Ctrl+Enter or Cmd+Enter', 'Start the task from the new-task box'],
      ['Enter', 'Send a message in a task window (Shift+Enter adds a line)'],
      ['y or n', 'Allow or deny the first question a task is waiting on'],
      ['Home or End', 'Jump to the top or bottom of a task feed'],
    ];
    const dl = h('dl', {}, ...rows.flatMap(([k, d]) => [h('dt', {}, h('kbd', {}, k)), h('dd', {}, d)]));
    const done = h('button', { class: 'btn ghost small', type: 'button', onclick: close }, 'Close');
    const box = h('div', { class: 'kbhelp', role: 'dialog', 'aria-modal': 'true', 'aria-label': 'Keyboard shortcuts' }, h('h2', {}, 'Keyboard shortcuts'), dl, h('div', { class: 'row end' }, h('span', { class: 'hint' }, 'Shortcuts are off while you type in a field.'), done));
    const bg = h('div', { class: 'cmdk-bg', onclick: (e) => { if (e.target === bg) close(); } }, box);
    const tabs = trapTab(bg);
    const onKey = (e) => { if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); close(); } else tabs(e); };
    open = { bg, restore: document.activeElement, onKey };
    document.addEventListener('keydown', onKey, true);
    document.body.append(bg);
    done.focus();
  }

  document.addEventListener('keydown', (e) => {
    if ((e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey && (e.key === 'k' || e.key === 'K')) { e.preventDefault(); showPalette(); return; }
    if (e.key === '?' && !e.ctrlKey && !e.metaKey && !e.altKey && !typing(e.target) && !open && !document.querySelector('.modal-bg')) { e.preventDefault(); showHelp(); }
  });

  // A visible entry point next to the status area, so the palette is discoverable.
  const meta = document.querySelector('header.top .meta');
  if (meta && !document.getElementById('cmdkbtn')) {
    const mac = /Mac|iPhone|iPad/.test(navigator.platform || '');
    meta.prepend(h('button', { id: 'cmdkbtn', class: 'cmdk-btn', type: 'button', 'aria-label': 'Open the command palette', title: 'Command palette', onclick: showPalette }, h('span', {}, 'Search'), h('kbd', {}, mac ? 'Cmd+K' : 'Ctrl+K')));
  }
})();
