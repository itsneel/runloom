import { injectDialogs } from '@runloom/studio-platform';
import { CommonModule } from '@angular/common';
import { HttpClient } from '@angular/common/http';
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
import { firstValueFrom } from 'rxjs';
import { AuthService } from '@runloom/studio-platform';
import { Notice } from '@runloom/studio-platform';

interface Grant {
  definition_id: string;
  versions: number[];
  input_fields: string[];
  output_fields: string[];
  versions_text?: string;
  input_text?: string;
  output_text?: string;
}
interface Policy {
  name: string;
  scopes: string[];
  grants: Grant[];
  requests_per_minute: number;
  max_concurrent_runs: number;
}
interface Account extends Policy {
  client_id: string;
  status: string;
  created_at: string;
}
interface Credentials {
  client_id: string;
  client_secret: string;
}

@Component({
  selector: 'runloom-service-accounts',
  imports: [CommonModule, FormsModule, Notice],
  templateUrl: './service-accounts.component.html',
  styleUrls: ['../../shared/styles/management-forms.scss', './service-accounts.component.scss'],
  
})
export class ServiceAccounts implements OnChanges, OnDestroy {
  @Input({ required: true }) teamId = '';
  @Input({ required: true }) orgId = '';
  private readonly dialogs = injectDialogs();
  private readonly auth = inject(AuthService);
  private readonly http = inject(HttpClient);
  private readonly cd = inject(ChangeDetectorRef);
  private generation = 0;
  accounts: Account[] = [];
  definitions: { id: string; name: string; latest_version: number; published_kind: string }[] = [];
  scopes = ['workflows:read', 'runs:start', 'runs:read', 'runs:cancel'];
  policy: Policy = this.emptyPolicy();
  editing = '';
  credentials?: Credentials;
  busy = false;
  error = '';
  notice = '';

  private emptyPolicy(): Policy {
    return {
      name: '',
      scopes: ['workflows:read', 'runs:start', 'runs:read'],
      grants: [],
      requests_per_minute: 60,
      max_concurrent_runs: 5,
    };
  }
  private path(): string {
    return `/api/v1/studio/orgs/${encodeURIComponent(this.orgId)}/teams/${encodeURIComponent(this.teamId)}/service-accounts`;
  }
  ngOnChanges(): void {
    this.dialogs.cancel();
    this.credentials = undefined;
    this.editing = '';
    this.policy = this.emptyPolicy();
    void this.load();
  }
  ngOnDestroy(): void {
    this.generation++;
    this.credentials = undefined;
  }
  private confirmedExit?: Credentials;
  async confirmDiscard(): Promise<boolean> {
    const credentials = this.credentials, generation = this.generation;
    if (!credentials) return true;
    const accepted = await this.dialogs.confirm(
      'This client secret is visible only once. Save it somewhere secure before leaving; it cannot be retrieved later.',
      { title: 'Leave one-time credentials?', confirmText: 'Leave anyway', cancelText: 'Stay and copy', danger: true },
    );
    return accepted && generation === this.generation && credentials === this.credentials;
  }
  async withConfirmedExit(work: () => Promise<void>): Promise<void> {
    this.confirmedExit = this.credentials;
    try { await work(); }
    finally { this.confirmedExit = undefined; }
  }
  @HostListener('window:beforeunload', ['$event']) warnSecret(event: BeforeUnloadEvent): void {
    const confirmed = this.confirmedExit;
    this.confirmedExit = undefined;
    if (this.credentials && this.credentials !== confirmed) {
      event.preventDefault();
      event.returnValue = '';
    }
  }
  async load(): Promise<void> {
    const generation = ++this.generation;
    this.busy = true;
    this.error = '';
    this.accounts = [];
    this.definitions = [];
    try {
      const [accounts, definitions] = await Promise.all([
        this.auth.request<Account[]>(this.path()),
        firstValueFrom(
          this.http.get<typeof this.definitions>('/api/v1/studio/definitions', {
            headers: { 'X-Runloom-Team': this.teamId },
          }),
        ),
      ]);
      if (generation !== this.generation) return;
      this.accounts = accounts;
      this.definitions = definitions.filter(
        (d) => d.latest_version > 0 && d.published_kind !== 'child',
      );
    } catch (error) {
      if (generation === this.generation) this.error = this.message(error);
    } finally {
      if (generation === this.generation) {
        this.busy = false;
        this.cd.markForCheck();
      }
    }
  }
  toggleScope(scope: string, checked: boolean): void {
    if (this.editing) return;
    this.policy.scopes = checked
      ? [...this.policy.scopes, scope]
      : this.policy.scopes.filter((s) => s !== scope);
  }
  addGrant(): void {
    this.policy.grants.push({
      definition_id: '',
      versions: [1],
      input_fields: [],
      output_fields: [],
    });
  }
  selectDefinition(grant: Grant, id: string): void {
    grant.definition_id = id;
    grant.versions = [this.definitions.find((d) => d.id === id)?.latest_version || 1];
    delete grant.versions_text;
  }
  edit(account: Account): void {
    if (this.credentials) return;
    this.editing = account.client_id;
    const { name, scopes, grants, requests_per_minute, max_concurrent_runs } = account;
    this.policy = structuredClone({
      name,
      scopes,
      grants,
      requests_per_minute,
      max_concurrent_runs,
    });
  }
  reset(): void {
    this.editing = '';
    this.policy = this.emptyPolicy();
  }
  async save(): Promise<void> {
    const split = (value: string) =>
      value
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean);
    const body = {
      ...this.policy,
      grants: this.policy.grants.map((g) => ({
        definition_id: g.definition_id,
        versions: g.versions_text === undefined ? g.versions : split(g.versions_text).map(Number),
        input_fields: g.input_text === undefined ? g.input_fields : split(g.input_text),
        output_fields: g.output_text === undefined ? g.output_fields : split(g.output_text),
      })),
    };
    await this.mutate(this.editing ? `/${this.editing}` : '', body, this.editing ? 'PUT' : 'POST');
  }
  async rotate(account: Account): Promise<void> {
    if (this.busy || this.credentials) return;
    const generation = this.generation;
    if (
      !(await this.dialogs.confirm(
        'Rotate this secret? Existing credentials and access tokens stop working in Runloom.',
        { title: 'Rotate client secret?', confirmText: 'Rotate secret', danger: true },
      ))
    )
      return;
    if (generation !== this.generation) return;
    await this.mutate(`/${account.client_id}/rotate`, {});
  }
  async revoke(account: Account): Promise<void> {
    if (this.busy || this.credentials) return;
    const generation = this.generation;
    if (
      !(await this.dialogs.confirm(
        'Revoke this service account? New API requests will be blocked; existing workflows continue.',
        { title: 'Revoke service account?', confirmText: 'Revoke account', danger: true },
      ))
    )
      return;
    if (generation !== this.generation) return;
    await this.mutate(`/${account.client_id}/revoke`, {});
  }
  dismissSecret(): void {
    this.credentials = undefined;
  }
  private async mutate(suffix: string, body: unknown, method = 'POST'): Promise<void> {
    if (this.busy || this.credentials) return;
    const generation = this.generation,
      path = this.path();
    this.busy = true;
    this.error = '';
    this.notice = '';
    try {
      const result = await this.auth.request<Credentials & { provider_disabled?: boolean }>(
        path + suffix,
        body,
        method,
      );
      if (generation !== this.generation) return;
      this.reset();
      await this.load();
      if (path !== this.path() || generation + 1 !== this.generation) return;
      if (result.client_secret)
        this.credentials = { client_id: result.client_id, client_secret: result.client_secret };
      this.notice =
        result.provider_disabled === false
          ? 'Revoked in Runloom. Keycloak disable failed; retry revoke to finish provider cleanup.'
          : 'Service account updated.';
    } catch (error) {
      if (generation === this.generation) this.error = this.message(error);
    } finally {
      if (path === this.path()) {
        this.busy = false;
        this.cd.markForCheck();
      }
    }
  }
  private message(error: unknown): string {
    return (error as { message?: string })?.message || 'Service account request failed.';
  }
}
