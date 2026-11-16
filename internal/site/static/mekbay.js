// Scenario page: the deploy popup. It lists the mechs in the player's MekBay hangar, lets them pick the ones going on
// this mission (within the scenario's BV limit), and asks the server to sort the hangar into Deployed and Hangar groups
// and set up the OpFor operation. The server checks everything again; this only keeps the player from a doomed request.
(function () {
  var dialog = document.getElementById('deploy-dialog');
  var openBtn = document.getElementById('deploy-open');
  if (!dialog || !openBtn || typeof dialog.showModal !== 'function') return;

  var list = document.getElementById('deploy-list');
  var meter = document.getElementById('deploy-meter');
  var status = document.getElementById('deploy-status');
  var error = document.getElementById('deploy-error');
  var go = document.getElementById('deploy-go');
  var resetRow = document.getElementById('deploy-reset-row');
  var reset = document.getElementById('deploy-reset');

  var limit = parseInt(dialog.dataset.limit, 10) || 0;
  var canModify = dialog.dataset.canModify === '1';
  var query = 'campaign=' + encodeURIComponent(dialog.dataset.campaign) + '&scenario=' + encodeURIComponent(dialog.dataset.scenario);
  var units = [];
  var selected = {};
  var touched = false; // the player has ticked or unticked a unit, so a fresh read must not undo their choice
  var busy = false;
  // The hangar as the server last read it from MekBay, embedded in the page: shown at once when the popup opens, then replaced
  // by the live read.
  var cachedData = null;
  try { cachedData = JSON.parse(dialog.dataset.cached || 'null'); } catch (e) { cachedData = null; }

  function fmt(n) { return Number(n).toLocaleString('en-US'); }

  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }

  function totals() {
    var count = 0, bv = 0;
    units.forEach(function (u) { if (selected[u.id]) { count++; bv += u.bv; } });
    return { count: count, bv: bv };
  }

  function update() {
    var t = totals();
    var over = limit > 0 && t.bv > limit;
    meter.textContent = t.count + ' selected · ' + fmt(t.bv) + (limit ? ' / ' + fmt(limit) : '') + ' BV' + (limit ? '' : ' (no limit)');
    meter.classList.toggle('over', over);
    go.disabled = busy || !canModify || t.count === 0 || over;
  }

  function row(u) {
    var unavailable = u.destroyed || u.unknown;
    var li = el('li', 'deploy-unit' + (unavailable ? ' unavailable' : '') + (selected[u.id] ? ' picked' : ''));
    var label = el('label');
    var box = document.createElement('input');
    box.type = 'checkbox';
    box.checked = !!selected[u.id];
    box.disabled = unavailable;
    box.addEventListener('change', function () {
      touched = true;
      if (box.checked) selected[u.id] = true; else delete selected[u.id];
      li.classList.toggle('picked', box.checked);
      error.textContent = '';
      update();
    });
    label.appendChild(box);
    label.appendChild(MechRow.build(u));
    li.appendChild(label);
    return li;
  }

  function render() {
    list.textContent = '';
    units.forEach(function (u) { list.appendChild(row(u)); });
    update();
  }

  // Shows a hangar (cached or live). A choice the player has already made is kept for the units that are still there; before
  // that, the units already in the Deployed group are ticked.
  function apply(data) {
    var before = selected;
    units = data.units || [];
    if (data.limit) limit = data.limit;
    selected = {};
    units.forEach(function (u) {
      if (u.destroyed || u.unknown) return;
      if (touched ? before[u.id] : u.inDeployed) selected[u.id] = true;
    });
    resetRow.hidden = !(data.hasOperation && canModify);
    status.hidden = units.length > 0;
    if (!units.length) status.textContent = 'Your hangar force has no mechs in it yet. Add some in MekBay first.';
    render();
  }

  // A failed live read keeps a hangar that is already showing, and says so.
  function failed(message) {
    status.hidden = false;
    status.textContent = units.length ? 'Showing your last known hangar. ' + message : message;
  }

  function load() {
    error.textContent = '';
    reset.checked = false;
    touched = false;
    if (cachedData && cachedData.units) {
      apply(cachedData); // instant
    } else {
      status.hidden = false;
      status.textContent = 'Loading your hangar…';
      list.textContent = '';
      units = [];
      selected = {};
      resetRow.hidden = true;
      update();
    }
    fetch('/api/mekbay/hangar?' + query, { credentials: 'same-origin', cache: 'no-store' }).then(function (res) {
      if (!res.ok) return res.text().then(function (t) { failed(t.trim() || 'Could not read your hangar.'); });
      return res.json().then(function (data) {
        cachedData = data; // the next time the popup opens starts from this
        apply(data);
      });
    }, function () { failed('Could not reach the server.'); });
  }

  openBtn.addEventListener('click', function () {
    dialog.showModal();
    load();
  });
  document.getElementById('deploy-cancel').addEventListener('click', function () { dialog.close(); });

  go.addEventListener('click', function () {
    busy = true;
    update();
    error.textContent = '';
    var body = query;
    units.forEach(function (u) { if (selected[u.id]) body += '&units=' + encodeURIComponent(u.id); });
    if (reset.checked) body += '&reset_opfor=1';
    fetch('/api/mekbay/deploy', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: body,
      credentials: 'same-origin'
    }).then(function (res) {
      if (res.ok) { location.reload(); return; }
      busy = false;
      update();
      return res.text().then(function (t) { error.textContent = t.trim() || 'Could not deploy.'; });
    }, function () { busy = false; update(); error.textContent = 'Could not reach the server.'; });
  });
})();
