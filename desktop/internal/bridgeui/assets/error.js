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
