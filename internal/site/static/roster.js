// Campaign overview: hovering a pilot in "Your force" shows the pilot card (pilotcard.js). The page carries the living named pilots as data-pilots on the
// overview and the Edge ability catalog as data-abilities; each roster row names its pilot in data-name. mechrow.js, abilitycard.js and pilotcard.js load first.
(function () {
  var root = document.getElementById('force-overview');
  if (!root || typeof PilotCard === 'undefined') return;
  var pilots = [];
  try { pilots = JSON.parse(root.dataset.pilots || '[]') || []; } catch (e) { pilots = []; }
  try { if (typeof AbilityCard !== 'undefined') AbilityCard.setCatalog(JSON.parse(root.dataset.abilities || '[]')); } catch (e) { /* the ability chips just won't explain themselves */ }
  root.querySelectorAll('.roster-pilot[data-name]').forEach(function (row) {
    var name = row.dataset.name.toLowerCase();
    for (var i = 0; i < pilots.length; i++) {
      if (pilots[i].name.toLowerCase() === name) { row.tabIndex = 0; PilotCard.attach(row, pilots[i]); break; } // focusable, so the card is there for the keyboard too
    }
  });
})();
