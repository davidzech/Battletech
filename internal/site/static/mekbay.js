// Scenario page: the Deploy wizard. The force's mechs (its MekBay hangar) and named pilots are one shared pool, and the players divide it: each takes the mechs
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
// The last step, Start battle, sends the whole plan to POST /api/battle/start, which checks it on the server (the same rules, over every player's mechs together), builds a
// force for each player and one for the OpFor in MekBay, saves the operation that sets them against each other, and answers with the one MekBay link the whole table opens,
// which the Operation in progress pane shows (enterOperation), with the battle's live status read every 10 seconds (refreshStatus) and the mission's actions:
// Complete mission (outcome.js) and Abort mission (roll back, or just close).
(function () {
  var dialog = document.getElementById('deploy-dialog');
  var openBtn = document.getElementById('deploy-open');
  if (!dialog || !openBtn || typeof dialog.showModal !== 'function') return;

  // The mission log's IN PROGRESS state opens the operation, like the View operation button.
  var logOpen = document.getElementById('mission-log-open');
  if (logOpen) logOpen.addEventListener('click', function () { openBtn.click(); });

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
  var unitLimit = parseInt(dialog.dataset.unitLimit, 10) || 0;      // the most mechs in all (a scenario rule), 0 for none
  var needPilots = dialog.dataset.pilotsRequired === '1';           // every mech taken needs a named pilot (a scenario rule)
  var query = 'campaign=' + encodeURIComponent(dialog.dataset.campaign) + '&scenario=' + encodeURIComponent(dialog.dataset.scenario);
  var MAX = 4; // the most players a campaign can be played with (maxPlayers on the server)
  var maxPlayers = Math.min(MAX, Math.max(1, parseInt(dialog.dataset.maxPlayers, 10) || 1));
  var minPlayers = Math.min(maxPlayers, Math.max(1, parseInt(dialog.dataset.minPlayers, 10) || 1));

  var units = [];         // the hangar's mechs, as the server describes them
  var provided = false;   // the scenario provides its own mechs in place of the hangar
  var original = {};      // unit ID -> the named pilot the hangar has in it now, if any
  var plans = [];         // per player: { selected: { unitId: true }, crew: { unitId: pilot name } }
  // The count chosen when the campaign was started is where every deploy begins (the picker can change it for one deploy); a run from before that was
  // asked, or a count outside the range, starts at the minimum.
  var defaultPlayers = Math.min(maxPlayers, Math.max(minPlayers, parseInt(dialog.dataset.defaultPlayers, 10) || minPlayers));
  // page is told which are saved, never the IDs). A saved default whose IDs have since been forgotten steps down to what the IDs cover.
  var playerCount = defaultPlayers;
  var step = 0;
  var finished = null;    // the server's answer once the battle has been started: the link the table opens
  var title = document.getElementById('deploy-title');
  var cachedStatus = null; // the last status read of the battle, embedded by the page: it paints at once while a fresh read is made
  try { cachedStatus = dialog.dataset.cachedStatus ? JSON.parse(dialog.dataset.cachedStatus) : null; } catch (e) { cachedStatus = null; }
  var operationUrl = dialog.dataset.operationUrl || ''; // the table's link, known from the page, so it shows before anything is read from MekBay
  var inProgress = dialog.dataset.inProgress === '1'; // the scenario has a battle going: the dialog opens on its Operation in progress view
  var opActions = document.getElementById('op-actions');
  var abortOpen = document.getElementById('abort-open');
  var statusData = null;  // the battle's live status as the server last read it from MekBay: { url, operation, forces: [{ name, alignment, units }], updatedAt }
  var statusBusy = false;
  var statusTimer = null;
  var REFRESH_ICON = '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M21 12a9 9 0 1 1-2.64-6.36"/><path d="M21 3v6h-6"/></svg>';
  var QR_ICON = '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/><path d="M14 14h3v3h-3zM20 14v.01M14 20h.01M17 20h4v-3"/></svg>';
  var POLL_MS = 10000;    // how often an open Operation in progress view asks again
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

  var fmt = Dom.commas;
  var el = Dom.el;

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
  function totalCount() { var t = 0; for (var i = 0; i < playerCount; i++) t += countOf(i); return t; }
  function over() { return (limit > 0 && totalBV() > limit) || (unitLimit > 0 && totalCount() > unitLimit); }
  // The most mechs player i may take under the scenario's unit limit: every other player needs at least one, so a player who has taken none yet still
  // holds a place (with a limit of 2 and two players, each takes one). Infinity without a limit.
  function roomFor(i) {
    if (!(unitLimit > 0)) return Infinity;
    var others = 0;
    for (var j = 0; j < playerCount; j++) if (j !== i) others += Math.max(countOf(j), 1);
    return unitLimit - others;
  }
  // Mechs a player has taken that have no named pilot, when the scenario requires one on each.
  function unmanned(i) {
    if (!needPilots) return 0;
    var n = 0;
    units.forEach(function (u) { if (plan(i).selected[u.id] && !plan(i).crew[u.id]) n++; });
    return n;
  }
  function anyUnmanned() { for (var i = 0; i < playerCount; i++) if (unmanned(i)) return true; return false; }

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

  // The compact pilot card's parts (pilotcard.js): the portrait, name and a line, and the strip of four levels.
  var statSummary = PilotCard.summary, statStrip = PilotCard.strip, pilotTop = PilotCard.top;

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
      box.appendChild(el('span', 'deploy-slot-empty', 'Select this unit first'));
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
    // A scenario's unit limit is reached, counting a mech kept for every other player: the mechs not yet taken can't be.
    box.disabled = unavailable || (!cur().selected[u.id] && countOf(step) >= roomFor(step));
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
    var total = totalBV();
    // The BV part says "(no limit)" only when nothing else limits the force; a scenario with a unit limit counts mechs against it instead.
    var limitText = (limit ? ' / ' + fmt(limit) : '') + ' BV' + (limit || unitLimit ? '' : ' (no limit)');
    var mechs = function (n) { return n + (unitLimit ? ' / ' + unitLimit : '') + (unitLimit || n !== 1 ? ' mechs' : ' mech'); };
    meter.textContent = '';
    if (playerCount > 1) {
      meter.appendChild(el('span', null, 'This player: ' + countOf(step) + ' selected · ' + fmt(bvOf(step)) + ' BV'));
      meter.appendChild(el('span', null, 'Force total: ' + (unitLimit ? mechs(totalCount()) + ' · ' : '') + fmt(total) + limitText));
    } else {
      meter.appendChild(el('span', null, (unitLimit ? mechs(countOf(0)) : countOf(0) + ' selected') + ' · ' + fmt(total) + limitText));
    }
    meter.classList.toggle('over', over());
    // The scenario's own rules, in words, and what is still missing.
    var rules = document.getElementById('deploy-rules');
    var parts = [];
    if (unitLimit) parts.push('Take up to ' + unitLimit + ' mechs in all' + (playerCount > 1 ? ', at least one for each player' : ''));
    if (needPilots) parts.push('every mech needs a named pilot');
    rules.hidden = !parts.length;
    if (parts.length) {
      var text = parts.join(', ') + '.';
      if (anyUnmanned()) text += ' Assign a named pilot to each mech you take.';
      if (countOf(step) > roomFor(step)) text += ' Leave a mech for each other player.';
      rules.textContent = text.charAt(0).toUpperCase() + text.slice(1);
    }
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
    var count = 0;
    data.forEach(function (pl) { count += pl.units.length; });
    var total = el('p', 'deploy-review-total' + ((limit > 0 && tot > limit) || (unitLimit > 0 && count > unitLimit) ? ' over' : ''));
    total.appendChild(el('span', null, 'Force total: ' + (unitLimit ? count + ' / ' + unitLimit + ' mechs · ' : '') + fmt(tot) + (limit ? ' / ' + fmt(limit) : '') + ' BV'));
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

  // Puts text on the clipboard, falling back to selecting it for Ctrl+C where the clipboard is not available.
  function copyLink(text, button, urlEl) {
    function done() {
      button.textContent = 'Copied';
      setTimeout(function () { button.textContent = 'Copy'; }, 1600);
    }
    function select() {
      var range = document.createRange();
      range.selectNodeContents(urlEl);
      var sel = window.getSelection();
      sel.removeAllRanges();
      sel.addRange(range);
      var ok = false;
      try { ok = document.execCommand('copy'); } catch (e) { ok = false; }
      if (ok) done(); else button.textContent = 'Press Ctrl+C';
    }
    if (navigator.clipboard && navigator.clipboard.writeText) navigator.clipboard.writeText(text).then(done, select);
    else select();
  }

  // The QR code of a link as an SVG: dark modules on a white square with the four-module quiet zone scanners need, whatever the page's theme. Only numbers go
  // into the markup.
  function qrSvg(text) {
    var qr = qrcode(0, 'M'); // the smallest version that fits, with the medium error correction
    qr.addData(text);
    qr.make();
    var n = qr.getModuleCount(), border = 4, size = n + border * 2, d = '';
    for (var r = 0; r < n; r++) {
      for (var c = 0; c < n; c++) if (qr.isDark(r, c)) d += 'M' + (c + border) + ',' + (r + border) + 'h1v1h-1z';
    }
    return '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ' + size + ' ' + size + '" shape-rendering="crispEdges" role="img" aria-label="QR code of the table link">' +
      '<rect width="' + size + '" height="' + size + '" fill="#fff"/><path d="' + d + '" fill="#000"/></svg>';
  }

  // The QR code in a dialog of its own, on top of this one, for a phone to scan. It closes with its button, with Escape, or by a click outside the code.
  var qrDialog = null;
  function showQr(url) {
    if (typeof qrcode === 'undefined') { error.textContent = 'The QR code could not be made: its script did not load.'; return; }
    if (!qrDialog) {
      qrDialog = el('dialog', 'qr-dialog');
      qrDialog.setAttribute('aria-labelledby', 'qr-title');
      var inner = el('div', 'qr-inner');
      var h = el('h2', null, 'Scan to open');
      h.id = 'qr-title';
      inner.appendChild(h);
      inner.appendChild(el('div', 'qr-box'));
      var actions = el('div', 'form-actions');
      var close = el('button', null, 'Close');
      close.type = 'button';
      close.addEventListener('click', function () { qrDialog.close(); });
      actions.appendChild(close);
      inner.appendChild(actions);
      qrDialog.appendChild(inner);
      qrDialog.addEventListener('click', function (e) { if (e.target === qrDialog) qrDialog.close(); }); // the backdrop
      document.body.appendChild(qrDialog);
    }
    qrDialog.querySelector('.qr-box').innerHTML = qrSvg(url);
    qrDialog.showModal();
  }

  // The link the whole table opens. It opens in a new tab, and its text can be selected for copying (the anchor is not draggable, so a drag selects the
  // text instead of carrying the link away, and a click that leaves text selected does not open it).
  function linkPanel(url) {
    var box = el('div', 'battle-links');
    var ul = el('ul', 'battle-link-list');
    var li = el('li', 'battle-link');
    var a = el('a', 'battle-link-url', url);
    if (/^https?:\/\//.test(url)) a.href = url;
    a.target = '_blank';
    a.rel = 'noopener noreferrer';
    a.draggable = false;
    a.addEventListener('click', function (e) {
      var sel = window.getSelection();
      if (sel && !sel.isCollapsed && sel.toString().length > 0 && a.contains(sel.anchorNode)) e.preventDefault();
    });
    li.appendChild(a);
    var q = el('button', 'op-icon op-qr');
    q.type = 'button';
    q.title = 'Show a QR code';
    q.setAttribute('aria-label', 'Show a QR code');
    q.innerHTML = QR_ICON;
    q.addEventListener('click', function () { showQr(url); });
    li.appendChild(q);
    var b = el('button', 'mr-action battle-copy', 'Copy');
    b.type = 'button';
    b.addEventListener('click', function () { copyLink(url, b, a); });
    li.appendChild(b);
    ul.appendChild(li);
    box.appendChild(ul);
    box.appendChild(el('p', 'muted battle-links-note', 'Everyone at the table opens MekBay from this one link. It shows every player\u2019s force against the OpFor, and each of you can edit any of them; a change made on one device appears on the others as it is saved. Anyone with the link can edit everything in it, so send it only to your table. It stays valid for two weeks and is extended whenever it is used.'));
    return box;
  }

  // The Operation in progress view: the link, and how the battle stands, with each mech's live condition as MekBay has it (armor and structure lost, critical hits,
  // ammunition used), refreshed every few seconds and on demand.
  function renderOperation() {
    review.textContent = '';
    var box = el('div', 'op-view');
    if (statusData && statusData.url) box.appendChild(linkPanel(statusData.url));
    var bar = el('div', 'op-status-bar');
    var stamp = statusBusy ? 'Updating' : statusData && statusData.updatedAt ? 'Updated ' + new Date(statusData.updatedAt * 1000).toLocaleTimeString() : 'Reading the battle from MekBay';
    bar.appendChild(el('span', 'op-updated', stamp));
    var refresh = el('button', 'op-icon op-refresh' + (statusBusy ? ' spinning' : ''));
    refresh.type = 'button';
    refresh.title = 'Refresh the status';
    refresh.setAttribute('aria-label', 'Refresh the status');
    refresh.innerHTML = REFRESH_ICON; // the circle arrow, the same icon as the hangar link check
    refresh.disabled = statusBusy;
    refresh.addEventListener('click', function () { refreshStatus(); });
    bar.appendChild(refresh);
    box.appendChild(bar);
    // Until the first read has come back there are no forces to show yet: say so rather than leave the pane bare.
    if (!(statusData && statusData.updatedAt)) box.appendChild(el('p', 'muted op-reading', 'Reading the battle from MekBay'));
    ((statusData && statusData.forces) || []).forEach(function (f) {
      var sec = el('section', 'deploy-review-player op-force');
      var h = el('h3', null, f.name || 'Force');
      h.appendChild(el('span', null, f.units.length + (f.units.length === 1 ? ' mech' : ' mechs')));
      sec.appendChild(h);
      var ul = el('ul', 'deploy-review-list');
      f.units.forEach(function (u) {
        // A mech crewed by a named pilot shows them to the right, as the pilot slots do (the card of the pilot, with their levels, on hover); any other crew keeps
        // the plain Gunnery/Piloting chip on the row.
        var named = u.pilot && pilotNamed(u.pilot);
        // An uncrewed (or unnamed) mech is the same height and scale as one with a pilot, with its row spanning the width where the pilot's card would be.
        var li = el('li', 'deploy-unit review has-slot' + (named ? '' : ' wide'));
        li.appendChild(MechRow.build(named ? viewOf(u) : u));
        if (named) {
          var slot = staticPilot(u.pilot);
          // A pilot the record sheet shows dead is stamped K.I.A. over their card, as the Memorial does.
          if (u.pilotDead) { slot.classList.add('kia'); slot.appendChild(el('span', 'kia-stamp', 'K.I.A.')); }
          li.appendChild(slot);
        }
        ul.appendChild(li);
      });
      sec.appendChild(ul);
      box.appendChild(sec);
    });
    review.appendChild(box);
  }

  function refreshStatus() {
    if (statusBusy || !finished) return;
    statusBusy = true;
    renderOperation();
    fetch('/api/battle/status?' + query, { credentials: 'same-origin' }).then(function (res) {
      if (res.status === 404) { location.reload(); return null; } // the battle ended elsewhere (completed or aborted)
      if (!res.ok) return res.text().then(function (t) { throw new Error(t.trim() || 'Could not read the battle.'); });
      return res.json();
    }).then(function (data) {
      statusBusy = false;
      if (!data) return;
      statusData = data;
      error.textContent = '';
      if (finished) renderOperation();
    }, function (err) {
      statusBusy = false;
      error.textContent = (err && err.message ? err.message + ' ' : '') + (statusData ? 'Showing the last known status.' : '');
      if (finished) renderOperation();
    });
  }

  function stopPolling() {
    if (statusTimer) clearInterval(statusTimer);
    statusTimer = null;
  }

  function startPolling() {
    stopPolling();
    statusTimer = setInterval(function () { if (!document.hidden && dialog.open) refreshStatus(); }, POLL_MS);
  }

  // Opens the Operation in progress view, for a battle just started or one that was already going.
  function enterOperation(initial) {
    finished = {};
    inProgress = true;
    if (initial) statusData = { url: initial.url, operation: initial.operation, forces: [], updatedAt: 0 };
    else if (!statusData) statusData = { url: operationUrl, forces: cachedStatus && cachedStatus.forces || [], updatedAt: cachedStatus && cachedStatus.updatedAt || 0 };
    render();
    refreshStatus();
    startPolling();
  }

  function renderFooter() {
    var multi = playerCount > 1;
    var last = !multi || isReview();
    back.hidden = !multi || step === 0 || !!finished;
    next.hidden = last || !!finished;
    go.hidden = !last || !!finished;
    cancel.textContent = finished ? 'Close' : 'Cancel';
    go.textContent = busy ? 'Starting' : 'Start battle';
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
    if (!last) next.disabled = !ok || countOf(step) === 0 || unmanned(step) > 0 || countOf(step) > roomFor(step);
    else { var allHave = true; for (var i = 0; i < playerCount; i++) if (countOf(i) === 0) allHave = false; go.disabled = !ok || !allHave || anyUnmanned(); }
  }

  function render() {
    if (typeof MechRow.hideTip === 'function') MechRow.hideTip(); // the pilot elements are about to be replaced, so a card open on one would point at nothing
    renderPlayers();
    list.textContent = '';
    review.hidden = true;
    stepTitle.hidden = true;
    opActions.hidden = !finished;
    if (finished) {
      meter.hidden = true;
      list.hidden = true;
      resetRow.hidden = true;
      status.hidden = true;
      review.hidden = false;
      title.textContent = 'Operation in progress';
      renderOperation();
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
    provided = !!data.provided;
    document.getElementById('deploy-provided').hidden = !provided;
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
    stopPolling();
    title.textContent = 'Deploy your force';
    busy = false;
    held = '';
    step = 0;
    playerCount = defaultPlayers;
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
    if (inProgress) enterOperation(null);
    else load();
  });
  dialog.addEventListener('close', stopPolling);
  // One Escape can be taken for both dialogs when the QR code was opened without a click in between; the one under it stays.
  dialog.addEventListener('cancel', function (e) { if ((qrDialog && qrDialog.open) || (abortDialog && abortDialog.open)) e.preventDefault(); });
  cancel.addEventListener('click', function () { dialog.close(); });
  next.addEventListener('click', function () { if (!next.disabled && step < stepCount() - 1) { step++; held = ''; error.textContent = ''; render(); } });
  back.addEventListener('click', function () { if (step > 0) { step--; held = ''; error.textContent = ''; render(); } });

  // Abort mission: give the battle up after a confirmation. The scenario goes back to not started, with no rewards.
  // The two ways to give a battle up, in a dialog of their own: roll the campaign back to the end of the previous mission, or just close the battle and keep every
  // change made since (repairs, sales, purchases, pilots). Resolves to 'rollback', 'keep' or null (keep playing).
  var abortDialog = null;
  function chooseAbort() {
    return new Promise(function (resolve) {
      if (!abortDialog) {
        abortDialog = el('dialog', 'abort-dialog');
        abortDialog.setAttribute('aria-labelledby', 'abort-title');
        var inner = el('div', 'abort-inner');
        var h = el('h2', null, 'Abort mission');
        h.id = 'abort-title';
        inner.appendChild(h);
        inner.appendChild(el('p', 'muted', 'The battle is given up and the mission goes back to not started, with no rewards. Choose what happens to the campaign.'));
        [['rollback', 'Roll back', 'Return the campaign to the end of the previous mission. Repairs, sales, purchases and SP spent since then are undone.', 'button button-outline danger'],
         ['keep', 'Just close', 'Close the battle and clean up its operation in MekBay. Every change since the previous mission stays.', 'button button-outline']].forEach(function (o) {
          var row = el('div', 'abort-choice');
          var btn = el('button', o[3], o[1]);
          btn.type = 'button';
          btn.dataset.choice = o[0];
          row.appendChild(btn);
          row.appendChild(el('p', 'muted', o[2]));
          inner.appendChild(row);
        });
        var actions = el('div', 'form-actions');
        var keep = el('button', null, 'Keep playing');
        keep.type = 'button';
        keep.dataset.choice = '';
        actions.appendChild(keep);
        inner.appendChild(actions);
        abortDialog.appendChild(inner);
        document.body.appendChild(abortDialog);
      }
      var picked = null;
      abortDialog.querySelectorAll('button[data-choice]').forEach(function (b) {
        b.onclick = function () { picked = b.dataset.choice || null; abortDialog.close(); };
      });
      abortDialog.onclose = function () { resolve(picked); };
      abortDialog.onclick = function (e) { if (e.target === abortDialog) abortDialog.close(); };
      abortDialog.showModal();
    });
  }

  if (abortOpen) abortOpen.addEventListener('click', function () {
    chooseAbort().then(function (mode) {
      if (!mode) return;
      fetch('/api/battle/abort', {
        method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body: query + '&mode=' + mode, credentials: 'same-origin'
      }).then(function (res) {
        if (res.ok || res.status === 404) { location.reload(); return; }
        return res.text().then(function (t) { error.textContent = t.trim() || 'Could not abort the battle.'; });
      }, function () { error.textContent = 'Could not reach the server.'; });
    });
  });

  // Start battle: the whole plan goes to the server, which checks it and then builds the battle in MekBay. Every mech that has a named pilot is sent with them (so skills are current), and any mech that had
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
    fetch('/api/battle/start', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: body,
      credentials: 'same-origin'
    }).then(function (res) {
      busy = false;
      if (res.ok) return res.json().then(function (data) { enterOperation(data); });
      renderFooter();
      return res.text().then(function (t) { error.textContent = t.trim() || 'Could not start the battle.'; });
    }, function () { busy = false; renderFooter(); error.textContent = 'Could not reach the server.'; });
  });
})();
