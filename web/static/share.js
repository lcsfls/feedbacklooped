// Settings → Share & embed: build the prefilled link and the widget snippet.
(() => {
  const lg = document.getElementById('link-gen');
  if (lg) {
    const base = lg.dataset.base;
    const out = document.getElementById('lg-out');
    const update = () => {
      const q = new URLSearchParams();
      const cat = document.getElementById('lg-cat').value;
      const ctx = document.getElementById('lg-ctx').value.trim();
      const title = document.getElementById('lg-title').value.trim();
      if (cat) q.set('category', cat);
      if (ctx) q.set('context', ctx);
      if (title) q.set('title', title);
      out.value = base + '/' + (q.toString() ? '?' + q : '');
    };
    lg.addEventListener('input', update);
    update();
  }
  const sg = document.getElementById('snip-gen');
  if (sg) {
    const base = sg.dataset.base;
    const out = document.getElementById('sg-out');
    const esc = (s) => s.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;');
    const update = () => {
      const label = document.getElementById('sg-label').value.trim();
      const ctx = document.getElementById('sg-ctx').value.trim();
      const pos = document.getElementById('sg-pos').value;
      let tag = `<script src="${base}/widget.js" async`;
      if (label && label !== 'Feedback') tag += ` data-label="${esc(label)}"`;
      if (ctx) tag += ` data-context="${esc(ctx)}"`;
      if (pos === 'left') tag += ' data-position="left"';
      out.textContent = tag + '></' + 'script>';
    };
    sg.addEventListener('input', update);
    update();
  }
  document.querySelectorAll('[data-copy]').forEach((btn) => {
    btn.addEventListener('click', async () => {
      const el = document.getElementById(btn.dataset.copy);
      const text = el.value ?? el.textContent;
      try {
        await navigator.clipboard.writeText(text);
        const old = btn.textContent;
        btn.textContent = 'Copied ✓';
        setTimeout(() => { btn.textContent = old; }, 1400);
      } catch {
        if (el.select) el.select();
      }
    });
  });
})();
