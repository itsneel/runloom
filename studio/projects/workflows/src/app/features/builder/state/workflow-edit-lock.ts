import type { EditLockStatus, WorkflowApiService } from '../../../core/api/workflow-api.service';
import type { WorkflowDefinition } from '../../../core/models/workflow.types';
import type { BuilderFeedback } from './builder-context';

interface EditLockContext extends BuilderFeedback {
  readonly definition: WorkflowDefinition;
  readonly isNewWorkflow: boolean;
  readonly saving: boolean;
  readonly projectBusy: boolean;
  readonly documentGeneration: number;
  readonly destroyed: boolean;
  readonly builderOpen: boolean;
  canEdit(): boolean;
  confirmDiscardChanges(): Promise<boolean>;
  replaceDefinition(definition: WorkflowDefinition, saved: boolean): void;
  openDefinition(id: string, discardConfirmed: boolean): Promise<void>;
}

export class WorkflowEditLock {
  constructor(private readonly api: WorkflowApiService, private readonly context: EditLockContext) {}
  start(): void { this.lockTimer = setInterval(() => void this.refreshEditLock(), 20000); }
  dispose(): void { clearInterval(this.lockTimer); void this.releaseEditing(); }

  editLock?: EditLockStatus;
  acquiringEdit = false;
  editingId = '';
  editingToken = '';
  editUntil = 0;
  editLost = false;
  lockTimer?: ReturnType<typeof setInterval>;
  renewingEdit = false;
  get ownsEdit(): boolean { return this.editingId === this.context.definition.id && !!this.editingToken && Date.now() < this.editUntil; }
  get canEditWorkflow(): boolean {
    return this.context.canEdit() && !this.acquiringEdit && !this.editLost && (this.ownsEdit || (this.context.isNewWorkflow && !this.editingToken));
  }
  get editMessage(): string {
    if (this.editLost || (this.editingToken && !this.ownsEdit)) return 'Editing connection lost. Your unsaved changes are kept. Copy them from JSON before reloading to edit.';
    if (this.ownsEdit) return 'You are editing. Other sessions have read-only access.';
    if (this.editLock?.locked) return `${this.editLock.owner_name || 'Another team member'} is editing this workflow. Read-only until they finish.`;
    return 'Read-only preview. Choose Edit workflow to reserve it for your session.';
  }
  get editingInterrupted(): boolean { return this.editLost || (!!this.editingToken && !this.ownsEdit); }
  get editHeading(): string {
    if (this.editingInterrupted) return 'Editing paused';
    if (this.ownsEdit) return 'Editing reserved for you';
    if (this.editLock?.locked) return 'Another session is editing';
    return 'Preview mode';
  }

  async releaseEditing(): Promise<void> {
    const id = this.editingId, token = this.editingToken;
    this.editingId = ''; this.editingToken = ''; this.editUntil = 0;
    this.context.markForCheck();
    if (!id || !token) return;
    this.api.setEditToken(id);
    try { await this.api.editLock(id, 'DELETE', token); } catch { /* Lease expires if the network is unavailable. */ }
  }

  async editWorkflow(): Promise<void> {
    if (!this.context.canEdit() || this.acquiringEdit || this.context.saving || this.context.projectBusy || !(await this.context.confirmDiscardChanges())) return;
    this.acquiringEdit = true;
    const id = this.context.definition.id, generation = this.context.documentGeneration, token = crypto.randomUUID();
    await this.releaseEditing();
    try {
      const started = Date.now();
      const lock = await this.api.editLock(id, 'POST', token);
      const latest = await this.api.getDefinition(id, 0);
      if (this.context.destroyed || generation !== this.context.documentGeneration) {
        await this.api.editLock(id, 'DELETE', token); return;
      }
      this.context.replaceDefinition(latest, true);
      this.editingId = id; this.editingToken = token; this.editLock = lock;
      this.editUntil = started + Math.min(lock.ttl_seconds * 1000, 60000);
      this.editLost = false;
      this.api.setEditToken(id, token);
      this.context.notify('Latest draft loaded. You have exclusive editing access.');
    } catch (error) {
      await this.api.editLock(id, 'DELETE', token).catch(() => undefined);
      this.context.handleError(error);
      await this.refreshEditLock();
    } finally { this.acquiringEdit = false; this.context.markForCheck(); }
  }

  async finishEditing(): Promise<void> {
    if (this.context.saving || this.context.projectBusy || !(await this.context.confirmDiscardChanges())) return;
    await this.releaseEditing();
    this.editLost = false;
    // Reload after an explicit discard so a stale draft cannot be edited later.
    await this.context.openDefinition(this.context.definition.id, true);
  }

  async ensureNewDraftLock(): Promise<boolean> {
    if (this.ownsEdit) return true;
    if (!this.canEditWorkflow || !this.context.isNewWorkflow) return false;
    const id = this.context.definition.id, generation = this.context.documentGeneration, token = crypto.randomUUID();
    const started = Date.now();
    this.acquiringEdit = true;
    try {
      const lock = await this.api.editLock(id, 'POST', token);
      // A name collision must not overwrite a workflow created since list load.
      let exists = false;
      try { await this.api.getDefinition(id, 0); exists = true; }
      catch (error) { if ((error as {status?: number}).status !== 404) throw error; }
      if (exists) throw new Error('A workflow with this ID already exists. Open its latest draft before editing.');
      if (this.context.destroyed || generation !== this.context.documentGeneration || id !== this.context.definition.id) {
        await this.api.editLock(id, 'DELETE', token); return false;
      }
      this.editingId=id; this.editingToken=token; this.editLock=lock;
      this.editUntil=started + Math.min(lock.ttl_seconds * 1000, 60000);
      this.api.setEditToken(id,token);
      return this.ownsEdit;
    } catch (error) {
      await this.api.editLock(id,'DELETE',token).catch(() => undefined);
      this.context.handleError(error); return false;
    } finally { this.acquiringEdit=false; this.context.markForCheck(); }
  }

  async refreshEditLock(): Promise<void> {
    if (this.context.destroyed || !this.context.builderOpen || this.acquiringEdit || this.renewingEdit) return;
    const id=this.context.definition.id, token=this.editingToken, generation=this.context.documentGeneration;
    this.renewingEdit=true;
    const started=Date.now();
    try {
      const lock = await this.api.editLock(id, token && !this.editLost ? 'PUT' : 'GET', token);
      if (this.context.destroyed || generation!==this.context.documentGeneration || token!==this.editingToken) return;
      this.editLock=lock;
      if (token && !this.editLost) this.editUntil=started + Math.min(lock.ttl_seconds * 1000,60000);
    } catch (error) {
      if (generation===this.context.documentGeneration && token===this.editingToken && token) {
        this.editUntil=0; this.editLost=true;
        this.context.notify('Editing lock could not be renewed. Your changes are kept, but saving is blocked. Copy JSON before reloading.',true);
      }
    } finally { this.renewingEdit=false; this.context.markForCheck(); }
  }
}
