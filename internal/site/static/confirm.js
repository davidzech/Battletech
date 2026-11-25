// The site's own confirmation dialog, used instead of the browser's window.confirm so it matches the rest of the page.
//
//   Confirm.ask({ title, message, items, detail, confirmLabel, cancelLabel, danger, action, busyLabel }) -> Promise<boolean>
//
// `action`, when given, is the work the confirm button does: a function returning a Promise that resolves to '' (or nothing) when it worked, or to a message
// saying why it didn't. The dialog then stays open while it runs, its buttons disabled and the confirm button saying `busyLabel` (e.g. "Selling") with the wait
// cursor, and Escape and the backdrop do nothing. It closes, resolving true, only once the action has succeeded; a failure shows the message in the dialog and
// lets the player try again or cancel. Without an action it resolves true at once, as before.
//
// `items` is an optional list of { label, value } rows shown under the message as a small breakdown (the rearm popup's tons of each ammo type).
//
// `detail` is an optional second line in bold yellow, the same style as the repair dialog's cost line (e.g. "Cost 10 SP. Warchest 4,500 \u2192 4,490 SP.").
//
// It resolves true when the player confirms and false for Cancel, Escape or a click on the backdrop. `danger` makes the
// confirm button red and puts the focus on Cancel, so a stray Enter never confirms something destructive. Text is inserted
// as text, never HTML. It opens on top of any dialog that is already open (a modal dialog stacks above the others).
(function () {
  var dialog = null, titleEl = null, messageEl = null, itemsEl = null, detailEl = null, errorEl = null, okBtn = null, cancelBtn = null;
  var settle = null; // resolves the question that is open
  var current = {};  // the open question's options
  var busy = false;  // its action is running

  var el = Dom.el;

  function build() {
    dialog = el('dialog', 'confirm-dialog');
    dialog.setAttribute('aria-labelledby', 'confirm-title');
    dialog.setAttribute('aria-describedby', 'confirm-message');
    titleEl = el('h2');
    titleEl.id = 'confirm-title';
    messageEl = el('p', 'confirm-message');
    messageEl.id = 'confirm-message';
    itemsEl = el('dl', 'confirm-items');
    detailEl = el('p', 'confirm-detail');
    errorEl = el('p', 'form-error confirm-error');
    errorEl.setAttribute('role', 'alert');
    var actions = el('div', 'form-actions');
    okBtn = el('button', 'button');
    okBtn.type = 'button';
    cancelBtn = el('button');
    cancelBtn.type = 'button';
    actions.appendChild(okBtn);
    actions.appendChild(cancelBtn);
    dialog.appendChild(titleEl);
    dialog.appendChild(messageEl);
    dialog.appendChild(itemsEl);
    dialog.appendChild(detailEl);
    dialog.appendChild(errorEl);
    dialog.appendChild(actions);

    function finish(answer) {
      var done = settle;
      settle = null;
      if (dialog.hasAttribute('open')) dialog.close();
      if (done) done(answer);
    }
    function setBusy(on) {
      busy = on;
      okBtn.disabled = cancelBtn.disabled = on;
      okBtn.setAttribute('aria-busy', on ? 'true' : 'false');
      okBtn.textContent = on ? (current.busyLabel || 'Working') : (current.confirmLabel || 'Confirm');
    }
    okBtn.addEventListener('click', function () {
      if (busy) return;
      if (typeof current.action !== 'function') { finish(true); return; }
      errorEl.textContent = '';
      setBusy(true);
      var asked = settle; // the question this click answers (a new one may replace it meanwhile)
      Promise.resolve().then(current.action).then(function (msg) { return msg || ''; }, function () { return 'Something went wrong.'; }).then(function (msg) {
        if (settle !== asked) return;
        setBusy(false);
        if (msg) { errorEl.textContent = msg; okBtn.focus(); return; }
        finish(true);
      });
    });
    cancelBtn.addEventListener('click', function () { if (!busy) finish(false); });
    // Escape closes the dialog by itself (not while an action runs); the backdrop is the dialog element outside its content.
    dialog.addEventListener('cancel', function (e) { if (busy) e.preventDefault(); });
    // The close event comes a moment after close(): by then a question asked straight after the last one (a second "Are you sure?") may have reopened the
    // dialog, and that stale event must not answer it. A close that leaves the dialog shut is the player's (Escape) and cancels.
    dialog.addEventListener('close', function () { if (!dialog.open) finish(false); });
    dialog.addEventListener('click', function (e) { if (e.target === dialog && !busy) finish(false); });
    document.body.appendChild(dialog);
  }

  function ask(o) {
    o = o || {};
    if (typeof HTMLDialogElement === 'undefined' || typeof document.createElement('dialog').showModal !== 'function') {
      return Promise.resolve(window.confirm((o.title ? o.title + '\n\n' : '') + (o.message || '')));
    }
    if (!dialog) build();
    if (settle) { var previous = settle; settle = null; previous(false); } // a second question replaces the first
    current = o;
    busy = false;
    okBtn.disabled = cancelBtn.disabled = false;
    okBtn.setAttribute('aria-busy', 'false');
    errorEl.textContent = '';
    titleEl.textContent = o.title || 'Are you sure?';
    messageEl.textContent = o.message || '';
    itemsEl.textContent = '';
    (o.items || []).forEach(function (it) {
      itemsEl.appendChild(el('dt', null, it.label));
      itemsEl.appendChild(el('dd', null, it.value));
    });
    itemsEl.hidden = !(o.items && o.items.length);
    detailEl.textContent = o.detail || '';
    detailEl.hidden = !o.detail;
    detailEl.classList.toggle('short', !!o.detailShort);
    okBtn.textContent = o.confirmLabel || 'Confirm';
    cancelBtn.textContent = o.cancelLabel || 'Cancel';
    okBtn.classList.toggle('danger', !!o.danger);
    dialog.classList.toggle('danger', !!o.danger);
    return new Promise(function (resolve) {
      settle = resolve;
      dialog.showModal();
      (o.danger ? cancelBtn : okBtn).focus();
    });
  }

  window.Confirm = { ask: ask };
})();
