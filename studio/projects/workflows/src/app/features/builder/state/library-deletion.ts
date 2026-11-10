import type { WorkflowApiService } from '../../../core/api/workflow-api.service';
import type { DefinitionSummary, WorkflowProject } from '../../../core/models/workflow.types';

interface LibraryDeletionContext {
  readonly canDelete: boolean;
  readonly destroyed: boolean;
  readonly generation: number;
  readonly projects: WorkflowProject[];
  readonly workflows: DefinitionSummary[];
  busy: boolean;
  confirm(message: string, options: {title: string; confirmText: string; danger: boolean}): Promise<boolean>;
  reload(): Promise<void>;
  projectDeleted(id: string): void;
  notify(message: string, error?: boolean): void;
  handleError(error: unknown): void;
  markForCheck(): void;
}

// Dashboard actions own no editor state; confirmed operations are fenced against
// navigation and component destruction. Eligibility is rechecked by the API.
export class LibraryDeletion {
  constructor(private readonly api: WorkflowApiService, private readonly context: LibraryDeletionContext) {}

  async project(id: string): Promise<void> {
    const project = this.context.projects.find(item => item.id === id);
    if (!project || id === 'default' || this.context.workflows.some(item => (item.project_id ?? 'default') === id)) return;
    await this.remove(`Delete project “${project.name}”? Only empty projects can be deleted.`, 'Delete project',
      () => this.api.deleteProject(id), () => this.context.projectDeleted(id), `Project “${project.name}” deleted.`);
  }

  async workflow(id: string): Promise<void> {
    const workflow = this.context.workflows.find(item => item.id === id);
    if (!workflow?.has_draft || workflow.latest_version > 0 || workflow.published_versions?.length) return;
    await this.remove(`Delete draft workflow “${workflow.name}”? It will leave the workflow library. Commit, run and audit history are retained. Drafts referencing it will need updating.`, 'Delete workflow',
      () => this.api.deleteDraftWorkflow(id), () => {}, `Draft workflow “${workflow.name}” deleted.`);
  }

  private async remove(message: string, title: string, request: () => Promise<void>, accepted: () => void, success: string): Promise<void> {
    if (!this.context.canDelete || this.context.busy || this.context.destroyed) return;
    let generation = this.context.generation;
    const current = () => !this.context.destroyed && generation === this.context.generation;
    this.context.busy = true;
    try {
      if (!(await this.context.confirm(message, {title: `${title}?`, confirmText: title, danger: true})) || !current() || !this.context.canDelete) return;
      await request();
      if (!current()) return;
      accepted();
      generation = this.context.generation;
      await this.context.reload();
      if (current()) this.context.notify(success);
    } catch (error) {
      if (current()) this.context.handleError(error);
    } finally {
      this.context.busy = false;
      if (!this.context.destroyed) this.context.markForCheck();
    }
  }
}
