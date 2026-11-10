import { CommonModule } from '@angular/common';
import {
  ChangeDetectorRef,
  Component,
  Input,
  OnChanges,
  OnDestroy,
  inject,
  ElementRef,
  ViewChild,
} from '@angular/core';
import { ExecutionTimelineComponent } from './components/execution-timeline/execution-timeline.component';
import { FormsModule } from '@angular/forms';
import { Notice, WorkspaceBridge } from '@runloom/studio-platform';
import { DashboardApi } from '../../core/api/dashboard-api.service';
import { errorMessage, sorted, withinDates } from '../../shared/utils/dashboard-data';
import { duration, isTest, runStatus, runTagFields, runTags } from './utils/run-history.utils';
import type { RunHistory, RunRecord } from './models/run.models';

@Component({
  selector: 'runloom-run-history',
  imports: [CommonModule, FormsModule, Notice, ExecutionTimelineComponent],
  templateUrl: './run-history.component.html',
  styleUrls: ['../../shared/styles/dashboard.scss', './run-history.component.scss'],
})
export class RunHistoryDashboard implements OnChanges, OnDestroy {
  @Input() teamId = '';
  private readonly api = inject(DashboardApi);
  private readonly cd = inject(ChangeDetectorRef);
  @ViewChild('detailsDialog') private detailsDialog?: ElementRef<HTMLDialogElement>;
  readonly bridge = inject(WorkspaceBridge);
  private generation = 0;
  private detailGeneration = 0;
  private requestGeneration = 0;
  private detailTimer?: ReturnType<typeof setTimeout>;
  private searchTimer?: ReturnType<typeof setTimeout>;
  private offset = 0;
  hasMore = false;
  tagKey = '';
  tagValue = '';
  readonly tagFields = runTagFields;
  readonly tagsOf = runTags;
  runs: RunRecord[] = [];
  detail?: RunHistory;
  selectedId = '';
  search = '';
  workflow = '';
  status = '';
  classification = '';
  from = '';
  to = '';
  busy = false;
  detailBusy = false;
  error = '';
  detailError = '';
  refreshed = '';
  sort: 'started' | 'workflow' | 'status' | 'duration' = 'started';
  ascending = false;
  page = 0;
  readonly statusOf = runStatus;
  readonly testRun = isTest;
  get workflows(): string[] {
    return [...new Set(this.runs.map((r) => r.definition_id))].sort();
  }
  get statuses(): string[] {
    return [...new Set(this.runs.map(runStatus))].sort();
  }
  get filtered(): RunRecord[] {
    return sorted(
      this.runs.filter(
        (r) =>
          (!this.workflow || r.definition_id === this.workflow) &&
          (!this.status || runStatus(r) === this.status) &&
          (!this.classification || isTest(r) === (this.classification === 'test')) &&
          withinDates(r.created_at, this.from, this.to),
      ),
      (r) =>
        this.sort === 'workflow'
          ? r.definition_id
          : this.sort === 'status'
            ? runStatus(r)
            : this.sort === 'duration'
              ? duration(r)
              : r.created_at,
      this.ascending,
    );
  }
  get rows(): RunRecord[] {
    return this.filtered.slice(this.page * 20, this.page * 20 + 20);
  }
  get pages(): number {
    return Math.max(1, Math.ceil(this.filtered.length / 20));
  }
  get completed(): number {
    return this.filtered.filter((r) => r.temporal_status === 'completed').length;
  }
  get openCount(): number {
    return this.filtered.filter((r) => r.temporal_status === 'running').length;
  }
  get failed(): number {
    return this.filtered.filter((r) => ['failed', 'timedout'].includes(r.temporal_status)).length;
  }
  setSort(sort: typeof this.sort): void {
    this.ascending = this.sort === sort ? !this.ascending : ['workflow', 'status'].includes(sort);
    this.sort = sort;
    this.page = 0;
  }
  ariaSort(sort: typeof this.sort): string {
    return this.sort === sort ? (this.ascending ? 'ascending' : 'descending') : 'none';
  }
  sortLabel(sort: typeof this.sort): string {
    return this.sort === sort ? (this.ascending ? '↑' : '↓') : '';
  }
  resetFilters(reload = true): void {
    this.search = '';
    this.tagKey = '';
    this.tagValue = '';
    this.workflow = '';
    this.status = '';
    this.classification = '';
    this.from = '';
    this.to = '';
    this.page = 0;
    if (reload) this.searchChanged();
  }
  searchChanged(): void {
    clearTimeout(this.searchTimer);
    this.requestGeneration++;
    this.runs = [];
    this.page = 0;
    this.hasMore = false;
    this.busy = true;
    this.searchTimer = setTimeout(() => void this.load(), 300);
  }
  filterTag(key: string, value: string): void {
    this.tagKey = key;
    this.tagValue = value;
    this.searchChanged();
  }
  ngOnChanges(): void {
    this.close();
    this.generation++;
    this.runs = [];
    this.busy = false;
    this.error = '';
    this.refreshed = '';
    clearTimeout(this.searchTimer);
    this.resetFilters(false);
    void this.load();
  }
  ngOnDestroy(): void {
    clearTimeout(this.detailTimer);
    clearTimeout(this.searchTimer);
    this.requestGeneration++;
    this.generation++;
    this.detailGeneration++;
  }
  async load(more = false): Promise<void> {
    if (!this.teamId) return;
    clearTimeout(this.searchTimer);
    const generation = this.generation;
    const request = ++this.requestGeneration;
    this.busy = true;
    this.error = '';
    try {
      if (Boolean(this.tagKey) !== Boolean(this.tagValue.trim())) {
        throw new Error('Choose a tag and enter its exact value, or clear both fields.');
      }
      const query = new URLSearchParams({
        limit: '200',
        offset: String(more ? this.offset : 0),
        search: this.search.trim(),
      });
      if (this.tagKey) {
        query.set('tag_key', this.tagKey);
        query.set('tag_value', this.tagValue.trim());
      }
      const runs = await this.api.get<RunRecord[]>(this.teamId, `runs?${query}`);
      if (generation !== this.generation || request !== this.requestGeneration) return;
      this.offset = (more ? this.offset : 0) + runs.length;
      this.hasMore = runs.length === 200 && this.offset <= 1000000;
      this.runs = more
        ? [...new Map([...this.runs, ...runs].map((run) => [run.workflow_id, run])).values()]
        : runs;
      this.refreshed = new Date().toISOString();
      this.page = Math.min(this.page, this.pages - 1);
      if (this.selectedId && !more) await this.open(this.selectedId);
    } catch (e) {
      if (generation === this.generation && request === this.requestGeneration)
        this.error = errorMessage(e);
    } finally {
      if (generation === this.generation && request === this.requestGeneration) this.busy = false;
      this.cd.markForCheck();
    }
  }
  async open(id: string): Promise<void> {
    clearTimeout(this.detailTimer);
    const generation = this.generation,
      detailGeneration = ++this.detailGeneration;
    const reveal = this.selectedId !== id;
    this.selectedId = id;
    if (reveal) this.detail = undefined;
    this.detailError = '';
    this.detailBusy = true;
    const dialog = this.detailsDialog?.nativeElement;
    if (dialog && !dialog.open) dialog.showModal();
    this.cd.markForCheck();
    try {
      const detail = await this.api.get<RunHistory>(
        this.teamId,
        `runs/${encodeURIComponent(id)}/history?trace=true`,
      );
      if (generation === this.generation && detailGeneration === this.detailGeneration) {
        this.detail = detail;
        if (detail.run.temporal_status === 'running') {
          this.detailTimer = setTimeout(() => void this.open(id), 5000);
        }
      }
    } catch (e) {
      if (generation === this.generation && detailGeneration === this.detailGeneration)
        this.detailError = errorMessage(e);
    } finally {
      if (generation === this.generation && detailGeneration === this.detailGeneration)
        this.detailBusy = false;
      this.cd.markForCheck();
    }
  }
  close(): void {
    this.detailsDialog?.nativeElement.close();
    clearTimeout(this.detailTimer);
    this.detailGeneration++;
    this.selectedId = '';
    this.detail = undefined;
    this.detailError = '';
    this.detailBusy = false;
  }
  durationLabel(run: RunRecord): string {
    const ms = duration(run);
    return ms < 0
      ? '—'
      : ms < 1000
        ? ms + ' ms'
        : ms < 60000
          ? (ms / 1000).toFixed(1) + ' s'
          : (ms / 60000).toFixed(1) + ' min';
  }
  tone(run: RunRecord): string {
    return ['failed', 'timedout'].includes(run.temporal_status)
      ? 'danger'
      : run.temporal_status === 'completed'
        ? 'success'
        : run.temporal_status === 'running'
          ? 'warning'
          : '';
  }
}
