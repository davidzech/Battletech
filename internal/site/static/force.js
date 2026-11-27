// Campaign overview: the one-line hangar summary (forcesummary.js, which must load first), painted from the hangar the page embeds (only a page that could not
// load it reads GET /api/campaign/force). Names are inserted as text, never HTML.
(function () {
  var box = document.getElementById('force-units');
  var root = document.getElementById('force-overview');
  if (!box || !root) return;

  var el = Dom.el;

  function fail(message) {
    box.textContent = '';
    box.appendChild(el('p', 'form-error', message));
    var retry = el('button', null, 'Try again');
    retry.type = 'button';
    retry.addEventListener('click', load);
    box.appendChild(retry);
  }

  function render(data) {
    MechRow.hideTip(); // the icons are about to be replaced, so a card open on one would point at nothing
    box.textContent = '';
    var card = ForceSummary.build(data);
    if (!card) { box.appendChild(el('p', 'muted', 'Your hangar has no units in it yet.')); return; }
    box.appendChild(card);
  }

  function load() {
    box.textContent = '';
    box.appendChild(el('p', 'muted', 'Reading your hangar…'));
    fetch('/api/campaign/force?campaign=' + encodeURIComponent(root.dataset.campaign), { credentials: 'same-origin' })
      .then(function (res) {
        if (res.ok) return res.json().then(render);
        return res.text().then(function (t) { fail(t.trim() || 'Could not read your hangar.'); });
      }, function () { fail('Could not reach the server.'); });
  }

  var embedded = null;
  try { embedded = box.dataset.hangar ? JSON.parse(box.dataset.hangar) : null; } catch (e) { embedded = null; }
  if (embedded) render(embedded);
  else load();
})();
