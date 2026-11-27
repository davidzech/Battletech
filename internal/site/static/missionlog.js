// The scenario page's mission log: mark the mission completed (or not). Only an available mission can be completed; the server checks that too.
// A completed mission's pilot lines show the pilot's hover card (pilotcard.js), the same as the campaign overview's, for each pilot still in the campaign.
document.addEventListener('DOMContentLoaded', function () { // once every deferred script, pilotcard.js among them, has run
  var list = document.getElementById('outcome-summary');
  if (!list || typeof PilotCard === 'undefined') return;
  var pilots = [];
  try { pilots = JSON.parse(list.dataset.pilots || '[]'); } catch (e) { pilots = []; }
  list.querySelectorAll('li[data-name]').forEach(function (li) {
    var p = pilots.find(function (q) { return q.name === li.dataset.name; }); // names are unique within a campaign
    var who = li.querySelector('.outcome-who');
    if (p && who) { PilotCard.attach(who, p); who.classList.add('has-card'); } // the portrait and name, which then look hoverable (style.css)
  });
});
(function () {
  var box = document.getElementById('mission-log');
  var btn = document.getElementById('mission-log-toggle');
  if (!box || !btn) return;
  var error = document.getElementById('mission-log-error');
  btn.addEventListener('click', function () {
    error.textContent = '';
    btn.disabled = true;
    var done = box.dataset.done === '1' ? '0' : '1';
    fetch('/api/campaign/scenario-complete', {
      method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, credentials: 'same-origin',
      body: 'campaign=' + encodeURIComponent(box.dataset.campaign) + '&scenario=' + encodeURIComponent(box.dataset.scenario) + '&done=' + done
    }).then(function (res) {
      if (res.ok) { location.reload(); return; }
      btn.disabled = false;
      return res.text().then(function (t) { error.textContent = t.trim() || 'Could not save the mission log.'; });
    }, function () { btn.disabled = false; error.textContent = 'Could not reach the server.'; });
  });
})();
