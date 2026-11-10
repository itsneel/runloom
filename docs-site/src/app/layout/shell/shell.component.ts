import {
  Component,
  DestroyRef,
  ElementRef,
  HostListener,
  inject,
  signal,
  viewChild,
} from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { NavigationEnd, Router, RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { filter } from 'rxjs';
import { pages } from '../../core/content/generated/navigation';
import { SettingsService } from '../../core/settings/settings.service';
import { SearchComponent } from '../../features/search/search.component';
import { IconComponent } from '../../shared/icon/icon.component';

@Component({
  selector: 'docs-root',
  imports: [RouterOutlet, RouterLink, RouterLinkActive, SearchComponent, IconComponent],
  templateUrl: './shell.component.html',
  styleUrl: './shell.component.scss',
})
export class ShellComponent {
  readonly settings = inject(SettingsService);
  readonly search = viewChild.required(SearchComponent);
  readonly connection = viewChild.required<ElementRef<HTMLDialogElement>>('connection');
  readonly main = viewChild.required<ElementRef<HTMLElement>>('main');
  readonly mobileMenu = signal(false);
  readonly destination = signal('');
  readonly connectionError = signal(false);
  readonly groups = [...new Set(pages.map((page) => page.group))].map((name) => ({
    name,
    pages: pages.filter((page) => page.group === name),
  }));
  constructor() {
    inject(Router)
      .events.pipe(
        filter((event) => event instanceof NavigationEnd),
        takeUntilDestroyed(inject(DestroyRef)),
      )
      .subscribe(() => {
        this.mobileMenu.set(false);
        // Focus the new article after route rendering without changing the scroll position.
        if (typeof window !== 'undefined')
          requestAnimationFrame(() => this.main().nativeElement.focus({ preventScroll: true }));
      });
  }
  @HostListener('document:keydown', ['$event']) shortcut(event: KeyboardEvent): void {
    if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
      event.preventDefault();
      void this.search().open();
    }
    if (event.key === 'Escape') this.mobileMenu.set(false);
  }
  configureStudio(): void {
    this.destination.set(this.settings.studioUrl());
    this.connectionError.set(false);
    this.connection().nativeElement.showModal();
  }
  saveDestination(): void {
    this.connectionError.set(!this.settings.setStudio(this.destination()));
    if (!this.connectionError()) this.connection().nativeElement.close();
  }
}
