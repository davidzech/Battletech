// A unit's hover card, the same everywhere: a scenario's unit links (unitlinks.js) and the overview's unit icons (forcesummary.js). The server sends the
// card's data (unitCard in unitlinks.go); this builds it. Names are inserted as text, never HTML.
//
//   UnitCard.build(card, extra) -> HTMLElement
//     card:  the unit's catalog card (name, model, icon, role, class, ..., weapons), with gunnery/piloting/skillBV when skills were written beside it
//     extra: optional { status: HTMLElement placed under the name (e.g. the condition pills), pilot: the pilot's name }
//
//   UnitCard.unknown(name) -> the card of a unit the catalog doesn't know
//   UnitCard.forUnit(view) -> the card of a hangar unit (a unit view from the server, with `card`), its condition pills under the name
(function () {
  var el = Dom.el;
  var commas = Dom.commas;

  // MekBay's weapon types, with its colours and icons (static/weapon-*.svg, see weapon-icons-NOTICE.txt).
  var WEAPON_TYPES = [
    { key: 'ballistic', label: 'Ballistic', color: '#9482B4' },
    { key: 'energy', label: 'Energy', color: '#6082F6' },
    { key: 'missile', label: 'Missile', color: '#86C86E' },
  ];

  function head(name, model, icon, bv) {
    var h = el('div', 'tip-head');
    if (icon) {
      var img = document.createElement('img');
      img.src = icon; img.alt = ''; img.width = 42; img.height = 36;
      h.appendChild(img);
    }
    var names = el('div');
    names.appendChild(el('b', null, name));
    if (model) names.appendChild(el('span', 'tip-model', model));
    h.appendChild(names);
    // The unit's BV, top right beside its name: "BV: 2,663", or at the pilot's skills when they aren't the standard 4/5, "BV (3/4): 3,515".
    if (bv) h.appendChild(el('span', 'tip-bv', bv));
    return h;
  }

  // The weapon type bar: how many ballistic, energy and missile weapons the mech mounts, each an icon on its colour and a count (dimmed at zero).
  function weaponBar(w) {
    var bar = el('div', 'weapon-types');
    WEAPON_TYPES.forEach(function (t) {
      var n = w[t.key] || 0;
      var chip = el('span', 'weapon-type' + (n ? '' : ' none'));
      chip.style.setProperty('--wt', t.color);
      chip.title = n + ' ' + t.label.toLowerCase() + (n === 1 ? ' weapon' : ' weapons');
      chip.setAttribute('aria-label', chip.title);
      var ico = el('span', 'weapon-type-icon');
      var img = document.createElement('img');
      img.src = '/static/weapon-' + t.key + '.svg'; img.alt = '';
      ico.appendChild(img);
      chip.appendChild(ico);
      chip.appendChild(el('span', 'weapon-type-count', String(n)));
      bar.appendChild(chip);
    });
    return bar;
  }

  function build(u, extra) {
    extra = extra || {};
    var box = el('div', 'unit-card');
    var skilled = (u.gunnery || u.piloting) && !(u.gunnery === 4 && u.piloting === 5);
    box.appendChild(head(u.name, u.model, u.icon, skilled ? 'BV (' + u.gunnery + '/' + u.piloting + '): ' + commas(u.skillBV) : 'BV: ' + commas(u.bv)));
    if (extra.status) box.appendChild(extra.status);
    // Two columns: what the unit is on the left, how it is built on the right (a unit without construction figures, not a mech, has only the first).
    var cols = el('div', 'unit-card-cols');
    function column() { var c = el('div', 'unit-card-col'); var dl = el('dl', 'tip-facts'); c.appendChild(dl); cols.appendChild(c); return dl; }
    function fact(dl, k, v) { dl.appendChild(el('dt', null, k)); dl.appendChild(el('dd', null, v)); }
    var about = column();
    if (u.role) fact(about, 'Role', u.role);
    fact(about, 'Class', [u.class, u.omni ? 'OmniMech' : ''].filter(Boolean).join(' · '));
    fact(about, 'Tonnage', String(u.tons));
    if (u.tech) fact(about, 'Tech', u.tech);
    if (u.engine) fact(about, 'Engine', u.engine);
    if (u.armorType) fact(about, 'Armor type', u.armorType);
    if (u.structureType) fact(about, 'Structure type', u.structureType);
    // The heading carries the BV (at the pilot's skills when they were given); the base BV stays in the list, which also keeps the two columns level.
    fact(about, 'Base BV', commas(u.bv));
    if (extra.pilot) fact(about, 'Pilot', extra.pilot);
    if (u.armor || u.structure) {
      var build = column();
      fact(build, 'Armor', String(u.armor));
      fact(build, 'Structure', String(u.structure));
      if (u.heat) fact(build, 'Heat', String(u.heat));
      if (u.dissipation) fact(build, 'Dissipation', String(u.dissipation));
      fact(build, 'Walk/Run/Jump', u.walk + ' / ' + u.run + ' / ' + (u.jump || 0));
      if (u.firepower) fact(build, 'Firepower', String(u.firepower));
      // How far its weapons reach: the longest of them, and where most of its damage reaches (each weapon's range weighted by its damage, MekBay's
      // "weighted" range), always, so the two columns have the same number of rows; e.g. a Mongrel Prime's ER large laser reaches 25 hexes but most of its damage only 15.
      if (u.maxRange) {
        fact(build, 'Max range', u.maxRange + ' hexes');
        fact(build, 'Effective range', (u.range || u.maxRange) + ' hexes');
      }
      if (u.weapons) build.parentNode.appendChild(weaponBar(u.weapons));
    }
    box.appendChild(cols);
    return box;
  }

  function unknown(name) {
    var box = el('div', 'unit-card');
    box.appendChild(head(name, '', ''));
    var dl = el('dl', 'tip-facts');
    dl.appendChild(el('dt', null, 'Catalog'));
    dl.appendChild(el('dd', null, 'Unknown unit, so its BV is unknown'));
    box.appendChild(dl);
    return box;
  }

  // ---- A hangar unit's card (a unit view from the server, which carries its catalog card as `card`): the unit's state under its name as the same
  // pills the Hangar rows use (the worst condition, and a separate Ammo expended pill; needs mechrow.js), and its pilot if it has one.
  function status(u) {
    var cond = !u.unknown && u.condition && typeof MechRow !== 'undefined' ? MechRow.CONDITIONS[u.condition] : null;
    if (!cond && !u.ammoExpended) {
      if (u.condition === 'truly') return el('span', 'tag bad', 'Truly destroyed');
      if (u.destroyed) return el('span', 'tag bad', 'Destroyed');
      if (u.damaged) return el('span', 'tag', 'Damaged');
      return null;
    }
    var pills = el('div', 'tip-pills');
    if (cond) pills.appendChild(el('span', 'mr-chip ' + cond.cls, cond.label));
    if (u.ammoExpended) pills.appendChild(el('span', 'mr-chip ammo', 'Ammo expended'));
    return pills;
  }
  function forUnit(u) {
    if (u.unknown || !u.card) return unknown(u.name);
    return build(u.card, { status: status(u), pilot: u.pilot });
  }

  window.UnitCard = { build: build, unknown: unknown, forUnit: forUnit };
})();
