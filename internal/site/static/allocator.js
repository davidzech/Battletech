// The SP allocator, shared by the start wizard and the Manage SP dialog: for each of a pilot's four columns a slider and
// a number box for the same amount of SP, and the big yellow "stat box" showing the level that amount reaches. The
// ladders (the book's Named Pilot Improvement Table) come from the server as JSON, so levels are worked out here exactly
// as the server does. Load it before the script that uses it.
//
//   SPAllocator.row(p, ladder, { ladders, budget, step, refresh, onEdit, base })  one column's slider and number box
//   SPAllocator.statBox(ladder)                                                    box.show(spent) sets level and "next"
//   SPAllocator.reach / upcoming / spentOf                                         ladder maths
//
// p.sp[ladder.key] is the SP in a column. With `base` ({key: SP already in the column}) p.sp holds only the SP being
// added, and the row says what the column holds now.
(function () {
  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }

  // The row of the ladder reached with this much SP (its level and the SP it takes).
  function reach(l, spent) {
    var r = { level: l.base, sp: 0 };
    l.rows.forEach(function (row) { if (spent >= row.sp) r = row; });
    return r;
  }

  // The next row up the ladder, or null at the top.
  function upcoming(l, spent) {
    for (var j = 0; j < l.rows.length; j++) if (spent < l.rows[j].sp) return l.rows[j];
    return null;
  }

  function spentOf(p, ladders) { return ladders.reduce(function (sum, l) { return sum + p.sp[l.key]; }, 0); }

  // One column of the allocator: a slider and a number box for the same amount of SP. Never more than o.budget in total
  // across the columns; o.refresh() is called after every change.
  function row(p, l, o) {
    var r = el('div', 'alloc-row');
    var name = el('label', 'alloc-name', l.name);
    var slider = document.createElement('input');
    slider.type = 'range'; slider.min = 0; slider.max = o.budget; slider.step = o.step;
    var number = document.createElement('input');
    number.type = 'number'; number.min = 0; number.max = o.budget; number.step = o.step; number.inputMode = 'numeric';
    name.appendChild(number);
    r.appendChild(name);
    r.appendChild(slider);
    var note = o.base ? el('span', 'alloc-base') : null;
    if (note) r.appendChild(note);

    function set(raw) {
      var n = parseInt(raw, 10);
      if (isNaN(n) || n < 0) n = 0;
      var others = spentOf(p, o.ladders) - p.sp[l.key];
      p.sp[l.key] = Math.min(n, Math.max(0, o.budget - others)); // never more than is left to allocate
      if (o.onEdit) o.onEdit();
      o.refresh();
    }
    slider.addEventListener('input', function () { set(slider.value); });
    number.addEventListener('input', function () { set(number.value); });
    r.show = function () {
      slider.value = p.sp[l.key]; number.value = p.sp[l.key];
      if (note) {
        note.textContent = 'Total: ' + o.base[l.key] + ' SP' + (p.sp[l.key] ? ' → ' + (o.base[l.key] + p.sp[l.key]) + ' SP' : '');
        note.classList.toggle('changed', p.sp[l.key] > 0); // yellow only while the column has a pending change
      }
    };
    r.show();
    return r;
  }

  // The big number for one column: the level the SP allocated so far reaches, and what the next level takes.
  function statBox(l) {
    var box = el('div', 'stat-box');
    var value = el('span', 'stat-value');
    var next = el('span', 'stat-next');
    box.appendChild(el('span', 'stat-label', l.name));
    box.appendChild(value);
    box.appendChild(next);
    box.show = function (spent) {
      var r = reach(l, spent), n = upcoming(l, spent);
      value.textContent = r.level;
      next.textContent = n ? 'next: ' + spent + '/' + n.sp + ' SP' : 'max';
      box.classList.toggle('up', r.level !== l.base);
    };
    return box;
  }

  window.SPAllocator = { reach: reach, upcoming: upcoming, spentOf: spentOf, row: row, statBox: statBox };
})();
