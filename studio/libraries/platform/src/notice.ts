import { ChangeDetectorRef, Component, ElementRef, EventEmitter, Input, OnChanges, OnDestroy, Output, inject } from '@angular/core';

export type NoticeTone = 'success' | 'error' | 'info' | 'warning';

/** Shared feedback surface. Errors and inline guidance never disappear on a timer. */
@Component({
  selector: 'runloom-notice',
  templateUrl: './notice.html',
  styleUrl: './notice.scss',
  host: { '[class.toast]': 'floating', '[attr.data-tone]': 'tone' },
})
export class Notice implements OnChanges, OnDestroy {
  @Input() message = '';
  @Input() title = '';
  @Input() tone: NoticeTone = 'info';
  @Input() floating = false;
  @Input() dismissible = true;
  @Input() revision = 0;
  @Input() dismissLabel = 'Dismiss notification';
  @Output() dismissed = new EventEmitter<void>();
  private readonly element = inject(ElementRef<HTMLElement>);
  private readonly cd = inject(ChangeDetectorRef);
  private timer?: ReturnType<typeof setTimeout>;
  private remaining = 6000;
  private started = 0;
  private hovered = false;
  private focused = false;
  get heading(): string {
    return this.title || { success: 'All set', error: 'Something needs your attention', info: 'Good to know', warning: 'Before you continue' }[this.tone];
  }
  get timed(): boolean { return this.floating && this.dismissible && this.tone === 'success'; }
  get paused(): boolean { return this.hovered || this.focused; }
  ngOnChanges(): void { this.clear(); this.remaining = 6000; this.resume(); }
  ngOnDestroy(): void { this.clear(); }
  close(): void { this.clear(); this.dismissed.emit(); this.cd.markForCheck(); }
  hover(value: boolean): void { this.hovered = value; this.updateTimer(); }
  focus(value: boolean, event?: FocusEvent): void {
    if (!value && event?.relatedTarget instanceof Node && this.element.nativeElement.contains(event.relatedTarget)) return;
    this.focused = value; this.updateTimer();
  }
  private updateTimer(): void {
    if (this.paused) {
      if (this.timer !== undefined) this.remaining = Math.max(0, this.remaining - (performance.now() - this.started));
      this.clear();
    } else this.resume();
  }
  private resume(): void {
    if (!this.timed || this.paused || this.timer !== undefined) return;
    this.started = performance.now();
    this.timer = setTimeout(() => this.close(), this.remaining);
  }
  private clear(): void { if (this.timer !== undefined) clearTimeout(this.timer); this.timer = undefined; }
}
