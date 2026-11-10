import { CommonModule } from '@angular/common';
import { Component, DestroyRef, ElementRef, ViewChild, inject, Input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { AuthService, Notice } from '@runloom/studio-platform';
import { PlatformAccess } from '../../core/api/platform-access.service';
import { PlatformApi } from './services/platform-api';
import { PlatformConsoleState } from './state/platform-console';
import { OrganizationLimit } from './components/organization-limit/organization-limit.component';
import { PlatformNavigation } from './components/platform-navigation/platform-navigation.component';

@Component({
  selector: 'runloom-platform-console',
  imports: [CommonModule, FormsModule, Notice, OrganizationLimit, PlatformNavigation],
  templateUrl: './platform-console.component.html',
  styleUrl: './platform-console.component.scss',
})
export class PlatformConsole {
  readonly access = inject(PlatformAccess);
  readonly auth = inject(AuthService);
  private readonly api = inject(PlatformApi);
  readonly state = new PlatformConsoleState(this.api);
  readonly section = signal('health');
  @ViewChild('content') content?: ElementRef<HTMLElement>;
  navigationOpen = false;
  get pageTitle(): string {
    return (
      this.sections.find((section) => section.id === this.section())?.label || 'Platform console'
    );
  }
  @Input() set platformSection(value: string) {
    this.section.set(value || 'health');
    this.state.clear();
    this.content?.nativeElement.scrollTo(0, 0);
    void this.refresh();
  }
  readonly loadingAccess = signal(true);
  readonly saving = signal(false);
  email = '';
  role = 'platform_staff';
  reason = '';
  maxChildren = 2;
  private generation = 0;
  readonly sections = [
    { id: 'health', label: 'System health', permission: 'platform.health.read', icon: 'monitor' },
    {
      id: 'organizations',
      label: 'Organizations',
      permission: 'platform.organizations.read',
      icon: 'building',
    },
    { id: 'staff', label: 'Staff & access', permission: 'platform.staff.manage', icon: 'team' },
    { id: 'audit', label: 'Platform audit', permission: 'platform.audit.read', icon: 'shield' },
  ];
  constructor() {
    const timer = setInterval(() => {
      if (this.section() === 'health' && !document.hidden && !this.state.busy())
        void this.refresh();
    }, 30000);
    inject(DestroyRef).onDestroy(() => {
      this.generation++;
      this.state.clear();
      clearInterval(timer);
    });
  }
  allowed(): boolean {
    return this.sections.some((s) => s.id === this.section() && this.access.can(s.permission));
  }
  async refresh(offset = 0): Promise<void> {
    const generation = ++this.generation;
    try {
      await this.access.refresh();
      if (generation !== this.generation) return;
      if (this.allowed()) {
        await this.state.load(
          this.section(),
          offset,
          this.access.can('platform.organizations.configure'),
        );
        if (generation === this.generation && this.state.organizationSettings())
          this.maxChildren = this.state.organizationSettings()!.max_children;
      } else this.state.clear();
    } catch (error) {
      if (generation === this.generation) {
        this.state.clear();
        this.state.error.set((error as Error).message);
      }
    } finally {
      if (generation === this.generation) this.loadingAccess.set(false);
    }
  }
  async save(): Promise<void> {
    if (this.saving()) return;
    this.saving.set(true);
    this.state.error.set('');
    this.state.notice.set('');
    try {
      await this.api.assign(this.email.trim(), this.role, this.reason.trim());
      this.email = '';
      this.reason = '';
      this.state.notice.set('Platform access updated. Organization permissions were not changed.');
      await this.refresh();
    } catch (error) {
      this.state.error.set((error as Error).message);
    } finally {
      this.saving.set(false);
    }
  }
  async saveOrganizationSettings(): Promise<void> {
    if (this.saving() || !this.access.can('platform.organizations.configure')) return;
    this.saving.set(true);
    this.state.error.set('');
    this.state.notice.set('');
    try {
      if (!Number.isInteger(this.maxChildren) || this.maxChildren < 0 || this.maxChildren > 10)
        throw new Error('Choose a whole number from 0 to 10 children per parent.');
      const settings = await this.api.organizationSettings(this.maxChildren);
      this.state.organizationSettings.set(settings);
      this.state.notice.set('Platform default saved. Organization overrides are preserved.');
      await this.refresh(this.state.offset());
    } catch (error) {
      this.state.error.set((error as Error).message);
    } finally {
      this.saving.set(false);
    }
  }
  async organizationLimitSaved(): Promise<void> {
    this.state.notice.set('Organization child limit saved. Existing children are preserved.');
    await this.refresh(this.state.offset());
  }
}
