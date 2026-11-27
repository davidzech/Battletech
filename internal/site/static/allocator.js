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
// added, and the controls show the column's whole total, with what it holds now as their minimum.
(function () {
  var el = Dom.el;

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
  //
  // Without o.base the boxes hold the SP being allocated (0 to o.budget). With o.base ({key: SP already in the column}) they
  // hold the column's whole total instead: the minimum of both controls is what the column has now, and they reach up to
  // that plus the budget, so p.sp[key] is still only the SP being added. Typing in the number box isn't rewritten while it
  // has focus (a half-typed "19" on the way to "190" is below the minimum), only tidied once it is left.
  function row(p, l, o) {
    var base = o.base ? o.base[l.key] : 0;
    var r = el('div', 'alloc-row');
    var name = el('label', 'alloc-name', l.name);
    var slider = document.createElement('input');
    slider.type = 'range'; slider.min = base; slider.max = base + o.budget; slider.step = o.step;
    slider.setAttribute('aria-label', l.name); // the label wraps only the number box
    var number = document.createElement('input');
    number.type = 'number'; number.min = base; number.max = base + o.budget; number.step = o.step; number.inputMode = 'numeric';
    name.appendChild(number);
    r.appendChild(name);
    r.appendChild(slider);

    function set(raw) {
      var n = parseInt(raw, 10) - base;
      if (isNaN(n) || n < 0) n = 0;
      var others = spentOf(p, o.ladders) - p.sp[l.key];
      p.sp[l.key] = Math.min(n, Math.max(0, o.budget - others)); // never more than is left to allocate
      if (o.onEdit) o.onEdit();
      o.refresh();
    }
    slider.addEventListener('input', function () { set(slider.value); });
    number.addEventListener('input', function () { set(number.value); });
    number.addEventListener('change', function () { number.value = base + p.sp[l.key]; });
    r.show = function () {
      slider.value = base + p.sp[l.key];
      if (!(o.base && document.activeElement === number)) number.value = base + p.sp[l.key];
      r.classList.toggle('changed', p.sp[l.key] > 0); // the number turns yellow while the column has a pending change
    };
    r.show();
    return r;
  }

  // The box for one column (Dom.statBox): a big, centred level; a tiny "-> next level" at its lower right; the SP so far over the SP the next level takes; and a
  // yellow progress bar for how far along that is. At the top of the ladder it says "max" with a full bar.
  function statBox(l) {
    var box = Dom.statBox(l.short || l.name); // the stat box's own, shorter label when there is one
    var show = box.show;
    box.show = function (spent) {
      var r = reach(l, spent), n = upcoming(l, spent);
      // How far from the breakpoint reached to the next one (pilot.go's stepPercent): Edge abilities at 180 SP is 0% of the way to 360.
      show(r.level, n ? { level: n.level, text: spent + '/' + n.sp + ' SP', percent: Math.round((spent - r.sp) / (n.sp - r.sp) * 100) } : null);
      box.classList.toggle('up', r.level !== l.base);
    };
    return box;
  }

  window.SPAllocator = { reach: reach, upcoming: upcoming, spentOf: spentOf, row: row, statBox: statBox };
})();
