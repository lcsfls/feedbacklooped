// Image markup editor: pen, box, circle, arrow and a "hide" tool to cover
// sensitive parts. window.markupImage(file) resolves with the edited PNG file,
// or null if the user cancels. Everything happens in the browser.
(() => {
  const COLORS = [
    ['#ef4444', 'Red'], ['#f59e0b', 'Yellow'], ['#3b82f6', 'Blue'], ['#22c55e', 'Green'], ['#111111', 'Black'],
  ];
  const TOOLS = [
    ['pen', 'Pen', 'P', '<path d="M4 20l4.5-1 10-10a2.1 2.1 0 0 0-3-3l-10 10L4 20z"/>'],
    ['rect', 'Box', 'B', '<rect x="4" y="6" width="16" height="12" rx="1.5"/>'],
    ['ellipse', 'Circle', 'C', '<ellipse cx="12" cy="12" rx="8.5" ry="6.5"/>'],
    ['arrow', 'Arrow', 'A', '<path d="M5 19L19 5M19 5h-8M19 5v8"/>'],
    ['hide', 'Hide (covers sensitive parts)', 'H', '<rect x="4" y="7" width="16" height="10" rx="1.5" fill="currentColor"/>'],
  ];
  const svg = (inner) => `<svg viewBox="0 0 24 24" aria-hidden="true">${inner}</svg>`;

  function loadImage(file) {
    return new Promise((resolve, reject) => {
      const url = URL.createObjectURL(file);
      const img = new Image();
      img.onload = () => resolve({ img, url });
      img.onerror = () => { URL.revokeObjectURL(url); reject(new Error('image')); };
      img.src = url;
    });
  }

  window.markupImage = async (file) => {
    const { img, url } = await loadImage(file);
    return new Promise((resolve) => {
      const W = img.naturalWidth, H = img.naturalHeight;
      const base = Math.max(W, H);
      const lineW = Math.max(3, Math.round(base / 260));

      const dlg = document.createElement('div');
      dlg.className = 'mk';
      dlg.setAttribute('role', 'dialog');
      dlg.setAttribute('aria-modal', 'true');
      dlg.setAttribute('aria-label', 'Mark up image');
      dlg.innerHTML = `
        <div class="mk-panel">
          <div class="mk-actions">
            <strong class="mk-title">Mark up</strong>
            <div class="mk-group mk-zoom" aria-label="Zoom">
              <button type="button" class="mk-btn" data-act="zoom-out" title="Zoom out (−)">${svg('<path d="M6 12h12"/>')}</button>
              <button type="button" class="mk-zoom-label" data-act="zoom-toggle" title="Fit / 100 % (0)">Fit</button>
              <button type="button" class="mk-btn" data-act="zoom-in" title="Zoom in (+)">${svg('<path d="M6 12h12M12 6v12"/>')}</button>
            </div>
            <div class="mk-spacer"></div>
            <button type="button" class="btn ghost" data-act="cancel">Cancel</button>
            <button type="button" class="btn primary" data-act="done">Use image</button>
          </div>
          <div class="mk-tools-bar">
            <div class="mk-group mk-tools" role="radiogroup" aria-label="Tool">
              ${TOOLS.map(([id, label, key, icon]) => `<button type="button" class="mk-btn" data-tool="${id}" role="radio" aria-checked="false" title="${label} (${key})" aria-label="${label}">${svg(icon)}</button>`).join('')}
            </div>
            <div class="mk-group mk-colors" role="radiogroup" aria-label="Color">
              ${COLORS.map(([c, label]) => `<button type="button" class="mk-swatch" data-color="${c}" role="radio" aria-checked="false" title="${label}" aria-label="${label}" style="--c:${c}"></button>`).join('')}
            </div>
            <div class="mk-group">
              <button type="button" class="mk-btn" data-act="undo" title="Undo (Ctrl+Z)" aria-label="Undo">${svg('<path d="M9 7L4 12l5 5M4 12h11a5 5 0 0 1 0 10h-2"/>')}</button>
              <button type="button" class="mk-btn" data-act="clear" title="Remove all markings" aria-label="Remove all markings">${svg('<path d="M5 7h14M10 7V5h4v2M7 7l1 12h8l1-12"/>')}</button>
            </div>
            <p class="mk-hint">Use <strong>Hide</strong> to cover passwords or names. Two fingers or Ctrl + scroll to zoom.</p>
          </div>
          <div class="mk-stage"><div class="mk-wrap"><canvas class="mk-canvas"></canvas></div></div>
        </div>`;
      document.body.append(dlg);
      document.documentElement.classList.add('mk-open');

      const canvas = dlg.querySelector('canvas');
      canvas.width = W;
      canvas.height = H;
      const ctx = canvas.getContext('2d');
      const ops = [];
      let tool = 'rect', color = COLORS[0][0], current = null;

      const select = (sel, attr, val) => dlg.querySelectorAll(sel).forEach((b) => {
        const on = b.dataset[attr] === val;
        b.classList.toggle('on', on);
        b.setAttribute('aria-checked', on);
      });
      const setTool = (t) => { tool = t; select('[data-tool]', 'tool', t); };
      const setColor = (c) => { color = c; select('[data-color]', 'color', c); };
      setTool(tool);
      setColor(color);

      function drawOp(o) {
        ctx.save();
        ctx.strokeStyle = o.c;
        ctx.fillStyle = o.c;
        ctx.lineWidth = o.w;
        ctx.lineCap = 'round';
        ctx.lineJoin = 'round';
        const x = Math.min(o.x1, o.x2), y = Math.min(o.y1, o.y2);
        const w = Math.abs(o.x2 - o.x1), h = Math.abs(o.y2 - o.y1);
        if (o.t === 'pen') {
          ctx.beginPath();
          o.pts.forEach(([px, py], i) => (i ? ctx.lineTo(px, py) : ctx.moveTo(px, py)));
          ctx.stroke();
        } else if (o.t === 'rect') {
          ctx.strokeRect(x, y, w, h);
        } else if (o.t === 'ellipse') {
          ctx.beginPath();
          ctx.ellipse(x + w / 2, y + h / 2, w / 2, h / 2, 0, 0, Math.PI * 2);
          ctx.stroke();
        } else if (o.t === 'arrow') {
          const ang = Math.atan2(o.y2 - o.y1, o.x2 - o.x1);
          const head = o.w * 4.5;
          ctx.beginPath();
          ctx.moveTo(o.x1, o.y1);
          ctx.lineTo(o.x2 - Math.cos(ang) * head * 0.6, o.y2 - Math.sin(ang) * head * 0.6);
          ctx.stroke();
          ctx.beginPath();
          ctx.moveTo(o.x2, o.y2);
          ctx.lineTo(o.x2 - head * Math.cos(ang - 0.45), o.y2 - head * Math.sin(ang - 0.45));
          ctx.lineTo(o.x2 - head * Math.cos(ang + 0.45), o.y2 - head * Math.sin(ang + 0.45));
          ctx.closePath();
          ctx.fill();
        } else if (o.t === 'hide') {
          // Solid fill, not blur/pixelation: those can be reversed.
          ctx.fillStyle = '#111111';
          ctx.fillRect(x, y, w, h);
        }
        ctx.restore();
      }

      function redraw() {
        ctx.drawImage(img, 0, 0, W, H);
        ops.forEach(drawOp);
        if (current) drawOp(current);
      }
      redraw();

      // --- zoom: "fit" fills the stage (up to 2× for small images), or a free factor
      const stage = dlg.querySelector('.mk-stage');
      const wrap = dlg.querySelector('.mk-wrap');
      const zoomLabel = dlg.querySelector('.mk-zoom-label');
      let zoom = 1, fit = true;
      const fitZoom = () => Math.min((stage.clientWidth - 16) / W, (stage.clientHeight - 16) / H, 2);
      function applyZoom(z, cx, cy) {
        // Keep the point under (cx, cy) – stage coordinates – in place.
        const r = stage.getBoundingClientRect();
        cx = cx ?? r.width / 2;
        cy = cy ?? r.height / 2;
        const px = (stage.scrollLeft + cx - wrap.offsetLeft) / zoom;
        const py = (stage.scrollTop + cy - wrap.offsetTop) / zoom;
        zoom = Math.min(8, Math.max(0.05, z));
        canvas.style.width = Math.round(W * zoom) + 'px';
        canvas.style.height = Math.round(H * zoom) + 'px';
        stage.scrollLeft = px * zoom + wrap.offsetLeft - cx;
        stage.scrollTop = py * zoom + wrap.offsetTop - cy;
        zoomLabel.textContent = fit ? 'Fit' : Math.round(zoom * 100) + ' %';
      }
      const setFit = () => { fit = true; applyZoom(fitZoom()); };
      const zoomBy = (f, cx, cy) => { fit = false; applyZoom(zoom * f, cx, cy); };
      requestAnimationFrame(setFit);
      const onResize = () => { if (fit) setFit(); };
      window.addEventListener('resize', onResize);
      stage.addEventListener('wheel', (e) => {
        if (!e.ctrlKey && !e.metaKey) return; // plain wheel scrolls
        e.preventDefault();
        const r = stage.getBoundingClientRect();
        zoomBy(e.deltaY < 0 ? 1.15 : 1 / 1.15, e.clientX - r.left, e.clientY - r.top);
      }, { passive: false });

      // --- drawing (one pointer) and pinch/pan (two pointers)
      const pos = (e) => {
        const r = canvas.getBoundingClientRect();
        return [(e.clientX - r.left) * (W / r.width), (e.clientY - r.top) * (H / r.height)];
      };
      const pointers = new Map();
      let gesture = null;
      const mid = () => {
        const [a, b] = [...pointers.values()];
        return { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2, d: Math.hypot(a.x - b.x, a.y - b.y) || 1 };
      };
      stage.addEventListener('pointerdown', (e) => {
        if (e.button > 0 && e.pointerType === 'mouse') return;
        pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
        if (pointers.size === 2) {
          current = null; // second finger: this is a gesture, not a stroke
          redraw();
          const m = mid();
          gesture = { ...m, zoom, sl: stage.scrollLeft, st: stage.scrollTop };
          return;
        }
        if (pointers.size > 2 || e.target !== canvas) return;
        e.preventDefault();
        canvas.setPointerCapture(e.pointerId);
        const [x, y] = pos(e);
        current = tool === 'pen'
          ? { t: 'pen', pts: [[x, y]], c: color, w: lineW }
          : { t: tool, x1: x, y1: y, x2: x, y2: y, c: color, w: lineW };
        redraw();
      });
      stage.addEventListener('pointermove', (e) => {
        if (!pointers.has(e.pointerId)) return;
        pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
        if (gesture && pointers.size === 2) {
          const m = mid();
          const r = stage.getBoundingClientRect();
          fit = false;
          stage.scrollLeft = gesture.sl - (m.x - gesture.x);
          stage.scrollTop = gesture.st - (m.y - gesture.y);
          applyZoom(gesture.zoom * (m.d / gesture.d), m.x - r.left, m.y - r.top);
          gesture = { ...m, zoom, sl: stage.scrollLeft, st: stage.scrollTop };
          return;
        }
        if (!current) return;
        const [x, y] = pos(e);
        if (current.t === 'pen') current.pts.push([x, y]);
        else { current.x2 = x; current.y2 = y; }
        redraw();
      });
      const finish = (e) => {
        pointers.delete(e.pointerId);
        if (pointers.size < 2) gesture = null;
        if (!current) return;
        const tiny = current.t !== 'pen' && Math.abs(current.x2 - current.x1) < 3 && Math.abs(current.y2 - current.y1) < 3;
        if (!tiny) ops.push(current);
        current = null;
        redraw();
      };
      stage.addEventListener('pointerup', finish);
      stage.addEventListener('pointercancel', finish);

      const close = (result) => {
        document.removeEventListener('keydown', onKey, true);
        window.removeEventListener('resize', onResize);
        document.documentElement.classList.remove('mk-open');
        dlg.classList.add('closing');
        setTimeout(() => dlg.remove(), 180);
        URL.revokeObjectURL(url);
        resolve(result);
      };
      const done = () => {
        if (!ops.length) { close(file); return; }
        canvas.toBlob((blob) => {
          const name = file.name.replace(/\.[a-z0-9]+$/i, '') + '-marked.png';
          close(new File([blob], name, { type: 'image/png' }));
        }, 'image/png');
      };

      dlg.addEventListener('click', (e) => {
        const b = e.target.closest('button');
        if (!b) return;
        if (b.dataset.tool) setTool(b.dataset.tool);
        else if (b.dataset.color) setColor(b.dataset.color);
        else if (b.dataset.act === 'undo') { ops.pop(); redraw(); }
        else if (b.dataset.act === 'clear') { ops.length = 0; redraw(); }
        else if (b.dataset.act === 'cancel') close(null);
        else if (b.dataset.act === 'done') done();
        else if (b.dataset.act === 'zoom-in') zoomBy(1.25);
        else if (b.dataset.act === 'zoom-out') zoomBy(1 / 1.25);
        else if (b.dataset.act === 'zoom-toggle') { if (fit) { fit = false; applyZoom(1); } else setFit(); }
      });
      function onKey(e) {
        if (e.key === 'Escape') { e.preventDefault(); close(null); return; }
        if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'z') { e.preventDefault(); ops.pop(); redraw(); return; }
        if (e.key === 'Enter' && !e.target.closest('button')) { e.preventDefault(); done(); return; }
        if (e.key === '+' || e.key === '=') { e.preventDefault(); zoomBy(1.25); return; }
        if (e.key === '-') { e.preventDefault(); zoomBy(1 / 1.25); return; }
        if (e.key === '0') { e.preventDefault(); if (fit) { fit = false; applyZoom(1); } else setFit(); return; }
        const t = TOOLS.find((x) => x[2].toLowerCase() === e.key.toLowerCase());
        if (t && !e.ctrlKey && !e.metaKey) setTool(t[0]);
      }
      document.addEventListener('keydown', onKey, true);
      dlg.querySelector('[data-act=done]').focus();
    });
  };

  // Grab a single frame of a tab, window or screen.
  window.captureScreenshot = async () => {
    // No surface preference: the browser offers tabs, windows and screens.
    // The current tab is listed too (selfBrowserSurface), but not preselected.
    const stream = await navigator.mediaDevices.getDisplayMedia({
      video: { cursor: 'never' }, audio: false,
      selfBrowserSurface: 'include', surfaceSwitching: 'exclude', monitorTypeSurfaces: 'include',
    });
    try {
      const video = document.createElement('video');
      video.muted = true;
      video.playsInline = true;
      video.srcObject = stream;
      await video.play();
      // Give the capture a moment so the browser's sharing prompt is gone.
      await new Promise((r) => setTimeout(r, 350));
      const c = document.createElement('canvas');
      c.width = video.videoWidth;
      c.height = video.videoHeight;
      c.getContext('2d').drawImage(video, 0, 0);
      const blob = await new Promise((r) => c.toBlob(r, 'image/png'));
      return blob;
    } finally {
      stream.getTracks().forEach((t) => t.stop());
    }
  };
})();
