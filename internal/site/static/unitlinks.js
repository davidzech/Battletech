// Hover cards for the links to units' MekBay pages in a scenario's text (the OpFor lists): the server gives each such link the unit's card as JSON in
// data-unit (unitlinks.go), and hovering or keyboard-focusing the link shows it in the site's floating card (MechRow.attachTip with UnitCard.build, so
// load mechrow.js and unitcard.js first). The link itself still opens MekBay.
(function () {
  function attach(root) {
    if (typeof MechRow === 'undefined') return;
    (root || document).querySelectorAll('a.unit-link[data-unit]').forEach(function (a) {
      var u;
      try { u = JSON.parse(a.getAttribute('data-unit')); } catch (e) { return; }
      // No delay: the card shows as soon as the link is hovered.
      MechRow.attachTip(a, { build: function () { return UnitCard.build(u); }, focusable: false });
    });
  }

  window.UnitLinks = { attach: attach };
  attach(document);
})();
