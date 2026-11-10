import { CommonModule } from '@angular/common';
import {
  ChangeDetectorRef,
  Component,
  HostListener,
  Input,
  OnChanges,
  OnDestroy,
  inject,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { AuthService, Notice, WorkspaceBridge, injectDialogs } from '@runloom/studio-platform';
import { OrganizationWorkspace } from './state/organization-workspace';

@Component({
  selector: 'runloom-organizations',
  imports: [CommonModule, FormsModule, Notice],
  templateUrl: './organizations.component.html',
  styleUrls: ['../../shared/styles/dashboard.scss', './organizations.component.scss'],
})
export class Organizations implements OnChanges, OnDestroy {
  @Input() orgId = '';
  readonly auth = inject(AuthService);
  private readonly bridge = inject(WorkspaceBridge);
  private readonly cd = inject(ChangeDetectorRef);
  private readonly dialogs = injectDialogs();
  readonly state = new OrganizationWorkspace({
    organization: () => this.auth.organization(),
    request: (path, body, method) => this.auth.request(path, body, method),
    refreshProfile: () => this.auth.refresh(),
    selectOrganization: async (id) => {
      this.auth.selectOrganization(id);
      await this.bridge.host?.openManagement('organization');
    },
    confirm: (message) =>
      this.dialogs.confirm(message, {
        title: 'Unsaved organization changes',
        confirmText: 'Discard changes',
        cancelText: 'Keep editing',
        danger: true,
      }),
    markForCheck: () => this.cd.markForCheck(),
  });
  ngOnChanges(): void {
    void this.state.load();
  }
  ngOnDestroy(): void {
    this.state.destroy();
  }
  confirmDiscard(): Promise<boolean> {
    return this.state.confirmDiscard();
  }
  async withConfirmedExit(action: () => Promise<void>): Promise<void> {
    this.state.confirmedExit = true;
    try {
      await action();
    } finally {
      this.state.confirmedExit = false;
    }
  }
  @HostListener('window:beforeunload', ['$event']) beforeUnload(event: BeforeUnloadEvent): void {
    if (this.state.dirty && !this.state.confirmedExit) {
      event.preventDefault();
      event.returnValue = '';
    }
  }
}
