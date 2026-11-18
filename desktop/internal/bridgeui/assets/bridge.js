(function () {
  const form = document.getElementById('config-form');
  const targetInput = document.getElementById('target-ip');
  const targetError = document.getElementById('target-error');
  const targetSubtitle = document.getElementById('target-subtitle');
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
  const logBox = document.getElementById('log-box');
  const optionsPanel = document.getElementById('options-panel');

  let presets = [];
  let serverTarget = '';
  let serverPreset = 'auto';
  let userEditedTarget = false;

  function showBanner(msg, kind) {
    actionBanner.textContent = msg;
    actionBanner.dataset.kind = kind || 'info';
    actionBanner.hidden = false;
  }

  function updateHeroText(displayTarget) {
    const clean = (displayTarget || '192.168.1.1').trim();
    openBtn.textContent = 'Open Router Page (' + clean + ') ↗';
    targetSubtitle.innerHTML = 'Ready to open <strong>' + escapeHtml(clean) + '</strong>';
  }

  function escapeHtml(str) {
    return String(str)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  function validateTarget() {
    const val = targetInput.value.trim();
    if (!val) {
      targetInput.setAttribute('aria-invalid', 'true');
      targetError.textContent = 'Enter a router IP or address (for example, 192.168.1.1).';
      targetError.hidden = false;
      return false;
    }
    if (/\s/.test(val)) {
      targetInput.setAttribute('aria-invalid', 'true');
      targetError.textContent = 'Address cannot contain spaces.';
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

  function updatePresetDescription() {
    const p = presets.find((item) => item.id === presetSelect.value);
    if (p) {
      presetHint.textContent = p.description;
    }
  }

  presetSelect.addEventListener('change', () => {
    updatePresetDescription();
  });

  function logClass(line) {
    if (line.includes('[ERR]') || line.includes('Error') || line.includes('failed')) return 'log-error';
    if (line.includes('[COOKIE]') || line.includes('[WARN]') || line.includes('[RETRY')) return 'log-warn';
    if (line.includes('[CACHE]') || line.includes('[TLS]') || line.includes('[HTTP]') || line.includes('[REDIRECT]') || line.includes('[RESET]') || line.includes('[TARGET]') || line.includes('[GATEWAY]')) return 'log-info';
    if (line.includes('[200]') || line.includes('[201]') || line.includes('[204]')) return 'log-ok';
    return 'log-plain';
  }

  function renderLogs(logs) {
    if (!logs || logs.length === 0) {
      logBox.innerHTML = '<div class="log-empty">No connection activity yet.</div>';
      return;
    }
    const nearBottom = (logBox.scrollHeight - logBox.scrollTop - logBox.clientHeight) < 40;
    logBox.innerHTML = '';
    for (const line of logs) {
      const div = document.createElement('div');
      div.className = 'log-line ' + logClass(line);
      div.textContent = line;
      logBox.appendChild(div);
    }
    if (nearBottom) {
      logBox.scrollTop = logBox.scrollHeight;
    }
  }

  function applyState(state) {
    if (!state) return;

    if (presets.length === 0 && Array.isArray(state.presets)) {
      presets = state.presets;
      presetSelect.innerHTML = '';
      for (const p of presets) {
        const opt = document.createElement('option');
        opt.value = p.id;
        opt.textContent = p.name;
        presetSelect.appendChild(opt);
      }
    }

    if (state.preset) {
      serverPreset = state.preset;
      if (!presetSelect.matches(':focus')) {
        presetSelect.value = state.preset;
      }
    }
    updatePresetDescription();

    if (state.target) {
      serverTarget = state.target;
      if (!userEditedTarget && !targetInput.matches(':focus')) {
        targetInput.value = state.target;
      }
      updateHeroText(state.target);
    }

    if (state.detectedGateway) {
      detectedGatewaySpan.textContent = state.detectedGateway;
    }

    const tls = state.tls || {};
    if (tls.protocol && tls.protocol !== '—') {
      let line = tls.transport + ' (' + tls.protocol + ')';
      if (tls.cipherSuite) line += ' · ' + tls.cipherSuite;
      tlsSummary.textContent = line;
    } else {
      tlsSummary.textContent = state.resolvedTarget ? ('Ready · ' + state.resolvedTarget) : 'No connections yet';
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
        showBanner(data.error || 'Could not update address.', 'error');
        return;
      }
      userEditedTarget = false;
      applyState(data);
      showBanner('Target updated to ' + data.resolvedTarget + '.', 'success');

      if (openDashboard) {
        if (pendingTab) {
          pendingTab.location.replace('/');
        } else {
          window.location.href = '/';
        }
      }
    } catch (err) {
      if (pendingTab) pendingTab.close();
      showBanner('Could not reach local proxy: ' + err.message, 'error');
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
      if (res.ok && data.gateway) {
        targetInput.value = data.gateway;
        userEditedTarget = true;
        validateTarget();
        detectedGatewaySpan.textContent = data.gateway;
        showBanner('Filled detected gateway: ' + data.gateway + '. Click Apply changes or Open Router Page.', 'success');
      }
    } catch (err) {
      showBanner('Gateway detection failed: ' + err.message, 'error');
    } finally {
      detectBtn.disabled = false;
    }
  });

  resetBtn.addEventListener('click', async () => {
    resetBtn.disabled = true;
    try {
      const res = await fetch('/_bridge/api/reset', { method: 'POST' });
      const data = await res.json();
      if (res.ok) {
        applyState(data);
        showBanner('Session cleared. You can open the router page again.', 'success');
      }
    } catch (err) {
      showBanner('Reset failed: ' + err.message, 'error');
    } finally {
      resetBtn.disabled = false;
    }
  });

  clearLogsBtn.addEventListener('click', async () => {
    await fetch('/_bridge/api/logs/clear', { method: 'POST' });
    renderLogs([]);
  });

  function hydrateInitialState() {
    try {
      const raw = document.getElementById('initial-state');
      if (raw && raw.textContent) {
        applyState(JSON.parse(raw.textContent));
      }
    } catch (_) {}
  }

  function connectSSE() {
    const es = new EventSource('/_bridge/events');
    es.onmessage = (event) => {
      try {
        applyState(JSON.parse(event.data));
      } catch (_) {}
    };
  }

  hydrateInitialState();
  connectSSE();
})();
