// Campaign overview: a compressed list of the units in the campaign's hangar and the BV they sum to, read from MekBay
// through GET /api/campaign/force. Names are inserted as text, never HTML.
(function () {
  var box = document.getElementById('force-units');
  var root = document.getElementById('force-overview');
  if (!box || !root) return;

  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }
  function commas(n) { return String(n).replace(/\B(?=(\d{3})+(?!\d))/g, ','); }

  function fail(message) {
    box.textContent = '';
    box.appendChild(el('p', 'form-error', message));
    var retry = el('button', null, 'Try again');
    retry.type = 'button';
    retry.addEventListener('click', load);
    box.appendChild(retry);
  }

  // ---- The hover card: one floating element, filled with the unit under the pointer (or focused with the keyboard).
  var tip = el('div', 'unit-tip');
  tip.setAttribute('role', 'tooltip');
  tip.id = 'unit-tip';
  tip.hidden = true;
  document.body.appendChild(tip);

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
    if (u.destroyed) tip.appendChild(el('span', 'tag bad', 'Destroyed'));
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

  // The card hangs off the icon it describes: below it, left-aligned, or above it when there is no room underneath, and
  // kept inside the viewport horizontally.
  function showTip(u, anchor) {
    fillTip(u);
    tip.hidden = false;
    var r = anchor.getBoundingClientRect(), w = tip.offsetWidth, h = tip.offsetHeight, gap = 6;
    var left = Math.min(Math.max(8, r.left), window.innerWidth - w - 8);
    var top = r.bottom + gap;
    if (top + h > window.innerHeight - 8 && r.top - gap - h >= 8) top = r.top - gap - h;
    tip.style.left = left + 'px';
    tip.style.top = top + 'px';
  }
  function hideTip() { tip.hidden = true; }

  function render(data) {
    box.textContent = '';
    var units = data.units || [];
    if (!units.length) { box.appendChild(el('p', 'muted', 'Your hangar has no units in it yet.')); return; }
    var bv = 0, damaged = 0, destroyed = 0;
    units.forEach(function (u) {
      if (!u.unknown) bv += u.bv;
      if (u.destroyed) destroyed++; else if (u.damaged) damaged++;
    });
    var row = el('div', 'force-summary');
    // The first few units as icons (more would crowd the row; the rest are only counted).
    var CAP = 8;
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
    var total = el('span', 'total', commas(data.bv) + ' BV');
    row.appendChild(total);
    box.appendChild(row);
  }

  // The page embeds the hangar as it was last read, so it paints at once and the live read below only refreshes it. Only a run
  // that has never been read shows "Reading your hangar...".
  var shown = false;
  try {
    if (box.dataset.cached) { render(JSON.parse(box.dataset.cached)); shown = true; }
  } catch (e) { shown = false; }

  // A failed refresh keeps what is on screen and says so; with nothing on screen it is an error with a retry.
  function stale(message) {
    if (!shown) { fail(message); return; }
    var old = box.querySelector('.stale-note');
    if (old) old.remove();
    box.appendChild(el('p', 'muted stale-note', 'Showing your last known hangar. ' + message));
  }

  function load() {
    if (!shown) {
      box.textContent = '';
      box.appendChild(el('p', 'muted', 'Reading your hangar…'));
    }
    fetch('/api/campaign/force?campaign=' + encodeURIComponent(root.dataset.campaign), { credentials: 'same-origin' })
      .then(function (res) {
        if (res.ok) return res.json().then(function (data) { render(data); shown = true; });
        return res.text().then(function (t) { stale(t.trim() || 'Could not read your hangar.'); });
      }, function () { stale('Could not reach the server.'); });
  }
  load();
})();
