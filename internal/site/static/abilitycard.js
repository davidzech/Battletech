// The Edge ability card, shared by the Manage page (the picker, and the hover card on a pilot's learned abilities) and the Deploy popup (the abilities
// listed in a pilot's hover card). Text is inserted as text, never HTML.
//
//   AbilityCard.setCatalog(list)     the twelve abilities as { id, name, category, text } (from the page's data-abilities)
//   AbilityCard.byId(id)             one of them, or null
//   AbilityCard.fill(card, a)        fills a .skill-card element: the name, a category pill and the rule with its trigger in bold; null empties it
//   AbilityCard.select(id, list)     a <select> with this id for choosing one of the listed abilities: a blank first option, then the abilities by category
//   AbilityCard.attach(chip, id)     shows the card in a floating panel under the chip on hover and keyboard focus (above it when there is no room below)
//
// The floating panel is a popover where the browser has them, so it renders in the top layer: above a dialog or another popover (the Deploy popup's pilot
// card) and not clipped by a dialog's cut corner. Otherwise it is a fixed panel on the page.
(function () {
  var catalog = [];

  var el = Dom.el;

  function setCatalog(list) { catalog = list || []; }
  function byId(id) {
    for (var i = 0; i < catalog.length; i++) if (catalog[i].id === id) return catalog[i];
    return null;
  }

  // A rule starts with its trigger, which the book prints in bold: everything up to the first ":" that isn't inside parentheses (Protector's
  // trigger has a "(TW: 1 hex)" aside in it). Returns [trigger including its colon, the rest]; the trigger is "" if there is no colon.
  function splitTrigger(text) {
    var depth = 0;
    for (var i = 0; i < text.length; i++) {
      var ch = text.charAt(i);
      if (ch === '(') depth++;
      else if (ch === ')') depth = Math.max(0, depth - 1);
      else if (ch === ':' && depth === 0) return [text.slice(0, i + 1), text.slice(i + 1)];
    }
    return ['', text];
  }

  // Appends text to a parent, with each "(TW: ...)" aside (the Total Warfare version of a rule) in its own dimmed span.
  function appendRule(parent, text) {
    text.split(/(\(TW:[^)]*\))/).forEach(function (part, i) {
      if (!part) return;
      if (i % 2) parent.appendChild(el('span', 'tw', part)); else parent.appendChild(document.createTextNode(part));
    });
  }

  function fill(card, a) {
    card.textContent = '';
    card.className = 'skill-card' + (a ? ' cat-' + a.category.toLowerCase().replace(/[^a-z]+/g, '-') : ' empty');
    if (!a) { card.appendChild(el('p', 'skill-rule', 'Choose an ability to read what it does.')); return; }
    var head = el('div', 'skill-card-head');
    head.appendChild(el('span', 'skill-name', a.name));
    head.appendChild(el('span', 'skill-cat', a.category));
    card.appendChild(head);
    var rule = el('p', 'skill-rule');
    var parts = splitTrigger(a.text);
    if (parts[0]) {
      var trigger = el('b', 'skill-trigger');
      appendRule(trigger, parts[0]);
      rule.appendChild(trigger);
    }
    appendRule(rule, parts[1]);
    card.appendChild(rule);
  }

  // The categories in the order the pickers list them; one the list doesn't know comes after them.
  var CATEGORY_ORDER = ['Attack', 'Indirect Fire', 'Defense', 'Heat', 'Melee', 'Movement'];

  function select(id, list) {
    var s = document.createElement('select');
    s.id = id;
    var blank = el('option', null, 'Choose an ability\u2026');
    blank.value = '';
    s.appendChild(blank);
    var categories = CATEGORY_ORDER.slice();
    list.forEach(function (a) { if (categories.indexOf(a.category) < 0) categories.push(a.category); });
    categories.forEach(function (cat) {
      var inCat = list.filter(function (a) { return a.category === cat; });
      if (!inCat.length) return;
      var group = document.createElement('optgroup');
      group.label = cat;
      inCat.forEach(function (a) { var o = el('option', null, a.name); o.value = a.id; group.appendChild(o); });
      s.appendChild(group);
    });
    return s;
  }

  // ---- The floating panel: one for the page.
  var canPopover = typeof HTMLElement !== 'undefined' && typeof HTMLElement.prototype.showPopover === 'function';
  var tip = null, tipCard = null;
  function ensureTip() {
    if (tip) return tip;
    tip = el('div', 'skill-tip');
    tip.id = 'skill-tip';
    tip.setAttribute('role', 'tooltip');
    if (canPopover) tip.setAttribute('popover', 'manual');
    tip.hidden = true;
    tipCard = el('div', 'skill-card');
    tip.appendChild(tipCard);
    document.body.appendChild(tip);
    window.addEventListener('scroll', hide, true); // a fixed card would drift away from its chip
    return tip;
  }
  function show(chip, id) {
    var a = byId(id);
    if (!a) return;
    var t = ensureTip();
    // Inside the open dialog when the chip is in one (as a popover it is still on top and not clipped), like the pilot card it is shown from.
    var host = chip.closest('dialog[open]') || document.body;
    if (t.parentNode !== host) host.appendChild(t);
    fill(tipCard, a);
    t.hidden = false;
    // Shown (and so put in the top layer) after any popover already open, it sits on top of it.
    if (canPopover) { if (t.matches(':popover-open')) t.hidePopover(); t.showPopover(); }
    var r = chip.getBoundingClientRect(), w = t.offsetWidth, h = t.offsetHeight, gap = 6;
    var left = Math.min(Math.max(8, r.left), window.innerWidth - w - 8);
    var top = r.bottom + gap;
    if (top + h > window.innerHeight - 8 && r.top - gap - h >= 8) top = r.top - gap - h;
    t.style.left = left + 'px';
    t.style.top = top + 'px';
  }
  function hide() {
    if (!tip) return;
    tip.hidden = true;
    if (canPopover && tip.matches(':popover-open')) tip.hidePopover();
  }

  function attach(chip, id) {
    ensureTip();
    chip.dataset.ability = id;
    chip.setAttribute('aria-describedby', 'skill-tip');
    chip.addEventListener('mouseenter', function () { show(chip, id); });
    chip.addEventListener('mouseleave', hide);
    chip.addEventListener('focus', function () { show(chip, id); });
    chip.addEventListener('blur', hide);
  }

  window.AbilityCard = { setCatalog: setCatalog, byId: byId, fill: fill, select: select, attach: attach };
})();
