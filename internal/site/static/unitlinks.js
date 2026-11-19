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
    var facts = el('dl', 'tip-facts');
    function fact(k, v) { facts.appendChild(el('dt', null, k)); facts.appendChild(el('dd', null, v)); }
    if (u.role) fact('Role', u.role);
    fact('Class', [u.class, u.omni ? 'OmniMech' : ''].filter(Boolean).join(' · '));
    fact('Tonnage', String(u.tons));
    if (u.tech) fact('Tech', u.tech);
    if (u.engine) fact('Engine', u.engine);
    fact('Base BV', commas(u.bv));
    if (u.gunnery || u.piloting) {
      fact('Pilot', u.gunnery + '/' + u.piloting);
      fact('BV at ' + u.gunnery + '/' + u.piloting, commas(u.skillBV));
    }
    box.appendChild(facts);
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
