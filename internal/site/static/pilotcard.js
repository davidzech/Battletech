// The pilot hover card, shared by the campaign overview (hovering a pilot in "Your force") and the Deploy popup (hovering a pilot in the tray or a slot): the
// portrait, name, rank and SP earned, the four stat boxes laid out like the Manage page's pilot card, and the Edge abilities they have chosen. It is an
// interactive card, so the pointer can move onto it, and an ability chip inside it shows that ability's card (abilitycard.js). mechrow.js (which owns the
// floating card) and abilitycard.js must load first. Text is inserted as text, never HTML.
//
//   PilotCard.build(p)         the card's content
//   PilotCard.attach(node, p)  shows it after half a second of hovering node (or at once on keyboard focus)
//   PilotCard.chip(p, sub)     the compact card (portrait, name, a line under it, and the GUN/PIL/EDGE/ABIL strip) the Deploy popup's tray and slots and
//                              the start wizard use; p needs only name, portraitUrl and the levels gunnery, piloting, edgeTokens, abilities
//   PilotCard.top(p, sub), PilotCard.strip(p), PilotCard.summary(p)  its parts, and the four levels as a sentence (for an aria-label)
//
// p is { name, rank, portraitUrl, earned, columns: [{ label, level, sp, hasNext, nextAt, nextLvl, percent }], learned: [{ id, name }] }, as Page.PilotsJSON sends it.
(function () {
  var ARROW = '<svg viewBox="0 0 16 10" width="16" height="10" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M1 5h13M10 1l4 4-4 4"/></svg>';

  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }

  // One of the Manage page's stat boxes: the level, the next level, SP progress and its bar (same classes, so the same look).
  function statBox(c) {
    var box = el('div', 'stat-box');
    box.appendChild(el('span', 'stat-label', c.label));
    var main = el('span', 'stat-main'), wrap = el('span', 'stat-valwrap');
    wrap.appendChild(el('span', 'stat-value', String(c.level)));
    if (c.hasNext) {
      var to = el('span', 'stat-to'), arrow = el('span', 'stat-arrow');
      arrow.innerHTML = ARROW; // a constant above, never data
      to.appendChild(arrow);
      to.appendChild(el('b', null, String(c.nextLvl)));
      wrap.appendChild(to);
    }
    main.appendChild(wrap);
    box.appendChild(main);
    box.appendChild(el('span', 'stat-next', c.hasNext ? c.sp + '/' + c.nextAt + ' SP' : 'max'));
    var bar = el('span', 'stat-bar'), fill = el('span');
    fill.style.width = (c.percent || 0) + '%';
    bar.appendChild(fill);
    box.appendChild(bar);
    return box;
  }

  function build(p) {
    var card = el('div', 'deploy-pilot-card');
    var head = el('div', 'dpc-head');
    var img = document.createElement('img');
    img.src = p.portraitUrl; img.alt = ''; img.width = 56; img.height = 56;
    head.appendChild(img);
    var who = el('span');
    who.appendChild(el('b', 'dpc-name', p.name));
    var sub = el('span', 'dpc-sub');
    if (p.rank) sub.appendChild(el('span', 'dpc-rank', p.rank));
    var sp = el('span', 'dpc-sp');
    sp.appendChild(document.createTextNode('SP earned '));
    sp.appendChild(el('b', null, String(p.earned || 0)));
    sub.appendChild(sp);
    who.appendChild(sub);
    head.appendChild(who);
    card.appendChild(head);
    var boxes = el('div', 'stat-boxes');
    (p.columns || []).forEach(function (c) { boxes.appendChild(statBox(c)); });
    card.appendChild(boxes);
    var skills = el('div', 'edge-skills');
    skills.appendChild(el('span', 'edge-skills-label', 'Edge abilities'));
    if ((p.learned || []).length) {
      p.learned.forEach(function (a) {
        var chip = el('span', 'skill-chip', a.name);
        chip.tabIndex = 0;
        if (typeof AbilityCard !== 'undefined') AbilityCard.attach(chip, a.id); // hovering one shows the ability card, on top of this card
        skills.appendChild(chip);
      });
    } else {
      skills.appendChild(el('span', 'dpc-none', 'None learned yet'));
    }
    card.appendChild(skills);
    return card;
  }

  function attach(node, p) {
    if (typeof MechRow !== 'undefined' && typeof MechRow.attachTip === 'function') {
      MechRow.attachTip(node, { focusable: false, delay: 500, interactive: true, build: function () { return build(p); } });
    }
  }

  // ---- The compact card: a pilot's four levels in a strip of mini stat boxes (a tiny label over a bold number), under the portrait, name and a line of text.
  var STATS = [['gunnery', 'GUN', 'Gunnery'], ['piloting', 'PIL', 'Piloting'], ['edgeTokens', 'EDGE', 'Edge tokens'], ['abilities', 'ABIL', 'Edge abilities']];
  function summary(p) { return STATS.map(function (s) { return s[2] + ' ' + (p[s[0]] || 0); }).join(', '); }
  function strip(p) {
    var box = el('span', 'deploy-stats');
    STATS.forEach(function (s) {
      var cell = el('span', 'deploy-stat');
      cell.appendChild(el('span', 'deploy-stat-label', s[1]));
      cell.appendChild(el('span', 'deploy-stat-value', String(p[s[0]] || 0)));
      box.appendChild(cell);
    });
    return box;
  }
  function top(p, sub) {
    var t = el('span', 'deploy-pilot-top');
    var img = document.createElement('img');
    img.className = 'deploy-portrait';
    img.src = p.portraitUrl; img.alt = ''; img.width = 32; img.height = 32;
    t.appendChild(img);
    var text = el('span', 'deploy-pilot-text');
    text.appendChild(el('b', null, p.name));
    text.appendChild(el('small', null, sub));
    t.appendChild(text);
    return t;
  }
  // The compact card on its own (not a drag source, not a button): a filled slot's look.
  function chip(p, sub) {
    var box = el('div', 'deploy-slot static filled');
    box.appendChild(top(p, sub));
    box.appendChild(strip(p));
    box.setAttribute('aria-label', p.name + ', ' + summary(p));
    return box;
  }

  window.PilotCard = { build: build, attach: attach, chip: chip, top: top, strip: strip, summary: summary };
})();
