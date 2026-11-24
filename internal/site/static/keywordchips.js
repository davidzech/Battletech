// Earned keywords (the "keywords" template: the campaign overview and the Manage page): hovering or focusing a chip
// shows its description from Keywords.md and the mission that granted it, in the shared floating card. mechrow.js loads first.
(function () {
  if (typeof MechRow === 'undefined') return;
  document.querySelectorAll('.keyword-chip').forEach(function (k) {
    MechRow.attachTip(k, { title: k.textContent, cls: 'keyword-tip', text: k.dataset.text || 'No description yet.', lines: k.dataset.from ? [{ label: 'Granted by', value: k.dataset.from }] : [] });
  });
})();
