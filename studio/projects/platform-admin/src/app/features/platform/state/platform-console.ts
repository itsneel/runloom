import { signal } from '@angular/core';
import {
  Health,
  OrganizationMetadata,
  PlatformApi,
  PlatformEvent,
  StaffMember,
} from '../services/platform-api';

export class PlatformConsoleState {
  readonly busy = signal(false);
  readonly error = signal('');
  readonly notice = signal('');
  readonly organizations = signal<OrganizationMetadata[]>([]);
  readonly staff = signal<StaffMember[]>([]);
  readonly events = signal<PlatformEvent[]>([]);
  readonly health = signal<Health | undefined>(undefined);
  readonly total = signal(0);
  readonly offset = signal(0);
  readonly organizationSettings = signal<{ max_children: number; maximum: number } | undefined>(
    undefined,
  );
  private generation = 0;
  constructor(private readonly api: PlatformApi) {}
  clear(): void {
    this.generation++;
    this.error.set('');
    this.notice.set('');
    this.organizations.set([]);
    this.staff.set([]);
    this.events.set([]);
    this.health.set(undefined);
    this.total.set(0);
    this.busy.set(false);
    this.organizationSettings.set(undefined);
  }
  async load(section: string, offset = 0, configureOrganizations = false): Promise<void> {
    const generation = ++this.generation;
    this.busy.set(true);
    this.error.set('');
    this.offset.set(offset);
    try {
      if (section === 'health') {
        const data = await this.api.health();
        if (generation === this.generation) this.health.set(data);
      } else {
        const data = await this.api.list<OrganizationMetadata | StaffMember | PlatformEvent>(
          section,
          offset,
        );
        if (generation !== this.generation) return;
        this.total.set(data.total);
        if (section === 'organizations')
          this.organizations.set(data.items as OrganizationMetadata[]);
        if (section === 'staff') this.staff.set(data.items as StaffMember[]);
        if (section === 'audit') this.events.set(data.items as PlatformEvent[]);
        if (section === 'organizations' && configureOrganizations) {
          const settings = await this.api.organizationSettings();
          if (generation === this.generation) this.organizationSettings.set(settings);
        }
      }
    } catch (error) {
      if (generation !== this.generation) return;
      this.organizations.set([]);
      this.staff.set([]);
      this.events.set([]);
      this.health.set(undefined);
      this.error.set((error as Error).message || 'Unable to load platform information.');
    } finally {
      if (generation === this.generation) this.busy.set(false);
    }
  }
}
