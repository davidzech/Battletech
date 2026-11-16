// Campaign page: name a new named pilot, award SP to a pilot (allocated across their columns), or mark one as fallen.
// The server validates everything; these handlers only post the form and reload to show the result.
(function () {
  var box = document.getElementById('pilots');
  if (!box) return;
  var error = document.getElementById('pilot-error');

  function send(url, body, button) {
    error.textContent = '';
    if (button) button.disabled = true;
    return fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: 'campaign=' + encodeURIComponent(box.dataset.campaign) + '&' + body,
      credentials: 'same-origin'
    }).then(function (res) {
      if (res.ok) { location.reload(); return; }
      if (button) button.disabled = false;
      return res.text().then(function (t) { error.textContent = t.trim() || 'Something went wrong.'; });
    }, function () {
      if (button) button.disabled = false;
      error.textContent = 'Could not reach the server.';
    });
  }

  var add = document.getElementById('pilot-add');
  if (add) {
    add.addEventListener('submit', function (e) {
      e.preventDefault();
      var name = add.elements.name.value.trim();
      if (!name) { error.textContent = 'Give your pilot a name.'; return; }
      send('/api/pilots', 'name=' + encodeURIComponent(name), add.querySelector('button'));
    });
  }

  box.querySelectorAll('li.pilot').forEach(function (li) {
    var id = li.dataset.pilot;
    var award = li.querySelector('.award-form');
    if (award) {
      award.addEventListener('submit', function (e) {
        e.preventDefault();
        var parts = ['pilot=' + encodeURIComponent(id)];
        ['gunnery', 'piloting', 'edge_tokens', 'edge_abilities'].forEach(function (f) {
          parts.push(f + '=' + encodeURIComponent(award.elements[f].value));
        });
        send('/api/pilots/sp', parts.join('&'), award.querySelector('button'));
      });
    }
    var portraitBtn = li.querySelector('button.portrait-btn');
    if (portraitBtn) {
      portraitBtn.addEventListener('click', function () {
        if (typeof PortraitPicker === 'undefined') return;
        var taken = [];
        box.querySelectorAll('li.pilot').forEach(function (o) { if (o !== li && o.dataset.portrait) taken.push(o.dataset.portrait); });
        PortraitPicker.open({
          portraits: JSON.parse(box.dataset.portraits || '[]'), selected: li.dataset.portrait, taken: taken,
          onPick: function (file) { send('/api/pilots/portrait', 'pilot=' + encodeURIComponent(id) + '&portrait=' + encodeURIComponent(file)); }
        });
      });
    }
    var fallen = li.querySelector('.pilot-fallen');
    if (fallen) {
      fallen.addEventListener('click', function () {
        var name = li.querySelector('h3').textContent;
        if (!window.confirm('Mark ' + name + ' as fallen? They are permanently removed from the campaign.')) return;
        send('/api/pilots/fallen', 'pilot=' + encodeURIComponent(id), fallen);
      });
    }
  });
})();
