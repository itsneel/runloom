export function withinDates(timestamp: string, from: string, to: string): boolean {
  const value = Date.parse(timestamp);
  return (
    (!from || value >= Date.parse(from + 'T00:00:00Z')) &&
    (!to || value < Date.parse(to + 'T00:00:00Z') + 86400000)
  );
}
export function sorted<T>(items: T[], key: (item: T) => string | number, ascending: boolean): T[] {
  return [...items].sort((a, b) => {
    const x = key(a),
      y = key(b);
    const result =
      typeof x === 'number' && typeof y === 'number'
        ? x - y
        : String(x).localeCompare(String(y), undefined, { numeric: true });
    return ascending ? result : -result;
  });
}
export function errorMessage(error: unknown): string {
  const e = error as { error?: { detail?: string }; message?: string };
  return e?.error?.detail || e?.message || 'Unable to complete the request. Please try again.';
}
