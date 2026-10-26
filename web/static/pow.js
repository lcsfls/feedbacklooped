// Proof-of-work spam check (ALTCHA-compatible). For every form with
// data-pow it fetches a challenge, finds the number n with
// sha256(salt + n) === challenge in the background and puts the solution
// into a hidden "altcha" field. Nothing leaves the browser except the result.
(() => {
  const forms = document.querySelectorAll('form[data-pow]');
  if (!forms.length || !window.crypto?.subtle) return;
  const enc = new TextEncoder();
  const hex = (buf) => [...new Uint8Array(buf)].map((b) => b.toString(16).padStart(2, '0')).join('');
  const MAX_AGE = 15 * 60 * 1000; // challenges expire after 20 min on the server

  async function solve() {
    const res = await fetch('/api/challenge', { cache: 'no-store' });
    if (!res.ok) throw new Error('challenge ' + res.status);
    const c = await res.json();
    for (let n = 0; n <= c.maxnumber; n++) {
      if (hex(await crypto.subtle.digest('SHA-256', enc.encode(c.salt + n))) === c.challenge) {
        return btoa(JSON.stringify({ algorithm: c.algorithm, challenge: c.challenge, number: n, salt: c.salt, signature: c.signature }));
      }
    }
    throw new Error('no solution');
  }

  forms.forEach((form) => {
    let field = form.querySelector('input[name=altcha]');
    if (!field) {
      field = document.createElement('input');
      field.type = 'hidden';
      field.name = 'altcha';
      form.append(field);
    }
    let job = null;
    let solvedAt = 0;
    const start = () => {
      if (job && Date.now() - solvedAt < MAX_AGE) return job;
      job = solve().then((v) => { field.value = v; solvedAt = Date.now(); return v; })
        .catch((e) => { job = null; throw e; });
      return job;
    };
    // Start as soon as the visitor shows interest, so it's done before they hit send.
    ['focusin', 'pointerdown', 'dragenter'].forEach((ev) => form.addEventListener(ev, () => start().catch(() => {}), { once: true }));
    // submit.js awaits this before sending via XHR.
    form.powReady = () => start();
    // Plain forms: hold the submit until the check is done.
    form.addEventListener('submit', (e) => {
      if (form.id === 'fb-form') return; // handled in submit.js
      if (field.value && Date.now() - solvedAt < MAX_AGE) return;
      e.preventDefault();
      const btn = form.querySelector('button:not([type=button])');
      if (btn) { btn.disabled = true; btn.dataset.label = btn.textContent; btn.textContent = 'Checking …'; }
      start().then(() => form.submit()).catch(() => {
        if (btn) { btn.disabled = false; btn.textContent = btn.dataset.label; }
        alert('The spam check could not be completed. Please reload the page and try again.');
      });
    });
  });
})();
