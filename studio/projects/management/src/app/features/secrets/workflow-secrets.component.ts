import { injectDialogs } from '@runloom/studio-platform';
import { CommonModule } from '@angular/common';
import { HttpClient, HttpErrorResponse } from '@angular/common/http';
import {
  ChangeDetectorRef,
  Component,
  HostListener,
  Input,
  OnChanges,
  OnDestroy,
  inject,
} from '@angular/core';
import { FormsModule, NgForm } from '@angular/forms';
import { firstValueFrom } from 'rxjs';
import { AuthService } from '@runloom/studio-platform';
import { Notice, UiIcon } from '@runloom/studio-platform';
import { secretNameError, secretValueError } from './secret-validation';

@Component({
  selector: 'runloom-workflow-secrets',
  imports: [CommonModule, FormsModule, Notice, UiIcon],
  styleUrls: ['../../shared/styles/management-forms.scss', './workflow-secrets.component.scss'],
  templateUrl: './workflow-secrets.component.html',
})
export class WorkflowSecrets implements OnChanges, OnDestroy {
  @Input() teamId = '';
  @Input() orgId = '';
  private readonly dialogs = injectDialogs();
  readonly auth = inject(AuthService);
  private readonly http = inject(HttpClient);
  private readonly cd = inject(ChangeDetectorRef);
  private generation = 0;
  private confirmedExit?: () => boolean;
  name = '';
  value = '';
  message = '';
  configured = false;
  busy = false;
  loaded = false;
  messageError = false;
  items: Array<{ name: string; updated_at: string }> = [];
  get nameError(): string {
    return secretNameError(this.name);
  }
  get valueError(): string {
    return secretValueError(this.value);
  }
  get dirty(): boolean {
    return !!(this.name || this.value) || this.busy;
  }
  private unchanged(): () => boolean {
    const { generation, name, value, busy } = this;
    return () =>
      generation === this.generation &&
      name === this.name &&
      value === this.value &&
      busy === this.busy;
  }
  async confirmDiscard(): Promise<boolean> {
    if (!this.dirty) return true;
    const unchanged = this.unchanged();
    return (
      (await this.dialogs.confirm('Discard unsaved workflow secret changes?', {
        title: 'Discard secret changes?',
        confirmText: 'Discard changes',
        cancelText: 'Keep editing',
        danger: true,
      })) && unchanged()
    );
  }
  async withConfirmedExit(work: () => Promise<void>): Promise<void> {
    this.confirmedExit = this.unchanged();
    try {
      await work();
    } finally {
      this.confirmedExit = undefined;
    }
  }
  @HostListener('window:beforeunload', ['$event']) beforeUnload(event: BeforeUnloadEvent): void {
    const confirmed = this.confirmedExit;
    this.confirmedExit = undefined;
    if (confirmed?.()) return;
    if (this.dirty) {
      event.preventDefault();
      event.returnValue = '';
    }
  }
  ngOnChanges(): void {
    this.dialogs.cancel();
    this.generation++;
    this.value = '';
    this.name = '';
    this.items = [];
    this.message = '';
    this.busy = false;
    this.configured = false;
    this.loaded = false;
    void this.load();
  }
  ngOnDestroy(): void {
    this.generation++;
    this.value = '';
  }
  private get endpoint(): string {
    return `/api/v1/studio/orgs/${encodeURIComponent(this.orgId)}/teams/${encodeURIComponent(this.teamId)}/secrets`;
  }
  private async load(): Promise<void> {
    if (!this.orgId || !this.teamId) return;
    const generation = this.generation;
    try {
      const result = await firstValueFrom(
        this.http.get<{ configured: boolean; items: Array<{ name: string; updated_at: string }> }>(
          this.endpoint,
        ),
      );
      if (generation !== this.generation) return;
      this.items = Array.isArray(result.items) ? result.items : [];
      this.configured = result.configured === true;
      this.loaded = true;
    } catch {
      if (generation === this.generation) {
        this.messageError = true;
        this.message = 'Unable to load workflow secrets.';
      }
    } finally {
      this.cd.markForCheck();
    }
  }
  async save(form?: NgForm): Promise<void> {
    if (this.busy || !this.configured || !this.auth.teamAdmin()) return;
    if (this.nameError || this.valueError) {
      this.messageError = true;
      this.message = this.nameError || this.valueError;
      return;
    }
    const generation = this.generation;
    if (
      this.items.some((s) => s.name === this.name) &&
      !(await this.dialogs.confirm(
        'Replace this secret? Future activity attempts will use the new value.',
        { title: 'Replace workflow secret?', confirmText: 'Replace secret', danger: true },
      ))
    )
      return;
    if (generation !== this.generation || this.busy) return;
    this.busy = true;
    const request = { value: this.value };
    this.value = '';
    try {
      await firstValueFrom(
        this.http.put(`${this.endpoint}/${encodeURIComponent(this.name)}`, request),
      );
      if (generation !== this.generation) return;
      this.name = '';
      form?.resetForm({ 'secret-name': '', 'secret-value': '' });
      this.messageError = false;
      this.message = 'Secret saved. Its value cannot be retrieved.';
      await this.load();
    } catch (error) {
      if (generation === this.generation) {
        this.messageError = true;
        this.message = this.saveError(error);
      }
    } finally {
      request.value = '';
      if (generation === this.generation) this.busy = false;
      this.cd.markForCheck();
    }
  }
  private saveError(error: unknown): string {
    // Never echo a server/proxy response body: it may contain the submitted credential.
    const status = error instanceof HttpErrorResponse ? error.status : 0;
    const reason =
      status === 503
        ? 'Secret storage is not configured. An operator must mount the same encryption key on the API and worker.'
        : status === 403
          ? 'Only team administrators and organization owners/admins can save secrets.'
          : status === 401
            ? 'Your session has expired. Sign in again.'
            : status === 400
              ? 'The request was rejected. Check the secret name and value; the secret directory may also be full.'
              : status === 0
                ? 'Could not reach the server. Check your connection.'
                : 'The server could not save the secret. Try again or contact your operator.';
    return `${reason} Re-enter the value to retry.`;
  }
  async remove(name: string): Promise<void> {
    if (this.busy) return;
    const generation = this.generation;
    if (
      !(await this.dialogs.confirm('Delete this secret? Future calls using it will fail.', {
        title: 'Delete workflow secret?',
        confirmText: 'Delete secret',
        danger: true,
      }))
    )
      return;
    if (generation !== this.generation || this.busy) return;
    this.busy = true;
    try {
      await firstValueFrom(this.http.delete(`${this.endpoint}/${encodeURIComponent(name)}`));
      if (generation === this.generation) await this.load();
    } catch {
      if (generation === this.generation) {
        this.messageError = true;
        this.message = 'Unable to delete secret.';
      }
    } finally {
      if (generation === this.generation) this.busy = false;
      this.cd.markForCheck();
    }
  }
}
