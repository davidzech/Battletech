// The Reconfigure dialog: choosing another variant for an omnimech, shared by the Manage page's hangar (hangarlist.js, which reconfigures at once and charges
// the warchest) and the Deploy wizard's review (mekbay.js, which only proposes a variant: Start battle pays for it, and a scenario's own mechs are free). Load
// it after variantpicker.js. It builds its own <dialog> the first time it opens, on top of any dialog already open.
//
//   SwapDialog.open(unit, opts)
//     opts.title         the heading (default 'Reconfigure <name>')
//     opts.availability  the label of a box that limits the list to the variants the campaign's faction fields (ticked each time it opens); '' for no box
//     opts.onlyAvailable without a box: list only those variants (a scenario's own mechs)
//     opts.price(o)      the price shown after a variant, e.g. '38 SP' or 'Free'
//     opts.cost(o)       the SP a variant costs, for the breakdown and the cost line; with opts.warchest unset there is no cost line (nothing is paid)
//     opts.warchest      the SP there is to pay with
//     opts.value         the variant chosen to begin with ('' for the current one)
//     opts.current       the label of the current variant's line (default 'current')
//     opts.apply         the button's label (default 'Reconfigure')
//     opts.submit(o)     called with the chosen variant (undefined for the current one) when the button is pressed; returns a Promise: resolved, the dialog closes;
//                        rejected with a message, the message is shown. The button is enabled when the choice differs from opts.value.
var SwapDialog = (function () {
  var el = Dom.el, commas = Dom.commas;
  var dlg = null, select, availRow, availBox, availText, sub, breakdown, costText, err, applyBtn;
  var unit = null, opts = {}, busy = false;

  function build() {
    dlg = el('dialog', 'wizard');
    dlg.id = 'swap-dialog';
    dlg.setAttribute('aria-labelledby', 'swap-title');
    var form = el('form');
    form.id = 'swap-form';
    form.setAttribute('autocomplete', 'off');
    var h = el('h2', null, 'Reconfigure');
    h.id = 'swap-title';
    form.appendChild(h);
    var label = el('label', null, 'Variant');
    label.htmlFor = 'swap-select';
    form.appendChild(label);
    select = document.createElement('select');
    select.id = 'swap-select';
    form.appendChild(select);
    availRow = el('label', 'themed-check swap-available');
    availBox = document.createElement('input');
    availBox.type = 'checkbox';
    availBox.id = 'swap-available';
    availText = el('span');
    availRow.appendChild(availBox);
    availRow.appendChild(availText);
    form.appendChild(availRow);
    sub = el('div', 'swap-unit');
    sub.id = 'swap-sub';
    form.appendChild(sub);
    breakdown = el('dl', 'confirm-items');
    breakdown.id = 'swap-breakdown';
    breakdown.hidden = true;
    form.appendChild(breakdown);
    costText = el('p', 'repair-cost');
    costText.id = 'swap-cost';
    form.appendChild(costText);
    err = el('p', 'form-error');
    err.id = 'swap-error';
    err.setAttribute('role', 'alert');
    form.appendChild(err);
    var actions = el('div', 'form-actions');
    applyBtn = el('button', 'button', 'Reconfigure');
    applyBtn.type = 'submit';
    applyBtn.id = 'swap-apply';
    var cancel = el('button', null, 'Cancel');
    cancel.type = 'button';
    cancel.id = 'swap-cancel';
    actions.appendChild(applyBtn);
    actions.appendChild(cancel);
    form.appendChild(actions);
    dlg.appendChild(form);
    document.body.appendChild(dlg);

    select.addEventListener('change', show);
    availBox.addEventListener('change', function () { fill(select.value); show(); });
    cancel.addEventListener('click', function () { dlg.close(); });
    dlg.addEventListener('click', function (e) { if (e.target === dlg) dlg.close(); });
    // Escape closes only this dialog, not one under it (the Deploy wizard).
    dlg.addEventListener('cancel', function (e) { e.stopPropagation(); });
    form.addEventListener('submit', function (e) {
      e.preventDefault();
      if (applyBtn.disabled || busy) return;
      busy = true;
      err.textContent = '';
      applyBtn.disabled = true;
      Promise.resolve(opts.submit(chosen())).then(function () {
        busy = false;
        dlg.close();
      }, function (e) {
        busy = false;
        err.textContent = (e && e.message) || String(e || 'Something went wrong.');
        show();
      });
    });
  }

  function chosen() { return VariantPicker.option(unit, select.value); }

  // The current variant comes first and is chosen to begin with (or opts.value, when one was already chosen). With the box ticked, only the variants the
  // campaign's faction fields in its era are listed; unticking it lists them all. A variant still listed stays chosen when the box changes.
  function fill(value) {
    VariantPicker.fill(select, unit, {
      onlyAvailable: opts.availability ? availBox.checked : !!opts.onlyAvailable,
      price: opts.price,
      current: opts.current,
      value: value
    });
  }

  // The flow from the current variant to the chosen one, the price's breakdown, the cost line and the button.
  function show() {
    var o = chosen(), u = unit;
    VariantPicker.mark(select);
    VariantPicker.flow(sub, u, o, { price: opts.price });
    var paid = typeof opts.warchest === 'number' && typeof opts.cost === 'function';
    breakdown.textContent = '';
    breakdown.hidden = !(o && paid);
    if (o && paid) {
      // Half the tonnage, plus half the BV gained (nothing for a variant worth less).
      var gained = Math.max(0, o.bv - u.bv);
      [['50% of tonnage', u.tons + 't · ' + Math.ceil(u.tons / 2) + ' SP'],
       ['50% of BV gained', (gained ? '+' + commas(gained) + ' BV' : 'no BV gained') + ' · ' + Math.floor(gained / 2) + ' SP'],
       ['Cost', opts.cost(o) + ' SP']].forEach(function (r) { breakdown.appendChild(el('dt', null, r[0])); breakdown.appendChild(el('dd', null, r[1])); });
    }
    var short = false;
    if (!o) {
      costText.textContent = (opts.value ? 'Keep its current variant.' : 'Choose another variant to reconfigure to.');
    } else if (paid) {
      var line = Dom.costLine(opts.cost(o), opts.warchest);
      costText.textContent = line.text;
      short = line.short;
    } else {
      costText.textContent = '';
    }
    costText.classList.toggle('short', short);
    applyBtn.disabled = busy || short || select.value === (opts.value || '');
  }

  function open(u, o) {
    if (!dlg) build();
    unit = u;
    opts = o || {};
    busy = false;
    document.getElementById('swap-title').textContent = opts.title || 'Reconfigure ' + u.name;
    applyBtn.textContent = opts.apply || 'Reconfigure';
    availRow.hidden = !opts.availability;
    availText.textContent = opts.availability || '';
    availBox.checked = true;
    err.textContent = '';
    fill(opts.value || '');
    // A chosen variant the faction doesn't field is still shown: list them all.
    if (opts.availability && opts.value && select.value !== opts.value) { availBox.checked = false; fill(opts.value); }
    show();
    if (!dlg.open) dlg.showModal();
  }

  return { open: open };
})();
