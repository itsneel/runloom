import { ChangeDetectorRef, Component, ComponentRef, inject, Input, OnChanges, OnDestroy, ViewChild, ViewContainerRef } from '@angular/core';
import { ApplicationKind, ManagementPort, ManagementSection, Notice, WorkflowPort, WorkspaceBridge } from '@runloom/studio-platform';
import { HostedApplication, RemoteLoader } from '../../core/federation/remote-loader.service';

@Component({selector:'runloom-remote-outlet',imports:[Notice],templateUrl:'./remote-outlet.component.html',styleUrl:'./remote-outlet.component.scss'})
export class RemoteOutlet implements OnChanges, OnDestroy {
  @Input() kind: HostedApplication = 'workflows';
  @Input() platformSection = 'health';
  @Input() teamId = '';
  @Input() orgId = '';
  @Input() section: ManagementSection = 'teams';
  @Input() projectsView = false;
  @Input() projectId = '';
  @Input() workflowId = '';
  @Input() navigationExpanded = true;
  @Input() showNavigationToggle = false;
  @ViewChild('outlet',{read:ViewContainerRef,static:true}) private outlet!: ViewContainerRef;
  private readonly loader = inject(RemoteLoader);
  private readonly bridge = inject(WorkspaceBridge);
  private readonly cd = inject(ChangeDetectorRef);
  private ref?: ComponentRef<unknown>;
  private key = '';
  private generation = 0;
  loading = false;
  error = '';

  ngOnChanges(): void {
    const key = this.kind === 'workflows' ? JSON.stringify([this.kind, this.orgId, this.teamId, this.projectId, this.workflowId]) : this.kind;
    if (key !== this.key) { this.key = key; void this.mount(); }
    else this.inputs();
  }
  async mount(): Promise<void> {
    const generation = ++this.generation, kind = this.kind;
    this.clear(); this.loading = true; this.error = '';
    try {
      const component = await this.loader.load(kind);
      if (generation !== this.generation) return;
      this.ref = this.outlet.createComponent(component);
      this.inputs();
      this.ref.changeDetectorRef.detectChanges();
      if (kind === 'workflows') {
        const workflow = this.ref.instance as WorkflowPort;
        const subscription = workflow.toggleNavigation.subscribe(() => this.bridge.toggleNavigation.emit());
        this.ref.onDestroy(() => subscription.unsubscribe());
        this.bridge.workflow.set(workflow);
      } else if (kind === 'management') this.bridge.management.set(this.ref.instance as ManagementPort);
    } catch {
      if (generation === this.generation) {
        this.clear();
        this.error = `Unable to load ${kind === 'workflows' ? 'Workflow Studio' : kind === 'platform' ? 'Platform Admin' : 'Management'}. Check the connection and try again.`;
      }
    } finally {
      if (generation === this.generation) { this.loading = false; this.cd.markForCheck(); }
    }
  }
  private inputs(): void {
    if (!this.ref) return;
    const values = this.kind === 'platform' ? {platformSection:this.platformSection} : this.kind === 'workflows'
      ? {navigationExpanded:this.navigationExpanded,showNavigationToggle:this.showNavigationToggle,projectsView:this.projectsView,routeProjectId:this.projectId,routeDefinitionId:this.workflowId}
      : {orgId:this.orgId,teamId:this.teamId,managementSection:this.section};
    for (const [name,value] of Object.entries(values)) this.ref.setInput(name,value);
  }
  private clear(): void {
    this.bridge.workflow.set(undefined); this.bridge.management.set(undefined);
    this.outlet?.clear(); this.ref = undefined;
  }
  ngOnDestroy(): void { this.generation++; this.clear(); }
}
