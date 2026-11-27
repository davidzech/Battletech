// The mission completion dialog: report each named pilot's inputs (who took part, kills, whether they were killed, ... whatever the scenario's outcome engine asks for,
// see outcomes.go), see the result as you enter it, and commit the mission as completed. The preview comes from the server (POST /api/campaign/scenario-outcome) so
// the rules live in one place; committing (POST /api/campaign/scenario-outcome/commit) applies them and cannot be undone, so it asks first (confirm.js).
//
// What the players have entered is kept in the page's fragment (#outcome=...; the Deploy wizard on the same page uses #deploy=...), so a reload brings the dialog
// back as it was: each pilot's Active and Killed in action boxes and inputs, the mission's inputs and the MVP. The fragment is replaced, not added to the history,
// and goes when the dialog closes or the mission is committed.
(function () {
  var dialog = document.getElementById('outcome-dialog');
  var open = document.getElementById('outcome-open');
  if (!dialog || !open || typeof dialog.showModal !== 'function') {
    // No dialog (the mission is completed, or locked): a saved one has nothing to come back to.
    if (location.hash.indexOf('#outcome=') === 0 && window.history && history.replaceState) history.replaceState(history.state, '', location.pathname + location.search);
    return;
  }
  var inputs = JSON.parse(dialog.dataset.inputs || '[]');
  var pilots = JSON.parse(dialog.dataset.pilots || '[]');
  var body = document.getElementById('outcome-body');
  var total = document.getElementById('outcome-total');
  var error = document.getElementById('outcome-error');
  var commit = document.getElementById('outcome-commit');
  var mvpSP = parseInt(dialog.dataset.mvpSp, 10) || 0; // the extra SP the mission's MVP earns; 0 means the mission has no MVP
  var hasMVP = false, mvpNote = null; // whether the mission has an MVP (an MVP box on each pilot's card), and the note above the pilots
  var candidates = null; // who can be the MVP, from the last preview (took part and stays in the campaign); null before the first
  // Whether a battle is in progress, so who took part and who is dead are read from it. Checked when the dialog opens, not when the page loads: Start battle
  // (mekbay.js) begins one without reloading the page and marks the dialog then.
  var inBattle = false;
  var ui = {};        // pilot id -> { took, tb, fields, killedBox, boxes }, the parts of a card the battle (or a restore) fills in; boxes: input id -> its box
  var missionBoxes = {}; // mission input id -> its box
  var state = null;   // { pilots: { id: { tookPart, killed, values } }, mission: { id: value }, mvp: the MVP's pilot id, '' while not chosen }
  var results = {};   // pilot id -> the preview's lines, and the mission's
  var seq = 0, timer = null, valid = false;
  var battleSeq = 0; // the battle reads, counted apart from the previews

  var el = Dom.el;
  function byScope(scope) { return inputs.filter(function (i) { return i.scope === scope; }); }
  function blank(list) {
    var v = {};
    list.forEach(function (i) { v[i.id] = i.type === 'bool' ? false : NaN; }); // a number starts empty: nothing is assumed until it is entered
    return v;
  }

  // One input: a number box or a themed checkbox.
  function field(input, values, onChange, key) {
    key = key || input.id;
    var wrap = el('label', 'outcome-field' + (input.type === 'bool' ? ' check themed-check' : ''));
    var box = document.createElement('input');
    if (input.type === 'bool') {
      box.type = 'checkbox'; box.className = 'themed-box';
      if (input.required) { values[key] = true; box.disabled = true; wrap.title = 'Required to complete the mission'; wrap.classList.add('required'); }
      box.checked = !!values[key];
      box.addEventListener('change', function () { values[key] = box.checked; onChange(); });
      wrap.appendChild(box);
      wrap.appendChild(el('span', null, input.label));
      if (input.help) wrap.appendChild(el('span', 'muted outcome-help', input.help));
    } else {
      wrap.appendChild(el('span', 'outcome-label', input.label));
      box.type = 'number'; box.min = input.min; box.max = input.max; box.step = 1; box.inputMode = 'numeric'; box.value = typeof values[input.id] === 'number' && !isNaN(values[input.id]) ? values[input.id] : '';
      box.addEventListener('input', function () { var n = parseInt(box.value, 10); values[input.id] = isNaN(n) ? NaN : n; onChange(); });
      wrap.appendChild(box);
    }
    if (input.help) wrap.title = input.help;
    return wrap;
  }

  function build() {
    state = { pilots: {}, mission: blank(byScope('mission')), mvp: '' };
    hasMVP = mvpSP > 0 && pilots.length > 0; mvpNote = null; candidates = null;
    results = {};
    ui = {};
    missionBoxes = {};
    body.textContent = '';
    var pilotInputs = byScope('pilot');
    if (!pilots.length) body.appendChild(el('p', 'muted', 'You have no living named pilots to report on.'));
    if (hasMVP) { mvpNote = el('p', 'outcome-mvp-note muted', ''); body.appendChild(mvpNote); }
    pilots.forEach(function (p) {
      var st = state.pilots[p.id] = { tookPart: true, killed: false, values: blank(pilotInputs) };
      var card = el('div', 'outcome-pilot');
      var head = el('div', 'outcome-pilot-head');
      var img = document.createElement('img');
      img.src = p.portrait; img.alt = ''; img.width = 56; img.height = 56;
      head.appendChild(img);
      var who = el('div', 'outcome-pilot-name');
      who.appendChild(el('b', null, p.name));
      if (p.rank) who.appendChild(el('span', 'muted', p.rank));
      head.appendChild(who);
      card.appendChild(head);
      // The card's checkboxes in one row under the name: Active (unless a battle says who took part), Killed in action, MVP. The mission's inputs for the pilot
      // (such as Kills) are a row of their own below.
      var checks = el('div', 'outcome-checks');
      var took, tb = null;
      if (inBattle) {
        // The battle says who took part (they crew a mech in it), so there is nothing to tick: it is a line under the name once the battle has been read.
        took = el('span', 'outcome-took', '');
        who.appendChild(took);
      } else {
        took = el('label', 'outcome-field check themed-check');
        took.title = 'Untick if the pilot sat this one out (benched): they get half the pilot SP, if the mission gives any.';
        tb = document.createElement('input');
        tb.type = 'checkbox'; tb.className = 'themed-box'; tb.checked = true;
        took.appendChild(tb); took.appendChild(el('span', null, 'Active'));
        checks.appendChild(took);
      }
      // Every pilot can be killed in action, whatever the scenario asks: a pilot killed gets nothing and is removed from the campaign.
      var kill = field({ id: 'killed', label: 'Killed in action', type: 'bool' }, st, changed, 'killed');
      checks.appendChild(kill);
      // Every mission has an MVP: tick the box on one pilot's card (ticking another moves it). Only a pilot who took part and stays in the campaign can be it.
      var mb = null;
      if (hasMVP) {
        var mvpLabel = el('label', 'outcome-field check themed-check outcome-mvp-pick');
        mvpLabel.title = 'The mission\u2019s most valuable pilot earns +' + mvpSP + ' SP, free: it does not come out of the mission\u2019s SP.';
        mb = document.createElement('input');
        mb.type = 'checkbox'; mb.className = 'themed-box outcome-mvp-box';
        mvpLabel.appendChild(mb); mvpLabel.appendChild(el('span', null, 'MVP'));
        checks.appendChild(mvpLabel);
        mb.addEventListener('change', function () {
          if (mb.checked) state.mvp = p.id;
          else if (state.mvp === p.id) state.mvp = '';
          mvpOptions();
          changed();
        });
      }
      card.appendChild(checks);
      var fields = el('div', 'outcome-fields');
      var boxes = {};
      pilotInputs.forEach(function (i) { var f = field(i, st.values, changed); boxes[i.id] = f.querySelector('input'); fields.appendChild(f); });
      if (pilotInputs.length) card.appendChild(fields);
      ui[p.id] = { took: took, tb: tb, mvpBox: mb, fields: fields, kill: kill, killedBox: kill.querySelector('input'), boxes: boxes, killTouched: false };
      ui[p.id].killedBox.addEventListener('change', function () { ui[p.id].killTouched = true; });
      var preview = el('ul', 'outcome-preview');
      preview.id = 'outcome-preview-' + p.id;
      card.appendChild(preview);
      if (tb) tb.addEventListener('change', function () { st.tookPart = tb.checked; if (!tb.checked) { st.killed = false; ui[p.id].killedBox.checked = false; } part(ui[p.id], tb.checked); changed(); });
      body.appendChild(card);
    });
    var mission = byScope('mission');
    if (mission.length) {
      var box = el('div', 'outcome-mission');
      box.appendChild(el('h3', 'alloc-head', 'The mission'));
      mission.forEach(function (i) { var f = field(i, state.mission, changed); missionBoxes[i.id] = f.querySelector('input'); box.appendChild(f); });
      body.appendChild(box);
    }
    mvpOptions();
    render(null);
  }

  // Shows a card's Killed in action box and its inputs only while the pilot took part (a benched pilot has nothing to report).
  function part(u, on) { u.fields.hidden = !on; u.kill.hidden = !on; }

  // The pilots who can be the MVP now: active and not killed, and (once the server has worked the result out) not removed by the mission's rules either, such as a
  // pilot who fails the Trial of Position.
  function eligible() {
    return pilots.filter(function (p) {
      var st = state.pilots[p.id];
      return st.tookPart && !st.killed && (!candidates || candidates.indexOf(p.id) >= 0);
    });
  }

  // Sets each pilot's MVP box: ticked for the MVP, and usable only by a pilot who can be it; a choice that no longer fits is cleared.
  function mvpOptions() {
    if (!hasMVP) return;
    var list = eligible();
    if (state.mvp && !list.some(function (p) { return p.id === state.mvp; })) state.mvp = '';
    pilots.forEach(function (p) {
      var mb = ui[p.id] && ui[p.id].mvpBox;
      if (!mb) return;
      var ok = list.some(function (q) { return q.id === p.id; });
      mb.disabled = !ok;
      mb.checked = state.mvp === p.id;
      mb.parentNode.title = ok ? 'The mission\u2019s most valuable pilot earns +' + mvpSP + ' SP, free: it does not come out of the mission\u2019s SP.'
        : 'Only a pilot who took part and stays in the campaign can be the MVP.';
    });
    mvpNote.textContent = !list.length ? 'No named pilot took part and stays in the campaign, so this mission has no MVP.'
      : 'Tick MVP on the mission\u2019s most valuable pilot: they earn +' + mvpSP + ' SP, free (it does not come out of the mission\u2019s SP).';
  }

  // Whether the mission still needs its MVP chosen: someone can be it and nobody is.
  function mvpMissing() { return hasMVP && !state.mvp && eligible().length > 0; }

  // What is sent: booleans as true/false, numbers as numbers (a number box left empty makes the request invalid until it is filled).
  // The pilots who crew a mech in the battle's players' forces, by lowercased name, and whether their record sheet shows them dead.
  function crewOf(status) {
    var crew = {};
    ((status && status.forces) || []).forEach(function (f) {
      if (f.allies) return; // the scenario's automata: their crews are never the players' named pilots
      (f.units || []).forEach(function (u) {
        if (!u.pilot) return;
        var k = u.pilot.toLowerCase();
        crew[k] = { dead: !!(crew[k] && crew[k].dead) || !!u.pilotDead };
      });
    });
    return crew;
  }

  // Fills the cards in from the battle: a pilot who crews a mech took part, the rest are benched, and a pilot the record sheet shows dead starts ticked as killed in
  // action (the players can untick it).
  // With keepTouched, a Killed in action box the player has changed keeps their choice (a fresh read arriving after the dialog opened from the cached one).
  function applyBattle(crew, keepTouched) {
    pilots.forEach(function (p) {
      var c = crew[p.name.toLowerCase()], u = ui[p.id], st = state.pilots[p.id];
      st.tookPart = !!c;
      if (!(keepTouched && u.killTouched)) st.killed = !!(c && c.dead);
      if (!c) st.killed = false;
      u.took.textContent = c ? (c.dead ? 'Took part \u00b7 dead on the record sheet' : 'Took part') : 'Benched: not in the battle';
      u.took.classList.toggle('outcome-bad', !!(c && c.dead));
      part(u, !!c);
      if (u.killedBox) u.killedBox.checked = st.killed;
    });
  }

  function payload() {
    var out = { pilots: [], mission: {} };
    var ok = true;
    function clean(list, values) {
      var v = {};
      list.forEach(function (i) {
        var x = values[i.id];
        if (typeof x === 'number' && isNaN(x)) ok = false;
        v[i.id] = x;
      });
      return v;
    }
    pilots.forEach(function (p) {
      var st = state.pilots[p.id];
      out.pilots.push({ id: p.id, tookPart: st.tookPart, killed: st.tookPart && st.killed, mvp: state.mvp === p.id, values: st.tookPart ? clean(byScope('pilot'), st.values) : {} });
    });
    out.mission = clean(byScope('mission'), state.mission);
    return ok ? out : null;
  }

  function request(url) {
    var data = payload();
    if (!data) return Promise.resolve(null);
    return fetch(url, {
      method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, credentials: 'same-origin', cache: 'no-store',
      body: 'campaign=' + encodeURIComponent(dialog.dataset.campaign) + '&scenario=' + encodeURIComponent(dialog.dataset.scenario) + '&inputs=' + encodeURIComponent(JSON.stringify(data))
    });
  }

  // Shows the preview under each pilot, and the mission totals.
  function render(res) {
    pilots.forEach(function (p) {
      var ul = document.getElementById('outcome-preview-' + p.id);
      if (!ul) return;
      ul.textContent = '';
      var r = res && res.pilots.filter(function (x) { return x.id === p.id; })[0];
      if (!r) return;
      r.notes.forEach(function (n) { ul.appendChild(el('li', r.kia ? 'outcome-bad' : r.status === 'benched' ? 'muted' : 'outcome-good', n)); });
    });
    var lines = [];
    var money = function (label, text) { return { label: label, text: text }; };
    if (res) {
      (res.notes || []).forEach(function (n) { lines.push({ text: n }); });
      var before = parseInt(dialog.dataset.warchest, 10);
      if (res.pool) {
        // The breakdown of the pool: what the mission awarded, what the pilots take of it, and what the warchest ends with.
        lines.push(money('Awarded', res.pool + ' SP'));
        lines.push(money('Assigned to pilots', (res.paid || 0) + ' SP'));
      }
      if (res.warchest) {
        lines.push(money('To the warchest', (res.warchest > 0 ? '+' : '') + res.warchest + ' SP'));
        lines.push(money('Warchest', isNaN(before) ? (res.warchest > 0 ? '+' : '') + res.warchest + ' SP' : before + ' \u2192 ' + (before + res.warchest) + ' SP'));
      }
      if (res.keywords && res.keywords.length) lines.push({ text: 'Keywords: ' + (res.keywordNames || res.keywords).join(', ') });
      if (res.freeRecruits) lines.push({ text: res.freeRecruits === 1 ? '1 free recruit: a replacement pilot can be recruited for free.' : res.freeRecruits + ' free recruits: replacement pilots can be recruited for free.' });
    }
    total.hidden = !lines.length;
    total.textContent = '';
    lines.forEach(function (l) {
      var row = el('div', 'outcome-total-row');
      if (l.label) { row.appendChild(el('span', 'muted', l.label)); row.appendChild(el('b', null, l.text)); } else row.textContent = l.text;
      total.appendChild(row);
    });
  }

  // ---- The dialog in the URL (#outcome=...): { v: 1, p: { pilot id: { a: active 1/0, k: killed 1/0, v: { input id: value } } }, m: { input id: value }, mvp }.
  // Only what was entered is kept: a number box left empty and an unticked box are left out.
  var restoring = null; // a state read from the URL, waiting for the dialog to be built (and the battle read) to be put back on
  function encode(o) { return btoa(unescape(encodeURIComponent(JSON.stringify(o)))).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, ''); }
  function decode(t) {
    try {
      var b = t.replace(/-/g, '+').replace(/_/g, '/');
      var d = JSON.parse(decodeURIComponent(escape(atob(b + '==='.slice((b.length + 3) % 4)))));
      return d && d.v === 1 ? d : null;
    } catch (e) {
      return null;
    }
  }
  function entered(list, values) {
    var out = {}, any = false;
    list.forEach(function (i) {
      var x = values[i.id];
      if (i.type === 'bool' ? x === true && !i.required : typeof x === 'number' && !isNaN(x)) { out[i.id] = i.type === 'bool' ? 1 : x; any = true; }
    });
    return any ? out : null;
  }
  // The URL follows the open dialog; only closing it (or committing) takes it out, so nothing done while the dialog is closed touches it.
  function saveState() {
    if (restoring || !state || !dialog.open || !window.history || !history.replaceState) return;
    var d = { v: 1, p: {} };
    pilots.forEach(function (p) {
      var st = state.pilots[p.id], o = { a: st.tookPart ? 1 : 0, k: st.tookPart && st.killed ? 1 : 0 };
      var v = st.tookPart && entered(byScope('pilot'), st.values);
      if (v) o.v = v;
      d.p[p.id] = o;
    });
    var m = entered(byScope('mission'), state.mission);
    if (m) d.m = m;
    if (state.mvp) d.mvp = state.mvp;
    var hash = '#outcome=' + encode(d);
    if (location.hash !== hash) history.replaceState(history.state, '', location.pathname + location.search + hash);
  }
  function clearState() {
    if (location.hash.indexOf('#outcome=') === 0 && window.history && history.replaceState) history.replaceState(history.state, '', location.pathname + location.search);
  }
  // Puts saved values back over the defaults, keeping only what still holds: inputs the mission still has, of their type and within their range, pilots still in
  // the campaign, and an MVP who is one of them (mvpOptions and the preview then drop one who can't be the MVP). With a battle behind it, who took part comes from
  // the battle, not the URL.
  function restoreValues(list, values, saved, boxes) {
    if (!saved || typeof saved !== 'object') return;
    list.forEach(function (i) {
      if (!Object.prototype.hasOwnProperty.call(saved, i.id)) return;
      var x = saved[i.id], box = boxes[i.id];
      if (i.type === 'bool') {
        if (i.required || (x !== 1 && x !== 0 && typeof x !== 'boolean')) return;
        values[i.id] = !!x;
        if (box) box.checked = !!x;
      } else {
        if (typeof x !== 'number' || Math.floor(x) !== x || x < i.min || x > i.max) return;
        values[i.id] = x;
        if (box) box.value = x;
      }
    });
  }
  function restore(d) {
    var saved = d.p && typeof d.p === 'object' ? d.p : {};
    pilots.forEach(function (p) {
      var s = saved[p.id], st = state.pilots[p.id], u = ui[p.id];
      if (!s || typeof s !== 'object') return;
      if (!inBattle && (s.a === 0 || s.a === 1)) {
        st.tookPart = s.a === 1;
        u.tb.checked = st.tookPart;
        part(u, st.tookPart);
      }
      if (s.k === 0 || s.k === 1) { st.killed = st.tookPart && s.k === 1; u.killTouched = true; } // the player's, so a fresh battle read keeps it
      if (!st.tookPart) st.killed = false;
      u.killedBox.checked = st.killed;
      if (st.tookPart) restoreValues(byScope('pilot'), st.values, s.v, u.boxes);
    });
    restoreValues(byScope('mission'), state.mission, d.m, missionBoxes);
    if (typeof d.mvp === 'string' && pilots.some(function (p) { return p.id === d.mvp; })) state.mvp = d.mvp;
  }
  // Called once the dialog is built (and filled in from the battle, if any): puts back a state from the URL, then works out the result.
  function ready() {
    if (restoring) { restore(restoring); restoring = null; }
    changed();
  }

  function changed() {
    mvpOptions();
    saveState();
    error.textContent = '';
    valid = false;
    commit.disabled = true;
    clearTimeout(timer);
    var mine = ++seq;
    timer = setTimeout(function () {
      var p = request('/api/campaign/scenario-outcome');
      p.then(function (res) {
        if (!res) { render(null); total.hidden = false; total.textContent = 'Enter every box to see the result.'; return; } // an incomplete form: no result yet
        if (mine !== seq) return;
        if (!res.ok) { return res.text().then(function (t) { if (mine === seq) error.textContent = t.trim() || 'Could not work out the result.'; }); }
        return res.json().then(function (data) {
          if (mine !== seq) return;
          candidates = data.mvpCandidates || [];
          mvpOptions();
          saveState(); // the MVP may have been dropped
          render(data);
          // The result is previewed without an MVP too, but it can only be committed once the MVP is chosen.
          valid = !mvpMissing();
          commit.disabled = !valid;
          if (!valid) { total.hidden = false; total.appendChild(el('div', 'outcome-total-row outcome-mvp-needed', 'Choose the mission\u2019s MVP to commit.')); }
        });
      }, function () { if (mine === seq) error.textContent = 'Could not reach the server.'; });
    }, 150);
  }

  open.addEventListener('click', function () {
    error.textContent = '';
    total.hidden = true;
    commit.disabled = true;
    inBattle = dialog.dataset.inBattle === '1';
    if (!inBattle) { build(); dialog.showModal(); ready(); return; }
    // With a battle behind it, who took part and who is dead come from its record sheets. The page carries the last read of them (the battle view's), so the
    // dialog opens from that at once while a fresh read is made; if the fresh one differs, the cards follow it, keeping a Killed in action box the player has
    // changed. With no earlier read, nothing that depends on them is shown until they have been read. Committing reads the battle again on the server either way.
    var cached = null;
    try { cached = dialog.dataset.cachedStatus ? JSON.parse(dialog.dataset.cachedStatus) : null; } catch (e) { cached = null; }
    var note = null, mine = ++battleSeq; // its own count: the preview's requests (seq) start while this one is out
    if (cached) {
      build();
      applyBattle(crewOf(cached));
      note = el('p', 'muted outcome-battle-note', 'Checking MekBay for changes to the battle');
      body.insertBefore(note, body.firstChild);
      dialog.showModal();
      ready();
    } else {
      body.textContent = '';
      state = null;
      body.appendChild(el('p', 'muted outcome-battle-note', 'Reading the battle from MekBay'));
      dialog.showModal();
    }
    fetch('/api/battle/status?campaign=' + encodeURIComponent(dialog.dataset.campaign) + '&scenario=' + encodeURIComponent(dialog.dataset.scenario), { credentials: 'same-origin' }).then(function (res) {
      if (!res.ok) return res.text().then(function (t) { throw new Error(t.trim() || 'Could not read the battle.'); });
      return res.json();
    }).then(function (status) {
      if (mine !== battleSeq || !dialog.open) return;
      if (cached) {
        if (note) note.remove();
        if (JSON.stringify(crewOf(status)) !== JSON.stringify(crewOf(cached))) { applyBattle(crewOf(status), true); changed(); }
        return;
      }
      build();
      applyBattle(crewOf(status));
      ready();
    }, function (err) {
      if (mine !== battleSeq) return;
      if (cached) { if (note) note.textContent = 'Could not reach MekBay: this is the battle as last read. Committing reads it again.'; return; }
      body.textContent = '';
      error.textContent = (err && err.message) || 'Could not read the battle.';
    });
  });
  document.getElementById('outcome-cancel').addEventListener('click', function () { dialog.close(); });
  dialog.addEventListener('click', function (e) { if (e.target === dialog) dialog.close(); });
  dialog.addEventListener('close', function () { restoring = null; clearState(); });
  document.getElementById('outcome-form').addEventListener('submit', function (e) {
    e.preventDefault();
    if (!valid || commit.disabled) return;
    var ask = Confirm.ask({
      title: 'Commit this outcome?',
      message: 'The rewards and losses are applied now and the mission is marked completed. This cannot be undone here.',
      confirmLabel: 'Commit', danger: true
    });
    ask.then(function (yes) {
      if (!yes) return;
      commit.disabled = true;
      request('/api/campaign/scenario-outcome/commit').then(function (res) {
        if (res && res.ok) { clearState(); location.reload(); return; } // cleared first: a reload with it would open the dialog again
        commit.disabled = false;
        if (res) res.text().then(function (t) { error.textContent = t.trim() || 'Could not save the outcome.'; });
      }, function () { commit.disabled = false; error.textContent = 'Could not reach the server.'; });
    });
  });

  // A reload (or a bookmark) with the dialog in the URL opens it as the page would, and puts the saved values back once it is built. Only while the mission can
  // still be completed from here: the dialog is on the page (not for a mission completed or locked), its button isn't disabled (another mission's battle), and a
  // mission with forces has its battle in progress (its button is on the Operation view, which shows only then).
  (function () {
    if (location.hash.indexOf('#outcome=') !== 0) return;
    var saved = decode(location.hash.slice(9));
    var hosted = open.closest('dialog'); // the Deploy wizard's Operation view, for a mission with a battle
    if (!saved || open.disabled || (hosted && dialog.dataset.inBattle !== '1')) { clearState(); return; }
    restoring = saved;
    open.click();
  })();
})();
