// Campaign page: start the campaign by giving it a MekBay hangar link (or change the link of a started one).
(function () {
  var box = document.getElementById('campaign-run');
  var dialog = document.getElementById('run-dialog');
  var openBtn = document.getElementById('run-open');
  if (!box || !dialog || !openBtn || typeof dialog.showModal !== 'function') return;

  var form = document.getElementById('run-form');
  var input = document.getElementById('run-hangar');
  var error = document.getElementById('run-error');
  var UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

  // Mirrors the server's check (the server is still the authority).
  function problem(raw) {
    var u, text = 'Paste a MekBay hangar link, like https://mekbay.com/?instance=…';
    try { u = new URL(raw.trim()); } catch (e) { return text; }
    var host = u.hostname.toLowerCase();
    if (u.protocol !== 'https:' || (host !== 'mekbay.com' && host !== 'www.mekbay.com')) return text;
    if (!UUID.test((u.searchParams.get('instance') || '').toLowerCase())) return 'That MekBay link has no valid instance id (?instance=…).';
    return '';
  }

  openBtn.addEventListener('click', function () { error.textContent = ''; dialog.showModal(); input.focus(); });
  document.getElementById('run-cancel').addEventListener('click', function () { dialog.close(); });
  input.addEventListener('input', function () { error.textContent = ''; });

  form.addEventListener('submit', function (e) {
    e.preventDefault();
    var bad = problem(input.value);
    if (bad) { error.textContent = bad; return; }
    fetch('/api/campaign/start', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: 'campaign=' + encodeURIComponent(box.dataset.campaign) + '&hangar=' + encodeURIComponent(input.value.trim()),
      credentials: 'same-origin'
    }).then(function (res) {
      if (res.ok) { location.reload(); return; }
      return res.text().then(function (t) { error.textContent = t.trim() || 'Could not save the hangar link.'; });
    }, function () { error.textContent = 'Could not reach the server.'; });
  });
})();
