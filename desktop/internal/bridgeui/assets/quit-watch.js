let events;
window.addEventListener('pageshow', () => {
  events = new EventSource('/quit/events');
});
window.addEventListener('pagehide', () => events?.close());
