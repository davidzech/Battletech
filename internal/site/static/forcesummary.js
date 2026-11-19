// The one-line hangar summary card shared by the campaign overview and the Manage page's Hangar section: up to eight unit icons (each with the
// unit's hover card, UnitCard, so load mechrow.js and unitcard.js first), the unit count, damaged/destroyed counts and the total base BV in big yellow, so the state of the force
// reads at a glance. Names are inserted as text, never HTML.
//
//   ForceSummary.build({ units: [...hangar unit views], bv: total }) -> HTMLElement, or null when there are no units
//   ForceSummary.stats(data) -> the same facts as plain labelled number boxes (UNITS, BASE BV, and DAMAGED / DESTROYED when there are any), with no icons
(function () {
  var CAP = 8; // icons shown; more would crowd the row, the rest are only counted

  var el = Dom.el;
  var commas = Dom.commas;

  function build(data) {
    var units = (data && data.units) || [];
    if (!units.length) return null;
    var bv = 0, damaged = 0, destroyed = 0;
    units.forEach(function (u) {
      if (!u.unknown) bv += u.bv;
      if (u.destroyed) destroyed++; else if (u.damaged) damaged++;
    });
    if (typeof data.bv === 'number') bv = data.bv;

    var row = el('div', 'force-summary');
    var icons = el('span', 'icons');
    units.slice(0, CAP).forEach(function (u) {
      var img = document.createElement('img');
      img.src = u.icon; img.alt = u.name + (u.model ? ' ' + u.model : '');
      img.width = 42; img.height = 36;
      if (u.destroyed) img.className = 'destroyed';
      MechRow.attachTip(img, { build: function () { return UnitCard.forUnit(u); } }); // focusable, and shown at once like a scenario's unit links
      icons.appendChild(img);
    });
    if (units.length > CAP) icons.appendChild(el('span', 'more', '+' + (units.length - CAP)));
    row.appendChild(icons);
    row.appendChild(el('b', null, units.length + (units.length === 1 ? ' unit' : ' units')));
    var notes = [];
    if (damaged) notes.push(damaged + ' damaged');
    if (destroyed) notes.push(destroyed + ' destroyed');
    if (notes.length) row.appendChild(el('span', 'notes', notes.join(' · ')));
    row.appendChild(el('span', 'total', commas(bv) + ' BV'));
    return row;
  }

  // The facts without the icons, for a page where the unit rows are right underneath: big yellow numbers with small-caps labels, in the
  // style of the pilots' stat boxes. Damaged and destroyed boxes appear only when there are any.
  function stats(data) {
    var units = (data && data.units) || [];
    if (!units.length) return null;
    var bv = 0, damaged = 0, destroyed = 0;
    units.forEach(function (u) {
      if (!u.unknown) bv += u.bv;
      if (u.destroyed) destroyed++; else if (u.damaged) damaged++;
    });
    if (typeof data.bv === 'number') bv = data.bv;
    var row = el('div', 'hangar-stats');
    function box(label, value, cls) {
      var b = el('div', 'hangar-stat' + (cls ? ' ' + cls : ''));
      b.appendChild(el('span', 'hangar-stat-label', label));
      b.appendChild(el('span', 'hangar-stat-value', value));
      row.appendChild(b);
    }
    box('Units', String(units.length));
    box('Base BV', commas(bv));
    if (damaged) box('Damaged', String(damaged), 'warn');
    if (destroyed) box('Destroyed', String(destroyed), 'bad');
    return row;
  }

  window.ForceSummary = { build: build, stats: stats };
})();
