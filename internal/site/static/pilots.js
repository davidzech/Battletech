// Manage page: name a new named pilot, change a pilot's portrait, spend their unallocated SP in the Manage SP dialog, or
// mark one as fallen. The server validates everything; these handlers only post the forms and reload to show the result.
(function () {
  var box = document.getElementById('pilots');
  if (!box) return;
  var error = document.getElementById('pilot-error');
  var STEP = 10; // the sliders and the number boxes' arrows both move this many SP at a time

  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }

  function post(url, body) {
    return fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: 'campaign=' + encodeURIComponent(box.dataset.campaign) + '&' + body,
      credentials: 'same-origin'
    });
  }

  function send(url, body, button) {
    error.textContent = '';
    if (button) button.disabled = true;
    return post(url, body).then(function (res) {
      if (res.ok) { location.reload(); return; }
      if (button) button.disabled = false;
      return res.text().then(function (t) { error.textContent = t.trim() || 'Something went wrong.'; });
    }, function () {
      if (button) button.disabled = false;
      error.textContent = 'Could not reach the server.';
    });
  }

  var add = document.getElementById('pilot-add');
  if (add) {
    add.addEventListener('submit', function (e) {
      e.preventDefault();
      var name = add.elements.name.value.trim();
      if (!name) { error.textContent = 'Give your pilot a name.'; return; }
      send('/api/pilots', 'name=' + encodeURIComponent(name), add.querySelector('button'));
    });
  }

  // ---- The Manage SP dialog: a level-up screen. The pilot's earned SP that isn't in any column yet is spent with the same
  // sliders and number boxes as the start wizard; the boxes show the levels the spending reaches before it is applied.
  var dialog = document.getElementById('sp-dialog');
  var ladders = JSON.parse(box.dataset.ladders || '[]');
  var spBody = document.getElementById('sp-body');
  var spError = document.getElementById('sp-error');
  var apply = document.getElementById('sp-apply');
  var current = null; // the pilot whose dialog is open: {id, name, base, pool, sp}
  var dirty = false;  // something was saved, so the page behind is stale

  function render() {
    var p = current;
    spBody.textContent = '';
    document.getElementById('sp-title').textContent = 'Manage SP: ' + p.name;

    var boxes = el('div', 'stat-boxes');
    var stats = ladders.map(SPAllocator.statBox);
    stats.forEach(function (b) { boxes.appendChild(b); });
    spBody.appendChild(boxes);

    var remaining = el('p', 'alloc-remaining');
    spBody.appendChild(el('h3', 'alloc-head', 'Level up'));
    spBody.appendChild(remaining);
    var rows = ladders.map(function (l) {
      return SPAllocator.row(p, l, { ladders: ladders, budget: p.pool, step: STEP, base: p.base, onEdit: function () { spError.textContent = ''; }, refresh: refresh });
    });
    rows.forEach(function (r) { spBody.appendChild(r); });
    if (!p.pool) spBody.appendChild(el('p', 'muted', 'This pilot has no unallocated SP. Add the SP they earned below, then spend it here.'));

    // Earned SP is added to the pilot's total here, then spent above.
    var earn = el('form', 'sp-earn');
    earn.autocomplete = 'off';
    var label = el('label', null, 'Add earned SP');
    var input = document.createElement('input');
    input.type = 'number'; input.min = 1; input.max = 10000; input.step = STEP; input.inputMode = 'numeric'; input.placeholder = '0';
    label.appendChild(input);
    var addBtn = el('button', null, 'Add');
    addBtn.type = 'submit';
    earn.appendChild(label);
    earn.appendChild(addBtn);
    earn.addEventListener('submit', function (e) {
      e.preventDefault();
      var n = parseInt(input.value, 10);
      if (isNaN(n) || n < 1) { spError.textContent = 'Enter how much SP the pilot earned.'; return; }
      addBtn.disabled = true;
      post('/api/pilots/earn', 'pilot=' + encodeURIComponent(p.id) + '&sp=' + n).then(function (res) {
        if (res.ok) {
          dirty = true;
          p.pool += n;
          ladders.forEach(function (l) { p.sp[l.key] = 0; });
          spError.textContent = '';
          render();
          return;
        }
        addBtn.disabled = false;
        return res.text().then(function (t) { spError.textContent = t.trim() || 'Could not save the SP.'; });
      }, function () { addBtn.disabled = false; spError.textContent = 'Could not reach the server.'; });
    });
    spBody.appendChild(earn);

    function refresh() {
      var used = SPAllocator.spentOf(p, ladders);
      var left = p.pool - used;
      remaining.textContent = left + ' of ' + p.pool + ' SP unallocated';
      remaining.classList.toggle('done', left === 0);
      rows.forEach(function (r) { r.show(); });
      stats.forEach(function (b, i) { b.show(p.base[ladders[i].key] + p.sp[ladders[i].key]); });
      apply.disabled = used === 0;
    }
    refresh();
  }

  function openSP(li) {
    var base = {}, parts = (li.dataset.sp || '').split(',');
    var p = { id: li.dataset.pilot, name: li.dataset.name, base: base, pool: parseInt(li.dataset.unallocated, 10) || 0, sp: {} };
    ladders.forEach(function (l, i) { base[l.key] = parseInt(parts[i], 10) || 0; p.sp[l.key] = 0; });
    current = p;
    dirty = false;
    spError.textContent = '';
    render();
    dialog.showModal();
  }

  if (dialog && typeof dialog.showModal === 'function') {
    document.getElementById('sp-cancel').addEventListener('click', function () { dialog.close(); });
    dialog.addEventListener('close', function () { if (dirty) location.reload(); });
    document.getElementById('sp-form').addEventListener('submit', function (e) {
      e.preventDefault();
      if (!current || apply.disabled) return;
      var parts = ['pilot=' + encodeURIComponent(current.id)];
      ladders.forEach(function (l) { parts.push(l.key + '=' + current.sp[l.key]); });
      apply.disabled = true;
      post('/api/pilots/allocate', parts.join('&')).then(function (res) {
        if (res.ok) { dirty = true; dialog.close(); return; }
        apply.disabled = false;
        return res.text().then(function (t) { spError.textContent = t.trim() || 'Could not save the SP.'; });
      }, function () { apply.disabled = false; spError.textContent = 'Could not reach the server.'; });
    });
  }

  box.querySelectorAll('li.pilot').forEach(function (li) {
    var id = li.dataset.pilot;
    var manage = li.querySelector('.pilot-sp');
    if (manage && dialog) manage.addEventListener('click', function () { openSP(li); });

    var portraitBtn = li.querySelector('button.portrait-btn');
    if (portraitBtn) {
      portraitBtn.addEventListener('click', function () {
        if (typeof PortraitPicker === 'undefined') return;
        var taken = [];
        box.querySelectorAll('li.pilot').forEach(function (o) { if (o !== li && o.dataset.portrait) taken.push(o.dataset.portrait); });
        PortraitPicker.open({
          portraits: JSON.parse(box.dataset.portraits || '[]'), selected: li.dataset.portrait, taken: taken,
          onPick: function (file) { send('/api/pilots/portrait', 'pilot=' + encodeURIComponent(id) + '&portrait=' + encodeURIComponent(file)); }
        });
      });
    }

    var fallen = li.querySelector('.pilot-fallen');
    if (fallen) {
      fallen.addEventListener('click', function () {
        if (!window.confirm('Mark ' + li.dataset.name + ' as fallen? They are permanently removed from the campaign.')) return;
        send('/api/pilots/fallen', 'pilot=' + encodeURIComponent(id), fallen);
      });
    }
  });
})();
