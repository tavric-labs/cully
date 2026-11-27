// Setup page: the pure part. Diff folding, progress words, bar scaling and the plain wording of
// estimates. It touches no DOM, so it can be tested with node.
(function (root) {
  'use strict';
  const num = (n) => (typeof n === 'number' && isFinite(n) && n > 0 ? n : 0);
  const plural = (n, one, many) => n + ' ' + (n === 1 ? one : many || one + 's');

  // Keep changed lines, and up to ctx unchanged lines around them; fold the rest into one row each.
  function collapseDiff(diff, ctx) {
    const keep = ctx == null ? 2 : ctx;
    const rows = Array.isArray(diff) ? diff : [];
    const show = rows.map((r) => r.t === 'add' || r.t === 'del');
    const near = rows.map(() => false);
    rows.forEach((r, i) => { if (show[i]) for (let k = Math.max(0, i - keep); k <= Math.min(rows.length - 1, i + keep); k++) near[k] = true; });
    const out = [];
    let hidden = 0;
    rows.forEach((r, i) => {
      if (near[i]) {
        if (hidden) { out.push({ t: 'fold', n: hidden }); hidden = 0; }
        out.push({ t: r.t === 'add' || r.t === 'del' ? r.t : 'same', l: r.l == null ? '' : String(r.l) });
      } else hidden++;
    });
    if (hidden) out.push({ t: 'fold', n: hidden });
    return out;
  }
  function diffCounts(diff) {
    let add = 0; let del = 0;
    for (const r of Array.isArray(diff) ? diff : []) { if (r.t === 'add') add++; else if (r.t === 'del') del++; }
    return { add, del };
  }
  function diffSentence(diff) {
    const c = diffCounts(diff);
    if (!c.add && !c.del) return 'No changes to your rules.';
    const parts = [];
    if (c.add) parts.push(plural(c.add, 'line') + ' added');
    if (c.del) parts.push(plural(c.del, 'line') + ' removed');
    return parts.join(', ');
  }

  function progress(index, total) {
    if (!total) return 'No questions';
    return 'Question ' + (Math.min(Math.max(index, 0), total - 1) + 1) + ' of ' + total;
  }
  // answers: id to string, null (skipped) or undefined (not touched yet).
  function tally(questions, answers) {
    let answered = 0; let skipped = 0;
    for (const q of questions || []) {
      const a = (answers || {})[q.id];
      if (a === null) skipped++; else if (typeof a === 'string' && a.trim()) answered++;
    }
    return { answered, skipped, left: (questions || []).length - answered - skipped };
  }
  function groups(questions) {
    const seen = []; for (const q of questions || []) if (q.group && !seen.includes(q.group)) seen.push(q.group);
    return seen;
  }

  // Bars: the biggest harness fills the bar, a tiny one still shows a sliver, zero shows none.
  function barScale(byHarness, key, order) {
    const ids = order || Object.keys(byHarness || {});
    const vals = ids.map((id) => num(((byHarness || {})[id] || {})[key]));
    const max = Math.max(0, ...vals);
    return ids.map((id, i) => ({ id, value: vals[i], pct: !max || !vals[i] ? 0 : Math.max(4, Math.round((vals[i] / max) * 100)) }));
  }

  function tokens(n) {
    n = num(n);
    if (n >= 1e9) return (n / 1e9).toFixed(n >= 1e10 ? 0 : 1).replace(/\.0$/, '') + 'B';
    if (n >= 1e6) return (n / 1e6).toFixed(n >= 1e7 ? 0 : 1).replace(/\.0$/, '') + 'M';
    if (n >= 1e3) return Math.round(n / 1e3) + 'k';
    return String(Math.round(n));
  }
  function hours(n) {
    n = num(n);
    if (n && n < 1) return Math.max(1, Math.round(n * 60)) + ' min';
    return (n >= 10 ? String(Math.round(n)) : n.toFixed(1).replace(/\.0$/, '')) + ' h';
  }
  function basisSentence(b) {
    b = b || {};
    if (!num(b.sessions)) return 'No sessions found yet.';
    return 'From ' + plural(b.sessions, 'session') + (num(b.days) ? ' over ' + plural(b.days, 'day') : '') + ', about ' + hours(b.hours) + ' and ' + tokens(b.tokens) + ' tokens';
  }
  function saves(min) {
    min = Math.round(num(min));
    if (!min) return '';
    if (min >= 90) return 'saves about ' + (Math.round(min / 6) / 10) + ' h a week';
    return 'saves about ' + min + ' min a week';
  }
  function costs(t) {
    t = num(t);
    return t ? 'costs about ' + tokens(t) + ' tokens a run' : '';
  }
  const MIN_SESSIONS = 20;
  const enough = (b) => num((b || {}).sessions) >= MIN_SESSIONS;
  function evidenceWord(n) { n = num(n); return n ? 'Seen in ' + plural(n, 'session') : ''; }

  // Whether the server counts a step as done, so a tick can show without blocking anything.
  function stepDone(id, st) {
    const s = ((st || {}).steps || []).find((x) => x.id === id);
    return !!(s && s.done);
  }

  const api = { collapseDiff, diffCounts, diffSentence, progress, tally, groups, barScale, tokens, hours, basisSentence, saves, costs, enough, evidenceWord, stepDone, MIN_SESSIONS };
  root.CullySetup = api;
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
})(typeof window !== 'undefined' ? window : globalThis);
