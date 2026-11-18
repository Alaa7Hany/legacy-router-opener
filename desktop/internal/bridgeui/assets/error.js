import { activateLanguage } from './locale.js';

activateLanguage();
const retry = document.getElementById('retry-btn');
retry?.addEventListener('click', () => window.location.reload());
