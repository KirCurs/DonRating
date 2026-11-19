'use strict';
(() => {
  const $ = (id) => document.getElementById(id);
  const { json, storage, active, scores } = window.DonAPI;
  const deviceId = new URL(location.href).searchParams.get('device_id')?.trim() || 'reception-1';
  const tokenKey = 'don-tablet-token:' + deviceId;
  // Tablet credentials survive tab and browser restarts on the reception device.
  const tabletStorage = {
    get() { try { return localStorage.getItem(tokenKey) || storage.get(tokenKey); } catch { return storage.get(tokenKey); } },
    set(value) { try { localStorage.setItem(tokenKey, value); } catch { storage.set(tokenKey, value); } },
  };
  let token = tabletStorage.get();
  const fragment = new URLSearchParams(location.hash.slice(1));
  if (fragment.has('token')) {
    token = fragment.get('token') || '';
    tabletStorage.set(token);
    history.replaceState(null, '', location.pathname + location.search);
  }
  const base = '/api/tablets/' + encodeURIComponent(deviceId);
  const form = $('rating-form');
  const options = $('rating-options');
  const submit = $('submit-rating');
  const submitLabel = submit.querySelector('span');
  const views = { ready: $('rating-view'), success: $('success-view'), already: $('already-view') };
  const finished = new Set();
  let request = null;
  let phase = 'standby';
  let generation = 0;
  let selectedScore = null;
  let busy = false;
  let deadline = 0;
  let wallDeadline = 0;
  let returnTimer = null;
  let pollTimer = null;
  let pollController = null;
  let submitController = null;
  let ackController = null;
  let ackedId = '';
  let stopped = false;
  let playerURL = '';

  function status(text) { $('tablet-status').textContent = text; }
  function playerMessage(type) {
    if (!playerURL) return;
    $('slideshow-player').contentWindow?.postMessage({ type: 'don:' + type }, location.origin);
  }
  $('slideshow-player').addEventListener('load', () => playerMessage(phase === 'standby' ? 'resume' : 'pause'));
  function configurePlayer(data) {
    // Keep the same browsing context throughout a guest's answer and subsequent invitations.
    if (typeof data.slideshow_url !== 'string' || !data.slideshow_url || data.slideshow_configured === false) return;
    let url;
    try { url = new URL(data.slideshow_url, location.origin); } catch { return; }
    if (url.origin !== location.origin || !url.pathname.startsWith('/slideshow/')) return;
    if (playerURL) return;
    playerURL = url.href;
    $('slideshow-player').src = url.href;
    $('slideshow-player').hidden = false;
    $('standby-brand').hidden = true;
  }
  function showPhase(name, focus = false) {
    const wasStandby = phase === 'standby';
    phase = name;
    $('guest-overlay').hidden = name === 'standby';
    $('slideshow-stage').setAttribute('aria-hidden', String(name !== 'standby'));
    $('slideshow-stage').inert = name !== 'standby';
    Object.entries(views).forEach(([key, view]) => { view.hidden = key !== name; });
    if (name !== 'standby') {
      const heading = views[name].querySelector('h1');
      $('guest-overlay').setAttribute('aria-labelledby', heading.id);
      if (focus) { heading.tabIndex = -1; heading.focus({ preventScroll: true }); }
    }
    if (wasStandby && name !== 'standby') playerMessage('pause');
    if (!wasStandby && name === 'standby') playerMessage('resume');
  }
  function setBusy(value) {
    busy = value;
    form.setAttribute('aria-busy', String(value));
    options.disabled = value;
    submit.disabled = value || selectedScore === null || phase !== 'ready';
    submitLabel.textContent = value ? 'Сохраняем оценку…' : $('rating-error').hidden ? 'Отправить оценку' : 'Повторить отправку';
    $('selection-hint').textContent = value ? 'Пожалуйста, подождите' : selectedScore === null ? 'Выберите оценку от 1 до 5' : 'Вы выбрали: ' + scores[selectedScore - 1];
  }
  function closeRequest() {
    if (request) finished.add(request.id);
    generation += 1;
    submitController?.abort(); ackController?.abort();
    submitController = null; ackController = null;
    clearTimeout(returnTimer);
    request = null; deadline = 0; wallDeadline = 0; selectedScore = null;
    form.reset(); $('rating-error').hidden = true;
    showPhase('standby'); setBusy(false);
    $('request-countdown').textContent = '';
  }
  function openRequest(next, serverNow) {
    generation += 1;
    request = next; ackedId = ''; selectedScore = null;
    form.reset(); $('rating-error').hidden = true;
    updateDeadline(next, serverNow);
    if (deadline <= performance.now()) { closeRequest(); return; }
    showPhase('ready', true); setBusy(false); tick();
  }
  function updateDeadline(next, serverNow) {
    const remaining = Date.parse(next.expires_at) - Date.parse(serverNow);
    if (!Number.isFinite(remaining)) return;
    const proposed = performance.now() + Math.max(0, remaining);
    const proposedWall = Date.now() + Math.max(0, remaining);
    deadline = deadline ? Math.min(deadline, proposed) : proposed;
    wallDeadline = wallDeadline ? Math.min(wallDeadline, proposedWall) : proposedWall;
  }
  function tick() {
    if (phase !== 'ready' || !request) return;
    const remaining = Math.max(0, Math.ceil(Math.min(deadline - performance.now(), wallDeadline - Date.now()) / 1000));
    $('request-countdown').textContent = remaining + ' с';
    if (remaining <= 0) closeRequest();
  }
  const countdownTimer = setInterval(tick, 200);
  function finish(name, score) {
    if (!request || phase === 'success' || phase === 'already') return;
    finished.add(request.id);
    generation += 1;
    submitController?.abort(); ackController?.abort();
    submitController = null; ackController = null;
    if (name === 'success') {
      const number = document.createElement('strong'); number.textContent = score + ' / 5';
      const label = document.createElement('span'); label.textContent = scores[score - 1] || '';
      $('saved-score').replaceChildren(number, label);
    }
    $('request-countdown').textContent = '';
    showPhase(name, true); setBusy(false);
    clearTimeout(returnTimer);
    returnTimer = setTimeout(closeRequest, 2000);
  }
  options.addEventListener('change', (event) => {
    if (busy || phase !== 'ready' || event.target.name !== 'score') return;
    selectedScore = Number(event.target.value);
    $('rating-error').hidden = true; setBusy(false);
  });
  form.addEventListener('submit', async (event) => {
    event.preventDefault(); tick();
    if (busy || phase !== 'ready' || !request || selectedScore === null) return;
    const attempt = generation, requestId = request.id, score = selectedScore;
    const controller = new AbortController(); submitController = controller;
    $('rating-error').hidden = true; setBusy(true);
    try {
      const answer = await json(base + '/answer', { method: 'POST', token, signal: controller.signal, body: JSON.stringify({ request_id: requestId, score }) });
      if (attempt !== generation || request?.id !== requestId) return;
      finish('success', Number(answer.score) || score);
    } catch (error) {
      if (attempt !== generation || request?.id !== requestId || error.name === 'AbortError') return;
      if (error.status === 410 || (error.status === 409 && /expir|cancel|inactive|not_active/i.test(error.code))) closeRequest();
      else if (error.status === 409) finish('already');
      else {
        $('rating-error').hidden = false;
        $('rating-error').textContent = error.status === 401 ? 'Не удалось отправить оценку. Пожалуйста, обратитесь к сотруднику.' : error.message + ' Ваша оценка осталась выбрана.';
      }
    } finally {
      if (attempt === generation && request?.id === requestId) { submitController = null; setBusy(false); }
    }
  });
  async function acknowledge(id) {
    if (ackedId === id || !request || phase !== 'ready') return;
    const attempt = generation;
    const controller = new AbortController(); ackController = controller;
    try {
      await json(base + '/ack', { method: 'POST', token, signal: controller.signal, body: JSON.stringify({ id }) });
      if (attempt === generation && request?.id === id) ackedId = id;
    } catch (error) {
      if (attempt !== generation || error.name === 'AbortError') return;
      if (error.status === 409 || error.status === 410) {
        // The state poll resolves whether another answer arrived or the invitation expired.
      }
    } finally { if (ackController === controller) ackController = null; }
  }
  async function poll() {
    if (stopped) return;
    const controller = new AbortController(); pollController = controller;
    try {
      const data = await json(base + '/state', { token, signal: controller.signal });
      if (stopped) return;
      configurePlayer(data); status('');
      const next = data.request;
      if (phase === 'success' || phase === 'already') return;
      if (request && busy && !next) return;
      if (request && (!next || next.id !== request.id || !active(next))) {
        if (next?.id === request.id && next.status === 'answered') {
          if (busy && Number(next.score) === selectedScore) finish('success', Number(next.score));
          else finish('already');
        } else closeRequest();
      }
      if (!next || !active(next) || finished.has(next.id)) return;
      if (!request) openRequest(next, data.server_now);
      else if (request.id === next.id) { request = next; updateDeadline(next, data.server_now); tick(); }
      if (request?.id === next.id && phase === 'ready') await acknowledge(next.id);
    } catch (error) {
      if (stopped || error.name === 'AbortError') return;
      status(error.status === 401 ? 'Экран временно недоступен. Обратитесь к сотруднику.' : 'Восстанавливаем соединение…');
      // An unanswered form and its selection stay available until its local deadline.
    } finally {
      if (pollController === controller) pollController = null;
      if (!stopped) pollTimer = setTimeout(poll, 1000);
    }
  }
  document.addEventListener('keydown', (event) => {
    if (event.key !== 'Tab' || phase === 'standby') return;
    const candidates = Array.from($('guest-overlay').querySelectorAll('button:not(:disabled), input:not(:disabled), [tabindex="0"]')).filter((element) => element.getClientRects().length);
    const chosenRadio = candidates.find((element) => element.type === 'radio' && element.checked);
    const firstRadio = candidates.find((element) => element.type === 'radio');
    const focusable = candidates.filter((element) => element.type !== 'radio' || element === (chosenRadio || firstRadio));
    if (!focusable.length) { event.preventDefault(); return; }
    const first = focusable[0], last = focusable[focusable.length - 1];
    if (event.shiftKey && (document.activeElement === first || !focusable.includes(document.activeElement))) { event.preventDefault(); last.focus(); }
    else if (!event.shiftKey && (document.activeElement === last || !focusable.includes(document.activeElement))) { event.preventDefault(); first.focus(); }
  });
  window.addEventListener('pageshow', (event) => {
    if (event.persisted) location.reload();
  });
  window.addEventListener('pagehide', () => {
    stopped = true; generation += 1;
    clearTimeout(pollTimer); clearTimeout(returnTimer); clearInterval(countdownTimer);
    pollController?.abort(); submitController?.abort(); ackController?.abort();
  });
  $('selection-hint').setAttribute('aria-live', 'polite');
  showPhase('standby'); setBusy(false); poll();
})();
