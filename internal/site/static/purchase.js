// Manage page: "Purchase new unit". A popup with a search box for MekBay's unit list (still a stub: it lists nothing yet), a staging area for the
// units chosen so far, shown as the same mech rows as the hangar (mechrow.js), and a Buy button that commits the purchase (POST /api/hangar/purchase).
// BV is SP: each unit costs its base BV, shown against the warchest. mechrow.js must load first. Names go in as text, never HTML.
//
// When the search exists it only has to call Purchase.stage(unit), with unit = { unit: <MekBay unit ID, e.g. BMMadCat_H>, name, model, icon, role,
// class, tons, bv }; staging the same unit twice buys two of it, and each staged row has a Remove button.
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
  function total() { return staged.reduce(function (sum, s) { return sum + (Number(s.unit.bv) || 0); }, 0); }

  function render() {
    listEl.textContent = '';
    if (!staged.length) listEl.appendChild(el('p', 'muted', 'Nothing staged yet.'));
    staged.forEach(function (s) {
      var row = {};
      Object.keys(s.unit).forEach(function (k) { row[k] = s.unit[k]; });
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

  // The search is a stub for now: it takes the text but finds nothing.
  var STUB = 'Unit search is not connected yet. When it is, matching units from MekBay’s list will appear here to stage.';
  results.textContent = STUB;
  search.addEventListener('input', function () { results.textContent = STUB; });

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
