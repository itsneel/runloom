// Resolve before styles paint. No credentials or account data are stored here.
(() => {
  let preference = 'system';
  try {
    const saved = localStorage.getItem('runloom.theme');
    if (saved === 'light' || saved === 'dark') preference = saved;
  } catch { /* Storage can be unavailable in private or restricted browsers. */ }
  document.documentElement.dataset.theme = preference === 'system'
    ? (matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light')
    : preference;
})();
