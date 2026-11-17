// Campaign overview: the one-line hangar summary (forcesummary.js, which must load first) read from MekBay through
// GET /api/campaign/force and painted from the page's cached copy first. Names are inserted as text, never HTML.
(function () {
  var box = document.getElementById('force-units');
  var root = document.getElementById('force-overview');
  if (!box || !root) return;

  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }

  function fail(message) {
    box.textContent = '';
    box.appendChild(el('p', 'form-error', message));
    var retry = el('button', null, 'Try again');
    retry.type = 'button';
    retry.addEventListener('click', load);
    box.appendChild(retry);
  }

  function render(data) {
    box.textContent = '';
    var card = ForceSummary.build(data);
    if (!card) { box.appendChild(el('p', 'muted', 'Your hangar has no units in it yet.')); return; }
    box.appendChild(card);
  }

  // The page embeds the hangar as it was last read, so it paints at once and the live read below only refreshes it. Only a run
  // that has never been read shows "Reading your hangar...".
  var shown = false;
  try {
    if (box.dataset.cached) { render(JSON.parse(box.dataset.cached)); shown = true; }
  } catch (e) { shown = false; }

  // A failed refresh keeps what is on screen and says so; with nothing on screen it is an error with a retry.
  function stale(message) {
    if (!shown) { fail(message); return; }
    var old = box.querySelector('.stale-note');
    if (old) old.remove();
    box.appendChild(el('p', 'muted stale-note', 'Showing your last known hangar. ' + message));
  }

  function load() {
    if (!shown) {
      box.textContent = '';
      box.appendChild(el('p', 'muted', 'Reading your hangar…'));
    }
    fetch('/api/campaign/force?campaign=' + encodeURIComponent(root.dataset.campaign), { credentials: 'same-origin' })
      .then(function (res) {
        if (res.ok) return res.json().then(function (data) { render(data); shown = true; });
        return res.text().then(function (t) { stale(t.trim() || 'Could not read your hangar.'); });
      }, function () { stale('Could not reach the server.'); });
  }
  load();
})();
