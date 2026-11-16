// Scenario page: ask the server to create (or reset) this scenario's MekBay operation, then reload to show its link.
(function () {
  var box = document.getElementById('mekbay-op');
  if (!box) return;
  var error = document.getElementById('mekbay-error');

  function run(btn, rebuild) {
    if (rebuild && !window.confirm('Reset the OpFor? Any damage recorded on it in MekBay is lost.')) return;
    btn.disabled = true;
    error.textContent = '';
    var body = 'campaign=' + encodeURIComponent(box.dataset.campaign) + '&scenario=' + encodeURIComponent(box.dataset.scenario) + (rebuild ? '&rebuild=1' : '');
    fetch('/api/mekbay/operation', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: body,
      credentials: 'same-origin'
    }).then(function (res) {
      if (res.ok) { location.reload(); return; }
      btn.disabled = false;
      return res.text().then(function (t) { error.textContent = t.trim() || 'Could not create the operation.'; });
    }, function () { btn.disabled = false; error.textContent = 'Could not reach the server.'; });
  }

  var create = document.getElementById('mekbay-create');
  if (create) create.addEventListener('click', function () { run(create, false); });
  var rebuild = document.getElementById('mekbay-rebuild');
  if (rebuild) rebuild.addEventListener('click', function () { run(rebuild, true); });
})();
