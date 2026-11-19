// Hover cards for the links to units' MekBay pages in a scenario's text (the OpFor lists): the server gives each such link the unit's details as JSON
// in data-unit (unitlinks.go), and hovering or keyboard-focusing the link shows them in the site's floating card (MechRow.attachTip, so load
// mechrow.js first). The link itself still opens MekBay. Names are inserted as text, never HTML.
(function () {
  var el = Dom.el;
  var commas = Dom.commas;

  function card(u) {
    var box = el('div', 'unit-card');
    var head = el('div', 'tip-head');
    if (u.icon) {
      var img = document.createElement('img');
      img.src = u.icon; img.alt = ''; img.width = 42; img.height = 36;
      head.appendChild(img);
    }
    var names = el('div');
    names.appendChild(el('b', null, u.name));
    if (u.model) names.appendChild(el('span', 'tip-model', u.model));
    head.appendChild(names);
    box.appendChild(head);
    // Two columns: what the unit is on the left, how it is built on the right (a unit without construction figures, not a mech, has only the first).
    var cols = el('div', 'unit-card-cols');
    function column() { var dl = el('dl', 'tip-facts'); cols.appendChild(dl); return dl; }
    function fact(dl, k, v) { dl.appendChild(el('dt', null, k)); dl.appendChild(el('dd', null, v)); }
    var about = column();
    if (u.role) fact(about, 'Role', u.role);
    fact(about, 'Class', [u.class, u.omni ? 'OmniMech' : ''].filter(Boolean).join(' · '));
    fact(about, 'Tonnage', String(u.tons));
    if (u.tech) fact(about, 'Tech', u.tech);
    if (u.engine) fact(about, 'Engine', u.engine);
    if (u.armorType) fact(about, 'Armor type', u.armorType);
    if (u.structureType) fact(about, 'Structure type', u.structureType);
    fact(about, 'Base BV', commas(u.bv));
    // The pilot's skills are in the label: "BV at 3/4".
    if (u.gunnery || u.piloting) fact(about, 'BV at ' + u.gunnery + '/' + u.piloting, commas(u.skillBV));
    if (u.armor || u.structure) {
      var build = column();
      fact(build, 'Armor', String(u.armor));
      fact(build, 'Structure', String(u.structure));
      if (u.heat) fact(build, 'Heat', String(u.heat));
      if (u.dissipation) fact(build, 'Dissipation', String(u.dissipation));
      fact(build, 'Walk/Run/Jump', u.walk + ' / ' + u.run + ' / ' + (u.jump || 0));
    }
    box.appendChild(cols);
    return box;
  }

  function attach(root) {
    if (typeof MechRow === 'undefined') return;
    (root || document).querySelectorAll('a.unit-link[data-unit]').forEach(function (a) {
      var u;
      try { u = JSON.parse(a.getAttribute('data-unit')); } catch (e) { return; }
      // No delay: the card shows as soon as the link is hovered.
      MechRow.attachTip(a, { build: function () { return card(u); }, focusable: false });
    });
  }

  window.UnitLinks = { attach: attach, card: card };
  attach(document);
})();
