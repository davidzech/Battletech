// The Manage page's Rollback (Danger zone): roll the campaign back to one of its saved points (see checkpoint.go). It asks first, since everything after that point is forgotten.
(function () {
  var box = document.getElementById('rollback-dialog');
  var open = document.getElementById('rollback-open');
  if (!box || !open || typeof box.showModal !== 'function') return;
  var error = document.getElementById('rollback-error');
  open.addEventListener('click', function () { error.textContent = ''; box.showModal(); });
  document.getElementById('rollback-close').addEventListener('click', function () { box.close(); });
  box.addEventListener('click', function (e) { if (e.target === box) box.close(); });
  box.querySelectorAll('.cp-rollback').forEach(function (btn) {
    btn.addEventListener('click', function () {
      var label = btn.dataset.label;
      var ask = window.Confirm ? Confirm.ask({
        title: 'Roll back the campaign?',
        message: 'The campaign goes back to "' + label + '": pilots, SP, warchest, completed missions and the hangar are put back as they were, and everything after it is forgotten. A battle in progress is given up. This cannot be undone.',
        confirmLabel: 'Roll back', danger: true
      }) : Promise.resolve(true);
      ask.then(function (yes) {
        if (!yes) return;
        error.textContent = '';
        box.querySelectorAll('button').forEach(function (b) { b.disabled = true; });
        fetch('/api/campaign/rollback', {
          method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, credentials: 'same-origin',
          body: 'campaign=' + encodeURIComponent(box.dataset.campaign) + '&checkpoint=' + encodeURIComponent(btn.dataset.checkpoint)
        }).then(function (res) {
          if (res.ok) { location.reload(); return; }
          box.querySelectorAll('button').forEach(function (b) { b.disabled = false; });
          return res.text().then(function (t) { error.textContent = t.trim() || 'Could not roll back.'; });
        }, function () {
          box.querySelectorAll('button').forEach(function (b) { b.disabled = false; });
          error.textContent = 'Could not reach the server.';
        });
      });
    });
  });
})();
