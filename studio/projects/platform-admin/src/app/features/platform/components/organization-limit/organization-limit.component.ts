import { CommonModule } from '@angular/common';
import {
  ChangeDetectorRef,
  Component,
  EventEmitter,
  Input,
  OnChanges,
  OnDestroy,
  SimpleChanges,
  Output,
  inject,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { OrganizationMetadata, PlatformApi } from '../../services/platform-api';
import { OrganizationLimitEditor } from '../../state/organization-limit-editor';

@Component({
  selector: 'runloom-organization-limit',
  imports: [CommonModule, FormsModule],
  templateUrl: './organization-limit.component.html',
  styleUrl: './organization-limit.component.scss',
})
export class OrganizationLimit implements OnChanges, OnDestroy {
  @Input({ required: true }) organization!: OrganizationMetadata;
  @Input() canConfigure = false;
  @Input() disabled = false;
  @Output() saved = new EventEmitter<void>();
  private readonly api = inject(PlatformApi);
  private readonly cd = inject(ChangeDetectorRef);
  readonly state = new OrganizationLimitEditor({
    canEdit: () => this.canConfigure && !this.disabled,
    save: (id, limit) => this.api.organizationLimit(id, limit),
    saved: () => this.saved.emit(),
  });
  ngOnChanges(changes: SimpleChanges): void {
    if (changes['organization']) this.state.load(this.organization);
  }
  ngOnDestroy(): void {
    this.state.destroy();
  }
  async save(): Promise<void> {
    await this.state.save();
    this.cd.markForCheck();
  }
}
