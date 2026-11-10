import { CommonModule } from '@angular/common';
import {
  ChangeDetectorRef,
  Component,
  HostListener,
  Input,
  OnChanges,
  OnDestroy,
  Output,
  EventEmitter,
  inject,
  afterNextRender,
  Injector,
  ViewChild,
  ElementRef,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { AuthService, Notice, injectDialogs } from '@runloom/studio-platform';
import { DashboardApi } from '../../core/api/dashboard-api.service';
import { errorMessage, sorted } from '../../shared/utils/dashboard-data';
import { scheduleConfig } from './utils/schedule-config';
import type { ScheduledDefinition, ScheduleDraft, WorkflowSchedule } from './models/schedule.models';

@Component({
  selector: 'runloom-schedules',
  imports: [CommonModule, FormsModule, Notice],
  templateUrl: './schedules.component.html',
  styleUrls: ['../../shared/styles/dashboard.scss', './schedules.component.scss'],
})
export class Schedules implements OnChanges, OnDestroy {
  @Input() teamId = '';
  @Output() viewRun = new EventEmitter<string>();
  @ViewChild('editorPanel') private editorPanel?: ElementRef<HTMLElement>;
  readonly auth = inject(AuthService);
  private readonly api = inject(DashboardApi);
  private readonly dialogs = injectDialogs();
  private readonly cd = inject(ChangeDetectorRef);
  private readonly injector = inject(Injector);
  private generation = 0;
  private baseline = '';
  private confirmedExit = '';
  entries: WorkflowSchedule[] = [];
  definitions: ScheduledDefinition[] = [];
  draft?: ScheduleDraft;
  busy = false;
  loading = false;
  error = '';
  notice = '';
  search = '';
  status = '';
  page = 0;
  ascending = true;
  get snapshot(): string {
    return JSON.stringify(this.draft) || '';
  }
  get dirty(): boolean {
    return this.busy || (!!this.draft && this.snapshot !== this.baseline);
  }
  get activeCount(): number {
    return this.entries.filter((s) => !s.paused).length;
  }
  get filtered(): WorkflowSchedule[] {
    const q = this.search.trim().toLowerCase();
    return sorted(
      this.entries.filter(
        (s) =>
          (!q || [s.name, s.definition_id, s.id].some((v) => v.toLowerCase().includes(q))) &&
          (!this.status || s.paused === (this.status === 'paused')),
      ),
      (s) => s.name,
      this.ascending,
    );
  }
  get pages(): number {
    return Math.max(1, Math.ceil(this.filtered.length / 15));
  }
  get rows(): WorkflowSchedule[] {
    return this.filtered.slice(this.page * 15, this.page * 15 + 15);
  }
  ngOnChanges(): void {
    this.generation++;
    this.entries = [];
    this.definitions = [];
    this.draft = undefined;
    this.busy = false;
    this.loading = false;
    this.error = '';
    this.notice = '';
    this.search = '';
    this.status = '';
    this.page = 0;
    void this.load();
  }
  ngOnDestroy(): void {
    this.generation++;
  }
  async load(): Promise<void> {
    if (!this.teamId || this.loading || this.busy) return;
    const generation = this.generation,
      team = this.teamId;
    this.loading = true;
    this.error = '';
    try {
      const [entries, definitions] = await Promise.all([
        this.api.get<WorkflowSchedule[]>(team, 'schedules'),
        this.api.get<ScheduledDefinition[]>(team, 'definitions'),
      ]);
      if (generation !== this.generation) return;
      this.entries = entries;
      this.definitions = definitions.filter(
        (d) => d.latest_version > 0 && d.published_kind !== 'child',
      );
      this.page = Math.min(this.page, this.pages - 1);
    } catch (e) {
      if (generation === this.generation) this.error = errorMessage(e);
    } finally {
      if (generation === this.generation) this.loading = false;
      this.cd.markForCheck();
    }
  }
  async confirmDiscard(): Promise<boolean> {
    if (this.busy) return false;
    if (!this.dirty) return true;
    const snapshot = this.snapshot,
      generation = this.generation;
    return (
      (await this.dialogs.confirm('Discard your unsaved schedule changes?', {
        title: 'Unsaved schedule changes',
        confirmText: 'Discard changes',
        cancelText: 'Keep editing',
        danger: true,
      })) &&
      !this.busy &&
      snapshot === this.snapshot &&
      generation === this.generation
    );
  }
  async withConfirmedExit(action: () => Promise<void>): Promise<void> {
    this.confirmedExit = this.snapshot;
    try {
      await action();
    } finally {
      this.confirmedExit = '';
    }
  }
  @HostListener('window:beforeunload', ['$event']) beforeUnload(event: BeforeUnloadEvent): void {
    if (this.dirty && (this.busy || this.confirmedExit !== this.snapshot)) {
      event.preventDefault();
      event.returnValue = '';
    }
  }
  async create(): Promise<void> {
    if (!this.auth.canEdit() || !(await this.confirmDiscard())) return;
    const target = this.definitions[0];
    this.draft = {
      name: `${target?.name || 'Workflow'} schedule`,
      definitionId: target?.id || '',
      version: target?.latest_version || 1,
      mode: 'interval',
      minutes: 60,
      cron: '0 9 * * 1-5',
      timeZone: Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC',
      at: '',
      input: '{}',
      paused: false,
    };
    this.baseline = '';
    this.error = '';
    this.notice = '';
    const generation = this.generation;
    afterNextRender(
      () => {
        if (generation !== this.generation) return;
        this.editorPanel?.nativeElement.scrollIntoView({ block: 'start', behavior: 'smooth' });
        this.editorPanel?.nativeElement.focus({ preventScroll: true });
      },
      { injector: this.injector },
    );
    this.cd.markForCheck();
  }
  async close(): Promise<void> {
    if (!(await this.confirmDiscard())) return;
    this.draft = undefined;
    this.cd.markForCheck();
  }
  selectDefinition(id: string): void {
    if (!this.draft) return;
    this.draft.definitionId = id;
    this.draft.version = this.definitions.find((d) => d.id === id)?.latest_version || 1;
  }
  async save(): Promise<void> {
    if (!this.draft || this.busy || this.loading || !this.auth.canEdit()) return;
    const generation = this.generation,
      team = this.teamId;
    this.error = '';
    this.notice = '';
    try {
      if (!this.definitions.some((d) => d.id === this.draft?.definitionId))
        throw new Error('Choose a published main workflow.');
      const config = scheduleConfig(this.draft);
      this.busy = true;
      const created = await this.api.post<WorkflowSchedule>(team, 'schedules', config);
      if (generation !== this.generation) return;
      this.entries = [created, ...this.entries.filter((s) => s.id !== created.id)];
      this.draft = undefined;
      this.search = '';
      this.status = '';
      this.page = 0;
      this.notice = `Schedule created for published v${created.version}.${created.paused ? ' It starts paused.' : ''}`;
    } catch (e) {
      if (generation === this.generation) this.error = errorMessage(e);
    } finally {
      if (generation === this.generation) this.busy = false;
      this.cd.markForCheck();
    }
  }
  async change(
    item: WorkflowSchedule,
    action: 'pause' | 'resume' | 'trigger' | 'delete',
  ): Promise<void> {
    if (this.busy || this.loading || !this.auth.canEdit()) return;
    const generation = this.generation,
      team = this.teamId;
    if (
      action === 'delete' &&
      !(await this.dialogs.confirm('Delete this schedule? Existing runs will continue.', {
        title: 'Delete schedule?',
        confirmText: 'Delete schedule',
        danger: true,
      }))
    )
      return;
    if (generation !== this.generation || this.busy || this.loading || !this.auth.canEdit()) return;
    this.busy = true;
    this.error = '';
    this.notice = '';
    const path = `schedules/${encodeURIComponent(item.id)}`;
    try {
      if (action === 'delete') await this.api.delete(team, path);
      else await this.api.post(team, `${path}/${action}`, {});
      if (generation !== this.generation) return;
      if (action === 'delete') this.entries = this.entries.filter((s) => s.id !== item.id);
      else {
        // Mutation succeeded. A failed follow-up read must not look like a failed trigger.
        if (action !== 'trigger') item.paused = action === 'pause';
        try {
          const updated = await this.api.get<WorkflowSchedule>(team, path);
          if (generation !== this.generation) return;
          this.entries = this.entries.map((s) => (s.id === item.id ? updated : s));
        } catch {
          if (generation === this.generation)
            this.error =
              'The action succeeded, but updated details could not be loaded. Refresh schedules; do not repeat Run now.';
        }
      }
      if (generation !== this.generation) return;
      this.page = Math.min(this.page, this.pages - 1);
      this.notice =
        action === 'trigger'
          ? 'Run requested using saved input. Overlapping starts are skipped.'
          : `Schedule ${action} completed.`;
    } catch (e) {
      if (generation === this.generation) this.error = errorMessage(e);
    } finally {
      if (generation === this.generation) this.busy = false;
      this.cd.markForCheck();
    }
  }
}
