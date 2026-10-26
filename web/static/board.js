(() => {
  const board = document.getElementById('board');
  if (!board) return;

  // Filtering
  const q = document.getElementById('q');
  const cat = document.getElementById('cat');
  function filter() {
    const term = q.value.trim().toLowerCase();
    const c = cat.value;
    board.querySelectorAll('.kcard').forEach((el) => {
      const hit = (!term || el.dataset.search.toLowerCase().includes(term)) && (!c || el.dataset.cat === c);
      el.hidden = !hit;
    });
    counts();
  }
  function counts() {
    board.querySelectorAll('.col').forEach((col) => {
      col.querySelector('.count').textContent = col.querySelectorAll('.kcard:not([hidden])').length;
    });
  }
  q.addEventListener('input', filter);
  cat.addEventListener('change', filter);

  // Drag & Drop
  let dragged = null;
  let justDragged = false;

  board.addEventListener('dragstart', (e) => {
    const card = e.target.closest('.kcard');
    if (!card) return;
    dragged = card;
    card.classList.add('dragging');
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', card.dataset.id);
  });

  board.addEventListener('dragend', () => {
    if (!dragged) return;
    dragged.classList.remove('dragging');
    board.querySelectorAll('.cards.over').forEach((l) => l.classList.remove('over'));
    dragged = null;
    justDragged = true;
    setTimeout(() => { justDragged = false; }, 50);
  });

  function afterElement(list, y) {
    let closest = null;
    let best = -Infinity;
    list.querySelectorAll('.kcard:not(.dragging):not([hidden])').forEach((el) => {
      const box = el.getBoundingClientRect();
      const off = y - box.top - box.height / 2;
      if (off < 0 && off > best) { best = off; closest = el; }
    });
    return closest;
  }

  board.addEventListener('dragover', (e) => {
    const list = e.target.closest('.col')?.querySelector('.cards');
    if (!list || !dragged) return;
    e.preventDefault();
    board.querySelectorAll('.cards.over').forEach((l) => l !== list && l.classList.remove('over'));
    list.classList.add('over');
    const after = afterElement(list, e.clientY);
    if (after) list.insertBefore(dragged, after);
    else list.appendChild(dragged);
  });

  board.addEventListener('drop', async (e) => {
    const list = e.target.closest('.col')?.querySelector('.cards');
    if (!list || !dragged) return;
    e.preventDefault();
    const card = dragged;
    const order = [...list.querySelectorAll('.kcard')].map((el) => +el.dataset.id);
    counts();
    try {
      const res = await fetch('/admin/api/move', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
        body: JSON.stringify({ id: +card.dataset.id, status: list.dataset.status, order }),
      });
      if (!res.ok) throw new Error(res.status);
    } catch {
      alert('Moving the card failed, reloading the page.');
      location.reload();
    }
  });

  // Don't trigger the card link right after a drag
  board.addEventListener('click', (e) => {
    if (justDragged && e.target.closest('.kcard')) e.preventDefault();
  });
})();
