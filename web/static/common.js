// Small helpers for all pages (CSP disallows inline scripts).
document.addEventListener('submit', (e) => {
  const f = e.target.closest('form.confirm');
  if (f && !confirm(f.dataset.confirm || 'Are you sure?')) e.preventDefault();
});
document.querySelectorAll('.copyline input[readonly]').forEach((el) => {
  el.addEventListener('focus', () => el.select());
});
