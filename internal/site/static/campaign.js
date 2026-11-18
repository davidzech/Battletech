// Campaign page. An unstarted campaign opens the start wizard: a step naming the force, one asking how many players (when the campaign allows a range), one step per starting named pilot (a name and an
// allocator for that pilot's starting SP), then the hangar: built from mechs picked with the unit search (unitsearch.js, the default) or copied from a MekBay
// hangar link (a shortcut for a force already built there), with a step count and progress bar at the bottom. The server checks every field again; this keeps
// the player from a doomed request and previews the pilots' levels.
(function () {
  var box = document.getElementById('campaign-run');
  if (!box) return;

  var UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
  var HANGAR_HELP = 'Paste a MekBay hangar link, like https://mekbay.com/?instance=…';

  // Mirrors the server's reading of a hangar link (parseHangar in setup.go): any MekBay link will do, and only the
  // hangar's instance ID matters. A list of forces ("<mine>,enemy:<theirs>") gives its friendly force.
  function friendlyInstance(list) {
    var entries = (list || '').split(',');
    for (var i = 0; i < entries.length; i++) {
      var entry = entries[i].trim().toLowerCase();
      if (entry.indexOf('enemy:') === 0) continue;
      return UUID.test(entry) ? entry : '';
    }
    return '';
  }

  function mekbayUrl(raw) {
    var u;
    try { u = new URL(raw.trim()); } catch (e) { return null; }
    var host = u.hostname.toLowerCase();
    if (u.protocol !== 'https:' || u.username || u.password || u.port || (host !== 'mekbay.com' && host !== 'www.mekbay.com')) return null;
    return u;
  }

  // The link reduced to just the hangar, https://mekbay.com/?instance=<id>, or '' if it isn't a hangar link.
  function canonicalHangar(raw) {
    var u = mekbayUrl(raw);
    var id = u ? friendlyInstance(u.searchParams.get('instance')) : '';
    return id ? 'https://mekbay.com/?instance=' + id : '';
  }

  function hangarProblem(raw) {
    if (canonicalHangar(raw)) return '';
    var u = mekbayUrl(raw);
    if (!u) return HANGAR_HELP;
    if (u.searchParams.get('operation') && !u.searchParams.get('instance')) return 'That is an operation link. Paste the link to your hangar instead (it has ?instance=\u2026).';
    return 'That MekBay link has no valid instance id (?instance=\u2026).';
  }

  // A pasted link often carries more than the hangar (the selected unit, an operation, enemy forces...). As soon as it is
  // recognised as a hangar link the box is rewritten to just the hangar, so what is shown is what is used.
  function trimField(input) {
    var plain = canonicalHangar(input.value);
    if (plain && plain !== input.value.trim()) input.value = plain;
    return input.value;
  }

  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }

  function post(body) {
    return fetch('/api/campaign/start', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: body,
      credentials: 'same-origin'
    });
  }

  // ---- An unstarted campaign: the start wizard.
  var wiz = document.getElementById('start-wizard');
  var wizOpen = document.getElementById('wizard-open');
  if (!wiz || !wizOpen || typeof wiz.showModal !== 'function') return;

  var ladders = JSON.parse(wiz.dataset.ladders);
  var budget = parseInt(wiz.dataset.sp, 10) || 0;
  var STEP = 10; // the sliders and the number boxes' arrows both move this many SP at a time
  var pilotCount = parseInt(wiz.dataset.pilots, 10) || 0;
  // When the campaign allows a range of players, the first step asks how many will play (the Deploy wizard's picker); that is only the default count each Deploy
  // starts with. Then a step per pilot, then the hangar link.
  var maxPlayers = Math.min(4, Math.max(1, parseInt(wiz.dataset.maxPlayers, 10) || 1));
  var minPlayers = Math.min(maxPlayers, Math.max(1, parseInt(wiz.dataset.minPlayers, 10) || 1));
  var asksPlayers = minPlayers < maxPlayers;
  var lead = 1 + (asksPlayers ? 1 : 0); // steps before the pilots: the force's name, then the player count when it is asked
  var forceName = '';
  var players = minPlayers;
  var total = lead + pilotCount + 1;

  var title = document.getElementById('wiz-title');
  var body = document.getElementById('wiz-body');
  var error = document.getElementById('wiz-error');
  var hint = document.getElementById('wiz-hint');
  var back = document.getElementById('wiz-back');
  var next = document.getElementById('wiz-next');
  var count = document.getElementById('wiz-count');
  var progress = document.getElementById('wiz-progress');
  var bar = document.getElementById('wiz-bar');

  var portraits = JSON.parse(wiz.dataset.portraits || '[]');
  // The Edge ability catalog and its card are shared with the Manage page's picker (abilitycard.js).
  var abilityCatalog = JSON.parse(wiz.dataset.abilities || '[]');
  var CATEGORY_ORDER = ['Attack', 'Indirect Fire', 'Defense', 'Heat', 'Melee', 'Movement'];
  if (window.AbilityCard) AbilityCard.setCatalog(abilityCatalog);
  // How many Edge abilities a pilot's allocation entitles them to choose: their Edge abilities level.
  function abilitySlots(p) {
    for (var i = 0; i < ladders.length; i++) if (ladders[i].key === 'edge_abilities') return reach(ladders[i], p.sp.edge_abilities).level;
    return 0;
  }
  var pilots = [];
  for (var i = 0; i < pilotCount; i++) {
    var sp = {};
    ladders.forEach(function (l) { sp[l.key] = 0; });
    pilots.push({ name: '', sp: sp, portrait: '', abilities: [] });
  }
  // Every pilot starts with a random portrait nobody else has been given.
  pilots.forEach(function (p) {
    var free = portraits.filter(function (q) { return !pilots.some(function (o) { return o.portrait === q.file; }); });
    if (free.length) p.portrait = free[Math.floor(Math.random() * free.length)].file;
  });
  var hangar = '';
  // The last step makes the hangar one of two ways: built from mechs picked here ('build', the default) or copied from a MekBay link ('link', a shortcut for a
  // force already built there). picks are the mechs picked, one entry per mech (a design can be picked more than once).
  var hangarMode = 'build';
  var picks = [];
  var maxBV = parseInt(wiz.dataset.maxBv, 10) || 0;
  var minMechs = parseInt(wiz.dataset.minMechs, 10) || 0;
  var unitSearch = null, picksBox = null, pickSummary = null;
  var step = 0;
  var busy = false;

  // The ladder maths, the sliders and the stat boxes are shared with the Manage SP dialog (allocator.js).
  function reach(l, spent) { return SPAllocator.reach(l, spent); }
  function spentOf(p) { return SPAllocator.spentOf(p, ladders); }
  function allocatorRow(p, l, refresh) {
    return SPAllocator.row(p, l, { ladders: ladders, budget: budget, step: STEP, refresh: refresh, onEdit: function () { error.textContent = ''; } });
  }
  function statBox(l) { return SPAllocator.statBox(l); }

  // The first step: this campaign's force gets its own name, to suit the campaign (it can be changed later on the Manage page).
  function renderForceStep() {
    title.textContent = 'Name your force';
    body.textContent = '';
    body.appendChild(el('p', 'muted', 'Every campaign has its own force, named to suit it: a mercenary company, a Clan Star, a house regiment. You can rename it later on the Manage page.'));
    var label = el('label', null, 'Force name');
    label.setAttribute('for', 'wiz-force');
    var input = document.createElement('input');
    input.type = 'text'; input.id = 'wiz-force'; input.className = 'wide'; input.maxLength = 40; input.autocomplete = 'off'; input.spellcheck = false;
    input.placeholder = window.PilotNames ? PilotNames.randomForce() : 'Wolf’s Dragoons';
    input.value = forceName;
    input.addEventListener('input', function () { forceName = input.value; error.textContent = ''; updateNext(); });
    body.appendChild(label);
    body.appendChild(input);
    input.focus();
  }

  function renderPlayersStep() {
    title.textContent = 'How many players?';
    body.textContent = '';
    body.appendChild(el('p', 'muted', 'This campaign can be played by ' + minPlayers + ' to ' + maxPlayers + ' players sharing one force. The number you choose is what every Deploy starts with; you can still change it for a single deploy.'));
    var pick = el('div', 'deploy-players-pick');
    for (var n = minPlayers; n <= maxPlayers; n++) {
      (function (n) {
        var b = el('button', 'deploy-count', String(n));
        b.type = 'button';
        b.setAttribute('aria-pressed', n === players ? 'true' : 'false');
        b.setAttribute('aria-label', n + (n === 1 ? ' player' : ' players'));
        b.addEventListener('click', function () {
          players = n;
          [].forEach.call(pick.children, function (c, i) { c.setAttribute('aria-pressed', minPlayers + i === players ? 'true' : 'false'); });
        });
        pick.appendChild(b);
      })(n);
    }
    body.appendChild(pick);
  }

  function renderPilot(idx) {
    var p = pilots[idx];
    title.textContent = 'Named pilot ' + (idx + 1) + ' of ' + pilotCount;
    body.textContent = '';
    body.appendChild(el('p', 'muted', 'Only named pilots improve their skills, use Edge and earn abilities. Name this pilot, then allocate their ' + budget + ' starting SP across the four columns. Every point has to go somewhere.'));

    // The name, with the pilot's four numbers beside it.
    var top = el('div', 'pilot-top');
    var idBlock = el('div', 'pilot-id');
    var nameBlock = el('div', 'pilot-name');
    // The portrait: click it to pick another from the grid.
    var portraitBtn = el('button', 'portrait-btn');
    portraitBtn.type = 'button';
    portraitBtn.title = 'Choose a portrait';
    portraitBtn.setAttribute('aria-label', 'Choose a portrait');
    var portraitImg = document.createElement('img');
    portraitImg.alt = ''; portraitImg.width = 96; portraitImg.height = 96;
    portraitImg.src = '/portraits/' + p.portrait;
    portraitBtn.appendChild(portraitImg);
    portraitBtn.appendChild(el('span', 'portrait-edit', 'Change'));
    portraitBtn.addEventListener('click', function () {
      if (typeof PortraitPicker === 'undefined') return;
      PortraitPicker.open({
        portraits: portraits, selected: p.portrait,
        taken: pilots.filter(function (o) { return o !== p; }).map(function (o) { return o.portrait; }),
        onPick: function (file) {
          p.portrait = file; portraitImg.src = '/portraits/' + file;
          // The placeholder name follows the portrait's gender (only while the player hasn't typed a name of their own).
          if (window.PilotNames && !nameInput.value) nameInput.placeholder = PilotNames.random(PilotNames.groupOf(portraits, file));
        }
      });
    });

    var nameLabel = el('label', null, 'Name');
    nameLabel.setAttribute('for', 'wiz-name');
    var nameInput = document.createElement('input');
    nameInput.type = 'text'; nameInput.id = 'wiz-name'; nameInput.maxLength = 30; nameInput.autocomplete = 'off';
    nameInput.placeholder = window.PilotNames ? PilotNames.random(PilotNames.groupOf(portraits, p.portrait)) : 'Alrek'; nameInput.spellcheck = false; nameInput.value = p.name;
    nameInput.addEventListener('input', function () { p.name = nameInput.value; error.textContent = ''; updateNext(); });
    nameBlock.appendChild(nameLabel);
    nameBlock.appendChild(nameInput);
    var boxes = el('div', 'stat-boxes');
    var stats = ladders.map(statBox);
    stats.forEach(function (b) { boxes.appendChild(b); });
    idBlock.appendChild(portraitBtn);
    idBlock.appendChild(nameBlock);
    top.appendChild(idBlock);
    top.appendChild(boxes);
    body.appendChild(top);

    var remaining = el('p', 'alloc-remaining');
    var rows = ladders.map(function (l) { return allocatorRow(p, l, refresh); });
    // Edge abilities: one combo box per ability the allocation pays for, which have to be chosen before moving on. Moving SP out of the
    // Edge abilities column until the level drops removes the choices that no longer fit, the last ones first.
    var abilityBox = el('div', 'wiz-abilities');
    var shownSlots = -1;
    function renderAbilities() {
      var n = abilitySlots(p);
      if (p.abilities.length > n) p.abilities.length = n;
      while (p.abilities.length < n) p.abilities.push('');
      if (n === shownSlots) return;
      shownSlots = n;
      abilityBox.textContent = '';
      if (!n) return;
      abilityBox.appendChild(el('h3', 'alloc-head', n === 1 ? 'Choose an Edge ability' : 'Choose ' + n + ' Edge abilities'));
      abilityBox.appendChild(el('p', 'muted', 'Your Edge abilities level lets this pilot start with ' + (n === 1 ? 'an Edge ability' : n + ' Edge abilities') + '. The choice is permanent. Lowering the Edge abilities SP above removes the choices that no longer fit.'));
      var selects = [];
      function sync() {
        selects.forEach(function (s, i) {
          [].forEach.call(s.options, function (o) { o.disabled = o.value !== '' && p.abilities.some(function (c, j) { return j !== i && c === o.value; }); });
          AbilityCard.fill(s.parentNode.querySelector('.skill-card'), AbilityCard.byId(s.value));
        });
        updateNext();
      }
      for (var k = 0; k < n; k++) (function (k) {
        var slot = el('div', 'skill-slot');
        var label = el('label', null, 'Edge ability ' + (n > 1 ? (k + 1) : ''));
        var select = document.createElement('select');
        select.id = 'wiz-ability-' + k;
        label.setAttribute('for', select.id);
        var blank = el('option', null, 'Choose an ability…');
        blank.value = '';
        select.appendChild(blank);
        var categories = CATEGORY_ORDER.slice();
        abilityCatalog.forEach(function (a) { if (categories.indexOf(a.category) < 0) categories.push(a.category); });
        categories.forEach(function (cat) {
          var inCat = abilityCatalog.filter(function (a) { return a.category === cat; });
          if (!inCat.length) return;
          var group = document.createElement('optgroup');
          group.label = cat;
          inCat.forEach(function (a) { var o = el('option', null, a.name); o.value = a.id; group.appendChild(o); });
          select.appendChild(group);
        });
        select.value = p.abilities[k] || '';
        select.addEventListener('change', function () { p.abilities[k] = select.value; error.textContent = ''; sync(); });
        slot.appendChild(label);
        slot.appendChild(select);
        slot.appendChild(el('div', 'skill-card empty'));
        abilityBox.appendChild(slot);
        selects.push(select);
      })(k);
      sync();
    }
    function refresh() {
      var left = budget - spentOf(p);
      remaining.textContent = left + ' of ' + budget + ' SP left to allocate';
      remaining.classList.toggle('done', left === 0);
      rows.forEach(function (r) { r.show(); });
      stats.forEach(function (b, i) { b.show(p.sp[ladders[i].key]); });
      renderAbilities();
      updateNext();
    }
    body.appendChild(el('h3', 'alloc-head', 'Allocate SP'));
    body.appendChild(remaining);
    rows.forEach(function (r) { body.appendChild(r); });
    body.appendChild(abilityBox);
    refresh();
    nameInput.focus();
  }

  // What the server made of the pasted hangar: idle, checking, error (with a message) or done (with the result).
  var check = { link: '', state: 'idle', message: '', result: null };
  var checkTimer = null;
  var checkSeq = 0;
  var statusBox = null;

  function number(n) { return Number(n).toLocaleString('en-US'); }

  // Reads the hangar behind the link and checks it against the campaign's starting conditions. A reply for a link
  // the player has since changed is ignored.
  function runCheck(link) {
    var seq = ++checkSeq;
    fetch('/api/campaign/hangar-check?campaign=' + encodeURIComponent(wiz.dataset.campaign) + '&hangar=' + encodeURIComponent(link), {
      credentials: 'same-origin',
      cache: 'no-store'
    }).then(function (res) {
      if (check.link !== link || seq !== checkSeq) return;
      if (!res.ok) {
        return res.text().then(function (t) {
          if (check.link !== link || seq !== checkSeq) return;
          failed(link, t.trim() || 'Could not read your hangar.');
        });
      }
      return res.json().then(function (data) {
        if (check.link !== link || seq !== checkSeq) return;
        // Identical data (the usual case) needs no repaint: only the "Updating" note goes away.
        var same = check.state === 'done' && JSON.stringify(check.result) === JSON.stringify(data);
        check = { link: link, state: 'done', message: '', result: data };
        if (same) { syncNotes(); syncRefresh(); } else { renderCheck(); }
        updateNext();
      });
    }, function () {
      if (check.link !== link || seq !== checkSeq) return;
      failed(link, 'Could not reach the server.');
    });
  }

  // A read failed. If it was a refresh, the result already on screen stays (it is still the latest data we have) and a
  // note says the refresh didn't work; otherwise the error replaces the checking state.
  function failed(link, message) {
    if (check.state === 'done' && check.refreshing) {
      check.refreshing = false;
      check.refreshError = message;
      syncNotes(); syncRefresh();
    } else {
      check = { link: link, state: 'error', message: message, result: null };
      renderCheck();
    }
    updateNext();
  }

  // Called whenever the link changes: once it has the right shape, read the hangar (after a short pause, so typing
  // doesn't send a request per key).
  function scheduleCheck(delay) {
    var link = hangar.trim();
    if (hangarProblem(link)) { clearTimeout(checkTimer); check = { link: '', state: 'idle', message: '', result: null }; renderCheck(); return; }
    // The same link as before (a keystroke that trims back to it, say): whatever is pending or already read stands.
    // Clearing the pending timer here would cancel its read for good.
    if (check.link === link && check.state !== 'idle') { renderCheck(); return; }
    clearTimeout(checkTimer);
    check = { link: link, state: 'checking', message: '', result: null };
    renderCheck();
    checkTimer = setTimeout(function () { runCheck(link); }, delay);
  }

  // Reads the current link again at once. The player edits their hangar in MekBay and comes back, and the link
  // hasn't changed, so nothing else would make the wizard look again.
  function forceCheck() {
    clearTimeout(checkTimer);
    var link = hangar.trim();
    if (hangarProblem(link)) return;
    if (check.link === link && check.state === 'done') {
      // The same hangar again: keep what is shown, and swap it for the new data once it is here.
      check.refreshing = true;
      check.refreshError = '';
      syncNotes();
      syncRefresh();
    } else {
      check = { link: link, state: 'checking', message: '', result: null };
      renderCheck();
    }
    updateNext();
    runCheck(link);
  }

  // The refresh icon inside the link box: enabled once the link has the right shape, spinning while it is read.
  var refreshBtn = null;
  var REFRESH_ICON = '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M21 12a9 9 0 1 1-2.64-6.36"/><path d="M21 3v6h-6"/></svg>';
  function syncRefresh() {
    if (!refreshBtn) return;
    var reading = check.state === 'checking' || !!check.refreshing;
    refreshBtn.disabled = reading || hangarProblem(hangar.trim()) !== '';
    refreshBtn.classList.toggle('spinning', reading);
  }

  // The small notes on a finished result: "Updating" while it is read again, and why a refresh failed. They are added
  // and removed in place, so the rest of the result is never repainted.
  function syncNotes() {
    if (!statusBox) return;
    var summary = statusBox.querySelector('.hc-summary');
    if (!summary) return;
    var updating = summary.querySelector('.hc-updating');
    if (check.refreshing && !updating) {
      updating = el('span', 'hc-updating');
      updating.setAttribute('role', 'status');
      updating.appendChild(el('span', 'spinner'));
      updating.appendChild(el('span', null, 'Updating\u2026'));
      summary.appendChild(updating);
    } else if (!check.refreshing && updating) {
      updating.remove();
    }
    var note = statusBox.querySelector('.hc-refresh-error');
    if (check.refreshError) {
      if (!note) {
        note = el('p', 'hc-problem hc-refresh-error');
        summary.parentNode.insertBefore(note, summary.nextSibling);
      }
      note.textContent = 'Couldn\u2019t refresh: ' + check.refreshError;
    } else if (note) {
      note.remove();
    }
  }

  function renderCheck() {
    syncRefresh();
    if (!statusBox) return;
    statusBox.textContent = '';
    if (check.state === 'checking') {
      var loading = el('p', 'hc-loading');
      loading.setAttribute('role', 'status');
      loading.appendChild(el('span', 'spinner'));
      loading.appendChild(el('span', null, 'Reading your hangar\u2026'));
      statusBox.appendChild(loading);
    } else if (check.state === 'error') {
      statusBox.appendChild(el('p', 'hc-problem', check.message));
    } else if (check.state === 'done') {
      var r = check.result;
      var summary = el('p', 'hc-summary');
      var mechs = el('span', r.minMechs && r.mechs < r.minMechs ? 'bad' : null);
      mechs.appendChild(el('b', null, String(r.mechs)));
      mechs.appendChild(document.createTextNode(r.mechs === 1 ? ' mech' : ' mechs'));
      summary.appendChild(mechs);
      var bv = el('span', r.maxBV && r.bv > r.maxBV ? 'bad' : null);
      bv.appendChild(el('b', null, number(r.bv)));
      bv.appendChild(document.createTextNode(r.maxBV ? ' of ' + number(r.maxBV) + ' BV' : ' BV'));
      summary.appendChild(bv);
      statusBox.appendChild(summary);
      r.problems.forEach(function (t) { statusBox.appendChild(el('p', 'hc-problem', t)); });
      if (r.units.length) {
        var list = el('ul', 'hc-units');
        r.units.forEach(function (u) {
          var li = el('li');
          li.appendChild(MechRow.build({
            name: u.name, model: u.model, role: u.role, icon: u.icon, tons: u.tons, bv: u.bv,
            gunnery: u.gunnery, piloting: u.piloting, unknown: u.unknown, notMech: !u.mech && !u.unknown
          }));
          list.appendChild(li);
        });
        statusBox.appendChild(list);
      }
      syncNotes();
    }
  }

  function renderHangar() {
    title.textContent = hangarMode === 'build' ? 'Build your force' : 'Link your hangar';
    body.textContent = '';
    if (pilotCount) {
      body.appendChild(el('p', 'muted', 'Your named pilots:'));
      var list = el('ul', 'wiz-recap');
      pilots.forEach(function (p) {
        var li = el('li');
        li.appendChild(el('b', null, p.name.trim()));
        li.appendChild(el('span', 'muted', ' \u2014 '));
        ladders.forEach(function (l, i) {
          if (i) li.appendChild(el('span', 'muted', ' \u00b7 '));
          li.appendChild(el('span', 'muted', l.name + ' '));
          li.appendChild(el('b', 'recap-num', String(reach(l, p.sp[l.key]).level)));
        });
        list.appendChild(li);
      });
      body.appendChild(list);
    }
    // The two ways to make the hangar, as a segmented switch.
    var modes = el('div', 'wiz-modes');
    modes.setAttribute('role', 'group');
    modes.setAttribute('aria-label', 'How to make your hangar');
    [['build', 'Pick your mechs'], ['link', 'Use a MekBay link']].forEach(function (m) {
      var b = el('button', 'wiz-mode' + (hangarMode === m[0] ? ' active' : ''), m[1]);
      b.type = 'button';
      b.setAttribute('aria-pressed', hangarMode === m[0] ? 'true' : 'false');
      b.addEventListener('click', function () { if (hangarMode !== m[0]) { hangarMode = m[0]; render(); } });
      modes.appendChild(b);
    });
    body.appendChild(modes);
    if (hangarMode === 'build') { renderBuild(); return; }
    statusBox = null;
    body.appendChild(el('p', 'muted', 'Paste the link to this campaign\u2019s MekBay hangar, the force holding all of your mechs. We read it to check it meets the starting conditions.'));
    var label = el('label', null, 'MekBay hangar link');
    label.setAttribute('for', 'wiz-hangar');
    var input = document.createElement('input');
    input.id = 'wiz-hangar'; input.className = 'wide'; input.type = 'url'; input.spellcheck = false;
    input.placeholder = 'https://mekbay.com/?instance=\u2026'; input.value = hangar;
    input.addEventListener('input', function () {
      hangar = trimField(input); error.textContent = '';
      scheduleCheck(350);
      syncRefresh();
      updateNext();
    });
    // The link box, with a refresh icon embedded on its right: the player fixes the hangar in MekBay without
    // changing the link, so this reads it again.
    var field = el('div', 'link-field');
    refreshBtn = el('button', 'refresh');
    refreshBtn.type = 'button';
    refreshBtn.title = 'Check the hangar again';
    refreshBtn.setAttribute('aria-label', 'Check the hangar again');
    refreshBtn.innerHTML = REFRESH_ICON;
    refreshBtn.addEventListener('click', forceCheck);
    field.appendChild(input);
    field.appendChild(refreshBtn);
    body.appendChild(label);
    body.appendChild(field);
    statusBox = el('div', 'hangar-check');
    statusBox.setAttribute('aria-live', 'polite');
    body.appendChild(statusBox);
    scheduleCheck(0); // a link entered earlier (then Back and forward) is checked again straight away
    input.focus();
  }

  // ---- Build your force: the mechs of the starting hangar, picked from the unit list (unitsearch.js). A result is added each time it is clicked; each picked
  // mech has a remove button. The summary follows the campaign's starting conditions, which the server checks again when the campaign starts.
  function pickCount(id) { return picks.filter(function (u) { return u.unit === id; }).length; }
  function picksBV() { return picks.reduce(function (n, u) { return n + (Number(u.bv) || 0); }, 0); }
  function buildProblem() {
    if (!picks.length) return 'Pick the mechs of your starting force.';
    if (minMechs && picks.length < minMechs) return 'You need at least ' + minMechs + ' mechs (you have ' + picks.length + ').';
    if (maxBV && picksBV() > maxBV) return 'Your force totals ' + number(picksBV()) + ' BV, over the ' + number(maxBV) + ' BV starting limit.';
    return '';
  }
  function renderPicks() {
    if (!picksBox) return;
    picksBox.textContent = '';
    pickSummary.textContent = '';
    var mechs = el('span', minMechs && picks.length < minMechs ? 'bad' : null);
    mechs.appendChild(el('b', null, String(picks.length)));
    mechs.appendChild(document.createTextNode((picks.length === 1 ? ' mech' : ' mechs') + (minMechs ? ' (at least ' + minMechs + ')' : '')));
    pickSummary.appendChild(mechs);
    var bv = el('span', maxBV && picksBV() > maxBV ? 'bad' : null);
    bv.appendChild(el('b', null, number(picksBV())));
    bv.appendChild(document.createTextNode(maxBV ? ' of ' + number(maxBV) + ' BV' : ' BV'));
    pickSummary.appendChild(bv);
    if (!picks.length) picksBox.appendChild(el('p', 'muted purchase-empty', 'No mechs yet. Search above and click a mech to add it to your force.'));
    picks.forEach(function (u, i) {
      var row = UnitSearch.plain(u);
      row.picked = true;
      row.actions = [{ icon: '\u00d7', label: 'Remove', title: 'Remove from your force', onClick: function () { picks.splice(i, 1); renderPicks(); unitSearch.repaint(); updateNext(); } }];
      picksBox.appendChild(MechRow.build(row));
    });
  }
  function renderBuild() {
    var limits = [];
    if (minMechs) limits.push('at least ' + minMechs + ' mechs');
    if (maxBV) limits.push('up to ' + number(maxBV) + ' BV in all');
    body.appendChild(el('p', 'muted', 'Pick the mechs of your starting force' + (limits.length ? ': ' + limits.join(', ') : '') + '. Click a mech to add it, and again to add another of it; right-click it to take one off. Each starts fully repaired, with an unnamed 4/5 pilot.'));
    unitSearch = UnitSearch.create({
      campaign: wiz.dataset.campaign, faction: wiz.dataset.faction, era: wiz.dataset.era, idPrefix: 'wiz-units',
      decorate: function (row, u) { var n = pickCount(u.unit); if (n) { row.picked = true; row.badge = n === 1 ? 'In force' : n + ' in force'; } },
      onPick: function (u) { picks.push(u); renderPicks(); unitSearch.repaint(); updateNext(); },
      // A right-click takes one of that mech back off (the last one picked), down to none.
      onUnpick: function (u) {
        for (var i = picks.length - 1; i >= 0; i--) if (picks[i].unit === u.unit) { picks.splice(i, 1); break; }
        renderPicks(); unitSearch.repaint(); updateNext();
      }
    });
    body.appendChild(unitSearch.element);
    body.appendChild(el('p', 'purchase-staged-head', 'Your force'));
    pickSummary = el('p', 'hc-summary');
    body.appendChild(pickSummary);
    picksBox = el('div', 'mech-list purchase-staged');
    body.appendChild(picksBox);
    renderPicks();
    unitSearch.focus();
  }

  function render() {
    error.textContent = '';
    if (step === 0) renderForceStep(); else if (asksPlayers && step === 1) renderPlayersStep(); else if (step - lead < pilotCount) renderPilot(step - lead); else renderHangar();
    count.textContent = 'Step ' + (step + 1) + ' of ' + total;
    bar.style.width = ((step + 1) / total * 100) + '%';
    progress.setAttribute('aria-valuemax', total);
    progress.setAttribute('aria-valuenow', step + 1);
    back.hidden = step === 0;
    next.textContent = step === total - 1 ? 'Start campaign' : 'Next';
    updateNext();
  }

  // Next stays disabled until everything this step needs has been decided, and the line beside it says what is
  // still missing, so a dimmed button is never a mystery.
  function updateNext() {
    var missing = problem();
    next.disabled = busy || missing !== '';
    // Once the hangar has been read, the status box right above already spells out what is wrong, so the hint stays
    // quiet there (the reason is still the button's tooltip).
    var spelledOut = step >= lead + pilotCount && (hangarMode === 'build' ? picks.length > 0 : check.link === hangar.trim() && (check.state === 'done' || check.state === 'error'));
    hint.textContent = busy || spelledOut ? '' : missing;
    if (missing) next.title = missing; else next.removeAttribute('title');
  }

  // The problem with the current step, or ''.
  function problem() {
    if (step === 0) return forceName.trim() ? '' : 'Name your force.';
    if (asksPlayers && step === 1) return '';
    if (step >= lead + pilotCount && hangarMode === 'build') return buildProblem();
    if (step >= lead + pilotCount) {
      var bad = hangarProblem(hangar);
      if (bad) return bad;
      if (check.link !== hangar.trim() || check.state === 'checking' || check.state === 'idle') return 'Checking your hangar\u2026';
      if (check.state === 'error') return check.message;
      if (!check.result.ok) return check.result.problems[0];
      return '';
    }
    var p = pilots[step - lead], name = p.name.trim();
    if (!name) return 'Name your pilot.';
    var clash = pilots.some(function (o, j) { return j !== step - lead && o.name.trim().toLowerCase() === name.toLowerCase(); });
    if (clash) return 'Two of your pilots can’t share a name.';
    var left = budget - spentOf(p);
    if (left !== 0) return 'Allocate all ' + budget + ' SP before moving on (' + left + ' left).';
    var open = p.abilities.filter(function (a) { return !a; }).length;
    if (open) return 'Choose ' + (open === 1 ? 'your pilot’s Edge ability' : 'all ' + p.abilities.length + ' Edge abilities') + ' before moving on.';
    return '';
  }

  function submit() {
    var parts = ['campaign=' + encodeURIComponent(wiz.dataset.campaign), 'force_name=' + encodeURIComponent(forceName.trim()), 'players=' + players];
    if (hangarMode === 'build') picks.forEach(function (u) { parts.push('unit=' + encodeURIComponent(u.unit)); });
    else parts.push('hangar=' + encodeURIComponent(canonicalHangar(hangar) || hangar.trim()));
    pilots.forEach(function (p, idx) {
      parts.push('pilot' + idx + '_name=' + encodeURIComponent(p.name.trim()));
      if (p.portrait) parts.push('pilot' + idx + '_portrait=' + encodeURIComponent(p.portrait));
      ladders.forEach(function (l) { parts.push('pilot' + idx + '_' + l.key + '=' + p.sp[l.key]); });
      p.abilities.forEach(function (a) { parts.push('pilot' + idx + '_ability=' + encodeURIComponent(a)); });
    });
    busy = true;
    updateNext();
    post(parts.join('&')).then(function (res) {
      if (res.ok) { location.reload(); return; }
      busy = false;
      updateNext();
      return res.text().then(function (t) { error.textContent = t.trim() || 'Could not start the campaign.'; });
    }, function () { busy = false; updateNext(); error.textContent = 'Could not reach the server.'; });
  }

  document.getElementById('wiz-form').addEventListener('submit', function (e) {
    e.preventDefault();
    var bad = problem();
    if (bad) { error.textContent = bad; return; }
    if (step < total - 1) { step++; render(); } else { submit(); }
  });
  back.addEventListener('click', function () { if (step > 0) { step--; render(); } });
  document.getElementById('wiz-cancel').addEventListener('click', function () { wiz.close(); });
  wizOpen.addEventListener('click', function () { step = 0; wiz.showModal(); render(); });
  function comingBack() {
    var failing = check.state === 'error' || (check.state === 'done' && !check.result.ok && !check.refreshing);
    if (wiz.hasAttribute('open') && step >= lead + pilotCount && hangarMode === 'link' && failing) forceCheck();
  }
  window.addEventListener('focus', comingBack);
  document.addEventListener('visibilitychange', function () { if (!document.hidden) comingBack(); });
})();
