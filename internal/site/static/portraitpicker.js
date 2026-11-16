// Portrait picker: a modal grid of every pilot portrait, grouped, that scrolls. Shared by the start wizard and the
// named-pilots panel; load it before the script that uses it.
//
//   PortraitPicker.open({ portraits: [{file, group, label}], selected: 'file.png', taken: ['other.png'],
//                         onPick: function (file) {} })
//
// Portraits in `taken` (another living pilot's) are shown dimmed and can't be chosen. Names and labels are inserted
// as text, never HTML.
(function () {
  var dialog = null;

  function build() {
    dialog = document.createElement('dialog');
    dialog.className = 'portrait-dialog';
    dialog.setAttribute('aria-labelledby', 'portrait-title');
    var form = document.createElement('div');
    var h = document.createElement('h2');
    h.id = 'portrait-title';
    h.textContent = 'Choose a portrait';
    var grid = document.createElement('div');
    grid.className = 'portrait-scroll';
    var actions = document.createElement('div');
    actions.className = 'form-actions';
    var cancel = document.createElement('button');
    cancel.type = 'button';
    cancel.textContent = 'Cancel';
    cancel.addEventListener('click', function () { dialog.close(); });
    actions.appendChild(cancel);
    form.appendChild(h);
    form.appendChild(grid);
    form.appendChild(actions);
    dialog.appendChild(form);
    dialog.grid = grid;
    // A click on the backdrop (the dialog element itself, outside its content) closes it.
    dialog.addEventListener('click', function (e) { if (e.target === dialog) dialog.close(); });
    document.body.appendChild(dialog);
  }

  function open(opts) {
    if (!dialog) build();
    var taken = {};
    (opts.taken || []).forEach(function (f) { taken[f] = true; });
    var grid = dialog.grid;
    grid.textContent = '';
    var group = null, section = null, selectedBtn = null;
    opts.portraits.forEach(function (p) {
      if (p.group !== group) {
        group = p.group;
        var head = document.createElement('h3');
        head.textContent = group;
        section = document.createElement('div');
        section.className = 'portrait-grid';
        grid.appendChild(head);
        grid.appendChild(section);
      }
      var b = document.createElement('button');
      b.type = 'button';
      b.className = 'portrait-choice';
      b.title = p.label + (taken[p.file] ? ' (used by another pilot)' : '');
      b.setAttribute('aria-label', b.title);
      var img = document.createElement('img');
      img.src = '/portraits/' + p.file;
      img.alt = '';
      img.loading = 'lazy';
      img.width = 96; img.height = 96;
      b.appendChild(img);
      if (taken[p.file]) { b.disabled = true; b.classList.add('taken'); }
      if (p.file === opts.selected) { b.classList.add('selected'); selectedBtn = b; }
      b.addEventListener('click', function () { dialog.close(); opts.onPick(p.file); });
      section.appendChild(b);
    });
    dialog.showModal();
    if (selectedBtn) selectedBtn.scrollIntoView({ block: 'center' });
    else grid.scrollTop = 0;
  }

  window.PortraitPicker = { open: open };
})();
