import { CommonModule } from '@angular/common';
import {
  Component,
  ChangeDetectorRef,
  HostListener,
  Input,
  OnChanges,
  OnDestroy,
  inject,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { HttpClient } from '@angular/common/http';
import { firstValueFrom } from 'rxjs';
import { AuthService, Notice, UiIcon, injectDialogs } from '@runloom/studio-platform';

interface Connection {
  id: string;
  name: string;
  provider: string;
  host: string;
  port: number;
  database: string;
  username: string;
  password?: string;
  tls: boolean;
  srv?: boolean;
  auth_source?: string;
  access: string;
  enabled: boolean;
  updated_at?: string;
}
interface Table {
  schema: string;
  name: string;
  columns: Array<{ name: string; type: string; nullable: boolean }>;
  inferred?: boolean;
}
const blank = (): Connection => ({
  id: '',
  name: '',
  provider: 'postgres',
  host: '',
  port: 5432,
  database: '',
  username: '',
  password: '',
  tls: true,
  access: 'read',
  enabled: true,
});

@Component({
  selector: 'runloom-connections',
  imports: [CommonModule, FormsModule, Notice, UiIcon],
  templateUrl: './connections.component.html',
  styleUrls: ['../../shared/styles/management-forms.scss', './connections.component.scss'],
})
export class Connections implements OnChanges, OnDestroy {
  @Input() orgId = '';
  @Input() teamId = '';
  readonly auth = inject(AuthService);
  private readonly http = inject(HttpClient);
  private readonly cd = inject(ChangeDetectorRef);
  private readonly dialogs = injectDialogs();
  private generation = 0;
  private leaving = false;
  items: Connection[] = [];
  tables: Table[] = [];
  draft = blank();
  baseline = JSON.stringify(this.draft);
  editing = false;
  formOpen = false;
  busy = false;
  testing = false;
  configured = false;
  message = '';
  failed = false;
  schemaFor = '';
  search = '';
  get dirty(): boolean {
    return this.formOpen && (JSON.stringify(this.draft) !== this.baseline || this.busy);
  }
  get filtered(): Connection[] {
    return this.items.filter((c) =>
      `${c.name} ${c.provider} ${c.host}`.toLowerCase().includes(this.search.toLowerCase()),
    );
  }
  get endpoint(): string {
    return `/api/v1/studio/orgs/${encodeURIComponent(this.orgId)}/teams/${encodeURIComponent(this.teamId)}/connections`;
  }
  ngOnChanges(): void {
    this.generation++;
    this.draft = blank();
    this.baseline = JSON.stringify(this.draft);
    this.formOpen = false;
    this.items = [];
    this.tables = [];
    this.busy = false;
    this.testing = false;
    this.message = '';
    this.configured = false;
    void this.load();
  }
  ngOnDestroy(): void {
    this.generation++;
    this.draft.password = '';
  }
  @HostListener('window:beforeunload', ['$event']) beforeUnload(e: BeforeUnloadEvent): void {
    if (this.dirty && !this.leaving) {
      e.preventDefault();
      e.returnValue = '';
    }
  }
  async confirmDiscard(): Promise<boolean> {
    if (!this.dirty) return true;
    const snapshot = JSON.stringify(this.draft),
      generation = this.generation;
    return (
      (await this.dialogs.confirm('Discard your unsaved connection settings?', {
        title: 'Discard connection changes?',
        confirmText: 'Discard',
        danger: true,
      })) &&
      snapshot === JSON.stringify(this.draft) &&
      generation === this.generation
    );
  }
  async withConfirmedExit(action: () => Promise<void>): Promise<void> {
    this.leaving = true;
    try {
      await action();
    } finally {
      this.leaving = false;
    }
  }
  async load(): Promise<void> {
    if (!this.orgId || !this.teamId) return;
    const g = this.generation;
    try {
      const r = await firstValueFrom(
        this.http.get<{ configured: boolean; items: Connection[] }>(this.endpoint),
      );
      if (g === this.generation) {
        this.items = r.items;
        this.configured = r.configured;
      }
    } catch {
      if (g === this.generation) {
        this.message = 'Could not load connections.';
        this.failed = true;
      }
    } finally {
      this.cd.markForCheck();
    }
  }
  async edit(c?: Connection): Promise<void> {
    if (this.busy || !this.auth.teamAdmin() || !(await this.confirmDiscard())) return;
    this.draft = c ? { ...c, password: '' } : blank();
    this.baseline = JSON.stringify(this.draft);
    this.editing = !!c;
    this.formOpen = true;
    this.message = '';
  }
  async close(): Promise<void> {
    if (this.busy || !(await this.confirmDiscard())) return;
    this.draft.password = '';
    this.formOpen = false;
  }
  providerChanged(): void {
    this.draft.port =
      this.draft.provider === 'mysql' ? 3306 : this.draft.provider === 'mongodb' ? 27017 : 5432;
    this.draft.srv = false;
  }
  async save(): Promise<void> {
    if (this.busy || !this.auth.teamAdmin() || !this.configured) return;
    const d = this.draft;
    if (
      !/^[a-z][a-z0-9-]{0,63}$/.test(d.id) ||
      !d.name.trim() ||
      !d.host.trim() ||
      !d.database ||
      !d.username ||
      (!this.editing && !d.password)
    ) {
      this.message =
        'Provide a lowercase connection ID, name, host, database, username and password.';
      this.failed = true;
      return;
    }
    const g = this.generation;
    const endpoint = `${this.endpoint}/${encodeURIComponent(d.id)}`;
    this.busy = true;
    this.message = '';
    const body = { ...d };
    this.draft.password = '';
    try {
      await firstValueFrom(this.http.put(endpoint, body));
      if (g !== this.generation) return;
      this.editing = true;
      this.baseline = JSON.stringify(this.draft);
      this.failed = false;
      if (body.enabled) {
        this.testing = true;
        this.message = 'Connection saved. Testing database access…';
        this.cd.markForCheck();
        try {
          await firstValueFrom(this.http.post(`${endpoint}/test`, {}));
          if (g !== this.generation) return;
          this.message = 'Connection saved and connection test passed. Query permissions are checked when you discover schemas or run database nodes.';
          this.formOpen = false;
        } catch (error) {
          if (g !== this.generation) return;
          this.failed = true;
          this.message = `Connection saved, but its test failed. ${this.errorDetail(error, 'Could not reach the connection-test API. Check your session and try Test again.')} Edit the settings below and save to test again.`;
        }
      } else {
        this.message = 'Connection saved as disabled. Enable it and save to test database access.';
        this.formOpen = false;
      }
      body.password = '';
      await this.load();
    } catch (error) {
      if (g === this.generation) {
        this.message = `${this.errorDetail(error, 'Could not save connection. Check your network connection and permissions.')} Re-enter the password if you were adding or replacing it.`;
        this.failed = true;
      }
    } finally {
      body.password = '';
      if (g === this.generation) { this.busy = false; this.testing = false; }
      this.cd.markForCheck();
    }
  }
  async test(c: Connection, discover = false): Promise<void> {
    if (this.busy || !this.auth.canEdit()) return;
    const g = this.generation;
    this.busy = true;
    this.message = '';
    this.tables = [];
    try {
      if (discover) {
        const r = await firstValueFrom(
          this.http.get<{ tables: Table[] }>(`${this.endpoint}/${encodeURIComponent(c.id)}/schema`),
        );
        if (g !== this.generation) return;
        this.tables = r.tables;
        this.schemaFor = c.name;
        this.message = `Discovered ${r.tables.length} tables or collections.`;
      } else {
        await firstValueFrom(
          this.http.post(`${this.endpoint}/${encodeURIComponent(c.id)}/test`, {}),
        );
        if (g !== this.generation) return;
        this.message = `Connected to ${c.name}.`;
      }
      this.failed = false;
    } catch (error) {
      if (g === this.generation) {
        this.message = this.errorDetail(error, 'Could not reach the connection-test API. Check your session and network connection, then try again.');
        this.failed = true;
      }
    } finally {
      if (g === this.generation) this.busy = false;
      this.cd.markForCheck();
    }
  }
  private errorDetail(error: unknown, fallback: string): string {
    const detail = (error as { error?: { detail?: unknown } })?.error?.detail;
    return typeof detail === 'string' && detail.trim() ? detail.slice(0, 2000) : fallback;
  }
  async remove(c: Connection): Promise<void> {
    const g = this.generation;
    if (
      this.busy ||
      !this.auth.teamAdmin() ||
      !(await this.dialogs.confirm(
        'Future runs using this connection will fail. Existing database data will not be deleted.',
        { title: 'Delete connection?', confirmText: 'Delete connection', danger: true },
      )) ||
      g !== this.generation
    )
      return;
    this.busy = true;
    try {
      await firstValueFrom(this.http.delete(`${this.endpoint}/${encodeURIComponent(c.id)}`));
      if (g === this.generation) {
        this.tables = [];
        this.message = 'Connection deleted. External data was not changed.';
        this.failed = false;
        await this.load();
      }
    } catch {
      if (g === this.generation) {
        this.message = 'Could not delete connection.';
        this.failed = true;
      }
    } finally {
      if (g === this.generation) this.busy = false;
      this.cd.markForCheck();
    }
  }
}
