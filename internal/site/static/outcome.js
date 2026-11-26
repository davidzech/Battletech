// The mission completion dialog: report each named pilot's inputs (who took part, kills, whether they were killed, ... whatever the scenario's outcome engine asks for,
// see outcomes.go), see the result as you enter it, and commit the mission as completed. The preview comes from the server (POST /api/campaign/scenario-outcome) so
// the rules live in one place; committing (POST /api/campaign/scenario-outcome/commit) applies them and cannot be undone, so it asks first (confirm.js).
(function () {
  var dialog = document.getElementById('outcome-dialog');
  var open = document.getElementById('outcome-open');
  if (!dialog || !open || typeof dialog.showModal !== 'function') return;
  var inputs = JSON.parse(dialog.dataset.inputs || '[]');
  var pilots = JSON.parse(dialog.dataset.pilots || '[]');
  var body = document.getElementById('outcome-body');
  var total = document.getElementById('outcome-total');
  var error = document.getElementById('outcome-error');
  var commit = document.getElementById('outcome-commit');
  var mvpSP = parseInt(dialog.dataset.mvpSp, 10) || 0; // the extra SP the mission's MVP earns; 0 means the mission has no MVP
  var mvpSelect = null, mvpNote = null; // the MVP picker and its note, when the mission has an MVP
  var candidates = null; // who can be the MVP, from the last preview (took part and stays in the campaign); null before the first
  // Whether a battle is in progress, so who took part and who is dead are read from it. Checked when the dialog opens, not when the page loads: Start battle
  // (mekbay.js) begins one without reloading the page and marks the dialog then.
  var inBattle = false;
  var ui = {};        // pilot id -> { took, fields, killedBox }, the parts of a card the battle fills in
  var state = null;   // { pilots: { id: { tookPart, killed, values } }, mission: { id: value }, mvp: the MVP's pilot id, '' while not chosen }
  var results = {};   // pilot id -> the preview's lines, and the mission's
  var seq = 0, timer = null, valid = false;

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
    mvpSelect = null; mvpNote = null; candidates = null;
    results = {};
    ui = {};
    body.textContent = '';
    var pilotInputs = byScope('pilot');
    if (!pilots.length) body.appendChild(el('p', 'muted', 'You have no living named pilots to report on.'));
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
      var took, tb = null;
      if (inBattle) {
        // The battle says who took part (they crew a mech in it), so there is nothing to tick; the card says which it is once the battle has been read.
        took = el('span', 'outcome-took muted', '');
        head.appendChild(took);
      } else {
        took = el('label', 'outcome-field check themed-check');
        took.title = 'Untick if the pilot sat this one out (benched): they get half the pilot SP, if the mission gives any.';
        tb = document.createElement('input');
        tb.type = 'checkbox'; tb.className = 'themed-box'; tb.checked = true;
        took.appendChild(tb); took.appendChild(el('span', null, 'Active'));
        head.appendChild(took);
      }
      card.appendChild(head);
      var fields = el('div', 'outcome-fields');
      pilotInputs.forEach(function (i) { fields.appendChild(field(i, st.values, changed)); });
      // Every pilot can be killed in action, whatever the scenario asks: a pilot killed gets nothing and is removed from the campaign.
      fields.appendChild(field({ id: 'killed', label: 'Killed in action', type: 'bool' }, st, changed, 'killed'));
      card.appendChild(fields);
      ui[p.id] = { took: took, fields: fields, killedBox: fields.lastChild.querySelector('input') };
      var preview = el('ul', 'outcome-preview');
      preview.id = 'outcome-preview-' + p.id;
      card.appendChild(preview);
      if (tb) tb.addEventListener('change', function () { st.tookPart = tb.checked; if (!tb.checked) { st.killed = false; ui[p.id].killedBox.checked = false; } fields.hidden = !tb.checked; changed(); });
      body.appendChild(card);
    });
    var mission = byScope('mission');
    if (mission.length) {
      var box = el('div', 'outcome-mission');
      box.appendChild(el('h3', 'alloc-head', 'The mission'));
      mission.forEach(function (i) { box.appendChild(field(i, state.mission, changed)); });
      body.appendChild(box);
    }
    if (mvpSP > 0 && pilots.length) {
      // Every mission has an MVP: one pilot who took part and stays in the campaign earns the extra SP, free (it doesn't come out of the mission's SP). It must be
      // chosen whenever anyone can be it, so the mission can't be committed until it is.
      var mvp = el('div', 'outcome-mission outcome-mvp');
      mvp.appendChild(el('h3', 'alloc-head', 'Mission MVP'));
      mvpNote = el('label', 'outcome-mvp-label muted', '');
      mvpNote.htmlFor = 'outcome-mvp';
      mvp.appendChild(mvpNote);
      mvpSelect = document.createElement('select');
      mvpSelect.id = 'outcome-mvp';
      mvpSelect.addEventListener('change', function () { state.mvp = mvpSelect.value; changed(); });
      mvp.appendChild(mvpSelect);
      body.appendChild(mvp);
      mvpOptions();
    }
    render(null);
  }

  // The pilots who can be the MVP now: active and not killed, and (once the server has worked the result out) not removed by the mission's rules either, such as a
  // pilot who fails the Trial of Position.
  function eligible() {
    return pilots.filter(function (p) {
      var st = state.pilots[p.id];
      return st.tookPart && !st.killed && (!candidates || candidates.indexOf(p.id) >= 0);
    });
  }

  // Fills the MVP picker with the pilots who can be it; a choice that no longer fits is cleared.
  function mvpOptions() {
    if (!mvpSelect) return;
    var list = eligible();
    if (state.mvp && !list.some(function (p) { return p.id === state.mvp; })) state.mvp = '';
    var none = !list.length;
    mvpSelect.hidden = none;
    mvpNote.textContent = none ? 'No named pilot took part and stays in the campaign, so this mission has no MVP.'
      : 'The most valuable pilot earns +' + mvpSP + ' SP, free: it does not come out of the mission\u2019s SP. Choose one of the pilots who took part and stay in the campaign.';
    mvpSelect.textContent = '';
    var choose = new Option('Choose the MVP', '');
    choose.disabled = true;
    mvpSelect.appendChild(choose);
    list.forEach(function (p) { mvpSelect.appendChild(new Option(p.name, p.id)); });
    mvpSelect.value = state.mvp;
  }

  // Whether the mission still needs its MVP chosen: someone can be it and nobody is.
  function mvpMissing() { return !!mvpSelect && !state.mvp && eligible().length > 0; }

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
  function applyBattle(crew) {
    pilots.forEach(function (p) {
      var c = crew[p.name.toLowerCase()], u = ui[p.id], st = state.pilots[p.id];
      st.tookPart = !!c;
      st.killed = !!(c && c.dead);
      u.took.textContent = c ? (c.dead ? 'Took part \u00b7 dead on the record sheet' : 'Took part') : 'Benched: not in the battle';
      u.took.classList.toggle('outcome-bad', !!(c && c.dead));
      u.fields.hidden = !c;
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

  function changed() {
    mvpOptions();
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
    if (!inBattle) { build(); dialog.showModal(); changed(); return; }
    // With a battle behind it, who took part and who is dead come from its record sheets, so nothing that depends on them is shown until they have been read.
    body.textContent = '';
    state = null;
    body.appendChild(el('p', 'muted outcome-battle-note', 'Reading the battle from MekBay'));
    dialog.showModal();
    var mine = ++seq;
    fetch('/api/battle/status?campaign=' + encodeURIComponent(dialog.dataset.campaign) + '&scenario=' + encodeURIComponent(dialog.dataset.scenario), { credentials: 'same-origin' }).then(function (res) {
      if (!res.ok) return res.text().then(function (t) { throw new Error(t.trim() || 'Could not read the battle.'); });
      return res.json();
    }).then(function (status) {
      if (mine !== seq || !dialog.open) return;
      build();
      applyBattle(crewOf(status));
      changed();
    }, function (err) {
      if (mine !== seq) return;
      body.textContent = '';
      error.textContent = (err && err.message) || 'Could not read the battle.';
    });
  });
  document.getElementById('outcome-cancel').addEventListener('click', function () { dialog.close(); });
  dialog.addEventListener('click', function (e) { if (e.target === dialog) dialog.close(); });
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
        if (res && res.ok) { location.reload(); return; }
        commit.disabled = false;
        if (res) res.text().then(function (t) { error.textContent = t.trim() || 'Could not save the outcome.'; });
      }, function () { commit.disabled = false; error.textContent = 'Could not reach the server.'; });
    });
  });
})();
