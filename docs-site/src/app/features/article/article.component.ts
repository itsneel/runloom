import { afterNextRender, Component, computed, DestroyRef, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { ActivatedRoute, Router, RouterLink } from '@angular/router';
import { DomSanitizer, Meta } from '@angular/platform-browser';
import { Article, PageMeta } from '../../core/content/content.models';
import { pages } from '../../core/content/generated/navigation';
import { ApiReferenceComponent } from '../api-reference/api-reference.component';

@Component({
  selector: 'docs-article',
  imports: [RouterLink, ApiReferenceComponent],
  templateUrl: './article.component.html',
  styleUrl: './article.component.scss',
})
export class ArticleComponent {
  private readonly router = inject(Router);
  private readonly route = inject(ActivatedRoute);
  private readonly meta = inject(Meta);
  readonly page = signal<PageMeta>(inject(ActivatedRoute).snapshot.data['page']);
  readonly article = signal<Article>(inject(ActivatedRoute).snapshot.data['article']);
  private readonly sanitizer = inject(DomSanitizer);
  // Only build-generated HTML, sanitized by scripts/markdown.mjs, enters this binding.
  // Angular's generic sanitizer otherwise strips the generated heading IDs and buttons.
  // Never use this binding with live API responses, URL inputs or unsanitized Markdown.
  readonly rendered = computed(() => this.sanitizer.bypassSecurityTrustHtml(this.article().html));
  readonly renderedReference = computed(() =>
    this.sanitizer.bypassSecurityTrustHtml(this.article().reference?.html || ''),
  );
  readonly referenceOpen = signal(false);
  readonly headings = computed(() => [
    ...this.article().headings,
    ...(this.referenceOpen() ? this.article().reference?.headings || [] : []),
  ]);
  readonly copied = signal('');
  readonly activeHeading = signal('');
  readonly browser = signal(false);
  private observer?: IntersectionObserver;
  private frame?: number;
  private copyTimer?: ReturnType<typeof setTimeout>;
  get previous(): PageMeta | undefined {
    return pages[pages.findIndex((p) => p.slug === this.page().slug) - 1];
  }
  get next(): PageMeta | undefined {
    return pages[pages.findIndex((p) => p.slug === this.page().slug) + 1];
  }

  constructor() {
    const destroy = inject(DestroyRef);
    inject(ActivatedRoute)
      .data.pipe(takeUntilDestroyed(destroy))
      .subscribe((data) => {
        this.page.set(data['page']);
        this.article.set(data['article']);
        this.referenceOpen.set(
          !!this.article().reference?.headings.some((h) => h.id === this.route.snapshot.fragment),
        );
        this.copied.set('');
        this.meta.updateTag({ name: 'description', content: this.page().description });
        if (this.browser()) {
          if (this.frame) cancelAnimationFrame(this.frame);
          this.frame = requestAnimationFrame(() => this.observeHeadings());
        }
      });
    afterNextRender(() => {
      this.browser.set(true);
      this.route.fragment.pipe(takeUntilDestroyed(destroy)).subscribe((fragment) => {
        if (fragment && this.article().reference?.headings.some((h) => h.id === fragment)) {
          this.referenceOpen.set(true);
          if (this.frame) cancelAnimationFrame(this.frame);
          this.frame = requestAnimationFrame(() => {
            this.observeHeadings();
            document.getElementById(fragment)?.scrollIntoView();
          });
        }
      });
      this.observeHeadings();
    });
    destroy.onDestroy(() => {
      this.observer?.disconnect();
      clearTimeout(this.copyTimer);
      if (this.frame) cancelAnimationFrame(this.frame);
    });
  }
  toggleReference(event: Event): void {
    this.referenceOpen.set((event.target as HTMLDetailsElement).open);
    if (this.browser()) this.observeHeadings();
  }
  private observeHeadings(): void {
    this.observer?.disconnect();
    this.activeHeading.set(this.article().headings[0]?.id || '');
    this.observer = new IntersectionObserver(
      (entries) => {
        const visible = entries.find((entry) => entry.isIntersecting);
        if (visible) this.activeHeading.set(visible.target.id);
      },
      { rootMargin: '-100px 0px -60% 0px' },
    );
    document
      .querySelectorAll('.prose h2, .prose h3')
      .forEach((heading) => this.observer?.observe(heading));
  }
  async interact(event: MouseEvent): Promise<void> {
    const target = event.target as HTMLElement;
    const button = target.closest<HTMLButtonElement>('button[data-copy]');
    if (button) {
      try {
        await navigator.clipboard.writeText(
          button.closest('.code-block')?.querySelector('code')?.textContent || '',
        );
        button.textContent = 'Copied';
        this.copied.set('Code copied to clipboard.');
        clearTimeout(this.copyTimer);
        this.copyTimer = setTimeout(() => {
          button.textContent = 'Copy';
          this.copied.set('');
        }, 2500);
      } catch {
        this.copied.set('Clipboard unavailable. Select the code and copy it manually.');
      }
      return;
    }
    const anchor = target.closest<HTMLAnchorElement>('a');
    const href = anchor?.getAttribute('href');
    if (
      !href ||
      event.ctrlKey ||
      event.metaKey ||
      event.shiftKey ||
      event.altKey ||
      event.button !== 0
    )
      return;
    if (href.startsWith('/') && !href.startsWith('/generated/')) {
      event.preventDefault();
      await this.router.navigateByUrl(href);
    }
  }
}
