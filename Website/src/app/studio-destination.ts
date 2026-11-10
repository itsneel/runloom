// Optional hosting metadata; no dependency on Studio code, APIs or credentials.
export function studioDestination(value: string | undefined, fallback: string): string {
  if (!value) return fallback;
  try {
    const url = new URL(value);
    if (!["http:", "https:"].includes(url.protocol) || url.username || url.password) return fallback;
    return url.href;
  } catch {
    return fallback;
  }
}
