// The site's own confirmation dialog, used instead of the browser's window.confirm so it matches the rest of the page.
//
//   Confirm.ask({ title, message, confirmLabel, cancelLabel, danger }) -> Promise<boolean>
//
// It resolves true when the player confirms and false for Cancel, Escape or a click on the backdrop. `danger` makes the
// confirm button red and puts the focus on Cancel, so a stray Enter never confirms something destructive. Text is inserted
// as text, never HTML. It opens on top of any dialog that is already open (a modal dialog stacks above the others).
(function () {
  var dialog = null, titleEl = null, messageEl = null, okBtn = null, cancelBtn = null;
  var settle = null; // resolves the question that is open

  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }

  function build() {
    dialog = el('dialog', 'confirm-dialog');
    dialog.setAttribute('aria-labelledby', 'confirm-title');
    dialog.setAttribute('aria-describedby', 'confirm-message');
    titleEl = el('h2');
    titleEl.id = 'confirm-title';
    messageEl = el('p', 'confirm-message');
    messageEl.id = 'confirm-message';
    var actions = el('div', 'form-actions');
    okBtn = el('button', 'button');
    okBtn.type = 'button';
    cancelBtn = el('button');
    cancelBtn.type = 'button';
    actions.appendChild(okBtn);
    actions.appendChild(cancelBtn);
    dialog.appendChild(titleEl);
    dialog.appendChild(messageEl);
    dialog.appendChild(actions);

    function finish(answer) {
      var done = settle;
      settle = null;
      if (dialog.hasAttribute('open')) dialog.close();
      if (done) done(answer);
    }
    okBtn.addEventListener('click', function () { finish(true); });
    cancelBtn.addEventListener('click', function () { finish(false); });
    // Escape closes the dialog by itself; the backdrop is the dialog element outside its content.
    dialog.addEventListener('close', function () { finish(false); });
    dialog.addEventListener('click', function (e) { if (e.target === dialog) finish(false); });
    document.body.appendChild(dialog);
  }

  function ask(o) {
    o = o || {};
    if (typeof HTMLDialogElement === 'undefined' || typeof document.createElement('dialog').showModal !== 'function') {
      return Promise.resolve(window.confirm((o.title ? o.title + '\n\n' : '') + (o.message || '')));
    }
    if (!dialog) build();
    if (settle) { var previous = settle; settle = null; previous(false); } // a second question replaces the first
    titleEl.textContent = o.title || 'Are you sure?';
    messageEl.textContent = o.message || '';
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
