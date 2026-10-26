// Runs in <head>, before the page is painted, so a chosen theme never flashes.
// The choice stays in this browser only (localStorage); nothing is sent to the server.
(function () {
  try {
    var t = localStorage.getItem('fl-theme');
    if (t === 'light' || t === 'dark') document.documentElement.dataset.theme = t;
  } catch (e) {}
  document.documentElement.classList.add('js-theme');
})();
