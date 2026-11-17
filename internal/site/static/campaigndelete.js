// Campaign page: the Danger zone's Delete button opens a confirmation popup; confirming deletes the campaign's progress
// (POST /api/campaign/delete) and reloads, which shows the campaign as not started.
(function () {
  var zone = document.getElementById('danger-zone');
  var dialog = document.getElementById('delete-dialog');
  var open = document.getElementById('delete-open');
  if (!zone || !dialog || !open || typeof dialog.showModal !== 'function') return;
  var form = document.getElementById('delete-form');
  var confirmBtn = document.getElementById('delete-confirm');
  var error = document.getElementById('delete-error');

  open.addEventListener('click', function () { error.textContent = ''; confirmBtn.disabled = false; dialog.showModal(); });
  document.getElementById('delete-cancel').addEventListener('click', function () { dialog.close(); });
  dialog.addEventListener('click', function (e) { if (e.target === dialog) dialog.close(); });

  form.addEventListener('submit', function (e) {
    e.preventDefault();
    confirmBtn.disabled = true;
    error.textContent = '';
    fetch('/api/campaign/delete', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: 'campaign=' + encodeURIComponent(zone.dataset.campaign),
      credentials: 'same-origin'
    }).then(function (res) {
      if (res.ok) { location.reload(); return; }
      confirmBtn.disabled = false;
      return res.text().then(function (t) { error.textContent = t.trim() || 'Could not delete the campaign.'; });
    }, function () {
      confirmBtn.disabled = false;
      error.textContent = 'Could not reach the server.';
    });
  });
})();
