// Manage page: "Purchase new unit". A popup with a search box over MekBay's unit list (GET /api/units/search), laid out like a shop. Clicking a result
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
  if (!dialog || !openBtn || !section || typeof MechRow === 'undefined') return;

  var canModify = section.dataset.canModify === '1';
  var warchest = parseInt(section.dataset.warchest, 10) || 0;
  var NO_UID = 'This hangar is from before the site kept its own copy. Import it again under Manage force, Danger zone, Re-import hangar.';
  var LIMIT = 10; // the server's limit on units per purchase

  var staged = [];  // { key, unit }
  var nextKey = 1;
  var lastResults = null, lastQuery = ''; // the latest search reply, repainted as the cart changes

  var listEl = document.getElementById('purchase-staged');
  var headEl = document.getElementById('purchase-staged-head');
  var costEl = document.getElementById('purchase-cost');
  var errEl = document.getElementById('purchase-error');
  var buyBtn = document.getElementById('purchase-buy');
  var search = document.getElementById('purchase-search');
  var clearBtn = document.getElementById('purchase-clear');
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

  // ---- The search: GET /api/units/search over MekBay's unit list. A campaign that names a faction and an era limits it to what that faction fields
  // in that era until the player unticks the box. A result is a hangar-style row: click it (or press Enter or Space on it) to add it to the cart or take it out.
  var availableBox = document.getElementById('purchase-available');
  var searchSeq = 0, searchTimer = null;

  function paintResults() {
    if (!lastResults) return;
    var data = lastResults, units = data.units || [];
    var scroll = results.querySelector('.purchase-found');
    var keep = scroll ? scroll.scrollTop : 0;
    results.textContent = '';
    if (!units.length) {
      var none = 'No mechs match “' + lastQuery + '”.';
      if (data.filter && data.filter.active) none += ' Untick the box above to search every unit.';
      results.appendChild(el('p', 'muted', none));
      return;
    }
    var list = el('div', 'mech-list purchase-found');
    units.forEach(function (u) {
      var row = plain(u);
      if (u.mekbayUrl) row.externalUrl = u.mekbayUrl;
      if (countOf(u.unit)) { row.picked = true; row.badge = 'In cart'; }
      row.onPick = function () { toggle(u); };
      list.appendChild(MechRow.build(row));
    });
    results.appendChild(list);
    list.scrollTop = keep;
    if (data.limited) results.appendChild(el('p', 'muted', 'Showing the first ' + units.length + ' of ' + data.total + ' matches. Type more to narrow them down.'));
  }

  function showResults(data, query) { lastResults = data; lastQuery = query; paintResults(); }

  function runSearch() {
    var q = search.value.trim();
    var seq = ++searchSeq;
    if (!q) { lastResults = null; results.textContent = ''; return; } // nothing typed: nothing to show (the box's placeholder says what to do)
    var url = '/api/units/search?campaign=' + encodeURIComponent(section.dataset.campaign) + '&q=' + encodeURIComponent(q) + (availableBox && !availableBox.checked ? '&all=1' : '');
    fetch(url, { credentials: 'same-origin' }).then(function (res) {
      if (!res.ok) throw new Error('search failed');
      return res.json();
    }).then(function (data) {
      if (seq === searchSeq) showResults(data, q); // a reply for text that has since changed is ignored
    }, function () {
      if (seq !== searchSeq) return;
      lastResults = null;
      results.textContent = '';
      results.appendChild(el('p', 'form-error', 'Could not search the unit list. Try again.'));
    });
  }
  function queueSearch() { clearTimeout(searchTimer); searchTimer = setTimeout(runSearch, 250); }
  search.addEventListener('input', function () { clearBtn.hidden = !search.value; queueSearch(); });
  // Enter belongs to the search, not to Buy.
  search.addEventListener('keydown', function (e) { if (e.key === 'Enter') { e.preventDefault(); clearTimeout(searchTimer); runSearch(); } });
  clearBtn.addEventListener('click', function () { search.value = ''; clearBtn.hidden = true; clearTimeout(searchTimer); runSearch(); search.focus(); });
  if (availableBox) availableBox.addEventListener('change', runSearch);

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
