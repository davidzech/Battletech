// Keyboard shortcuts for every wizard: Enter presses the open wizard's Next button and Backspace its Back button, only while that button can be clicked (shown
// and enabled). A wizard opts in by marking its buttons data-wizard-next and data-wizard-back (the start wizard, the Deploy wizard, the account popup).
//
// The keys are left alone where they already mean something: Enter on a button, link, select or text area, or wherever a handler of its own took the key (the
// Deploy wizard's pilot slots do, while a pilot is held), and Backspace in a field that takes text. With a modifier held, or a dialog stacked over the wizard (a
// confirmation, the portrait picker), nothing happens.
(function () {
  var TEXT_INPUTS = /^(text|search|email|url|tel|password|number|date|datetime-local|month|time|week)$/;

  function takesText(t) {
    if (!t) return false;
    if (t.isContentEditable || t.tagName === 'TEXTAREA') return true;
    return t.tagName === 'INPUT' && TEXT_INPUTS.test(t.type || 'text');
  }

  // The wizard the key is for: the dialog holding the focus, or, with the focus on the page itself, the only dialog open.
  function wizardFor(t) {
    var d = t && t.closest ? t.closest('dialog[open]') : null;
    if (d) return d;
    var open = document.querySelectorAll('dialog[open]');
    return open.length === 1 ? open[0] : null;
  }

  function clickable(b) {
    return !!b && !b.disabled && !b.hidden && !b.closest('[hidden]') && b.getClientRects().length > 0;
  }

  document.addEventListener('keydown', function (e) {
    if (e.key !== 'Enter' && e.key !== 'Backspace') return;
    if (e.defaultPrevented || e.isComposing || e.altKey || e.ctrlKey || e.metaKey || e.shiftKey) return;
    var t = document.activeElement;
    var d = wizardFor(t);
    if (!d) return;
    var b;
    if (e.key === 'Enter') {
      if (t && t !== d && t.closest('button, a[href], summary, select, textarea, [contenteditable="true"]')) return;
      b = d.querySelector('[data-wizard-next]');
    } else {
      if (takesText(t)) return;
      b = d.querySelector('[data-wizard-back]');
    }
    if (!clickable(b)) return; // left alone: a form doesn't submit through a disabled default button either
    e.preventDefault();
    b.click();
  });
})();
