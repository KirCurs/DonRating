'use strict';
(() => {
  const $ = (id) => document.getElementById(id);
  const { json, storage, active, scores } = window.DonAPI;
  const cards = new Map();
  const picker = $('device-picker');
  let selectedDevice = storage.get('don-admin-device');
  let deviceSignature = '';
  picker.addEventListener('change', () => {
    selectedDevice = picker.value;
    storage.set('don-admin-device', selectedDevice);
    render();
  });
  const busyDevices = new Set();
  let token = storage.get('don-admin-token');
  let snapshot = null;
  let connected = false;
  let authRequired = false;
  let generation = 0;
  let revision = 0;
  let pollTimer = null;
  let pollController = null;
  let polling = false;
  let stopped = false;
  let lastConnected = null;
  let enrolling = false;

  function connection(message, good = false) {
    $('admin-connection').textContent = message;
    $('admin-connection').classList.toggle('connected', good);
  }
  function showError(message) {
    $('admin-error').hidden = !message;
    $('admin-error').textContent = message;
  }
  function requireLogin(message = '') {
    clearEnrollment();
    authRequired = true;
    connected = false;
    $('login-panel').hidden = false;
    $('login-error').hidden = !message;
    $('login-error').textContent = message;
    $('change-access').hidden = true;
    connection('Для управления планшетами нужен ключ доступа.');
    render();
  }
  function createCard(device) {
    const article = document.createElement('article');
    article.className = 'device-card';
    const top = document.createElement('div'); top.className = 'device-card-top';
    const title = document.createElement('h2');
    const badge = document.createElement('span'); badge.className = 'device-badge';
    top.append(title, badge);
    const details = document.createElement('p'); details.className = 'device-details';
    const request = document.createElement('div'); request.className = 'device-request';
    const requestTitle = document.createElement('p'); requestTitle.className = 'device-request-title';
    const requestNote = document.createElement('p'); requestNote.className = 'device-request-note';
    request.append(requestTitle, requestNote);
    const button = document.createElement('button'); button.className = 'button button-primary device-trigger'; button.type = 'button';
    button.addEventListener('click', () => trigger(device.id));
    const link = document.createElement('a'); link.className = 'text-button device-link';
    link.href = '/tablet?device_id=' + encodeURIComponent(device.id); link.target = '_blank'; link.rel = 'noopener noreferrer';
    link.textContent = 'Открыть экран планшета ↗';
    article.append(top, details, request, button, link);
    $('device-list').append(article);
    const card = { article, title, badge, details, requestTitle, requestNote, button, link };
    cards.set(device.id, card);
    return card;
  }
  function render() {
    const devices = Array.isArray(snapshot?.devices) ? snapshot.devices : [];
    const signature = JSON.stringify(devices.map(({ id, name, online }) => [id, name, connected && online]));
    if (signature !== deviceSignature) {
      deviceSignature = signature;
      const all = new Option('Все планшеты', '');
      picker.replaceChildren(all, ...devices.map((device) => new Option(
        (device.name || device.id) + ' · ' + device.id + (connected && device.online ? ' · В сети' : ' · Не в сети'), device.id)));
      if (snapshot && selectedDevice && !devices.some((device) => device.id === selectedDevice)) {
        selectedDevice = ''; storage.set('don-admin-device', '');
      }
      picker.value = selectedDevice;
    }
    picker.disabled = !connected || authRequired || devices.length === 0;
    const current = new Set(devices.map((device) => device.id));
    for (const [id, card] of cards) if (!current.has(id)) { card.article.remove(); cards.delete(id); }
    for (const device of devices) {
      const card = cards.get(device.id) || createCard(device);
      card.article.hidden = Boolean(selectedDevice && selectedDevice !== device.id);
      const online = connected && !authRequired && device.online === true;
      const request = device.request;
      const waiting = active(request);
      const busy = busyDevices.has(device.id);
      card.title.textContent = device.name || device.id;
      card.badge.textContent = online ? 'В сети' : connected && !authRequired ? 'Не в сети' : 'Связь не подтверждена';
      card.badge.classList.toggle('online', online);
      card.details.textContent = online ? 'Планшет готов принять форму' : connected && !authRequired ? 'Откройте экран на планшете и проверьте подключение' : 'Ждём подтверждения соединения';
      card.button.disabled = !online || waiting || busy || authRequired;
      card.button.textContent = busy ? 'Отправляем приглашение…' : waiting ? 'Ожидаем оценку гостя' : 'Попросить оценку';
      card.button.setAttribute('aria-label', card.button.textContent + ' · ' + (device.name || device.id));
      card.article.setAttribute('aria-busy', String(busy));
      if (request?.status === 'answered' || Number.isInteger(request?.score)) {
        const score = Number(request.score);
        card.requestTitle.textContent = 'Последняя оценка: ' + (score >= 1 && score <= 5 ? score + ' / 5 · ' + scores[score - 1] : 'принята');
        card.requestNote.textContent = 'Спасибо гостю! Можно пригласить следующего.';
      } else if (waiting) {
        const seconds = Math.max(0, Math.ceil((Date.parse(request.expires_at) - Date.now()) / 1000));
        card.requestTitle.textContent = request.status === 'displayed' ? 'Форма открыта на планшете' : 'Приглашение отправлено';
        card.requestNote.textContent = seconds > 0 ? 'Осталось около ' + Math.min(seconds, snapshot?.request_ttl_seconds || 30) + ' с' : 'Завершаем приглашение…';
      } else if (request?.status === 'expired') {
        card.requestTitle.textContent = 'Время ответа истекло';
        card.requestNote.textContent = 'При необходимости пригласите гостя ещё раз.';
      } else {
        card.requestTitle.textContent = 'Готов к следующему гостю';
        card.requestNote.textContent = 'Одна оценка — одно приглашение.';
      }
    }
    $('empty-devices').hidden = !snapshot || devices.length !== 0;
    $('device-list').setAttribute('aria-busy', String(!snapshot && !authRequired));
    if (snapshot?.request_ttl_seconds) $('request-ttl').textContent = String(snapshot.request_ttl_seconds);
    $('change-access').hidden = !token || authRequired;
    $('enrollment-panel').hidden = !connected || authRequired;
    $('enroll-device').disabled = !connected || authRequired || enrolling;
  }
  async function trigger(deviceId) {
    const device = snapshot?.devices?.find((item) => item.id === deviceId);
    if (!connected || authRequired || !device?.online || active(device.request) || busyDevices.has(deviceId)) return;
    const attempt = generation;
    busyDevices.add(deviceId); showError(''); render();
    try {
      const request = await json('/request', { method: 'POST', token, body: JSON.stringify({ device_id: deviceId }) });
      if (attempt !== generation) return;
      revision += 1;
      const currentDevice = snapshot?.devices?.find((item) => item.id === deviceId);
      if (currentDevice) currentDevice.request = request;
      render();
    } catch (error) {
      if (attempt !== generation) return;
      if (error.status === 401) requireLogin('Ключ доступа не принят. Введите его ещё раз.');
      else {
        showError(error.message);
        if (!error.status) { connected = false; connection('Нет связи с сервером. Данные на экране могут быть устаревшими.'); }
      }
    } finally {
      busyDevices.delete(deviceId); render();
      if (!polling && !authRequired) schedule(0);
    }
  }
  function schedule(delay = 1000) {
    clearTimeout(pollTimer);
    if (!stopped && !authRequired) pollTimer = setTimeout(poll, delay);
  }
  async function poll() {
    if (polling || stopped || authRequired) return;
    polling = true;
    const attempt = generation;
    const readRevision = revision;
    const controller = new AbortController(); pollController = controller;
    try {
      const data = await json('/api/admin/state', { token, signal: controller.signal });
      if (attempt !== generation || readRevision !== revision || stopped) return;
      if (!Array.isArray(data.devices)) throw new Error('Не удалось прочитать список планшетов.');
      snapshot = data; connected = true; lastConnected = new Date();
      $('login-panel').hidden = true;
      connection('Связь с сервером установлена · обновляем каждую секунду', true);
      render();
    } catch (error) {
      if (attempt !== generation || stopped || error.name === 'AbortError') return;
      if (error.status === 401) requireLogin();
      else {
        connected = false;
        const last = lastConnected ? ' Последнее обновление — ' + lastConnected.toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit', second: '2-digit' }) + '.' : '';
        connection('Нет связи с сервером.' + last + ' Пробуем подключиться снова.');
        render();
      }
    } finally {
      if (pollController === controller) pollController = null;
      polling = false;
      schedule();
    }
  }
  function clearEnrollment() {
    $('tablet-link').value = ''; $('enrollment-result').hidden = true;
    $('tablet-qr').replaceChildren(); $('tablet-qr-panel').hidden = true; $('tablet-qr-error').hidden = true;
    $('enrollment-error').hidden = true; $('copy-status').textContent = '';
  }
  $('enrollment-form').addEventListener('submit', async (event) => {
    event.preventDefault();
    if (!connected || authRequired || enrolling) return;
    const name = $('device-name').value.trim();
    if (!name) return;
    const attempt = generation;
    enrolling = true; clearEnrollment(); render();
    try {
      const data = await json('/api/admin/devices', { method: 'POST', token, body: JSON.stringify({ name }) });
      if (attempt !== generation || stopped) return;
      const url = new URL(data.tablet_url, location.origin);
      if (url.origin !== location.origin || url.pathname !== '/tablet') throw new Error('Некорректная ссылка планшета');
      $('tablet-link').value = url.href; $('enrollment-result').hidden = false;
      const qrReady = window.DonEnrollmentQR?.render($('tablet-qr'), url.href) === true;
      $('tablet-qr-panel').hidden = !qrReady;
      $('tablet-qr-error').hidden = qrReady;
      $('enrollment-local-note').hidden = !['localhost', '127.0.0.1', '[::1]'].includes(location.hostname);
      $('device-name').value = '';
      selectedDevice = data.id; storage.set('don-admin-device', selectedDevice);
      // Do not render against an older snapshot: that would reset the new selection.
      revision += 1;
      if (snapshot?.devices) snapshot.devices.push({ id: data.id, name: data.name, online: false, request: null });
    } catch (error) {
      if (attempt !== generation || stopped) return;
      if (error.status === 401) requireLogin('Ключ доступа не принят.');
      else { $('enrollment-error').textContent = error.message; $('enrollment-error').hidden = false; }
    } finally {
      enrolling = false; render(); if (!polling && !authRequired) schedule(0);
    }
  });
  $('copy-tablet-link').addEventListener('click', async () => {
    const value = $('tablet-link').value;
    if (!value) return;
    try {
      if (!navigator.clipboard) throw new Error('Clipboard unavailable');
      await navigator.clipboard.writeText(value);
      $('copy-status').textContent = 'Ссылка скопирована';
    } catch {
      $('tablet-link').focus(); $('tablet-link').select();
      $('copy-status').textContent = 'Скопируйте выделенную ссылку вручную';
    }
  });
  $('login-form').addEventListener('submit', (event) => {
    event.preventDefault();
    const value = $('admin-token').value.trim();
    if (!value) return;
    token = value; storage.set('don-admin-token', token); $('admin-token').value = '';
    generation += 1; revision += 1; authRequired = false;
    $('login-error').hidden = true; $('login-panel').hidden = true;
    connection('Проверяем ключ доступа…');
    pollController?.abort();
    schedule(0);
  });
  $('change-access').addEventListener('click', () => {
    generation += 1; pollController?.abort(); clearTimeout(pollTimer);
    requireLogin(); $('admin-token').focus();
  });
  window.addEventListener('pageshow', (event) => { if (event.persisted) location.reload(); });
  window.addEventListener('pagehide', () => { stopped = true; clearTimeout(pollTimer); pollController?.abort(); });
  render(); schedule(0);
})();
