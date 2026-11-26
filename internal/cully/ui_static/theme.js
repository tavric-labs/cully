// Runs before the page paints so the chosen theme never flashes.
(function () {
  try {
    var t = localStorage.getItem('cully-theme');
    if (t === 'harbor' || t === 'graphite') document.documentElement.setAttribute('data-theme', t);
  } catch (e) { /* storage blocked: the default theme applies */ }
})();
