import { DestroyRef, Injectable, inject, signal } from '@angular/core';

interface DialogOptions {
  title?: string;
  confirmText?: string;
  cancelText?: string;
  danger?: boolean;
  initialValue?: string;
}
interface DialogRequest extends DialogOptions {
  id: symbol;
  owner: symbol;
  kind: 'confirm' | 'prompt';
  message: string;
  resolve: (value: string | boolean | null) => void;
}

@Injectable({ providedIn: 'root' })
export class DialogService {
  readonly active = signal<DialogRequest | null>(null);

  open(owner: symbol, kind: DialogRequest['kind'], message: string, options: DialogOptions): Promise<string | boolean | null> {
    // Never queue destructive actions behind another confirmation.
    if (this.active()) return Promise.resolve(null);
    return new Promise(resolve => this.active.set({ ...options, id: Symbol(), owner, kind, message, resolve }));
  }

  finish(id: symbol, value: string | boolean | null): void {
    const request = this.active();
    if (!request || request.id !== id) return;
    this.active.set(null);
    request.resolve(value);
  }

  cancel(owner: symbol): void {
    const request = this.active();
    if (request?.owner === owner) this.finish(request.id, null);
  }
}

/** Pending decisions belong to the requesting screen, not its next replacement. */
export function injectDialogs() {
  const service = inject(DialogService);
  const owner = Symbol();
  let destroyed = false;
  inject(DestroyRef).onDestroy(() => { destroyed = true; service.cancel(owner); });
  return {
    confirm: async (message: string, options: DialogOptions = {}): Promise<boolean> =>
      !destroyed && await service.open(owner, 'confirm', message, options) === true,
    prompt: async (message: string, options: DialogOptions = {}): Promise<string | null> => {
      const result = destroyed ? null : await service.open(owner, 'prompt', message, options);
      return typeof result === 'string' ? result : null;
    },
    cancel: () => service.cancel(owner),
  };
}
