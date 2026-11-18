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
  var state = null;   // { pilots: { id: { tookPart, killed, values } }, mission: { id: value } }
  var results = {};   // pilot id -> the preview's lines, and the mission's
  var seq = 0, timer = null, valid = false;

  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }
  function byScope(scope) { return inputs.filter(function (i) { return i.scope === scope; }); }
  function blank(list) {
    var v = {};
    list.forEach(function (i) { v[i.id] = i.type === 'bool' ? false : i.min || 0; });
    return v;
  }

  // One input: a number box or a themed checkbox.
  function field(input, values, onChange, key) {
    key = key || input.id;
    var wrap = el('label', 'outcome-field' + (input.type === 'bool' ? ' check themed-check' : ''));
    var box = document.createElement('input');
    if (input.type === 'bool') {
      box.type = 'checkbox'; box.className = 'themed-box'; box.checked = !!values[key];
      box.addEventListener('change', function () { values[key] = box.checked; onChange(); });
      wrap.appendChild(box);
      wrap.appendChild(el('span', null, input.label));
      if (input.help) wrap.appendChild(el('span', 'muted outcome-help', input.help));
    } else {
      wrap.appendChild(el('span', 'outcome-label', input.label));
      box.type = 'number'; box.min = input.min; box.max = input.max; box.step = 1; box.inputMode = 'numeric'; box.value = values[input.id];
      box.addEventListener('input', function () { var n = parseInt(box.value, 10); values[input.id] = isNaN(n) ? NaN : n; onChange(); });
      wrap.appendChild(box);
    }
    if (input.help) wrap.title = input.help;
    return wrap;
  }

  function build() {
    state = { pilots: {}, mission: blank(byScope('mission')) };
    results = {};
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
      var took = el('label', 'outcome-field check themed-check');
      took.title = 'Untick if the pilot sat this one out (benched): they get half the pilot SP, if the mission gives any.';
      var tb = document.createElement('input');
      tb.type = 'checkbox'; tb.className = 'themed-box'; tb.checked = true;
      took.appendChild(tb); took.appendChild(el('span', null, 'Active'));
      head.appendChild(took);
      card.appendChild(head);
      var fields = el('div', 'outcome-fields');
      pilotInputs.forEach(function (i) { fields.appendChild(field(i, st.values, changed)); });
      // Every pilot can be killed in action, whatever the scenario asks: a pilot killed gets nothing and is removed from the campaign.
      fields.appendChild(field({ id: 'killed', label: 'Killed in action', type: 'bool' }, st, changed, 'killed'));
      card.appendChild(fields);
      var preview = el('ul', 'outcome-preview');
      preview.id = 'outcome-preview-' + p.id;
      card.appendChild(preview);
      tb.addEventListener('change', function () { st.tookPart = tb.checked; if (!tb.checked) st.killed = false; fields.hidden = !tb.checked; changed(); });
      body.appendChild(card);
    });
    var mission = byScope('mission');
    if (mission.length) {
      var box = el('div', 'outcome-mission');
      box.appendChild(el('h3', 'alloc-head', 'The mission'));
      mission.forEach(function (i) { box.appendChild(field(i, state.mission, changed)); });
      body.appendChild(box);
    }
    render(null);
  }

  // What is sent: booleans as true/false, numbers as numbers (a number box left empty makes the request invalid until it is filled).
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
      out.pilots.push({ id: p.id, tookPart: st.tookPart, killed: st.tookPart && st.killed, values: st.tookPart ? clean(byScope('pilot'), st.values) : {} });
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
    if (res) {
      (res.notes || []).forEach(function (n) { lines.push(n); });
      if (res.warchest && !res.pool) lines.push((res.warchest > 0 ? '+' : '') + res.warchest + ' SP to the warchest');
      if (res.keywords && res.keywords.length) lines.push('Keywords: ' + res.keywords.join(', '));
      if (res.freeRecruits) lines.push(res.freeRecruits === 1 ? '1 free recruit: a replacement pilot can be recruited for free.' : res.freeRecruits + ' free recruits: replacement pilots can be recruited for free.');
    }
    total.hidden = !lines.length;
    total.textContent = lines.join(' · ');
  }

  function changed() {
    error.textContent = '';
    valid = false;
    commit.disabled = true;
    clearTimeout(timer);
    var mine = ++seq;
    timer = setTimeout(function () {
      var p = request('/api/campaign/scenario-outcome');
      if (!p) return;
      p.then(function (res) {
        if (!res) { error.textContent = 'Fill in every box.'; return; }
        if (mine !== seq) return;
        if (!res.ok) { return res.text().then(function (t) { if (mine === seq) error.textContent = t.trim() || 'Could not work out the result.'; }); }
        return res.json().then(function (data) { if (mine !== seq) return; render(data); valid = true; commit.disabled = false; });
      }, function () { if (mine === seq) error.textContent = 'Could not reach the server.'; });
    }, 150);
  }

  open.addEventListener('click', function () { build(); error.textContent = ''; dialog.showModal(); changed(); });
  document.getElementById('outcome-cancel').addEventListener('click', function () { dialog.close(); });
  dialog.addEventListener('click', function (e) { if (e.target === dialog) dialog.close(); });
  document.getElementById('outcome-form').addEventListener('submit', function (e) {
    e.preventDefault();
    if (!valid || commit.disabled) return;
    var ask = window.Confirm ? Confirm.ask({
      title: 'Commit this outcome?',
      message: 'The rewards and losses are applied now and the mission is marked completed. This cannot be undone here.',
      confirmLabel: 'Commit', danger: true
    }) : Promise.resolve(true);
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
