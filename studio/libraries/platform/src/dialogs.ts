import { Component, ElementRef, effect, inject, signal, viewChild } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { DialogService } from './dialog.service';

@Component({
  selector: 'runloom-dialogs',
  imports: [FormsModule],
  templateUrl: './dialogs.html',
  styleUrl: './dialogs.scss',
})
export class Dialogs {
  readonly dialogs = inject(DialogService);
  readonly value = signal('');
  readonly error = signal('');
  private readonly element = viewChild<ElementRef<HTMLDialogElement>>('modal');

  constructor() {
    effect(() => {
      const request = this.dialogs.active();
      const modal = this.element()?.nativeElement;
      if (!modal) return;
      if (!request) { if (modal.open) modal.close(); return; }
      this.value.set(request.initialValue ?? '');
      this.error.set('');
      queueMicrotask(() => {
        if (this.dialogs.active()?.id !== request.id || !modal.isConnected) return;
        if (!modal.open) modal.showModal();
        const target = modal.querySelector<HTMLElement>(request.kind === 'prompt' ? 'input' : '[data-testid="dialog-cancel"]');
        target?.focus();
        if (target instanceof HTMLInputElement) target.select();
      });
    });
  }

  cancel(event?: Event): void {
    event?.preventDefault();
    const request = this.dialogs.active();
    if (request) this.dialogs.finish(request.id, null);
  }

  trapFocus(event: KeyboardEvent): void {
    if (event.key !== 'Tab') return;
    const targets = this.element()?.nativeElement.querySelectorAll<HTMLElement>('input, button');
    if (!targets?.length) return;
    const first = targets[0], last = targets[targets.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault(); last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault(); first.focus();
    }
  }

  submit(): void {
    const request = this.dialogs.active();
    if (!request) return;
    if (request.kind === 'prompt') {
      const value = this.value().trim();
      if (!value || [...value].length > 80 || /\p{Cc}/u.test(value)) {
        this.error.set('Enter a project name between 1 and 80 characters, without control characters.');
        return;
      }
      this.dialogs.finish(request.id, value);
    } else this.dialogs.finish(request.id, true);
  }
}
