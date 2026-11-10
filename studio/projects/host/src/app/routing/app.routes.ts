import { Routes } from '@angular/router';
import { ManagementSection } from '@runloom/studio-platform';
import { WorkspacePage } from '../layout/workspace-page/workspace-page.component';
import { managementPaths } from './workspace-paths';
import { confirmManagementTabExit, confirmWorkspaceExit } from './workspace-exit.guard';

export const routes: Routes = [
  { path: 'platform', pathMatch: 'full', redirectTo: 'platform/health' },
  { path: 'platform/:section', component: WorkspacePage, data: { application: 'platform' }, canDeactivate: [confirmWorkspaceExit] },
  { path: '', pathMatch: 'full', redirectTo: 'workflows' },
  {
    path: 'workflows/projects/:projectId', component: WorkspacePage,
    data: { application: 'workflows' }, canDeactivate: [confirmWorkspaceExit], runGuardsAndResolvers: 'always',
  },
  {
    path: 'workflows/definitions/:workflowId', component: WorkspacePage,
    data: { application: 'workflows' }, canDeactivate: [confirmWorkspaceExit], runGuardsAndResolvers: 'always',
  },
  {
    path: 'workflows/projects', component: WorkspacePage,
    data: { application: 'workflows', view: 'projects' }, canDeactivate: [confirmWorkspaceExit],
  },
  {
    path: 'workflows', component: WorkspacePage,
    data: { application: 'workflows' }, canDeactivate: [confirmWorkspaceExit],
  },
  {
    path: 'management', component: WorkspacePage, data: { application: 'management' },
    canDeactivate: [confirmWorkspaceExit], canActivateChild: [confirmManagementTabExit], children: [
      { path: '', pathMatch: 'full', redirectTo: 'teams' },
      ...Object.entries(managementPaths).map(([section, path]) => ({
        path, children: [],
        data: { application: 'management', section: section as ManagementSection },
      })),
      { path: '**', redirectTo: 'teams' },
    ],
  },
  { path: '**', redirectTo: 'workflows' },
];
