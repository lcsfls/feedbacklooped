// feedbacklooped widget. Wrapped by the server as (function(){var CFG={…}; <this> })();
// Usage: <script src="https://feedback.example.com/widget.js" async
//          data-label="Feedback" data-context="app 1.4" data-category="Bug"
//          data-position="right" data-button="true"></script>
// API:   window.Feedbacklooped.open({ context, category, title }) / .close()
var me = document.currentScript || document.querySelector('script[src*="/widget.js"]');
var opt = (me && me.dataset) || {};
if (!CFG.enabled) {
  console.warn('[feedbacklooped] Embedding is switched off. Add this site under Settings → Share & embed.');
  return;
}
if (window.Feedbacklooped) return; // loaded twice

var lum = function (hex) {
  var n = parseInt(hex.slice(1), 16);
  return (0.2126 * (n >> 16 & 255) + 0.7152 * (n >> 8 & 255) + 0.0722 * (n & 255)) / 255;
};
var fg = lum(CFG.color) > 0.6 ? '#171717' : '#ffffff';
var left = opt.position === 'left';

var css = [
  ':host{all:initial}',
  '*{box-sizing:border-box}',
  '.btn{position:fixed;bottom:20px;' + (left ? 'left' : 'right') + ':20px;z-index:2147483646;display:inline-flex;align-items:center;gap:8px;',
  'height:44px;padding:0 18px 0 14px;border:0;border-radius:999px;background:' + CFG.color + ';color:' + fg + ';',
  'font:600 14px/1 ui-sans-serif,system-ui,-apple-system,"Segoe UI",Roboto,sans-serif;cursor:pointer;',
  'box-shadow:0 6px 20px rgba(0,0,0,.18),0 1px 3px rgba(0,0,0,.12);transition:transform .25s cubic-bezier(.34,1.56,.64,1),opacity .2s}',
  '.btn:hover{transform:translateY(-2px)}.btn:active{transform:scale(.96)}',
  '.btn:focus-visible{outline:3px solid ' + CFG.color + ';outline-offset:3px}',
  '.btn svg{width:18px;height:18px;fill:none;stroke:currentColor;stroke-width:2;stroke-linejoin:round}',
  '.btn.hidden{opacity:0;pointer-events:none;transform:scale(.8)}',
  '.panel{position:fixed;bottom:20px;' + (left ? 'left' : 'right') + ':20px;z-index:2147483647;width:420px;height:min(720px,calc(100vh - 40px));',
  'max-width:calc(100vw - 40px);border-radius:16px;overflow:hidden;background:#fff;',
  'box-shadow:0 24px 64px rgba(0,0,0,.28),0 2px 8px rgba(0,0,0,.12);transform-origin:bottom ' + (left ? 'left' : 'right') + ';',
  'opacity:0;transform:scale(.85) translateY(12px);pointer-events:none;transition:opacity .2s,transform .35s cubic-bezier(.34,1.56,.64,1)}',
  '.panel.open{opacity:1;transform:none;pointer-events:auto}',
  '.panel iframe{width:100%;height:100%;border:0;display:block;background:transparent}',
  '@media (max-width:520px){.panel{inset:0;width:100%;height:100%;max-width:none;border-radius:0}}',
  '@media (prefers-reduced-motion:reduce){.btn,.panel{transition:none}}'
].join('');

var host = document.createElement('div');
host.setAttribute('data-feedbacklooped', '');
var root = host.attachShadow({ mode: 'open' });
try {
  var sheet = new CSSStyleSheet();
  sheet.replaceSync(css); // constructable sheets also work under a strict host CSP
  root.adoptedStyleSheets = [sheet];
} catch (e) {
  var st = document.createElement('style');
  st.textContent = css;
  root.appendChild(st);
}

var btn = document.createElement('button');
btn.className = 'btn';
btn.type = 'button';
btn.setAttribute('aria-haspopup', 'dialog');
btn.innerHTML = '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M7 4h10a4 4 0 0 1 4 4v5a4 4 0 0 1-4 4h-6l-4.5 3v-3.2A4 4 0 0 1 3 13V8a4 4 0 0 1 4-4z"/></svg><span></span>';
btn.querySelector('span').textContent = opt.label || 'Feedback';
if (opt.button === 'false') btn.style.display = 'none';

var panel = document.createElement('div');
panel.className = 'panel';
panel.setAttribute('role', 'dialog');
panel.setAttribute('aria-label', opt.label || 'Feedback');
var frame = null;
var lastFocus = null;

function src(o) {
  var q = ['embed=1', 'page=' + encodeURIComponent(location.href.split('#')[0])];
  var ctx = o.context || opt.context, cat = o.category || opt.category;
  if (ctx) q.push('context=' + encodeURIComponent(ctx));
  if (cat) q.push('category=' + encodeURIComponent(cat));
  if (o.title) q.push('title=' + encodeURIComponent(o.title));
  return CFG.base + '/?' + q.join('&');
}

function open(o) {
  o = o || {};
  lastFocus = document.activeElement;
  if (!frame || o.context || o.category || o.title) {
    if (frame) frame.remove();
    frame = document.createElement('iframe');
    frame.title = opt.label || 'Feedback';
    frame.setAttribute('allow', 'display-capture; clipboard-write');
    frame.src = src(o);
    panel.appendChild(frame);
  }
  panel.classList.add('open');
  btn.classList.add('hidden');
  setTimeout(function () { frame && frame.focus(); }, 50);
}

function close() {
  panel.classList.remove('open');
  btn.classList.remove('hidden');
  if (lastFocus && lastFocus.focus) lastFocus.focus();
}

btn.addEventListener('click', function () { open(); });
window.addEventListener('message', function (e) {
  if (e.origin !== CFG.base || !e.data || typeof e.data !== 'object') return;
  if (e.data.type === 'feedbacklooped:close') close();
  if (e.data.type === 'feedbacklooped:sent' && frame) {
    // Start fresh next time; the sent form stays visible until closed.
    var f = frame;
    frame = null;
    panel.addEventListener('transitionend', function once() { if (!panel.classList.contains('open')) { f.remove(); } panel.removeEventListener('transitionend', once); });
  }
});
document.addEventListener('keydown', function (e) {
  if (e.key === 'Escape' && panel.classList.contains('open')) close();
});

root.appendChild(btn);
root.appendChild(panel);
(document.body || document.documentElement).appendChild(host);
window.Feedbacklooped = { open: open, close: close };
