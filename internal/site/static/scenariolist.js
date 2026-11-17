// The campaign page's scenario list shows only the scenarios that are available or completed; the "Show all Scenarios" box reveals the locked ones too. The choice
// is remembered in this browser (best effort: storage may be unavailable).
(function () {
  var list = document.getElementById('scenario-cards');
  var box = document.getElementById('show-all-scenarios');
  if (!list || !box) return;
  var KEY = 'show-all-scenarios';
  function apply() { list.classList.toggle('show-all', box.checked); }
  try { box.checked = window.localStorage.getItem(KEY) === '1'; } catch (e) { /* no storage: start unchecked */ }
  apply();
  box.addEventListener('change', function () {
    apply();
    try { window.localStorage.setItem(KEY, box.checked ? '1' : '0'); } catch (e) { /* not remembered */ }
  });
})();
