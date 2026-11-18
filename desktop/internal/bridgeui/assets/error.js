import { activateLanguage } from './locale.js';

const locale = activateLanguage();
const form = document.getElementById('switch-form');
const input = document.getElementById('new-target');
const submit = document.getElementById('switch-btn');
const feedback = document.getElementById('switch-error');
const retry = document.getElementById('retry-btn');

function showError(key) {
  locale.setMessage(feedback, key);
  feedback.hidden = false;
}

retry.addEventListener('click', () => window.location.reload());

input.addEventListener('input', () => {
  input.removeAttribute('aria-invalid');
  feedback.hidden = true;
});

form.addEventListener('submit', async (e) => {
  e.preventDefault();
  const target = input.value.trim();
  if (!target || /\s/.test(target)) {
    showError(target ? 'address_spaces' : 'address_required');
    input.setAttribute('aria-invalid', 'true');
    input.focus();
    return;
  }

  feedback.hidden = true;
  input.disabled = true;
  submit.disabled = true;
  retry.disabled = true;
  try {
    const res = await fetch('/_bridge/api/config', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ target }),
    });
    if (res.ok) {
      window.location.reload();
      return;
    }
    showError('changes_failed');
  } catch {
    showError('changes_failed');
  }
  input.disabled = false;
  submit.disabled = false;
  retry.disabled = false;
  input.focus();
});
