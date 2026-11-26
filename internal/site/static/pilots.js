// Manage page: change a pilot's portrait, spend their unallocated SP in the Manage SP dialog,
// or correct the records (mark a pilot killed in action, revive or delete one). The server validates everything; these handlers only post the forms and reload to show the result.
(function () {
  var box = document.getElementById('pilots');
  if (!box) return;
  var error = document.getElementById('pilot-error');
  var STEP = 10; // the sliders and the number boxes' arrows both move this many SP at a time

  var el = Dom.el;

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

  // ---- Renaming a pilot: a pencil turns the name into a text field with a confirm check embedded in it. Used on the pilot cards
  // and in Advanced Management, and a new name is shown in both places at once.
  var PENCIL_DONE = '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M20 6 9 17l-5-5"/></svg>';

  // Shows a pilot's new name everywhere on the page.
  function setPilotName(id, name) {
    document.querySelectorAll('#pilots li.pilot[data-pilot="' + id + '"], #records-list li[data-pilot="' + id + '"]').forEach(function (row) {
      row.dataset.name = name;
      var text = row.querySelector('.name-text');
      if (text) text.textContent = name;
      var edit = row.querySelector('.name-edit');
      if (edit) edit.setAttribute('aria-label', 'Edit ' + name + '\u2019s name');
    });
  }

  // o: {id, nameBox, nameText, edit, current() -> the name now, error(message)}
  function attachNameEditor(o) {
    var form = null;
    function close() {
      if (!form) return;
      form.remove();
      form = null;
      o.nameText.hidden = false;
      o.edit.hidden = false;
    }
    o.edit.addEventListener('click', function () {
      if (form) return;
      o.error('');
      form = el('form', 'name-form');
      var input = document.createElement('input');
      input.type = 'text'; input.maxLength = 30; input.autocomplete = 'off'; input.spellcheck = false;
      input.value = o.current();
      input.setAttribute('aria-label', 'Pilot name');
      var ok = el('button', 'name-ok');
      ok.type = 'submit'; ok.title = 'Save the name'; ok.setAttribute('aria-label', 'Save the name');
      ok.innerHTML = PENCIL_DONE; // a constant defined above, never data
      form.appendChild(input);
      form.appendChild(ok);
      form.addEventListener('submit', function (e) {
        e.preventDefault();
        e.stopPropagation();
        var name = input.value.trim().replace(/\s+/g, ' '); // as the server saves it: runs of spaces collapsed
        if (!name) { o.error('Give your pilot a name.'); return; }
        if (name === o.current()) { close(); return; }
        ok.disabled = true;
        o.error('');
        post('/api/pilots/rename', 'pilot=' + encodeURIComponent(o.id) + '&name=' + encodeURIComponent(name)).then(function (res) {
          ok.disabled = false;
          if (res.ok) { setPilotName(o.id, name); close(); return; }
          return res.text().then(function (t) { o.error(t.trim() || 'Something went wrong.'); });
        }, function () { ok.disabled = false; o.error('Could not reach the server.'); });
      });
      input.addEventListener('keydown', function (e) { if (e.key === 'Escape') { e.stopPropagation(); e.preventDefault(); close(); } });
      o.nameText.hidden = true;
      o.edit.hidden = true;
      o.nameBox.appendChild(form);
      input.focus();
      input.select();
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
    if (!p.pool) spBody.appendChild(el('p', 'muted', 'This pilot has no unallocated SP. Missions award SP when they are completed.'));

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
      if (e.target !== e.currentTarget) return; // only the dialog's own submit (Apply), never a form nested in it
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

  // ---- Edge abilities: whenever a pilot's Edge abilities level is above the number they have learned, the card offers a picker with one
  // combo box for each new ability. The choice is permanent: Advanced Management resets it.
  var skillDialog = document.getElementById('skill-dialog');
  var abilityCatalog = JSON.parse(box.dataset.abilities || '[]');
  var CATEGORY_ORDER = ['Attack', 'Indirect Fire', 'Defense', 'Heat', 'Melee', 'Movement'];
  var skillPilot = null; // the pilot's row the picker is open for
  var skillSelects = [];
  var skillSlots = document.getElementById('skill-slots');
  var skillLearn = document.getElementById('skill-learn');
  var skillError = document.getElementById('skill-error');

  // The ability card (the catalog, filling a card, and the hover card on a chip) is shared with the Deploy popup: abilitycard.js, which loads first.
  AbilityCard.setCatalog(abilityCatalog);
  var abilityById = AbilityCard.byId, fillAbilityCard = AbilityCard.fill;

  // Each combo box lists the abilities the pilot doesn't have yet, grouped by category; one chosen in another box is greyed out here, and
  // the chosen ability's rule is shown under its box.
  function syncSkills() {
    var chosen = skillSelects.map(function (s) { return s.value; });
    skillSelects.forEach(function (s, i) {
      [].forEach.call(s.options, function (o) { o.disabled = o.value !== '' && chosen.some(function (c, j) { return j !== i && c === o.value; }); });
      fillAbilityCard(s.parentNode.querySelector('.skill-card'), abilityById(s.value));
    });
    skillLearn.disabled = chosen.some(function (c) { return c === ''; });
    skillError.textContent = '';
  }

  // ---- Hover card for a learned ability, like the one on the mech icons of the overview: the same ability card as the picker, in a floating panel
  // anchored under the chip (above it when there is no room below), shown on hover and on keyboard focus.
  box.querySelectorAll('.skill-chip').forEach(function (chip) { AbilityCard.attach(chip, chip.dataset.ability); });

  function openSkills(li) {
    skillPilot = li;
    var pending = parseInt(li.dataset.edgePending, 10) || 0;
    var learned = (li.dataset.learned || '').split(' ').filter(Boolean);
    var available = abilityCatalog.filter(function (a) { return learned.indexOf(a.id) < 0; });
    document.getElementById('skill-title').textContent = li.dataset.name + ': choose ' + (pending === 1 ? 'a new Edge ability' : pending + ' new Edge abilities');
    document.getElementById('skill-intro').textContent = 'As a named pilot reaches a new level of Edge abilities, they choose one from the list. This choice is permanent: it can only be undone from Advanced Management.';
    skillSlots.textContent = '';
    skillSelects = [];
    for (var i = 0; i < pending; i++) {
      var slot = el('div', 'skill-slot');
      var label = el('label', null, 'Edge ability ' + (pending > 1 ? (i + 1) : ''));
      var select = document.createElement('select');
      select.id = 'skill-select-' + i;
      label.setAttribute('for', select.id);
      var blank = el('option', null, 'Choose an ability\u2026');
      blank.value = '';
      select.appendChild(blank);
      var categories = CATEGORY_ORDER.slice();
      available.forEach(function (a) { if (categories.indexOf(a.category) < 0) categories.push(a.category); });
      categories.forEach(function (cat) {
        var inCat = available.filter(function (a) { return a.category === cat; });
        if (!inCat.length) return;
        var group = document.createElement('optgroup');
        group.label = cat;
        inCat.forEach(function (a) { var o = el('option', null, a.name); o.value = a.id; group.appendChild(o); });
        select.appendChild(group);
      });
      select.addEventListener('change', syncSkills);
      slot.appendChild(label);
      slot.appendChild(select);
      slot.appendChild(el('div', 'skill-card empty'));
      skillSlots.appendChild(slot);
      skillSelects.push(select);
    }
    syncSkills();
    skillDialog.showModal();
    if (skillSelects.length) skillSelects[0].focus();
  }

  if (skillDialog && typeof skillDialog.showModal === 'function') {
    document.getElementById('skill-cancel').addEventListener('click', function () { skillDialog.close(); });
    skillDialog.addEventListener('click', function (e) { if (e.target === skillDialog) skillDialog.close(); });
    document.getElementById('skill-form').addEventListener('submit', function (e) {
      e.preventDefault();
      if (!skillPilot || skillLearn.disabled) return;
      var ids = skillSelects.map(function (s) { return s.value; });
      var names = ids.map(function (id) { return abilityById(id).name; });
      Confirm.ask({
        title: 'Learn ' + names.join(', ') + '?',
        message: 'This choice is permanent. To undo it you will have to reset ' + skillPilot.dataset.name + '\u2019s Edge abilities under Advanced Management.',
        confirmLabel: 'Learn', danger: true
      }).then(function (ok) {
        if (!ok) return;
        skillLearn.disabled = true;
        post('/api/pilots/abilities', 'pilot=' + encodeURIComponent(skillPilot.dataset.pilot) + ids.map(function (id) { return '&ability=' + encodeURIComponent(id); }).join('')).then(function (res) {
          if (res.ok) { location.reload(); return; }
          skillLearn.disabled = false;
          return res.text().then(function (t) { skillError.textContent = t.trim() || 'Could not save the Edge abilities.'; });
        }, function () { skillLearn.disabled = false; skillError.textContent = 'Could not reach the server.'; });
      });
    });
  }

  // ---- Recruit New Pilot: a one-step wizard. A random portrait nobody has and a name (the rank is the campaign's recruit_rank); the new pilot is a standard 4/5 pilot
  // with 1 Edge token and no SP, shown in the same stat boxes as the start wizard.
  var recruit = document.getElementById('recruit-dialog');
  if (recruit && typeof recruit.showModal === 'function') {
    var portraitList = JSON.parse(box.dataset.portraits || '[]');
    var rName = document.getElementById('recruit-name');
    var rImg = document.getElementById('recruit-portrait-img');
    var rError = document.getElementById('recruit-error');
    var rGo = document.getElementById('recruit-go');
    var rBoxes = document.getElementById('recruit-boxes');
    var rPortrait = '';
    // The new pilot's starting SP (the campaign's) and the same sliders, number boxes and stat boxes as the start wizard. Spending it
    // all is optional: what is left stays unallocated.
    var rBudget = parseInt(recruit.dataset.sp, 10) || 0;
    var rCost = parseInt(recruit.dataset.cost, 10) || 0;          // what recruiting takes from the warchest
    var rWarchest = parseInt(recruit.dataset.warchest, 10) || 0;
    var rCostText = document.getElementById('recruit-cost');
    var commas = Dom.commas;
    var rSP = {};
    ladders.forEach(function (l) { rSP[l.key] = 0; });
    var rPilot = { sp: rSP };
    var rStats = ladders.map(SPAllocator.statBox);
    rStats.forEach(function (b) { rBoxes.appendChild(b); });
    var rRemaining = document.getElementById('recruit-remaining');
    var rRowsBox = document.getElementById('recruit-rows');
    var rRows = rBudget ? ladders.map(function (l) {
      return SPAllocator.row(rPilot, l, { ladders: ladders, budget: rBudget, step: STEP, refresh: rRefresh, onEdit: function () { rError.textContent = ''; } });
    }) : [];
    rRows.forEach(function (r) { rRowsBox.appendChild(r); });
    function rRefresh() {
      if (rRemaining) {
        var left = rBudget - SPAllocator.spentOf(rPilot, ladders);
        rRemaining.textContent = left + ' of ' + rBudget + ' SP unallocated';
        rRemaining.classList.toggle('done', left === 0);
      }
      rRows.forEach(function (r) { r.show(); });
      rStats.forEach(function (b, i) { b.show(rSP[ladders[i].key]); });
    }
    rRefresh();

    // Portraits living pilots already have: a new pilot never shares one.
    function takenPortraits() {
      var taken = [];
      box.querySelectorAll('li.pilot:not(.kia)').forEach(function (li) { if (li.dataset.portrait) taken.push(li.dataset.portrait); });
      return taken;
    }
    // The placeholder name follows the portrait's gender, until the player types a name of their own.
    function showPortrait(file) {
      rPortrait = file; rImg.src = '/portraits/' + file;
      if (window.PilotNames && !rName.value) rName.placeholder = PilotNames.random(PilotNames.groupOf(portraitList, file));
    }

    var openBtn = document.getElementById('recruit-open');
    if (openBtn) openBtn.addEventListener('click', function () {
      var taken = takenPortraits();
      var free = portraitList.filter(function (p) { return taken.indexOf(p.file) < 0; });
      var pick = free.length ? free : portraitList;
      rName.value = '';
      showPortrait(pick.length ? pick[Math.floor(Math.random() * pick.length)].file : '');
      rError.textContent = '';
      ladders.forEach(function (l) { rSP[l.key] = 0; });
      rRefresh();
      rGo.disabled = false;
      if (rCost > 0) {
        var after = rWarchest - rCost;
        rCostText.hidden = false;
        rCostText.textContent = 'Cost ' + commas(rCost) + ' SP. Warchest ' + commas(rWarchest) + ' \u2192 ' + commas(Math.max(after, 0)) + ' SP.' + (after < 0 ? ' Not enough funds.' : '');
        rCostText.classList.toggle('short', after < 0);
        rGo.textContent = 'Recruit for ' + commas(rCost) + ' SP';
        if (after < 0) rGo.disabled = true;
      }
      recruit.showModal();
      rName.focus();
    });
    document.getElementById('recruit-portrait').addEventListener('click', function () {
      if (typeof PortraitPicker === 'undefined') return;
      PortraitPicker.open({ portraits: portraitList, selected: rPortrait, taken: takenPortraits(), onPick: showPortrait });
    });
    document.getElementById('recruit-cancel').addEventListener('click', function () { recruit.close(); });
    recruit.addEventListener('click', function (e) { if (e.target === recruit) recruit.close(); });
    rName.addEventListener('input', function () { rError.textContent = ''; });
    document.getElementById('recruit-form').addEventListener('submit', function (e) {
      e.preventDefault();
      var name = rName.value.trim();
      if (!name) { rError.textContent = 'Give your pilot a name.'; rName.focus(); return; }
      rGo.disabled = true;
      rError.textContent = '';
      var fields = 'name=' + encodeURIComponent(name) + '&portrait=' + encodeURIComponent(rPortrait);
      ladders.forEach(function (l) { fields += '&' + l.key + '=' + rSP[l.key]; });
      post('/api/pilots', fields).then(function (res) {
        if (res.ok) { location.reload(); return; }
        rGo.disabled = false;
        return res.text().then(function (t) { rError.textContent = t.trim() || 'Could not recruit the pilot.'; });
      }, function () { rGo.disabled = false; rError.textContent = 'Could not reach the server.'; });
    });
  }

  // A rank never wraps: the longest of a faction's ranks fits the box at the stylesheet's size, and a rank that is still too wide
  // (a longer ladder in another campaign) is scaled down in proportion.
  function fitRanks() {
    box.querySelectorAll('.rank-label').forEach(function (e) {
      e.style.fontSize = '';
      var room = e.clientWidth, need = e.scrollWidth;
      if (room > 0 && need > room) e.style.fontSize = (parseFloat(getComputedStyle(e).fontSize) * room / need * 0.97).toFixed(2) + 'px';
    });
  }
  fitRanks();
  if (document.fonts && document.fonts.ready) document.fonts.ready.then(fitRanks); // the display font changes the widths when it arrives
  window.addEventListener('resize', fitRanks);

  box.querySelectorAll('li.pilot').forEach(function (li) {
    var id = li.dataset.pilot;
    var skillBtn = li.querySelector('.skill-pick');
    if (skillBtn && skillDialog) skillBtn.addEventListener('click', function () { openSkills(li); });
    var cardEdit = li.querySelector('.pilot-head .name-edit');
    if (cardEdit) {
      attachNameEditor({
        id: id,
        nameBox: li.querySelector('.pilot-head h3'),
        nameText: li.querySelector('.pilot-head .name-text'),
        edit: cardEdit,
        current: function () { return li.dataset.name; },
        error: function (m) { error.textContent = m; }
      });
    }
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
  });

  // ---- Advanced Management: edit a name, mark killed in action or revive, delete, reset abilities or override the total SP, to correct a misclick or a
  // desync. Every action updates its row in place and leaves the dialog open; the page behind is refreshed when the dialog
  // is closed (if anything changed).
  var records = document.getElementById('records-dialog');
  if (records && typeof records.showModal === 'function') {
    var recError = document.getElementById('records-error');
    var recDirty = false;
    document.getElementById('records-open').addEventListener('click', function () { recError.textContent = ''; records.showModal(); });
    document.getElementById('records-close').addEventListener('click', function () { records.close(); });
    records.addEventListener('click', function (e) { if (e.target === records) records.close(); });
    records.addEventListener('close', function () { if (recDirty) location.reload(); });

    // ---- Change rank: a small dialog with the campaign's ranks, over Advanced Management. Saving updates the pilot's row in place (the page behind is
    // reloaded when Advanced Management closes, like every other correction here).
    var rankDialog = document.getElementById('rank-dialog');
    var rankSelect = document.getElementById('rank-select');
    var rankError = document.getElementById('rank-error');
    var rankPilot = null; // the row whose rank is being changed
    if (rankDialog && typeof rankDialog.showModal === 'function') {
      document.getElementById('rank-cancel').addEventListener('click', function () { rankDialog.close(); });
      rankDialog.addEventListener('click', function (e) { if (e.target === rankDialog) rankDialog.close(); });
      document.getElementById('rank-form').addEventListener('submit', function (e) {
        e.preventDefault();
        if (!rankPilot) return;
        var li = rankPilot, rank = rankSelect.value, save = document.getElementById('rank-save');
        rankError.textContent = '';
        save.disabled = true;
        save.setAttribute('aria-busy', 'true');
        post('/api/pilots/rank', 'pilot=' + encodeURIComponent(li.dataset.pilot) + '&rank=' + encodeURIComponent(rank)).then(function (res) {
          save.disabled = false;
          save.setAttribute('aria-busy', 'false');
          if (res.ok) {
            recDirty = true;
            li.dataset.rank = rank;
            li.querySelector('.rec-rank-btn').textContent = rank;
            rankDialog.close();
            return;
          }
          return res.text().then(function (t) { rankError.textContent = t.trim() || 'Could not save the rank.'; });
        }, function () { save.disabled = false; save.setAttribute('aria-busy', 'false'); rankError.textContent = 'Could not reach the server.'; });
      });
    } else rankDialog = null;
    function openRank(li) {
      rankPilot = li;
      rankError.textContent = '';
      document.getElementById('rank-title').textContent = 'Change ' + li.dataset.name + '\u2019s rank';
      rankSelect.value = li.dataset.rank;
      rankDialog.showModal();
      rankSelect.focus();
    }

    // The warchest: set the force's funds directly.
    var wcForm = document.getElementById('warchest-form');
    if (wcForm) wcForm.addEventListener('submit', function (e) {
      e.preventDefault();
      var input = document.getElementById('warchest-input'), save = document.getElementById('warchest-save');
      recError.textContent = '';
      save.disabled = true;
      post('/api/warchest', 'funds=' + encodeURIComponent(input.value)).then(function (res) {
        save.disabled = false;
        if (res.ok) { recDirty = true; save.textContent = 'Saved'; setTimeout(function () { save.textContent = 'Set funds'; }, 1500); return; }
        return res.text().then(function (t) { recError.textContent = t.trim() || 'Something went wrong.'; });
      }, function () { save.disabled = false; recError.textContent = 'Could not reach the server.'; });
    });

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

    // Resetting a pilot's SP allocation (directly, or by setting a total below it) forgets their learned Edge abilities too, since those
    // depended on that SP. The confirmation says so when there are any, and the row is updated in place.
    function learnedNote(li) {
      var n = parseInt(li.dataset.learnedCount, 10) || 0;
      return n ? ' They also forget the ' + n + ' Edge abilit' + (n === 1 ? 'y' : 'ies') + ' they have learned, which depended on that SP.' : '';
    }
    function clearLearned(li) {
      li.dataset.learnedCount = 0;
      li.querySelector('.rec-n-skills').textContent = 'none';
      li.querySelector('.rec-skills-reset').disabled = true;
    }

    records.querySelectorAll('#records-list li[data-pilot]').forEach(function (li) {
      // The name, editable with the pencil (shared with the pilot cards).
      attachNameEditor({
        id: li.dataset.pilot,
        nameBox: li.querySelector('.rec-name'),
        nameText: li.querySelector('.name-text'),
        edit: li.querySelector('.name-edit'),
        current: function () { return li.dataset.name; },
        error: function (m) { recError.textContent = m; }
      });

      // Killed in Action and Revive are one slot: the row shows whichever applies, and each swaps for the other once it is saved.
      function stateButton(fallen) {
        var btn = document.createElement('button');
        btn.type = 'button';
        btn.className = fallen ? 'rec-revive' : 'rec-kia';
        btn.textContent = fallen ? 'Revive' : 'Mark KIA';
        btn.addEventListener('click', function () {
          if (fallen) { recAct('/api/pilots/revive', li, btn, '', function () { showState(btn, false); }); return; }
          Confirm.ask({
            title: 'Mark ' + li.dataset.name + ' as Killed in Action?',
            message: 'They move to the Memorial and are out of the campaign. Their record stays, and you can revive them here.',
            confirmLabel: 'Killed in Action', danger: true
          }).then(function (ok) { if (ok) recAct('/api/pilots/fallen', li, btn, '', function () { showState(btn, true); }); });
        });
        return btn;
      }
      function showState(old, fallen) {
        var state = li.querySelector('.rec-state');
        state.textContent = fallen ? 'Killed in Action' : 'Active';
        state.classList.toggle('kia', fallen);
        var next = stateButton(fallen);
        old.replaceWith(next);
        next.focus();
      }
      var stateBtn = li.querySelector('.rec-revive, .rec-kia');
      if (stateBtn) stateBtn.replaceWith(stateButton(stateBtn.classList.contains('rec-revive')));

      var reset = li.querySelector('.rec-reset');
      reset.addEventListener('click', function () {
        var allocated = parseInt(li.dataset.allocated, 10) || 0;
        Confirm.ask({
          title: 'Reset ' + li.dataset.name + '\u2019s SP allocation?',
          message: 'Gunnery, Piloting and Edge go back to the starting levels, and all ' + allocated + ' allocated SP become unallocated. Their total SP stays the same.' + learnedNote(li),
          confirmLabel: 'Reset SP allocation', danger: true
        }).then(function (ok) {
          if (ok) recAct('/api/pilots/reset', li, reset, '', function () { li.dataset.allocated = 0; showSP(li); clearLearned(li); });
        });
      });

      // Forget the Edge abilities the pilot has learned, so they can choose them again. Their SP is not touched.
      var skillsReset = li.querySelector('.rec-skills-reset');
      skillsReset.addEventListener('click', function () {
        var count = parseInt(li.dataset.learnedCount, 10) || 0;
        Confirm.ask({
          title: 'Reset ' + li.dataset.name + '\u2019s Edge abilities?',
          message: 'They forget the ' + count + ' Edge abilit' + (count === 1 ? 'y' : 'ies') + ' they have learned and can choose them again from the list. Their SP is not touched.',
          confirmLabel: 'Reset Edge abilities', danger: true
        }).then(function (ok) {
          if (!ok) return;
          recAct('/api/pilots/abilities/reset', li, skillsReset, '', function () {
            li.dataset.learnedCount = 0;
            li.querySelector('.rec-n-skills').textContent = 'none';
            skillsReset.disabled = true;
          });
        });
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
        function save() {
          recAct('/api/pilots/total', li, form.querySelector('button[type=submit]'), 'total=' + total + (resets ? '&reset=1' : ''), function () {
            li.dataset.total = total;
            if (resets) { li.dataset.allocated = 0; clearLearned(li); }
            showSP(li);
            form.hidden = true;
          });
        }
        if (!resets) { save(); return; }
        Confirm.ask({
          title: 'Reset the allocation?',
          message: 'Setting ' + li.dataset.name + '\u2019s total to ' + total + ' SP is below the ' + allocated + ' SP already allocated. Doing this resets all of their allocations (Gunnery, Piloting and Edge go back to the starting levels) and leaves ' + total + ' SP unallocated.' + learnedNote(li),
          confirmLabel: 'Set total and reset', danger: true
        }).then(function (ok) { if (ok) save(); });
      });

      // Rank: an advanced correction (promotions come from mission outcomes). The rank button opens the Change rank dialog on top of this one.
      var rankBtn = li.querySelector('.rec-rank-btn');
      if (rankBtn && rankDialog) rankBtn.addEventListener('click', function () { openRank(li); });
      var del = li.querySelector('.rec-delete');
      del.addEventListener('click', function () {
        Confirm.ask({
          title: 'Delete ' + li.dataset.name + '?',
          message: 'This permanently deletes their record and all of their SP. It cannot be undone.',
          confirmLabel: 'Delete pilot', danger: true
        }).then(function (ok) {
          if (!ok) return;
          recAct('/api/pilots/delete', li, del, '', function () {
            var list = li.parentNode;
            li.remove();
            if (!list.querySelector('li[data-pilot]')) list.appendChild(el('li', 'muted', 'No pilots.'));
          });
        });
      });
    });
  }
})();
