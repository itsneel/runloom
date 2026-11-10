import { Component, ElementRef, inject, output, signal, viewChild } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import type MiniSearch from 'minisearch';
import { IconComponent } from '../../shared/icon/icon.component';

interface Result {
  slug: string;
  title: string;
  description: string;
  group: string;
}
@Component({
  selector: 'docs-search',
  imports: [RouterLink, IconComponent],
  templateUrl: './search.component.html',
  styleUrl: './search.component.scss',
})
export class SearchComponent {
  readonly dismissed = output<void>();
  private readonly router = inject(Router);
  readonly dialog = viewChild.required<ElementRef<HTMLDialogElement>>('dialog');
  readonly input = viewChild.required<ElementRef<HTMLInputElement>>('input');
  readonly query = signal('');
  readonly results = signal<Result[]>([]);
  readonly loading = signal(false);
  readonly error = signal(false);
  readonly selected = signal(0);
  private index?: MiniSearch<Result>;

  async open(): Promise<void> {
    if (!this.dialog().nativeElement.open) this.dialog().nativeElement.showModal();
    this.input().nativeElement.focus();
    if (this.index || this.loading()) return;
    this.loading.set(true);
    this.error.set(false);
    try {
      const [{ default: MiniSearch }, response] = await Promise.all([
        import('minisearch'),
        fetch('/generated/search.json'),
      ]);
      if (!response.ok) throw new Error('Search unavailable');
      this.index = MiniSearch.loadJSON<Result>(await response.text(), {
        fields: ['title', 'description', 'text'],
        storeFields: ['slug', 'title', 'description', 'group'],
        searchOptions: { prefix: true, fuzzy: 0.2, boost: { title: 4, description: 2 } },
      });
      this.update(this.query());
    } catch {
      this.error.set(true);
    } finally {
      this.loading.set(false);
    }
  }
  close(): void {
    this.dialog().nativeElement.close();
  }
  update(value: string): void {
    this.query.set(value);
    this.selected.set(0);
    this.results.set(
      (this.index?.search(value).slice(0, 8) || []).map((result) => ({
        slug: result['slug'],
        title: result['title'],
        description: result['description'],
        group: result['group'],
      })),
    );
  }
  navigate(event: KeyboardEvent): void {
    const results = this.results();
    if (!results.length) return;
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      this.selected.update(
        (index) => (index + (event.key === 'ArrowDown' ? 1 : results.length - 1)) % results.length,
      );
      this.dialog()
        .nativeElement.querySelector(`#result-${this.selected()}`)
        ?.scrollIntoView({ block: 'nearest' });
    } else if (event.key === 'Enter') {
      event.preventDefault();
      void this.router.navigateByUrl('/' + results[this.selected()].slug);
      this.close();
    }
  }
}
