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
    var table = el('table', 'force-table unit-table');
    var head = el('thead');
    var hr = el('tr');
    ['Unit', 'Role', 'Tons', 'BV'].forEach(function (h) { hr.appendChild(el('th', null, h)); });
    head.appendChild(hr);
    table.appendChild(head);
    var tbody = el('tbody');
    units.forEach(function (u) {
      var tr = el('tr', u.destroyed ? 'destroyed' : '');
      var cell = el('td', 'who');
      var who = el('div');
      cell.appendChild(who);
      var img = document.createElement('img');
      img.src = u.icon; img.alt = ''; img.width = 42; img.height = 36; img.loading = 'lazy';
      who.appendChild(img);
      var label = el('span', null, u.name);
      if (u.model) label.appendChild(el('small', null, ' ' + u.model));
      who.appendChild(label);
      if (u.destroyed) who.appendChild(el('span', 'tag bad', 'Destroyed'));
      else if (u.damaged) who.appendChild(el('span', 'tag', 'Damaged'));
      else if (u.unknown) who.appendChild(el('span', 'tag', 'Unknown'));
      tr.appendChild(cell);
      tr.appendChild(el('td', null, u.unknown ? '—' : u.role));
      tr.appendChild(el('td', null, u.unknown ? '—' : String(u.tons)));
      tr.appendChild(el('td', null, u.unknown ? '—' : commas(u.bv)));
      tbody.appendChild(tr);
    });
    table.appendChild(tbody);
    var foot = el('tfoot');
    var fr = el('tr');
    var total = el('td', null, units.length + (units.length === 1 ? ' unit' : ' units'));
    total.colSpan = 3;
    fr.appendChild(total);
    fr.appendChild(el('td', 'total', commas(data.bv) + ' BV'));
    foot.appendChild(fr);
    table.appendChild(foot);
    box.appendChild(table);
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
