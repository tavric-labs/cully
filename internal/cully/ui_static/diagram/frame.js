// Draws one Mermaid diagram for the page that embeds this frame. The frame is sandboxed without
// same-origin access, so the page's cookies, token and DOM are out of reach of the diagram code.
// It accepts a message only from its parent, draws in Mermaid's strict mode, and reports its size.
(function () {
  'use strict';
  var host = null;
  var counter = 0;
  function post(msg) { parent.postMessage(msg, '*'); }
  function size() { post({ type: 'size', h: Math.ceil(document.documentElement.scrollHeight) }); }
  window.addEventListener('message', function (e) {
    if (e.source !== parent) return;
    var d = e.data;
    if (!d || d.type !== 'render' || typeof d.src !== 'string' || d.src.length > 20000) return;
    host = host || document.getElementById('host');
    var m = window.mermaid;
    if (!m) { post({ type: 'error', msg: 'The diagram library did not load' }); return; }
    m.initialize({ startOnLoad: false, securityLevel: 'strict', theme: d.dark ? 'dark' : 'neutral', fontFamily: 'system-ui, sans-serif', flowchart: { htmlLabels: false }, sequence: { useMaxWidth: true } });
    m.render('cully-d' + (++counter), d.src).then(function (r) {
      var doc = new DOMParser().parseFromString(r.svg, 'image/svg+xml');
      var svg = doc.documentElement;
      if (!svg || svg.nodeName.toLowerCase() !== 'svg' || doc.getElementsByTagName('parsererror').length) throw new Error('The diagram could not be drawn');
      host.replaceChildren(document.importNode(svg, true));
      size();
      if (window.ResizeObserver && !window.__ro) { window.__ro = new ResizeObserver(size); window.__ro.observe(document.body); }
    }).catch(function (err) {
      var t = String((err && err.message) || err).split('\n')[0].slice(0, 160);
      post({ type: 'error', msg: t });
    });
  });
  post({ type: 'ready' });
})();
