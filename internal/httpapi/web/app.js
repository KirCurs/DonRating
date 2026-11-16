'use strict';
(() => {
  class APIError extends Error {
    constructor(message, status = 0, code = '') { super(message); this.status = status; this.code = code; }
  }
  async function json(url, options = {}) {
    const controller = new AbortController();
    let timedOut = false;
    const abort = () => controller.abort();
    const { signal, token, ...settings } = options;
    if (signal?.aborted) controller.abort();
    else signal?.addEventListener('abort', abort, { once: true });
    const timer = setTimeout(() => { timedOut = true; controller.abort(); }, 8000);
    const headers = { Accept: 'application/json', ...settings.headers };
    if (token) headers.Authorization = 'Bearer ' + token;
    if (settings.body) headers['Content-Type'] = 'application/json';
    try {
      const response = await fetch(url, { ...settings, headers, credentials: 'same-origin', cache: 'no-store', signal: controller.signal });
      const body = await response.text();
      let data = {};
      if (body) { try { data = JSON.parse(body); } catch { if (response.ok) throw new APIError('Сервер вернул непонятный ответ. Попробуйте ещё раз.'); } }
      if (!response.ok) throw new APIError(typeof data.message === 'string' ? data.message : 'Не удалось выполнить действие. Попробуйте ещё раз.', response.status, data.error || '');
      return data;
    } catch (error) {
      if (error instanceof APIError) throw error;
      if (error.name === 'AbortError' && !timedOut) throw error;
      throw new APIError(timedOut ? 'Сервер не ответил вовремя. Попробуйте ещё раз.' : 'Нет связи с сервером. Проверьте соединение и попробуйте ещё раз.');
    } finally {
      clearTimeout(timer);
      signal?.removeEventListener('abort', abort);
    }
  }
  const storage = {
    get(key) { try { return sessionStorage.getItem(key) || ''; } catch { return ''; } },
    set(key, value) { try { if (value) sessionStorage.setItem(key, value); else sessionStorage.removeItem(key); } catch { /* The current screen can still use its in-memory token. */ } },
  };
  window.DonAPI = { json, storage, active: (request) => !!request && !['answered', 'expired', 'cancelled', 'canceled'].includes(request.status), scores: ['Плохо', 'Не очень', 'Нормально', 'Хорошо', 'Отлично'] };
})();
