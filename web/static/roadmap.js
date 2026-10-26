// Roadmap search: filters as you type, including older shipped items.
// Without JavaScript the same form searches on the server (?q=).
(() => {
  const form = document.getElementById('rm-search');
  const input = document.getElementById('rm-q');
  const board = document.getElementById('roadmap');
  if (!form || !input || !board) return;
  const empty = document.getElementById('rm-empty');
  const emptyQ = document.getElementById('rm-empty-q');
  const older = document.getElementById('rm-older');
  let showOlder = !older;

  function run() {
    const words = input.value.toLowerCase().split(/\s+/).filter(Boolean);
    let total = 0;
    board.querySelectorAll('.rm-col').forEach((col) => {
      let n = 0;
      col.querySelectorAll('.rm-item').forEach((el) => {
        const hit = words.every((w) => el.dataset.search.includes(w));
        const folded = el.hasAttribute('data-older') && !showOlder && !words.length;
        el.hidden = !hit || folded;
        if (hit) n++;
      });
      const c = col.querySelector('h2 .count');
      if (c) c.textContent = n;
      const none = col.querySelector('.rm-none');
      if (none) none.hidden = n > 0;
      total += n;
    });
    if (older) older.hidden = showOlder || words.length > 0;
    if (empty) { empty.hidden = !words.length || total > 0; emptyQ.textContent = input.value.trim(); }
    const url = new URL(location.href);
    if (words.length) url.searchParams.set('q', input.value.trim()); else url.searchParams.delete('q');
    url.searchParams.delete('all');
    history.replaceState(null, '', url);
  }

  // Columns that start empty need a placeholder that can be shown/hidden.
  board.querySelectorAll('.rm-col').forEach((col) => {
    if (!col.querySelector('ul') || col.querySelector('.rm-none')) return;
    const p = document.createElement('p');
    p.className = 'muted small rm-none'; p.textContent = 'No matches here.'; p.hidden = true;
    col.append(p);
  });
  let t;
  input.addEventListener('input', () => { clearTimeout(t); t = setTimeout(run, 80); });
  input.addEventListener('keydown', (e) => { if (e.key === 'Escape' && input.value) { input.value = ''; run(); } });
  form.addEventListener('submit', (e) => { e.preventDefault(); run(); });
  if (older) older.addEventListener('click', (e) => { e.preventDefault(); showOlder = true; run(); });
  if (input.value) run();
})();
