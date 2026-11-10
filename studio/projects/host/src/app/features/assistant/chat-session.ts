export interface Message { id: string; role: string; content: string; job_id: string; created_at: string; }
export interface Job { save_draft?: boolean; id: string; request_id: string; kind: string; status: string; error?: string; model: string;
  authoring_context?: {workflows: {id: string; definition?: Record<string, unknown>}[]};
  proposal?: { name: string; steps: Record<string, unknown>[]; before_step_id: string; operation?: 'create' | 'update'; definition?: Record<string, unknown> }; proposal_hash?: string;
  reverted?: { request_id: string; parent_id: string; draft_only: boolean };
  applied?: { parent_hash?: string; request_id: string; parent_id: string; child_id: string; draft_only: boolean };
}
export interface Conversation { id: string; title: string; revision: number; messages: Message[]; jobs: Job[]; }
export interface Outgoing {
  saveDraft?: boolean; id: string; content: string; mode: 'queue' | 'stop_and_send'; createdAt: string;
  state: 'pending' | 'sending' | 'failed'; attempts: number; error?: string; errorCode?: string;
}
export interface ChatSession { draft: string; needsCreate: boolean; outbox: Outgoing[]; }
export interface DisplayMessage extends Message { key: string; delivery?: Outgoing; request_id?: string; }

// Acknowledgement changes delivery state, not the identity of the rendered row.
// Equal message text is never used for matching: repeated messages are valid.
export function displayMessages(conversation: Conversation | undefined, outbox: Outgoing[]): DisplayMessage[] {
  const jobs = new Map((conversation?.jobs || []).map(job => [job.id, job]));
  const rows: DisplayMessage[] = (conversation?.messages || []).map(message => {
    const request = message.role === 'user' ? jobs.get(message.job_id)?.request_id : undefined;
    return { ...message, key: request ? 'user:' + request : 'message:' + message.id, request_id: request };
  });
  const acknowledged = new Set(rows.map(row => row.request_id).filter(Boolean));
  return [...rows, ...outbox.filter(item => !acknowledged.has(item.id)).map(item => ({
    id: item.id, key: 'user:' + item.id, request_id: item.id, role: 'user', content: item.content,
    created_at: item.createdAt, job_id: '', delivery: item,
  }))];
}
export const sessionPrefix = 'runloom-chat-session:';
export const sessionKey = (scope: string, id: string) => sessionPrefix + encodeURIComponent(scope) + ':' + id;

// An acknowledgement is the persisted job's request ID, never matching text.
export function unacknowledged(outbox: Outgoing[], conversation: Conversation): Outgoing[] {
  const saved = new Set(conversation.jobs.map(job => job.request_id));
  return outbox.filter(item => !saved.has(item.id));
}
export function restoreSession(value: string | null): ChatSession | undefined {
  if (!value) return undefined;
  try {
    const parsed = JSON.parse(value) as ChatSession;
    if (typeof parsed.draft !== 'string' || parsed.draft.length > 8000 ||
        typeof parsed.needsCreate !== 'boolean' || !Array.isArray(parsed.outbox) || parsed.outbox.length > 10) return undefined;
    if (parsed.outbox.some(item => !/^[0-9a-f-]{36}$/i.test(item.id) || typeof item.content !== 'string' ||
      !item.content.trim() || new TextEncoder().encode(item.content).length > 8000 ||
      !['queue', 'stop_and_send'].includes(item.mode) || typeof item.createdAt !== 'string')) return undefined;
    return { ...parsed, outbox: parsed.outbox.map(item => ({ ...item, state: 'pending', attempts: 0, error: undefined })) };
  } catch { return undefined; }
}
