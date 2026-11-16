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
  var busy = false;

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
    var li = el('li', 'deploy-unit' + (u.destroyed || u.unknown ? ' unavailable' : ''));
    var label = el('label');
    var box = document.createElement('input');
    box.type = 'checkbox';
    box.checked = !!selected[u.id];
    box.disabled = u.destroyed || u.unknown;
    box.addEventListener('change', function () {
      if (box.checked) selected[u.id] = true; else delete selected[u.id];
      error.textContent = '';
      update();
    });
    label.appendChild(box);
    label.appendChild(el('span', 'du-name', u.name));
    var meta = [u.class, u.tons ? u.tons + ' t' : '', u.role].filter(Boolean).join(' · ');
    label.appendChild(el('span', 'du-meta', meta));
    label.appendChild(el('span', 'du-pilot', (u.pilot || 'Unnamed pilot') + ' ' + u.gunnery + '/' + u.piloting));
    var tags = el('span', 'du-tags');
    if (u.destroyed) tags.appendChild(el('span', 'tag bad', 'Destroyed'));
    else if (u.unknown) tags.appendChild(el('span', 'tag bad', 'Unknown unit'));
    else if (u.damaged) tags.appendChild(el('span', 'tag warn', 'Damaged'));
    label.appendChild(tags);
    label.appendChild(el('span', 'du-bv', u.unknown ? '?' : fmt(u.bv)));
    li.appendChild(label);
    return li;
  }

  function render() {
    list.textContent = '';
    units.forEach(function (u) { list.appendChild(row(u)); });
    update();
  }

  function load() {
    status.hidden = false;
    status.textContent = 'Loading your hangar…';
    error.textContent = '';
    list.textContent = '';
    units = [];
    selected = {};
    resetRow.hidden = true;
    reset.checked = false;
    update();
    fetch('/api/mekbay/hangar?' + query, { credentials: 'same-origin', cache: 'no-store' }).then(function (res) {
      if (!res.ok) return res.text().then(function (t) { status.textContent = t.trim() || 'Could not read your hangar.'; });
      return res.json().then(function (data) {
        units = data.units || [];
        if (data.limit) limit = data.limit;
        // Reopening after a deployment starts from the units already in the Deployed group.
        units.forEach(function (u) { if (u.inDeployed && !u.destroyed && !u.unknown) selected[u.id] = true; });
        resetRow.hidden = !(data.hasOperation && canModify);
        status.hidden = units.length > 0;
        if (!units.length) status.textContent = 'Your hangar force has no mechs in it yet. Add some in MekBay first.';
        render();
      });
    }, function () { status.textContent = 'Could not reach the server.'; });
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
