// Manage page: the campaign's MekBay hangar as two big labelled numbers, UNITS and BASE BV (forcesummary.js), above the full list of its mechs, as the same mech
// rows the deploy popup uses (mechrow.js). Both scripts must load first. Like the overview, the page embeds the hangar as it was last read, so the list paints at once and
// the live read (GET /api/campaign/force) only refreshes it; only a run that has never been read shows "Reading your hangar...". Names,
// roles and models go in as text, never HTML. Each mech row also has Rearm and Repair buttons (costs come from the server, which charges the
// warchest and changes the unit in the site's own copy of the hangar (Sell pays the warchest); both need a copy the site owns). Confirm (confirm.js) must load first.
(function () {
  var section = document.getElementById('hangar-units');
  var list = document.getElementById('hangar-units-list');
  var summary = document.getElementById('hangar-units-summary');
  if (!section || !list || typeof MechRow === 'undefined') return;

  var el = Dom.el;
  // The count and total BV as big yellow numbers with labels; the rows underneath carry the per-unit detail, so no icons here.
  var hangarLink = section.dataset.hangarUrl || ''; // the player's MekBay hangar, which a row click opens with that unit selected

  var canModify = section.dataset.canModify === '1';
  var warchest = parseInt(section.dataset.warchest, 10) || 0;
  var NO_UID = 'This hangar is from before the site kept its own copy. Delete the campaign and start it again to import your hangar.';
  var commas = Dom.commas;
  function fullName(u) { return (u.name + (u.model ? ' ' + u.model : '')).trim(); }

  // Posts a service (repair or rearm) for one unit; resolves with the server's answer or a message.
  function service(fields) {
    var body = 'campaign=' + encodeURIComponent(section.dataset.campaign);
    Object.keys(fields).forEach(function (k) { body += '&' + k + '=' + encodeURIComponent(fields[k]); });
    return fetch('/api/hangar/service', {
      method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body: body, credentials: 'same-origin'
    }).then(function (res) {
      if (res.ok) return { ok: true };
      return res.text().then(function (t) { return { ok: false, message: t.trim() || 'Something went wrong.' }; });
    }, function () { return { ok: false, message: 'Could not reach the server.' }; });
  }

  // Rearm: one confirmation.
  function rearm(u) {
    var after = warchest - u.rearmCost;
    if (after < 0) {
      Confirm.ask({ title: 'Not enough funds', message: 'The warchest cannot cover rearming ' + fullName(u) + '.', detail: 'Cost ' + u.rearmCost + ' SP. Warchest ' + commas(warchest) + ' SP. Not enough funds.', detailShort: true, confirmLabel: 'OK', cancelLabel: 'Close' });
      return;
    }
    Confirm.ask({
      title: 'Rearm ' + u.name + '?',
      message: 'Refills the ammunition it has used, at 10 SP per ton.',
      items: (u.ammoBreakdown || []).map(function (a) {
        return { label: a.name, value: a.tons + (a.tons === 1 ? ' ton' : ' tons') + ' \u00b7 ' + a.used + (a.capacity ? ' of ' + a.capacity : '') + ' shots used' };
      }),
      detail: 'Cost ' + u.rearmCost + ' SP. Warchest ' + commas(warchest) + ' \u2192 ' + commas(after) + ' SP.',
      confirmLabel: 'Rearm for ' + u.rearmCost + ' SP',
      busyLabel: 'Rearming',
      // The dialog stays open until the rearm is saved, then closes and the page shows the updated hangar; a refusal is shown in it.
      action: function () { return service({ kind: 'rearm', unit: u.id }).then(function (r) { return r.ok ? '' : r.message; }); }
    }).then(function (yes) { if (yes) location.reload(); });
  }

  // Sell: one confirmation. A fully repaired and rearmed unit sells for half its BV, any other for a quarter; the server works the price out again.
  function sell(u) {
    var after = warchest + u.sellValue;
    var half = Math.floor(u.bv / 2);
    var why = [];
    if (u.condition === 'truly') why.push('Truly destroyed');
    else {
      if (u.condition && u.condition !== 'ok') why.push('Damaged');
      if (u.ammoExpended) why.push('Ammunition spent');
      if (!why.length) why.push('Not fully repaired');
    }
    var items = [
      { label: 'Base BV', value: commas(u.bv) + ' BV · 100%' },
      { label: u.sellFull ? 'Fully repaired and rearmed' : 'If fully repaired and rearmed', value: '50% · ' + commas(half) + ' SP' }
    ];
    if (!u.sellFull) items.push({ label: why.join(', ') + ': halved again, rounded down', value: '25% · ' + commas(u.sellValue) + ' SP' });
    items.push({ label: 'Sells for', value: commas(u.sellValue) + ' SP' });
    Confirm.ask({
      title: 'Sell ' + u.name + '?',
      message: fullName(u) + ' leaves your hangar for good, crew and all.' + (!u.sellFull && u.condition !== 'truly' ? ' Repair and rearm it first and it sells for ' + commas(half) + ' SP.' : u.condition === 'truly' ? ' A truly destroyed unit can’t be repaired, so this is all it is worth.' : ''),
      items: items,
      detail: 'Warchest ' + commas(warchest) + ' → ' + commas(after) + ' SP.',
      confirmLabel: 'Sell for ' + u.sellValue + ' SP', danger: true,
      busyLabel: 'Selling',
      // The dialog stays open until the sale is saved (the unit removed in MekBay, then the warchest paid), then closes and the page shows the updated hangar.
      action: function () {
        return fetch('/api/hangar/sell', {
          method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, credentials: 'same-origin',
          body: 'campaign=' + encodeURIComponent(section.dataset.campaign) + '&unit=' + encodeURIComponent(u.id)
        }).then(function (res) {
          return res.ok ? '' : res.text().then(function (t) { return t.trim() || 'Something went wrong.'; });
        }, function () { return 'Could not reach the server.'; });
      }
    }).then(function (yes) { if (yes) location.reload(); });
  }

  // Repair: a dialog listing only the repairs that would fix something on this unit. Any one can be chosen on its own (a crippled unit can have just
  // its armor repaired), and each level also covers the damage of the levels before it in the list. The highest is selected to begin with.
  var LEVELS = {
    armor: 'Repair Armor',
    structure: 'Repair Structure',
    crippled: 'Repair Crippled Unit',
    destroyed: 'Repair Destroyed Unit'
  };
  var repairDlg = document.getElementById('repair-dialog');
  var repairUnit = null;
  function repairCostText() {
    var level = document.getElementById('repair-select').value;
    var cost = repairUnit.repairCosts[level] || 0, after = warchest - cost;
    var text = document.getElementById('repair-cost');
    text.textContent = 'Cost ' + cost + ' SP. Warchest ' + commas(warchest) + ' \u2192 ' + commas(Math.max(after, 0)) + ' SP.' + (after < 0 ? ' Not enough funds.' : '');
    text.classList.toggle('short', after < 0);
    document.getElementById('repair-apply').disabled = after < 0;
  }
  function repair(u) {
    if (!repairDlg || typeof repairDlg.showModal !== 'function') return;
    repairUnit = u;
    document.getElementById('repair-title').textContent = 'Repair ' + u.name;
    document.getElementById('repair-sub').textContent = fullName(u) + (u.conditionNote ? ' \u2014 ' + u.conditionNote : '');
    var select = document.getElementById('repair-select');
    select.textContent = '';
    var levels = u.repairLevels || [];
    levels.forEach(function (level) {
      var o = document.createElement('option');
      o.value = level;
      o.textContent = (LEVELS[level] || level) + ' \u2014 ' + (u.repairCosts[level] || 0) + ' SP';
      select.appendChild(o);
    });
    select.value = levels[levels.length - 1];
    document.getElementById('repair-error').textContent = '';
    repairCostText();
    repairDlg.showModal();
  }
  if (repairDlg) {
    document.getElementById('repair-select').addEventListener('change', repairCostText);
    document.getElementById('repair-cancel').addEventListener('click', function () { repairDlg.close(); });
    repairDlg.addEventListener('click', function (e) { if (e.target === repairDlg) repairDlg.close(); });
    document.getElementById('repair-form').addEventListener('submit', function (e) {
      e.preventDefault();
      var apply = document.getElementById('repair-apply'), err = document.getElementById('repair-error');
      err.textContent = '';
      apply.disabled = true;
      service({ kind: 'repair', unit: repairUnit.id, level: document.getElementById('repair-select').value }).then(function (r) {
        if (r.ok) { repairDlg.close(); location.reload(); return; }
        err.textContent = r.message;
        repairCostText();
      });
    });
  }

  // Gives a mech row its unit card (build() makes it): shown on hover or focus under the row, right-aligned with it, the row lit while it is open. The row's
  // status pills have cards of their own, so coming back off a pill onto the row brings the unit's card back.
  function withCard(r, build) {
    MechRow.attachTip(r, { focusable: false, align: 'right', build: build });
    r.addEventListener('mouseover', function (e) {
      var own = e.target.closest && e.target.closest('[aria-describedby]');
      if ((!own || own === r) && !r.classList.contains('tip-open')) r.dispatchEvent(new Event('mouseenter'));
    });
  }

  // Reconfigure (omnimechs only): a dialog listing the unit's other variants with their prices, like the repair dialog.
  var swapDlg = document.getElementById('swap-dialog');
  var swapUnitRow = null;
  function swapOption() { return VariantPicker.option(swapUnitRow, document.getElementById('swap-select').value); }
  // The dialog shows the unit as its full row, then, once another variant is chosen, an arrow with what changes and the variant's own row: a flow from one to the
  // other, each row with its unit card (the shared VariantPicker, which the Deploy wizard's Configure step uses too).
  function swapCostText() {
    var o = swapOption(), u = swapUnitRow, cost = o ? o.cost : 0, after = warchest - cost;
    VariantPicker.mark(document.getElementById('swap-select'));
    VariantPicker.flow(document.getElementById('swap-sub'), u, o, { price: function (o) { return o.cost + ' SP'; } });
    // The price as a short breakdown: half the tonnage, plus half the BV gained (nothing for a variant worth less).
    var bd = document.getElementById('swap-breakdown');
    bd.textContent = '';
    bd.hidden = !o;
    if (o) {
      var gained = Math.max(0, o.bv - u.bv);
      var rows = [
        ['50% of tonnage', u.tons + 't · ' + Math.ceil(u.tons / 2) + ' SP'],
        ['50% of BV gained', (gained ? '+' + commas(gained) + ' BV' : 'no BV gained') + ' · ' + Math.floor(gained / 2) + ' SP']
      ];
      rows.push(['Cost', o.cost + ' SP']);
      rows.forEach(function (r) { bd.appendChild(el('dt', null, r[0])); bd.appendChild(el('dd', null, r[1])); });
    }
    var text = document.getElementById('swap-cost');
    text.textContent = o ? 'Cost ' + cost + ' SP. Warchest ' + commas(warchest) + ' → ' + commas(Math.max(after, 0)) + ' SP.' + (after < 0 ? ' Not enough funds.' : '') : 'Choose another variant to reconfigure to.';
    text.classList.toggle('short', !!o && after < 0);
    document.getElementById('swap-apply').disabled = !o || after < 0;
  }
  function swap(u, keep) {
    if (!swapDlg || typeof swapDlg.showModal !== 'function') return;
    swapUnitRow = u;
    var box = document.getElementById('swap-available');
    if (box && !keep) box.checked = true;
    var select = document.getElementById('swap-select');
    document.getElementById('swap-title').textContent = 'Reconfigure ' + u.name;
    // The current variant comes first and is chosen to begin with: reconfiguring to it would change nothing, so Reconfigure stays disabled until another is
    // picked. Like Purchase, only the variants the campaign's faction fields in its era are offered while the box is ticked (it is, each time the dialog
    // opens); unticking it lists them all. A variant still listed stays chosen when the box changes.
    VariantPicker.fill(select, u, {
      onlyAvailable: !!(box && box.checked),
      price: function (o) { return o.cost + ' SP'; },
      value: keep ? select.value : ''
    });
    document.getElementById('swap-error').textContent = '';
    swapCostText();
    if (!swapDlg.open) swapDlg.showModal();
  }
  if (swapDlg) {
    document.getElementById('swap-select').addEventListener('change', swapCostText);
    var availBox = document.getElementById('swap-available');
    if (availBox) availBox.addEventListener('change', function () { if (swapUnitRow) swap(swapUnitRow, true); });
    document.getElementById('swap-cancel').addEventListener('click', function () { swapDlg.close(); });
    swapDlg.addEventListener('click', function (e) { if (e.target === swapDlg) swapDlg.close(); });
    document.getElementById('swap-form').addEventListener('submit', function (e) {
      e.preventDefault();
      var apply = document.getElementById('swap-apply'), err = document.getElementById('swap-error');
      err.textContent = '';
      apply.disabled = true;
      fetch('/api/hangar/swap', {
        method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, credentials: 'same-origin',
        body: 'campaign=' + encodeURIComponent(section.dataset.campaign) + '&unit=' + encodeURIComponent(swapUnitRow.id) + '&to=' + encodeURIComponent(document.getElementById('swap-select').value)
      }).then(function (res) {
        if (res.ok) { swapDlg.close(); location.reload(); return; }
        return res.text().then(function (t) { err.textContent = t.trim() || 'Something went wrong.'; swapCostText(); });
      }, function () { err.textContent = 'Could not reach the server.'; swapCostText(); });
    });
  }

  // The buttons at the right of a row. Anything the unit can't do is disabled, with the reason as its tooltip.
  function actionsFor(u) {
    if (u.unknown || u.notMech) return null;
    var truly = u.condition === 'truly';
    return [
      {
        label: 'Rearm', onClick: function () { rearm(u); },
        disabled: !canModify || truly || !u.ammoExpended,
        title: !canModify ? NO_UID : truly ? 'A truly destroyed unit can only be sold.' : !u.ammoExpended ? 'No ammunition has been spent.' : 'Refill spent ammunition for ' + u.rearmCost + ' SP'
      },
      {
        label: 'Repair', onClick: function () { repair(u); },
        disabled: !canModify || !(u.repairLevels && u.repairLevels.length),
        title: !canModify ? NO_UID : truly ? 'Truly destroyed units cannot be repaired.' : !(u.repairLevels && u.repairLevels.length) ? 'Nothing to repair.' : 'Choose a repair'
      },
      u.swapOptions && u.swapOptions.length ? {
        label: 'Reconfigure', onClick: function () { swap(u); },
        disabled: !canModify || !u.sellFull,
        title: !canModify ? NO_UID : !u.sellFull ? 'An omnimech has to be fully repaired and rearmed before it can be reconfigured.' : 'Swap to another variant of this omnimech'
      } : null,
      {
        label: 'Sell', onClick: function () { sell(u); },
        disabled: !canModify,
        title: !canModify ? NO_UID : 'Sell for ' + u.sellValue + ' SP' + (u.sellFull ? '' : ' (repair and rearm it first to get twice as much)')
      }
    ].filter(Boolean);
  }

  function render(data) {
    var units = data.units || [];
    list.textContent = '';
    summary.textContent = '';
    var card = typeof ForceSummary !== 'undefined' ? ForceSummary.stats(data) : null;
    if (card) summary.appendChild(card);
    if (!units.length) {
      list.appendChild(el('p', 'muted', 'Your hangar has no units in it yet. Add some in MekBay.'));
      return;
    }
    // The hangar's pilot skills are a matter of assigning pilots, not something the mech itself has, so the rows leave them out.
    units.forEach(function (u) {
      var row = {};
      Object.keys(u).forEach(function (k) { if (k !== 'gunnery' && k !== 'piloting' && k !== 'pilot') row[k] = u[k]; });
      if (u.mekbayUrl) row.externalUrl = u.mekbayUrl; // the unit's own page on MekBay, opened in a new tab
      // Clicking the row opens the player's hangar on MekBay with this unit selected (MekBay's `sel` parameter is the force unit's ID).
      if (hangarLink && u.id) row.rowUrl = hangarLink + (hangarLink.indexOf('?') < 0 ? '?' : '&') + 'sel=' + encodeURIComponent(u.id);
      row.actions = actionsFor(u);
      var built = MechRow.build(row);
      // Hovering the row (or focusing it) shows the unit's card, the same card as the overview's and a scenario's unit links, under the row and
      // right-aligned with it, so the mechs listed on the left stay in view; the row keeps its yellow border while the card is open
      // (.mech-row.tip-open). Its status pills have cards of their own, so coming back off a pill onto the row brings the row's card back.
      if (typeof UnitCard !== 'undefined') withCard(built, function () { return UnitCard.forUnit(u); });
      list.appendChild(built);
    });
  }

  var shown = false;
  try {
    if (section.dataset.cached) { render(JSON.parse(section.dataset.cached)); shown = true; }
  } catch (e) { shown = false; }

  // A failed refresh keeps what is on screen and says so; with nothing on screen it is an error with a retry.
  function stale(message) {
    if (!shown) {
      list.textContent = '';
      list.appendChild(el('p', 'form-error', message));
      var retry = el('button', null, 'Try again');
      retry.type = 'button';
      retry.addEventListener('click', load);
      list.appendChild(retry);
      return;
    }
    var old = section.querySelector('.stale-note');
    if (old) old.remove();
    section.appendChild(el('p', 'muted stale-note', 'Showing your last known hangar. ' + message));
  }

  function load() {
    if (!shown) {
      list.textContent = '';
      list.appendChild(el('p', 'muted', 'Reading your hangar…'));
    }
    fetch('/api/campaign/force?campaign=' + encodeURIComponent(section.dataset.campaign), { credentials: 'same-origin' })
      .then(function (res) {
        if (res.ok) return res.json().then(function (data) { var old = section.querySelector('.stale-note'); if (old) old.remove(); render(data); shown = true; });
        return res.text().then(function (t) { stale(t.trim() || 'Could not read your hangar.'); });
      }, function () { stale('Could not reach the server.'); });
  }
  load();
})();
