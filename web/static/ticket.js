// Status track on the card page: drag the token into a lane, or click a lane.
(() => {
  const track = document.getElementById('track');
  if (!track) return;
  const id = +track.dataset.id;
  const hint = document.getElementById('track-hint');
  const statusLabel = document.getElementById('status-label');
  let token = document.getElementById('token');
  let busy = false;

  async function moveTo(lane) {
    const current = track.querySelector('.lane.current');
    if (busy || lane === current) return;
    busy = true;
    // Optimistic: move the token right away, roll back on error.
    current.classList.remove('current');
    current.setAttribute('aria-pressed', 'false');
    lane.classList.add('current', 'saving');
    lane.setAttribute('aria-pressed', 'true');
    lane.append(token);
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
      current.classList.add('current');
      current.setAttribute('aria-pressed', 'true');
      current.append(token);
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

  token.addEventListener('dragstart', (e) => {
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', String(id));
    track.classList.add('dragging');
  });
  token.addEventListener('dragend', () => {
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
