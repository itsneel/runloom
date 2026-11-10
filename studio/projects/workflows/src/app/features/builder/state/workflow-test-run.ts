import type { WorkflowApiService } from '../../../core/api/workflow-api.service';
import type {
  PauseGate,
  RunHistory,
  RunStatus,
  WorkflowDefinition,
  WorkflowRunRecord,
} from '../../../core/models/workflow.types';
import type { BuilderFeedback } from './builder-context';

interface WorkflowTestRunContext extends BuilderFeedback {
  readonly definition: WorkflowDefinition;
  readonly isNewWorkflow: boolean;
  readonly documentGeneration: number;
  readonly destroyed: boolean;
  canEdit(): boolean;
  canSaveEditors(): boolean;
}
export class WorkflowTestRun {
  constructor(
    private readonly api: WorkflowApiService,
    private readonly context: WorkflowTestRunContext,
  ) {}

  pollTimer?: ReturnType<typeof setInterval>;
  runOpen = false;
  startingTest = false;
  runInput = '{\n  "employee_name": "Avery",\n  "department": "Engineering"\n}';
  callbackPayload = '{\n  "status": "ready"\n}';
  resumePayload = '{}';
  signalPayload = '{\n  "status": "received"\n}';
  approvalComment = '';
  run?: RunStatus;
  runHistory?: RunHistory;

  get runState(): RunStatus['state'] {
    return this.run?.state;
  }

  get activeApproval(): { step_id: string; prompt: string } | undefined {
    return this.runState?.waiting_approval ?? this.firstValue(this.runState?.waiting_approvals);
  }

  get activeCallback(): { step_id: string; callback_key: string; prompt: string } | undefined {
    return this.runState?.waiting_callback ?? this.firstValue(this.runState?.waiting_callbacks);
  }

  get activePause(): PauseGate | undefined {
    return this.runState?.paused_step ?? this.firstValue(this.runState?.paused_steps);
  }

  readonly handlerPayloads: Record<string, string> = {};
  readonly handlerSending = new Set<string>();
  private readonly handlerRequests = new Map<string, { id: string; payload: string }>();
  get signalHandlers() {
    return Object.values(this.runState?.signal_handlers ?? {});
  }
  async sendHandlerSignal(name: string): Promise<void> {
    if (!this.run || this.runFinished || this.handlerSending.has(name)) return;
    const key = this.run.workflow_id + '/' + name,
      raw = this.handlerPayloads[name] || '{}';
    this.handlerSending.add(name);
    try {
      const payload = JSON.parse(raw);
      const request = this.handlerRequests.get(key);
      const id = request?.payload === raw ? request.id : crypto.randomUUID();
      this.handlerRequests.set(key, { id, payload: raw });
      await this.api.signal(this.run.workflow_id, name, payload, id);
      this.handlerRequests.delete(key);
      this.context.notify('Signal accepted. Check handler status for processing completion.');
      await this.pollRun();
    } catch (error) {
      this.context.handleError(error);
    } finally {
      this.handlerSending.delete(name);
      this.context.markForCheck();
    }
  }
  get activeSignal(): { step_id: string; signal_name: string; prompt: string } | undefined {
    return this.runState?.waiting_signal ?? this.firstValue(this.runState?.waiting_signals);
  }

  get runFinished(): boolean {
    return ['completed', 'failed', 'canceled', 'cancelled', 'terminated', 'timed_out'].includes(
      this.run?.temporal_status ?? '',
    );
  }

  async openRun(): Promise<void> {
    this.runOpen = true;
  }

  async startRun(): Promise<void> {
    if (this.startingTest || !this.context.canEdit() || (!!this.run && !this.runFinished)) return;
    if (!this.context.canSaveEditors()) return;
    if (this.context.isNewWorkflow) {
      this.context.notify(
        'Save this new workflow as a draft once before testing. Publishing is not required.',
        true,
      );
      return;
    }
    this.startingTest = true;
    const generation = this.context.documentGeneration;
    try {
      const input = JSON.parse(this.runInput) as unknown;
      const snapshot = structuredClone(this.context.definition);
      snapshot.version = 0;
      const started = await this.api.testDraft(snapshot, input);
      if (this.context.destroyed || generation !== this.context.documentGeneration) return;
      this.runHistory = undefined;
      this.run = {
        workflow_id: started.workflow_id,
        temporal_status: 'running',
        state: { status: 'starting', is_test: true },
      };
      await this.pollRun();
      this.stopPolling();
      if (!this.context.destroyed && !this.runFinished)
        this.pollTimer = setInterval(() => void this.pollRun(), 1200);
    } catch (error) {
      this.context.handleError(error);
    } finally {
      this.startingTest = false;
      this.context.markForCheck();
    }
  }

  async pollRun(): Promise<void> {
    if (!this.run?.workflow_id) return;
    const workflowId = this.run.workflow_id;
    try {
      const status = await this.api.runStatus(workflowId);
      if (this.context.destroyed || this.run?.workflow_id !== workflowId) return;
      this.run = status;
      await this.loadRunHistory(workflowId);
      if (this.run?.workflow_id === workflowId && this.runFinished) this.stopPolling();
    } catch (error) {
      if (this.context.destroyed || this.run?.workflow_id !== workflowId) return;
      this.stopPolling();
      this.context.handleError(error);
    } finally {
      this.context.markForCheck();
    }
  }

  async decide(approved: boolean): Promise<void> {
    const gate = this.activeApproval;
    if (!this.run || !gate) return;
    try {
      await this.api.approval(this.run.workflow_id, gate.step_id, approved, this.approvalComment);
      this.approvalComment = '';
      await this.pollRun();
    } catch (error) {
      this.context.handleError(error);
    } finally {
      this.context.markForCheck();
    }
  }

  async sendCallback(): Promise<void> {
    const gate = this.activeCallback;
    if (!this.run || !gate) return;
    try {
      await this.api.callback(this.run.workflow_id, gate.step_id, JSON.parse(this.callbackPayload));
      await this.pollRun();
    } catch (error) {
      this.context.handleError(error);
    } finally {
      this.context.markForCheck();
    }
  }

  async resume(): Promise<void> {
    const gate = this.activePause;
    if (!this.run || !gate) return;
    try {
      await this.api.resume(
        this.run.workflow_id,
        gate.step_id,
        gate.budget ? {} : JSON.parse(this.resumePayload),
      );
      await this.pollRun();
    } catch (error) {
      this.context.handleError(error);
    } finally {
      this.context.markForCheck();
    }
  }

  async sendSignal(): Promise<void> {
    const gate = this.activeSignal;
    if (!this.run || !gate) return;
    try {
      await this.api.signal(this.run.workflow_id, gate.signal_name, JSON.parse(this.signalPayload));
      await this.pollRun();
    } catch (error) {
      this.context.handleError(error);
    } finally {
      this.context.markForCheck();
    }
  }

  async cancelRun(): Promise<void> {
    if (!this.run) return;
    try {
      await this.api.cancel(this.run.workflow_id);
      await this.pollRun();
    } catch (error) {
      this.context.handleError(error);
    } finally {
      this.context.markForCheck();
    }
  }

  async openHistoricRun(record: WorkflowRunRecord): Promise<void> {
    this.stopPolling();
    this.runHistory = undefined;
    this.run = {
      workflow_id: record.workflow_id,
      temporal_status: record.temporal_status,
      state: record.state,
    };
    await this.loadRunHistory(record.workflow_id);
    if (
      !this.context.destroyed &&
      this.run?.workflow_id === record.workflow_id &&
      !this.runFinished
    ) {
      await this.pollRun();
      if (
        !this.context.destroyed &&
        this.run?.workflow_id === record.workflow_id &&
        !this.runFinished
      )
        this.pollTimer = setInterval(() => void this.pollRun(), 1200);
    }
    this.context.markForCheck();
  }

  eventLabel(value: string): string {
    return value.replaceAll('.', ' ').replaceAll('_', ' ');
  }

  async openRelatedRun(id: string): Promise<void> {
    this.runOpen = true;
    this.stopPolling();
    this.runHistory = undefined;
    this.run = { workflow_id: id, temporal_status: 'running' };
    await this.pollRun();
    if (!this.context.destroyed && this.run?.workflow_id === id && !this.runFinished)
      this.pollTimer = setInterval(() => void this.pollRun(), 1200);
    this.context.markForCheck();
  }

  async loadRunHistory(workflowId: string): Promise<void> {
    try {
      const history = await this.api.runHistory(workflowId);
      if (this.context.destroyed || this.run?.workflow_id !== workflowId) return;
      this.runHistory = history;
    } catch {
      if (!this.context.destroyed && this.run?.workflow_id === workflowId)
        this.runHistory = undefined;
    } finally {
      this.context.markForCheck();
    }
  }

  stopPolling(): void {
    if (this.pollTimer) clearInterval(this.pollTimer);
    this.pollTimer = undefined;
  }

  firstValue<T>(values: Record<string, T> | undefined): T | undefined {
    return values ? Object.values(values)[0] : undefined;
  }
}
