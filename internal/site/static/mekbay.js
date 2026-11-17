// Scenario page: the Deploy wizard. The company's mechs (its MekBay hangar) and named pilots are one shared pool, and the players divide it: each takes the mechs
// and pilots they want from what the players before them left, and the last step reviews the whole plan.
//
//   1 player:  one step (pick mechs, assign pilots, Deploy).
//   2-4:       a "Players" picker on the first step (within the campaign's min_players and max_players), then one step per player, then a review, with a
//              progress bar and "Step n of m" at the bottom.
//
// A player's mechs are the ones they tick; the Pilots tray above the list holds the named pilots nobody else has taken. Drag a pilot onto a ticked mech's slot to
// assign them (or click the pilot, then the slot, which also works by keyboard and on touch screens). A pilot drives one mech, so assigning them again moves them;
// dropping a pilot on the tray, or the x on a slot, takes them off. A mech another player took is not offered, and neither is a pilot they took.
//
// Deploy sends the whole plan to POST /api/deploy/plan, which checks it on the server (the same rules, over every player's mechs together) and describes it back.
// It does NOT build anything in MekBay yet: that, each player's force in their own MekBay account and the OpFor in the OpFor's, is the next step.
(function () {
  var dialog = document.getElementById('deploy-dialog');
  var openBtn = document.getElementById('deploy-open');
  if (!dialog || !openBtn || typeof dialog.showModal !== 'function') return;

  var list = document.getElementById('deploy-list');
  var meter = document.getElementById('deploy-meter');
  var status = document.getElementById('deploy-status');
  var error = document.getElementById('deploy-error');
  var go = document.getElementById('deploy-go');
  var next = document.getElementById('deploy-next');
  var back = document.getElementById('deploy-back');
  var cancel = document.getElementById('deploy-cancel');
  var resetRow = document.getElementById('deploy-reset-row');
  var reset = document.getElementById('deploy-reset');
  var tray = document.getElementById('deploy-pilots');
  var trayList = document.getElementById('deploy-pilots-list');
  var playersBox = document.getElementById('deploy-players');
  var playersPick = document.getElementById('deploy-players-pick');
  var stepTitle = document.getElementById('deploy-step');
  var review = document.getElementById('deploy-review');
  var progress = document.getElementById('deploy-progress');
  var progressTrack = document.getElementById('deploy-progress-track');
  var bar = document.getElementById('deploy-bar');
  var count = document.getElementById('deploy-count');

  var limit = parseInt(dialog.dataset.limit, 10) || 0;
  var query = 'campaign=' + encodeURIComponent(dialog.dataset.campaign) + '&scenario=' + encodeURIComponent(dialog.dataset.scenario);
  var MAX = 4; // the most players a company can have MekBay accounts for
  var maxPlayers = Math.min(MAX, Math.max(1, parseInt(dialog.dataset.maxPlayers, 10) || 1));
  var minPlayers = Math.min(maxPlayers, Math.max(1, parseInt(dialog.dataset.minPlayers, 10) || 1));

  var units = [];         // the hangar's mechs, as the server describes them
  var original = {};      // unit ID -> the named pilot the hangar has in it now, if any
  var plans = [];         // per player: { selected: { unitId: true }, crew: { unitId: pilot name } }
  var playerCount = minPlayers;
  var step = 0;
  var finished = null;    // the server's answer once the plan has been checked
  var touched = false;    // the player has changed something, so a fresh read of the hangar must not undo it
  var busy = false;
  var held = '';          // a pilot picked up by clicking, waiting to be put in a mech
  // The hangar as the server last read it from MekBay, embedded in the page: shown at once when the popup opens, then replaced by the live read.
  var cachedData = null;
  try { cachedData = JSON.parse(dialog.dataset.cached || 'null'); } catch (e) { cachedData = null; }

  // The campaign's living named pilots, as Page.PilotsJSON sends them.
  var pilots = [];
  try { pilots = JSON.parse(dialog.dataset.pilots || '[]') || []; } catch (e) { pilots = []; }
  if (typeof AbilityCard !== 'undefined') { try { AbilityCard.setCatalog(JSON.parse(dialog.dataset.abilities || '[]')); } catch (e) { /* no catalog: the chips just won't explain themselves */ } }

  function fmt(n) { return Number(n).toLocaleString('en-US'); }
  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }

  // ---- The plan
  function plan(i) { if (!plans[i]) plans[i] = { selected: {}, crew: {} }; return plans[i]; }
  function cur() { return plan(step); }
  function stepCount() { return playerCount > 1 ? playerCount + 1 : 1; }
  function isReview() { return playerCount > 1 && step === playerCount; }
  function unitById(id) { for (var i = 0; i < units.length; i++) if (units[i].id === id) return units[i]; return null; }
  function pilotNamed(name) {
    var lower = String(name || '').toLowerCase();
    for (var i = 0; i < pilots.length; i++) if (pilots[i].name.toLowerCase() === lower) return pilots[i];
    return null;
  }
  function same(a, b) { return String(a).toLowerCase() === String(b).toLowerCase(); }
  function takenElsewhere(id) {
    for (var j = 0; j < playerCount; j++) if (j !== step && plan(j).selected[id]) return true;
    return false;
  }
  function pilotElsewhere(name) {
    for (var j = 0; j < playerCount; j++) {
      if (j === step) continue;
      for (var id in plan(j).crew) if (plan(j).crew[id] && same(plan(j).crew[id], name)) return true;
    }
    return false;
  }
  function unitOf(name) {
    var crew = cur().crew;
    for (var id in crew) if (crew[id] && same(crew[id], name)) return id;
    return '';
  }
  function bvOf(i) {
    var bv = 0;
    units.forEach(function (u) { if (plan(i).selected[u.id]) bv += u.bv; });
    return bv;
  }
  function countOf(i) {
    var n = 0;
    units.forEach(function (u) { if (plan(i).selected[u.id]) n++; });
    return n;
  }
  function totalBV() { var t = 0; for (var i = 0; i < playerCount; i++) t += bvOf(i); return t; }
  function over() { return limit > 0 && totalBV() > limit; }

  // ---- Assigning pilots (to the current player's mechs)
  function assign(unitId, name) {
    var p = pilotNamed(name);
    if (!p || !cur().selected[unitId] || pilotElsewhere(p.name)) return;
    for (var id in cur().crew) if (cur().crew[id] && same(cur().crew[id], p.name)) delete cur().crew[id]; // one pilot, one mech
    cur().crew[unitId] = p.name;
    touched = true;
    held = '';
    error.textContent = '';
    render();
  }
  function unassign(name) {
    var id = unitOf(name);
    if (id) delete cur().crew[id];
    touched = true;
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
  // Gunnery, Piloting, Edge tokens and Edge abilities. Hovering the pilot shows the full card (pilotcard.js).
  var STATS = [['gunnery', 'GUN', 'Gunnery'], ['piloting', 'PIL', 'Piloting'], ['edgeTokens', 'EDGE', 'Edge tokens'], ['abilities', 'ABIL', 'Edge abilities']];
  function statSummary(p) { return STATS.map(function (s) { return s[2] + ' ' + (p[s[0]] || 0); }).join(', '); }
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
  function pilotTop(p, sub) {
    var top = el('span', 'deploy-pilot-top');
    top.appendChild(portrait(p));
    var text = el('span', 'deploy-pilot-text');
    text.appendChild(el('b', null, p.name));
    text.appendChild(el('small', null, sub));
    top.appendChild(text);
    return top;
  }

  // The tray: the named pilots no other player has taken. One already in this player's mech says which, and can be picked up to move or remove them.
  function renderTray() {
    var mine = pilots.filter(function (p) { return !pilotElsewhere(p.name); });
    tray.hidden = !pilots.length || isReview() || !!finished;
    trayList.textContent = '';
    mine.forEach(function (p) {
      var inUnit = unitById(unitOf(p.name));
      var chip = el('button', 'deploy-pilot' + (held === p.name ? ' held' : '') + (inUnit ? ' assigned' : ''));
      chip.type = 'button';
      chip.setAttribute('aria-pressed', held === p.name ? 'true' : 'false');
      chip.setAttribute('aria-label', p.name + ', ' + statSummary(p) + (inUnit ? ', in ' + inUnit.name : ''));
      chip.appendChild(pilotTop(p, inUnit ? 'in ' + inUnit.name : (p.rank || 'Unassigned')));
      chip.appendChild(statStrip(p));
      if (typeof PilotCard !== 'undefined') PilotCard.attach(chip, p);
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

  // A mech's pilot slot: the pilot in it (draggable, with an x), a dashed place to drop one, or, for a mech this player isn't taking, a disabled hint.
  function slot(u, unavailable) {
    var box = el('div', 'deploy-slot' + (unavailable ? ' unavailable' : ''));
    if (!pilots.length || unavailable) return box;
    if (!cur().selected[u.id]) {
      box.classList.add('disabled');
      box.appendChild(el('span', 'deploy-slot-empty', 'Take this mech first'));
      return box;
    }
    var p = pilotNamed(cur().crew[u.id]);
    if (p) {
      box.classList.add('filled');
      var top = pilotTop(p, p.rank || 'Pilot');
      var x = el('button', 'deploy-slot-x', '×');
      x.type = 'button';
      x.title = 'Take ' + p.name + ' out of this mech';
      x.setAttribute('aria-label', 'Take ' + p.name + ' out of ' + u.name);
      x.addEventListener('click', function (e) { e.stopPropagation(); unassign(p.name); });
      top.appendChild(x);
      box.appendChild(top);
      box.appendChild(statStrip(p));
      if (typeof PilotCard !== 'undefined') PilotCard.attach(box, p);
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

  function viewOf(u) {
    var view = {};
    Object.keys(u).forEach(function (k) { if (k !== 'gunnery' && k !== 'piloting' && k !== 'pilot') view[k] = u[k]; });
    return view;
  }

  function row(u) {
    var unavailable = u.destroyed || u.unknown;
    var li = el('li', 'deploy-unit' + (unavailable ? ' unavailable' : '') + (cur().selected[u.id] ? ' picked' : ''));
    var label = el('label');
    var box = document.createElement('input');
    box.type = 'checkbox';
    box.className = 'themed-box';
    box.checked = !!cur().selected[u.id];
    box.disabled = unavailable;
    box.addEventListener('change', function () {
      touched = true;
      if (box.checked) cur().selected[u.id] = true; else { delete cur().selected[u.id]; delete cur().crew[u.id]; } // a mech not being taken has no pilot
      error.textContent = '';
      render();
    });
    label.appendChild(box);
    label.appendChild(MechRow.build(pilots.length ? viewOf(u) : u)); // the pilot's skills are shown in the slot
    li.appendChild(label);
    if (pilots.length) {
      li.classList.add('has-slot');
      li.appendChild(slot(u, unavailable));
    }
    return li;
  }

  // ---- Showing a step
  // The meter: on a player step, what this player has taken and the force total against the limit (two figures, set apart); with one player, one figure.
  function setMeter() {
    var total = totalBV(), limitText = (limit ? ' / ' + fmt(limit) : '') + ' BV' + (limit ? '' : ' (no limit)');
    meter.textContent = '';
    if (playerCount > 1) {
      meter.appendChild(el('span', null, 'This player: ' + countOf(step) + ' selected · ' + fmt(bvOf(step)) + ' BV'));
      meter.appendChild(el('span', null, 'Force total: ' + fmt(total) + limitText));
    } else {
      meter.appendChild(el('span', null, countOf(0) + ' selected · ' + fmt(total) + limitText));
    }
    meter.classList.toggle('over', over());
  }

  function renderPlayers() {
    playersBox.hidden = !(step === 0 && !finished && minPlayers < maxPlayers);
    playersPick.textContent = '';
    for (var n = minPlayers; n <= maxPlayers; n++) {
      (function (n) {
        var b = el('button', 'deploy-count', String(n));
        b.type = 'button';
        b.setAttribute('aria-pressed', n === playerCount ? 'true' : 'false');
        b.setAttribute('aria-label', n + (n === 1 ? ' player' : ' players'));
        b.addEventListener('click', function () {
          if (n === playerCount) return;
          playerCount = n;
          plans.length = Math.min(plans.length, n); // players who are no longer playing give their choices back
          touched = true;
          held = '';
          render();
        });
        playersPick.appendChild(b);
      })(n);
    }
  }

  // A pilot in a review row: the same compact card as a filled slot, not draggable.
  function staticPilot(name) {
    var box = el('div', 'deploy-slot static');
    var p = pilotNamed(name);
    if (!p) { box.appendChild(el('span', 'deploy-slot-empty', 'No named pilot')); return box; }
    box.classList.add('filled');
    box.appendChild(pilotTop(p, p.rank || 'Pilot'));
    box.appendChild(statStrip(p));
    if (typeof PilotCard !== 'undefined') PilotCard.attach(box, p);
    return box;
  }

  // The plan as the review and the result show it. data = [{ player, units: [{ id, ... }], crew: { unitId: pilot name }, bv, hasAccount }].
  function renderSummary(data, notes) {
    review.textContent = '';
    notes.forEach(function (n) { review.appendChild(n); });
    data.forEach(function (pl) {
      var sec = el('section', 'deploy-review-player');
      var h = el('h3', null, 'Player ' + pl.player);
      h.appendChild(el('span', null, pl.units.length + (pl.units.length === 1 ? ' mech' : ' mechs') + ' · ' + fmt(pl.bv) + ' BV'));
      sec.appendChild(h);
      var ul = el('ul', 'deploy-review-list');
      pl.units.forEach(function (pu) {
        var u = unitById(pu.id);
        var li = el('li', 'deploy-unit review' + (pilots.length ? ' has-slot' : ''));
        li.appendChild(MechRow.build(viewOf(u || { name: pu.name, model: pu.model, bv: pu.bv, icon: '', role: '', tons: 0 })));
        if (pilots.length) li.appendChild(staticPilot(pl.crew[pu.id] || ''));
        ul.appendChild(li);
      });
      sec.appendChild(ul);
      review.appendChild(sec);
    });
    var tot = 0;
    data.forEach(function (pl) { tot += pl.bv; });
    var total = el('p', 'deploy-review-total' + (limit > 0 && tot > limit ? ' over' : ''));
    total.appendChild(el('span', null, 'Force total: ' + fmt(tot) + (limit ? ' / ' + fmt(limit) : '') + ' BV'));
    review.appendChild(total);
  }

  function planFromState() {
    var out = [];
    for (var i = 0; i < playerCount; i++) {
      var us = units.filter(function (u) { return plan(i).selected[u.id]; });
      out.push({ player: i + 1, units: us.map(function (u) { return { id: u.id, name: u.name, model: u.model, bv: u.bv }; }), crew: plan(i).crew, bv: bvOf(i) });
    }
    return out;
  }

  function renderFooter() {
    var multi = playerCount > 1;
    var last = !multi || isReview();
    back.hidden = !multi || step === 0 || !!finished;
    next.hidden = last || !!finished;
    go.hidden = !last || !!finished;
    cancel.textContent = finished ? 'Close' : 'Cancel';
    progress.hidden = !multi || !!finished;
    if (!multi) count.textContent = '';
    if (multi) {
      var total = stepCount();
      bar.style.width = Math.round(((step + 1) / total) * 100) + '%';
      progressTrack.setAttribute('aria-valuemax', total);
      progressTrack.setAttribute('aria-valuenow', step + 1);
      count.textContent = 'Step ' + (step + 1) + ' of ' + total;
    }
    var ok = !busy && !over();
    if (!last) next.disabled = !ok || countOf(step) === 0;
    else { var allHave = true; for (var i = 0; i < playerCount; i++) if (countOf(i) === 0) allHave = false; go.disabled = !ok || !allHave; }
  }

  function render() {
    if (typeof MechRow.hideTip === 'function') MechRow.hideTip(); // the pilot elements are about to be replaced, so a card open on one would point at nothing
    renderPlayers();
    list.textContent = '';
    review.hidden = true;
    stepTitle.hidden = true;
    if (finished) {
      meter.hidden = true;
      list.hidden = true;
      resetRow.hidden = true;
      review.hidden = false;
      var note = el('div', 'deploy-note good');
      note.appendChild(el('b', null, 'Your plan is valid. '));
      note.appendChild(document.createTextNode('Nothing has been sent to MekBay yet: building each player’s force in their own MekBay account, and the OpFor in the OpFor’s, comes next.'));
      var notes = [note];
      renderSummary((finished.players || []).map(function (pl) {
        var crew = {};
        (pl.crew || []).forEach(function (c) { crew[c.unit] = c.pilot; });
        return { player: pl.player, units: pl.units, crew: crew, bv: pl.bv };
      }), notes);
      renderTray();
      renderFooter();
      return;
    }
    meter.hidden = false;
    if (isReview()) {
      list.hidden = true;
      review.hidden = false;
      stepTitle.hidden = false;
      stepTitle.textContent = 'Review your plan';
      meter.hidden = true; // the review ends with the force total
      renderSummary(planFromState(), []);
      resetRow.hidden = !(cachedData && cachedData.hasOperation);
    } else {
      list.hidden = false;
      if (playerCount > 1) {
        stepTitle.hidden = false;
        stepTitle.textContent = '';
        stepTitle.appendChild(document.createTextNode('Player ' + (step + 1) + ' of ' + playerCount));
      }
      units.forEach(function (u) { if (!takenElsewhere(u.id)) list.appendChild(row(u)); });
      setMeter();
      resetRow.hidden = !(playerCount === 1 && cachedData && cachedData.hasOperation);
    }
    renderTray();
    renderFooter();
  }

  // Shows a hangar (cached or live). A choice the player has already made is kept for the units that are still there; before that, the units already in the
  // Deployed group are Player 1's, with the named pilots already in them.
  function apply(data) {
    units = data.units || [];
    if (data.limit) limit = data.limit;
    original = {};
    var seen = {};
    units.forEach(function (u) {
      var p = u.pilot && pilotNamed(u.pilot);
      if (p && !seen[p.name.toLowerCase()]) { original[u.id] = p.name; seen[p.name.toLowerCase()] = true; }
    });
    var present = {};
    units.forEach(function (u) { if (!u.destroyed && !u.unknown) present[u.id] = true; });
    if (touched) {
      plans.forEach(function (pl) {
        Object.keys(pl.selected).forEach(function (id) { if (!present[id]) { delete pl.selected[id]; delete pl.crew[id]; } });
        Object.keys(pl.crew).forEach(function (id) { if (!pl.selected[id]) delete pl.crew[id]; });
      });
    } else {
      plans = [];
      units.forEach(function (u) {
        if (!present[u.id] || !u.inDeployed) return;
        plan(0).selected[u.id] = true;
        if (original[u.id]) plan(0).crew[u.id] = original[u.id];
      });
    }
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
    finished = null;
    busy = false;
    held = '';
    step = 0;
    playerCount = minPlayers;
    plans = [];
    if (cachedData && cachedData.units) {
      apply(cachedData); // instant
    } else {
      status.hidden = false;
      status.textContent = 'Loading your hangar…';
      units = [];
      original = {};
      render();
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
  cancel.addEventListener('click', function () { dialog.close(); });
  next.addEventListener('click', function () { if (!next.disabled && step < stepCount() - 1) { step++; held = ''; error.textContent = ''; render(); } });
  back.addEventListener('click', function () { if (step > 0) { step--; held = ''; error.textContent = ''; render(); } });

  // Deploy: the whole plan goes to the server to be checked. Every mech that has a named pilot is sent with them (so skills are current), and any mech that had
  // one and no longer does is sent with an empty pilot.
  go.addEventListener('click', function () {
    if (go.disabled) return;
    busy = true;
    renderFooter();
    error.textContent = '';
    var body = query + '&players=' + playerCount;
    for (var i = 0; i < playerCount; i++) {
      var pre = '&p' + (i + 1) + '_';
      units.forEach(function (u) {
        if (!plan(i).selected[u.id]) return;
        body += pre + 'units=' + encodeURIComponent(u.id);
      });
      units.forEach(function (u) {
        if (!plan(i).selected[u.id]) return;
        var want = plan(i).crew[u.id] || '', was = original[u.id] || '';
        if (want || want !== was) body += pre + 'assign_unit=' + encodeURIComponent(u.id) + pre + 'assign_pilot=' + encodeURIComponent(want);
      });
    }
    if (reset.checked) body += '&reset_opfor=1';
    fetch('/api/deploy/plan', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: body,
      credentials: 'same-origin'
    }).then(function (res) {
      busy = false;
      if (res.ok) return res.json().then(function (data) { finished = data; render(); });
      renderFooter();
      return res.text().then(function (t) { error.textContent = t.trim() || 'Could not check the plan.'; });
    }, function () { busy = false; renderFooter(); error.textContent = 'Could not reach the server.'; });
  });
})();
