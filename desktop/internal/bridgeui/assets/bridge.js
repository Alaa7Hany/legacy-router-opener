import { activateLanguage } from './locale.js';

{
  const form = document.getElementById('config-form');
  const targetInput = document.getElementById('target-ip');
  const targetError = document.getElementById('target-error');
  const targetDisplay = document.getElementById('target-display');
  const detectedGatewaySpan = document.getElementById('detected-gateway');
  const presetSelect = document.getElementById('preset-select');
  const presetHint = document.getElementById('preset-hint');
  const detectBtn = document.getElementById('detect-btn');
  const saveBtn = document.getElementById('save-btn');
  const openBtn = document.getElementById('open-btn');
  const resetBtn = document.getElementById('reset-btn');
  const clearLogsBtn = document.getElementById('clear-logs-btn');
  const actionBanner = document.getElementById('action-banner');
  const tlsSummary = document.getElementById('tls-summary');
  const connectionIdle = document.getElementById('connection-idle');
  const logBox = document.getElementById('log-box');
  const logLines = document.getElementById('log-lines');
  const logsEmpty = document.getElementById('logs-empty');
  const optionsPanel = document.getElementById('options-panel');

  let presets = [];
  let serverTarget = '';
  let serverPreset = 'auto';
  let userEditedTarget = false;
  let userEditedPreset = false;

  const locale = activateLanguage(() => {
    updatePresetLabels();
    updatePresetDescription();
  });

  function showBanner(key, kind) {
    locale.setMessage(actionBanner, key);
    actionBanner.dataset.kind = kind;
    actionBanner.hidden = false;
  }

  function validateTarget() {
    const val = targetInput.value.trim();
    if (!val) {
      targetInput.setAttribute('aria-invalid', 'true');
      locale.setMessage(targetError, 'address_required');
      targetError.hidden = false;
      return false;
    }
    if (/\s/.test(val)) {
      targetInput.setAttribute('aria-invalid', 'true');
      locale.setMessage(targetError, 'address_spaces');
      targetError.hidden = false;
      return false;
    }
    targetInput.removeAttribute('aria-invalid');
    targetError.hidden = true;
    targetError.textContent = '';
    return true;
  }

  targetInput.addEventListener('input', () => {
    userEditedTarget = true;
    if (targetInput.getAttribute('aria-invalid') === 'true') {
      validateTarget();
    }
  });

  targetInput.addEventListener('blur', () => {
    if (userEditedTarget) {
      validateTarget();
    }
  });

  function updatePresetLabels() {
    for (const option of presetSelect.options) {
      option.textContent = locale.text('preset_' + option.value + '_name');
    }
  }

  function updatePresetDescription() {
    const p = presets.find((item) => item.id === presetSelect.value);
    if (p) {
      presetHint.textContent = locale.text('preset_' + p.id + '_help');
    }
  }

  presetSelect.addEventListener('change', () => {
    userEditedPreset = true;
    updatePresetDescription();
  });

  for (const button of document.querySelectorAll('.help-button')) {
    const help = document.getElementById(button.getAttribute('aria-controls'));
    button.addEventListener('click', () => {
      help.hidden = !help.hidden;
      button.setAttribute('aria-expanded', String(!help.hidden));
    });
  }

  function logClass(line) {
    if (line.includes('[ERR]') || line.includes('Error') || line.includes('failed')) return 'log-error';
    if (line.includes('[COOKIE]') || line.includes('[WARN]') || line.includes('[RETRY')) return 'log-warn';
    if (line.includes('[CACHE]') || line.includes('[TLS]') || line.includes('[HTTP]') || line.includes('[REDIRECT]') || line.includes('[RESET]') || line.includes('[TARGET]') || line.includes('[GATEWAY]')) return 'log-info';
    if (line.includes('[200]') || line.includes('[201]') || line.includes('[204]')) return 'log-ok';
    return 'log-plain';
  }

  function renderLogs(logs) {
    const nearBottom = (logBox.scrollHeight - logBox.scrollTop - logBox.clientHeight) < 40;
    logsEmpty.hidden = logs.length > 0;
    logLines.replaceChildren();
    for (const line of logs) {
      const div = document.createElement('div');
      div.className = 'log-line ' + logClass(line);
      div.textContent = line;
      logLines.appendChild(div);
    }
    if (nearBottom) {
      logBox.scrollTop = logBox.scrollHeight;
    }
  }

  function applyState(state) {
    if (!state) return;

    if (presets.length === 0 && Array.isArray(state.presets)) {
      presets = state.presets;
      for (const p of presets) {
        const opt = document.createElement('option');
        opt.value = p.id;
        opt.dir = 'auto';
        presetSelect.appendChild(opt);
      }
      updatePresetLabels();
    }

    if (state.preset) {
      serverPreset = state.preset;
      if (!userEditedPreset && !presetSelect.matches(':focus')) {
        presetSelect.value = state.preset;
      }
    }
    updatePresetDescription();

    if (state.target) {
      serverTarget = state.target;
      if (!userEditedTarget && !targetInput.matches(':focus')) {
        targetInput.value = state.target;
      }
      targetDisplay.textContent = state.target;
    }

    if (state.detectedGateway) {
      detectedGatewaySpan.textContent = state.detectedGateway;
    }

    const tls = state.tls || {};
    if (tls.protocol && tls.protocol !== '—') {
      let line = tls.transport + ' (' + tls.protocol + ')';
      if (tls.cipherSuite) line += ' · ' + tls.cipherSuite;
      tlsSummary.textContent = line;
      tlsSummary.hidden = false;
      connectionIdle.hidden = true;
    } else {
      tlsSummary.hidden = true;
      connectionIdle.hidden = false;
    }

    if (Array.isArray(state.logs)) {
      renderLogs(state.logs);
    }
  }

  async function saveAndMaybeOpen(openDashboard) {
    if (!validateTarget()) {
      optionsPanel.open = true;
      targetInput.focus();
      return;
    }

    const nextTarget = targetInput.value.trim();
    const nextPreset = presetSelect.value || 'auto';
    const needsConfigUpdate = (nextTarget !== serverTarget) || (nextPreset !== serverPreset);

    if (openDashboard && !needsConfigUpdate) {
      window.open('/', '_blank');
      return;
    }

    // Open the tab synchronously during the user click gesture when needed so popup blockers do not block it.
    const pendingTab = openDashboard ? window.open('', '_blank') : null;

    openBtn.disabled = true;
    saveBtn.disabled = true;
    try {
      const res = await fetch('/_bridge/api/config', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          target: nextTarget,
          preset: nextPreset
        })
      });
      const data = await res.json();
      if (!res.ok) {
        if (pendingTab) pendingTab.close();
        optionsPanel.open = true;
        showBanner('changes_failed', 'error');
        return;
      }
      if (targetInput.value.trim() === nextTarget) userEditedTarget = false;
      if (presetSelect.value === nextPreset) userEditedPreset = false;
      applyState(data);
      showBanner('changes_saved', 'success');

      if (openDashboard) {
        if (pendingTab) {
          pendingTab.location.replace('/');
        } else {
          window.location.href = '/';
        }
      }
    } catch {
      if (pendingTab) pendingTab.close();
      showBanner('changes_failed', 'error');
    } finally {
      openBtn.disabled = false;
      saveBtn.disabled = false;
    }
  }

  form.addEventListener('submit', (e) => {
    e.preventDefault();
    saveAndMaybeOpen(true);
  });

  saveBtn.addEventListener('click', () => {
    saveAndMaybeOpen(false);
  });

  detectBtn.addEventListener('click', async () => {
    detectBtn.disabled = true;
    try {
      const res = await fetch('/_bridge/api/detect-gateway', { method: 'POST' });
      const data = await res.json();
      if (!res.ok || !data.gateway) {
        showBanner('detection_failed', 'error');
        return;
      }
      targetInput.value = data.gateway;
      userEditedTarget = true;
      validateTarget();
      detectedGatewaySpan.textContent = data.gateway;
      showBanner('detected_saved', 'success');
    } catch {
      showBanner('detection_failed', 'error');
    } finally {
      detectBtn.disabled = false;
    }
  });

  resetBtn.addEventListener('click', async () => {
    resetBtn.disabled = true;
    try {
      const res = await fetch('/_bridge/api/reset', { method: 'POST' });
      if (!res.ok) {
        showBanner('login_fix_failed', 'error');
        return;
      }
      applyState(await res.json());
      showBanner('login_fixed', 'success');
    } catch {
      showBanner('login_fix_failed', 'error');
    } finally {
      resetBtn.disabled = false;
    }
  });

  clearLogsBtn.addEventListener('click', async () => {
    clearLogsBtn.disabled = true;
    try {
      const res = await fetch('/_bridge/api/logs/clear', { method: 'POST' });
      if (res.ok) renderLogs([]);
      else showBanner('clear_failed', 'error');
    } catch {
      showBanner('clear_failed', 'error');
    } finally {
      clearLogsBtn.disabled = false;
    }
  });

  function hydrateInitialState() {
    try {
      const raw = document.getElementById('initial-state');
      if (raw && raw.textContent) {
        applyState(JSON.parse(raw.textContent));
      }
    } catch (_) {}
  }

  let events;
  function connectSSE() {
    events = new EventSource('/_bridge/events?opener=1');
    events.onmessage = (event) => {
      try {
        applyState(JSON.parse(event.data));
      } catch (_) {}
    };
  }

  hydrateInitialState();
  window.addEventListener('pageshow', connectSSE);
  window.addEventListener('pagehide', () => events?.close());
}
