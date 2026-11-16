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
  // The arrow between the SP so far and the next level, drawn rather than typed so it reads clearly in any font.
  var ARROW = '<svg viewBox="0 0 16 10" width="16" height="10" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M1 5h13M10 1l4 4-4 4"/></svg>';

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

  // The box for one column: a big, centred level; a tiny "-> next level" at its lower right; the SP so far over the SP the next
  // level takes; and a yellow progress bar for how far along that is. At the top of the ladder it says "max" with a full bar.
  function statBox(l) {
    var box = el('div', 'stat-box');
    var value = el('span', 'stat-value');
    var to = el('span', 'stat-to');
    var arrow = el('span', 'stat-arrow');
    arrow.setAttribute('aria-hidden', 'true');
    arrow.innerHTML = ARROW; // a constant defined above, never data
    var nextLevel = el('b');
    to.appendChild(arrow);
    to.appendChild(nextLevel);
    var wrap = el('span', 'stat-valwrap'); // only the value sits in the flow, so it stays centred whatever the "to" says
    wrap.appendChild(value);
    wrap.appendChild(to);
    var main = el('span', 'stat-main');
    main.appendChild(wrap);
    var next = el('span', 'stat-next');
    var bar = el('span', 'stat-bar');
    var fill = el('span');
    bar.appendChild(fill);
    box.appendChild(el('span', 'stat-label', l.name));
    box.appendChild(main);
    box.appendChild(next);
    box.appendChild(bar);
    box.show = function (spent) {
      var r = reach(l, spent), n = upcoming(l, spent);
      value.textContent = r.level;
      to.hidden = !n;
      if (n) {
        nextLevel.textContent = String(n.level);
        next.textContent = spent + '/' + n.sp + ' SP';
        fill.style.width = Math.min(100, Math.round(spent / n.sp * 100)) + '%';
      } else {
        next.textContent = 'max';
        fill.style.width = '100%';
      }
      box.classList.toggle('up', r.level !== l.base);
    };
    return box;
  }

  window.SPAllocator = { reach: reach, upcoming: upcoming, spentOf: spentOf, row: row, statBox: statBox };
})();
