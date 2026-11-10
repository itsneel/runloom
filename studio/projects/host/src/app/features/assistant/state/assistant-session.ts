import { computed, signal } from '@angular/core';
import type { AuthService } from '@runloom/studio-platform';
import { Conversation, Job, Outgoing, displayMessages, unacknowledged } from '../chat-session';
import { AssistantApi, ChatRequestError } from '../services/assistant-api';
import { ChatStorage } from '../services/chat-storage';
import { ConversationStream } from '../services/conversation-stream';

export interface AssistantContext {
  auth: Pick<AuthService, 'profile' | 'organizationId' | 'teamId' | 'team' | 'bearer'>;
  workflowId(): string | undefined;
  workflowSaved(): boolean;
  canApply(): boolean;
  prepareDraft?(): Promise<void>;
  applyDraft?(conversationId: string, jobId: string, hash: string, revert?: boolean): Promise<boolean>;
}
export interface AssistantView {
  isOpen(): boolean;
  focus(): void;
  clearComposer(): void;
  clearEntrance(): void;
  enterMessage(id: string): void;
  shouldFollow(): boolean;
  scrollToBottom(): void;
  draftChanged(id: string): void;
}

// One owner for conversation state and delivery; the component owns only DOM behavior.
export class AssistantSession {
  readonly applying = signal(false);
  readonly conversations = signal<Conversation[]>([]);
  readonly current = signal<Conversation | undefined>(undefined);
  readonly error = signal('');
  readonly connection = signal('');
  readonly busy = signal(false);
  readonly configured = signal(false);
  readonly ready = signal(false);
  draft = '';
  nextCursor = '';
  private scope = '';
  private epoch = 0;
  private stream?: AbortController;
  readonly outbox = signal<Outgoing[]>([]);
  readonly messagesToDisplay = computed(() => displayMessages(this.current(), this.outbox()));
  readonly storageWarning = signal('');
  readonly conversationLimit = computed(() => {
    if ((this.current()?.jobs.length || 0) >= 40) return 'Conversation limit reached: 40 submissions per chat.';
    const rejected = this.outbox().find(item => ['conversation_limit', 'conversation_size_limit'].includes(item.errorCode || ''));
    return rejected?.error || (rejected ? 'Conversation limit reached. Continue in a new chat.' : '');
  });
  readonly deliveryBlocked = computed(() => !!this.conversationLimit() || this.outbox()[0]?.state === 'failed');
  private needsCreate = true;
 private readonly attemptedSaves = new Set<string>();
  private retryTimer?: ReturnType<typeof setTimeout>;
  private lastUser = '';
  private newID: string = crypto.randomUUID();
  get activeJob(): Job | undefined { return this.current()?.jobs.find(job => ['queued', 'running'].includes(job.status)); }
  get queuedCount(): number { return this.current()?.jobs.filter(job => job.status === 'queued').length || 0; }
  jobFor(id: string): Job | undefined { return this.current()?.jobs.find(job => job.id === id); }
  get sending(): boolean { return this.busy() || this.outbox().length > 0; }
  async applyProposal(job: Job): Promise<void> {
    if (!this.context.canApply() || this.applying() || !job.proposal || !job.proposal_hash || job.applied) return;
    const epoch = this.epoch, id = this.current()?.id, workflow = this.context.workflowId();
    if (!id || (!workflow && job.proposal.operation !== 'create')) return;
    this.applying.set(true); this.error.set('');
    try {
      const handled = workflow && this.context.applyDraft ? await this.context.applyDraft(id, job.id, job.proposal_hash) : false;
      if (epoch !== this.epoch) return;
      const result = handled
        ? await this.request<Conversation>(this.url('conversations/' + id))
        : await this.request<Conversation>(this.url('conversations/' + id + '/jobs/' + job.id + '/apply'), { approved: true, proposal_hash: job.proposal_hash });
      if (epoch !== this.epoch) return;
      this.update(result); const receipt = result.jobs.find(item => item.id === job.id)?.applied;
      this.view.draftChanged(receipt?.parent_id || workflow || '');
    } catch (error) { if (epoch === this.epoch) this.error.set(error instanceof Error ? error.message : 'Draft could not be saved. Retry saving when the problem is resolved.'); }
    finally { if (epoch === this.epoch) { this.applying.set(false); this.saveNextDraft(); } }
  }
  async revertProposal(job: Job): Promise<void> {
    if (!this.context.canApply() || this.applying() || this.activeJob || this.sending || !job.applied || job.reverted || job.proposal?.operation !== 'update' || !job.proposal_hash) return;
    const epoch=this.epoch,id=this.current()?.id,workflow=this.context.workflowId();
    if (!id || !workflow || job.applied.parent_id!==workflow)return;
    this.applying.set(true);this.error.set('');
    try {
      const handled=this.context.applyDraft ? await this.context.applyDraft(id,job.id,job.proposal_hash,true) : false;
      if(epoch!==this.epoch)return;
      const result=handled ? await this.request<Conversation>(this.url('conversations/'+id)) : await this.request<Conversation>(this.url('conversations/'+id+'/jobs/'+job.id+'/apply'),{approved:true,proposal_hash:job.proposal_hash,revert:true});
      if(epoch!==this.epoch)return;
      this.update(result);this.view.draftChanged(workflow);
    } catch(error){if(epoch===this.epoch)this.error.set(error instanceof Error ? error.message : 'The previous draft could not be restored. Your current draft was kept.');}
    finally{if(epoch===this.epoch)this.applying.set(false);}
  }
  setDraft(value: string): void {
    this.draft = value; this.persist();
    if (value || this.outbox().length) this.remember();
  }
  private reset(): void {
    this.view.clearEntrance();
    this.attemptedSaves.clear();
    this.applying.set(false);
    this.epoch++; this.stream?.abort(); this.current.set(undefined); this.conversations.set([]);
    this.error.set(''); this.connection.set(''); this.ready.set(false); this.configured.set(false);
    this.busy.set(false); this.draft = ''; this.outbox.set([]); this.needsCreate = true; clearTimeout(this.retryTimer); this.newID = crypto.randomUUID(); this.nextCursor = '';
  }
  async load(): Promise<void> {
    if (this.context.workflowId() !== undefined && !this.context.workflowSaved()) {
      this.error.set('Save this workflow draft first to start its private, scoped chat. Publishing is not required.');
      return;
    }
    const epoch = this.epoch;
    try {
      const [config, list] = await Promise.all([
        this.request<{ configured: boolean }>(this.url('config')),
        this.request<{ items: Conversation[]; next_cursor: string }>(this.url('conversations')),
      ]);
      if (epoch !== this.epoch) return;
      this.configured.set(config.configured); this.conversations.set(list.items); this.nextCursor = list.next_cursor; this.ready.set(true);
      const selected = this.storage.selected(this.scope);
      if (selected) await this.select(selected);
    } catch (error) { if (epoch === this.epoch) this.error.set((error as Error).message); }
  }
  async more(): Promise<void> {
    const epoch = this.epoch;
    try {
      const page = await this.request<{ items: Conversation[]; next_cursor: string }>(this.url('conversations?after=' + encodeURIComponent(this.nextCursor)));
      if (epoch !== this.epoch) return;
      this.conversations.update(items => [...items, ...page.items.filter(item => !items.some(old => old.id === item.id))]); this.nextCursor = page.next_cursor;
    } catch (error) { if (epoch === this.epoch) this.error.set((error as Error).message); }
  }
  newChat(): void {
    if (this.busy()) return;
    if (this.outbox().length) { void this.moveToNewChat(); return; }
    const draft = this.draft;
    this.view.clearEntrance();
    this.epoch++; this.stream?.abort(); this.current.set(undefined); this.error.set(''); this.connection.set('');
    this.draft = ''; this.outbox.set([]); this.needsCreate = true; clearTimeout(this.retryTimer); this.newID = crypto.randomUUID(); this.busy.set(false);
    this.storage.forgetSelection(this.scope);
    this.draft = draft; this.persist();
    if (draft) this.remember();
    this.view.focus();
  }
  async moveToNewChat(): Promise<void> {
    if (this.busy() || !this.ready()) return;
    if (!this.outbox().length) { this.newChat(); return; }
    const epoch = this.epoch, oldID = this.newID;
    this.busy.set(true); clearTimeout(this.retryTimer); this.error.set('');
    try {
      // A timed-out send may already be saved. Reconcile before moving anything.
      if (!this.needsCreate) {
        const latest = await this.request<Conversation>(this.url('conversations/' + encodeURIComponent(oldID)));
        if (epoch !== this.epoch) return;
        this.update(latest);
      }
      const id = crypto.randomUUID();
      const outbox = this.outbox().map(item => ({ ...item, state: 'pending' as const, attempts: 0, error: undefined, errorCode: undefined }));
      // Persist the destination before clearing the source. If storage fails, do
      // not switch chats or submit anything: the original copy remains usable.
      this.storage.transfer(this.scope, oldID, id, { draft: this.draft, needsCreate: true, outbox });
      this.epoch++; this.stream?.abort(); this.view.clearEntrance();
      this.newID = id; this.needsCreate = true; this.current.set(undefined);
      this.outbox.set(outbox); this.connection.set(''); this.busy.set(false);
      this.remember(); this.persist();
      this.view.focus();
      void this.deliver();
    } catch (error) {
      if (epoch === this.epoch) this.error.set('Could not move messages. Your draft and unsent messages are still in this chat. ' + (error instanceof ChatRequestError ? error.message : 'Check the connection and browser storage, then try again.'));
    } finally { if (epoch === this.epoch) this.busy.set(false); }
  }
  async select(id: string): Promise<void> {
    this.view.clearEntrance();
    const epoch = ++this.epoch; this.stream?.abort(); clearTimeout(this.retryTimer);
    this.busy.set(true); this.error.set(''); this.current.set(undefined); this.restore(id);
    try {
      const c = await this.request<Conversation>(this.url('conversations/' + encodeURIComponent(id)));
      if (epoch !== this.epoch) return;
      this.needsCreate = false; this.update(c);
      this.remember();
      if (this.view.isOpen()) void this.watch(id);
    } catch (error) {
      if (epoch !== this.epoch) return;
      if (!(error instanceof ChatRequestError && error.status === 404 && this.needsCreate)) this.error.set((error as Error).message);
    } finally {
      if (epoch === this.epoch) {
        this.busy.set(false);
        if (!this.error()) void this.deliver();
      }
    }
  }
  private update(c: Conversation): void {
    if (c.id !== this.newID || (this.current()?.id === c.id && c.revision < this.current()!.revision)) return;
    const follow = this.view.shouldFollow();
    this.current.set(c); this.outbox.set(unacknowledged(this.outbox(), c)); this.persist();
    this.conversations.update(items => [{ ...c, messages: [], jobs: [] }, ...items.filter(item => item.id !== c.id)]);
    if (follow) this.view.scrollToBottom();
    this.saveNextDraft();
  }
  resumeDraftSaving(): void { this.saveNextDraft(); }
  private saveNextDraft(): void {
    if (this.applying() || !this.context.canApply()) return;
    const job = this.current()?.jobs.find(item => item.save_draft && item.status === "completed" && item.proposal && item.proposal_hash && !item.applied && !this.attemptedSaves.has(this.epoch + ":" + item.id));
    if (!job) return;
    this.attemptedSaves.add(this.epoch + ":" + job.id);
    void this.applyProposal(job);
  }
  async send(mode: 'queue' | 'stop_and_send' = 'queue'): Promise<void> {
    const content = this.draft.trim();
    if (!content || !this.ready() || this.conversationLimit()) return;
    if (new TextEncoder().encode(content).length > 8000 || this.outbox().length >= 10) {
      this.error.set('Use at most 8,000 bytes per message and wait for pending messages to be saved.'); return;
    }
    this.view.focus();
    this.error.set('');
    const follow = this.view.shouldFollow();
    const requestID = crypto.randomUUID();
    this.view.enterMessage(requestID);
    this.outbox.update(items => [...items, { saveDraft: true, id: requestID, content, mode, createdAt: new Date().toISOString(), state: 'pending', attempts: 0 }]);
    // Capture the submitted text first. Later acknowledgements never clear a new draft.
    this.draft = '';
    // Angular may render on the next frame. Clear the native value now so a
    // keystroke immediately after Enter cannot append to the submitted text.
    this.view.clearComposer();
    this.persist();
    if (follow) this.view.scrollToBottom();
    this.remember();
    void this.deliver();
  }
  private async deliver(): Promise<void> {
    if (this.busy() || !this.ready() || !this.outbox().length || this.deliveryBlocked()) return;
    const epoch = this.epoch, id = this.newID;
    let submittedID = this.outbox()[0].id;
    this.busy.set(true);
    try {
      if (this.needsCreate) {
        const c = await this.request<Conversation>(this.url('conversations'), { id, title: 'New conversation' });
        if (epoch !== this.epoch) return;
        this.needsCreate = false; this.update(c);
      }
      while (epoch === this.epoch && this.outbox().length && !this.conversationLimit()) {
        const item = this.outbox()[0];
        if (item.state === 'failed') break;
        submittedID = item.id;
        this.outbox.update(items => items.map(old => old.id === item.id ? { ...old, state: 'sending' } : old));
        this.persist();
        if (this.context.workflowId() && this.context.prepareDraft) await this.context.prepareDraft();
        if (epoch !== this.epoch) return;
        const c = await this.request<Conversation>(this.url('conversations/' + id + '/messages'), { request_id: item.id, content: item.content, mode: item.mode, save_draft: item.saveDraft === true });
        if (epoch !== this.epoch) return;
        this.update(c);
        if (this.view.isOpen()) void this.watch(id);
      }
    } catch (error) {
      if (epoch !== this.epoch) return;
      const message = (error as Error).message || 'Connection interrupted.';
      const first = this.outbox().find(item => item.id === submittedID);
      if (first) {
        this.outbox.update(items => items.map(item => item.id === first.id ? { ...item, state: 'failed', attempts: item.attempts + 1, error: message, errorCode: error instanceof ChatRequestError ? error.code : undefined } : item));
        this.persist();
        // A lost response may already have committed. Same request ID is safe to retry.
        const retryable = !(error instanceof ChatRequestError) || error.status >= 500;
        if (retryable && first.attempts < 5) {
          clearTimeout(this.retryTimer);
          this.retryTimer = setTimeout(() => { if (epoch === this.epoch) this.retryDelivery(); }, Math.min(8000, 1000 * 2 ** first.attempts));
        }
      }
    } finally {
      if (epoch === this.epoch) {
        this.busy.set(false);
        if (this.outbox().length && !this.deliveryBlocked()) void this.deliver();
      }
    }
  }
  retryDelivery(): void {
    clearTimeout(this.retryTimer);
    this.outbox.update(items => items.map(item => ({ ...item, state: 'pending', error: undefined })));
    this.persist(); void this.deliver();
  }
  dismissLocal(id: string): void {
    if (this.busy()) return;
    this.outbox.update(items => items.filter(item => item.id !== id));
    this.persist(); void this.deliver();
  }
  async stop(): Promise<void> {
    const c = this.current(), job = this.activeJob, epoch = this.epoch; if (!c || !job) return;
    try { const saved = await this.request<Conversation>(this.url('conversations/' + c.id + '/jobs/' + job.id + '/cancel'), {}); if (epoch === this.epoch) this.update(saved); }
    catch (error) {
      if (epoch !== this.epoch) return;
      if (error instanceof ChatRequestError && error.status === 409) {
        try {
          const latest = await this.request<Conversation>(this.url('conversations/' + c.id));
          if (epoch === this.epoch) this.update(latest);
          return;
        } catch (refreshError) { error = refreshError; }
      }
      if (epoch === this.epoch) this.error.set((error as Error).message);
    }
  }
  reconnect(): void {
    if (!this.context.auth.team()) return;
    this.error.set('');
    if (this.outbox().length) this.retryDelivery();
    const c = this.current();
    if (c && this.view.isOpen()) void this.watch(c.id); else if (this.view.isOpen()) void this.load();
  }
  private remember(): void {
    this.storage.remember(this.scope, this.newID);
  }
  private persist(): void {
    try {
      this.storage.save(this.scope, this.newID, { draft: this.draft, needsCreate: this.needsCreate, outbox: this.outbox() });
      this.storageWarning.set('');
    } catch { this.storageWarning.set('Browser draft storage is unavailable. Keep this tab open until messages are saved.'); }
  }
  private restore(id: string): void {
    this.newID = id;
    try {
      const cached = this.storage.restore(this.scope, id);
      this.draft = cached?.draft || ''; this.outbox.set(cached?.outbox || []); this.needsCreate = cached?.needsCreate ?? false;
    } catch { this.storageWarning.set('Browser draft storage is unavailable.'); }
  }
  constructor(private readonly context: AssistantContext, private readonly view: AssistantView) {
    this.api = new AssistantApi({
      auth: context.auth, epoch: () => this.epoch, workflowId: () => context.workflowId(),
    });
  }
  private readonly storage = new ChatStorage();
  private readonly api: AssistantApi;
  private request<T>(path: string, body?: unknown): Promise<T> { return this.api.request<T>(path, body); }
  private url(path: string): string { return this.api.url(path); }

  synchronizeScope(): void {
    const user = this.context.auth.profile()?.user.id || '';
    if (this.lastUser && user !== this.lastUser) this.storage.clearTabDrafts();
    this.lastUser = user;
    const scope = this.context.workflowId() === undefined
      ? [user, this.context.auth.organizationId(), this.context.auth.teamId()].join(':')
      : JSON.stringify([user, this.context.auth.organizationId(), this.context.auth.teamId(), this.context.workflowId(), this.context.workflowSaved()]);
    if (scope === this.scope) return;
    this.scope = scope; this.reset();
    if (this.view.isOpen() && this.context.auth.team()) void this.load();
  }
  dispose(): void { this.epoch++; this.stream?.abort(); clearTimeout(this.retryTimer); }
  pauseStream(): void { this.stream?.abort(); }
  async watch(id: string): Promise<void> {
    this.stream?.abort();
    const live = new ConversationStream({
      auth: this.context.auth, epoch: () => this.epoch, isOpen: () => this.view.isOpen(),
      url: path => this.url(path), revision: () => this.current()?.revision || 0,
      update: snapshot => this.update(snapshot), activeJob: () => !!this.activeJob,
      connection: value => this.connection.set(value), error: value => this.error.set(value),
      accessChanged: message => { this.current.set(undefined); this.error.set(message); },
    });
    this.stream = live.controller;
    await live.watch(id);
  }
}
