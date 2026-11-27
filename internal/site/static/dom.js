// Small helpers every page script shares. The layout loads this first, before any page's own scripts, so they can rely on it.
//
//   Dom.el(tag, cls, text)  a new element with an optional class and text (inserted as text, never HTML)
//   Dom.commas(n)           a whole number with thousands separators, e.g. 10000 as "10,000"
//   Dom.ARROW, Dom.REFRESH_ICON
//                           SVG markup (constants, never data): the stat boxes' arrow to the next level, and the circle arrow of a refresh button
//   Dom.statBox(label)      a pilot's stat box (a big level, a small "-> next level", the SP so far and a progress bar); box.show(level, next, percent) fills
//                           it, where next is { level, text, percent } for the next level up, or null at the top of the ladder ("max", with the bar at percent,
//                           100 if left out)
//   Dom.players(box, min, max, onPick)
//                           fills box with the [1][2][3][4] player-count buttons, from min to max; a click calls onPick(n). Returns show(n, disabled), which marks
//                           n as the chosen count (and disables every button while disabled is true)
//   Dom.costLine(cost, warchest)
//                           what something costs against the warchest: { text, short }, e.g. "Cost 1,200 SP. Warchest 5,000 → 3,800 SP.", or with short true
//                           "Cost 1,200 SP. Warchest 800 SP. Not enough funds."; a cost of 0 is "Cost 0 SP. Warchest 5,000 SP."
(function () {
  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }
  function commas(n) { return String(Math.round(Number(n) || 0)).replace(/\B(?=(\d{3})+(?!\d))/g, ','); }

  // Drawn rather than typed, so they read clearly in any font.
  var ARROW = '<svg viewBox="0 0 16 10" width="16" height="10" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M1 5h13M10 1l4 4-4 4"/></svg>';
  var REFRESH_ICON = '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M21 12a9 9 0 1 1-2.64-6.36"/><path d="M21 3v6h-6"/></svg>';

  function statBox(label) {
    var box = el('div', 'stat-box');
    var value = el('span', 'stat-value');
    var to = el('span', 'stat-to');
    var arrow = el('span', 'stat-arrow');
    arrow.setAttribute('aria-hidden', 'true');
    arrow.innerHTML = ARROW; // a constant above, never data
    var nextLevel = el('b');
    to.appendChild(arrow);
    to.appendChild(nextLevel);
    var wrap = el('span', 'stat-valwrap'); // only the value sits in the flow, so it stays centred whatever the "to" says
    wrap.appendChild(value);
    wrap.appendChild(to);
    var main = el('span', 'stat-main');
    main.appendChild(wrap);
    var nextText = el('span', 'stat-next');
    var bar = el('span', 'stat-bar');
    var fill = el('span');
    bar.appendChild(fill);
    box.appendChild(el('span', 'stat-label', label));
    box.appendChild(main);
    box.appendChild(nextText);
    box.appendChild(bar);
    box.show = function (level, next, percent) {
      value.textContent = String(level);
      to.hidden = !next;
      if (next) nextLevel.textContent = String(next.level);
      nextText.textContent = next ? next.text : 'max';
      var p = next ? next.percent : (percent === undefined ? 100 : percent);
      fill.style.width = Math.max(0, Math.min(100, Number(p) || 0)) + '%';
    };
    return box;
  }

  function players(box, min, max, onPick) {
    box.textContent = '';
    var buttons = [];
    for (var n = min; n <= max; n++) {
      (function (n) {
        var b = el('button', 'deploy-count', String(n));
        b.type = 'button';
        b.setAttribute('aria-label', n + (n === 1 ? ' player' : ' players'));
        b.addEventListener('click', function () { onPick(n); });
        box.appendChild(b);
        buttons.push(b);
      })(n);
    }
    return function show(chosen, disabled) {
      buttons.forEach(function (b, i) {
        b.setAttribute('aria-pressed', min + i === chosen ? 'true' : 'false');
        b.disabled = !!disabled;
      });
    };
  }

  function costLine(cost, warchest) {
    var after = warchest - cost, short = after < 0;
    var text = 'Cost ' + commas(cost) + ' SP. Warchest ' + commas(warchest) + (cost && !short ? ' → ' + commas(after) : '') + ' SP.' + (short ? ' Not enough funds.' : '');
    return { text: text, short: short };
  }

  window.Dom = { el: el, commas: commas, ARROW: ARROW, REFRESH_ICON: REFRESH_ICON, statBox: statBox, players: players, costLine: costLine };
})();
