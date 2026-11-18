// A search over MekBay's unit list (GET /api/units/search), shared by the Manage page's Purchase dialog and the start wizard's "Build your force". It builds its
// own search box, the campaign's faction/era checkbox (when the campaign names both) and the result list of mech rows (mechrow.js, which must load first). A
// result row is clicked (or Enter or Space on it) to pick it; what picking means is the caller's: the Purchase cart toggles it, the wizard adds another of it.
//
//   var search = UnitSearch.create({
//     campaign: 'refusalwar',
//     faction: 'Clan Wolf', era: 'Clan Invasion',   // optional: both, to offer the "only what this faction fields" box (ticked)
//     placeholder: 'Search units, e.g. Timber Wolf',
//     idPrefix: 'purchase',                           // optional: ids for its parts (purchase-search, -clear, -available, -results)
//     decorate: function (row, unit) { ... },        // optional: mark a result (row.picked, row.badge) from the caller's state
//     onPick: function (unit) { ... }                 // a result was clicked
//   });
//   container.appendChild(search.element);
//   search.repaint();   // after the caller's state changes, so the badges follow
//   search.focus();
//
// A unit is { unit: <MekBay unit ID, e.g. BMMadCat_H>, name, model, icon, role, class, tons, bv, mekbayUrl }. Replies to older text are ignored, and typing
// waits a moment before searching. Names go in as text, never HTML.
(function () {
  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }
  var ICON = '<svg class="search-icon" viewBox="0 0 24 24" aria-hidden="true"><circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/></svg>';
  var ids = 0;

  // A unit view without the pilot fields: skills belong to assigning pilots, not to a unit being picked.
  function plain(u) {
    var row = {};
    Object.keys(u).forEach(function (k) { if (k !== 'gunnery' && k !== 'piloting' && k !== 'pilot') row[k] = u[k]; });
    return row;
  }

  function create(opts) {
    var n = ++ids;
    var element = el('div', 'unit-search');
    var field = el('div', 'search-field');
    field.innerHTML = ICON;
    var input = document.createElement('input');
    input.type = 'search'; input.autocomplete = 'off';
    input.placeholder = opts.placeholder || 'Search units, e.g. Timber Wolf';
    input.setAttribute('aria-label', 'Search units');
    var prefix = opts.idPrefix || 'unit-search-' + n;
    input.id = prefix + '-search';
    var results = el('div', 'purchase-results');
    results.id = prefix + '-results';
    results.setAttribute('aria-live', 'polite');
    input.setAttribute('aria-describedby', results.id);
    var clear = el('button', 'search-clear', '×');
    clear.type = 'button'; clear.hidden = true; clear.id = prefix + '-clear';
    clear.setAttribute('aria-label', 'Clear search');
    field.appendChild(input);
    field.appendChild(clear);
    element.appendChild(field);
    var available = null;
    if (opts.faction && opts.era) {
      var label = el('label', 'themed-check');
      available = document.createElement('input');
      available.type = 'checkbox'; available.checked = true; available.id = prefix + '-available';
      label.appendChild(available);
      label.appendChild(el('span', null, 'Only units ' + opts.faction + ' fields in the ' + opts.era + ' era'));
      element.appendChild(label);
    }
    element.appendChild(results);

    var last = null, lastQuery = '', seq = 0, timer = null;

    function paint() {
      if (!last) return;
      var units = last.units || [];
      var scroll = results.querySelector('.purchase-found');
      var keep = scroll ? scroll.scrollTop : 0;
      results.textContent = '';
      if (!units.length) {
        var none = 'No mechs match “' + lastQuery + '”.';
        if (last.filter && last.filter.active) none += ' Untick the box above to search every unit.';
        results.appendChild(el('p', 'muted', none));
        return;
      }
      var list = el('div', 'mech-list purchase-found');
      units.forEach(function (u) {
        var row = plain(u);
        if (u.mekbayUrl) row.externalUrl = u.mekbayUrl;
        if (opts.decorate) opts.decorate(row, u);
        row.onPick = function () { opts.onPick(u); };
        list.appendChild(MechRow.build(row));
      });
      results.appendChild(list);
      list.scrollTop = keep;
      if (last.limited) results.appendChild(el('p', 'muted', 'Showing the first ' + units.length + ' of ' + last.total + ' matches. Type more to narrow them down.'));
    }

    function run() {
      var q = input.value.trim();
      var mine = ++seq;
      if (!q) { last = null; results.textContent = ''; return; } // nothing typed: nothing to show (the placeholder says what to do)
      var url = '/api/units/search?campaign=' + encodeURIComponent(opts.campaign) + '&q=' + encodeURIComponent(q) + (available && !available.checked ? '&all=1' : '');
      fetch(url, { credentials: 'same-origin' }).then(function (res) {
        if (!res.ok) throw new Error('search failed');
        return res.json();
      }).then(function (data) {
        if (mine !== seq) return; // a reply for text that has since changed
        last = data; lastQuery = q; paint();
      }, function () {
        if (mine !== seq) return;
        last = null;
        results.textContent = '';
        results.appendChild(el('p', 'form-error', 'Could not search the unit list. Try again.'));
      });
    }
    function queue() { clearTimeout(timer); timer = setTimeout(run, 250); }
    input.addEventListener('input', function () { clear.hidden = !input.value; queue(); });
    // Enter searches at once; it never submits the form around the search.
    input.addEventListener('keydown', function (e) { if (e.key === 'Enter') { e.preventDefault(); clearTimeout(timer); run(); } });
    clear.addEventListener('click', function () { input.value = ''; clear.hidden = true; clearTimeout(timer); run(); input.focus(); });
    if (available) available.addEventListener('change', run);

    return { element: element, repaint: paint, focus: function () { input.focus(); }, input: input };
  }

  window.UnitSearch = { create: create, plain: plain };
})();
