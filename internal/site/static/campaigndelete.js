// Campaign page: the Danger zone's Delete button opens a confirmation popup; confirming asks once more ("Are you sure?"), and confirming that deletes the
// campaign's progress (POST /api/campaign/delete) and reloads, which shows the campaign as not started.
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

  var post = function () {
    return fetch('/api/campaign/delete', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: 'campaign=' + encodeURIComponent(zone.dataset.campaign),
      credentials: 'same-origin'
    }).then(function (res) {
      if (res.ok) return '';
      return res.text().then(function (t) { return t.trim() || 'Could not delete the campaign.'; });
    }, function () { return 'Could not reach the server.'; });
  };

  // Two confirmations: this popup, then a plain "Are you sure?" (Confirm.ask), which does the delete and stays open while it runs.
  form.addEventListener('submit', function (e) {
    e.preventDefault();
    error.textContent = '';
    dialog.close();
    Confirm.ask({
      title: 'Are you sure?',
      message: 'All of this campaign\'s progress will be lost for good.',
      confirmLabel: 'Yes, delete it', busyLabel: 'Deleting', danger: true, action: post
    }).then(function (done) { if (done) location.reload(); });
  });
})();
