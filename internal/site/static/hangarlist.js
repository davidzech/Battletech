// Manage page: the campaign's MekBay hangar as two big labelled numbers, UNITS and BASE BV (forcesummary.js), above the full list of its mechs, as the same mech
// rows the deploy popup uses (mechrow.js). Both scripts must load first. Like the overview, the page embeds the hangar, so the list paints at once (only a page
// that could not load it reads GET /api/campaign/force). Names, roles and models go in as text, never HTML. Each mech row also has Rearm and Repair buttons
// (costs come from the server, which charges the warchest and changes the unit in the campaign's hangar; Sell pays the warchest). Confirm (confirm.js) must
// load first.
(function () {
  var section = document.getElementById('hangar-units');
  var list = document.getElementById('hangar-units-list');
  var summary = document.getElementById('hangar-units-summary');
  if (!section || !list || typeof MechRow === 'undefined') return;

  var el = Dom.el;
  // The count and total BV as big yellow numbers with labels; the rows underneath carry the per-unit detail, so no icons here.
  var warchest = parseInt(section.dataset.warchest, 10) || 0;
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
    var cost = Dom.costLine(u.rearmCost, warchest);
    if (cost.short) {
      Confirm.ask({ title: 'Not enough funds', message: 'The warchest cannot cover rearming ' + fullName(u) + '.', detail: cost.text, detailShort: true, confirmLabel: 'OK', cancelLabel: 'Close' });
      return;
    }
    Confirm.ask({
      title: 'Rearm ' + u.name + '?',
      message: 'Refills the ammunition it has used, at 10 SP per ton.',
      items: (u.ammoBreakdown || []).map(function (a) {
        return { label: a.name, value: a.tons + (a.tons === 1 ? ' ton' : ' tons') + ' \u00b7 ' + a.used + (a.capacity ? ' of ' + a.capacity : '') + ' shots used' };
      }),
      detail: cost.text,
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
    var cost = Dom.costLine(repairUnit.repairCosts[level] || 0, warchest);
    var text = document.getElementById('repair-cost');
    text.textContent = cost.text;
    text.classList.toggle('short', cost.short);
    document.getElementById('repair-apply').disabled = cost.short;
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

  // Reconfigure (omnimechs only): the shared Reconfigure dialog (swapdialog.js), which lists the unit's other variants with their prices; the change is made
  // (and paid for) at once.
  function swap(u) {
    if (typeof SwapDialog === 'undefined') return;
    SwapDialog.open(u, {
      availability: section.dataset.swapAvailability || '',
      price: function (o) { return o.cost + ' SP'; },
      cost: function (o) { return o.cost; },
      warchest: warchest,
      submit: function (o) {
        return fetch('/api/hangar/swap', {
          method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, credentials: 'same-origin',
          body: 'campaign=' + encodeURIComponent(section.dataset.campaign) + '&unit=' + encodeURIComponent(u.id) + '&to=' + encodeURIComponent(o ? o.unit : '')
        }).then(function (res) {
          if (res.ok) { location.reload(); return; }
          return res.text().then(function (t) { throw new Error(t.trim() || 'Something went wrong.'); });
        }, function () { throw new Error('Could not reach the server.'); });
      }
    });
  }

  // The buttons at the right of a row. Anything the unit can't do is disabled, with the reason as its tooltip.
  function actionsFor(u) {
    if (u.unknown || u.notMech) return null;
    var truly = u.condition === 'truly';
    return [
      {
        label: 'Rearm', onClick: function () { rearm(u); },
        disabled: truly || !u.ammoExpended,
        title: truly ? 'A truly destroyed unit can only be sold.' : !u.ammoExpended ? 'No ammunition has been spent.' : 'Refill spent ammunition for ' + u.rearmCost + ' SP'
      },
      {
        label: 'Repair', onClick: function () { repair(u); },
        disabled: !(u.repairLevels && u.repairLevels.length),
        title: truly ? 'Truly destroyed units cannot be repaired.' : !(u.repairLevels && u.repairLevels.length) ? 'Nothing to repair.' : 'Choose a repair'
      },
      u.swapOptions && u.swapOptions.length ? {
        label: 'Reconfigure', onClick: function () { swap(u); },
        disabled: !u.sellFull,
        title: !u.sellFull ? 'An omnimech has to be fully repaired and rearmed before it can be reconfigured.' : 'Swap to another variant of this omnimech'
      } : null,
      {
        label: 'Sell', onClick: function () { sell(u); },
        title: 'Sell for ' + u.sellValue + ' SP' + (u.sellFull ? '' : ' (repair and rearm it first to get twice as much)')
      }
    ].filter(Boolean);
  }

  function render(data) {
    var units = data.units || [];
    MechRow.hideTip(); // the rows are about to be replaced, so a card open on one would point at nothing
    list.textContent = '';
    summary.textContent = '';
    var card = typeof ForceSummary !== 'undefined' ? ForceSummary.stats(data) : null;
    if (card) summary.appendChild(card);
    if (!units.length) {
      list.appendChild(el('p', 'muted', 'Your hangar has no units in it yet. Buy some with Purchase New Units.'));
      return;
    }
    // The hangar's pilot skills are a matter of assigning pilots, not something the mech itself has, so the rows leave them out.
    units.forEach(function (u) {
      var row = MechRow.withoutPilot(u);
      if (u.mekbayUrl) row.externalUrl = u.mekbayUrl; // the unit's own page on MekBay, opened in a new tab
      row.actions = actionsFor(u);
      var built = MechRow.build(row);
      // Hovering the row (or focusing it) shows the unit's card, the same card as the overview's and a scenario's unit links, under the row and
      // right-aligned with it, so the mechs listed on the left stay in view; the row keeps its yellow border while the card is open
      // (.mech-row.tip-open). Its status pills have cards of their own, so coming back off a pill onto the row brings the row's card back.
      if (typeof UnitCard !== 'undefined') MechRow.attachCard(built, function () { return UnitCard.forUnit(u); });
      list.appendChild(built);
    });
  }

  function fail(message) {
    list.textContent = '';
    list.appendChild(el('p', 'form-error', message));
    var retry = el('button', null, 'Try again');
    retry.type = 'button';
    retry.addEventListener('click', load);
    list.appendChild(retry);
  }

  function load() {
    list.textContent = '';
    list.appendChild(el('p', 'muted', 'Reading your hangar…'));
    fetch('/api/campaign/force?campaign=' + encodeURIComponent(section.dataset.campaign), { credentials: 'same-origin' })
      .then(function (res) {
        if (res.ok) return res.json().then(render);
        return res.text().then(function (t) { fail(t.trim() || 'Could not read your hangar.'); });
      }, function () { fail('Could not reach the server.'); });
  }

  var embedded = null;
  try { embedded = section.dataset.hangar ? JSON.parse(section.dataset.hangar) : null; } catch (e) { embedded = null; }
  if (embedded) render(embedded);
  else load();
})();
