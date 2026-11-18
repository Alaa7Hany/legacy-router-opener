package proxy

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"
)

// RenderBridgeHTML renders the minimalist opener card pre-hydrated with the active server snapshot.
func RenderBridgeHTML(snap StateSnapshot) string {
	target := strings.TrimSpace(snap.Target)
	if target == "" {
		target = "192.168.1.1"
	}
	gw := strings.TrimSpace(snap.DetectedGateway)
	if gw == "" {
		gw = "192.168.1.1"
	}
	safeTarget := html.EscapeString(target)
	safeGW := html.EscapeString(gw)

	snapBytes, _ := json.Marshal(snap)
	safeJSON := strings.ReplaceAll(string(snapBytes), "</", `<\/`)

	page := bridgePageTemplate
	page = strings.ReplaceAll(page, "__INIT_TARGET__", safeTarget)
	page = strings.ReplaceAll(page, "__INIT_GATEWAY__", safeGW)
	page = strings.Replace(page, "__INIT_STATE_JSON__", safeJSON, 1)
	return page
}

const bridgePageTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="color-scheme" content="light dark">
  <title>Router Page Opener</title>
  <style>
    :root {
      color-scheme: light dark;
      --bg: #f4f3ef;
      --card: #ffffff;
      --surface-alt: #f9f8f6;
      --terminal-bg: #fafaf9;
      --border: #d6d3cd;
      --border-strong: #a8a29e;
      --text: #1c1917;
      --text-muted: #57534e;
      --primary: #1d4ed8;
      --primary-hover: #1e40af;
      --primary-text: #ffffff;
      --link: #1d4ed8;
      --ok: #15803d;
      --warn: #b45309;
      --err: #b91c1c;
      --info: #0369a1;
      --focus: #2563eb;
      --shadow: 0 1px 3px rgba(28, 25, 23, 0.08);
      --scrollbar-track: #e7e5e4;
      --scrollbar-thumb: #78716c;
    }

    @media (prefers-color-scheme: dark) {
      :root {
        --bg: #18181b;
        --card: #222226;
        --surface-alt: #1c1c20;
        --terminal-bg: #141417;
        --border: #3f3f46;
        --border-strong: #52525b;
        --text: #f4f4f5;
        --text-muted: #a1a1aa;
        --primary: #2563eb;
        --primary-hover: #1d4ed8;
        --primary-text: #ffffff;
        --link: #93c5fd;
        --ok: #4ade80;
        --warn: #fbbf24;
        --err: #f87171;
        --info: #7dd3fc;
        --focus: #60a5fa;
        --shadow: 0 2px 6px rgba(0, 0, 0, 0.35);
        --scrollbar-track: #18181b;
        --scrollbar-thumb: #52525b;
      }
    }

    .log-box {
      scrollbar-color: var(--scrollbar-thumb) var(--scrollbar-track);
    }

    @supports not (scrollbar-color: auto) {
      .log-box {
        scrollbar-width: auto;
      }
      .log-box::-webkit-scrollbar {
        width: 8px;
        height: 8px;
      }
      .log-box::-webkit-scrollbar-track {
        background: var(--scrollbar-track);
      }
      .log-box::-webkit-scrollbar-thumb {
        background: var(--scrollbar-thumb);
        border-radius: 4px;
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
      display: grid;
      place-items: center;
      padding: 1.5rem 1rem;
    }

    .card {
      width: 100%;
      max-width: 420px;
      background-color: var(--card);
      border: 1px solid var(--border);
      border-radius: 0.75rem;
      box-shadow: var(--shadow);
      padding: 1.5rem;
    }

    .header {
      margin-bottom: 1.25rem;
    }

    h1 {
      margin: 0 0 0.25rem;
      font-size: 1.25rem;
      font-weight: 700;
      letter-spacing: -0.01em;
    }

    .subtitle {
      margin: 0;
      font-size: 0.875rem;
      color: var(--text-muted);
    }

    .primary-btn {
      width: 100%;
      min-height: 50px;
      padding: 0.75rem 1rem;
      background-color: var(--primary);
      color: var(--primary-text);
      border: 1px solid transparent;
      border-radius: 0.5rem;
      font: inherit;
      font-size: 1rem;
      font-weight: 600;
      cursor: pointer;
      display: inline-flex;
      align-items: center;
      justify-content: center;
      gap: 0.375rem;
    }

    .primary-btn:hover {
      background-color: var(--primary-hover);
    }

    .primary-btn:disabled,
    .secondary-btn:disabled,
    .link-btn:disabled {
      opacity: 0.65;
      cursor: wait;
    }

    .helper-row {
      margin-top: 0.75rem;
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      justify-content: center;
      gap: 0.35rem;
      font-size: 0.84rem;
      color: var(--text-muted);
      text-align: center;
    }

    .link-btn {
      background: none;
      border: none;
      padding: 0.15rem 0.25rem;
      margin: 0;
      font: inherit;
      font-size: inherit;
      font-weight: 600;
      color: var(--link);
      cursor: pointer;
      text-decoration: underline;
      text-underline-offset: 2px;
    }

    .status-banner {
      margin-top: 0.875rem;
      padding: 0.625rem 0.75rem;
      border-radius: 0.5rem;
      border: 1px solid var(--border);
      background-color: var(--surface-alt);
      font-size: 0.84rem;
      color: var(--text);
    }

    .status-banner[data-kind="success"] {
      border-color: var(--ok);
      color: var(--ok);
    }

    .status-banner[data-kind="error"] {
      border-color: var(--err);
      color: var(--err);
    }

    .disclosures {
      margin-top: 1.25rem;
      border-top: 1px solid var(--border);
      display: flex;
      flex-direction: column;
    }

    details {
      border-bottom: 1px solid var(--border);
    }

    summary {
      list-style: none;
      cursor: pointer;
      padding: 0.75rem 0.125rem;
      font-size: 0.9rem;
      font-weight: 600;
      color: var(--text);
      display: flex;
      align-items: center;
      justify-content: space-between;
      user-select: none;
    }

    summary::-webkit-details-marker {
      display: none;
    }

    summary::after {
      content: "+";
      font-size: 1rem;
      font-weight: 400;
      color: var(--text-muted);
      line-height: 1;
    }

    details[open] > summary::after {
      content: "−";
    }

    .panel-body {
      padding: 0.125rem 0 1rem;
      display: flex;
      flex-direction: column;
      gap: 0.875rem;
    }

    .field {
      display: flex;
      flex-direction: column;
      gap: 0.3rem;
    }

    .label-row {
      display: flex;
      flex-wrap: wrap;
      align-items: baseline;
      justify-content: space-between;
      gap: 0.5rem;
      font-size: 0.84rem;
    }

    label {
      font-size: 0.84rem;
      font-weight: 600;
      color: var(--text);
    }

    input[type="text"],
    select {
      width: 100%;
      min-height: 42px;
      padding: 0.5rem 0.75rem;
      font: inherit;
      font-size: max(1rem, 1em);
      color: var(--text);
      background-color: var(--bg);
      border: 1px solid var(--border-strong);
      border-radius: 0.45rem;
    }

    input[aria-invalid="true"] {
      border-color: var(--err);
    }

    .hint {
      margin: 0;
      font-size: 0.78rem;
      color: var(--text-muted);
    }

    .error-text {
      margin: 0;
      font-size: 0.82rem;
      font-weight: 600;
      color: var(--err);
    }

    .error-text[hidden] {
      display: none;
    }

    .secondary-btn {
      width: 100%;
      min-height: 40px;
      padding: 0.5rem 0.875rem;
      background-color: var(--surface-alt);
      color: var(--text);
      border: 1px solid var(--border-strong);
      border-radius: 0.45rem;
      font: inherit;
      font-size: 0.875rem;
      font-weight: 600;
      cursor: pointer;
    }

    .secondary-btn:hover {
      border-color: var(--text-muted);
    }

    .logs-meta {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 0.5rem;
      font-size: 0.78rem;
      color: var(--text-muted);
    }

    .tls-line {
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .log-box {
      background-color: var(--terminal-bg);
      border: 1px solid var(--border);
      border-radius: 0.45rem;
      padding: 0.625rem;
      height: 185px;
      overflow-y: auto;
      font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
      font-size: 0.76rem;
      line-height: 1.45;
    }

    .log-empty {
      color: var(--text-muted);
    }

    .log-line {
      padding: 0.1rem 0;
      white-space: pre-wrap;
      word-break: break-word;
    }

    .log-error { color: var(--err); }
    .log-warn  { color: var(--warn); }
    .log-info  { color: var(--info); }
    .log-ok    { color: var(--ok); }
    .log-plain { color: var(--text); }

    :where(a, button, input, select, summary):focus-visible {
      outline: 3px solid var(--focus);
      outline-offset: 2px;
    }

    @media (forced-colors: active) {
      .card, .status-banner, .log-box, input, select, .secondary-btn {
        border: 1px solid CanvasText;
      }
      .primary-btn {
        border: 2px solid ButtonText;
      }
    }
  </style>
</head>
<body>
  <main class="card">
    <header class="header">
      <h1>Router Page Opener</h1>
      <p id="target-subtitle" class="subtitle">Ready to open <strong>__INIT_TARGET__</strong></p>
    </header>

    <form id="config-form" novalidate>
      <button id="open-btn" type="submit" class="primary-btn">Open Router Page (__INIT_TARGET__) ↗</button>

      <div class="helper-row">
        <span>Page stuck or says already logged in?</span>
        <button id="reset-btn" type="button" class="link-btn">Reset session</button>
      </div>

      <div id="action-banner" class="status-banner" role="status" aria-live="polite" hidden></div>

      <div class="disclosures">
        <details id="options-panel">
          <summary>Change IP or device type</summary>
          <div class="panel-body">
            <div class="field">
              <div class="label-row">
                <label for="target-ip">Router or device address</label>
                <button id="detect-btn" type="button" class="link-btn">Use detected (<span id="detected-gateway">__INIT_GATEWAY__</span>)</button>
              </div>
              <input
                id="target-ip"
                name="target"
                type="text"
                value="__INIT_TARGET__"
                placeholder="192.168.1.1"
                required
                autocorrect="off"
                autocapitalize="none"
                spellcheck="false"
                enterkeyhint="go"
                aria-describedby="target-hint target-error"
              >
              <p id="target-hint" class="hint">Add :port or http:// if needed (e.g. 192.168.0.1 or 10.0.0.5:8443).</p>
              <p id="target-error" class="error-text" hidden></p>
            </div>

            <div class="field">
              <label for="preset-select">Device compatibility profile</label>
              <select id="preset-select" name="preset" aria-describedby="preset-hint"></select>
              <p id="preset-hint" class="hint">Balanced defaults for home routers and older hardware.</p>
            </div>

            <button id="save-btn" type="button" class="secondary-btn">Apply changes</button>
          </div>
        </details>

        <details id="logs-panel">
          <summary>Connection logs</summary>
          <div class="panel-body">
            <div class="logs-meta">
              <span id="tls-summary" class="tls-line">No connections yet</span>
              <button id="clear-logs-btn" type="button" class="link-btn">Clear</button>
            </div>
            <div id="log-box" class="log-box" role="log" aria-label="Connection activity log">
              <div class="log-empty">No connection activity yet.</div>
            </div>
          </div>
        </details>
      </div>
    </form>
  </main>

  <script id="initial-state" type="application/json">__INIT_STATE_JSON__</script>
  <script>
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
  </script>
</body>
</html>
`

// RenderErrorHTML renders a compact, classic 502 error page with an inline target switcher and Launchpad link.
func RenderErrorHTML(target string, errMsg string) string {
	safeTarget := html.EscapeString(target)
	safeErr := html.EscapeString(errMsg)

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="color-scheme" content="light dark">
  <title>Could Not Reach Device — Router Page Opener</title>
  <style>
    :root {
      color-scheme: light dark;
      --bg: #f4f3ef;
      --card: #ffffff;
      --surface-alt: #f9f8f6;
      --border: #d6d3cd;
      --border-strong: #a8a29e;
      --text: #1c1917;
      --text-muted: #57534e;
      --primary: #1d4ed8;
      --primary-hover: #1e40af;
      --err: #b91c1c;
      --focus: #2563eb;
    }
    @media (prefers-color-scheme: dark) {
      :root {
        --bg: #18181b;
        --card: #222226;
        --surface-alt: #1c1c20;
        --border: #3f3f46;
        --border-strong: #52525b;
        --text: #f4f4f5;
        --text-muted: #a1a1aa;
        --primary: #2563eb;
        --primary-hover: #1d4ed8;
        --err: #f87171;
        --focus: #60a5fa;
      }
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
      max-width: 440px;
      background: var(--card);
      border: 1px solid var(--border);
      border-radius: 0.75rem;
      padding: 1.5rem;
    }
    h1 {
      margin: 0 0 0.375rem;
      font-size: 1.2rem;
      color: var(--err);
    }
    p { margin: 0 0 0.875rem; color: var(--text-muted); font-size: 0.9rem; }
    pre {
      margin: 0 0 1.25rem;
      padding: 0.75rem;
      background: var(--surface-alt);
      border: 1px solid var(--border);
      border-radius: 0.45rem;
      color: var(--text);
      font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
      font-size: 0.78rem;
      white-space: pre-wrap;
      word-break: break-word;
    }
    form {
      display: flex;
      flex-direction: column;
      gap: 0.5rem;
      margin-bottom: 1rem;
    }
    label { font-size: 0.84rem; font-weight: 600; }
    .row { display: flex; gap: 0.5rem; flex-wrap: wrap; }
    input {
      flex: 1 1 190px;
      min-height: 42px;
      padding: 0.5rem 0.75rem;
      font: inherit;
      font-size: max(1rem, 1em);
      color: var(--text);
      background: var(--bg);
      border: 1px solid var(--border-strong);
      border-radius: 0.45rem;
    }
    button, a.btn {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      min-height: 42px;
      padding: 0.5rem 0.9rem;
      border-radius: 0.45rem;
      font: inherit;
      font-size: 0.875rem;
      font-weight: 600;
      cursor: pointer;
      text-decoration: none;
      border: 1px solid transparent;
    }
    button.primary { background: var(--primary); color: #fff; }
    button.primary:hover { background: var(--primary-hover); }
    button.secondary, a.btn.secondary {
      background: var(--surface-alt);
      color: var(--text);
      border-color: var(--border-strong);
    }
    :where(a, button, input):focus-visible { outline: 3px solid var(--focus); outline-offset: 2px; }
  </style>
</head>
<body>
  <main>
    <h1>Could Not Reach Device</h1>
    <p>No response from <strong>%s</strong></p>
    <pre>%s</pre>
    <form id="switch-form">
      <label for="new-target">Try another address or port:</label>
      <div class="row">
        <input id="new-target" name="target" type="text" value="%s" required autocorrect="off" autocapitalize="none" spellcheck="false">
        <button type="submit" class="primary">Switch &amp; Retry</button>
      </div>
    </form>
    <div class="row">
      <button id="retry-btn" type="button" class="secondary">Retry</button>
      <a href="/_bridge" class="btn secondary">Back to Opener</a>
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
