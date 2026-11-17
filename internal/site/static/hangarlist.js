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

  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }
  // The count and total BV as big yellow numbers with labels; the rows underneath carry the per-unit detail, so no icons here.
  var hangarLink = section.dataset.hangarUrl || ''; // the player's MekBay hangar, which a row click opens with that unit selected

  var canModify = section.dataset.canModify === '1';
  var warchest = parseInt(section.dataset.warchest, 10) || 0;
  var NO_UID = 'This hangar is from before the site kept its own copy. Import it again under Manage force, Danger zone, Re-import hangar.';
  function commas(n) { return String(n).replace(/\B(?=(\d{3})+(?!\d))/g, ','); }
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
      confirmLabel: 'Rearm for ' + u.rearmCost + ' SP'
    }).then(function (yes) {
      if (!yes) return;
      service({ kind: 'rearm', unit: u.id }).then(function (r) {
        if (r.ok) location.reload();
        else Confirm.ask({ title: 'Could not rearm', message: r.message, confirmLabel: 'OK', cancelLabel: 'Close' });
      });
    });
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
      { label: 'Base BV', value: commas(u.bv) },
      { label: 'Sale price: half of BV', value: commas(half) + ' SP' },
      u.sellFull
        ? { label: 'Fully repaired and rearmed', value: 'full price' }
        : { label: why.join(', ') + ': halved again, rounded down', value: commas(u.sellValue) + ' SP' },
      { label: 'Sells for', value: commas(u.sellValue) + ' SP' }
    ];
    Confirm.ask({
      title: 'Sell ' + u.name + '?',
      message: fullName(u) + ' leaves your hangar for good, crew and all.' + (!u.sellFull && u.condition !== 'truly' ? ' Repair and rearm it first and it sells for ' + commas(half) + ' SP.' : u.condition === 'truly' ? ' A truly destroyed unit can’t be repaired, so this is all it is worth.' : ''),
      items: items,
      detail: 'Warchest ' + commas(warchest) + ' → ' + commas(after) + ' SP.',
      confirmLabel: 'Sell for ' + u.sellValue + ' SP', danger: true
    }).then(function (yes) {
      if (!yes) return;
      fetch('/api/hangar/sell', {
        method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, credentials: 'same-origin',
        body: 'campaign=' + encodeURIComponent(section.dataset.campaign) + '&unit=' + encodeURIComponent(u.id)
      }).then(function (res) {
        if (res.ok) { location.reload(); return; }
        return res.text().then(function (t) { Confirm.ask({ title: 'Could not sell', message: t.trim() || 'Something went wrong.', confirmLabel: 'OK', cancelLabel: 'Close' }); });
      }, function () { Confirm.ask({ title: 'Could not sell', message: 'Could not reach the server.', confirmLabel: 'OK', cancelLabel: 'Close' }); });
    });
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
      {
        label: 'Sell', onClick: function () { sell(u); },
        disabled: !canModify,
        title: !canModify ? NO_UID : 'Sell for ' + u.sellValue + ' SP' + (u.sellFull ? '' : ' (repair and rearm it first to get twice as much)')
      }
    ];
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
      list.appendChild(MechRow.build(row));
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
