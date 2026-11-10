import { Component, DestroyRef, ElementRef, ViewChild, effect, inject, input, output, signal } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { AuthService, UiIcon } from '@runloom/studio-platform';
import type { AuthoringDraftRequest } from '@runloom/studio-platform';
import type { Job } from './chat-session';
import { AssistantSession } from './state/assistant-session';

@Component({
  selector: 'runloom-assistant',
  imports: [CommonModule, FormsModule, UiIcon],
  templateUrl: './assistant.component.html',
  styleUrl: './assistant.component.scss',
})
export class AssistantComponent {
  readonly workflowId = input<string | undefined>(undefined);
  readonly workflowName = input('');
  readonly workflowSaved = input(true);
  readonly canApply = input(false);
  readonly draftChanged = output<string>();
  readonly prepareWorkflow = input<(() => Promise<void>) | undefined>();
  readonly applyWorkflowDraft = input<((request: AuthoringDraftRequest) => Promise<void>) | undefined>();
  readonly auth = inject(AuthService);
  readonly open = signal(false);
  readonly closing = signal(false);
  readonly shortcut = /Mac|iPhone|iPad/.test(globalThis.navigator?.platform || '') ? '⌘ K' : 'Ctrl K';
  readonly enteringMessages = signal<Set<string>>(new Set());
  @ViewChild('messages') private messages?: ElementRef<HTMLElement>;
  @ViewChild('composer') private composer?: ElementRef<HTMLTextAreaElement>;
  private returnFocus?: HTMLElement;
  private panelElement?: HTMLDialogElement;
  private closeTimer?: ReturnType<typeof setTimeout>;
  @ViewChild('launcher') private launcher?: ElementRef<HTMLButtonElement>;
  @ViewChild('panel') set panel(value: ElementRef<HTMLDialogElement> | undefined) {
    // Angular refreshes view queries when message views change. Animate only a
    // newly mounted dialog, not another ElementRef for the same open element.
    if (this.panelElement === value?.nativeElement) return;
    this.panelElement = value?.nativeElement;
    if (!value) return;
    queueMicrotask(() => {
      if (!this.open() || this.panelElement !== value.nativeElement || !value.nativeElement.isConnected || value.nativeElement.open) return;
      value.nativeElement.showModal();
      this.measureOrigin(value.nativeElement);
      value.nativeElement.classList.add('is-opening');
      this.composer?.nativeElement.focus({ preventScroll: true });
    });
  }
  async toggle(): Promise<void> {
    if (this.closing()) return;
    if (this.open()) { this.close(); return; }
    this.returnFocus = document.activeElement instanceof HTMLElement ? document.activeElement : undefined;
    this.open.set(true);
    if (!this.chat.ready()) await this.chat.load();
    else if (this.chat.current()) void this.chat.watch(this.chat.current()!.id);
    setTimeout(() => { if (this.open()) this.composer?.nativeElement.focus({ preventScroll: true }); });
  }
  close(): void {
    if (this.closing()) return;
    const panel = this.panelElement;
    if (!panel || (globalThis.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? true)) { this.finishClose(); return; }
    // Capture the current transform so dismissal during expansion does not jump.
    panel.style.setProperty('--exit-transform', getComputedStyle(panel).transform);
    panel.classList.remove('is-opening');
    this.measureOrigin(panel);
    this.closing.set(true);
    this.closeTimer = setTimeout(() => this.finishClose(), 300);
  }
  private finishClose(): void {
    this.enteringMessages.set(new Set());
    clearTimeout(this.closeTimer);
    this.closing.set(false);
    this.open.set(false);
    this.chat.pauseStream();
    setTimeout(() => { if (!this.open() && this.returnFocus?.isConnected) this.returnFocus.focus({ preventScroll: true }); });
  }
  dismiss(event: Event): void {
    event.preventDefault();
    event.stopPropagation();
    this.close();
  }
  private measureOrigin(panel: HTMLDialogElement): void {
    const origin = this.launcher?.nativeElement.getBoundingClientRect();
    if (!origin) return;
    // Layout coordinates are unaffected by an in-progress transform.
    panel.style.setProperty('--launch-x', `${origin.x + origin.width / 2 - panel.offsetLeft - panel.offsetWidth / 2}px`);
    panel.style.setProperty('--launch-y', `${origin.bottom - panel.offsetTop - panel.offsetHeight}px`);
    panel.style.setProperty('--launch-scale-x', `${origin.width / panel.offsetWidth}`);
    panel.style.setProperty('--launch-scale-y', `${origin.height / panel.offsetHeight}`);
  }
  motionEnded(event: AnimationEvent): void {
    if (event.target !== this.panelElement) return;
    // Angular scopes CSS keyframe names in production builds.
    if (event.animationName.endsWith('assistant-collapse')) this.finishClose();
    else if (event.animationName.endsWith('assistant-expand')) this.panelElement?.classList.remove('is-opening');
  }
  backdrop(event: MouseEvent): void {
    if (event.target !== event.currentTarget) return;
    const bounds = (event.currentTarget as HTMLElement).getBoundingClientRect();
    if (event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom) this.close();
  }
  modalKeys(event: KeyboardEvent): void {
    if (event.key !== 'Tab') return;
    const panel = event.currentTarget as HTMLElement;
    const controls = Array.from(panel.querySelectorAll<HTMLElement>('button:not(:disabled), select:not(:disabled), textarea:not(:disabled), summary, a[href], [tabindex="0"]'))
      .filter(element => element.getClientRects().length > 0);
    const first = controls[0], last = controls.at(-1);
    if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
    else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
  }
  savedDefinition(job: Job): Record<string, unknown> | undefined {
    return job.authoring_context?.workflows.find(item => item.id === this.workflowId())?.definition;
  }
  suggest(prompt: string): void {
    this.chat.setDraft(prompt);
    this.composer?.nativeElement.focus();
  }
  finishMessageEntrance(event: AnimationEvent, requestID?: string): void {
    if (!requestID || event.target !== event.currentTarget || !event.animationName.endsWith('message-send-in')) return;
    this.enteringMessages.update(ids => new Set([...ids].filter(id => id !== requestID)));
  }
  keydown(event: KeyboardEvent): void {
    if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) { event.preventDefault(); void this.chat.send(); }
    if (event.key === 'Escape' && !event.isComposing) this.dismiss(event);
  }
  readonly chat = new AssistantSession({
    prepareDraft: async () => { await this.prepareWorkflow()?.(); },
    applyDraft: async (conversationId, jobId, proposalHash, revert) => {
      const apply = this.applyWorkflowDraft(), workflowId = this.workflowId();
      if (!apply || !workflowId) return false;
      const request: AuthoringDraftRequest & {revert?:boolean} = {conversationId,jobId,proposalHash,workflowId,orgId:this.auth.organizationId(),teamId:this.auth.teamId(),revert};
      await apply(request);
      return true;
    },
    auth: this.auth, workflowId: () => this.workflowId(), workflowSaved: () => this.workflowSaved(), canApply: () => this.canApply(),
  }, {
    isOpen: () => this.open(), focus: () => this.composer?.nativeElement.focus({ preventScroll: true }),
    clearComposer: () => { if (this.composer) this.composer.nativeElement.value = ''; },
    clearEntrance: () => this.enteringMessages.set(new Set()),
    enterMessage: id => { if (!globalThis.matchMedia?.('(prefers-reduced-motion: reduce)').matches) this.enteringMessages.update(ids => new Set([...ids, id])); },
    shouldFollow: () => { const pane = this.messages?.nativeElement; return !pane || pane.scrollHeight - pane.scrollTop - pane.clientHeight < 100; },
    scrollToBottom: () => { setTimeout(() => { const el = this.messages?.nativeElement; if (el) el.scrollTop = el.scrollHeight; }); },
    draftChanged: id => this.draftChanged.emit(id),
  });

  constructor() {
    effect(() => this.chat.synchronizeScope());
    effect(() => { if (this.open() && this.canApply()) this.chat.resumeDraftSaving(); });
    const online = () => this.chat.reconnect();
    const shortcut = (event: KeyboardEvent) => {
      if (!(event.metaKey || event.ctrlKey) || event.altKey || event.shiftKey || event.key.toLowerCase() !== 'k' || !this.auth.team()) return;
      if (document.querySelector('dialog[open]:not(#assistant-panel)')) return;
      event.preventDefault();
      void this.toggle();
    };
    window.addEventListener('online', online);
    window.addEventListener('keydown', shortcut);
    inject(DestroyRef).onDestroy(() => {
      this.chat.dispose(); clearTimeout(this.closeTimer);
      window.removeEventListener('online', online); window.removeEventListener('keydown', shortcut);
    });
  }
}
