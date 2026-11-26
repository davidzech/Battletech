// The variant picker: choosing another variant of an omnimech, shared by the Manage page's Reconfigure dialog (hangarlist.js, which charges SP) and the Deploy
// wizard's Configure step (mekbay.js, free for a scenario's provided mechs). Load it after mechrow.js and unitcard.js.
//
//   VariantPicker.fill(select, unit, opts)   the unit's current variant first (value ''), then its other variants (unit.swapOptions), each marked like a
//                                            stock: green ▲ for more BV, red ▼ for less. opts: onlyAvailable (leave out the variants the campaign's faction
//                                            doesn't field), price(o) (text after the BV, e.g. '50 SP'), current (the label of the current variant's line),
//                                            value (the variant to choose, kept when still listed).
//   VariantPicker.option(unit, value)        the swap option a select's value names, or undefined for the current variant.
//   VariantPicker.flow(box, unit, o, opts)   the unit as its full mech row, then, when a variant o is chosen, an arrow with the BV change (and opts.price(o) at its
//                                            right) and the variant's own row. Each row shows its unit card on hover or focus, right-aligned with the row.
var VariantPicker = (function () {
  var el = Dom.el, commas = Dom.commas;

  function dir(o, u) { return o.bv > u.bv ? 'up' : o.bv < u.bv ? 'down' : 'same'; }

  function option(u, value) {
    return (u.swapOptions || []).filter(function (o) { return o.unit === value; })[0];
  }

  function fill(select, u, opts) {
    opts = opts || {};
    select.textContent = '';
    select.classList.add('variant-select');
    var here = document.createElement('option');
    here.value = '';
    here.className = 'swap-same';
    here.textContent = '● ' + (u.model || 'This variant') + ' — ' + commas(u.bv) + ' BV — ' + (opts.current || 'current');
    select.appendChild(here);
    (u.swapOptions || []).forEach(function (o) {
      if (opts.onlyAvailable && o.available === false) return;
      var opt = document.createElement('option');
      var d = dir(o, u);
      opt.value = o.unit;
      opt.className = 'swap-' + d;
      opt.textContent = (d === 'up' ? '▲ ' : d === 'down' ? '▼ ' : '') + (o.model || o.name) + ' — ' + commas(o.bv) + ' BV' + (opts.price ? ' — ' + opts.price(o) : '');
      select.appendChild(opt);
    });
    select.value = opts.value || '';
    if (select.selectedIndex < 0) select.selectedIndex = 0;
    mark(select);
  }

  // The closed box takes the colour of the chosen option.
  function mark(select) {
    var chosen = select.options[select.selectedIndex];
    select.classList.remove('swap-up', 'swap-down', 'swap-same');
    if (chosen && chosen.className) select.classList.add(chosen.className);
  }

  // A mech row with its unit card: under the row, right-aligned with it, the row lit while it is open. Coming back off a status pill (which has a card of its
  // own) brings the unit's card back.
  function row(v, card) {
    var view = { name: v.name, model: v.model, role: v.role, icon: v.icon, class: v.class, tons: v.tons, bv: v.bv };
    if (v.mekbayUrl) view.externalUrl = v.mekbayUrl;
    var built = MechRow.build(view);
    if (card && typeof UnitCard !== 'undefined') {
      MechRow.attachTip(built, { align: 'right', build: card });
      built.addEventListener('mouseover', function (e) {
        var own = e.target.closest && e.target.closest('[aria-describedby]');
        if ((!own || own === built) && !built.classList.contains('tip-open')) built.dispatchEvent(new Event('mouseenter'));
      });
    }
    return built;
  }

  function flow(box, u, o, opts) {
    opts = opts || {};
    box.textContent = '';
    box.classList.add('swap-unit');
    box.appendChild(el('div', 'swap-label', o ? 'From' : 'Current variant'));
    box.appendChild(row(u, function () { return UnitCard.forUnit(u); }));
    if (!o) return;
    var d = dir(o, u), diff = o.bv - u.bv;
    var f = el('div', 'swap-flow swap-' + d);
    f.appendChild(el('span', 'swap-arrow', '↓'));
    f.appendChild(el('span', null, commas(u.bv) + ' → ' + commas(o.bv) + ' BV'));
    if (diff) f.appendChild(el('b', null, (diff > 0 ? '+' : '−') + commas(Math.abs(diff)) + ' BV'));
    if (opts.price) f.appendChild(el('span', 'swap-flow-cost', opts.price(o)));
    box.appendChild(f);
    box.appendChild(el('div', 'swap-label', 'To'));
    box.appendChild(row(o, function () { return o.card ? UnitCard.build(o.card) : UnitCard.unknown(o.name); }));
  }

  return { fill: fill, option: option, flow: flow, mark: mark };
})();
