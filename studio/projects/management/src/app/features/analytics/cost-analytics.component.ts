import { CommonModule } from '@angular/common';
import {
  ChangeDetectorRef,
  Component,
  Input,
  OnChanges,
  OnDestroy,
  SimpleChanges,
  inject,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { AuthService, Team } from '@runloom/studio-platform';
import { Notice } from '@runloom/studio-platform';
import { UiIcon } from '@runloom/studio-platform';

interface Metrics {
  estimated_usd: string;
  average_usd: string | null;
  requests: number;
  priced: number;
  unknown: number;
  unpriced: number;
  pending: number;
  runs: number;
  priced_runs: number;
  input_tokens: number;
  output_tokens: number;
  failed_requests: number;
  retry_requests: number;
}
interface CostRow extends Metrics {
  id: string;
  name: string;
  team_name?: string;
  workflow_id?: string;
  temporal_run_id?: string;
  definition_version?: number;
  last_request_at?: string;
}
interface Analytics {
  window: { from: string; until: string; previous_from: string; bucket: string };
  summary: Metrics;
  previous: Metrics;
  trend: CostRow[];
  rows: CostRow[];
  total_rows: number;
  page_size: number;
  allowed_teams: Team[];
  as_of: string;
}
type Sort = 'cost' | 'name' | 'tokens' | 'requests' | 'runs' | 'latest';
type Group = 'runs' | 'teams' | 'models';

@Component({
  selector: 'runloom-cost-analytics',
  imports: [CommonModule, FormsModule, Notice, UiIcon],
  templateUrl: './cost-analytics.component.html',
  styleUrl: './cost-analytics.component.scss',
})
export class CostAnalytics implements OnChanges, OnDestroy {
  @Input({ required: true }) orgId = '';
  @Input() teamId = '';
  readonly auth = inject(AuthService);
  private readonly cd = inject(ChangeDetectorRef);
  private generation = 0;
  private asOf = '';
  data?: Analytics;
  busy = false;
  error = '';
  period = '1m';
  team = '';
  mode = 'all';
  page = 0;
  group: Group = 'runs';
  sort: Sort = 'cost';
  direction: 'asc' | 'desc' = 'desc';
  readonly periods = [
    { id: '1d', label: '1 day' },
    { id: '1w', label: '1 week' },
    { id: '1m', label: '1 month' },
    { id: 'mtd', label: 'MTD' },
    { id: 'ytd', label: 'YTD' },
    { id: '6m', label: '6 months' },
    { id: '1y', label: '1 year' },
  ];
  readonly groups: Group[] = ['runs', 'teams', 'models'];
  ngOnChanges(changes: SimpleChanges): void {
    this.team = changes['orgId'] ? '' : this.teamId;
    this.refresh();
  }
  ngOnDestroy(): void {
    this.generation++;
  }
  refresh(): void {
    this.asOf = '';
    this.page = 0;
    void this.load();
  }
  changeGroup(group: Group): void {
    this.group = group;
    this.page = 0;
    void this.load();
  }
  order(field: Sort): void {
    this.direction = this.sort === field && this.direction === 'desc' ? 'asc' : 'desc';
    this.sort = field;
    this.page = 0;
    void this.load();
  }
  sortState(field: Sort): 'ascending' | 'descending' | 'none' {
    return this.sort === field ? (this.direction === 'asc' ? 'ascending' : 'descending') : 'none';
  }
  sortMark(field: Sort): string {
    return this.sort === field ? (this.direction === 'asc' ? '↑' : '↓') : '↕';
  }
  navigate(delta: number): void {
    if (this.busy) return;
    this.page += delta;
    void this.load();
  }
  get teams(): Team[] {
    return this.data?.allowed_teams || this.auth.organization()?.teams || [];
  }
  get lastPage(): number {
    return Math.max(1, Math.ceil((this.data?.total_rows || 0) / 50));
  }
  money(value: string | null | undefined): string {
    if (value == null) return '—';
    const amount = Number(value);
    if (amount > 0 && amount < 0.000001) return '<$0.000001';
    return (
      '$' + amount.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 6 })
    );
  }
  spend(m: Metrics): string {
    return m.priced ? this.money(m.estimated_usd) : '—';
  }
  get coverage(): string {
    const m = this.data?.summary;
    return m?.requests ? ((100 * m.priced) / m.requests).toFixed(1) + '%' : '—';
  }
  get change(): string {
    if (!this.data) return '';
    const { summary: a, previous: b } = this.data;
    if (
      !a.requests ||
      !b.requests ||
      a.priced !== a.requests ||
      b.priced !== b.requests ||
      Number(b.estimated_usd) === 0
    )
      return 'Comparison unavailable or incomplete';
    const percent =
      (100 * (Number(a.estimated_usd) - Number(b.estimated_usd))) / Number(b.estimated_usd);
    return (percent > 0 ? '+' : '') + percent.toFixed(1) + '% vs previous equal-length period';
  }
  barHeight(row: CostRow): number {
    const max = Math.max(...(this.data?.trend.map((r) => Number(r.estimated_usd)) || [0]));
    return max > 0 ? Math.max(row.priced ? 2 : 0, (100 * Number(row.estimated_usd)) / max) : 0;
  }
  async load(): Promise<void> {
    const generation = ++this.generation,
      org = this.orgId;
    this.busy = true;
    this.error = '';
    this.data = undefined;
    this.cd.markForCheck();
    if (!org) {
      this.busy = false;
      return;
    }
    const query = new URLSearchParams({
      period: this.period,
      team: this.team,
      mode: this.mode,
      group: this.group,
      sort: this.sort,
      direction: this.direction,
      page: String(this.page),
    });
    if (this.asOf) query.set('as_of', this.asOf);
    try {
      const data = await this.auth.request<Analytics>(
        '/api/v1/studio/orgs/' + encodeURIComponent(org) + '/llm-analytics?' + query,
      );
      if (generation !== this.generation) return;
      if (!data?.summary || !Array.isArray(data.rows) || !Array.isArray(data.trend))
        throw new Error('Invalid analytics response.');
      this.data = data;
      this.asOf = data.as_of;
    } catch (error) {
      if (generation === this.generation)
        this.error = (error as Error).message || 'Cost analytics is unavailable.';
    } finally {
      if (generation === this.generation) {
        this.busy = false;
        this.cd.markForCheck();
      }
    }
  }
}
