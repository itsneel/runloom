import { Injectable, signal } from '@angular/core';

export function documentationOrigin(value: unknown): string | undefined {
  if (typeof value !== 'string') return undefined;
  try {
    const url = new URL(value);
    if (
      !['http:', 'https:'].includes(url.protocol) ||
      url.username ||
      url.password ||
      url.pathname !== '/' ||
      url.search ||
      url.hash
    )
      return undefined;
    return url.origin;
  } catch {
    return undefined;
  }
}

@Injectable({ providedIn: 'root' })
export class DocumentationService {
  readonly url = signal('http://localhost:4400');
  constructor() {
    void fetch('/documentation.json', { cache: 'no-store', credentials: 'omit' })
      .then((response) => (response.ok ? response.json() : undefined))
      .then((config) => {
        const url = documentationOrigin(config?.url);
        if (url) this.url.set(url);
      })
      .catch(() => {
        /* Local development keeps the standalone docs default. */
      });
  }
}
