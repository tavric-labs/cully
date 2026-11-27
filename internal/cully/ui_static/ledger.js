(() => {
  'use strict';
  // The History of changes page, the "What will change" preview on proposals, and the lifecycle chips and
  // buttons (pin, archive) for agents and proposals. app.js hands over its helpers in window.CullyKit.
  const K = () => window.CullyKit;
  const KINDS = [['', 'Everything'], ['agent', 'Agents'], ['job', 'Background jobs'], ['rule', 'Workflow rules'], ['policy', 'Tool policy']];
  const sourceText = (s) => (s === 'user' ? 'You' : s === 'scheduled' ? 'A schedule' : s.startsWith('proposal:') ? 'An advisor proposal' : s.startsWith('rollback:') ? 'A rollback' : s);

  function diffRows(changes) {
    const { h } = K();
    if (!changes || !changes.length) return null;
    return h('ul', { class: 'diffrows' }, ...changes.map((c) => h('li', {},
      h('span', { class: 'df-field' }, c.field),
      c.old ? h('span', { class: 'df-old' }, c.old) : null,
      c.old && c.new ? h('span', { class: 'df-arrow', 'aria-hidden': 'true' }, 'to') : null,
      c.new ? h('span', { class: 'df-new' }, c.new) : h('span', { class: 'faint' }, 'removed'))));
  }

  // The dry run of a proposal: computed on the server from the same data the ledger records.
  function preview(pv) {
    const { h } = K();
    if (!pv) return null;
    return h('div', { class: 'preview' }, h('b', {}, 'What will change'), h('p', { class: 'faint small' }, pv.summary), diffRows(pv.changes));
  }

  async function page(kind, again) {
    const { h, api, toast, ago, confirmDialog, emptyCard } = K();
    let list = [];
    try { list = await api('/api/ledger' + (kind ? '?kind=' + encodeURIComponent(kind) : '')); } catch (e) { return h('div', { class: 'err' }, e.message); }
    const refresh = () => { if (again) again(); };
    const doRollback = async (x, force) => {
      try {
        await api('/api/ledger/' + x.id + '/rollback', { method: 'POST', body: JSON.stringify({ force }) });
        toast('Rolled back: ' + x.summary);
        refresh();
      } catch (e) {
        if (e.status === 409 && !force) {
          setTimeout(() => confirmDialog({ title: 'Roll back anyway?', body: [e.message], confirmLabel: 'Roll back anyway', onConfirm: () => doRollback(x, true) }), 0);
          return;
        }
        throw e;
      }
    };
    const ask = (x) => confirmDialog({ title: 'Roll back this change?', body: [x.summary + '. This puts back how it was before.', 'If it has been changed since, Cully asks again before going on.'], confirmLabel: 'Roll back', onConfirm: () => doRollback(x, false) });
    const item = (x) => h('li', { class: 'tl-item tl-' + x.action + (x.rolledBack ? ' tl-undone' : '') },
      h('span', { class: 'tl-dot', 'aria-hidden': 'true' }),
      h('div', { class: 'tl-body' },
        h('div', { class: 'tl-head' }, h('b', {}, x.summary), x.rolledBack ? h('span', { class: 'chip' }, 'Rolled back') : null),
        h('div', { class: 'tl-meta faint small' }, h('time', { datetime: x.time, title: new Date(x.time).toLocaleString() }, ago(x.time)), h('span', {}, 'By ' + sourceText(x.source).toLowerCase()), h('span', { class: 'chip' }, (KINDS.find((k) => k[0] === x.kind) || ['', x.kind])[1])),
        diffRows(x.changes),
        h('div', { class: 'row' },
          x.canRollback ? h('button', { class: 'btn ghost small', onclick: () => ask(x) }, 'Roll back') : h('span', { class: 'faint small' }, x.whyNot || ''))));
    const pills = h('div', { class: 'pills', role: 'tablist', 'aria-label': 'Filter by kind' }, ...KINDS.map(([k, label]) => h('a', { class: 'pill', role: 'tab', 'aria-selected': k === (kind || '') ? 'true' : 'false', href: '#/dashboard/history' + (k ? '/' + k : '') }, label)));
    return h('div', { class: 'ledger' }, pills,
      list.length ? h('ol', { class: 'timeline' }, ...list.map(item))
        : emptyCard('No changes yet', 'When an agent, a background job, your workflow rules or your tool policy is created, changed or removed, it is listed here with a way to undo it.'));
  }

  const link = (kind) => { const { h } = K(); return h('a', { class: 'btn ghost small', href: '#/dashboard/history' + (kind ? '/' + kind : ''), title: 'See every change, and undo one' }, 'History of changes'); };

  // Agent cards
  function agentChips(a) {
    const { h } = K();
    const out = [];
    if (a.pinned) out.push(h('span', { class: 'ac-state lc-pinned', title: 'Pinned agents never go stale' }, 'Pinned'));
    if (a.lifecycle === 'stale') out.push(h('span', { class: 'ac-state lc-stale', title: 'It has not run for 30 days' }, 'Stale'));
    if (a.lifecycle === 'archived') out.push(h('span', { class: 'ac-state lc-archived' }, 'Archived'));
    return out;
  }
  function agentButtons(a, again) {
    const { h, api, toast } = K();
    const go = (what, on, msg) => async () => { try { await api('/api/identities/' + a.id + '/' + what, { method: 'POST', body: JSON.stringify({ on }) }); toast(msg); again(); } catch (e) { toast(e.message, true); } };
    return [
      h('button', { class: 'btn ghost small', title: a.pinned ? 'Let it go stale again' : 'Keep it from going stale', onclick: go('pin', !a.pinned, a.pinned ? a.name + ' is no longer pinned' : a.name + ' is pinned') }, a.pinned ? 'Unpin' : 'Pin'),
      h('button', { class: 'btn ghost small', title: 'Hide it and stop its schedule. Nothing is deleted.', onclick: go('archive', true, a.name + ' is archived. Find it under Archived agents.') }, 'Archive'),
    ];
  }
  async function archivedAgents(again) {
    const { h, api, toast } = K();
    let list = [];
    try { list = await api('/api/identities?archived=1'); } catch (_) { return null; }
    if (!list.length) return null;
    return h('details', { class: 'lib-card' }, h('summary', {}, h('b', {}, 'Archived agents'), ' ', h('span', { class: 'faint small' }, String(list.length))),
      h('ul', { class: 'hrows' }, ...list.map((a) => h('li', { class: 'hrow' }, h('div', { class: 'hmain' }, h('div', { class: 'ht' }, a.name), h('p', { class: 'faint small' }, a.role)),
        h('button', { class: 'btn ghost small', onclick: async () => { try { await api('/api/identities/' + a.id + '/archive', { method: 'POST', body: JSON.stringify({ on: false }) }); toast(a.name + ' is back'); again(); } catch (e) { toast(e.message, true); } } }, 'Restore')))));
  }

  // Proposal cards: pin and archive (or restore, in the Archived filter)
  function proposalButtons(p, again, archived) {
    const { h, api, toast } = K();
    const go = (what, on) => async () => { try { await api('/api/suggestions/' + p.id + '/' + what, { method: 'POST', body: JSON.stringify({ on }) }); again(); } catch (e) { toast(e.message, true); } };
    if (archived) return [h('button', { class: 'btn ghost small', onclick: go('archive', false) }, 'Restore')];
    return [
      h('button', { class: 'btn ghost small', title: p.pinned ? 'Let it archive itself when nobody acts on it' : 'Keep it here even if nobody acts on it for 14 days', onclick: go('pin', !p.pinned) }, p.pinned ? 'Unpin' : 'Pin'),
      h('button', { class: 'btn ghost small', title: 'Hide it for now. Nothing is deleted.', onclick: go('archive', true) }, 'Archive'),
    ];
  }

  window.CullyLedger = { page, preview, link, agentChips, agentButtons, archivedAgents, proposalButtons };
})();
