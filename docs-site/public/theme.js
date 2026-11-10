try {
  const saved = localStorage.getItem('runloom-docs-theme');
  document.documentElement.dataset.theme =
    saved === 'light' || saved === 'dark'
      ? saved
      : matchMedia('(prefers-color-scheme: dark)').matches
        ? 'dark'
        : 'light';
} catch {
  /* Documentation remains readable without browser storage. */
}
