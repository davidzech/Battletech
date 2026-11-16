// Campaign page. An unstarted campaign opens the start wizard: one step per starting named pilot (a name and an
// allocator for that pilot's starting SP), then a step for the campaign's MekBay hangar link, with a step count and
// progress bar at the bottom. A started campaign only has a small dialog for changing its hangar link. The server
// checks every field again; this keeps the player from a doomed request and previews the pilots' levels.
(function () {
  var box = document.getElementById('campaign-run');
  if (!box) return;

  var UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
  var HANGAR_HELP = 'Paste a MekBay hangar link, like https://mekbay.com/?instance=…';

  // Mirrors the server's check of a hangar link.
  function hangarProblem(raw) {
    var u;
    try { u = new URL(raw.trim()); } catch (e) { return HANGAR_HELP; }
    var host = u.hostname.toLowerCase();
    if (u.protocol !== 'https:' || (host !== 'mekbay.com' && host !== 'www.mekbay.com')) return HANGAR_HELP;
    if (!UUID.test((u.searchParams.get('instance') || '').toLowerCase())) return 'That MekBay link has no valid instance id (?instance=…).';
    return '';
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

  // ---- A started campaign: change the hangar link.
  var runDialog = document.getElementById('run-dialog');
  var runOpen = document.getElementById('run-open');
  if (runDialog && runOpen && typeof runDialog.showModal === 'function') {
    var runInput = document.getElementById('run-hangar');
    var runError = document.getElementById('run-error');
    runOpen.addEventListener('click', function () { runError.textContent = ''; runDialog.showModal(); runInput.focus(); });
    document.getElementById('run-cancel').addEventListener('click', function () { runDialog.close(); });
    runInput.addEventListener('input', function () { runError.textContent = ''; });
    document.getElementById('run-form').addEventListener('submit', function (e) {
      e.preventDefault();
      var bad = hangarProblem(runInput.value);
      if (bad) { runError.textContent = bad; return; }
      post('campaign=' + encodeURIComponent(box.dataset.campaign) + '&hangar=' + encodeURIComponent(runInput.value.trim())).then(function (res) {
        if (res.ok) { location.reload(); return; }
        return res.text().then(function (t) { runError.textContent = t.trim() || 'Could not save the hangar link.'; });
      }, function () { runError.textContent = 'Could not reach the server.'; });
    });
  }

  // ---- An unstarted campaign: the start wizard.
  var wiz = document.getElementById('start-wizard');
  var wizOpen = document.getElementById('wizard-open');
  if (!wiz || !wizOpen || typeof wiz.showModal !== 'function') return;

  var ladders = JSON.parse(wiz.dataset.ladders);
  var budget = parseInt(wiz.dataset.sp, 10) || 0;
  var pilotCount = parseInt(wiz.dataset.pilots, 10) || 0;
  var total = pilotCount + 1; // a step per pilot, then the hangar link

  var title = document.getElementById('wiz-title');
  var body = document.getElementById('wiz-body');
  var error = document.getElementById('wiz-error');
  var back = document.getElementById('wiz-back');
  var next = document.getElementById('wiz-next');
  var count = document.getElementById('wiz-count');
  var progress = document.getElementById('wiz-progress');
  var bar = document.getElementById('wiz-bar');

  var pilots = [];
  for (var i = 0; i < pilotCount; i++) {
    var sp = {};
    ladders.forEach(function (l) { sp[l.key] = 0; });
    pilots.push({ name: '', sp: sp });
  }
  var hangar = '';
  var step = 0;
  var busy = false;

  function reach(l, spent) {
    var r = { level: l.base, sp: 0 };
    l.rows.forEach(function (row) { if (spent >= row.sp) r = row; });
    return r;
  }
  function upcoming(l, spent) {
    for (var j = 0; j < l.rows.length; j++) if (spent < l.rows[j].sp) return l.rows[j];
    return null;
  }
  function spentOf(p) { return ladders.reduce(function (sum, l) { return sum + p.sp[l.key]; }, 0); }
  function summaryOf(p) {
    return ladders.map(function (l) { return l.name + ' ' + reach(l, p.sp[l.key]).level; }).join(' · ');
  }

  // One column of the allocator: a slider and a number box for the same amount, and what that buys.
  function allocatorRow(p, l, refresh) {
    var row = el('div', 'alloc-row');
    var name = el('label', 'alloc-name', l.name);
    var slider = document.createElement('input');
    slider.type = 'range'; slider.min = 0; slider.max = budget; slider.step = 10;
    var number = document.createElement('input');
    number.type = 'number'; number.min = 0; number.max = budget; number.step = 1; number.inputMode = 'numeric';
    var preview = el('span', 'alloc-preview');
    name.appendChild(number);
    row.appendChild(name);
    row.appendChild(slider);
    row.appendChild(preview);

    function show() {
      var spent = p.sp[l.key];
      slider.value = spent; number.value = spent;
      var r = reach(l, spent), n = upcoming(l, spent);
      preview.textContent = l.name + ' ' + r.level +
        (n ? ' — level ' + n.level + ' at ' + n.sp + ' SP' : ' — top level');
    }
    function set(raw) {
      var n = parseInt(raw, 10);
      if (isNaN(n) || n < 0) n = 0;
      var others = spentOf(p) - p.sp[l.key];
      p.sp[l.key] = Math.min(n, Math.max(0, budget - others)); // never more than is left to allocate
      error.textContent = '';
      refresh();
    }
    slider.addEventListener('input', function () { set(slider.value); });
    number.addEventListener('input', function () { set(number.value); });
    row.show = show;
    show();
    return row;
  }

  function renderPilot(idx) {
    var p = pilots[idx];
    title.textContent = 'Named pilot ' + (idx + 1) + ' of ' + pilotCount;
    body.textContent = '';
    body.appendChild(el('p', 'muted', 'Only named pilots improve their skills, use Edge and earn abilities. Name this pilot, then allocate their ' + budget + ' starting SP across the four columns. Every point has to go somewhere.'));

    var nameLabel = el('label', null, 'Name');
    nameLabel.setAttribute('for', 'wiz-name');
    var nameInput = document.createElement('input');
    nameInput.id = 'wiz-name'; nameInput.className = 'wide'; nameInput.maxLength = 30;
    nameInput.placeholder = 'Star Captain Alrek'; nameInput.spellcheck = false; nameInput.value = p.name;
    nameInput.addEventListener('input', function () { p.name = nameInput.value; error.textContent = ''; });
    body.appendChild(nameLabel);
    body.appendChild(nameInput);

    var remaining = el('p', 'alloc-remaining');
    var summary = el('p', 'alloc-summary');
    var rows = ladders.map(function (l) { return allocatorRow(p, l, refresh); });
    function refresh() {
      var left = budget - spentOf(p);
      remaining.textContent = left + ' of ' + budget + ' SP left to allocate';
      remaining.classList.toggle('done', left === 0);
      rows.forEach(function (r) { r.show(); });
      summary.textContent = summaryOf(p);
    }
    body.appendChild(el('h3', 'alloc-head', 'Allocate SP'));
    body.appendChild(remaining);
    rows.forEach(function (r) { body.appendChild(r); });
    body.appendChild(summary);
    refresh();
    nameInput.focus();
  }

  function renderHangar() {
    title.textContent = 'Link your hangar';
    body.textContent = '';
    if (pilotCount) {
      body.appendChild(el('p', 'muted', 'Your named pilots:'));
      var list = el('ul', 'wiz-recap');
      pilots.forEach(function (p) {
        var li = el('li');
        li.appendChild(el('b', null, p.name.trim()));
        li.appendChild(el('span', 'muted', ' — ' + summaryOf(p)));
        list.appendChild(li);
      });
      body.appendChild(list);
    }
    body.appendChild(el('p', 'muted', 'Paste the link to this campaign’s MekBay hangar, the force holding all of your mechs.'));
    var label = el('label', null, 'MekBay hangar link');
    label.setAttribute('for', 'wiz-hangar');
    var input = document.createElement('input');
    input.id = 'wiz-hangar'; input.className = 'wide'; input.type = 'url'; input.spellcheck = false;
    input.placeholder = 'https://mekbay.com/?instance=…'; input.value = hangar;
    input.addEventListener('input', function () { hangar = input.value; error.textContent = ''; });
    body.appendChild(label);
    body.appendChild(input);
    input.focus();
  }

  function render() {
    error.textContent = '';
    if (step < pilotCount) renderPilot(step); else renderHangar();
    count.textContent = 'Step ' + (step + 1) + ' of ' + total;
    bar.style.width = ((step + 1) / total * 100) + '%';
    progress.setAttribute('aria-valuemax', total);
    progress.setAttribute('aria-valuenow', step + 1);
    back.hidden = step === 0;
    next.textContent = step === total - 1 ? 'Start campaign' : 'Next';
    next.disabled = busy;
  }

  // The problem with the current step, or ''.
  function problem() {
    if (step >= pilotCount) return hangarProblem(hangar);
    var p = pilots[step], name = p.name.trim();
    if (!name) return 'Name your pilot.';
    var clash = pilots.some(function (o, j) { return j !== step && o.name.trim().toLowerCase() === name.toLowerCase(); });
    if (clash) return 'Two of your pilots can’t share a name.';
    var left = budget - spentOf(p);
    if (left !== 0) return 'Allocate all ' + budget + ' SP before moving on (' + left + ' left).';
    return '';
  }

  function submit() {
    var parts = ['campaign=' + encodeURIComponent(wiz.dataset.campaign), 'hangar=' + encodeURIComponent(hangar.trim())];
    pilots.forEach(function (p, idx) {
      parts.push('pilot' + idx + '_name=' + encodeURIComponent(p.name.trim()));
      ladders.forEach(function (l) { parts.push('pilot' + idx + '_' + l.key + '=' + p.sp[l.key]); });
    });
    busy = true;
    next.disabled = true;
    post(parts.join('&')).then(function (res) {
      if (res.ok) { location.reload(); return; }
      busy = false;
      next.disabled = false;
      return res.text().then(function (t) { error.textContent = t.trim() || 'Could not start the campaign.'; });
    }, function () { busy = false; next.disabled = false; error.textContent = 'Could not reach the server.'; });
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
})();
