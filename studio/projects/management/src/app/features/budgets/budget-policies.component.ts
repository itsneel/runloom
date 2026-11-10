import { CommonModule } from '@angular/common';
import {
  Component,
  Input,
  OnChanges,
  OnDestroy,
  HostListener,
  ChangeDetectorRef,
  inject,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { AuthService } from '@runloom/studio-platform';
import { injectDialogs } from '@runloom/studio-platform';
import { Notice } from '@runloom/studio-platform';

interface Policy {
  id: string;
  revision: number;
  name: string;
  enabled: boolean;
  soft_usd: string;
  hard_usd: string;
  projects: string[];
  excluded_workflows: string[];
  reason: string;
  updated_at?: string;
}
interface Decision {
  policy_id: string;
  policy_name: string;
  project_id: string;
  reason: string;
  message: string;
}
interface Report {
  items: Policy[];
  projects: { id: string; name: string }[];
  workflows: { id: string; name: string }[];
  spend: {
    project_id: string;
    project_name: string;
    spend_usd: string;
    incomplete: boolean;
    requests: number;
  }[];
  alerts: Decision[];
  events: {
    event_id: string;
    event_type: string;
    workflow_id: string;
    created_at: string;
    payload: Decision;
  }[];
  changes: { actor: string; policy: Policy }[];
  month: string;
  resets_at: string;
}

@Component({
  selector: 'runloom-budget-policies',
  imports: [CommonModule, FormsModule, Notice],
  templateUrl: './budget-policies.component.html',
  styleUrls: ['../../shared/styles/management-forms.scss', './budget-policies.component.scss'],
})
export class BudgetPolicies implements OnChanges, OnDestroy {
  @Input() teamId = '';
  @Input() orgId = '';
  readonly auth = inject(AuthService);
  private readonly cd = inject(ChangeDetectorRef);
  private readonly dialogs = injectDialogs();
  private generation = 0;
  private baseline = '';
  private confirmedExit?: () => boolean;
  data?: Report;
  draft?: Policy;
  busy = false;
  error = '';
  notice = '';
  allProjects = true;
  sort: 'name' | 'spend' = 'spend';
  ascending = false;
  get endpoint(): string {
    return `/api/v1/studio/orgs/${encodeURIComponent(this.orgId)}/teams/${encodeURIComponent(this.teamId)}/llm-budget-policies`;
  }
  get dirty(): boolean {
    return !!this.draft && (this.busy || this.snapshot() !== this.baseline);
  }
  private snapshot(): string {
    return JSON.stringify([this.draft, this.allProjects]);
  }
  private unchanged(): () => boolean {
    const generation = this.generation,
      snapshot = this.snapshot(),
      busy = this.busy;
    return () =>
      this.generation === generation && this.snapshot() === snapshot && this.busy === busy;
  }
  async confirmDiscard(): Promise<boolean> {
    if (!this.dirty) return true;
    const same = this.unchanged();
    return (
      (await this.dialogs.confirm('Discard unsaved LLM budget policy changes?', {
        title: 'Discard policy changes?',
        confirmText: 'Discard changes',
        cancelText: 'Keep editing',
        danger: true,
      })) && same()
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
    this.generation++;
    this.draft = undefined;
    this.data = undefined;
    this.error = '';
    this.notice = '';
    this.busy = false;
    void this.load();
  }
  ngOnDestroy(): void {
    this.generation++;
  }
  async load(): Promise<void> {
    if (!this.teamId || !this.orgId || this.busy) return;
    const generation = this.generation;
    this.busy = true;
    this.error = '';
    try {
      const data = await this.auth.request<Report>(this.endpoint);
      if (generation === this.generation) this.data = data;
    } catch (e) {
      if (generation === this.generation) {
        this.data = undefined;
        this.error = (e as Error).message;
      }
    } finally {
      if (generation === this.generation) this.busy = false;
      this.cd.markForCheck();
    }
  }
  async edit(policy?: Policy): Promise<void> {
    const generation = this.generation;
    if (this.busy || !this.auth.teamAdmin() || !(await this.confirmDiscard())) return;
    if (generation !== this.generation || !this.auth.teamAdmin()) return;
    this.draft = policy
      ? structuredClone(policy)
      : {
          id: crypto.randomUUID(),
          revision: 0,
          name: '',
          enabled: true,
          soft_usd: '',
          hard_usd: '',
          projects: [],
          excluded_workflows: [],
          reason: '',
        };
    this.draft.projects ||= [];
    this.draft.excluded_workflows ||= [];
    this.draft.reason = '';
    this.allProjects = this.draft.projects.length === 0;
    this.baseline = this.snapshot();
    this.notice = '';
    this.error = '';
    this.cd.markForCheck();
  }
  async cancel(): Promise<void> {
    if (await this.confirmDiscard()) {
      this.draft = undefined;
      this.cd.markForCheck();
    }
  }
  toggle(field: 'projects' | 'excluded_workflows', id: string, checked: boolean): void {
    if (!this.draft) return;
    this.draft[field] = checked
      ? [...new Set([...this.draft[field], id])]
      : this.draft[field].filter((v) => v !== id);
  }
  async save(): Promise<void> {
    if (!this.draft || this.busy || !this.auth.teamAdmin()) return;
    if (!this.allProjects && !this.draft.projects.length) {
      this.error = 'Select at least one project, or apply to all projects.';
      return;
    }
    const generation = this.generation;
    const body = { ...this.draft, projects: this.allProjects ? [] : this.draft.projects };
    this.busy = true;
    this.error = '';
    try {
      await this.auth.request(`${this.endpoint}/${encodeURIComponent(body.id)}`, body, 'PUT');
      if (generation !== this.generation) return;
      this.draft = undefined;
      this.notice =
        'Policy saved. Future checks use these rules. Paused workflows require an explicit resume.';
    } catch (e) {
      if (generation === this.generation) this.error = (e as Error).message;
    } finally {
      if (generation === this.generation) this.busy = false;
      this.cd.markForCheck();
    }
    if (generation === this.generation && !this.draft) await this.load();
  }
  get rows() {
    return [...(this.data?.spend || [])].sort((a, b) => {
      const delta =
        this.sort === 'name'
          ? a.project_name.localeCompare(b.project_name)
          : Number(a.spend_usd) - Number(b.spend_usd);
      return (this.ascending ? 1 : -1) * delta;
    });
  }
  order(field: 'name' | 'spend'): void {
    this.ascending = this.sort === field ? !this.ascending : field === 'name';
    this.sort = field;
  }
  state(project: string): string {
    if (
      !this.data?.items.some(
        (p) => p.enabled && (!p.projects?.length || p.projects.includes(project)),
      )
    )
      return 'No active policy';
    const alerts = this.data?.alerts.filter((a) => a.project_id === project) || [];
    return alerts.some((a) => a.reason === 'hard_limit_reached')
      ? 'Hard limit reached'
      : alerts.length
        ? 'Soft limit reached'
        : 'Below configured limits';
  }
  projectNames(policy: Policy): string {
    return !policy.projects?.length
      ? 'All current and future projects'
      : policy.projects
          .map((id) => this.data?.projects.find((p) => p.id === id)?.name || id)
          .join(', ');
  }
  money(value: string): string {
    const [whole, fraction = ''] = value.split('.');
    const decimals = fraction.replace(/0+$/, '');
    return '$' + whole.replace(/\B(?=(\d{3})+(?!\d))/g, ',') + (decimals ? '.' + decimals : '');
  }
}
