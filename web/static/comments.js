// Public comments on roadmap items: sent with fetch so the page (or the
// modal) stays where it is. Without JavaScript the form posts normally.
(() => {
  async function refresh(form) {
    const detail = form.closest('#rm-detail');
    if (!detail) return location.reload();
    const res = await fetch('/roadmap/' + detail.dataset.id, { cache: 'no-store' });
    if (!res.ok) return location.reload();
    const doc = new DOMParser().parseFromString(await res.text(), 'text/html');
    const fresh = doc.getElementById('rm-detail');
    if (!fresh) return;
    detail.replaceWith(fresh);
    document.dispatchEvent(new CustomEvent('rm:detail', { detail: fresh }));
    const thread = fresh.querySelector('#rm-thread');
    const last = thread && thread.lastElementChild;
    if (last) {
      last.classList.add('just-posted');
      last.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
    }
  }

  document.addEventListener('submit', async (e) => {
    const form = e.target;
    if (!form.matches('form.comment-form[data-ajax]')) return;
    e.preventDefault();
    const btn = form.querySelector('button');
    const err = form.querySelector('.error');
    err.hidden = true;
    btn.disabled = true;
    const label = btn.textContent;
    try {
      if (form.hasAttribute('data-pow')) {
        if (!form.powReady && window.powSetup) window.powSetup(form);
        if (form.powReady) { btn.textContent = 'Checking …'; await form.powReady(); }
      }
      btn.textContent = 'Posting …';
      const res = await fetch(form.action, { method: 'POST', body: new URLSearchParams(new FormData(form)), headers: { Accept: 'application/json' } });
      const data = await res.json().catch(() => ({}));
      if (!res.ok || !data.ok) throw new Error(data.error || 'Posting failed. Please try again.');
      await refresh(form);
    } catch (ex) {
      err.textContent = ex.message || 'Posting failed. Please try again.';
      err.hidden = false;
      btn.disabled = false;
      btn.textContent = label;
    }
  });

  // Start the spam check as soon as someone starts typing a comment.
  document.addEventListener('focusin', (e) => {
    const form = e.target.closest && e.target.closest('form.comment-form[data-pow]');
    if (form && !form.powReady && window.powSetup) { window.powSetup(form); form.powReady().catch(() => {}); }
  });
})();
