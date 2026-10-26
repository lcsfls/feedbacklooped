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
      const tc = document.querySelector(`.rm-tab[data-col="${col.dataset.col}"] .count`);
      if (tc) tc.textContent = n;
      const none = col.querySelector('.rm-none');
      if (none) none.hidden = n > 0;
      total += n;
    });
    if (older) older.hidden = showOlder || words.length > 0;
    // Phones show one column at a time: jump to the first one with hits.
    if (words.length && total && getComputedStyle(board).overflowX === 'auto') {
      const visible = [...board.querySelectorAll('.rm-col')].find((c) => {
        const x = c.offsetLeft - board.offsetLeft - board.scrollLeft;
        return x > -c.offsetWidth / 2 && x < c.offsetWidth / 2;
      });
      const hasHits = (c) => c && c.querySelector('.rm-item:not([hidden])');
      if (!hasHits(visible)) {
        const target = [...board.querySelectorAll('.rm-col')].find(hasHits);
        if (target) board.scrollTo({ left: target.offsetLeft - board.offsetLeft, behavior: 'smooth' });
      }
    }
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

  // Phones: columns sit side by side and swipe; the tabs jump and follow.
  const tabs = document.getElementById('rm-tabs');
  if (tabs) {
    const setActive = (key) => tabs.querySelectorAll('.rm-tab').forEach((t) => {
      const on = t.dataset.col === key;
      t.classList.toggle('active', on);
      if (on) t.setAttribute('aria-current', 'true'); else t.removeAttribute('aria-current');
      if (on && tabs.scrollWidth > tabs.clientWidth) t.scrollIntoView({ block: 'nearest', inline: 'nearest' });
    });
    tabs.addEventListener('click', (e) => {
      const t = e.target.closest('.rm-tab');
      if (!t) return;
      e.preventDefault();
      const col = document.getElementById('col-' + t.dataset.col);
      board.scrollTo({ left: col.offsetLeft - board.offsetLeft, behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' });
      setActive(t.dataset.col);
    });
    const io = new IntersectionObserver((entries) => {
      entries.forEach((en) => { if (en.isIntersecting) setActive(en.target.dataset.col); });
    }, { root: board, threshold: 0.6 });
    board.querySelectorAll('.rm-col').forEach((c) => io.observe(c));
    // Start on the first column that has something in it.
    const first = [...board.querySelectorAll('.rm-col')].find((c) => c.querySelector('.rm-item:not([hidden])'));
    setActive(first ? first.dataset.col : board.querySelector('.rm-col').dataset.col);
    if (first && getComputedStyle(board).overflowX === 'auto') board.scrollLeft = first.offsetLeft - board.offsetLeft;
  }
})();

// Open roadmap items in a modal: details on the left, the public discussion
// on the right. The address follows (/roadmap/{id}), so links, Back and
// reload all work; without JavaScript the link opens the item page.
(() => {
  const modal = document.getElementById('rm-modal');
  const body = document.getElementById('rm-modal-body');
  if (!modal || !body || typeof modal.showModal !== 'function') return;
  let listTitle = document.title;
  let seq = 0;

  async function load(id) {
    const my = ++seq;
    body.innerHTML = '<p class="muted rm-loading">Loading …</p>';
    if (!modal.open) modal.showModal();
    document.documentElement.classList.add('modal-open');
    try {
      const res = await fetch('/roadmap/' + encodeURIComponent(id), { cache: 'no-store' });
      if (my !== seq) return;
      if (!res.ok) throw new Error();
      const doc = new DOMParser().parseFromString(await res.text(), 'text/html');
      const d = doc.getElementById('rm-detail');
      if (!d) throw new Error();
      body.replaceChildren(d);
      document.title = doc.title;
      const close = modal.querySelector('[data-rm-close]');
      if (close) close.focus({ preventScroll: true });
    } catch (e) {
      if (my === seq) body.innerHTML = '<p class="muted rm-loading">This item is no longer on the roadmap.</p>';
    }
  }

  document.addEventListener('click', (e) => {
    const a = e.target.closest('[data-rm-open]');
    if (!a || e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    e.preventDefault();
    if (!modal.open) listTitle = document.title;
    history.pushState({ rm: a.dataset.rmOpen }, '', a.getAttribute('href'));
    load(a.dataset.rmOpen);
  });

  modal.addEventListener('close', () => {
    seq++;
    document.documentElement.classList.remove('modal-open');
    document.title = listTitle;
    body.replaceChildren();
    if (history.state && history.state.rm) history.back();
  });
  // Click on the dimmed backdrop closes it.
  modal.addEventListener('click', (e) => { if (e.target === modal) modal.close(); });
  modal.querySelector('[data-rm-close]').addEventListener('click', () => modal.close());
  addEventListener('popstate', (e) => {
    if (e.state && e.state.rm) load(e.state.rm);
    else if (modal.open) modal.close();
  });
})();
