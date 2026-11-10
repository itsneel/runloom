import { ChangeDetectorRef, Component, DestroyRef, inject, Signal } from '@angular/core';
import { ActivatedRoute, NavigationEnd, ROUTER_OUTLET_DATA, Router } from '@angular/router';
import { AuthService, ManagementSection } from '@runloom/studio-platform';
import { RemoteOutlet } from '../remote-outlet/remote-outlet.component';

@Component({
  selector: 'runloom-workspace-page',
  imports: [RemoteOutlet],
  templateUrl: './workspace-page.component.html',
  styleUrl: './workspace-page.component.scss',
})
export class WorkspacePage {
  readonly auth = inject(AuthService);
  readonly layout = inject(ROUTER_OUTLET_DATA) as Signal<{ navigationExpanded: boolean }>;
  private readonly route = inject(ActivatedRoute);
  readonly management = this.route.snapshot.data['application'] === 'management';
  readonly platform = this.route.snapshot.data['application'] === 'platform';
  get platformSection(): string { return this.route.snapshot.paramMap.get('section') || 'health'; }
  readonly projectsView = this.route.snapshot.data['view'] === 'projects';
  get projectId(): string { return this.route.snapshot.paramMap.get('projectId') ?? ''; }
  get workflowId(): string { return this.route.snapshot.paramMap.get('workflowId') ?? ''; }
  get section(): ManagementSection { return this.route.snapshot.firstChild?.data['section'] || 'teams'; }

  constructor() {
    const cd = inject(ChangeDetectorRef);
    const subscription = inject(Router).events.subscribe(event => {
      if (event instanceof NavigationEnd) cd.markForCheck();
    });
    inject(DestroyRef).onDestroy(() => subscription.unsubscribe());
  }
}
