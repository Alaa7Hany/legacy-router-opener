package proxy

import (
	"fmt"
	"html"
)

const bridgePageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="color-scheme" content="dark">
  <title>Legacy Router Opener — Control Launchpad</title>
  <style>
    :root {
      color-scheme: dark;
      --bg: #0b0f17;
      --surface: #161e2e;
      --card: #1e293b;
      --terminal: #090d16;
      --border: #475569;
      --border-subtle: #334155;
      --text: #f8fafc;
      --text-muted: #cbd5e1;
      --text-dim: #94a3b8;
      --primary: #2563eb;
      --primary-hover: #1d4ed8;
      --primary-text: #ffffff;
      --green: #10b981;
      --orange: #f59e0b;
      --red: #ef4444;
      --cyan: #a5f3fc;
      --focus: #60a5fa;
      --scrollbar-track: #0f172a;
      --scrollbar-thumb: #64748b;
      scrollbar-color: var(--scrollbar-thumb) var(--scrollbar-track);
    }

    @supports not (scrollbar-color: auto) {
      .log-box {
        scrollbar-width: auto;
      }
      .log-box::-webkit-scrollbar {
        width: 10px;
        height: 10px;
      }
      .log-box::-webkit-scrollbar-track {
        background: var(--scrollbar-track);
      }
      .log-box::-webkit-scrollbar-thumb {
        background: var(--scrollbar-thumb);
        border-radius: 5px;
      }
    }

    *, *::before, *::after {
      box-sizing: border-box;
    }

    body {
      margin: 0;
      min-height: 100vh;
      background-color: var(--bg);
      color: var(--text);
      font-family: system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
      line-height: 1.5;
      padding: 1.5rem 1rem 2.5rem;
    }

    .container {
      max-width: 920px;
      margin: 0 auto;
      display: flex;
      flex-direction: column;
      gap: 1.25rem;
    }

    .skip-link {
      position: absolute;
      left: 0.75rem;
      top: 0.75rem;
      padding: 0.625rem 1rem;
      background: var(--primary);
      color: var(--primary-text);
      border-radius: 0.5rem;
      font-weight: 600;
      text-decoration: none;
      z-index: 100;
    }

    .visually-hidden:where(:not(:focus-within, :active)) {
      position: absolute !important;
      clip-path: inset(50%) !important;
      overflow: hidden !important;
      width: 1px !important;
      height: 1px !important;
      margin: -1px !important;
      padding: 0 !important;
      border: 0 !important;
      white-space: nowrap !important;
    }

    header.topbar {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      justify-content: space-between;
      gap: 0.75rem;
    }

    .brand {
      display: flex;
      align-items: center;
      gap: 0.75rem;
    }

    .brand-icon {
      width: 2rem;
      height: 2rem;
      color: var(--focus);
      flex-shrink: 0;
    }

    h1 {
      margin: 0;
      font-size: 1.375rem;
      font-weight: 700;
      letter-spacing: -0.01em;
    }

    .subtitle {
      margin: 0;
      color: var(--text-dim);
      font-size: 0.875rem;
    }

    .status-pill {
      display: inline-flex;
      align-items: center;
      gap: 0.5rem;
      padding: 0.45rem 0.85rem;
      border-radius: 999px;
      background: var(--surface);
      border: 1px solid var(--border);
      font-size: 0.875rem;
      font-weight: 600;
    }

    .status-dot {
      width: 0.625rem;
      height: 0.625rem;
      border-radius: 50%;
      background-color: var(--green);
      flex-shrink: 0;
    }

    .status-pill.offline .status-dot {
      background-color: var(--red);
    }

    .card {
      background-color: var(--surface);
      border: 1px solid var(--border-subtle);
      border-radius: 1rem;
      padding: 1.25rem;
    }

    .card-header {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      justify-content: space-between;
      gap: 0.75rem;
      margin-bottom: 1rem;
    }

    h2 {
      margin: 0;
      font-size: 1rem;
      font-weight: 700;
      color: var(--text);
    }

    .form-grid {
      display: grid;
      grid-template-columns: 1fr;
      gap: 1rem;
    }

    @media (min-width: 700px) {
      .form-grid-2 {
        display: grid;
        grid-template-columns: 1.25fr 1fr;
        gap: 1rem;
        align-items: start;
      }
    }

    .field {
      display: flex;
      flex-direction: column;
      gap: 0.375rem;
    }

    label {
      font-size: 0.9rem;
      font-weight: 600;
      color: var(--text-muted);
    }

    .input-row {
      display: flex;
      gap: 0.5rem;
      align-items: stretch;
    }

    input[type="text"],
    select {
      width: 100%;
      min-height: 46px;
      padding: 0.625rem 0.875rem;
      font: inherit;
      font-size: max(1rem, 1em);
      color: var(--text);
      background-color: var(--bg);
      border: 1px solid var(--border);
      border-radius: 0.625rem;
    }

    input[aria-invalid="true"] {
      border-color: var(--red);
    }

    .hint {
      font-size: 0.8125rem;
      color: var(--text-dim);
      margin: 0;
    }

    .error-text {
      font-size: 0.875rem;
      color: #fca5a5;
      margin: 0;
      font-weight: 600;
    }

    .error-text[hidden] {
      display: none;
    }

    .actions-row {
      display: flex;
      flex-wrap: wrap;
      gap: 0.625rem;
      margin-top: 0.25rem;
    }

    button,
    a.btn {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      gap: 0.5rem;
      min-height: 46px;
      padding: 0.625rem 1.125rem;
      border-radius: 0.625rem;
      font: inherit;
      font-size: 0.9375rem;
      font-weight: 600;
      cursor: pointer;
      text-decoration: none;
      border: 1px solid transparent;
    }

    button.primary,
    a.btn.primary {
      background-color: var(--primary);
      color: var(--primary-text);
      flex: 1 1 240px;
    }

    button.primary:hover,
    a.btn.primary:hover {
      background-color: var(--primary-hover);
    }

    button.secondary {
      background-color: var(--card);
      color: var(--text);
      border-color: var(--border);
    }

    button.secondary:hover {
      background-color: #273549;
    }

    button.danger {
      background-color: transparent;
      color: #fca5a5;
      border-color: #b91c1c;
    }

    button.danger:hover {
      background-color: rgba(239, 68, 68, 0.14);
    }

    button.compact {
      min-height: 46px;
      padding: 0.5rem 0.875rem;
      white-space: nowrap;
      flex-shrink: 0;
    }

    button:disabled {
      opacity: 0.65;
      cursor: wait;
    }

    :where(a, button, input, select):focus-visible {
      outline: 3px solid var(--focus);
      outline-offset: 2px;
    }

    .status-banner {
      padding: 0.625rem 0.875rem;
      border-radius: 0.625rem;
      background-color: var(--card);
      border: 1px solid var(--border);
      font-size: 0.875rem;
      color: var(--text-muted);
      margin-top: 0.75rem;
    }

    .status-banner[data-kind="success"] {
      border-color: var(--green);
      color: #a7f3d0;
    }

    .status-banner[data-kind="error"] {
      border-color: var(--red);
      color: #fecaca;
    }

    .telemetry-grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(175px, 1fr));
      gap: 0.75rem;
    }

    .stat-tile {
      background-color: var(--bg);
      border: 1px solid var(--border-subtle);
      border-radius: 0.625rem;
      padding: 0.75rem 0.875rem;
    }

    .stat-label {
      font-size: 0.75rem;
      text-transform: uppercase;
      letter-spacing: 0.04em;
      color: var(--text-dim);
      margin-bottom: 0.25rem;
    }

    .stat-value {
      font-size: 0.9375rem;
      font-weight: 600;
      color: var(--text);
      word-break: break-word;
      font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    }

    .stat-value.warn {
      color: #fcd34d;
    }

    .log-box {
      background-color: var(--terminal);
      border: 1px solid var(--border-subtle);
      border-radius: 0.625rem;
      padding: 0.75rem;
      height: 290px;
      overflow-y: auto;
      font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
      font-size: 0.8125rem;
      line-height: 1.45;
      scrollbar-color: var(--scrollbar-thumb) var(--scrollbar-track);
    }

    .log-empty {
      color: var(--text-dim);
    }

    .log-line {
      padding: 0.125rem 0;
      white-space: pre-wrap;
      word-break: break-word;
    }

    .log-error { color: #fca5a5; }
    .log-warn  { color: #fcd34d; }
    .log-info  { color: var(--cyan); }
    .log-ok    { color: #6ee7b7; }
    .log-plain { color: #e2e8f0; }

    @media (forced-colors: active) {
      .card, .stat-tile, .status-pill, .log-box, .status-banner {
        border: 1px solid CanvasText;
      }
      .status-dot {
        background-color: Highlight;
      }
    }
  </style>
</head>
<body>
  <a href="#main-content" class="skip-link visually-hidden">Skip to main content</a>
  <div class="container">
    <header class="topbar">
      <div class="brand">
        <svg class="brand-icon" aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <rect x="2" y="14" width="20" height="8" rx="2"></rect>
          <path d="M6 18h.01"></path>
          <path d="M10 18h.01"></path>
          <path d="M12 14V6"></path>
          <path d="M8.5 9.5a5 5 0 0 1 7 0"></path>
          <path d="M5.5 6.5a9 9 0 0 1 13 0"></path>
        </svg>
        <div>
          <h1>Legacy Router &amp; Device Opener</h1>
          <p class="subtitle">Local TLS 1.0–1.3 Bridge &amp; Session Manager</p>
        </div>
      </div>
      <div id="proxy-status-pill" class="status-pill" role="status">
        <span class="status-dot" aria-hidden="true"></span>
        <span id="proxy-status-text">Proxy Running</span>
      </div>
    </header>

    <main id="main-content" tabindex="-1" class="container">
      <section class="card" aria-labelledby="config-heading">
        <div class="card-header">
          <h2 id="config-heading">Target Device &amp; Compatibility Preset</h2>
          <span id="active-proxy-url" class="hint">Listening on http://localhost:8080</span>
        </div>

        <form id="config-form" class="form-grid" novalidate>
          <div class="form-grid-2">
            <div class="field">
              <label for="target-ip">Target Router / Device IP (optional :port)</label>
              <div class="input-row">
                <input
                  id="target-ip"
                  name="target"
                  type="text"
                  value=""
                  placeholder="192.168.1.1 or 192.168.0.120:443"
                  required
                  autocorrect="off"
                  autocapitalize="none"
                  spellcheck="false"
                  enterkeyhint="go"
                  aria-describedby="target-hint target-error"
                >
                <button id="detect-btn" type="button" class="secondary compact">Detect Gateway</button>
              </div>
              <p id="target-hint" class="hint">
                Detected LAN Gateway: <strong id="detected-gateway">192.168.1.1</strong>.
                Prefix with <code>http://</code> for plain HTTP or add <code>:port</code> (e.g., <code>:8443</code>).
              </p>
              <p id="target-error" class="error-text" hidden></p>
            </div>

            <div class="field">
              <label for="preset-select">Hardware Compatibility Preset</label>
              <select id="preset-select" name="preset" aria-describedby="preset-hint"></select>
              <p id="preset-hint" class="hint">Select a hardware profile or leave on Auto / Universal.</p>
            </div>
          </div>

          <div class="actions-row">
            <button id="open-btn" type="submit" class="primary">Apply &amp; Open Device Dashboard ↗</button>
            <button id="save-btn" type="button" class="secondary">Apply Only</button>
            <button id="reset-btn" type="button" class="danger">Reset Stuck Session &amp; Cache</button>
          </div>
        </form>

        <div id="action-banner" class="status-banner" role="status" aria-live="polite" hidden></div>
      </section>

      <section class="card" aria-labelledby="telemetry-heading">
        <div class="card-header">
          <h2 id="telemetry-heading">Live Connection &amp; TLS Diagnostics</h2>
          <span id="telemetry-updated" class="hint">Waiting for first request</span>
        </div>
        <div class="telemetry-grid">
          <div class="stat-tile">
            <div class="stat-label">Active Target</div>
            <div id="stat-target" class="stat-value">—</div>
          </div>
          <div class="stat-tile">
            <div class="stat-label">Negotiated Protocol</div>
            <div id="stat-protocol" class="stat-value">—</div>
          </div>
          <div class="stat-tile">
            <div class="stat-label">Cipher Suite</div>
            <div id="stat-cipher" class="stat-value">—</div>
          </div>
          <div class="stat-tile">
            <div class="stat-label">Certificate / Key</div>
            <div id="stat-cert" class="stat-value">—</div>
          </div>
          <div class="stat-tile">
            <div class="stat-label">Sockets &amp; Cache</div>
            <div id="stat-pool" class="stat-value">—</div>
          </div>
        </div>
      </section>

      <section class="card" aria-labelledby="logs-heading">
        <div class="card-header">
          <h2 id="logs-heading">Live Proxy &amp; TLS Activity Logs</h2>
          <button id="clear-logs-btn" type="button" class="secondary compact">Clear Logs</button>
        </div>
        <div id="log-box" class="log-box" role="log" aria-label="Live proxy activity log">
          <div class="log-empty">Ready. Click "Apply &amp; Open Device Dashboard" to connect.</div>
        </div>
      </section>
    </main>
  </div>

  <script>
    (function () {
      const form = document.getElementById('config-form');
      const targetInput = document.getElementById('target-ip');
      const targetError = document.getElementById('target-error');
      const presetSelect = document.getElementById('preset-select');
      const presetHint = document.getElementById('preset-hint');
      const detectBtn = document.getElementById('detect-btn');
      const saveBtn = document.getElementById('save-btn');
      const openBtn = document.getElementById('open-btn');
      const resetBtn = document.getElementById('reset-btn');
      const clearLogsBtn = document.getElementById('clear-logs-btn');
      const actionBanner = document.getElementById('action-banner');
      const logBox = document.getElementById('log-box');
      const statusPill = document.getElementById('proxy-status-pill');
      const statusText = document.getElementById('proxy-status-text');

      let presets = [];
      let userEditedTarget = false;

      function showBanner(msg, kind) {
        actionBanner.textContent = msg;
        actionBanner.dataset.kind = kind || 'info';
        actionBanner.hidden = false;
      }

      function validateTarget() {
        const val = targetInput.value.trim();
        if (!val) {
          targetInput.setAttribute('aria-invalid', 'true');
          targetError.textContent = 'Enter a router or device IP/hostname (for example, 192.168.1.1).';
          targetError.hidden = false;
          return false;
        }
        if (/\s/.test(val)) {
          targetInput.setAttribute('aria-invalid', 'true');
          targetError.textContent = 'Target IP or hostname cannot contain spaces.';
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
        if (line.includes('❌') || line.includes('Error') || line.includes('FAILED')) return 'log-error';
        if (line.includes('🍪') || line.includes('Cookie') || line.includes('⚠️')) return 'log-warn';
        if (line.includes('🔄') || line.includes('Redirect') || line.includes('⚡')) return 'log-info';
        if (line.includes('📤') || line.includes('🚀')) return 'log-ok';
        return 'log-plain';
      }

      function renderLogs(logs) {
        if (!logs || logs.length === 0) {
          logBox.innerHTML = '<div class="log-empty">No log entries yet.</div>';
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

        if (state.preset && presetSelect.value !== state.preset && !presetSelect.matches(':focus')) {
          presetSelect.value = state.preset;
        }
        updatePresetDescription();

        if (!userEditedTarget && !targetInput.matches(':focus') && state.target) {
          targetInput.value = state.target;
        }

        document.getElementById('detected-gateway').textContent = state.detectedGateway || '192.168.1.1';
        document.getElementById('active-proxy-url').textContent =
          'Proxy URL: http://' + window.location.host + '/';
        statusText.textContent = 'Running • :' + state.proxyPort;
        statusPill.classList.remove('offline');

        document.getElementById('stat-target').textContent = state.resolvedTarget || state.target || '—';

        const tls = state.tls || {};
        document.getElementById('stat-protocol').textContent =
          tls.protocol && tls.protocol !== '—' ? (tls.transport + ' (' + tls.protocol + ')') : (tls.transport || 'Idle');
        document.getElementById('stat-cipher').textContent = tls.cipherSuite || '—';

        const certEl = document.getElementById('stat-cert');
        if (tls.peerSubject) {
          let certStr = tls.peerSubject;
          if (tls.peerKeyBits) certStr += ' • ' + tls.peerKeyBits;
          if (tls.certNotAfter) certStr += ' • exp ' + tls.certNotAfter;
          if (tls.certExpired) certStr += ' (EXPIRED / CLOCK SKEW)';
          certEl.textContent = certStr;
          certEl.className = tls.certExpired ? 'stat-value warn' : 'stat-value';
        } else {
          certEl.textContent = '—';
          certEl.className = 'stat-value';
        }

        document.getElementById('stat-pool').textContent =
          'Max ' + (state.maxSockets || 4) + ' sockets • ' + (state.cachedAssets || 0) + ' cached • ' + (state.cookieCount || 0) + ' cookies';

        document.getElementById('telemetry-updated').textContent =
          tls.updatedAt ? ('Last handshake: ' + tls.updatedAt) : 'Waiting for first request';

        if (Array.isArray(state.logs)) {
          renderLogs(state.logs);
        }
      }

      async function saveConfig(openDashboard) {
        if (!validateTarget()) {
          targetInput.focus();
          return;
        }

        openBtn.disabled = true;
        saveBtn.disabled = true;
        try {
          const res = await fetch('/_bridge/api/config', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
              target: targetInput.value.trim(),
              preset: presetSelect.value
            })
          });
          const data = await res.json();
          if (!res.ok) {
            showBanner('❌ ' + (data.error || 'Failed to update target.'), 'error');
            return;
          }
          userEditedTarget = false;
          applyState(data);
          showBanner('Target set to ' + data.resolvedTarget + ' (' + presetSelect.options[presetSelect.selectedIndex].text + ').', 'success');
          if (openDashboard) {
            window.open('/', '_blank', 'noopener');
          }
        } catch (err) {
          showBanner('❌ Could not communicate with local proxy: ' + err.message, 'error');
        } finally {
          openBtn.disabled = false;
          saveBtn.disabled = false;
        }
      }

      form.addEventListener('submit', (e) => {
        e.preventDefault();
        saveConfig(true);
      });

      saveBtn.addEventListener('click', () => {
        saveConfig(false);
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
            document.getElementById('detected-gateway').textContent = data.gateway;
            showBanner('Detected LAN gateway: ' + data.gateway + '. Click "Apply & Open Device Dashboard" to connect.', 'success');
          }
        } catch (err) {
          showBanner('❌ Gateway detection failed: ' + err.message, 'error');
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
            showBanner('Session cookies, browser auth state, and static asset cache cleared.', 'success');
          }
        } catch (err) {
          showBanner('❌ Reset failed: ' + err.message, 'error');
        } finally {
          resetBtn.disabled = false;
        }
      });

      clearLogsBtn.addEventListener('click', async () => {
        await fetch('/_bridge/api/logs/clear', { method: 'POST' });
        renderLogs([]);
      });

      async function loadInitialState() {
        try {
          const res = await fetch('/_bridge/api/state');
          if (res.ok) {
            applyState(await res.json());
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
        es.onerror = () => {
          statusPill.classList.add('offline');
          statusText.textContent = 'Reconnecting…';
        };
      }

      loadInitialState().then(connectSSE);
    })();
  </script>
</body>
</html>
`

// RenderErrorHTML renders a dark-mode 502 error page with an inline target switcher and Launchpad link.
func RenderErrorHTML(target string, errMsg string) string {
	safeTarget := html.EscapeString(target)
	safeErr := html.EscapeString(errMsg)

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="color-scheme" content="dark">
  <title>Connection Failed — Legacy Router Opener</title>
  <style>
    :root {
      color-scheme: dark;
      --bg: #0b0f17;
      --surface: #161e2e;
      --border: #475569;
      --text: #f8fafc;
      --muted: #cbd5e1;
      --red: #ef4444;
      --primary: #2563eb;
      --focus: #60a5fa;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      background: var(--bg);
      color: var(--text);
      font-family: system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
      display: grid;
      place-items: center;
      padding: 1.25rem;
    }
    main {
      width: 100%%;
      max-width: 540px;
      background: var(--surface);
      border: 1px solid #b91c1c;
      border-radius: 1rem;
      padding: 1.5rem;
    }
    h1 {
      margin: 0 0 0.5rem;
      font-size: 1.25rem;
      color: #fca5a5;
    }
    p { margin: 0 0 0.875rem; color: var(--muted); }
    pre {
      margin: 0 0 1.25rem;
      padding: 0.75rem;
      background: #090d16;
      border: 1px solid #334155;
      border-radius: 0.5rem;
      color: #e2e8f0;
      font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
      font-size: 0.8125rem;
      white-space: pre-wrap;
      word-break: break-word;
    }
    form {
      display: flex;
      flex-direction: column;
      gap: 0.75rem;
      margin-bottom: 1rem;
    }
    label { font-size: 0.875rem; font-weight: 600; color: var(--muted); }
    .row { display: flex; gap: 0.5rem; flex-wrap: wrap; }
    input {
      flex: 1 1 200px;
      min-height: 44px;
      padding: 0.5rem 0.75rem;
      font: inherit;
      font-size: max(1rem, 1em);
      color: var(--text);
      background: var(--bg);
      border: 1px solid var(--border);
      border-radius: 0.5rem;
    }
    button, a.btn {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      min-height: 44px;
      padding: 0.5rem 1rem;
      border-radius: 0.5rem;
      font: inherit;
      font-weight: 600;
      cursor: pointer;
      text-decoration: none;
      border: 1px solid transparent;
    }
    button.primary { background: var(--primary); color: #fff; }
    button.secondary, a.btn.secondary { background: #1e293b; color: var(--text); border-color: var(--border); }
    :where(a, button, input):focus-visible { outline: 3px solid var(--focus); outline-offset: 2px; }
  </style>
</head>
<body>
  <main>
    <h1>❌ Could Not Reach Device</h1>
    <p>Target endpoint: <strong>%s</strong></p>
    <pre>%s</pre>
    <form id="switch-form">
      <label for="new-target">Try a different IP / Port:</label>
      <div class="row">
        <input id="new-target" name="target" type="text" value="%s" required autocorrect="off" autocapitalize="none" spellcheck="false">
        <button type="submit" class="primary">Switch &amp; Connect</button>
      </div>
    </form>
    <div class="row">
      <button id="retry-btn" type="button" class="secondary">Retry Current Target</button>
      <a href="/_bridge" class="btn secondary">Open Control Launchpad</a>
    </div>
  </main>
  <script>
    document.getElementById('retry-btn').addEventListener('click', () => window.location.reload());
    document.getElementById('switch-form').addEventListener('submit', async (e) => {
      e.preventDefault();
      const target = document.getElementById('new-target').value.trim();
      if (!target) return;
      const res = await fetch('/_bridge/api/config', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ target })
      });
      if (res.ok) {
        window.location.reload();
      }
    });
  </script>
</body>
</html>`, safeTarget, safeErr, safeTarget)
}
