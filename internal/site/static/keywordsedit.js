// The Manage page's Edit keywords (Danger zone): tick the keywords the campaign holds and Save, which posts them all to
// POST /api/campaign/keywords and reloads the page. Cancel, Escape or a click on the backdrop changes nothing.
(function () {
  var box = document.getElementById('keywords-dialog');
  var open = document.getElementById('keywords-open');
  if (!box || !open || typeof box.showModal !== 'function') return;
  var form = document.getElementById('keywords-form');
  var error = document.getElementById('keywords-error');
  var save = document.getElementById('keywords-save');
  var boxes = Array.prototype.slice.call(form.querySelectorAll('input[name="keyword"]'));
  var saved = boxes.map(function (b) { return b.checked; });
  open.addEventListener('click', function () {
    boxes.forEach(function (b, i) { b.checked = saved[i]; }); // a cancelled edit doesn't linger
    error.textContent = '';
    save.disabled = false;
    box.showModal();
  });
  document.getElementById('keywords-cancel').addEventListener('click', function () { box.close(); });
  box.addEventListener('click', function (e) { if (e.target === box) box.close(); });
  form.addEventListener('submit', function (e) {
    e.preventDefault();
    var body = 'campaign=' + encodeURIComponent(box.dataset.campaign);
    boxes.forEach(function (b) { if (b.checked) body += '&keyword=' + encodeURIComponent(b.value); });
    save.disabled = true;
    error.textContent = '';
    fetch('/api/campaign/keywords', {
      method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, credentials: 'same-origin', body: body
    }).then(function (res) {
      if (res.ok) { location.reload(); return; }
      save.disabled = false;
      return res.text().then(function (t) { error.textContent = t.trim() || 'Could not save the keywords.'; });
    }, function () {
      save.disabled = false;
      error.textContent = 'Could not reach the server.';
    });
  });
})();
