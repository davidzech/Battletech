// A mech row in the style of MekBay's: a role band down the left edge, the unit's icon, the model above a bold name,
// and chips for BV, tonnage and the pilot's Gunnery/Piloting. Everywhere the site lists mechs builds its rows here, so
// they look the same. The name is the Clan name when the design has one (the server decides; this only displays).
//
//   MechRow.build({ name, model, role, icon, tons, bv, gunnery, piloting, pilot,
//                   unknown, destroyed, damaged, notMech, externalUrl }) -> HTMLElement
//
// rowUrl makes the whole row open a link; externalUrl (a https://mekbay.com/ link) adds an "opens in a new tab" button at the right of the row. It is opt-in: the deploy popup's rows are
// checkbox labels and have none.
(function () {
  // Small icons for the tonnage and pilot chips, drawn for this site.
  var WEIGHT = '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M7.2 9h9.6l2.4 11H4.8z"/><circle cx="12" cy="5.8" r="2.6"/></svg>';
  // The standard "opens in a new tab" icon: a box with an arrow leaving its top right corner.
  var HELMET = '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3.5 13.5a8.5 8.5 0 0 1 17 0V20h-5v-3.5h-7V20h-5z"/></svg>';

  var EXTERNAL = '<svg viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><path d="M15 3h6v6"/><path d="M10 14 21 3"/></svg>';

  // The pill for each condition: its label and colour class (see .mr-chip.cond-* in style.css). "ok" has none.
  var CONDITIONS = {
    armor: { label: 'Armor damage', cls: 'cond-armor' },
    structure: { label: 'Structure damage', cls: 'cond-structure' },
    crippled: { label: 'Crippled', cls: 'cond-crippled' },
    destroyed: { label: 'Destroyed', cls: 'cond-destroyed' },
    truly: { label: 'Truly destroyed', cls: 'cond-truly' }
  };

  // ---- The status pills' hover card: the same floating card as the unit icons and Edge ability chips, instead of a plain browser tooltip. One element
  // for the page, filled for the pill under the pointer (or focused with the keyboard). Inside an open dialog it lives in that dialog, so it isn't
  // hidden beneath it.
  var LOCS = {
    HD: 'Head', CT: 'Center Torso', LT: 'Left Torso', RT: 'Right Torso', LA: 'Left Arm', RA: 'Right Arm', LL: 'Left Leg', RL: 'Right Leg',
    FLL: 'Front Left Leg', FRL: 'Front Right Leg', RLL: 'Rear Left Leg', RRL: 'Rear Right Leg'
  };
  function locNames(list) { return list.split(', ').map(function (c) { return LOCS[c] || c; }).join(', '); }
  function sentence(s) { return s ? s.charAt(0).toUpperCase() + s.slice(1) + (/[.!?]$/.test(s) ? '' : '.') : ''; }
  // The server's short note ("Armor damaged: HD, CT") as a sentence a player can read.
  function conditionText(note) {
    var m = /^Armor damaged: (.+)$/.exec(note || '');
    if (m) return 'Armor lost in: ' + locNames(m[1]) + '. Its internal structure is untouched.';
    m = /^Internal structure damaged: (.+)$/.exec(note || '');
    if (m) return 'Internal structure lost in: ' + locNames(m[1]) + '.';
    m = /^(?:Crippled|Truly destroyed): (.+)$/.exec(note || '');
    if (m) return sentence(m[1]);
    if (note === 'Destroyed') return 'Destroyed in play. It can still be repaired between sorties.';
    return note || '';
  }

  var tip = null;
  function ensureTip() {
    if (tip) return tip;
    tip = el('div', 'unit-tip pill-tip');
    tip.id = 'pill-tip';
    tip.setAttribute('role', 'tooltip');
    tip.hidden = true;
    return tip;
  }
  // content: { title, cls, text, lines: [{ label, value }] }
  function attachTip(pill, content) {
    pill.tabIndex = 0;
    pill.setAttribute('aria-describedby', 'pill-tip');
    function show() {
      var t = ensureTip();
      var host = pill.closest('dialog[open]') || document.body;
      if (t.parentNode !== host) host.appendChild(t);
      t.textContent = '';
      t.appendChild(el('b', 'pill-tip-title ' + (content.cls || ''), content.title));
      if (content.text) t.appendChild(el('p', 'pill-tip-text', content.text));
      if (content.lines && content.lines.length) {
        var dl = el('dl', 'tip-facts');
        content.lines.forEach(function (l) { dl.appendChild(el('dt', null, l.label)); dl.appendChild(el('dd', null, l.value)); });
        t.appendChild(dl);
      }
      t.hidden = false;
      var r = pill.getBoundingClientRect(), w = t.offsetWidth, h = t.offsetHeight, gap = 6;
      var left = Math.min(Math.max(8, r.left), window.innerWidth - w - 8);
      var top = r.bottom + gap;
      if (top + h > window.innerHeight - 8 && r.top - gap - h >= 8) top = r.top - gap - h;
      t.style.left = left + 'px';
      t.style.top = top + 'px';
    }
    function hide() { if (tip) tip.hidden = true; }
    pill.addEventListener('mouseenter', show);
    pill.addEventListener('mouseleave', hide);
    pill.addEventListener('focus', show);
    pill.addEventListener('blur', hide);
  }

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
    var nameLine = el('span', 'mr-nameline');
    nameLine.appendChild(el('span', 'mr-name', u.name));
    main.appendChild(nameLine);

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
      attachTip(cp, { title: cond.label, cls: cond.cls, text: conditionText(u.conditionNote) });
      chips.appendChild(cp);
      if (u.ammoExpended) {
        var ap = chip('ammo', 'Ammo expended');
        var lines = (u.ammoBreakdown || []).map(function (a) {
          return { label: a.name, value: a.tons + (a.tons === 1 ? ' ton' : ' tons') + ' \u00b7 ' + a.used + (a.capacity ? ' of ' + a.capacity : '') + ' shots used' };
        });
        if (u.rearmCost) lines.push({ label: 'Rearm', value: u.rearmCost + ' SP' });
        attachTip(ap, { title: 'Ammo expended', cls: 'cond-ammo', text: 'Some ammunition has been used. Rearm it between sorties.', lines: lines });
        chips.appendChild(ap);
      }
    } else if (u.destroyed) chips.appendChild(chip('bad', 'Destroyed'));
    else if (u.unknown) chips.appendChild(chip('bad', 'Unknown unit'));
    else if (u.notMech) chips.appendChild(chip('warn', 'Not a mech'));
    else if (u.damaged) chips.appendChild(chip('warn', 'Damaged'));
    main.appendChild(chips);

    row.appendChild(main);

    // A small gray external-link icon after the name opens the unit's generic page (its General tab on MekBay) in a new tab.
    if (u.externalUrl && /^https:\/\/mekbay\.com\//.test(u.externalUrl)) {
      var link = document.createElement('a');
      link.className = 'mr-ext';
      link.href = u.externalUrl;
      link.target = '_blank';
      link.rel = 'noopener noreferrer';
      link.title = 'About this unit on MekBay';
      link.setAttribute('aria-label', 'About ' + (u.name + ' ' + (u.model || '')).trim() + ' on MekBay (opens in a new tab)');
      link.innerHTML = EXTERNAL; // a constant defined above, never data
      nameLine.appendChild(link);
    }
    // actions: [{ label, title, disabled, onClick }] adds buttons at the right end of the row (the Hangar's Rearm and Repair).
    if ((u.actions && u.actions.length) || u.badge) {
      var box = el('span', 'mr-actions');
      if (u.badge) box.appendChild(el('span', 'mr-badge', u.badge));
      (u.actions || []).forEach(function (a) {
        var b = el('button', 'mr-action' + (a.icon ? ' mr-action-icon' : ''), a.icon || a.label);
        b.type = 'button';
        if (a.title) b.title = a.title;
        b.disabled = !!a.disabled;
        b.setAttribute('aria-label', a.label + ' ' + (u.name + ' ' + (u.model || '')).trim());
        b.addEventListener('click', function (e) { e.stopPropagation(); if (!b.disabled && a.onClick) a.onClick(); });
        box.appendChild(b);
      });
      row.classList.add('has-actions');
      row.appendChild(box);
    }
    // onPick makes the whole row a button, for choosing it from a list: a click or Enter or Space calls it (the (i) link and the action buttons keep their
    // own meaning), and picked tints it like a chosen unit in the deploy popup. badge is a small label at its right end ("Selected", "x2").
    if (u.picked) row.classList.add('picked');
    if (u.onPick) {
      row.classList.add('has-pick');
      row.tabIndex = 0;
      row.setAttribute('role', 'button');
      row.setAttribute('aria-pressed', u.picked ? 'true' : 'false');
      row.addEventListener('click', function (e) { if (!e.target.closest('.mr-ext, .mr-actions button')) u.onPick(); });
      row.addEventListener('keydown', function (e) {
        if ((e.key === 'Enter' || e.key === ' ') && e.target === row) { e.preventDefault(); u.onPick(); }
      });
    }
    // rowUrl (a https://mekbay.com/ link) makes the whole row a link opened in a new tab, e.g. the player's hangar with this unit selected.
    // The (i) after the name keeps its own target.
    if (u.rowUrl && /^https:\/\/mekbay\.com\//.test(u.rowUrl)) {
      row.classList.add('has-row-link');
      row.tabIndex = 0;
      row.setAttribute('role', 'link');
      row.title = 'Open in your hangar on MekBay';
      var go = function () { window.open(u.rowUrl, '_blank', 'noopener,noreferrer'); };
      row.addEventListener('click', function (e) { if (!e.target.closest('.mr-ext, .mr-actions')) go(); });
      row.addEventListener('keydown', function (e) {
        if ((e.key === 'Enter' || e.key === ' ') && e.target === row) { e.preventDefault(); go(); }
      });
    }
    return row;
  }

  window.MechRow = { build: build };
})();
