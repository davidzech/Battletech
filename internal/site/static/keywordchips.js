// Keyword chips: hovering or focusing one shows its description from Keywords.md (and the mission that granted it, when known) in the shared floating card.
// The "keywords" template's chips (the campaign overview and the Manage page) are set up on load; other scripts build theirs with KeywordChips.chip (the Complete
// mission dialog's preview). mechrow.js loads first.
(function () {
  if (typeof MechRow === 'undefined') return;
  function attach(k) {
    MechRow.attachTip(k, { title: k.textContent, cls: 'keyword-tip', text: k.dataset.text || 'No description yet.', lines: k.dataset.from ? [{ label: 'Granted by', value: k.dataset.from }] : [] });
  }
  // chip(name, text, from) -> a keyword chip (an <li>, for a .keyword-list), with its hover card. Names go in as text, never HTML.
  function chip(name, text, from) {
    var k = document.createElement('li');
    k.className = 'keyword-chip';
    k.textContent = name;
    if (text) k.dataset.text = text;
    if (from) k.dataset.from = from;
    attach(k);
    return k;
  }
  document.querySelectorAll('.keyword-chip').forEach(attach);
  window.KeywordChips = { chip: chip };
})();
