// The campaign page's History dialog: every change to the campaign, newest first, rendered by the server. The filter buttons show one kind of entry at a time.
// The Danger zone's Rollback button opens the same dialog with only the lines that can be rolled back to, and no filters.
// A line a checkpoint was taken right after (the start, each completed mission) has a Roll back button: after two confirmations it rolls the campaign back to
// that checkpoint (POST /api/campaign/rollback) and reloads the page.
(function () {
  var box = document.getElementById('history-dialog');
  var open = document.getElementById('history-open');
  if (!box || !open || typeof box.showModal !== 'function') return;
  var filters = box.querySelectorAll('.history-filter');
  var entries = box.querySelectorAll('.history-entry');
  var title = document.getElementById('history-title');
  var historyTitle = title.textContent;
  var filterBar = box.querySelector('.history-filters');
  // History shows every line, with the filter last picked. The Danger zone's Rollback opens the same dialog showing only the lines it can roll back to, without
  // the filters; closing it puts the History view back.
  function showAll() {
    var picked = box.querySelector('.history-filter.selected');
    var kind = picked ? picked.getAttribute('data-kind') : '';
    title.textContent = historyTitle;
    if (filterBar) filterBar.hidden = false;
    Array.prototype.forEach.call(entries, function (e) { e.hidden = kind !== '' && e.getAttribute('data-kind') !== kind; });
  }
  function showRollbacks() {
    title.textContent = 'Rollback points';
    if (filterBar) filterBar.hidden = true;
    Array.prototype.forEach.call(entries, function (e) { e.hidden = !e.querySelector('.history-rollback'); });
  }
  open.addEventListener('click', function () { showAll(); box.showModal(); });
  var rollbackOpen = document.getElementById('rollback-open');
  if (rollbackOpen) rollbackOpen.addEventListener('click', function () { showRollbacks(); box.showModal(); });
  document.getElementById('history-close').addEventListener('click', function () { box.close(); });
  box.addEventListener('click', function (e) { if (e.target === box) box.close(); });
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
      var message = 'The campaign goes back to just after "' + btn.dataset.label + '": pilots, SP, warchest, completed missions and the hangar are put back as they were, and everything after it is forgotten, here in the history too. This cannot be undone.';
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
