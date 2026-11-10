import { Component, ElementRef, Input, ViewChild, inject } from '@angular/core';
import { ThemePreference, ThemeService } from './theme.service';
import { UiIcon } from './ui-icon';

@Component({
  selector: 'runloom-theme-picker',
  imports: [UiIcon],
  templateUrl: './theme-picker.html',
  styleUrl: './theme-picker.scss',
})
export class ThemePicker {
  @Input() compact = false;
  @ViewChild('panel') panel?: ElementRef<HTMLElement>;
  @ViewChild('trigger') trigger?: ElementRef<HTMLButtonElement>;
  readonly theme = inject(ThemeService);
  readonly options = [
    { value: 'light' as const, label: 'Light', icon: 'sun' },
    { value: 'dark' as const, label: 'Dark', icon: 'moon' },
    { value: 'system' as const, label: 'System', icon: 'monitor' },
  ];
  left = 12;
  bottom = 80;
  position(button: HTMLElement): void {
    const rect = button.getBoundingClientRect();
    this.left = Math.max(8, Math.min(rect.left, window.innerWidth - 220));
    this.bottom = Math.max(8, Math.min(window.innerHeight - rect.top + 8, window.innerHeight - 250));
  }
  choose(value: ThemePreference): void {
    this.theme.setPreference(value);
    this.panel?.nativeElement.hidePopover();
    this.trigger?.nativeElement.focus();
  }
}
