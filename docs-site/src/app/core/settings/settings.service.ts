import { afterNextRender, inject, Injectable, signal } from '@angular/core';
import { DOCUMENT } from '@angular/common';
import config from '../../../../site.config.json';
import { safeStudioUrl } from './destination';

@Injectable({ providedIn: 'root' })
export class SettingsService {
  private readonly document = inject(DOCUMENT);
  readonly dark = signal(false);
  readonly studioUrl = signal(safeStudioUrl(config.studioUrl) || 'http://localhost:4200');
  constructor() {
    afterNextRender(() => {
      this.dark.set(this.document.documentElement.dataset['theme'] === 'dark');
      void this.loadDestination();
      try {
        const saved = safeStudioUrl(localStorage.getItem('runloom-docs-studio') || '');
        if (saved) this.studioUrl.set(saved);
      } catch {
        /* Storage is optional. */
      }
    });
  }
  private async loadDestination(): Promise<void> {
    try {
      const response = await fetch(new URL('studio.json', this.document.baseURI), {
        cache: 'no-store',
        credentials: 'omit',
      });
      if (!response.ok) return;
      const url = safeStudioUrl((await response.json()).studioUrl || '');
      // User choice wins, including changes made while the config request is in flight.
      let saved: string | null = null;
      try {
        saved = safeStudioUrl(localStorage.getItem('runloom-docs-studio') || '');
      } catch {
        /* Optional storage. */
      }
      if (url && !saved) this.studioUrl.set(url);
    } catch {
      /* Static hosting may use the compiled default instead. */
    }
  }
  toggleTheme(): void {
    this.dark.update((value) => !value);
    const theme = this.dark() ? 'dark' : 'light';
    this.document.documentElement.dataset['theme'] = theme;
    try {
      localStorage.setItem('runloom-docs-theme', theme);
    } catch {
      /* Storage is optional. */
    }
  }
  setStudio(value: string): boolean {
    const url = safeStudioUrl(value);
    if (!url) return false;
    this.studioUrl.set(url);
    try {
      localStorage.setItem('runloom-docs-studio', url);
    } catch {
      /* Storage is optional. */
    }
    return true;
  }
}
