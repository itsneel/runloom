import type { WorkflowApiService } from '../../../core/api/workflow-api.service';
import type { DefinitionCommit, DefinitionHistory, WorkflowDefinition } from '../../../core/models/workflow.types';
import type { BuilderFeedback } from './builder-context';

interface WorkflowVersionsContext extends BuilderFeedback {
  readonly definition: WorkflowDefinition;
  readonly projectBusy: boolean;
  readonly canEditWorkflow: boolean;
  readonly ownsEdit: boolean;
  readonly documentGeneration: number;
  readonly workflowProjectId: string;
  readonly destroyed: boolean;
  readonly hasUnsavedChanges: boolean;
  draftValidationError: string;
  canSaveEditors(): boolean;
  editSnapshot(): string;
  ensureNewDraftLock(): Promise<boolean>;
  acceptSaved(definition: WorkflowDefinition, snapshot: string, projectId?: string): void;
  loadDefinitions(): Promise<void>;
  checkDraftValidation(definition: WorkflowDefinition): Promise<string>;
  confirmDiscardChanges(): Promise<boolean>;
  confirm(message: string, options: { title: string; confirmText: string; danger: boolean }): Promise<boolean>;
  advanceGeneration(): number;
  canApplyResponse(generation: number, snapshot: string): boolean;
  replaceDefinition(definition: WorkflowDefinition): void;
  selectFirstStep(): void;
  syncJson(): void;
  saveSettled?(): void;
}

// Commit/publish/history orchestration; draft ownership stays with the builder.
export class WorkflowVersions {
  constructor(private readonly api: WorkflowApiService, private readonly context: WorkflowVersionsContext) {}

  versionOpen = false;
  saving = false;
  lastWriteSucceeded = false;
  saveFailed = false;
  definitionHistory?: DefinitionHistory;
  commitMessage = 'Updated workflow';

  async save(automatic = false): Promise<boolean> {
    if (this.saving || this.context.projectBusy || !this.context.canEditWorkflow || !this.context.canSaveEditors()) return false;
    this.saving = true;
    this.lastWriteSucceeded = false;
    this.saveFailed = false;
    const generation = this.context.documentGeneration;
    const submitted = structuredClone(this.context.definition);
    const projectId = this.context.workflowProjectId;
    const snapshot = this.context.editSnapshot();
    try {
      if (!(await this.context.ensureNewDraftLock())) return false;
      const saved = await this.api.save(
        submitted,
        automatic ? 'Autosave draft' : this.commitMessage.trim() || 'Updated workflow',
        projectId,
      );
      if (this.context.destroyed || generation !== this.context.documentGeneration) return false;
      this.context.acceptSaved(saved, snapshot, projectId);
      this.lastWriteSucceeded = true;
      await this.context.loadDefinitions();
      if (this.versionOpen) await this.loadDefinitionHistory();
      const validationError = await this.context.checkDraftValidation(saved);
      if (this.context.destroyed || generation !== this.context.documentGeneration) return false;
      if (!this.context.hasUnsavedChanges) this.context.draftValidationError = validationError;
      if (!automatic) this.context.notify(this.context.hasUnsavedChanges
        ? 'Draft committed. Newer edits are still unsaved.'
        : validationError ? 'Draft saved. Fix the validation errors before publishing or testing.' : 'Draft committed to workflow history.');
      return generation === this.context.documentGeneration && !this.context.destroyed && !this.context.hasUnsavedChanges;
    } catch (error) {
      this.saveFailed = true;
      this.context.handleError(error);
      return false;
    } finally {
      this.saving = false;
      this.context.saveSettled?.();
      this.context.markForCheck();
    }
  }

  async publish(): Promise<void> {
    if (!(await this.save())) return;
    if (this.context.draftValidationError) { this.context.notify('Draft saved, but publishing is blocked: ' + this.context.draftValidationError, true); return; }
    const generation = this.context.documentGeneration;
    const snapshot = this.context.editSnapshot();
    this.saving = true;
    try {
      const published = await this.api.publish(
        this.context.definition.id,
        `Publish: ${this.commitMessage.trim() || 'Updated workflow'}`,
      );
      if (this.context.destroyed || generation !== this.context.documentGeneration) return;
      this.context.acceptSaved(published, snapshot);
      await this.context.loadDefinitions();
      if (this.versionOpen) await this.loadDefinitionHistory();
      this.commitMessage = 'Updated workflow';
      this.context.notify(`Version ${published.version} is published.${this.context.hasUnsavedChanges ? ' Newer edits are still unsaved.' : ''}`);
    } catch (error) {
      this.context.handleError(error);
    } finally {
      this.saving = false;
      this.context.markForCheck();
    }
  }

  async openVersions(): Promise<void> {
    this.versionOpen = true;
    await this.loadDefinitionHistory();
  }

  async restoreCommit(commit: DefinitionCommit): Promise<void> {
    if (!this.context.ownsEdit || !this.context.canEditWorkflow) return;
    if (!(await this.context.confirm(`${this.context.hasUnsavedChanges ? 'Discard your unsaved workflow changes? ' : ''}Restore commit #${commit.sequence} as a new draft? Published versions will not change.`, { title: 'Restore workflow draft?', confirmText: 'Restore draft', danger: true }))) {
      return;
    }
    if (!this.context.ownsEdit || !this.context.canEditWorkflow) return;
    const generation = this.context.advanceGeneration();
    const snapshot = this.context.editSnapshot();
    try {
      const definition = await this.api.restoreCommit(
        this.context.definition.id,
        commit.commit_id,
        `Restore commit #${commit.sequence}: ${commit.message}`,
      );
      if (!this.context.canApplyResponse(generation, snapshot)) return;
      this.context.replaceDefinition(definition);
      this.context.selectFirstStep();
      this.context.syncJson();
      await Promise.all([this.context.loadDefinitions(), this.loadDefinitionHistory()]);
      this.context.notify(`Commit #${commit.sequence} restored as a new draft.`);
    } catch (error) {
      this.context.handleError(error);
    } finally {
      this.context.markForCheck();
    }
  }

  async openPublishedVersion(version: number): Promise<void> {
    if (!(await this.context.confirmDiscardChanges())) return;
    const generation = this.context.advanceGeneration();
    const snapshot = this.context.editSnapshot();
    try {
      const definition = await this.api.getDefinition(this.context.definition.id, version);
      if (!this.context.canApplyResponse(generation, snapshot)) return;
      this.context.replaceDefinition(definition);
      this.context.selectFirstStep();
      this.context.syncJson();
      this.versionOpen = false;
      this.context.notify(`Published version ${version} opened. Editing it creates a new draft.`);
    } catch (error) {
      this.context.handleError(error);
    } finally {
      this.context.markForCheck();
    }
  }

  commitKindLabel(commit: DefinitionCommit): string {
    if (commit.kind === 'publish') return `Published v${commit.published_version}`;
    if (commit.kind === 'restore') return 'Restored snapshot';
    return 'Draft commit';
  }

  async loadDefinitionHistory(): Promise<void> {
    try {
      this.definitionHistory = await this.api.definitionHistory(this.context.definition.id);
    } catch (error) {
      this.definitionHistory = undefined;
      this.context.handleError(error);
    } finally {
      this.context.markForCheck();
    }
  }
}
