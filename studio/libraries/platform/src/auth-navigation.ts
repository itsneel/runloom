// Derive redirects from the current origin, never a caller-supplied return URL.
export function authReturnUrl(current: string): string {
  const url = new URL(current);
  for (const key of ['code', 'state', 'session_state', 'iss', 'error', 'error_description', 'signedOut']) {
    url.searchParams.delete(key);
  }
  return url.toString();
}
