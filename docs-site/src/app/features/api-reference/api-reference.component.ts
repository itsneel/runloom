import { Component, computed, inject, input } from '@angular/core';
import { DomSanitizer } from '@angular/platform-browser';
import { SettingsService } from '../../core/settings/settings.service';
import { IconComponent } from '../../shared/icon/icon.component';

@Component({
  selector: 'docs-api-reference',
  imports: [IconComponent],
  templateUrl: './api-reference.component.html',
  styleUrl: './api-reference.component.scss',
})
export class ApiReferenceComponent {
  readonly api = input.required<'consumer' | 'studio' | 'platform'>();
  readonly settings = inject(SettingsService);
  private readonly sanitizer = inject(DomSanitizer);
  // Only a closed, local resource allowlist is trusted; never a user-supplied URL.
  readonly source = computed(() =>
    this.sanitizer.bypassSecurityTrustResourceUrl(
      '/api-reference/index.html?api=' +
        (['consumer', 'studio', 'platform'].includes(this.api()) ? this.api() : 'consumer'),
    ),
  );
}
