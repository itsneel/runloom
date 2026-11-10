import type { AuthoringDraftRequest } from '@runloom/studio-platform';
import { HttpClient } from '@angular/common/http';
import { inject, Injectable } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import type { PromptEntry, PromptTemplate, VariableHint } from '../models/prompt.types';
import type { NodeCatalog } from '../models/integration.types';
import { AuthService } from '@runloom/studio-platform';
export interface EditLockStatus {
  locked: boolean;
  owner?: string;
  owner_name?: string;
  expires_at?: string;
  writing: boolean;
  ttl_seconds: number;
}
import {
  DefinitionSummary,
  DefinitionHistory,
  RunStatus,
  RunHistory,
  WorkflowRunRecord,
  WorkflowDefinition,
  WorkflowProject,
} from '../models/workflow.types';

@Injectable()
export class WorkflowApiService {
  deleteProject(id: string): Promise<void> {
    return firstValueFrom(
      this.http.delete<void>(`/api/v1/studio/projects/${encodeURIComponent(id)}`),
    );
  }
  deleteDraftWorkflow(id: string): Promise<void> {
    return firstValueFrom(
      this.http.delete<void>(`/api/v1/studio/definitions/${encodeURIComponent(id)}`, {
        headers: this.editHeaders(id),
      }),
    );
  }
  nodeCatalog(): Promise<NodeCatalog> {
    return firstValueFrom(this.http.get<NodeCatalog>('/api/v1/studio/catalog'));
  }
  prompts(): Promise<PromptEntry[]> {
    return firstValueFrom(this.http.get<PromptEntry[]>('/api/v1/studio/prompts'));
  }
  prompt(id: string, version = 0): Promise<PromptEntry> {
    return firstValueFrom(
      this.http.get<PromptEntry>(
        `/api/v1/studio/prompts/${encodeURIComponent(id)}?version=${version}`,
      ),
    );
  }
  promptHistory(id: string): Promise<PromptEntry[]> {
    return firstValueFrom(
      this.http.get<PromptEntry[]>(`/api/v1/studio/prompts/${encodeURIComponent(id)}/history`),
    );
  }
  savePrompt(entry: Partial<PromptEntry>, publish: boolean): Promise<PromptEntry> {
    return firstValueFrom(
      this.http.post<PromptEntry>(
        `/api/v1/studio/prompts/${encodeURIComponent(entry.id!)}/${publish ? 'publish' : 'save'}`,
        entry,
      ),
    );
  }
  previewPrompt(template: PromptTemplate, inputs: Record<string, unknown>): Promise<unknown> {
    return firstValueFrom(this.http.post('/api/v1/studio/prompts/preview', { template, inputs }));
  }
  testPrompt(
    definition_id: string,
    template: PromptTemplate,
    inputs: Record<string, unknown>,
    model?: string,
  ): Promise<{ workflow_id: string }> {
    return firstValueFrom(
      this.http.post<{ workflow_id: string }>('/api/v1/studio/prompts/test', {
        definition_id,
        template,
        inputs,
        model,
      }),
    );
  }
  variables(definition: WorkflowDefinition, step_id: string): Promise<VariableHint[]> {
    return firstValueFrom(
      this.http.post<VariableHint[]>('/api/v1/studio/variables', { definition, step_id }),
    );
  }
  private readonly editTokens = new Map<string, string>();
  setEditToken(id: string, token?: string): void {
    if (token) this.editTokens.set(id, token);
    else this.editTokens.delete(id);
  }
  private editHeaders(id: string): Record<string, string> {
    const token = this.editTokens.get(id);
    return token ? { 'X-Runloom-Edit-Token': token } : {};
  }
  editLock(id: string, method = 'GET', token = ''): Promise<EditLockStatus> {
    return firstValueFrom(
      this.client.request<EditLockStatus>(
        method,
        `/api/v1/studio/definitions/${encodeURIComponent(id)}/edit-lock`,
        {
          headers: {
            'X-Runloom-Team': this.team,
            ...(token ? { 'X-Runloom-Edit-Token': token } : {}),
          },
        },
      ),
    );
  }
  private readonly client = inject(HttpClient);
  // A studio instance stays bound to its original team, including async chains
  // which finish after the user switches to a different workspace.
  private readonly team = inject(AuthService).teamId();
  private readonly http = {
    get: <T>(url: string) => this.client.get<T>(url, { headers: { 'X-Runloom-Team': this.team } }),
    delete: <T>(url: string, options?: { headers?: Record<string, string> }) =>
      this.client.delete<T>(url, { headers: { ...options?.headers, 'X-Runloom-Team': this.team } }),
    post: <T>(url: string, body: unknown, options?: { headers?: Record<string, string> }) =>
      this.client.post<T>(url, body, {
        headers: { ...options?.headers, 'X-Runloom-Team': this.team },
      }),
  };

  health(): Promise<{
    status: string;
    temporal: boolean;
    database?: boolean;
    database_driver?: string;
  }> {
    return firstValueFrom(
      this.http.get<{
        status: string;
        temporal: boolean;
        database?: boolean;
        database_driver?: string;
      }>('/api/v1/health'),
    );
  }

  listDefinitions(): Promise<DefinitionSummary[]> {
    return firstValueFrom(this.http.get<DefinitionSummary[]>('/api/v1/studio/definitions'));
  }

  testCode(
    code: { runtime: string; source: string },
    input: Record<string, unknown>,
  ): Promise<{ output: unknown }> {
    return firstValueFrom(
      this.http.post<{ output: unknown }>('/api/v1/studio/code/test', { code, input }),
    );
  }

  listProjects(): Promise<WorkflowProject[]> {
    return firstValueFrom(this.http.get<WorkflowProject[]>('/api/v1/studio/projects'));
  }

  createProject(name: string): Promise<WorkflowProject> {
    return firstValueFrom(this.http.post<WorkflowProject>('/api/v1/studio/projects', { name }));
  }

  renameProject(id: string, name: string): Promise<WorkflowProject> {
    return firstValueFrom(
      this.http.post<WorkflowProject>(`/api/v1/studio/projects/${encodeURIComponent(id)}/rename`, {
        name,
      }),
    );
  }

  moveToProject(id: string, projectId: string): Promise<unknown> {
    return firstValueFrom(
      this.http.post(
        `/api/v1/studio/definitions/${encodeURIComponent(id)}/project`,
        { project_id: projectId },
        { headers: this.editHeaders(id) },
      ),
    );
  }

  async applyAuthoringDraft(request: AuthoringDraftRequest): Promise<void> {
    const query = new URLSearchParams({
      authoring_version: '2',
      workflow_id: request.workflowId,
      org_id: request.orgId,
      team_id: request.teamId,
    });
    try {
      await firstValueFrom(
        this.http.post(
          '/api/v1/studio/assistant/conversations/' +
            encodeURIComponent(request.conversationId) +
            '/jobs/' +
            encodeURIComponent(request.jobId) +
            '/apply?' +
            query,
          { approved: true, proposal_hash: request.proposalHash, revert: (request as AuthoringDraftRequest & {revert?:boolean}).revert },
          { headers: this.editHeaders(request.workflowId) },
        ),
      );
    } catch (error) {
      const detail = (error as { error?: { detail?: unknown } }).error?.detail;
      throw new Error(
        typeof detail === 'string'
          ? detail
          : 'Lumi could not save this draft. Check your connection and retry saving.',
      );
    }
  }

  getDefinition(id: string, version?: number): Promise<WorkflowDefinition> {
    const suffix = version === undefined ? '' : `?version=${version}`;
    return firstValueFrom(
      this.http.get<WorkflowDefinition>(
        `/api/v1/studio/definitions/${encodeURIComponent(id)}${suffix}`,
      ),
    );
  }

  validate(
    definition: WorkflowDefinition,
  ): Promise<{ valid: boolean; definition: WorkflowDefinition }> {
    return firstValueFrom(
      this.http.post<{ valid: boolean; definition: WorkflowDefinition }>(
        '/api/v1/studio/definitions/validate',
        definition,
      ),
    );
  }

  save(
    definition: WorkflowDefinition,
    message: string,
    projectId: string,
  ): Promise<WorkflowDefinition> {
    return firstValueFrom(
      this.http.post<WorkflowDefinition>('/api/v1/studio/definitions', definition, {
        headers: {
          ...this.editHeaders(definition.id),
          'X-Runloom-Actor': 'studio-user',
          'X-Runloom-Commit-Message': message,
          'X-Runloom-Project': projectId,
        },
      }),
    );
  }

  publish(id: string, message: string): Promise<WorkflowDefinition> {
    return firstValueFrom(
      this.http.post<WorkflowDefinition>(
        `/api/v1/studio/definitions/${encodeURIComponent(id)}/publish`,
        {},
        {
          headers: {
            ...this.editHeaders(id),
            'X-Runloom-Actor': 'studio-user',
            'X-Runloom-Commit-Message': message,
          },
        },
      ),
    );
  }

  definitionHistory(id: string): Promise<DefinitionHistory> {
    return firstValueFrom(
      this.http.get<DefinitionHistory>(
        `/api/v1/studio/definitions/${encodeURIComponent(id)}/history`,
      ),
    );
  }

  restoreCommit(id: string, commitId: string, message: string): Promise<WorkflowDefinition> {
    return firstValueFrom(
      this.http.post<WorkflowDefinition>(
        `/api/v1/studio/definitions/${encodeURIComponent(id)}/commits/${encodeURIComponent(commitId)}/restore`,
        { message },
        { headers: { ...this.editHeaders(id), 'X-Runloom-Actor': 'studio-user' } },
      ),
    );
  }

  startRun(
    definitionId: string,
    version: number | undefined,
    input: unknown,
  ): Promise<{ workflow_id: string }> {
    return firstValueFrom(
      this.http.post<{ workflow_id: string }>('/api/v1/studio/runs', {
        definition_id: definitionId,
        ...(version ? { version } : {}),
        input,
      }),
    );
  }

  testDraft(definition: WorkflowDefinition, input: unknown): Promise<{ workflow_id: string }> {
    return firstValueFrom(
      this.http.post<{ workflow_id: string }>(
        `/api/v1/studio/definitions/${encodeURIComponent(definition.id)}/test`,
        { definition, input },
      ),
    );
  }

  startWebhook(
    definitionId: string,
    path: string,
    input: unknown,
  ): Promise<{ workflow_id: string }> {
    return firstValueFrom(
      this.http.post<{ workflow_id: string }>(
        `/api/v1/studio/webhooks/${encodeURIComponent(definitionId)}/${encodeURIComponent(path)}`,
        input,
      ),
    );
  }

  runStatus(workflowId: string): Promise<RunStatus> {
    return firstValueFrom(
      this.http.get<RunStatus>(`/api/v1/studio/runs/${encodeURIComponent(workflowId)}`),
    );
  }

  listRuns(limit = 20): Promise<WorkflowRunRecord[]> {
    return firstValueFrom(
      this.http.get<WorkflowRunRecord[]>(`/api/v1/studio/runs?limit=${limit}&include_usage=true`),
    );
  }

  llmUsage(
    id: string,
    after = '',
    model = '',
    step = '',
  ): Promise<import('../models/workflow.types').LLMUsagePage> {
    const query = new URLSearchParams({ limit: '50', after, model, step_id: step });
    return firstValueFrom(
      this.http.get<import('../models/workflow.types').LLMUsagePage>(
        `/api/v1/studio/runs/${encodeURIComponent(id)}/llm-usage?${query}`,
      ),
    );
  }

  runHistory(workflowId: string): Promise<RunHistory> {
    return firstValueFrom(
      this.http.get<RunHistory>(`/api/v1/studio/runs/${encodeURIComponent(workflowId)}/history`),
    );
  }

  approval(
    workflowId: string,
    stepId: string,
    approved: boolean,
    comment: string,
  ): Promise<unknown> {
    return firstValueFrom(
      this.http.post(`/api/v1/studio/runs/${encodeURIComponent(workflowId)}/approvals`, {
        step_id: stepId,
        approved,
        comment,
        actor: 'studio-user',
      }),
    );
  }

  callback(workflowId: string, stepId: string, payload: unknown): Promise<unknown> {
    return firstValueFrom(
      this.http.post(
        `/api/v1/studio/callbacks/${encodeURIComponent(workflowId)}/${encodeURIComponent(stepId)}`,
        {
          payload,
          actor: 'studio-simulator',
        },
      ),
    );
  }

  resume(workflowId: string, stepId: string, payload: unknown): Promise<unknown> {
    return firstValueFrom(
      this.http.post(`/api/v1/studio/runs/${encodeURIComponent(workflowId)}/resume`, {
        step_id: stepId,
        payload,
        actor: 'studio-user',
      }),
    );
  }

  signal(
    workflowId: string,
    signalName: string,
    payload: unknown,
    eventID?: string,
  ): Promise<unknown> {
    return firstValueFrom(
      this.http.post(
        `/api/v1/studio/runs/${encodeURIComponent(workflowId)}/signals/${encodeURIComponent(signalName)}`,
        { payload, actor: 'studio-user', ...(eventID ? { event_id: eventID } : {}) },
      ),
    );
  }

  cancel(workflowId: string): Promise<unknown> {
    return firstValueFrom(
      this.http.post(`/api/v1/studio/runs/${encodeURIComponent(workflowId)}/cancel`, {}),
    );
  }
}
