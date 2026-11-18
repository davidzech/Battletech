// Small helpers every page script shares. The layout loads this first, before any page's own scripts, so they can rely on it.
//
//   Dom.el(tag, cls, text)  a new element with an optional class and text (inserted as text, never HTML)
//   Dom.commas(n)           a whole number with thousands separators, e.g. 10000 as "10,000"
(function () {
  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }
  function commas(n) { return String(Math.round(Number(n) || 0)).replace(/\B(?=(\d{3})+(?!\d))/g, ','); }
  window.Dom = { el: el, commas: commas };
})();
