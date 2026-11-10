import { Dialogs } from '@runloom/studio-platform';
import { CommonModule, Location } from '@angular/common';
import {
  afterNextRender,
  ChangeDetectorRef,
  Component,
  DestroyRef,
  effect,
  ElementRef,
  HostListener,
  inject,
  Injector,
  OnInit,
  signal,
  ViewChild,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { AuthService, ManagementSection, WorkspaceBridge, WorkflowPort, AuthoringDraftRequest } from '@runloom/studio-platform';
import { NavigationEnd, Router, RouterOutlet } from '@angular/router';
import { managementUrl } from '../../routing/workspace-paths';
import { Notice } from '@runloom/studio-platform';
import { UiIcon } from '@runloom/studio-platform';
import { ThemePicker } from '@runloom/studio-platform';
import { ThemeService } from '@runloom/studio-platform';
import { AssistantComponent } from '../../features/assistant/assistant.component';
import { PlatformAccess } from '../../core/api/platform-access.service';
import { loginLanding } from '../../routing/login-landing';
import { DocumentationService } from '../../core/config/documentation.service';
import { organizationOptions } from './organization-options';


@Component({
  selector: 'app-root',
  imports: [
    CommonModule,
    FormsModule,
    Dialogs,
    Notice,
    UiIcon,
    ThemePicker,
    RouterOutlet,
    AssistantComponent,
  ],
  templateUrl: './shell.component.html',
  styleUrl: './shell.component.scss',
})
export class Shell implements OnInit {
  readonly prepareAssistantDraft = async (): Promise<void> => {
    await this.bridge.workflow()?.prepareAuthoringDraft?.();
  };
  readonly applyAssistantDraft = async (request: AuthoringDraftRequest): Promise<void> => {
    const port = this.bridge.workflow();
    if (!port?.applyAuthoringDraft) throw new Error('Reload Studio to use Lumi with this workflow editor.');
    await port.applyAuthoringDraft(request);
  };
  readonly documentation = inject(DocumentationService);
  readonly startupReady = signal(false);
  readonly initialRouteReady = signal(false);
  private readonly location = inject(Location);
  readonly platformAccess = inject(PlatformAccess);
  platformMode = false;
  readonly bridge = inject(WorkspaceBridge);
  private get studio(): WorkflowPort | undefined { return this.bridge.workflow(); }
  @ViewChild('sidebar') private sidebar?: ElementRef<HTMLElement>;
  @ViewChild('switcherToggle') private switcherToggle?: ElementRef<HTMLButtonElement>;
  @ViewChild('organizationSelect') private organizationSelect?: ElementRef<HTMLSelectElement>;
  @ViewChild('mobileToggle') private mobileToggle?: ElementRef<HTMLButtonElement>;
  readonly auth = inject(AuthService);
  readonly theme = inject(ThemeService);
  private readonly cd = inject(ChangeDetectorRef);
  private readonly injector = inject(Injector);
  private readonly router = inject(Router);
  private navigationStarted = false;
  railTooltip?: { label: string; left: number; top: number };
  private tooltipTimer?: ReturnType<typeof setTimeout>;
  private tooltipButton?: HTMLButtonElement;
  error = '';
  notice = '';
  manage = false;
  projectsView = false;
  managementSection: ManagementSection = 'teams';
  sidebarCollapsed = false;
  workspaceSwitcherOpen = false;
  mobileOpen = false;
  mobileViewport = window.innerWidth < 900;
  private working = false;
  get busy(): boolean { return this.working || !!this.bridge.management()?.busy; }
  set busy(value: boolean) { this.working = value; }

  constructor() {
    // Capture before focused controls can stop bubbling. Cancel the browser's
    // Save Page command without blocking the workflow editor's save listener.
    document.addEventListener('keydown', this.preventBrowserSave, true);
    effect(() => {
      if (this.startupReady() && this.auth.ready() && this.auth.signedIn() && !this.navigationStarted) {
        this.navigationStarted = true;
        this.router.initialNavigation();
      }
    });
    this.bridge.host = {selectTeam: id => this.selectTeam(id), openManagement: section => this.openManagement(section)};
    const subscription = this.bridge.toggleNavigation.subscribe(() => this.toggleSidebar());
    const navigation = this.router.events.subscribe(event => {
      if (!(event instanceof NavigationEnd)) return;
      let route = this.router.routerState.snapshot.root;
      while (route.firstChild) route = route.firstChild;
      this.manage = route.data['application'] === 'management';
      const wasPlatform = this.platformMode;
      this.platformMode = route.data['application'] === 'platform';
      this.initialRouteReady.set(true);
      if (wasPlatform && !this.platformMode) void this.platformAccess.refresh().catch(() => {});
      this.projectsView = route.data['view'] === 'projects';
      this.managementSection = route.data['section'] || 'teams';
      this.workspaceSwitcherOpen = false;
      this.error = '';
      this.notice = '';
      this.closeMobile();
      this.cd.markForCheck();
    });
    inject(DestroyRef).onDestroy(() => {
      document.removeEventListener('keydown', this.preventBrowserSave, true);
      this.hideRailTooltip(); subscription.unsubscribe(); navigation.unsubscribe(); this.bridge.host = undefined;
    });
  }

  get pageTitle(): string {
    if (!this.manage && this.auth.team()) return this.projectsView ? 'Projects' : 'Workflows';
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
  initials(value: string): string {
    return value.trim().slice(0, 2).toUpperCase();
  }
  openDocumentation(page?: 'consumer'): void {
    const url = page === 'consumer' ? `${this.documentation.url()}/api/consumer` : this.documentation.url();
    window.open(url, '_blank', 'noopener,noreferrer');
    this.closeMobile();
  }
  get organizationChoices(): Array<{id: string; label: string}> { return organizationOptions(this.auth.organizations(), this.auth.organizationId()); }
  toggleSidebar(): void {
    this.hideRailTooltip();
    if (!this.mobileViewport) {
      this.sidebarCollapsed = !this.sidebarCollapsed;
      afterNextRender(
        () =>
          this.sidebar?.nativeElement.querySelector<HTMLButtonElement>('.sidebar-toggle')?.focus(),
        { injector: this.injector },
      );
      return;
    }
    this.mobileOpen = !this.mobileOpen;
    afterNextRender(
      () =>
        this.mobileOpen
          ? this.sidebar?.nativeElement.querySelector<HTMLButtonElement>('button')?.focus()
          : (
              this.mobileToggle?.nativeElement ||
              document.querySelector<HTMLButtonElement>('button[aria-label="Toggle workspace sidebar"]')
            )?.focus(),
      { injector: this.injector },
    );
  }
  toggleWorkspaceSwitcher(): void {
    this.workspaceSwitcherOpen = !this.workspaceSwitcherOpen;
    if (this.workspaceSwitcherOpen) {
      afterNextRender(() => {
        if (this.workspaceSwitcherOpen) this.organizationSelect?.nativeElement.focus();
      }, { injector: this.injector });
    }
  }
  @HostListener('document:pointerdown', ['$event'])
  dismissWorkspaceSwitcher(event: PointerEvent): void {
    const target = event.target;
    if (this.workspaceSwitcherOpen && target instanceof Element &&
        !target.closest('.workspace-switcher, .workspace-summary, runloom-dialogs')) {
      this.workspaceSwitcherOpen = false;
    }
  }
  showRailTooltip(event: Event): void {
    if (!this.sidebarCollapsed || this.mobileViewport) return;
    const button = (event.target as HTMLElement).closest('button');
    if (!button || button.disabled) return;
    const label = button.getAttribute('aria-label');
    if (!label) return;
    this.keepRailTooltip();
    this.tooltipButton = button;
    const rect = button.getBoundingClientRect();
    this.railTooltip = {
      label,
      left: (this.sidebar?.nativeElement.getBoundingClientRect().right || rect.right) + 8,
      top: Math.max(24, Math.min(window.innerHeight - 24, rect.top + rect.height / 2)),
    };
  }
  keepRailTooltip(): void {
    clearTimeout(this.tooltipTimer);
  }
  deferHideRailTooltip(): void {
    if (!this.railTooltip) return;
    this.keepRailTooltip();
    this.tooltipTimer = setTimeout(() => {
      this.hideRailTooltip();
      this.cd.markForCheck();
    }, 120);
  }
  hideRailTooltip(): void {
    this.keepRailTooltip();
    this.railTooltip = undefined;
    this.tooltipButton = undefined;
  }
  refreshRailTooltip(): void {
    const button = this.tooltipButton;
    if (!this.railTooltip || !button) return;
    const rect = button.getBoundingClientRect();
    if (rect.bottom <= 0 || rect.top >= window.innerHeight) {
      this.hideRailTooltip();
    } else {
      this.railTooltip = {...this.railTooltip, top: Math.max(24, Math.min(window.innerHeight - 24, rect.top + rect.height / 2))};
    }
  }
  closeMobile(): void {
    if (this.mobileOpen) {
      this.mobileOpen = false;
      afterNextRender(
        () =>
          (
            this.mobileToggle?.nativeElement ||
            document.querySelector<HTMLButtonElement>('button[aria-label="Toggle workspace sidebar"]')
          )?.focus(),
        { injector: this.injector },
      );
    }
  }
  @HostListener('window:resize') resize(): void {
    this.hideRailTooltip();
    this.mobileViewport = window.innerWidth < 900;
    if (!this.mobileViewport) this.mobileOpen = false;
  }
  preventBrowserSave(event: KeyboardEvent): void {
    if ((event.metaKey || event.ctrlKey) && !event.altKey && !event.shiftKey && event.key.toLowerCase() === 's') {
      event.preventDefault();
    }
  }

  @HostListener('document:keydown', ['$event']) navigationKeys(event: KeyboardEvent): void {
    if (document.querySelector('#assistant-panel[open]')) return;
    if (event.key === 'Escape') this.hideRailTooltip();
    // Native popovers own Escape before the navigation drawer does.
    if (event.key === 'Escape' && document.querySelector('runloom-theme-picker [popover]:popover-open')) return;
    if (event.key === 'Escape' && this.workspaceSwitcherOpen && !document.querySelector('runloom-dialogs dialog[open]')) {
      this.workspaceSwitcherOpen = false;
      this.switcherToggle?.nativeElement.focus();
      return;
    }
    if (!this.mobileOpen || document.querySelector('runloom-dialogs dialog[open]')) return;
    if (event.key === 'Escape') {
      event.preventDefault();
      this.closeMobile();
    }
    if (event.key !== 'Tab') return;
    const elements = Array.from(
      this.sidebar?.nativeElement.querySelectorAll<HTMLElement>(
        'button:not(:disabled), input:not(:disabled), select:not(:disabled), a[href]',
      ) || [],
    ).filter((e) => e.getClientRects().length > 0);
    const first = elements[0],
      last = elements.at(-1);
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last?.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first?.focus();
    }
  }
  async ngOnInit(): Promise<void> {
    try {
      await this.auth.initialize();
      if (this.auth.signedIn()) {
        const identity = await this.platformAccess.refresh().catch(() => undefined);
        const landing = loginLanding(this.location.path(), identity?.role || '');
        if (landing) this.location.replaceState(landing);
      }
    } catch (error) {
      this.error = (error as Error).message || 'Unable to connect to sign-in.';
      this.auth.ready.set(true);
    }
    this.startupReady.set(true);
    this.cd.markForCheck();
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
  signIn(google = false): void {
    void this.action(() => this.auth.login(google));
  }
  register(): void {
    void this.action(() => this.auth.register());
  }
  async logout(): Promise<void> {
    if (!(await this.confirmManagementDiscard())) return;
    const studio = this.studio;
    if (studio && !(await studio.confirmDiscardChanges())) return;
    const management = this.bridge.management();
    const exit = () => management ? management.withConfirmedExit(() => this.auth.logout()) : this.auth.logout();
    await this.action(() => studio ? studio.withConfirmedExit(exit) : exit());
  }
  private async confirmManagementDiscard(): Promise<boolean> {
    return !this.bridge.management() || await this.bridge.management()!.confirmDiscard();
  }

  async selectOrg(id: string, select?: HTMLSelectElement): Promise<void> {
    if (id === this.auth.organizationId()) return;
    if (!(await this.confirmManagementDiscard())) {
      if (select) select.value = this.auth.organizationId();
      return;
    }
    if (this.studio && !(await this.studio.confirmDiscardChanges())) {
      if (select) select.value = this.auth.organizationId();
      return;
    }
    this.auth.selectOrganization(id);
    this.error = '';
    this.notice = '';
    await this.loadMembers();
  }
  async selectTeam(id: string, select?: HTMLSelectElement): Promise<void> {
    if (id === this.auth.teamId()) return;
    if (!(await this.confirmManagementDiscard())) {
      if (select) select.value = this.auth.teamId();
      return;
    }
    if (this.studio && !(await this.studio.confirmDiscardChanges())) {
      if (select) select.value = this.auth.teamId();
      return;
    }
    this.auth.selectTeam(id);
    this.error = '';
    this.notice = '';
    await this.loadMembers();
  }
  async openWorkflowDashboard(): Promise<void> {
    if (this.busy || !this.auth.team()) return;
    if (this.router.url !== '/workflows') {
      if (await this.router.navigateByUrl('/workflows')) this.closeMobile();
      return;
    }
    if (await this.studio?.showDashboard?.()) this.closeMobile();
  }
  async openProjects(): Promise<void> {
    if (this.busy || !this.auth.team()) return;
    if (this.projectsView) {
      if (await this.studio?.showDashboard?.()) this.closeMobile();
      return;
    }
    if (await this.router.navigateByUrl('/workflows/projects')) this.closeMobile();
  }
  async openManagement(section?: ManagementSection): Promise<void> {
    if (this.busy) return;
    if (section && this.manage && this.managementSection === section) {
      this.closeMobile();
      return;
    }
    await this.router.navigateByUrl(section ? managementUrl(section) : this.manage ? '/workflows' : managementUrl('teams'));
  }
  async openPlatform(): Promise<void> {
    if (this.busy) return;
    if (await this.router.navigateByUrl('/platform')) this.closeMobile();
  }
  async loadMembers(): Promise<void> { await this.bridge.management()?.loadMembers(); }
}
