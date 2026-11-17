// The one-line hangar summary card shared by the campaign overview and the Manage page's Hangar section: up to eight unit icons (each with a
// hover card of that unit's details), the unit count, damaged/destroyed counts and the total base BV in big yellow, so the state of the force
// reads at a glance. Names are inserted as text, never HTML.
//
//   ForceSummary.build({ units: [...hangar unit views], bv: total }) -> HTMLElement, or null when there are no units
//   ForceSummary.stats(data) -> the same facts as plain labelled number boxes (UNITS, BASE BV, and DAMAGED / DESTROYED when there are any), with no icons
(function () {
  var CAP = 8; // icons shown; more would crowd the row, the rest are only counted

  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }
  function commas(n) { return String(n).replace(/\B(?=(\d{3})+(?!\d))/g, ','); }

  // ---- The hover card: one floating element for the page, filled with the unit under the pointer (or focused with the keyboard).
  var tip = null;
  function ensureTip() {
    if (tip) return tip;
    tip = el('div', 'unit-tip');
    tip.setAttribute('role', 'tooltip');
    tip.id = 'unit-tip';
    tip.hidden = true;
    document.body.appendChild(tip);
    return tip;
  }

  function fillTip(u) {
    tip.textContent = '';
    var head = el('div', 'tip-head');
    var img = document.createElement('img');
    img.src = u.icon; img.alt = ''; img.width = 42; img.height = 36;
    head.appendChild(img);
    var names = el('div');
    names.appendChild(el('b', null, u.name));
    if (u.model) names.appendChild(el('span', 'tip-model', u.model));
    head.appendChild(names);
    tip.appendChild(head);
    if (u.condition === 'truly') tip.appendChild(el('span', 'tag bad', 'Truly destroyed'));
    else if (u.destroyed) tip.appendChild(el('span', 'tag bad', 'Destroyed'));
    else if (u.damaged) tip.appendChild(el('span', 'tag', 'Damaged'));
    var facts = el('dl', 'tip-facts');
    function fact(k, v) { facts.appendChild(el('dt', null, k)); facts.appendChild(el('dd', null, v)); }
    if (u.unknown) {
      fact('Catalog', 'Unknown unit, so its BV is unknown');
    } else {
      fact('Role', u.role || '—');
      if (u.class) fact('Class', u.class);
      fact('Tonnage', String(u.tons));
      fact('Base BV', commas(u.bv));
    }
    if (u.pilot) fact('Pilot', u.pilot);
    tip.appendChild(facts);
  }

  // The card hangs off the icon it describes: below it, left-aligned, or above it when there is no room underneath, and kept inside the
  // viewport horizontally.
  function showTip(u, anchor) {
    ensureTip();
    fillTip(u);
    tip.hidden = false;
    var r = anchor.getBoundingClientRect(), w = tip.offsetWidth, h = tip.offsetHeight, gap = 6;
    var left = Math.min(Math.max(8, r.left), window.innerWidth - w - 8);
    var top = r.bottom + gap;
    if (top + h > window.innerHeight - 8 && r.top - gap - h >= 8) top = r.top - gap - h;
    tip.style.left = left + 'px';
    tip.style.top = top + 'px';
  }
  function hideTip() { if (tip) tip.hidden = true; }

  function build(data) {
    var units = (data && data.units) || [];
    if (!units.length) return null;
    var bv = 0, damaged = 0, destroyed = 0;
    units.forEach(function (u) {
      if (!u.unknown) bv += u.bv;
      if (u.destroyed) destroyed++; else if (u.damaged) damaged++;
    });
    if (typeof data.bv === 'number') bv = data.bv;

    var row = el('div', 'force-summary');
    var icons = el('span', 'icons');
    units.slice(0, CAP).forEach(function (u) {
      var img = document.createElement('img');
      img.src = u.icon; img.alt = u.name + (u.model ? ' ' + u.model : '');
      img.width = 42; img.height = 36;
      img.tabIndex = 0;
      img.setAttribute('aria-describedby', 'unit-tip');
      if (u.destroyed) img.className = 'destroyed';
      img.addEventListener('mouseenter', function () { showTip(u, img); });
      img.addEventListener('mouseleave', hideTip);
      img.addEventListener('focus', function () { showTip(u, img); });
      img.addEventListener('blur', hideTip);
      icons.appendChild(img);
    });
    if (units.length > CAP) icons.appendChild(el('span', 'more', '+' + (units.length - CAP)));
    row.appendChild(icons);
    row.appendChild(el('b', null, units.length + (units.length === 1 ? ' unit' : ' units')));
    var notes = [];
    if (damaged) notes.push(damaged + ' damaged');
    if (destroyed) notes.push(destroyed + ' destroyed');
    if (notes.length) row.appendChild(el('span', 'notes', notes.join(' · ')));
    row.appendChild(el('span', 'total', commas(bv) + ' BV'));
    return row;
  }

  // The facts without the icons, for a page where the unit rows are right underneath: big yellow numbers with small-caps labels, in the
  // style of the pilots' stat boxes. Damaged and destroyed boxes appear only when there are any.
  function stats(data) {
    var units = (data && data.units) || [];
    if (!units.length) return null;
    var bv = 0, damaged = 0, destroyed = 0;
    units.forEach(function (u) {
      if (!u.unknown) bv += u.bv;
      if (u.destroyed) destroyed++; else if (u.damaged) damaged++;
    });
    if (typeof data.bv === 'number') bv = data.bv;
    var row = el('div', 'hangar-stats');
    function box(label, value, cls) {
      var b = el('div', 'hangar-stat' + (cls ? ' ' + cls : ''));
      b.appendChild(el('span', 'hangar-stat-label', label));
      b.appendChild(el('span', 'hangar-stat-value', value));
      row.appendChild(b);
    }
    box('Units', String(units.length));
    box('Base BV', commas(bv));
    if (damaged) box('Damaged', String(damaged), 'warn');
    if (destroyed) box('Destroyed', String(destroyed), 'bad');
    return row;
  }

  window.ForceSummary = { build: build, stats: stats };
})();
