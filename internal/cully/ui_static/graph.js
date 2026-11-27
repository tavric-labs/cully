// Task graph: the pure part. It turns the trace from /api/agents/{id}/graph into x and y positions and
// curve paths, and words for the header line. It touches no DOM, so it can be tested with node.
(function (root) {
  'use strict';
  const PHASE_LABEL = {
    queued: 'Queued', preparing: 'Getting ready', working: 'Working', tool: 'Waiting on a tool', 'needs-you': 'Needs you',
    helper: 'Waiting on a helper', checking: 'Checking', stuck: 'Stuck', done: 'Done', failed: 'Failed',
    limit: 'Stopped by a limit', stopped: 'Stopped', closed: 'Closed', earlier: 'Earlier steps',
  };
  const REASON_LABEL = {
    started: 'The task started', 'state': 'The state changed', 'permission-asked': 'A tool asked for permission', 'answered-allow': 'You allowed it',
    'answered-deny': 'You denied it', 'tool-started': 'A tool started', 'tool-finished': 'The tool finished', 'turn-ended': 'The turn ended',
    'user-message': 'You sent a message', nudge: 'Cully asked it to change course', 'stuck-detected': 'It looked stuck', 'helper-handed': 'Handed to a helper',
    'helper-returned': 'The helper reported back', 'resumed-on-other-harness': 'Continued in another harness', resumed: 'Continued', 'limit-hit': 'A limit was reached',
    closed: 'You closed the task', finished: 'The task finished', failed: 'The task failed', stopped: 'The task was stopped', 'checks-started': 'Cully started a check',
    'checks-finished': 'The checks ended', restart: 'The server restarted',
  };
  const reasonLabel = (r) => REASON_LABEL[r] || r || '';
  // A node with no end time is still going; Go writes a missing time as year 1.
  const isOpen = (n) => !n.end || String(n.end).startsWith('0001');
  const keyOf = (n) => n.phase + '|' + (n.tags || []).join(',');

  function dur(ms) {
    const s = Math.round(ms / 1000);
    if (s < 1) return 'under a second';
    if (s < 60) return s + ' s';
    const m = Math.round(s / 60);
    if (m < 60) return m + ' min';
    return Math.floor(m / 60) + ' h' + (m % 60 ? ' ' + (m % 60) + ' min' : '');
  }

  function nodeLabel(n) {
    if (n.phase === 'tool') return n.tool ? 'Tool: ' + n.tool : PHASE_LABEL.tool;
    if (n.phase === 'earlier') return n.summary || PHASE_LABEL.earlier;
    return PHASE_LABEL[n.phase] || n.phase;
  }

  // Runs like working, tool, working, tool, ... more than three times fold into one pair with a counter.
  function collapseLoops(nodes, keepFrom) {
    const out = []; const loops = [];
    let i = 0;
    const last = keepFrom == null ? nodes.length - 1 : keepFrom;
    while (i < nodes.length) {
      if (i + 1 < last) {
        const a = keyOf(nodes[i]); const b = keyOf(nodes[i + 1]);
        let j = i; let reps = 0;
        while (j + 1 < last && a !== b && keyOf(nodes[j]) === a && keyOf(nodes[j + 1]) === b) { reps++; j += 2; }
        if (reps > 3) {
          out.push(nodes[i], nodes[i + 1]);
          loops.push({ from: nodes[i + 1].id, to: nodes[i].id, count: reps });
          i = j; continue;
        }
      }
      out.push(nodes[i]); i++;
    }
    return { nodes: out, loops, hidden: nodes.length - out.length };
  }

  const DEFAULTS = { nodeW: 112, nodeH: 26, gapX: 26, laneH: 64, pad: 14, labelW: 96, loopLift: 22 };

  // layout returns where everything goes. opts.collapsed is a Set of lane ids whose helper lane is folded away.
  function layout(view, opts) {
    const o = Object.assign({}, DEFAULTS, opts || {});
    const collapsed = o.collapsed || new Set();
    const all = (view && view.nodes) || [];
    const col = collapseLoops(all);
    const nodes = col.nodes;
    const laneOf = (n) => n.session || 'run';
    const info = {}; (view.lanes || []).forEach((l) => { info[l.id] = l; });
    const order = []; const hasHelper = {};
    for (const n of nodes) {
      const l = laneOf(n);
      if (!order.includes(l)) order.push(l);
      if (n.phase === 'helper') hasHelper[l] = true;
    }
    const rows = [];
    for (const l of order) {
      const meta = info[l] || { id: l, label: l };
      rows.push({ id: l, label: meta.label || l, agent: meta.agent || '', helper: false });
      if (hasHelper[l]) rows.push({ id: l + '/helper', parent: l, label: collapsed.has(l) ? 'helper (folded)' : 'helper', agent: '', helper: true, folded: collapsed.has(l) });
    }
    const rowIdx = {}; rows.forEach((r, k) => { r.y = o.pad + k * o.laneH; r.visible = !r.folded; rowIdx[r.id] = k; });
    const pos = {}; const out = [];
    nodes.forEach((n, k) => {
      let row = laneOf(n);
      if (n.phase === 'helper' && hasHelper[row] && !collapsed.has(row)) row += '/helper';
      else if (n.phase === 'helper') row = laneOf(n);
      const r = rows[rowIdx[row]];
      const p = { id: n.id, node: n, row: r.id, x: o.pad + o.labelW + k * (o.nodeW + o.gapX), y: r.y + (o.laneH - o.nodeH) / 2, w: o.nodeW, h: o.nodeH, label: nodeLabel(n) };
      pos[n.id] = p; out.push(p);
    });
    // A step before a failure is only a suspect: it is marked as a hint, never as the cause.
    nodes.forEach((n, k) => {
      if (n.phase !== 'failed') return;
      for (let m = k - 1; m >= 0; m--) if (nodes[m].phase === 'tool' || nodes[m].phase === 'working') { pos[nodes[m].id].suspected = true; break; }
    });
    const edges = [];
    for (let k = 1; k < nodes.length; k++) {
      const a = pos[nodes[k - 1].id]; const b = pos[nodes[k].id];
      const x1 = a.x + a.w; const y1 = a.y + a.h / 2; const x2 = b.x; const y2 = b.y + b.h / 2;
      const dx = Math.max(8, (x2 - x1) / 2);
      edges.push({ from: a.id, to: b.id, reason: nodes[k].cause || '', reasonLabel: reasonLabel(nodes[k].cause), cross: a.row !== b.row,
        d: 'M' + x1 + ' ' + y1 + ' C' + (x1 + dx) + ' ' + y1 + ' ' + (x2 - dx) + ' ' + y2 + ' ' + x2 + ' ' + y2 });
    }
    const back = col.loops.filter((l) => pos[l.from] && pos[l.to]).map((l) => {
      const a = pos[l.from]; const b = pos[l.to];
      const xa = a.x + a.w / 2; const xb = b.x + b.w / 2; const y = a.y;
      return { from: l.from, to: l.to, count: l.count, label: 'x' + l.count, lx: (xa + xb) / 2, ly: y - o.loopLift - 3,
        d: 'M' + xa + ' ' + y + ' C' + xa + ' ' + (y - o.loopLift) + ' ' + xb + ' ' + (y - o.loopLift) + ' ' + xb + ' ' + y };
    });
    return {
      width: o.pad * 2 + o.labelW + Math.max(1, nodes.length) * (o.nodeW + o.gapX), height: o.pad * 2 + Math.max(1, rows.length) * o.laneH,
      rows: rows.filter((r) => r.visible), helpers: rows.filter((r) => r.helper).map((r) => ({ parent: r.parent, folded: !!r.folded })), nodes: out, edges, back, hidden: col.hidden, opts: o,
    };
  }

  // headline reads like: 14 steps, 2 sessions, 1 waited on you for 3 min
  function headline(view, now) {
    const nodes = (view && view.nodes) || [];
    if (!nodes.length) return 'No steps yet';
    let steps = 0; let waits = 0; let waited = 0;
    const sessions = new Set();
    for (const n of nodes) {
      steps += n.phase === 'earlier' ? (n.earlier || 1) : 1;
      sessions.add(n.session || 'run');
      if (n.phase === 'needs-you') {
        waits++;
        waited += Math.max(0, (isOpen(n) ? (now || Date.now()) : new Date(n.end).getTime()) - new Date(n.start).getTime());
      }
    }
    const parts = [steps + (steps === 1 ? ' step' : ' steps'), sessions.size + (sessions.size === 1 ? ' session' : ' sessions')];
    if (waits) parts.push(waits + ' waited' + ' on you for ' + dur(waited));
    return parts.join(', ');
  }

  // What a state did, in one sentence, from the acts recorded in it.
  function actSummary(n) {
    const acts = (n && n.acts) || [];
    const tools = acts.filter((a) => a.k === 'tool');
    const failed = tools.filter((a) => a.ok === 'fail').length;
    const said = acts.filter((a) => a.k === 'said').length;
    const you = acts.filter((a) => a.k === 'you').length;
    const files = [];
    for (const a of tools) {
      const m = /^(Edit|Write|MultiEdit|NotebookEdit): (.+)$/.exec(a.l || '');
      if (m && !files.includes(m[2])) files.push(m[2]);
    }
    const parts = [];
    if (tools.length) parts.push('ran ' + tools.length + (tools.length === 1 ? ' tool' : ' tools') + (failed ? ' (' + failed + ' failed)' : ''));
    if (files.length) parts.push('edited ' + files.slice(0, 3).join(', ') + (files.length > 3 ? ' and ' + (files.length - 3) + ' more' : ''));
    if (said) parts.push('wrote ' + said + (said === 1 ? ' message' : ' messages'));
    if (you) parts.push('you sent ' + you + (you === 1 ? ' message' : ' messages'));
    let sentence = parts.join(', ');
    if (sentence) sentence = sentence.charAt(0).toUpperCase() + sentence.slice(1);
    return { sentence, tools: tools.length, failed, said, you, files, total: (n && n.actsN) || acts.length, shown: acts.length };
  }

  // The whole trace as one bar: a segment per state, as wide as it lasted (never less than a sliver).
  function strip(view, now) {
    const t = now || Date.now();
    return ((view && view.nodes) || []).map((n) => {
      const end = isOpen(n) ? t : new Date(n.end).getTime();
      const ms = Math.max(0, end - new Date(n.start).getTime());
      return { id: n.id, phase: n.phase, ms, grow: Math.max(1, Math.round(Math.sqrt(ms / 1000) * 4)) };
    });
  }

  const api = { isOpen, PHASE_LABEL, reasonLabel, nodeLabel, dur, collapseLoops, layout, headline, actSummary, strip, DEFAULTS };
  root.CullyGraph = api;
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
})(typeof window !== 'undefined' ? window : globalThis);
