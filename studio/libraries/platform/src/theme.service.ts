import { DestroyRef, Injectable, inject, signal } from '@angular/core';

export type ThemePreference = 'light' | 'dark' | 'system';
const storageKey = 'runloom.theme';
const normalize = (value: string | null): ThemePreference =>
  value === 'light' || value === 'dark' ? value : 'system';

@Injectable({ providedIn: 'root' })
export class ThemeService {
  private readonly media = window.matchMedia('(prefers-color-scheme: dark)');
  readonly preference = signal<ThemePreference>('system');
  readonly resolved = signal<'light' | 'dark'>('light');
  private frame = 0;

  constructor() {
    try { this.preference.set(normalize(localStorage.getItem(storageKey))); } catch {}
    this.apply();
    const systemChanged = () => this.apply();
    const storageChanged = (event: StorageEvent) => {
      if (event.key === storageKey || event.key === null) {
        this.preference.set(normalize(event.newValue));
        this.apply();
      }
    };
    this.media.addEventListener('change', systemChanged);
    window.addEventListener('storage', storageChanged);
    inject(DestroyRef).onDestroy(() => {
      cancelAnimationFrame(this.frame);
      document.documentElement.classList.remove('theme-changing');
      this.media.removeEventListener('change', systemChanged);
      window.removeEventListener('storage', storageChanged);
    });
  }

  setPreference(value: ThemePreference): void {
    this.preference.set(normalize(value));
    try { localStorage.setItem(storageKey, this.preference()); } catch {}
    this.apply();
  }

  private apply(): void {
    const theme = this.preference() === 'system'
      ? (this.media.matches ? 'dark' : 'light') : this.preference() as 'light' | 'dark';
    this.resolved.set(theme);
    const root = document.documentElement;
    if (root.dataset['theme'] !== theme) {
      cancelAnimationFrame(this.frame);
      root.classList.add('theme-changing');
      root.dataset['theme'] = theme;
      this.frame = requestAnimationFrame(() => {
        this.frame = requestAnimationFrame(() => root.classList.remove('theme-changing'));
      });
    }
  }
}
