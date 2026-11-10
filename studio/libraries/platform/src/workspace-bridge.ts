import { EventEmitter, Injectable, TemplateRef, signal } from '@angular/core';

export type ManagementSection = 'teams' | 'organization' | 'accounts' | 'secrets' | 'connections' | 'analytics' | 'budgets' | 'prompts' | 'runs' | 'schedules';
export type ApplicationKind = 'workflows' | 'management';
export interface AuthoringDraftRequest {
 workflowId: string; conversationId: string; jobId: string; proposalHash: string; orgId: string; teamId: string;
}
export interface WorkflowPort {
  /** Details-page scope, not the selected node or a run ID. No draft content crosses this contract. */
  readonly assistantContext?: { workflowId: string; name: string; saved: boolean; canApply?: boolean };
  /** Save pending canvas edits before reading Lumi context; never discard local edits. */
  prepareAuthoringDraft?(): Promise<void>;
  /** The owning app submits with its private edit lease; tokens never cross this bridge. */
  applyAuthoringDraft?(request: AuthoringDraftRequest): Promise<void>;
  refreshAuthoringDraft?(workflowId: string): Promise<void>;
  /** Return to the dashboard after resolving dirty edits; false means navigation was blocked. */
  showDashboard?(): Promise<boolean>;
  workflowNavigation: TemplateRef<unknown>;
  toggleNavigation: EventEmitter<void>;
  confirmDiscardChanges(): Promise<boolean>;
  withConfirmedExit(action: () => Promise<void>): Promise<void>;
  openRun(): Promise<void>;
  openSchedules(): Promise<void>;
}
export interface ManagementPort {
  busy: boolean;
  confirmDiscard(): Promise<boolean>;
  withConfirmedExit(action: () => Promise<void>): Promise<void>;
  loadMembers(): Promise<void>;
}

/** Versioned UI contract. No tokens, credentials or feature implementation cross it. */
@Injectable({providedIn:'root'})
export class WorkspaceBridge {
  readonly workflow = signal<WorkflowPort | undefined>(undefined);
  readonly management = signal<ManagementPort | undefined>(undefined);
  readonly toggleNavigation = new EventEmitter<void>();
  host?: {
    selectTeam(id: string): Promise<void>;
    openManagement(section?: ManagementSection): Promise<void>;
  };
}
