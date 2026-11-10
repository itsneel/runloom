import {
  Component,
  ElementRef,
  HostListener,
  Input,
  ViewChild,
  inject,
  Output,
  EventEmitter,
  Injector,
  afterNextRender,
  ChangeDetectorRef,
  OnDestroy,
} from '@angular/core';
import { RouterLink } from '@angular/router';
import { AuthService, ThemePicker, UiIcon } from '@runloom/studio-platform';
import { PlatformAccess } from '../../../../core/api/platform-access.service';

export interface PlatformSection {
  id: string;
  label: string;
  permission: string;
  icon: string;
}

@Component({
  selector: 'runloom-platform-navigation',
  imports: [RouterLink, ThemePicker, UiIcon],
  templateUrl: './platform-navigation.component.html',
  styleUrl: './platform-navigation.component.scss',
  host: { '[class.collapsed]': 'collapsed && !mobile', '[class.mobile]': 'mobile' },
})
export class PlatformNavigation implements OnDestroy {
  @Input() section = 'health';
  @Input() sections: PlatformSection[] = [];
  @Output() openChange = new EventEmitter<boolean>();
  @ViewChild('navigationToggle') toggleButton?: ElementRef<HTMLButtonElement>;
  @ViewChild('mobileToggle') mobileButton?: ElementRef<HTMLButtonElement>;
  readonly access = inject(PlatformAccess);
  readonly auth = inject(AuthService);
  private readonly injector = inject(Injector);
  private readonly cd = inject(ChangeDetectorRef);
  railTooltip?: { label: string; left: number; top: number };
  private tooltipTimer?: ReturnType<typeof setTimeout>;
  collapsed = this.readCollapsed();
  mobile = window.matchMedia('(max-width: 899px)').matches;
  open = false;
  get roleLabel(): string {
    return this.access.identity().role === 'platform_admin'
      ? 'Platform administrator'
      : this.access.identity().role === 'platform_staff'
        ? 'Restricted staff'
        : 'Platform access';
  }
  private readCollapsed(): boolean {
    try {
      return localStorage.getItem('runloom-platform-navigation-collapsed') === 'true';
    } catch {
      return false;
    }
  }
  toggle(): void {
    this.hideRailTooltip();
    if (this.mobile) {
      this.open = !this.open;
      this.openChange.emit(this.open);
      if (this.open)
        afterNextRender(() => this.toggleButton?.nativeElement.focus(), {
          injector: this.injector,
        });
    } else {
      this.collapsed = !this.collapsed;
      try {
        localStorage.setItem('runloom-platform-navigation-collapsed', String(this.collapsed));
      } catch {}
    }
  }
  close(restoreFocus = false): void {
    this.hideRailTooltip();
    this.open = false;
    this.openChange.emit(false);
    if (restoreFocus)
      afterNextRender(() => this.mobileButton?.nativeElement.focus(), { injector: this.injector });
  }
  @HostListener('window:resize') resized(): void {
    this.hideRailTooltip();
    const mobile = window.matchMedia('(max-width: 899px)').matches;
    if (mobile !== this.mobile) {
      this.mobile = mobile;
      this.close();
    }
  }
  @HostListener('window:keydown.escape') escape(): void {
    this.hideRailTooltip();
    if (document.querySelector('runloom-theme-picker [popover]:popover-open')) return;
    if (this.mobile && this.open) this.close(true);
  }
  showRailTooltip(event: Event): void {
    if (!this.collapsed || this.mobile || !(event.target instanceof Element)) return;
    const target = event.target.closest<HTMLElement>(
      'a[aria-label], button[aria-label], [data-tooltip]',
    );
    if (!target || (target instanceof HTMLButtonElement && target.disabled)) return;
    const label = target.getAttribute('data-tooltip') || target.getAttribute('aria-label');
    if (!label) return;
    this.keepRailTooltip();
    const rect = target.getBoundingClientRect();
    this.railTooltip = {
      label,
      left: (target.closest('aside')?.getBoundingClientRect().right || rect.right) + 8,
      top: Math.max(24, Math.min(window.innerHeight - 24, rect.top + rect.height / 2)),
    };
  }
  keepRailTooltip(): void {
    clearTimeout(this.tooltipTimer);
  }
  deferHideRailTooltip(): void {
    this.keepRailTooltip();
    this.tooltipTimer = setTimeout(() => {
      this.hideRailTooltip();
      this.cd.markForCheck();
    }, 120);
  }
  hideRailTooltip(): void {
    this.keepRailTooltip();
    this.railTooltip = undefined;
  }
  ngOnDestroy(): void {
    this.hideRailTooltip();
  }
  @HostListener('keydown', ['$event']) trapFocus(event: KeyboardEvent): void {
    if (!this.mobile || !this.open || event.key !== 'Tab') return;
    const sidebar = this.toggleButton?.nativeElement.closest('aside');
    const controls = [
      ...(sidebar?.querySelectorAll<HTMLElement>('a[href], button:not([disabled])') || []),
    ].filter((element) => element.getClientRects().length);
    const first = controls[0],
      last = controls.at(-1);
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last?.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first?.focus();
    }
  }
}
