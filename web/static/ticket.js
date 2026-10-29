// Status track on the card page: click a lane, or drag the current one onto another.
(() => {
  const track = document.getElementById('track');
  if (!track) return;
  const id = +track.dataset.id;
  const hint = document.getElementById('track-hint');
  const statusLabel = document.getElementById('status-label');
  let busy = false;

  function land(lane) {
    lane.classList.remove('landed'); void lane.offsetWidth; lane.classList.add('landed');
  }

  async function moveTo(lane) {
    const current = track.querySelector('.lane.current');
    if (busy || lane === current) return;
    busy = true;
    // Optimistic: switch right away, roll back on error.
    current.classList.remove('current');
    current.setAttribute('aria-pressed', 'false');
    current.removeAttribute('draggable');
    lane.classList.add('current', 'saving');
    lane.draggable = true;
    lane.setAttribute('aria-pressed', 'true');
    land(lane);
    try {
      const res = await fetch('/admin/api/move', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
        body: JSON.stringify({ id, status: lane.dataset.status, order: [] }),
      });
      if (!res.ok) throw new Error(res.status);
      const label = lane.querySelector('.lane-label').textContent;
      hint.textContent = `Moved to ${label}.`;
      if (statusLabel) statusLabel.textContent = label;
    } catch {
      lane.classList.remove('current');
      lane.setAttribute('aria-pressed', 'false');
      lane.removeAttribute('draggable');
      current.draggable = true;
      current.classList.add('current');
      current.setAttribute('aria-pressed', 'true');
      land(current);
      hint.textContent = 'Moving failed. Please try again.';
    } finally {
      lane.classList.remove('saving');
      busy = false;
    }
  }

  track.addEventListener('click', (e) => {
    const lane = e.target.closest('.lane');
    if (lane) moveTo(lane);
  });

  track.addEventListener('dragstart', (e) => {
    if (!e.target.closest?.('.lane.current')) return e.preventDefault();
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', String(id));
    track.classList.add('dragging');
  });
  track.addEventListener('dragend', () => {
    track.classList.remove('dragging');
    track.querySelectorAll('.lane.over').forEach((l) => l.classList.remove('over'));
  });
  track.addEventListener('dragover', (e) => {
    const lane = e.target.closest('.lane');
    if (!lane) return;
    e.preventDefault();
    track.querySelectorAll('.lane.over').forEach((l) => l !== lane && l.classList.remove('over'));
    lane.classList.add('over');
  });
  track.addEventListener('dragleave', (e) => {
    const lane = e.target.closest('.lane');
    if (lane && !lane.contains(e.relatedTarget)) lane.classList.remove('over');
  });
  track.addEventListener('drop', (e) => {
    const lane = e.target.closest('.lane');
    if (!lane) return;
    e.preventDefault();
    lane.classList.remove('over');
    moveTo(lane);
  });
})();

// Long titles: the title box grows with its text, Enter saves.
document.querySelectorAll('textarea.autogrow').forEach((t) => {
  const fit = () => { t.style.height = 'auto'; t.style.height = t.scrollHeight + 2 + 'px'; };
  t.addEventListener('input', fit);
  t.addEventListener('keydown', (e) => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); t.form?.requestSubmit(); } });
  fit();
});
