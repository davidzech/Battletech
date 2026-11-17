// Scenario page: the deploy popup. It lists the mechs in the player's MekBay hangar, lets them pick the ones going on
// this mission (within the scenario's BV limit), assign named pilots to mechs, and asks the server to sort the hangar into
// Deployed and Hangar groups, crew the mechs and set up the OpFor operation. The server checks everything again; this only
// keeps the player from a doomed request.
//
// Pilots: the campaign's living named pilots sit in a tray above the list. Drag one onto a mech's slot to assign them (or click
// the pilot, then the slot, which also works by keyboard and on touch screens). A pilot drives one mech, so assigning them again
// moves them; dropping a pilot on the tray, or the x on a slot, takes them off. Nothing is saved until Deploy: the assignments
// go to the server as assign_unit / assign_pilot pairs, which writes each pilot's name and current skills into the unit's crew
// in MekBay.
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
  var tray = document.getElementById('deploy-pilots');
  var trayList = document.getElementById('deploy-pilots-list');

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

  // The campaign's living named pilots, as { name, rank, portraitUrl, gunnery, piloting }.
  var pilots = [];
  try { pilots = JSON.parse(dialog.dataset.pilots || '[]') || []; } catch (e) { pilots = []; }
  if (typeof AbilityCard !== 'undefined') { try { AbilityCard.setCatalog(JSON.parse(dialog.dataset.abilities || '[]')); } catch (e) { /* no catalog: the chips just won't explain themselves */ } }
  var crew = {};          // unit ID -> name of the named pilot assigned to it in this popup
  var original = {};      // unit ID -> the named pilot the hangar has in it now, if any
  var crewTouched = false; // the player has moved a pilot, so a fresh read must not undo it
  var held = '';          // a pilot picked up by clicking, waiting to be put in a mech

  function fmt(n) { return Number(n).toLocaleString('en-US'); }

  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }

  function pilotNamed(name) {
    var lower = String(name || '').toLowerCase();
    for (var i = 0; i < pilots.length; i++) if (pilots[i].name.toLowerCase() === lower) return pilots[i];
    return null;
  }
  function unitOf(name) {
    for (var id in crew) if (crew[id] && crew[id].toLowerCase() === name.toLowerCase()) return id;
    return '';
  }
  function unitById(id) {
    for (var i = 0; i < units.length; i++) if (units[i].id === id) return units[i];
    return null;
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

  // ---- Assigning pilots
  function assign(unitId, name) {
    var p = pilotNamed(name);
    if (!p) return;
    for (var id in crew) if (crew[id] && crew[id].toLowerCase() === p.name.toLowerCase()) delete crew[id]; // one pilot, one mech
    crew[unitId] = p.name;
    crewTouched = true;
    held = '';
    error.textContent = '';
    render();
  }
  function unassign(name) {
    var id = unitOf(name);
    if (id) delete crew[id];
    crewTouched = true;
    held = '';
    render();
  }
  function dragName(e) {
    var raw = e.dataTransfer ? e.dataTransfer.getData('text/plain') : '';
    return raw.indexOf('pilot:') === 0 ? raw.slice(6) : '';
  }
  function makeDraggable(node, name) {
    node.draggable = true;
    node.addEventListener('dragstart', function (e) {
      e.dataTransfer.setData('text/plain', 'pilot:' + name);
      e.dataTransfer.effectAllowed = 'move';
      node.classList.add('dragging');
      dialog.classList.add('dragging-pilot');
    });
    node.addEventListener('dragend', function () { node.classList.remove('dragging'); dialog.classList.remove('dragging-pilot'); });
  }

  // A pilot's four numbers in a compact strip of mini stat boxes (a tiny label over a bold number, like the pilot cards on the Manage page):
  // Gunnery, Piloting, Edge tokens and Edge abilities. Hovering it shows the styled card with the full names.
  var STATS = [['gunnery', 'GUN', 'Gunnery'], ['piloting', 'PIL', 'Piloting'], ['edgeTokens', 'EDGE', 'Edge tokens'], ['abilities', 'ABIL', 'Edge abilities']];
  function statSummary(p) { return STATS.map(function (s) { return s[2] + ' ' + (p[s[0]] || 0); }).join(', '); }
  var ARROW = '<svg viewBox="0 0 16 10" width="16" height="10" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M1 5h13M10 1l4 4-4 4"/></svg>';
  // One of the Manage page's stat boxes: the level, the next level, SP progress and its bar (same classes, so the same look).
  function statBox(c) {
    var box = el('div', 'stat-box');
    box.appendChild(el('span', 'stat-label', c.label));
    var main = el('span', 'stat-main'), wrap = el('span', 'stat-valwrap');
    wrap.appendChild(el('span', 'stat-value', String(c.level)));
    if (c.hasNext) {
      var to = el('span', 'stat-to'), arrow = el('span', 'stat-arrow');
      arrow.innerHTML = ARROW; // a constant above, never data
      to.appendChild(arrow);
      to.appendChild(el('b', null, String(c.nextLvl)));
      wrap.appendChild(to);
    }
    main.appendChild(wrap);
    box.appendChild(main);
    box.appendChild(el('span', 'stat-next', c.hasNext ? c.sp + '/' + c.nextAt + ' SP' : 'max'));
    var bar = el('span', 'stat-bar'), fill = el('span');
    fill.style.width = (c.percent || 0) + '%';
    bar.appendChild(fill);
    box.appendChild(bar);
    return box;
  }
  // The hover card for a pilot: portrait, name, rank and SP earned, the four stat boxes and the Edge abilities they have chosen.
  function pilotCard(p) {
    var card = el('div', 'deploy-pilot-card');
    var head = el('div', 'dpc-head');
    head.appendChild(portrait(p));
    var who = el('span');
    who.appendChild(el('b', 'dpc-name', p.name));
    var sub = el('span', 'dpc-sub');
    sub.appendChild(document.createTextNode((p.rank ? p.rank + ' \u00b7 ' : '') + 'SP earned '));
    sub.appendChild(el('b', null, String(p.earned || 0)));
    who.appendChild(sub);
    head.appendChild(who);
    card.appendChild(head);
    var boxes = el('div', 'stat-boxes');
    (p.columns || []).forEach(function (c) { boxes.appendChild(statBox(c)); });
    card.appendChild(boxes);
    var skills = el('div', 'edge-skills');
    skills.appendChild(el('span', 'edge-skills-label', 'Edge abilities'));
    if ((p.learned || []).length) {
      p.learned.forEach(function (a) {
        var chip = el('span', 'skill-chip', a.name);
        chip.tabIndex = 0;
        if (typeof AbilityCard !== 'undefined') AbilityCard.attach(chip, a.id); // hovering one shows the ability card, on top of this card
        skills.appendChild(chip);
      });
    }
    else skills.appendChild(el('span', 'dpc-none', 'None learned yet'));
    card.appendChild(skills);
    return card;
  }
  function attachPilotCard(node, p) {
    if (typeof MechRow.attachTip === 'function') MechRow.attachTip(node, { focusable: false, delay: 500, interactive: true, build: function () { return pilotCard(p); } });
  }

  function statStrip(p) {
    var strip = el('span', 'deploy-stats');
    STATS.forEach(function (s) {
      var cell = el('span', 'deploy-stat');
      cell.appendChild(el('span', 'deploy-stat-label', s[1]));
      cell.appendChild(el('span', 'deploy-stat-value', String(p[s[0]] || 0)));
      strip.appendChild(cell);
    });
    return strip;
  }

  function portrait(p) {
    var img = document.createElement('img');
    img.className = 'deploy-portrait';
    img.src = p.portraitUrl; img.alt = ''; img.width = 32; img.height = 32;
    return img;
  }

  // The tray: every living named pilot. One already in a mech says which, and can still be picked up to move or remove them.
  function renderTray() {
    tray.hidden = !pilots.length;
    trayList.textContent = '';
    pilots.forEach(function (p) {
      var inUnit = unitById(unitOf(p.name));
      var chip = el('button', 'deploy-pilot' + (held === p.name ? ' held' : '') + (inUnit ? ' assigned' : ''));
      chip.type = 'button';
      chip.setAttribute('aria-pressed', held === p.name ? 'true' : 'false');
      chip.setAttribute('aria-label', p.name + ', ' + statSummary(p) + (inUnit ? ', in ' + inUnit.name : ''));
      var top = el('span', 'deploy-pilot-top');
      top.appendChild(portrait(p));
      var text = el('span', 'deploy-pilot-text');
      text.appendChild(el('b', null, p.name));
      text.appendChild(el('small', null, inUnit ? 'in ' + inUnit.name : (p.rank || 'Unassigned')));
      top.appendChild(text);
      chip.appendChild(top);
      chip.appendChild(statStrip(p));
      attachPilotCard(chip, p);
      chip.addEventListener('click', function () { held = held === p.name ? '' : p.name; render(); });
      makeDraggable(chip, p.name);
      trayList.appendChild(chip);
    });
    dialog.classList.toggle('holding-pilot', !!held);
  }
  // The tray also takes a pilot back off a mech.
  tray.addEventListener('dragover', function (e) { if (dragName(e) || (e.dataTransfer && e.dataTransfer.types && e.dataTransfer.types.indexOf('text/plain') >= 0)) { e.preventDefault(); tray.classList.add('over'); } });
  tray.addEventListener('dragleave', function () { tray.classList.remove('over'); });
  tray.addEventListener('drop', function (e) {
    e.preventDefault();
    tray.classList.remove('over');
    var name = dragName(e);
    if (name && unitOf(name)) unassign(name);
  });

  // A mech's pilot slot: the pilot in it (draggable, with an x), or a dashed place to drop one.
  function slot(u, unavailable) {
    var box = el('div', 'deploy-slot' + (unavailable ? ' unavailable' : ''));
    if (!pilots.length || unavailable) return box;
    var p = pilotNamed(crew[u.id]);
    if (p) {
      box.classList.add('filled');
      var top = el('span', 'deploy-pilot-top');
      top.appendChild(portrait(p));
      var text = el('span', 'deploy-pilot-text');
      text.appendChild(el('b', null, p.name));
      text.appendChild(el('small', null, p.rank || 'Pilot'));
      top.appendChild(text);
      var x = el('button', 'deploy-slot-x', '×');
      x.type = 'button';
      x.title = 'Take ' + p.name + ' out of this mech';
      x.setAttribute('aria-label', 'Take ' + p.name + ' out of ' + u.name);
      x.addEventListener('click', function (e) { e.stopPropagation(); unassign(p.name); });
      top.appendChild(x);
      box.appendChild(top);
      box.appendChild(statStrip(p));
      attachPilotCard(box, p);
      makeDraggable(box, p.name);
    } else {
      box.appendChild(el('span', 'deploy-slot-empty', held ? 'Put ' + held + ' here' : 'Drop a pilot here'));
    }
    // A click puts the pilot being held in this mech (this is also how it works by keyboard or on a touch screen).
    box.tabIndex = 0;
    box.setAttribute('role', 'button');
    box.setAttribute('aria-label', 'Pilot for ' + u.name + (p ? ': ' + p.name + ', ' + statSummary(p) : ''));
    function put() { if (held) assign(u.id, held); }
    box.addEventListener('click', put);
    box.addEventListener('keydown', function (e) { if ((e.key === 'Enter' || e.key === ' ') && e.target === box) { e.preventDefault(); put(); } });
    box.addEventListener('dragover', function (e) { e.preventDefault(); box.classList.add('over'); });
    box.addEventListener('dragleave', function () { box.classList.remove('over'); });
    box.addEventListener('drop', function (e) {
      e.preventDefault();
      box.classList.remove('over');
      var name = dragName(e);
      if (name) assign(u.id, name);
    });
    return box;
  }

  function row(u) {
    var unavailable = u.destroyed || u.unknown;
    var li = el('li', 'deploy-unit' + (unavailable ? ' unavailable' : '') + (selected[u.id] ? ' picked' : ''));
    var label = el('label');
    var box = document.createElement('input');
    box.type = 'checkbox';
    box.className = 'themed-box';
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
    // The pilot's skills are shown in the slot, so the row itself leaves out the crew the hangar has now.
    var view = {};
    Object.keys(u).forEach(function (k) { if (k !== 'gunnery' && k !== 'piloting' && k !== 'pilot') view[k] = u[k]; });
    label.appendChild(MechRow.build(pilots.length ? view : u));
    li.appendChild(label);
    if (pilots.length) {
      li.classList.add('has-slot');
      li.appendChild(slot(u, unavailable));
    }
    return li;
  }

  function render() {
    list.textContent = '';
    units.forEach(function (u) { list.appendChild(row(u)); });
    renderTray();
    update();
  }

  // Shows a hangar (cached or live). A choice the player has already made is kept for the units that are still there; before
  // that, the units already in the Deployed group are ticked and the named pilots already in a mech are shown in it.
  function apply(data) {
    var before = selected, beforeCrew = crew;
    units = data.units || [];
    if (data.limit) limit = data.limit;
    selected = {};
    original = {};
    crew = {};
    units.forEach(function (u) {
      if (u.destroyed || u.unknown) return;
      if (touched ? before[u.id] : u.inDeployed) selected[u.id] = true;
    });
    units.forEach(function (u) {
      var p = u.pilot && pilotNamed(u.pilot);
      if (p && !(p.name.toLowerCase() in invert(original))) original[u.id] = p.name;
      if (crewTouched) { if (beforeCrew[u.id]) crew[u.id] = beforeCrew[u.id]; } else if (original[u.id]) crew[u.id] = original[u.id];
    });
    resetRow.hidden = !(data.hasOperation && canModify);
    status.hidden = units.length > 0;
    if (!units.length) status.textContent = 'Your hangar force has no mechs in it yet. Add some in MekBay first.';
    render();
  }
  function invert(map) { var out = {}; for (var k in map) out[map[k].toLowerCase()] = k; return out; }

  // A failed live read keeps a hangar that is already showing, and says so.
  function failed(message) {
    status.hidden = false;
    status.textContent = units.length ? 'Showing your last known hangar. ' + message : message;
  }

  function load() {
    error.textContent = '';
    reset.checked = false;
    touched = false;
    crewTouched = false;
    held = '';
    if (cachedData && cachedData.units) {
      apply(cachedData); // instant
    } else {
      status.hidden = false;
      status.textContent = 'Loading your hangar…';
      list.textContent = '';
      units = [];
      selected = {};
      crew = {};
      original = {};
      resetRow.hidden = true;
      renderTray();
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
    // Every mech that has a named pilot, plus any that had one and no longer does, so skills are brought up to date and removals are saved.
    units.forEach(function (u) {
      var want = crew[u.id] || '', was = original[u.id] || '';
      if (want || want !== was) body += '&assign_unit=' + encodeURIComponent(u.id) + '&assign_pilot=' + encodeURIComponent(want);
    });
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
