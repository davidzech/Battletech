// A mech row in the style of MekBay's: a role band down the left edge, the unit's icon, the model above a bold name,
// and chips for BV, tonnage and the pilot's Gunnery/Piloting. Everywhere the site lists mechs builds its rows here, so
// they look the same. The name is the Clan name when the design has one (the server decides; this only displays).
//
//   MechRow.build({ name, model, role, icon, tons, bv, gunnery, piloting, pilot,
//                   unknown, destroyed, damaged, notMech, externalUrl }) -> HTMLElement
//
// externalUrl (a https://mekbay.com/ link) adds an "opens in a new tab" button at the right of the row. It is opt-in: the deploy popup's rows are
// checkbox labels and have none.
(function () {
  // Small icons for the tonnage and pilot chips, drawn for this site.
  var WEIGHT = '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M7.2 9h9.6l2.4 11H4.8z"/><circle cx="12" cy="5.8" r="2.6"/></svg>';
  // The standard "opens in a new tab" icon: a box with an arrow leaving its top right corner.
  var EXTERNAL = '<svg viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><path d="M15 3h6v6"/><path d="M10 14 21 3"/></svg>';
  var HELMET = '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3.5 13.5a8.5 8.5 0 0 1 17 0V20h-5v-3.5h-7V20h-5z"/></svg>';

  // The pill for each condition: its label and colour class (see .mr-chip.cond-* in style.css). "ok" has none.
  var CONDITIONS = {
    armor: { label: 'Armor damage', cls: 'cond-armor' },
    structure: { label: 'Structure damage', cls: 'cond-structure' },
    crippled: { label: 'Crippled', cls: 'cond-crippled' },
    destroyed: { label: 'Destroyed', cls: 'cond-destroyed' }
  };

  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }

  function chip(cls, text, svg) {
    var c = el('span', 'mr-chip' + (cls ? ' ' + cls : ''));
    c.appendChild(document.createTextNode(text));
    if (svg) {
      var s = el('span', 'mr-ico');
      s.innerHTML = svg; // a constant defined above, never data
      c.appendChild(s);
    }
    return c;
  }

  function build(u) {
    var row = el('div', 'mech-row' + (u.destroyed ? ' destroyed' : ''));

    var role = el('span', 'mr-role');
    var label = u.unknown ? 'Unknown' : (u.role || 'Mech');
    var roleText = el('span', null, label);
    // The label runs down the band on one line and never wraps. The longest real role ("Missile Boat", 12
    // characters) fits at the stylesheet's size; anything longer is scaled down in proportion.
    if (label.length > 12) roleText.style.fontSize = (0.54 * 12 / label.length).toFixed(3) + 'rem';
    role.appendChild(roleText);
    row.appendChild(role);

    var img = document.createElement('img');
    img.className = 'mr-icon';
    img.alt = '';
    img.loading = 'lazy';
    img.decoding = 'async';
    if (u.icon) img.src = u.icon;
    row.appendChild(img);

    var main = el('div', 'mr-main');
    main.appendChild(el('span', 'mr-model', u.model || ' ')); // the nbsp keeps rows without a model the same height
    main.appendChild(el('span', 'mr-name', u.name));

    var chips = el('span', 'mr-chips');
    chips.appendChild(chip('', u.unknown ? 'BV: ?' : 'BV: ' + Number(u.bv).toLocaleString('en-US')));
    if (u.tons) chips.appendChild(chip('', String(u.tons), WEIGHT));
    if (u.gunnery !== undefined && u.piloting !== undefined) {
      var pilot = chip('pilot', u.gunnery + '/' + u.piloting, HELMET);
      if (u.pilot) pilot.title = u.pilot;
      chips.appendChild(pilot);
    }
    // A unit with a worked-out condition (the hangar's) shows its worst state, one of armor / structure / crippled / destroyed, and a separate
    // pill when its ammo is spent; the note says why. Rows without one (the deploy popup) keep the plain Destroyed / Damaged tags.
    var cond = u.condition && !u.unknown && !u.notMech ? CONDITIONS[u.condition] : null;
    if (cond) {
      var cp = chip(cond.cls, cond.label);
      if (u.conditionNote) cp.title = u.conditionNote;
      chips.appendChild(cp);
      if (u.ammoExpended) chips.appendChild(chip('ammo', 'Ammo expended'));
    } else if (u.destroyed) chips.appendChild(chip('bad', 'Destroyed'));
    else if (u.unknown) chips.appendChild(chip('bad', 'Unknown unit'));
    else if (u.notMech) chips.appendChild(chip('warn', 'Not a mech'));
    else if (u.damaged) chips.appendChild(chip('warn', 'Damaged'));
    main.appendChild(chips);

    row.appendChild(main);

    if (u.externalUrl && /^https:\/\/mekbay\.com\//.test(u.externalUrl)) {
      var link = document.createElement('a');
      link.className = 'mr-ext';
      link.href = u.externalUrl;
      link.target = '_blank';
      link.rel = 'noopener noreferrer';
      link.title = 'Open this unit on MekBay';
      link.setAttribute('aria-label', 'Open ' + (u.name + ' ' + (u.model || '')).trim() + ' on MekBay (opens in a new tab)');
      link.innerHTML = EXTERNAL; // a constant defined above, never data
      row.classList.add('has-link');
      row.appendChild(link);
    }
    return row;
  }

  window.MechRow = { build: build };
})();
