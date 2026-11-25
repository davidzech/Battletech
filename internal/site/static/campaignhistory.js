// The campaign page's History dialog: every change to the campaign, newest first, rendered by the server. The filter buttons show one kind of entry at a time.
// A line a checkpoint was taken right after (the start, each completed mission) has a Roll back button: after two confirmations it rolls the campaign back to
// that checkpoint (POST /api/campaign/rollback, as the Manage page's Rollback does) and reloads the page.
(function () {
  var box = document.getElementById('history-dialog');
  var open = document.getElementById('history-open');
  if (!box || !open || typeof box.showModal !== 'function') return;
  open.addEventListener('click', function () { box.showModal(); });
  document.getElementById('history-close').addEventListener('click', function () { box.close(); });
  box.addEventListener('click', function (e) { if (e.target === box) box.close(); });
  var filters = box.querySelectorAll('.history-filter');
  var entries = box.querySelectorAll('.history-entry');
  Array.prototype.forEach.call(filters, function (f) {
    f.addEventListener('click', function () {
      var kind = f.getAttribute('data-kind');
      Array.prototype.forEach.call(filters, function (o) {
        o.classList.toggle('selected', o === f);
        o.setAttribute('aria-pressed', o === f ? 'true' : 'false');
      });
      Array.prototype.forEach.call(entries, function (e) { e.hidden = kind !== '' && e.getAttribute('data-kind') !== kind; });
    });
  });
  var error = document.getElementById('history-error');
  Array.prototype.forEach.call(box.querySelectorAll('.history-rollback'), function (btn) {
    btn.addEventListener('click', function () {
      if (error) error.textContent = '';
      var post = function () {
        return fetch('/api/campaign/rollback', {
          method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, credentials: 'same-origin',
          body: 'campaign=' + encodeURIComponent(box.dataset.campaign) + '&checkpoint=' + encodeURIComponent(btn.dataset.checkpoint)
        }).then(function (res) {
          if (res.ok) return '';
          return res.text().then(function (t) { return t.trim() || 'Could not roll back.'; });
        }, function () { return 'Could not reach the server.'; });
      };
      var message = 'The campaign goes back to just after "' + btn.dataset.label + '": pilots, SP, warchest, completed missions and the hangar are put back as they were, and everything after it is forgotten, here in the history too. A battle in progress is given up. This cannot be undone.';
      if (!window.Confirm) {
        post().then(function (err) { if (err) { if (error) error.textContent = err; } else location.reload(); });
        return;
      }
      // Two confirmations: what a rollback does, then a plain "Are you sure?", which does the work.
      Confirm.ask({ title: 'Roll back the campaign?', message: message, confirmLabel: 'Roll back', danger: true })
        .then(function (yes) {
          if (!yes) return false;
          return Confirm.ask({
            title: 'Are you sure?',
            message: 'Everything after "' + btn.dataset.label + '" will be lost for good.',
            confirmLabel: 'Yes, roll back', busyLabel: 'Rolling back', danger: true, action: post
          });
        })
        .then(function (done) { if (done) location.reload(); });
    });
  });
})();
