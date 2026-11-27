// Manage page: "Purchase New Units". A popup with the shared unit search (unitsearch.js, which loads after mechrow.js and before this), laid out like a shop. Clicking a result
// row puts it in "Your cart" below (the row is tinted and badged "In cart"); clicking it again takes it back out. A Buy button commits the purchase
// (POST /api/hangar/purchase). Both lists are the
// same mech rows as the hangar (mechrow.js, which must load first). BV is SP: each unit costs its base BV, shown against the warchest. Names go
// in as text, never HTML.
//
// The dialog's state is kept in the page's fragment (#purchase=...), so a reload brings it back: open, with its search (the query and the faction box) and its
// cart (the units' IDs, looked up again with GET /api/units). The fragment is replaced, not added to the history, and goes when the dialog closes or buys.
//
// stage(unit) adds a unit to the cart, with unit = { unit: <MekBay unit ID, e.g. BMMadCat_H>, name, model, icon, role, class, tons, bv }. The cart holds
// one of each unit; each cart row also has a small remove button.
(function () {
  var dialog = document.getElementById('purchase-dialog');
  var openBtn = document.getElementById('purchase-open');
  var section = document.getElementById('hangar-units');
  if (!dialog || !openBtn || !section || typeof MechRow === 'undefined' || typeof UnitSearch === 'undefined') return;

  var warchest = parseInt(section.dataset.warchest, 10) || 0;
  var LIMIT = 10; // the server's limit on units per purchase

  var staged = [];  // { unit }

  var listEl = document.getElementById('purchase-staged');
  var headEl = document.getElementById('purchase-staged-head');
  var costEl = document.getElementById('purchase-cost');
  var errEl = document.getElementById('purchase-error');
  var buyBtn = document.getElementById('purchase-buy');

  var el = Dom.el;
  var commas = Dom.commas;
  function total() { return staged.reduce(function (sum, s) { return sum + (Number(s.unit.bv) || 0); }, 0); }
  function countOf(id) { return staged.filter(function (s) { return s.unit.unit === id; }).length; }

  // ---- Your cart
  function render() {
    listEl.textContent = '';
    headEl.textContent = staged.length ? 'Your cart (' + staged.length + ')' : 'Your cart';
    if (!staged.length) listEl.appendChild(el('p', 'muted purchase-empty', 'Your cart is empty. Click a unit above to add it.'));
    staged.forEach(function (s) {
      var row = MechRow.withoutPilot(s.unit);
      row.picked = true;
      row.actions = [{ icon: '×', label: 'Remove', title: 'Remove from your cart', onClick: function () { staged = staged.filter(function (x) { return x !== s; }); render(); paintResults(); } }];
      listEl.appendChild(MechRow.build(row));
    });
    // The warchest is in the dialog's head; down here is only what the cart costs, red when the warchest can't cover it.
    var cost = total(), short = cost > warchest;
    costEl.classList.toggle('short', short);
    var why = !staged.length ? 'Add at least one unit to your cart.' : short ? 'The warchest cannot cover that.' : '';
    buyBtn.disabled = !!why;
    buyBtn.title = why;
    countCost(cost);
    saveState();
  }

  // ---- The dialog in the URL (#purchase=...): its search and its cart.
  var restoring = false; // while a cart from the URL is being looked up, the URL is left as it is
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
  // The URL follows the open dialog; only closing it (clearState) takes it out, so drawing the cart while the dialog is closed (as the page loads) leaves a
  // fragment to restore alone.
  function saveState() {
    if (restoring || !dialog.open || !window.history || !history.replaceState || typeof search === 'undefined') return;
    var st = search.state();
    var hash = '#purchase=' + encode({ v: 1, q: st.query, all: st.all ? 1 : 0, cart: staged.map(function (s) { return s.unit.unit; }) });
    if (location.hash !== hash) history.replaceState(history.state, '', location.pathname + location.search + hash);
  }
  function clearState() {
    if (location.hash.indexOf('#purchase=') === 0 && window.history && history.replaceState) history.replaceState(history.state, '', location.pathname + location.search);
  }

  // The total and the Buy button count from the cost they show to the new one (with reduced motion they just change). An empty cart's button says Buy.
  var shownCost = 0, costRun = 0;
  var calm = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  function showCost(v) {
    shownCost = v;
    costEl.textContent = 'Total cost: ' + commas(v) + ' SP';
    buyBtn.textContent = staged.length ? 'Buy for ' + commas(v) + ' SP' : 'Buy';
  }
  function countCost(to) {
    var from = shownCost, run = ++costRun;
    if (from === to || calm || !dialog.open) { showCost(to); return; }
    var start = null;
    function frame(t) {
      if (run !== costRun) return; // a newer change took over, counting on from what this one showed
      if (start === null) start = t;
      var k = Math.min(1, (t - start) / 300);
      showCost(Math.round(from + (to - from) * (1 - Math.pow(1 - k, 3)))); // eases out, like the Deploy meter
      if (k < 1) requestAnimationFrame(frame);
    }
    requestAnimationFrame(frame);
  }

  // Adds a unit to the cart. Returns false when it is already there or the cart is full.
  function stage(unit) {
    if (countOf(unit.unit)) return false;
    if (staged.length >= LIMIT) { errEl.textContent = 'You can buy at most ' + LIMIT + ' units at a time.'; return false; }
    errEl.textContent = '';
    staged.push({ unit: unit });
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
    onPick: toggle,
    onUnpick: function (u) { if (countOf(u.unit)) toggle(u); }, // a right-click only ever takes a unit out of the cart
    onChange: saveState
  });
  document.getElementById('purchase-search-box').appendChild(search.element);
  function paintResults() { search.repaint(); }

  openBtn.addEventListener('click', function () { errEl.textContent = ''; render(); dialog.showModal(); saveState(); search.focus(); });
  document.getElementById('purchase-cancel').addEventListener('click', function () { dialog.close(); });
  dialog.addEventListener('close', function () { restoring = false; clearState(); });
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
      if (res.ok) { staged = []; clearState(); dialog.close(); location.reload(); return; } // cleared first: the close event comes after the reload starts
      return res.text().then(function (t) { errEl.textContent = t.trim() || 'Something went wrong.'; render(); });
    }, function () { errEl.textContent = 'Could not reach the server.'; render(); });
  });

  render();

  // A reload with the dialog in the URL opens it as it was: the search run again, and the cart's units looked up (one no longer known is left out).
  (function () {
    if (location.hash.indexOf('#purchase=') !== 0) return;
    var saved = decode(location.hash.slice(10));
    if (!saved || openBtn.disabled) { history.replaceState(history.state, '', location.pathname + location.search); return; }
    restoring = true;
    dialog.showModal();
    search.set(saved.q || '', !!saved.all);
    var ids = (Array.isArray(saved.cart) ? saved.cart : []).filter(function (id) { return typeof id === 'string'; }).slice(0, LIMIT);
    var done = function () { restoring = false; render(); search.focus(); };
    if (!ids.length) { done(); return; }
    fetch('/api/units?ids=' + encodeURIComponent(ids.join(',')), { credentials: 'same-origin' }).then(function (res) {
      return res.ok ? res.json() : { units: [] };
    }).then(function (data) {
      (data.units || []).forEach(function (u) { if (!countOf(u.unit)) staged.push({ unit: u }); });
      paintResults();
      done();
    }, done);
  })();
})();
