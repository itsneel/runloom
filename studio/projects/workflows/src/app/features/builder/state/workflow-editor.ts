import type {
  DefinitionSummary,
  StepType,
  WorkflowDefinition,
  WorkflowStep,
} from '../../../core/models/workflow.types';
import {
  allSteps,
  rootPaths,
  availableID,
  containsPath,
  decisionPath,
  duplicateTree,
  parallelPath,
} from '../model/workflow-tree';
import type { StepLocation } from '../model/workflow-tree';
import { makeStep, slug } from '../model/workflow-defaults';
import { workflowPalette, PaletteItem } from '../model/node-presentation';
import type { BuilderFeedback } from './builder-context';

interface WorkflowEditorContext extends BuilderFeedback {
  readonly definition: WorkflowDefinition;
  readonly canEditWorkflow: boolean;
  readonly saving: boolean;
  readonly documentGeneration: number;
  readonly definitions: DefinitionSummary[];
  readonly publishedChildren: DefinitionSummary[];
  readonly webhookUrl: string;
  markChanged(): void;
  confirm(
    message: string,
    options: { title: string; confirmText: string; danger: boolean },
  ): Promise<boolean>;
}

// Owns canvas selection and unfinished node fields, never a second workflow copy.
export class WorkflowEditor {
  constructor(private readonly context: WorkflowEditorContext) {}

  nodeInsertIndex = 0;
  nodeInsertSteps?: WorkflowStep[];
  selectedContainer?: WorkflowStep[];
  get selectedSteps(): WorkflowStep[] {
    return this.selectedContainer ?? this.context.definition.steps;
  }
  get rootSteps(): WorkflowStep[] {
    return rootPaths(this.context.definition).flat();
  }
  get nodeCount(): number {
    return allSteps(this.rootSteps).length + (this.context.definition.signal_handlers?.length || 0);
  }
  readonly emptyPositions = {};
  updateCanvasLayout(positions: Record<string, { x: number; y: number }>): void {
    if (!this.context.canEditWorkflow || this.context.saving) return;
    if (Object.keys(positions).length) this.context.definition.layout = { positions };
    else delete this.context.definition.layout;
    this.context.markChanged();
  }
  readonly stepDataDrafts = new Map<WorkflowStep, string>();
  readonly schemaDrafts = new Map<WorkflowStep | WorkflowDefinition, string>();
  schemaText(target: WorkflowStep | WorkflowDefinition): string {
    return (
      this.schemaDrafts.get(target) ??
      this.objectText('steps' in target ? target.input_schema : target.output_schema)
    );
  }
  setSchema(target: WorkflowStep | WorkflowDefinition, raw: string): void {
    if (!this.context.canEditWorkflow || this.context.saving) return;
    try {
      const value: unknown = JSON.parse(raw);
      if (!value || typeof value !== 'object' || Array.isArray(value))
        throw new Error('Schema must be an object');
      if ('steps' in target) target.input_schema = value as Record<string, unknown>;
      else if (Object.keys(value).length) target.output_schema = value as Record<string, unknown>;
      else delete target.output_schema;
      this.schemaDrafts.delete(target);
      this.context.markChanged();
    } catch {
      this.schemaDrafts.set(target, raw);
      this.context.notify(
        'Schema must be a valid JSON object. Fix it before saving or testing.',
        true,
      );
    }
  }
  readonly emailRecipientDrafts = new WeakMap<
    WorkflowStep,
    Partial<Record<'to' | 'cc' | 'bcc', string>>
  >();
  selectedIndex = -1;
  inspectorOpen = true;
  nodePickerOpen = false;
  nodeSearch = '';

  get selectedStep(): WorkflowStep | undefined {
    return this.selectedSteps[this.selectedIndex];
  }

  get filteredPalette(): PaletteItem[] {
    const query = this.nodeSearch.trim().toLowerCase();
    if (!query) return workflowPalette;
    return workflowPalette.filter((item) =>
      `${item.label} ${item.summary} ${item.type}`.toLowerCase().includes(query),
    );
  }

  addStep(type: StepType): void {
    if (!this.context.canEditWorkflow) return;
    const steps = this.nodeInsertSteps ?? this.context.definition.steps;
    if (
      steps !== this.context.definition.steps &&
      !rootPaths(this.context.definition).some(
        (root) => root === steps || root.some((step) => containsPath(step, steps)),
      )
    ) {
      this.nodePickerOpen = false;
      this.context.notify('The workflow changed. Choose the target path again.', true);
      return;
    }
    if (
      type === 'end' &&
      (parallelPath(this.rootSteps, steps) || this.nodeInsertIndex !== steps.length)
    ) {
      this.context.notify(
        'Place End last in a path, outside parallel branches and AI Decision action paths.',
        true,
      );
      return;
    }
    if (type === 'fail' && this.nodeInsertIndex !== steps.length) {
      this.context.notify('Place Fail last in its path.', true);
      return;
    }
    if (type === 'decision' && decisionPath(this.rootSteps, steps)) {
      this.context.notify(
        'AI Decision nodes cannot be nested inside another AI Decision action path.',
        true,
      );
      return;
    }
    if (['end', 'fail'].includes(steps[this.nodeInsertIndex - 1]?.type)) {
      this.context.notify('No nodes can follow End or Fail.', true);
      return;
    }
    const step = duplicateTree(
      makeStep(type, this.context.definition, this.context.publishedChildren),
      this.rootSteps,
      false,
    );
    if (
      type === 'end' &&
      (this.context.definition.signal_handlers ?? []).some(
        (handler) =>
          handler.steps === steps || handler.steps.some((node) => containsPath(node, steps)),
      )
    ) {
      step.name = 'Return from handler';
      step.message = 'Return from signal handler';
    }
    steps.splice(this.nodeInsertIndex, 0, step);
    this.selectedContainer = steps;
    this.selectedIndex = this.nodeInsertIndex;
    this.inspectorOpen = true;
    this.nodePickerOpen = false;
    this.nodeSearch = '';
    this.context.markChanged();
  }

  openNodePicker(
    insertIndex = this.context.definition.steps.length,
    steps = this.context.definition.steps,
  ): void {
    if (!this.context.canEditWorkflow) return;
    this.nodeInsertIndex = insertIndex;
    this.nodeInsertSteps = steps;
    this.nodePickerOpen = true;
    this.inspectorOpen = false;
  }

  selectHandler(): void {
    this.selectedContainer = undefined;
    this.selectedIndex = -1;
    this.inspectorOpen = false;
    this.nodePickerOpen = false;
  }
  selectTrigger(): void {
    this.selectedContainer = undefined;
    this.selectedIndex = -1;
    this.inspectorOpen = true;
    this.nodePickerOpen = false;
  }

  setTriggerType(type: 'manual' | 'webhook'): void {
    if (this.context.definition.kind === 'child') return;
    this.context.definition.trigger =
      type === 'manual'
        ? { type: 'manual' }
        : {
            type: 'webhook',
            path: `${this.context.definition.id}-hook`.slice(0, 64),
            method: 'POST',
          };
    this.context.markChanged();
  }

  async copyWebhookUrl(): Promise<void> {
    if (!this.context.webhookUrl) return;
    await navigator.clipboard.writeText(this.context.webhookUrl);
    this.context.notify('Webhook URL copied.');
  }

  selectStep(index: number, steps = this.context.definition.steps): void {
    this.selectedContainer = steps;
    this.selectedIndex = index;
    this.inspectorOpen = true;
  }

  duplicateStep(index: number, steps = this.context.definition.steps): void {
    if (!this.context.canEditWorkflow) return;
    if (['end', 'fail'].includes(steps[index].type)) {
      this.context.notify('A path can only end once.', true);
      return;
    }
    const copy = duplicateTree(steps[index], this.rootSteps);
    steps.splice(index + 1, 0, copy);
    this.selectedContainer = steps;
    this.selectedIndex = index + 1;
    this.context.markChanged();
  }

  removeStep(index: number, steps = this.context.definition.steps): void {
    if (!this.context.canEditWorkflow) return;
    for (const child of allSteps([steps[index]])) {
      this.stepDataDrafts.delete(child);
      this.schemaDrafts.delete(child);
    }
    steps.splice(index, 1);
    this.selectedContainer = steps;
    this.selectedIndex = Math.min(index, steps.length - 1);
    if (!steps.length) this.inspectorOpen = false;
    this.context.markChanged();
  }

  moveStep(index: number, offset: number): void {
    const steps = this.selectedSteps;
    if (index + offset < 0 || index + offset >= steps.length) return;
    this.moveGraphNode({
      from: { steps, index },
      to: { steps, index: index + offset + (offset > 0 ? 1 : 0) },
    });
  }

  moveGraphNode({ from, to }: { from: StepLocation; to: StepLocation }): void {
    if (!this.context.canEditWorkflow) return;
    const step = from.steps[from.index];
    if (!step || containsPath(step, to.steps)) {
      this.context.notify('A node cannot be placed inside its own branch.', true);
      return;
    }
    const destination = to.index - (from.steps === to.steps && from.index < to.index ? 1 : 0);
    const target = to.steps.filter((_, index) => from.steps !== to.steps || index !== from.index);
    if (
      allSteps([step]).some((node) => node.type === 'decision') &&
      decisionPath(this.rootSteps, to.steps)
    ) {
      this.context.notify(
        'AI Decision nodes cannot be nested inside another AI Decision action path.',
        true,
      );
      return;
    }
    if (
      ['end', 'fail'].includes(target[destination - 1]?.type) ||
      (allSteps([step]).some((node) => node.type === 'end') &&
        parallelPath(this.rootSteps, to.steps)) ||
      (['end', 'fail'].includes(step.type) && destination !== target.length)
    ) {
      this.context.notify(
        'Place End last in a path, outside parallel branches and AI Decision action paths.',
        true,
      );
      return;
    }
    from.steps.splice(from.index, 1);
    to.steps.splice(destination, 0, step);
    this.selectStep(destination, to.steps);
    this.context.markChanged();
  }

  addParallelBranch(step: WorkflowStep): void {
    if (
      !this.context.canEditWorkflow ||
      !['parallel', 'decision'].includes(step.type) ||
      (step.branches?.length ?? 0) >= (step.type === 'decision' ? 20 : 50)
    )
      return;
    step.branches ??= [];
    const id = availableID('path', new Set(step.branches.map((branch) => branch.id)));
    step.branches.push(
      step.type === 'decision' ? { id, description: '', steps: [] } : { id, steps: [] },
    );
    this.context.markChanged();
  }

  renameBranch(step: WorkflowStep, index: number, value: string): void {
    if (!this.context.canEditWorkflow || !step.branches) return;
    step.branches[index].id = availableID(
      slug(value),
      new Set(step.branches.filter((_, i) => i !== index).map((branch) => branch.id)),
    );
    this.context.markChanged();
  }

  async removeBranch(step: WorkflowStep, index: number): Promise<void> {
    if (
      !this.context.canEditWorkflow ||
      !step.branches ||
      step.branches.length <= (step.type === 'decision' ? 1 : 2)
    )
      return;
    const branch = step.branches[index],
      generation = this.context.documentGeneration;
    if (
      branch.steps.length &&
      !(await this.context.confirm('Remove this path and all its nodes?', {
        title: 'Remove path?',
        confirmText: 'Remove path',
        danger: true,
      }))
    )
      return;
    if (
      !this.context.canEditWorkflow ||
      generation !== this.context.documentGeneration ||
      step.branches[index] !== branch
    )
      return;
    for (const child of allSteps(branch.steps)) {
      this.stepDataDrafts.delete(child);
      this.schemaDrafts.delete(child);
    }
    step.branches.splice(index, 1);
    this.context.markChanged();
  }

  conditionText(value: unknown): string {
    return typeof value === 'string' ? value : (JSON.stringify(value) ?? '');
  }
  setDecisionMode(step: WorkflowStep, mode: 'single' | 'multiple' | 'agent'): void {
    if (!this.context.canEditWorkflow) return;
    step.decision = {
      mode,
      execution: 'sequential',
      max_rounds: mode === 'agent' ? 5 : 1,
      max_actions: mode === 'single' ? 1 : 10,
    };
    this.context.markChanged();
  }
  setConditionValue(step: WorkflowStep, field: 'left' | 'right', raw: string): void {
    if (!this.context.canEditWorkflow) return;
    step.if ??= { left: '', operator: 'equals', right: '' };
    try {
      step.if[field] = JSON.parse(raw);
    } catch {
      step.if[field] = raw;
    }
    this.context.markChanged();
  }
  setConditionOperator(step: WorkflowStep, operator: string): void {
    if (!this.context.canEditWorkflow) return;
    step.if ??= { left: '', operator: 'equals', right: '' };
    step.if.operator = operator;
    this.context.markChanged();
  }

  updateMetadata(): void {
    this.context.definition.id = slug(this.context.definition.id);
    this.context.markChanged();
  }

  updateStep(): void {
    if (!this.selectedStep) return;
    this.selectedStep.id = this.uniqueId(slug(this.selectedStep.id), this.selectedStep);
    this.context.markChanged();
  }

  setOptionalNumber(field: 'timeout_seconds', raw: string): void {
    if (!this.selectedStep) return;
    this.selectedStep[field] = raw === '' ? undefined : Number(raw);
    this.context.markChanged();
  }

  setObjectField(field: 'with', raw: string): void {
    if (!this.selectedStep) return;
    try {
      const value = JSON.parse(raw);
      if (!value || typeof value !== 'object' || Array.isArray(value)) {
        throw new Error('Input data must be a JSON object.');
      }
      this.selectedStep[field] = value as Record<string, unknown>;
      this.stepDataDrafts.delete(this.selectedStep);
      this.context.markChanged();
      this.context.notify('Step data updated.');
    } catch (error) {
      this.stepDataDrafts.set(this.selectedStep, raw);
      this.context.notify(`That data is not valid JSON: ${this.context.errorText(error)}`, true);
    }
  }

  setHTTPData(raw: string): void {
    const step = this.selectedStep;
    if (!step) return;
    try {
      const value = JSON.parse(raw);
      if (
        !value ||
        typeof value !== 'object' ||
        Array.isArray(value) ||
        typeof value.url !== 'string' ||
        typeof value.method !== 'string'
      )
        throw new Error('API configuration needs a URL and method.');
      step.http = value;
      this.stepDataDrafts.delete(step);
      this.context.markChanged();
    } catch {
      this.stepDataDrafts.set(step, raw);
      this.context.notify('API configuration must be valid JSON with a URL and method.', true);
    }
  }

  httpDataText(step: WorkflowStep): string {
    return this.stepDataDrafts.get(step) ?? this.objectText(step.http);
  }

  databaseDraft(step: WorkflowStep): string | null {
    return this.stepDataDrafts.get(step) ?? null;
  }

  setDatabaseDraft(step: WorkflowStep, raw: string | null): void {
    if (raw !== null) this.stepDataDrafts.set(step, raw);
    else this.stepDataDrafts.delete(step);
    this.context.markForCheck();
  }

  setAPISecret(name: string): void {
    const h = this.selectedStep?.http;
    if (!h) return;
    h.secret_headers ??= {};
    if (name) h.secret_headers['Authorization'] = { secret: name, prefix: 'Bearer ' };
    else delete h.secret_headers['Authorization'];
    this.context.markChanged();
  }

  setEmailRecipients(field: 'to' | 'cc' | 'bcc', value: string): void {
    if (!this.context.canEditWorkflow || !this.selectedStep?.email) return;
    const drafts = this.emailRecipientDrafts.get(this.selectedStep) || {};
    drafts[field] = value;
    this.emailRecipientDrafts.set(this.selectedStep, drafts);
    this.selectedStep.email[field] = value
      .split(/[,\n]/)
      .map((address) => address.trim())
      .filter(Boolean);
    this.context.markChanged();
  }

  emailRecipientsText(step: WorkflowStep, field: 'to' | 'cc' | 'bcc'): string {
    return this.emailRecipientDrafts.get(step)?.[field] ?? (step.email?.[field] || []).join('\n');
  }

  stepDataText(step: WorkflowStep): string {
    return this.stepDataDrafts.get(step) ?? this.objectText(step.with);
  }

  objectText(value: unknown): string {
    return JSON.stringify(value ?? {}, null, 2);
  }

  setRetryAttempts(step: WorkflowStep, attempts: number): void {
    if (!this.context.canEditWorkflow) return;
    step.retry = { ...step.retry, maximum_attempts: attempts };
    this.context.markChanged();
  }
  nodeConfigurationReady(step: WorkflowStep): boolean {
    switch (step.type) {
      case 'http':
        return !!step.http;
      case 'database':
        return !!step.database;
      case 'integration':
        return (
          !!step.integration &&
          typeof step.integration.parameters === 'object' &&
          step.integration.parameters !== null &&
          !Array.isArray(step.integration.parameters)
        );
      case 'transform':
        return (
          !!step.data &&
          Array.isArray(step.data.fields) &&
          step.data.fields.every((field) => field !== null && typeof field === 'object')
        );
      case 'collection':
      case 'merge':
        return !!step.data && typeof step.data === 'object';
      case 'foreach':
        return (
          !!step.foreach &&
          Array.isArray(step.branches) &&
          step.branches.length === 1 &&
          Array.isArray(step.branches[0]?.steps)
        );
      case 'llm':
        return (
          !step.llm_task ||
          (typeof step.llm_task === 'object' &&
            (step.llm_task.labels === undefined ||
              (Array.isArray(step.llm_task.labels) &&
                step.llm_task.labels.every((label) => typeof label === 'string'))) &&
            (step.llm_task.fields === undefined ||
              (Array.isArray(step.llm_task.fields) &&
                step.llm_task.fields.every(
                  (field) => field !== null && typeof field === 'object',
                ))))
        );
      case 'email':
        return !!step.email;
      case 'code':
        return !!step.code;
      case 'condition':
        return !!step.if;
      case 'decision':
        return !!step.decision;
      default:
        return true;
    }
  }

  setWorkflowKind(kind: 'main' | 'child'): void {
    this.context.definition.kind = kind;
    if (kind === 'child') this.context.definition.trigger = { type: 'manual' };
    this.context.markChanged();
  }

  selectSubworkflow(id: string): void {
    if (!this.selectedStep) return;
    this.selectedStep.definition_id = id;
    this.selectedStep.version =
      this.context.definitions.find((item) => item.id === id)?.latest_version || 1;
    this.context.markChanged();
  }

  uniqueId(candidate: string, ignore?: WorkflowStep): string {
    return availableID(
      candidate,
      new Set([
        ...allSteps(this.rootSteps)
          .filter((step) => step !== ignore)
          .map((step) => step.id),
        ...(this.context.definition.signal_handlers ?? []).map((handler) => handler.id),
      ]),
    );
  }
}
