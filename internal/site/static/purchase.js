// Manage page: "Purchase new unit". A popup with the shared unit search (unitsearch.js, which loads after mechrow.js and before this), laid out like a shop. Clicking a result
// row puts it in "Your cart" below (the row is tinted and badged "In cart"); clicking it again takes it back out. A Buy button commits the purchase
// (POST /api/hangar/purchase). Both lists are the
// same mech rows as the hangar (mechrow.js, which must load first). BV is SP: each unit costs its base BV, shown against the warchest. Names go
// in as text, never HTML.
//
// stage(unit) adds a unit to the cart, with unit = { unit: <MekBay unit ID, e.g. BMMadCat_H>, name, model, icon, role, class, tons, bv }. The cart holds
// one of each unit; each cart row also has a small remove button.
(function () {
  var dialog = document.getElementById('purchase-dialog');
  var openBtn = document.getElementById('purchase-open');
  var section = document.getElementById('hangar-units');
  if (!dialog || !openBtn || !section || typeof MechRow === 'undefined' || typeof UnitSearch === 'undefined') return;

  var canModify = section.dataset.canModify === '1';
  var warchest = parseInt(section.dataset.warchest, 10) || 0;
  var NO_UID = 'This hangar is from before the site kept its own copy. Delete the campaign and start it again to import your hangar.';
  var LIMIT = 10; // the server's limit on units per purchase

  var staged = [];  // { key, unit }
  var nextKey = 1;

  var listEl = document.getElementById('purchase-staged');
  var headEl = document.getElementById('purchase-staged-head');
  var costEl = document.getElementById('purchase-cost');
  var errEl = document.getElementById('purchase-error');
  var buyBtn = document.getElementById('purchase-buy');

  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }
  function commas(n) { return String(n).replace(/\B(?=(\d{3})+(?!\d))/g, ','); }
  var plain = UnitSearch.plain;
  function total() { return staged.reduce(function (sum, s) { return sum + (Number(s.unit.bv) || 0); }, 0); }
  function countOf(id) { return staged.filter(function (s) { return s.unit.unit === id; }).length; }

  // ---- Your cart
  function render() {
    listEl.textContent = '';
    headEl.textContent = staged.length ? 'Your cart (' + staged.length + ')' : 'Your cart';
    if (!staged.length) listEl.appendChild(el('p', 'muted purchase-empty', 'Your cart is empty. Click a unit above to add it.'));
    staged.forEach(function (s) {
      var row = plain(s.unit);
      row.picked = true;
      row.actions = [{ icon: '×', label: 'Remove', title: 'Remove from your cart', onClick: function () { staged = staged.filter(function (x) { return x !== s; }); render(); paintResults(); } }];
      listEl.appendChild(MechRow.build(row));
    });
    var cost = total(), after = warchest - cost;
    costEl.textContent = 'Cost ' + commas(cost) + ' SP. Warchest ' + commas(warchest) + (cost ? ' → ' + commas(Math.max(after, 0)) : '') + ' SP.' + (after < 0 ? ' Not enough funds.' : '');
    costEl.classList.toggle('short', after < 0);
    var why = !canModify ? NO_UID : !staged.length ? 'Add at least one unit to your cart.' : after < 0 ? 'The warchest cannot cover that.' : '';
    buyBtn.disabled = !!why;
    buyBtn.title = why;
    buyBtn.textContent = staged.length ? 'Buy for ' + commas(cost) + ' SP' : 'Buy';
  }

  // Adds a unit to the cart. Returns false when it is already there or the cart is full.
  function stage(unit) {
    if (countOf(unit.unit)) return false;
    if (staged.length >= LIMIT) { errEl.textContent = 'You can buy at most ' + LIMIT + ' units at a time.'; return false; }
    errEl.textContent = '';
    staged.push({ key: nextKey++, unit: unit });
    render();
    paintResults();
    return true;
  }

  // A click on a result: in the cart means take it out again, otherwise put it in.
  function toggle(unit) {
    if (!countOf(unit.unit)) { stage(unit); return; }
    staged = staged.filter(function (s) { return s.unit.unit !== unit.unit; });
    errEl.textContent = '';
    render();
    paintResults();
  }

  // ---- The search (unitsearch.js): a result is in the cart or not, and clicking it toggles that.
  var search = UnitSearch.create({
    campaign: section.dataset.campaign,
    faction: dialog.dataset.faction, era: dialog.dataset.era,
    idPrefix: 'purchase',
    decorate: function (row, u) { if (countOf(u.unit)) { row.picked = true; row.badge = 'In cart'; } },
    onPick: toggle
  });
  document.getElementById('purchase-search-box').appendChild(search.element);
  function paintResults() { search.repaint(); }

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
