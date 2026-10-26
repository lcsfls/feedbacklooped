(() => {
  const form = document.getElementById('fb-form');
  if (!form) return;
  const maxFiles = +form.dataset.maxFiles || 10;
  const maxBytes = (+form.dataset.maxMb || 100) * 1024 * 1024;
  const maxSec = +form.dataset.maxSec || 180;

  const input = document.getElementById('files');
  const drop = document.getElementById('drop');
  const list = document.getElementById('filelist');
  const errEl = document.getElementById('err');
  const sendBtn = document.getElementById('send');
  const progress = document.getElementById('progress');
  const bar = document.getElementById('bar');
  let files = [];

  const fmtSize = (n) => n >= 1048576 ? (n / 1048576).toFixed(1) + ' MB' : Math.max(1, Math.round(n / 1024)) + ' KB';
  const stamp = () => {
    const d = new Date();
    const p = (x) => String(x).padStart(2, '0');
    return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}_${p(d.getHours())}${p(d.getMinutes())}${p(d.getSeconds())}`;
  };

  function showError(msg) {
    errEl.textContent = msg;
    errEl.hidden = !msg;
  }

  function addFiles(newFiles) {
    showError('');
    for (const f of newFiles) {
      if (files.length >= maxFiles) {
        showError(`Maximum ${maxFiles} files.`);
        break;
      }
      if (f.size > maxBytes) {
        showError(`“${f.name}” is too large (max. ${form.dataset.maxMb} MB).`);
        continue;
      }
      const ok = /^(image|video)\//.test(f.type) || /\.(txt|log)$/i.test(f.name);
      if (!ok) {
        showError(`“${f.name}”: file type not allowed.`);
        continue;
      }
      files.push(f);
    }
    render();
  }

  function render() {
    list.querySelectorAll('[data-url]').forEach((el) => URL.revokeObjectURL(el.dataset.url));
    list.innerHTML = '';
    files.forEach((f, i) => {
      const li = document.createElement('li');
      let thumb;
      if (f.type.startsWith('image/')) {
        thumb = document.createElement('img');
      } else if (f.type.startsWith('video/')) {
        thumb = document.createElement('video');
        thumb.muted = true;
        thumb.preload = 'metadata';
      } else {
        thumb = document.createElement('span');
        thumb.className = 'ficon';
        thumb.textContent = '📄';
      }
      if (thumb.tagName !== 'SPAN') {
        const url = URL.createObjectURL(f);
        thumb.src = url;
        thumb.dataset.url = url;
      }
      thumb.classList.add('thumb');
      const name = document.createElement('span');
      name.className = 'fname';
      name.textContent = f.name;
      const size = document.createElement('span');
      size.className = 'fsize';
      size.textContent = fmtSize(f.size);
      const rm = document.createElement('button');
      rm.type = 'button';
      rm.className = 'linklike danger';
      rm.textContent = 'remove';
      rm.addEventListener('click', () => { files.splice(i, 1); render(); });
      li.append(thumb, name, size, rm);
      list.append(li);
    });
  }

  input.addEventListener('change', () => {
    addFiles([...input.files]);
    input.value = '';
  });
  ['dragenter', 'dragover'].forEach((ev) => drop.addEventListener(ev, (e) => {
    e.preventDefault();
    drop.classList.add('over');
  }));
  ['dragleave', 'drop'].forEach((ev) => drop.addEventListener(ev, () => drop.classList.remove('over')));
  drop.addEventListener('drop', (e) => {
    e.preventDefault();
    addFiles([...e.dataTransfer.files]);
  });

  // Screenshot from the clipboard (Ctrl+V anywhere on the page)
  document.addEventListener('paste', (e) => {
    const items = [...(e.clipboardData?.items || [])].filter((it) => it.kind === 'file');
    if (!items.length) return;
    e.preventDefault();
    addFiles(items.map((it) => {
      const f = it.getAsFile();
      const ext = (f.type.split('/')[1] || 'png').replace('jpeg', 'jpg');
      return new File([f], `screenshot-${stamp()}.${ext}`, { type: f.type });
    }));
  });

  // Screen recording
  const recRow = document.getElementById('rec-row');
  const recBtn = document.getElementById('rec-btn');
  const recLabel = document.getElementById('rec-label');
  if (navigator.mediaDevices?.getDisplayMedia && window.MediaRecorder) {
    recRow.hidden = false;
    let recorder = null;
    let timer = null;

    const pickType = () => ['video/webm;codecs=vp9', 'video/webm;codecs=vp8', 'video/webm', 'video/mp4']
      .find((t) => MediaRecorder.isTypeSupported(t)) || '';

    async function start() {
      showError('');
      let stream;
      try {
        stream = await navigator.mediaDevices.getDisplayMedia({ video: { frameRate: 15 }, audio: false });
      } catch {
        return; // cancelled
      }
      const type = pickType();
      const chunks = [];
      recorder = new MediaRecorder(stream, { mimeType: type || undefined, videoBitsPerSecond: 1_500_000 });
      recorder.ondataavailable = (e) => e.data.size && chunks.push(e.data);
      recorder.onstop = () => {
        stream.getTracks().forEach((t) => t.stop());
        clearInterval(timer);
        recBtn.classList.remove('recording');
        recLabel.textContent = 'Record screen';
        const mime = (recorder.mimeType || type || 'video/webm').split(';')[0];
        const ext = mime === 'video/mp4' ? 'mp4' : 'webm';
        const blob = new Blob(chunks, { type: mime });
        recorder = null;
        if (blob.size) addFiles([new File([blob], `screen-recording-${stamp()}.${ext}`, { type: mime })]);
      };
      // User stops sharing via the browser's own button
      stream.getVideoTracks()[0].addEventListener('ended', () => recorder?.state === 'recording' && recorder.stop());
      recorder.start(1000);
      const t0 = Date.now();
      recBtn.classList.add('recording');
      const tick = () => {
        const s = Math.floor((Date.now() - t0) / 1000);
        recLabel.textContent = `Stop recording (${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')})`;
        if (s >= maxSec && recorder?.state === 'recording') recorder.stop();
      };
      tick();
      timer = setInterval(tick, 500);
    }

    recBtn.addEventListener('click', () => {
      if (recorder?.state === 'recording') recorder.stop();
      else start();
    });
  }

  // Submit via XHR (for the progress bar)
  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    showError('');
    sendBtn.disabled = true;
    if (form.powReady) {
      sendBtn.textContent = 'Checking …';
      try {
        await form.powReady();
      } catch {
        sendBtn.disabled = false;
        sendBtn.textContent = 'Send feedback';
        showError('The spam check could not be completed. Please reload the page and try again.');
        return;
      }
    }
    const fd = new FormData(form);
    fd.delete('files');
    files.forEach((f) => fd.append('files', f, f.name));
    const xhr = new XMLHttpRequest();
    xhr.open('POST', form.action);
    xhr.setRequestHeader('Accept', 'application/json');
    const hasFiles = files.length > 0;
    sendBtn.disabled = true;
    sendBtn.textContent = 'Sending …';
    if (hasFiles) { progress.hidden = false; bar.style.width = '0%'; }
    xhr.upload.onprogress = (ev) => {
      if (ev.lengthComputable) bar.style.width = Math.round(ev.loaded / ev.total * 100) + '%';
    };
    const reset = () => {
      sendBtn.disabled = false;
      sendBtn.textContent = 'Send feedback';
      progress.hidden = true;
    };
    xhr.onerror = () => { reset(); showError('Connection failed. Please try again.'); };
    xhr.onload = () => {
      let res = {};
      try { res = JSON.parse(xhr.responseText); } catch { /* */ }
      if (xhr.status === 413) res.error = 'Upload too large for the server.';
      if (xhr.status !== 200 || !res.ok) {
        reset();
        showError(res.error || `Error ${xhr.status}. Please try again.`);
        return;
      }
      form.hidden = true;
      const done = document.getElementById('done');
      done.hidden = false;
      document.getElementById('status-url').value = res.status_url;
      document.getElementById('status-open').href = res.status_url;
      document.getElementById('copy').addEventListener('click', async (ev) => {
        try {
          await navigator.clipboard.writeText(res.status_url);
          ev.target.textContent = 'Copied ✓';
        } catch {
          document.getElementById('status-url').select();
        }
      });
      done.scrollIntoView({ behavior: 'smooth', block: 'start' });
    };
    xhr.send(fd);
  });
})();
