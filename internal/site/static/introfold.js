// The started campaign's intro is folded to a fixed height (style.css, .intro-collapsible) that fades out; this shows the Show more button when the text is
// taller than that, and opens and closes it with an animated height. An intro short enough to fit is marked .fits and shown whole, with no button.
(function () {
  const section = document.getElementById('campaign-intro');
  const text = document.getElementById('campaign-intro-text');
  const button = section && section.querySelector('.intro-more');
  if (!section || !text || !button) return;
  const still = () => window.matchMedia('(prefers-reduced-motion: reduce)').matches;

  // Whether the folded text hides anything; checked again when the width (and so the wrapping) changes.
  function measure() {
    if (section.classList.contains('open')) return;
    section.classList.remove('fits');
    const fits = text.scrollHeight <= text.clientHeight + 4;
    section.classList.toggle('fits', fits);
    button.hidden = fits;
  }

  // Animate max-height between the folded height and the text's full height; the inline value is dropped once open so the text can reflow freely.
  function setOpen(open) {
    button.setAttribute('aria-expanded', open ? 'true' : 'false');
    button.textContent = open ? 'Show less' : 'Show more';
    if (still()) {
      section.classList.toggle('open', open);
      text.style.maxHeight = open ? 'none' : '';
      return;
    }
    text.style.maxHeight = text.scrollHeight + 'px';
    if (open) {
      section.classList.add('open');
      text.addEventListener('transitionend', function done(e) {
        if (e.target !== text) return;
        text.removeEventListener('transitionend', done);
        if (section.classList.contains('open')) text.style.maxHeight = 'none';
      });
    } else {
      void text.offsetHeight; // start from the full height
      section.classList.remove('open');
      text.style.maxHeight = '';
      // Closing a long intro can leave the reader below it; bring its top back into view.
      const top = section.getBoundingClientRect().top;
      if (top < 0) section.scrollIntoView({ behavior: 'smooth', block: 'start' });
    }
  }

  button.addEventListener('click', () => setOpen(!section.classList.contains('open')));
  measure();
  window.addEventListener('resize', measure);
  if (document.fonts && document.fonts.ready) document.fonts.ready.then(measure);
})();
