const storageKey = 'legacy-router-opener-language';

export function activateLanguage(onChange = () => {}) {
  const catalog = JSON.parse(document.getElementById('messages').textContent);
  const root = document.documentElement;
  const picker = document.querySelector('.language-picker');
  const trigger = document.getElementById('language-switch');
  const options = picker.querySelectorAll('.language-option');
  const quit = document.getElementById('quit-link');
  let language = root.lang;

  try {
    const saved = localStorage.getItem(storageKey);
    if (saved === 'ar' || saved === 'en') language = saved;
  } catch {
    // The switch still works on this page when browser storage is unavailable.
  }

  const text = (key) => catalog[language][key];
  const direction = (lang) => lang === 'ar' ? 'rtl' : 'ltr';

  function render() {
    root.lang = language;
    root.dir = direction(language);
    for (const node of document.querySelectorAll('[data-message]')) {
      node.textContent = text(node.dataset.message);
    }
    for (const node of document.querySelectorAll('[data-message-label]')) {
      node.setAttribute('aria-label', text(node.dataset.messageLabel));
    }
    for (const option of options) {
      option.setAttribute('aria-pressed', String(option.dataset.language === language));
    }
    if (quit) {
      const address = new URL(quit.href);
      address.searchParams.set('lang', language);
      quit.href = address.href;
    }
  }

  for (const option of options) {
    option.addEventListener('click', () => {
      language = option.dataset.language;
      try {
        localStorage.setItem(storageKey, language);
      } catch {
        // Keep the current selection in memory for this page.
      }
      picker.open = false;
      render();
      onChange();
      trigger.focus();
    });
  }
  document.addEventListener('click', (event) => {
    if (!picker.contains(event.target)) picker.open = false;
  });
  picker.addEventListener('keydown', (event) => {
    if (event.key === 'Escape' && picker.open) {
      picker.open = false;
      trigger.focus();
    }
  });
  render();

  return {
    text,
    setMessage(node, key) {
      node.dataset.message = key;
      node.textContent = text(key);
    },
  };
}
