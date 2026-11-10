import type { AuthService } from '@runloom/studio-platform';

export class ChatRequestError extends Error {
  constructor(message: string, readonly status: number, readonly code = '') { super(message); }
}
interface AssistantApiContext {
  auth: Pick<AuthService, 'profile' | 'bearer' | 'organizationId' | 'teamId'>;
  epoch(): number;
  workflowId(): string | undefined;
}
export class AssistantApi {
  constructor(private readonly context: AssistantApiContext) {}

  async request<T>(path: string, body?: unknown): Promise<T> {
    const epoch = this.context.epoch(), user = this.context.auth.profile()?.user.id;
    const token = await this.context.auth.bearer();
    if (epoch !== this.context.epoch() || user !== this.context.auth.profile()?.user.id) throw new Error('Chat context changed.');
    const response = await fetch(path, {
      method: body === undefined ? 'GET' : 'POST',
      headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: AbortSignal.timeout(30000),
    });
    if (!response.ok) {
      const data = await response.json().catch(() => ({}));
      throw new ChatRequestError(data.detail || 'Chat request failed. Please retry.', response.status, data.code || '');
    }
    return response.json() as Promise<T>;
  }
  url(path: string): string {
    return '/api/v1/studio/assistant/' + path + (path.includes('?') ? '&' : '?') +
      new URLSearchParams({ authoring_version: '2', org_id: this.context.auth.organizationId(), team_id: this.context.auth.teamId(),
        ...(this.context.workflowId() === undefined ? {} : { workflow_id: this.context.workflowId()! }) });
  }
}
