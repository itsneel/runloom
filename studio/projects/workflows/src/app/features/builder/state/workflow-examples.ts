import type { WorkflowApiService } from '../../../core/api/workflow-api.service';
import type { WorkflowDefinition } from '../../../core/models/workflow.types';
import { parallelExample, parallelExampleInput } from '../model/parallel-example';
import type { BuilderFeedback } from './builder-context';

interface WorkflowExamplesContext extends BuilderFeedback {
  readonly destroyed: boolean;
  readonly generation: number;
  readonly projectId: string;
  busy: boolean;
  canCreate(): boolean;
  confirm(message: string, options: { title: string; confirmText: string }): Promise<boolean>;
  reload(): Promise<void>;
  accept(definition: WorkflowDefinition, input: Record<string, unknown>, projectId: string): void | Promise<void>;
}

export class WorkflowExamples {
  constructor(private readonly api: WorkflowApiService, private readonly context: WorkflowExamplesContext) {}

  async createParallel(): Promise<void> {
    if (this.context.busy || !this.context.canCreate()) return;
    const generation = this.context.generation, projectId = this.context.projectId;
    const current = () => !this.context.destroyed && generation === this.context.generation && projectId === this.context.projectId;
    if (!(await this.context.confirm(
      'Create a demo child workflow and a parent draft in this project? The demo child will be published so both parallel paths can call it. Nothing runs until you choose Test workflow. Only timers and local greetings are used.',
      { title: 'Try parallel child workflows', confirmText: 'Create example' },
    )) || !current() || this.context.busy || !this.context.canCreate()) return;
    this.context.busy = true;
    this.context.markForCheck();
    const sample = parallelExample(crypto.randomUUID());
    try {
      const child = await this.saveNew(sample.child, projectId, true, current);
      if (!child || !current()) return;
      for (const branch of sample.parent.steps[0].branches!) branch.steps[0].version = child.version;
      const parent = await this.saveNew(sample.parent, projectId, false, current);
      if (!parent || !current()) return;
      await this.context.reload();
      if (!current()) return;
      this.context.busy = false;
      await this.context.accept(parent, { ...parallelExampleInput }, projectId);
      if (!this.context.destroyed) this.context.notify('Parallel example ready. Choose Test workflow; both children wait five seconds together, then return their greetings. The parent is still a draft.');
    } catch (error) {
      if (current()) this.context.notify(`${this.context.errorText(error)} Any sample workflows already saved were kept. Refresh the list to inspect them; retry creates new IDs.`, true);
    } finally {
      this.context.busy = false;
      this.context.markForCheck();
    }
  }

  private async saveNew(value: WorkflowDefinition, projectId: string, publish: boolean, current: () => boolean): Promise<WorkflowDefinition | undefined> {
    const token = crypto.randomUUID();
    try {
      await this.api.editLock(value.id, 'POST', token);
      if (!current()) return;
      for (const version of [0, undefined]) {
        try {
          await this.api.getDefinition(value.id, version);
          throw new Error('A workflow with this sample ID already exists. Retry to create fresh sample IDs.');
        } catch (error) { if ((error as { status?: number }).status !== 404) throw error; }
        if (!current()) return;
      }
      if (!current()) return;
      this.api.setEditToken(value.id, token);
      const saved = await this.api.save(value, 'Create parallel child-workflow example', projectId);
      if (!publish || !current()) return saved;
      return await this.api.publish(saved.id, 'Publish demo child for parallel example');
    } finally {
      this.api.setEditToken(value.id);
      await this.api.editLock(value.id, 'DELETE', token).catch(() => undefined);
    }
  }
}
