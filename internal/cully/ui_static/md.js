// A small, safe Markdown renderer for what agents write: headings, lists
// (nested, with task boxes), tables, quotes, fenced code with light syntax
// colour and a copy button, inline code, links, bold, italic and strike.
// Everything is built with DOM calls. No HTML string is ever parsed, and only
// http, https and mailto links are made clickable.
(function () {
  'use strict';
  function el(tag, cls, ...kids) {
    const e = document.createElement(tag);
    if (cls) e.className = cls;
    for (const k of kids.flat()) if (k != null && k !== false) e.append(k.nodeType ? k : document.createTextNode(String(k)));
    return e;
  }
  const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

  async function copyText(text) {
    if (navigator.clipboard && window.isSecureContext) { await navigator.clipboard.writeText(text); return; }
    const ta = document.createElement('textarea');
    ta.value = text; ta.setAttribute('readonly', ''); ta.style.position = 'fixed'; ta.style.opacity = '0';
    document.body.append(ta); ta.select();
    let ok = false;
    try { ok = document.execCommand('copy'); } catch (_) { ok = false; }
    ta.remove();
    if (!ok) throw new Error('blocked');
  }

  // ---- inline ----
  function safeURL(u) {
    try { const x = new URL(u, 'https://invalid.example'); return /^(https?|mailto):$/.test(x.protocol) && !/^[/#]/.test(u) ? u : ''; } catch (_) { return ''; }
  }
  function link(text, url) {
    const ok = safeURL(url);
    if (!ok) return el('span', '', text);
    const a = el('a', 'md-link', text);
    a.href = ok; a.target = '_blank'; a.rel = 'noopener noreferrer';
    return a;
  }
  function code(text) {
    const c = el('code', 'ic', text);
    if (UUID.test(text)) {
      c.classList.add('copyable'); c.title = 'Click to copy';
      c.addEventListener('click', () => { copyText(text).then(() => { c.classList.add('copied'); setTimeout(() => c.classList.remove('copied'), 900); }).catch(() => {}); });
    }
    return c;
  }
  const INLINE = [
    [/^`+([^`]|[^`][\s\S]*?[^`])`+(?!`)/, (m) => code(m[1].trim())],
    [/^\[([^\]\n]+)\]\(([^)\s]+)(?:\s+"[^"]*")?\)/, (m) => link(inline(m[1]), m[2])],
    [/^<(https?:\/\/[^>\s]+)>/, (m) => link(m[1], m[1])],
    [/^https?:\/\/[^\s<>)\]]+[^\s<>)\].,;:!?'"]/, (m) => link(m[0], m[0])],
    [/^\*\*(?=\S)([\s\S]*?\S)\*\*/, (m) => el('strong', '', inline(m[1]))],
    [/^__(?=\S)([\s\S]*?\S)__/, (m) => el('strong', '', inline(m[1]))],
    [/^~~(?=\S)([\s\S]*?\S)~~/, (m) => el('del', '', inline(m[1]))],
    [/^\*(?=\S)([^*]*?\S)\*/, (m) => el('em', '', inline(m[1]))],
    [/^_(?=\S)([^_]*?\S)_(?![A-Za-z0-9])/, (m) => el('em', '', inline(m[1]))],
  ];
  function inline(text) {
    const out = [];
    let buf = '';
    const flush = () => { if (buf) { out.push(buf); buf = ''; } };
    let i = 0;
    while (i < text.length) {
      const ch = text[i];
      if (ch === '\\' && i + 1 < text.length && /[\\`*_{}[\]()#+\-.!|~>]/.test(text[i + 1])) { buf += text[i + 1]; i += 2; continue; }
      let hit = null;
      if ('`[<*_~h'.includes(ch) && (i === 0 || !/[A-Za-z0-9]/.test(text[i - 1]) || ch === '`' || ch === '[' || ch === '<')) {
        const rest = text.slice(i);
        for (const [re, make] of INLINE) { const m = re.exec(rest); if (m) { hit = [m, make]; break; } }
      }
      if (hit) { flush(); out.push(hit[1](hit[0])); i += hit[0][0].length; } else { buf += ch; i += 1; }
    }
    flush();
    return out;
  }

  // ---- code ----
  const KW = {
    c: 'if else for while return break continue switch case default do struct enum union typedef static const void int char long short unsigned signed float double sizeof goto extern volatile',
    go: 'package import func return if else for range switch case default break continue go defer select chan map struct interface type var const fallthrough nil true false',
    js: 'const let var function return if else for while do switch case default break continue new class extends import export from async await try catch finally throw typeof instanceof in of this null undefined true false yield static get set',
    py: 'def class return if elif else for while in not and or is import from as with try except finally raise pass break continue lambda yield None True False async await self global nonlocal',
    sh: 'if then else elif fi for while do done case esac function in return export local echo cd exit set unset source',
    sql: 'select from where group by order having join left right inner outer on insert into values update set delete create table drop alter index primary key not null and or limit as distinct union all',
    rs: 'fn let mut pub struct enum impl trait use mod match if else for while loop return break continue move ref self Self crate super as const static type where async await true false',
  };
  const LANG = { go: 'go', golang: 'go', js: 'js', javascript: 'js', jsx: 'js', ts: 'js', typescript: 'js', tsx: 'js', json: 'js', py: 'py', python: 'py', sh: 'sh', bash: 'sh', shell: 'sh', zsh: 'sh', yaml: 'py', yml: 'py', toml: 'py', sql: 'sql', c: 'c', cpp: 'c', 'c++': 'c', h: 'c', java: 'c', rs: 'rs', rust: 'rs' };
  const KWSET = Object.fromEntries(Object.entries(KW).map(([k, v]) => [k, new Set(v.split(' '))]));
  function highlight(source, lang) {
    const fam = LANG[(lang || '').toLowerCase()];
    if (!fam) return [source];
    const hash = fam === 'py' || fam === 'sh';
    const comment = fam === 'sql' ? '--[^\\n]*' : (hash ? '#[^\\n]*' : '\\/\\/[^\\n]*');
    const re = new RegExp('(' + comment + (hash ? '' : '|\\/\\*[\\s\\S]*?\\*\\/') + ')|("(?:\\\\.|[^"\\\\\\n])*"|\'(?:\\\\.|[^\'\\\\\\n])*\'|`(?:\\\\.|[^`\\\\])*`)|(\\b\\d[\\d_.xa-fA-F]*\\b)|(\\b[A-Za-z_][A-Za-z0-9_]*\\b)', 'g');
    const out = [];
    let last = 0; let m;
    const kws = KWSET[fam];
    while ((m = re.exec(source))) {
      if (m.index > last) out.push(source.slice(last, m.index));
      if (m[1]) out.push(el('span', 'tok-c', m[1]));
      else if (m[2]) out.push(el('span', 'tok-s', m[2]));
      else if (m[3]) out.push(el('span', 'tok-n', m[3]));
      else if (kws.has(fam === 'sql' ? m[4].toLowerCase() : m[4])) out.push(el('span', 'tok-k', m[4]));
      else out.push(m[4]);
      last = m.index + m[0].length;
    }
    if (last < source.length) out.push(source.slice(last));
    return out;
  }
  function diffLines(source) {
    return source.split('\n').flatMap((l, i, a) => [el('span', l.startsWith('+') && !l.startsWith('+++') ? 'dl-add' : l.startsWith('-') && !l.startsWith('---') ? 'dl-del' : l.startsWith('@@') ? 'dl-hunk' : '', l), i < a.length - 1 ? '\n' : '']);
  }
  // A ```mermaid block is drawn as a diagram in a sandboxed frame (/diagram/frame.html), which has
  // its own relaxed style rules. The source stays one click away, and it is what shows if the
  // diagram cannot be drawn. A reply that is still streaming is drawn only once it settles.
  const isDark = () => { const m = /\d+/g; const c = (getComputedStyle(document.body).backgroundColor.match(m) || [255, 255, 255]).map(Number); return 0.299 * c[0] + 0.587 * c[1] + 0.114 * c[2] < 128; };
  function diagramBlock(source) {
    const src = source.trim();
    const frame = el('iframe', 'dg-frame');
    frame.setAttribute('sandbox', 'allow-scripts');
    frame.setAttribute('title', 'Diagram');
    frame.setAttribute('loading', 'lazy');
    frame.src = '/diagram/frame.html';
    const note = el('div', 'dg-note', 'Drawing the diagram');
    const code = codeBlock(src, 'mermaid', true); code.hidden = true;
    const toggle = el('button', 'btn ghost small', 'Source'); toggle.type = 'button';
    toggle.addEventListener('click', () => { code.hidden = !code.hidden; toggle.textContent = code.hidden ? 'Source' : 'Hide source'; });
    const box = el('div', 'diagram', el('div', 'cb-head', el('span', 'cb-lang', 'diagram'), toggle), el('div', 'dg-stage', frame, note), code);
    let timer = 0; let done = false;
    const failed = (why) => { done = true; frame.hidden = true; note.hidden = false; note.textContent = 'Could not draw this diagram' + (why ? ': ' + why : '') + '. The source is below.'; note.classList.add('bad'); code.hidden = false; toggle.hidden = true; };
    const onMsg = (e) => {
      if (e.source !== frame.contentWindow || !e.data) return;
      if (e.data.type === 'ready') { clearTimeout(timer); timer = setTimeout(() => { try { frame.contentWindow.postMessage({ type: 'render', src, dark: isDark() }, '*'); } catch (_) { failed(''); } }, 350); }
      else if (e.data.type === 'size' && typeof e.data.h === 'number') { done = true; note.hidden = true; frame.style.height = Math.min(Math.max(e.data.h + 8, 60), 900) + 'px'; }
      else if (e.data.type === 'error') failed(String(e.data.msg || ''));
    };
    window.addEventListener('message', onMsg);
    // stop listening once the block has been on the page and left it, as a streaming reply re-renders
    let seen = false;
    const gone = setInterval(() => { if (box.isConnected) seen = true; else if (seen) { clearInterval(gone); clearTimeout(timer); window.removeEventListener('message', onMsg); } }, 3000);
    setTimeout(() => { if (!done && box.isConnected) failed('it took too long'); }, 15000);
    return box;
  }
  function codeBlock(source, lang, plain) {
    if (lang === 'mermaid' && !plain) return diagramBlock(source);
    const btn = el('button', 'cb-copy', 'Copy');
    btn.type = 'button';
    btn.addEventListener('click', async () => { try { await copyText(source); btn.textContent = 'Copied'; } catch (_) { btn.textContent = 'Select and copy'; } setTimeout(() => { btn.textContent = 'Copy'; }, 1400); });
    const body = lang === 'diff' || lang === 'patch' ? diffLines(source) : highlight(source, lang);
    return el('div', 'codeblock', el('div', 'cb-head', el('span', 'cb-lang', lang || 'text'), btn), el('pre', '', el('code', '', body)));
  }

  // ---- blocks ----
  const FENCE = /^\s{0,3}(`{3,}|~{3,})\s*([\w+#.-]*)[^\n]*$/;
  const HEAD = /^\s{0,3}(#{1,6})\s+(.*?)\s*#*\s*$/;
  const HR = /^\s{0,3}([-*_])(\s*\1){2,}\s*$/;
  const LIST = /^(\s*)([-*+]|\d{1,9}[.)])\s+(.*)$/;
  const QUOTE = /^\s{0,3}>\s?(.*)$/;
  const TSEP = /^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$/;
  const isTableStart = (lines, i) => i + 1 < lines.length && lines[i].includes('|') && TSEP.test(lines[i + 1]) && lines[i + 1].includes('-');
  const cells = (row) => { let r = row.trim(); if (r.startsWith('|')) r = r.slice(1); if (r.endsWith('|') && !r.endsWith('\\|')) r = r.slice(0, -1); return r.split(/(?<!\\)\|/).map((c) => c.trim().replace(/\\\|/g, '|')); };
  const startsBlock = (lines, i) => FENCE.test(lines[i]) || HEAD.test(lines[i]) || HR.test(lines[i]) || QUOTE.test(lines[i]) || LIST.test(lines[i]) || isTableStart(lines, i);

  function table(lines, i) {
    const head = cells(lines[i]);
    const align = cells(lines[i + 1]).map((c) => (c.startsWith(':') && c.endsWith(':') ? 'center' : c.endsWith(':') ? 'right' : ''));
    const rows = [];
    let j = i + 2;
    while (j < lines.length && lines[j].trim() && lines[j].includes('|')) { rows.push(cells(lines[j])); j++; }
    const cell = (tag, txt, k) => { const c = el(tag, '', inline(txt)); if (align[k]) c.style.textAlign = align[k]; return c; };
    const t = el('table', 'md-table', el('thead', '', el('tr', '', head.map((c, k) => cell('th', c, k)))), el('tbody', '', rows.map((r) => el('tr', '', head.map((_, k) => cell('td', r[k] || '', k))))));
    return [el('div', 'md-tablewrap', t), j];
  }

  function list(lines, i, baseIndent) {
    const first = LIST.exec(lines[i]);
    const ordered = /\d/.test(first[2]);
    const ul = el(ordered ? 'ol' : 'ul', 'md-list');
    if (ordered && parseInt(first[2], 10) !== 1) ul.start = parseInt(first[2], 10);
    let j = i;
    while (j < lines.length) {
      const m = LIST.exec(lines[j]);
      if (!m || m[1].length !== baseIndent) break;
      let text = m[3];
      const li = el('li', '');
      const task = /^\[([ xX])\]\s+(.*)$/.exec(text);
      if (task) { const cb = el('input', 'md-task'); cb.type = 'checkbox'; cb.disabled = true; cb.checked = task[1] !== ' '; li.append(cb); text = task[2]; li.classList.add('task'); }
      const body = [text];
      j++;
      const nested = [];
      while (j < lines.length) {
        const nm = LIST.exec(lines[j]);
        if (nm && nm[1].length > baseIndent) { const [sub, nj] = list(lines, j, nm[1].length); nested.push(sub); j = nj; continue; }
        if (lines[j].trim() && !nm && /^\s{2,}/.test(lines[j]) && !startsBlock(lines, j)) { body.push(lines[j].trim()); j++; continue; }
        break;
      }
      li.append(...body.flatMap((l, k) => (k ? [document.createElement('br'), ...inline(l)] : inline(l))), ...nested);
      ul.append(li);
    }
    return [ul, j];
  }

  function blocks(src) {
    const lines = src.replace(/\r\n?/g, '\n').split('\n');
    const out = [];
    let i = 0;
    while (i < lines.length) {
      const line = lines[i];
      if (!line.trim()) { i++; continue; }
      let m = FENCE.exec(line);
      if (m) {
        const fence = m[1]; const lang = m[2]; const buf = [];
        i++;
        while (i < lines.length && !new RegExp('^\\s{0,3}' + fence[0] + '{' + fence.length + ',}\\s*$').test(lines[i])) buf.push(lines[i++]);
        i++;
        out.push(codeBlock(buf.join('\n'), lang));
        continue;
      }
      if ((m = HEAD.exec(line))) { const n = m[1].length; out.push(el('h' + n, 'md-h', inline(m[2]))); i++; continue; }
      if (HR.test(line)) { out.push(el('hr', 'md-hr')); i++; continue; }
      if (isTableStart(lines, i)) { const [t, j] = table(lines, i); out.push(t); i = j; continue; }
      if (QUOTE.test(line)) {
        const buf = [];
        while (i < lines.length && QUOTE.test(lines[i])) buf.push(QUOTE.exec(lines[i++])[1]);
        out.push(el('blockquote', 'md-quote', blocks(buf.join('\n'))));
        continue;
      }
      if ((m = LIST.exec(line))) { const [l, j] = list(lines, i, m[1].length); out.push(l); i = j; continue; }
      const buf = [];
      while (i < lines.length && lines[i].trim() && !(buf.length && startsBlock(lines, i))) buf.push(lines[i++]);
      out.push(el('p', 'md-p', buf.flatMap((l, k) => (k ? [document.createElement('br'), ...inline(l.trim())] : inline(l.trim())))));
    }
    return out;
  }

  window.CullyMD = {
    render(text) { const f = document.createDocumentFragment(); f.append(...blocks(String(text == null ? '' : text))); return f; },
  };
})();
