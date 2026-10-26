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
