// The Manage page's Overview: the default number of players, the same [1][2][3][4] picker as the Deploy wizard's, up to the campaign's maximum. It is where every Deploy
// starts (a single deploy can still change it). A count is offered only when the MekBay user IDs of all those players are saved (data-player-ids, booleans only).
(function () {
  var box = document.getElementById('overview');
  var pick = document.getElementById('overview-players-pick');
  if (!box || !pick) return;
  var max = Math.min(4, Math.max(1, parseInt(box.dataset.maxPlayers, 10) || 1));
  var min = Math.min(max, Math.max(1, parseInt(box.dataset.minPlayers, 10) || 1));
  var ids = [];
  try { ids = JSON.parse(box.dataset.playerIds || '[]'); } catch (e) { ids = []; }
  var players = Math.min(max, Math.max(min, parseInt(box.dataset.players, 10) || min));
  var error = document.getElementById('overview-error');
  var busy = false;

  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }
  // The first player in 1..n without a saved ID, or 0.
  function missing(n) {
    for (var i = 0; i < n; i++) if (!ids[i]) return i + 1;
    return 0;
  }

  function render() {
    pick.textContent = '';
    for (var n = min; n <= max; n++) {
      (function (n) {
        var b = el('button', 'deploy-count', String(n));
        b.type = 'button';
        b.setAttribute('aria-pressed', n === players ? 'true' : 'false');
        b.setAttribute('aria-label', n + (n === 1 ? ' player' : ' players'));
        var gap = missing(n);
        b.disabled = busy || (gap !== 0 && n !== players);
        if (gap) b.title = 'Player ' + gap + ' needs a MekBay user ID: add it under Account details.';
        b.addEventListener('click', function () { choose(n); });
        pick.appendChild(b);
      })(n);
    }
  }

  function choose(n) {
    if (n === players || busy) return;
    error.textContent = '';
    busy = true;
    render();
    fetch('/api/campaign/players', {
      method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, credentials: 'same-origin',
      body: 'campaign=' + encodeURIComponent(box.dataset.campaign) + '&players=' + n
    }).then(function (res) {
      busy = false;
      if (res.ok) { players = n; render(); return; }
      render();
      return res.text().then(function (t) { error.textContent = t.trim() || 'Could not save the player count.'; });
    }, function () { busy = false; render(); error.textContent = 'Could not reach the server.'; });
  }

  // The force's name, renamed in place: Rename turns it into a text box (Enter saves, Escape cancels).
  var nameBox = document.getElementById('force-name');
  var nameValue = document.getElementById('force-name-value');
  var nameEdit = document.getElementById('force-name-edit');
  if (nameBox && nameValue && nameEdit) {
    nameEdit.addEventListener('click', function () {
      var current = nameEdit.dataset.name || '';
      var input = el('input');
      input.type = 'text'; input.maxLength = 40; input.value = current; input.autocomplete = 'off'; input.spellcheck = false;
      input.setAttribute('aria-label', 'Force name');
      var save = el('button', 'mr-action', 'Save'), cancel = el('button', 'mr-action', 'Cancel');
      save.type = 'button'; cancel.type = 'button';
      nameValue.hidden = true; nameEdit.hidden = true;
      nameBox.appendChild(input); nameBox.appendChild(save); nameBox.appendChild(cancel);
      input.focus(); input.select();
      function close() { input.remove(); save.remove(); cancel.remove(); nameValue.hidden = false; nameEdit.hidden = false; }
      function commit() {
        var name = input.value.trim();
        if (!name) { error.textContent = 'Name your force.'; return; }
        if (name === current) { close(); return; }
        error.textContent = '';
        save.disabled = true;
        fetch('/api/campaign/force-name', {
          method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, credentials: 'same-origin',
          body: 'campaign=' + encodeURIComponent(box.dataset.campaign) + '&name=' + encodeURIComponent(name)
        }).then(function (res) {
          if (res.ok) { nameValue.textContent = name.replace(/\s+/g, ' '); nameEdit.dataset.name = name.replace(/\s+/g, ' '); close(); return; }
          save.disabled = false;
          return res.text().then(function (t) { error.textContent = t.trim() || 'Could not rename the force.'; });
        }, function () { save.disabled = false; error.textContent = 'Could not reach the server.'; });
      }
      save.addEventListener('click', commit);
      cancel.addEventListener('click', function () { error.textContent = ''; close(); });
      input.addEventListener('keydown', function (e) {
        if (e.key === 'Enter') { e.preventDefault(); commit(); } else if (e.key === 'Escape') { e.preventDefault(); error.textContent = ''; close(); }
      });
    });
  }

  // A campaign that is always played by the same number of players has nothing to choose: show the number.
  if (min === max) {
    pick.appendChild(el('b', 'hangar-stat-value', String(min)));
    document.getElementById('overview-note').hidden = true;
    return;
  }
  render();
})();
