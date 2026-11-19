'use strict';
(() => {
  const panel = document.getElementById('display-controls');
  const button = document.getElementById('enter-fullscreen');
  const message = document.getElementById('display-message');
  const standalone = window.matchMedia('(display-mode: standalone)');
  const fullscreenMode = window.matchMedia('(display-mode: fullscreen)');
  let wakeLock = null;
  let acquiring = false;
  let stopped = false;
  function update() {
    panel.hidden = Boolean(document.fullscreenElement || standalone.matches || fullscreenMode.matches);
    button.hidden = !document.fullscreenEnabled;
    if (!document.fullscreenEnabled) message.textContent = 'Для полного экрана откройте приложение в киоск-режиме.';
  }
  async function keepAwake() {
    if (stopped || acquiring || wakeLock || document.visibilityState !== 'visible' || !navigator.wakeLock) return;
    acquiring = true;
    try {
      const lock = await navigator.wakeLock.request('screen');
      if (stopped || document.visibilityState !== 'visible') { await lock.release(); return; }
      wakeLock = lock;
      lock.addEventListener('release', () => { if (wakeLock === lock) wakeLock = null; });
    } catch { /* Device settings or browser policy may deny the request. */ }
    finally { acquiring = false; }
  }
  button.addEventListener('click', async () => {
    button.disabled = true; message.textContent = '';
    try { await document.documentElement.requestFullscreen(); }
    catch { message.textContent = 'Не удалось включить полный экран. Проверьте настройки браузера.'; }
    finally { button.disabled = false; update(); }
    keepAwake();
  });
  document.addEventListener('fullscreenchange', update);
  standalone.addEventListener('change', update);
  fullscreenMode.addEventListener('change', update);
  document.addEventListener('visibilitychange', () => { update(); keepAwake(); });
  window.addEventListener('pagehide', () => { stopped = true; wakeLock?.release().catch(() => {}); wakeLock = null; });
  update(); keepAwake();
})();
