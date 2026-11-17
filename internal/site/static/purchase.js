// Manage page: "Purchase new unit". A popup with a search box for MekBay's unit list (GET /api/units/search), a staging area for the
// units chosen so far, shown as the same mech rows as the hangar (mechrow.js), and a Buy button that commits the purchase (POST /api/hangar/purchase).
// BV is SP: each unit costs its base BV, shown against the warchest. mechrow.js must load first. Names go in as text, never HTML.
//
// A search result's Stage button calls stage(unit), with unit = { unit: <MekBay unit ID, e.g. BMMadCat_H>, name, model, icon, role, class, tons, bv };
// staging the same unit twice buys two of it, and each staged row has a Remove button.
(function () {
  var dialog = document.getElementById('purchase-dialog');
  var openBtn = document.getElementById('purchase-open');
  var section = document.getElementById('hangar-units');
  if (!dialog || !openBtn || !section || typeof MechRow === 'undefined') return;

  var canModify = section.dataset.canModify === '1';
  var warchest = parseInt(section.dataset.warchest, 10) || 0;
  var NO_UID = 'Add your MekBay user ID in the campaign details so the site can change your hangar.';
  var LIMIT = 10; // the server's limit on units per purchase

  var staged = [];  // { key, unit }
  var nextKey = 1;

  var listEl = document.getElementById('purchase-staged');
  var costEl = document.getElementById('purchase-cost');
  var errEl = document.getElementById('purchase-error');
  var buyBtn = document.getElementById('purchase-buy');
  var search = document.getElementById('purchase-search');
  var results = document.getElementById('purchase-results');

  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }
  function commas(n) { return String(n).replace(/\B(?=(\d{3})+(?!\d))/g, ','); }
  // A unit view without the pilot fields: skills belong to assigning pilots, not to the unit being bought.
  function plain(u) {
    var row = {};
    Object.keys(u).forEach(function (k) { if (k !== 'gunnery' && k !== 'piloting' && k !== 'pilot') row[k] = u[k]; });
    return row;
  }
  function total() { return staged.reduce(function (sum, s) { return sum + (Number(s.unit.bv) || 0); }, 0); }

  function render() {
    listEl.textContent = '';
    if (!staged.length) listEl.appendChild(el('p', 'muted', 'Nothing staged yet.'));
    staged.forEach(function (s) {
      var row = plain(s.unit);
      row.actions = [{ label: 'Remove', onClick: function () { staged = staged.filter(function (x) { return x !== s; }); render(); } }];
      listEl.appendChild(MechRow.build(row));
    });
    var cost = total(), after = warchest - cost;
    costEl.textContent = 'Cost ' + commas(cost) + ' SP. Warchest ' + commas(warchest) + (cost ? ' → ' + commas(Math.max(after, 0)) : '') + ' SP.' + (after < 0 ? ' Not enough funds.' : '');
    costEl.classList.toggle('short', after < 0);
    var why = !canModify ? NO_UID : !staged.length ? 'Stage at least one unit to buy.' : after < 0 ? 'The warchest cannot cover that.' : '';
    buyBtn.disabled = !!why;
    buyBtn.title = why;
    buyBtn.textContent = staged.length ? 'Buy for ' + commas(cost) + ' SP' : 'Buy';
  }

  // Stages a unit for purchase. Returns false when the staging area is full.
  function stage(unit) {
    if (staged.length >= LIMIT) { errEl.textContent = 'You can buy at most ' + LIMIT + ' units at a time.'; return false; }
    errEl.textContent = '';
    staged.push({ key: nextKey++, unit: unit });
    render();
    return true;
  }

  // ---- The search: GET /api/units/search over MekBay's unit list. A campaign that names a faction and an era limits it to what that faction fields
  // in that era until the player unticks the box. Results are the same rows as the hangar, each with a Stage button.
  var availableBox = document.getElementById('purchase-available');
  var HINT = 'Type a name to search MekBay\u2019s unit list, e.g. \u201ctimber wolf\u201d.';
  var searchSeq = 0, searchTimer = null;

  function showResults(data, query) {
    results.textContent = '';
    var units = (data && data.units) || [];
    if (!units.length) {
      var none = 'No mechs match \u201c' + query + '\u201d.';
      if (data && data.filter && data.filter.active) none += ' Untick the box above to search every unit.';
      results.appendChild(el('p', 'muted', none));
      return;
    }
    var list = el('div', 'mech-list purchase-found');
    units.forEach(function (u) {
      var row = plain(u);
      if (u.mekbayUrl) row.externalUrl = u.mekbayUrl;
      row.actions = [{ label: 'Stage', onClick: function () { stage(u); } }];
      list.appendChild(MechRow.build(row));
    });
    results.appendChild(list);
    if (data.limited) results.appendChild(el('p', 'muted', 'Showing the first ' + units.length + ' of ' + data.total + ' matches. Type more to narrow them down.'));
  }

  function runSearch() {
    var q = search.value.trim();
    var seq = ++searchSeq;
    if (!q) { results.textContent = ''; results.appendChild(el('p', 'muted', HINT)); return; }
    var url = '/api/units/search?campaign=' + encodeURIComponent(section.dataset.campaign) + '&q=' + encodeURIComponent(q) + (availableBox && !availableBox.checked ? '&all=1' : '');
    fetch(url, { credentials: 'same-origin' }).then(function (res) {
      if (!res.ok) throw new Error('search failed');
      return res.json();
    }).then(function (data) {
      if (seq === searchSeq) showResults(data, q); // a reply for text that has since changed is ignored
    }, function () {
      if (seq !== searchSeq) return;
      results.textContent = '';
      results.appendChild(el('p', 'form-error', 'Could not search the unit list. Try again.'));
    });
  }
  function queueSearch() { clearTimeout(searchTimer); searchTimer = setTimeout(runSearch, 250); }
  search.addEventListener('input', queueSearch);
  // Enter belongs to the search, not to Buy.
  search.addEventListener('keydown', function (e) { if (e.key === 'Enter') { e.preventDefault(); clearTimeout(searchTimer); runSearch(); } });
  if (availableBox) availableBox.addEventListener('change', runSearch);
  results.appendChild(el('p', 'muted', HINT));

  openBtn.addEventListener('click', function () { errEl.textContent = ''; render(); dialog.showModal(); search.focus(); });
  document.getElementById('purchase-cancel').addEventListener('click', function () { dialog.close(); });
  dialog.addEventListener('click', function (e) { if (e.target === dialog) dialog.close(); });

  document.getElementById('purchase-form').addEventListener('submit', function (e) {
    e.preventDefault();
    if (buyBtn.disabled) return;
    errEl.textContent = '';
    buyBtn.disabled = true;
    var body = 'campaign=' + encodeURIComponent(section.dataset.campaign);
    staged.forEach(function (s) { body += '&unit=' + encodeURIComponent(s.unit.unit); });
    fetch('/api/hangar/purchase', {
      method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body: body, credentials: 'same-origin'
    }).then(function (res) {
      if (res.ok) { staged = []; dialog.close(); location.reload(); return; }
      return res.text().then(function (t) { errEl.textContent = t.trim() || 'Something went wrong.'; render(); });
    }, function () { errEl.textContent = 'Could not reach the server.'; render(); });
  });

  window.Purchase = { stage: stage, staged: function () { return staged.map(function (s) { return s.unit; }); } };
  render();
})();
