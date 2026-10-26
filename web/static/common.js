// Small helpers for all pages (CSP disallows inline scripts).
document.addEventListener('submit', (e) => {
  const f = e.target.closest('form.confirm');
  if (f && !confirm(f.dataset.confirm || 'Are you sure?')) e.preventDefault();
});
document.querySelectorAll('.copyline input[readonly]').forEach((el) => {
  el.addEventListener('focus', () => el.select());
});
// Insert a text template into a textarea (settings: privacy policy).
document.querySelectorAll('button[data-template]').forEach((btn) => {
  btn.addEventListener('click', () => {
    const ta = document.getElementById(btn.dataset.target);
    const tpl = document.getElementById(btn.dataset.template);
    if (!ta || !tpl) return;
    if (ta.value.trim() && !confirm('Replace the current text with the template?')) return;
    ta.value = tpl.content.textContent;
    ta.focus();
  });
});

const reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

// Segmented control: liquid blob. The leading edge moves first, the trailing
// edge follows a beat later, so the blob stretches and then settles.
document.querySelectorAll('.segmented').forEach((seg) => {
  const blob = document.createElement('span');
  blob.className = 'seg-blob';
  blob.setAttribute('aria-hidden', 'true');
  seg.prepend(blob);
  seg.classList.add('js');
  let lastLeft = null;
  const place = (animate) => {
    const span = seg.querySelector('input:checked + span');
    if (!span) return;
    const s = seg.getBoundingClientRect();
    const r = span.getBoundingClientRect();
    const left = r.left - s.left;
    const right = s.right - r.right;
    const forward = lastLeft === null || left >= lastLeft;
    blob.style.setProperty('--dl', animate && forward ? '.07s' : '0s');
    blob.style.setProperty('--dr', animate && !forward ? '.07s' : '0s');
    if (!animate) blob.style.transition = 'none';
    blob.style.setProperty('--l', left + 'px');
    blob.style.setProperty('--r', right + 'px');
    if (!animate) { blob.offsetWidth; blob.style.transition = ''; }
    if (animate && !reduceMotion) {
      blob.classList.remove('squish'); blob.offsetWidth; blob.classList.add('squish');
    }
    lastLeft = left;
  };
  place(false);
  seg.addEventListener('change', () => place(true));
  window.addEventListener('resize', () => place(false));
  document.fonts?.ready.then(() => place(false));
});

// Toast: flash messages leave on their own after a few seconds.
const flash = document.querySelector('.flash');
if (flash) {
  setTimeout(() => {
    flash.classList.add('out');
    flash.addEventListener('animationend', () => flash.remove(), { once: true });
  }, 3200);
}

// FLIP helper: animate elements from where they were to where they are now.
window.flip = (els, mutate) => {
  if (reduceMotion) { mutate(); return; }
  const before = new Map([...els].map((el) => [el, el.getBoundingClientRect()]));
  mutate();
  before.forEach((r, el) => {
    const n = el.getBoundingClientRect();
    const dx = r.left - n.left, dy = r.top - n.top;
    if (dx || dy) el.animate([{ transform: `translate(${dx}px, ${dy}px)` }, { transform: 'none' }], { duration: 260, easing: 'cubic-bezier(.22,1,.36,1)' });
  });
};
