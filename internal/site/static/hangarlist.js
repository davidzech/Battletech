// Manage page: the campaign's MekBay hangar as two big labelled numbers, UNITS and BASE BV (forcesummary.js), above the full list of its mechs, as the same mech
// rows the deploy popup uses (mechrow.js). Both scripts must load first. Like the overview, the page embeds the hangar as it was last read, so the list paints at once and
// the live read (GET /api/campaign/force) only refreshes it; only a run that has never been read shows "Reading your hangar...". Names,
// roles and models go in as text, never HTML.
(function () {
  var section = document.getElementById('hangar-units');
  var list = document.getElementById('hangar-units-list');
  var summary = document.getElementById('hangar-units-summary');
  if (!section || !list || typeof MechRow === 'undefined') return;

  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }
  // The count and total BV as big yellow numbers with labels; the rows underneath carry the per-unit detail, so no icons here.
  function render(data) {
    var units = data.units || [];
    list.textContent = '';
    summary.textContent = '';
    var card = typeof ForceSummary !== 'undefined' ? ForceSummary.stats(data) : null;
    if (card) summary.appendChild(card);
    if (!units.length) {
      list.appendChild(el('p', 'muted', 'Your hangar has no units in it yet. Add some in MekBay.'));
      return;
    }
    // The hangar's pilot skills are a matter of assigning pilots, not something the mech itself has, so the rows leave them out.
    units.forEach(function (u) {
      var row = {};
      Object.keys(u).forEach(function (k) { if (k !== 'gunnery' && k !== 'piloting' && k !== 'pilot') row[k] = u[k]; });
      if (u.mekbayUrl) row.externalUrl = u.mekbayUrl; // the unit's own page on MekBay, opened in a new tab
      list.appendChild(MechRow.build(row));
    });
  }

  var shown = false;
  try {
    if (section.dataset.cached) { render(JSON.parse(section.dataset.cached)); shown = true; }
  } catch (e) { shown = false; }

  // A failed refresh keeps what is on screen and says so; with nothing on screen it is an error with a retry.
  function stale(message) {
    if (!shown) {
      list.textContent = '';
      list.appendChild(el('p', 'form-error', message));
      var retry = el('button', null, 'Try again');
      retry.type = 'button';
      retry.addEventListener('click', load);
      list.appendChild(retry);
      return;
    }
    var old = section.querySelector('.stale-note');
    if (old) old.remove();
    section.appendChild(el('p', 'muted stale-note', 'Showing your last known hangar. ' + message));
  }

  function load() {
    if (!shown) {
      list.textContent = '';
      list.appendChild(el('p', 'muted', 'Reading your hangar…'));
    }
    fetch('/api/campaign/force?campaign=' + encodeURIComponent(section.dataset.campaign), { credentials: 'same-origin' })
      .then(function (res) {
        if (res.ok) return res.json().then(function (data) { var old = section.querySelector('.stale-note'); if (old) old.remove(); render(data); shown = true; });
        return res.text().then(function (t) { stale(t.trim() || 'Could not read your hangar.'); });
      }, function () { stale('Could not reach the server.'); });
  }
  load();
})();
