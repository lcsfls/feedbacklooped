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
  // leaves when its timer bar has run down (paused while the pointer is on it)
  flash.addEventListener('animationend', (e) => {
    if (e.animationName === 'toast-timer') flash.classList.add('out');
    else if (e.animationName === 'toast-out') flash.remove();
  });
  // a reload doesn't show the same message again
  const u = new URL(location.href);
  if (u.searchParams.has('msg')) { u.searchParams.delete('msg'); history.replaceState(null, '', u.pathname + u.search + u.hash); }
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

// Embedded in the widget: tell the host page to close the panel.
if (document.body.hasAttribute('data-embed') && window.parent !== window) {
  const closeEmbed = () => window.parent.postMessage({ type: 'feedbacklooped:close' }, '*');
  document.addEventListener('click', (e) => {
    if (e.target.closest('#embed-close, [data-embed-close]')) closeEmbed();
  });
  document.addEventListener('keydown', (e) => { if (e.key === 'Escape' && !document.querySelector('.mk')) closeEmbed(); });
}
// "Sent from" chip: let people drop the page/context before sending.
document.getElementById('sentfrom-remove')?.addEventListener('click', () => {
  const box = document.getElementById('sentfrom');
  box.querySelectorAll('input').forEach((i) => { i.value = ''; });
  box.classList.add('removed');
  setTimeout(() => box.remove(), 250);
});

// Checkboxes that save their form right away (e.g. sharing consent on the status page).
document.querySelectorAll('input[data-autosubmit]').forEach((el) => {
  el.addEventListener('change', () => el.form && el.form.requestSubmit());
});

// Theme toggle (system / light / dark). Stored in this browser only.
(() => {
  const root = document.documentElement;
  const meta = [...document.querySelectorAll('meta[name=theme-color]')];
  const current = () => root.dataset.theme || 'system';
  const isDark = (t) => t === 'dark' || (t === 'system' && matchMedia('(prefers-color-scheme: dark)').matches);
  function sync() {
    const t = current();
    const label = 'Theme: ' + t + ' (click to change)';
    document.querySelectorAll('[data-theme-toggle]').forEach((b) => { b.setAttribute('aria-label', label); b.title = label; });
    // Browser UI colour follows the chosen theme, not just the system.
    meta.forEach((m) => { m.removeAttribute('media'); m.content = isDark(t) ? '#0a0a0a' : '#fafafa'; });
  }
  function apply(t) {
    if (t === 'system') delete root.dataset.theme; else root.dataset.theme = t;
    try { if (t === 'system') localStorage.removeItem('fl-theme'); else localStorage.setItem('fl-theme', t); } catch (e) {}
    sync();
  }
  document.querySelectorAll('[data-theme-toggle]').forEach((btn) => {
    btn.addEventListener('click', () => {
      // First click always flips what you see: system → opposite → same as system → system.
      const sysDark = matchMedia('(prefers-color-scheme: dark)').matches;
      const flip = sysDark ? 'light' : 'dark', same = sysDark ? 'dark' : 'light';
      const cur = current();
      const next = cur === 'system' ? flip : cur === flip ? same : 'system';
      const reduce = matchMedia('(prefers-reduced-motion: reduce)').matches;
      if (document.startViewTransition && !reduce && isDark(next) !== isDark(current())) {
        document.startViewTransition(() => apply(next));
      } else {
        apply(next);
      }
    });
  });
  if (meta.length && root.dataset.theme) sync();
  else document.querySelectorAll('[data-theme-toggle]').forEach((b) => { const l = 'Theme: ' + current() + ' (click to change)'; b.setAttribute('aria-label', l); b.title = l; });
  // Other tabs: keep in step.
  addEventListener('storage', (e) => { if (e.key === 'fl-theme') { const t = e.newValue || 'system'; if (t === 'system') delete root.dataset.theme; else root.dataset.theme = t; sync(); } });
})();
