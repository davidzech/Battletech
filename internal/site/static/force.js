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
      img.src = u.icon; img.alt = u.name; img.title = u.name + (u.model ? ' ' + u.model : '');
      img.width = 42; img.height = 36;
      if (u.destroyed) img.className = 'destroyed';
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

  function load() {
    box.textContent = '';
    box.appendChild(el('p', 'muted', 'Reading your hangar…'));
    fetch('/api/campaign/force?campaign=' + encodeURIComponent(root.dataset.campaign), { credentials: 'same-origin' })
      .then(function (res) {
        if (res.ok) return res.json().then(render);
        return res.text().then(function (t) { fail(t.trim() || 'Could not read your hangar.'); });
      }, function () { fail('Could not reach the server.'); });
  }
  load();
})();
