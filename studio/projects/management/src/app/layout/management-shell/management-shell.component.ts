import { CommonModule } from '@angular/common';
import {
  ChangeDetectorRef,
  Component,
  Input,
  OnChanges,
  OnDestroy,
  ViewChild,
  inject,
  afterNextRender,
  Injector,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import {
  AuthService,
  Member,
  ManagementSection,
  WorkspaceBridge,
  injectDialogs,
  Notice,
  UiIcon,
} from '@runloom/studio-platform';
import { ServiceAccounts } from '../../features/service-accounts/service-accounts.component';
import { WorkflowSecrets } from '../../features/secrets/workflow-secrets.component';
import { Connections } from '../../features/connections/connections.component';
import { CostAnalytics } from '../../features/analytics/cost-analytics.component';
import { BudgetPolicies } from '../../features/budgets/budget-policies.component';
import { PromptLibrary } from '../../features/prompts/prompt-library.component';
import { RunHistoryDashboard } from '../../features/runs/run-history.component';
import { Schedules } from '../../features/schedules/schedules.component';
import { Organizations } from '../../features/organizations/organizations.component';

type TeamMember = Member & { inherited?: boolean };

@Component({
  selector: 'runloom-management',
  imports: [
    CommonModule,
    FormsModule,
    ServiceAccounts,
    WorkflowSecrets,
    Connections,
    CostAnalytics,
    BudgetPolicies,
    Notice,
    UiIcon,
    PromptLibrary,
    RunHistoryDashboard,
    Schedules,
    Organizations,
  ],
  templateUrl: './management-shell.component.html',
  styleUrl: './management-shell.component.scss',
})
export class Management implements OnChanges, OnDestroy {
  @Input() managementSection: ManagementSection = 'teams';
  @Input() orgId = '';
  @Input() teamId = '';
  @ViewChild(WorkflowSecrets) private secrets?: WorkflowSecrets;
  @ViewChild(Connections) private connections?: Connections;
  @ViewChild(BudgetPolicies) private budgets?: BudgetPolicies;
  @ViewChild(ServiceAccounts) private accounts?: ServiceAccounts;
  @ViewChild(PromptLibrary) private prompts?: PromptLibrary;
  @ViewChild(Schedules) private schedules?: Schedules;
  @ViewChild(Organizations) private organizations?: Organizations;
  @ViewChild(RunHistoryDashboard) private runs?: RunHistoryDashboard;
  private readonly injector = inject(Injector);
  readonly auth = inject(AuthService);
  private readonly bridge = inject(WorkspaceBridge);
  private readonly cd = inject(ChangeDetectorRef);
  private readonly dialogs = injectDialogs();
  readonly manage = true;
  busy = false;
  memberSearch = '';
  private membersGeneration = 0;
  error = '';
  notice = '';
  teamName = '';
  orgEmail = '';
  teamEmail = '';
  orgRole = 'member';
  teamRole = 'viewer';
  orgMembers: Member[] = [];
  teamMembers: TeamMember[] = [];
  get pageTitle(): string {
    return {
      teams: 'Teams & access',
      organization: 'Organization',
      accounts: 'Service accounts',
      secrets: 'Workflow secrets',
      connections: 'Database connections',
      analytics: 'Cost analytics',
      budgets: 'Soft and Hard Limit Policies',
      prompts: 'Prompt library',
      runs: 'Workflow run history',
      schedules: 'Workflow schedules',
    }[this.managementSection];
  }
  ngOnChanges(): void {
    this.memberSearch = '';
    this.error = '';
    this.notice = '';
    void this.loadMembers();
  }
  ngOnDestroy(): void {
    this.membersGeneration++;
  }
  async selectTeam(id: string): Promise<void> {
    await this.bridge.host?.selectTeam(id);
  }
  async openManagement(section: ManagementSection): Promise<void> {
    await this.bridge.host?.openManagement(section);
  }
  async openScheduledRun(id: string): Promise<void> {
    const team = this.teamId;
    await this.bridge.host?.openManagement('runs');
    afterNextRender(
      () => {
        if (this.managementSection === 'runs' && this.teamId === team) void this.runs?.open(id);
      },
      { injector: this.injector },
    );
  }
  async confirmDiscard(): Promise<boolean> {
    if (this.organizations && !(await this.organizations.confirmDiscard())) return false;
    if (this.connections && !(await this.connections.confirmDiscard())) return false;
    if (this.schedules && !(await this.schedules.confirmDiscard())) return false;
    if (this.prompts && !(await this.prompts.confirmDiscard())) return false;
    if (this.budgets && !(await this.budgets.confirmDiscard())) return false;
    if (this.secrets && !(await this.secrets.confirmDiscard())) return false;
    return !this.accounts || (await this.accounts.confirmDiscard());
  }
  private confirmManagementDiscard(): Promise<boolean> {
    return this.confirmDiscard();
  }
  async withConfirmedExit(action: () => Promise<void>): Promise<void> {
    const organizationExit = () =>
      this.organizations ? this.organizations.withConfirmedExit(action) : action();
    const connectionExit = () =>
      this.connections ? this.connections.withConfirmedExit(organizationExit) : organizationExit();
    const scheduleExit = () =>
      this.schedules ? this.schedules.withConfirmedExit(connectionExit) : connectionExit();
    const promptExit = () =>
      this.prompts ? this.prompts.withConfirmedExit(scheduleExit) : scheduleExit();
    const accountExit = () =>
      this.accounts ? this.accounts.withConfirmedExit(promptExit) : promptExit();
    const budgetExit = () =>
      this.budgets ? this.budgets.withConfirmedExit(accountExit) : accountExit();
    await (this.secrets ? this.secrets.withConfirmedExit(budgetExit) : budgetExit());
  }
  get shownOrgMembers(): Member[] {
    return this.orgMembers.filter((m) =>
      m.email.toLowerCase().includes(this.memberSearch.toLowerCase()),
    );
  }
  get shownTeamMembers(): TeamMember[] {
    return this.teamMembers.filter((m) =>
      m.email.toLowerCase().includes(this.memberSearch.toLowerCase()),
    );
  }
  initials(value: string): string {
    return value.trim().slice(0, 2).toUpperCase();
  }
  async action(work: () => Promise<unknown>): Promise<void> {
    this.busy = true;
    this.error = '';
    this.notice = '';
    try {
      await work();
    } catch (error) {
      this.error = (error as Error).message || 'The change could not be saved.';
    } finally {
      this.busy = false;
      this.cd.markForCheck();
    }
  }
  async loadMembers(): Promise<void> {
    const generation = ++this.membersGeneration;
    this.orgMembers = [];
    this.teamMembers = [];
    if (
      this.manage &&
      ['analytics', 'budgets', 'prompts', 'runs', 'schedules', 'connections'].includes(
        this.managementSection,
      )
    ) {
      this.busy = false;
      return;
    }
    if (!this.manage && this.auth.team()) return;
    const orgPath = this.orgPath(),
      teamPath = this.teamPath();
    this.busy = true;
    try {
      const [orgMembers, teamMembers] = await Promise.all([
        this.auth.orgAdmin()
          ? this.auth.request<Member[]>(orgPath + '/members')
          : Promise.resolve([]),
        this.auth.team()
          ? this.auth.request<TeamMember[]>(teamPath + '/members')
          : Promise.resolve([]),
      ]);
      if (generation === this.membersGeneration) {
        this.orgMembers = orgMembers;
        this.teamMembers = teamMembers;
      }
    } catch (error) {
      if (generation === this.membersGeneration)
        this.error = (error as Error).message || 'Unable to load members.';
    } finally {
      if (generation === this.membersGeneration) this.busy = false;
      this.cd.markForCheck();
    }
  }
  private orgPath(): string {
    return '/api/v1/studio/orgs/' + this.auth.organizationId();
  }
  private teamPath(): string {
    return this.orgPath() + '/teams/' + this.auth.teamId();
  }

  async createTeam(): Promise<void> {
    if (!(await this.confirmManagementDiscard())) return;
    await this.action(async () => {
      const team = await this.auth.request<{ id: string }>(this.orgPath() + '/teams', {
        name: this.teamName,
      });
      await this.auth.refresh();
      this.auth.selectTeam(team.id);
      this.teamName = '';
      this.notice = 'Team created. Add members below to get started.';
    });
    if (!this.error) await this.loadMembers();
  }
  async saveMember(team: boolean, email: string, role: string): Promise<void> {
    await this.action(async () => {
      await this.auth.request((team ? this.teamPath() : this.orgPath()) + '/members', {
        email,
        role,
      });
      await this.auth.refresh();
      this.orgEmail = '';
      this.teamEmail = '';
      this.notice = team ? 'Team membership saved.' : 'Organization membership saved.';
    });
    if (!this.error) await this.loadMembers();
  }
  async removeMember(team: boolean, email: string): Promise<void> {
    const path =
      (team ? this.teamPath() : this.orgPath()) + '/members/' + encodeURIComponent(email);
    if (
      !(await this.dialogs.confirm(
        'Remove ' + email + ' from this ' + (team ? 'team' : 'organization') + '?',
        { title: 'Remove member?', confirmText: 'Remove member', danger: true },
      ))
    )
      return;
    await this.action(async () => {
      await this.auth.request(path, undefined, 'DELETE');
      await this.auth.refresh();
    });
    if (!this.error) await this.loadMembers();
  }
}
