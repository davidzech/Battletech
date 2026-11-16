// Manage page: change a pilot's portrait, spend their unallocated SP in the Manage SP dialog, mark one killed in action,
// or correct the records (revive or delete a pilot). The server validates everything; these handlers only post the forms and reload to show the result.
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

  // ---- The Manage SP dialog: a level-up screen. The pilot's earned SP that isn't in any column yet is spent with the same
  // sliders and number boxes as the start wizard; the boxes show the levels the spending reaches before it is applied.
  var dialog = document.getElementById('sp-dialog');
  var ladders = JSON.parse(box.dataset.ladders || '[]');
  var spBody = document.getElementById('sp-body');
  var spError = document.getElementById('sp-error');
  var apply = document.getElementById('sp-apply');
  var current = null; // the pilot whose dialog is open: {id, name, base, pool, earned, sp}; earned is staged, not yet saved
  var dirty = false;  // something was saved, so the page behind is stale

  function render() {
    var p = current;
    spBody.textContent = '';
    document.getElementById('sp-title').textContent = 'Manage SP: ' + p.name;

    var boxes = el('div', 'stat-boxes');
    var stats = ladders.map(SPAllocator.statBox);
    stats.forEach(function (b) { boxes.appendChild(b); });
    spBody.appendChild(boxes);

    // "Level up" on the left, how much SP is still unallocated right-justified on the same line.
    var remaining = el('p', 'alloc-remaining');
    var headRow = el('div', 'alloc-headrow');
    headRow.appendChild(el('h3', 'alloc-head', 'Level up'));
    headRow.appendChild(remaining);
    spBody.appendChild(headRow);
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
    input.type = 'number'; input.min = 0; input.max = 10000; input.step = STEP; input.inputMode = 'numeric'; input.placeholder = '0';
    label.appendChild(input);
    var addBtn = el('button', null, 'Add');
    addBtn.type = 'submit';
    earn.appendChild(label);
    earn.appendChild(addBtn);
    // Earned SP is only staged here: it raises what can be spent in this dialog, but nothing is saved until Apply (closing
    // the dialog throws it away).
    earn.addEventListener('submit', function (e) {
      e.preventDefault();
      e.stopPropagation(); // this form sits inside the dialog's form: its submit must not also trigger Apply
      var n = parseInt(input.value, 10);
      if (isNaN(n) || n < 1) { spError.textContent = 'Enter how much SP the pilot earned.'; return; }
      if (p.earned + n > 10000) { spError.textContent = 'That is too much SP to add at once.'; return; }
      if (!window.confirm('Add ' + n + ' earned SP to ' + p.name + '? It is staged until you press Apply, and Clear or closing this window discards it.')) return;
      p.earned += n;
      p.pool += n;
      spError.textContent = '';
      render();
    });
    spBody.appendChild(earn);
    if (p.earned) {
      var staged = el('p', 'sp-staged', '+' + p.earned + ' SP earned. Not saved until you press Apply. ');
      var clear = el('button', 'link-button', 'Clear');
      clear.type = 'button';
      clear.addEventListener('click', function () {
        p.pool -= p.earned;
        p.earned = 0;
        if (SPAllocator.spentOf(p, ladders) > p.pool) ladders.forEach(function (l) { p.sp[l.key] = 0; });
        render();
      });
      staged.appendChild(clear);
      spBody.appendChild(staged);
    }

    function refresh() {
      var used = SPAllocator.spentOf(p, ladders);
      var left = p.pool - used;
      remaining.textContent = left + ' of ' + p.pool + ' SP unallocated';
      remaining.classList.toggle('done', left === 0);
      rows.forEach(function (r) { r.show(); });
      stats.forEach(function (b, i) { b.show(p.base[ladders[i].key] + p.sp[ladders[i].key]); });
      apply.disabled = used === 0 && p.earned === 0;
    }
    refresh();
  }

  function openSP(li) {
    var base = {}, parts = (li.dataset.sp || '').split(',');
    var p = { id: li.dataset.pilot, name: li.dataset.name, base: base, pool: parseInt(li.dataset.unallocated, 10) || 0, earned: 0, sp: {} };
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
      if (e.target !== e.currentTarget) return; // only the dialog's own submit (Apply), never a form nested in it
      if (!current || apply.disabled) return;
      var parts = ['pilot=' + encodeURIComponent(current.id), 'earned=' + current.earned];
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

    var kia = li.querySelector('.pilot-kia');
    if (kia) {
      kia.addEventListener('click', function () {
        if (!window.confirm('Mark ' + li.dataset.name + ' as killed in action? They are removed from your active pilots (you can bring them back under Pilot records).')) return;
        send('/api/pilots/fallen', 'pilot=' + encodeURIComponent(id), kia);
      });
    }
  });

  // ---- The "More" menus on the pilot cards: a native <details>, closed again by a click anywhere else or by Escape.
  function closeMenus(except) {
    box.querySelectorAll('details.more[open]').forEach(function (d) { if (d !== except) d.removeAttribute('open'); });
  }
  document.addEventListener('click', function (e) { closeMenus(e.target.closest ? e.target.closest('details.more') : null); });
  document.addEventListener('keydown', function (e) { if (e.key === 'Escape') closeMenus(null); });

  // ---- Manage Pilot Records: edit a name, revive, delete, reset abilities or override the total SP, to correct a misclick or a
  // desync. Every action updates its row in place and leaves the dialog open; the page behind is refreshed when the dialog
  // is closed (if anything changed).
  var records = document.getElementById('records-dialog');
  if (records && typeof records.showModal === 'function') {
    var recError = document.getElementById('records-error');
    var recDirty = false;
    var PENCIL_DONE = '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M20 6 9 17l-5-5"/></svg>';
    document.getElementById('records-open').addEventListener('click', function () { recError.textContent = ''; records.showModal(); });
    document.getElementById('records-close').addEventListener('click', function () { records.close(); });
    records.addEventListener('click', function (e) { if (e.target === records) records.close(); });
    records.addEventListener('close', function () { if (recDirty) location.reload(); });

    // Posts an action for one pilot. onOk runs when the server accepted it; the button is disabled meanwhile.
    function recAct(url, li, button, extra, onOk) {
      recError.textContent = '';
      button.disabled = true;
      post(url, 'pilot=' + encodeURIComponent(li.dataset.pilot) + (extra ? '&' + extra : '')).then(function (res) {
        button.disabled = false;
        if (res.ok) { recDirty = true; onOk(); return; }
        return res.text().then(function (t) { recError.textContent = t.trim() || 'Something went wrong.'; });
      }, function () { button.disabled = false; recError.textContent = 'Could not reach the server.'; });
    }

    // Shows the row's SP numbers from its data attributes.
    function showSP(li) {
      var total = parseInt(li.dataset.total, 10) || 0, allocated = parseInt(li.dataset.allocated, 10) || 0;
      li.querySelector('.rec-n-total').textContent = total;
      li.querySelector('.rec-n-alloc').textContent = allocated;
      li.querySelector('.rec-n-unalloc').textContent = Math.max(0, total - allocated);
    }

    records.querySelectorAll('#records-list li[data-pilot]').forEach(function (li) {
      // The name: a pencil turns it into a text field with a confirm button embedded in it.
      var nameBox = li.querySelector('.rec-name');
      var nameText = li.querySelector('.rec-name-text');
      var edit = li.querySelector('.rec-edit');
      var nameForm = null;
      function closeNameEdit() {
        if (!nameForm) return;
        nameForm.remove();
        nameForm = null;
        nameText.hidden = false;
        edit.hidden = false;
      }
      edit.addEventListener('click', function () {
        if (nameForm) return;
        recError.textContent = '';
        nameForm = el('form', 'rec-name-form');
        var input = document.createElement('input');
        input.type = 'text'; input.maxLength = 30; input.autocomplete = 'off'; input.spellcheck = false;
        input.value = li.dataset.name;
        input.setAttribute('aria-label', 'Pilot name');
        var ok = el('button', 'rec-name-ok');
        ok.type = 'submit'; ok.title = 'Save the name'; ok.setAttribute('aria-label', 'Save the name');
        ok.innerHTML = PENCIL_DONE; // a constant defined above, never data
        nameForm.appendChild(input);
        nameForm.appendChild(ok);
        nameForm.addEventListener('submit', function (e) {
          e.preventDefault();
          var name = input.value.trim();
          if (!name) { recError.textContent = 'Give your pilot a name.'; return; }
          if (name === li.dataset.name) { closeNameEdit(); return; }
          recAct('/api/pilots/rename', li, ok, 'name=' + encodeURIComponent(name), function () {
            li.dataset.name = name;
            nameText.textContent = name;
            edit.setAttribute('aria-label', 'Edit ' + name + '\u2019s name');
            closeNameEdit();
          });
        });
        input.addEventListener('keydown', function (e) { if (e.key === 'Escape') { e.stopPropagation(); e.preventDefault(); closeNameEdit(); } });
        nameText.hidden = true;
        edit.hidden = true;
        nameBox.appendChild(nameForm);
        input.focus();
        input.select();
      });

      var revive = li.querySelector('.rec-revive');
      if (revive) revive.addEventListener('click', function () {
        recAct('/api/pilots/revive', li, revive, '', function () {
          var state = li.querySelector('.rec-state');
          state.textContent = 'Active';
          state.classList.remove('kia');
          revive.remove();
        });
      });

      var reset = li.querySelector('.rec-reset');
      reset.addEventListener('click', function () {
        var allocated = parseInt(li.dataset.allocated, 10) || 0;
        if (!window.confirm('Factory-reset ' + li.dataset.name + '\u2019s abilities? Gunnery, Piloting and Edge go back to the starting levels, and all ' + allocated + ' allocated SP become unallocated. Their total SP stays the same.')) return;
        recAct('/api/pilots/reset', li, reset, '', function () { li.dataset.allocated = 0; showSP(li); });
      });

      // Override the total SP. A total below what is already allocated has to reset the allocation, so it asks first (the
      // server refuses it without that agreement too).
      var form = li.querySelector('.rec-total-form');
      var totalInput = form.querySelector('input');
      li.querySelector('.rec-total').addEventListener('click', function () {
        form.hidden = !form.hidden;
        if (!form.hidden) { totalInput.value = li.dataset.total; totalInput.focus(); totalInput.select(); }
      });
      form.querySelector('.rec-total-cancel').addEventListener('click', function () { form.hidden = true; });
      form.addEventListener('submit', function (e) {
        e.preventDefault();
        var total = parseInt(totalInput.value, 10), allocated = parseInt(li.dataset.allocated, 10) || 0;
        if (isNaN(total) || total < 0) { recError.textContent = 'Enter a whole number of SP, 0 or more.'; return; }
        var resets = total < allocated;
        if (resets && !window.confirm('Setting ' + li.dataset.name + '\u2019s total to ' + total + ' SP is below the ' + allocated + ' SP already allocated. Doing this resets all of their allocations (Gunnery, Piloting and Edge go back to the starting levels) and leaves ' + total + ' SP unallocated. Continue?')) return;
        recAct('/api/pilots/total', li, form.querySelector('button[type=submit]'), 'total=' + total + (resets ? '&reset=1' : ''), function () {
          li.dataset.total = total;
          if (resets) li.dataset.allocated = 0;
          showSP(li);
          form.hidden = true;
        });
      });

      var del = li.querySelector('.rec-delete');
      del.addEventListener('click', function () {
        if (!window.confirm('Delete ' + li.dataset.name + ' permanently? Their record and all of their SP are lost. This cannot be undone.')) return;
        recAct('/api/pilots/delete', li, del, '', function () {
          var list = li.parentNode;
          li.remove();
          if (!list.querySelector('li[data-pilot]')) list.appendChild(el('li', 'muted', 'No pilots.'));
        });
      });
    });
  }
})();
