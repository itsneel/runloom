import type { AuthService } from '@runloom/studio-platform';
import type { Conversation } from '../chat-session';

interface ConversationStreamContext {
  auth: Pick<AuthService, 'bearer'>;
  epoch(): number;
  isOpen(): boolean;
  url(path: string): string;
  revision(): number;
  update(snapshot: Conversation): void;
  activeJob(): boolean;
  connection(value: string): void;
  error(value: string): void;
  accessChanged(message: string): void;
}
export class ConversationStream {
  readonly controller = new AbortController();
  constructor(private readonly context: ConversationStreamContext) {}

  async watch(id: string): Promise<void> {
    const controller = this.controller;
    const epoch = this.context.epoch(); let failures = 0;
    const valid = () => !controller.signal.aborted && epoch === this.context.epoch() && this.context.isOpen();
    while (valid()) {
      try {
        const token = await this.context.auth.bearer(); if (!valid()) return;
        const response = await fetch(this.context.url('conversations/' + id + '/events'), { headers: { Authorization: 'Bearer ' + token }, signal: controller.signal });
        if (!response.ok || !response.body) {
          if ([401, 403, 404].includes(response.status)) { this.context.accessChanged('Conversation access changed. Refresh your session or select another chat.'); return; }
          throw new Error('Live updates unavailable');
        }
        this.context.connection('Connected');
        const reader = response.body.getReader(), decoder = new TextDecoder(); let buffer = ''; let received = false;
        try {
          while (valid()) {
            const { value, done } = await reader.read(); if (done) break;
            buffer += decoder.decode(value, { stream: true });
            let end: number;
            while ((end = buffer.indexOf('\n\n')) >= 0) {
              const event = buffer.slice(0, end); buffer = buffer.slice(end + 2);
              const data = event.split('\n').find(line => line.startsWith('data: '));
              if (data && valid()) { received = true; const snapshot = JSON.parse(data.slice(6)) as Conversation; if (snapshot.revision >= (this.context.revision() || 0)) this.context.update(snapshot); failures = 0; }
            }
            if (buffer.length > 2_000_000) throw new Error('Invalid event size');
          }
        } finally { await reader.cancel().catch(() => {}); }
        if (!valid()) return;
        if (!received) throw new Error('Empty event stream');
        if (!this.context.activeJob()) { this.context.connection('Saved'); return; }
      } catch {
        if (!valid()) return;
        failures++; this.context.connection('Reconnecting…');
        if (failures >= 5) { this.context.error('Live connection interrupted. Your job continues on the server. Reconnect to see its latest status.'); this.context.connection('Disconnected'); return; }
        await new Promise(resolve => setTimeout(resolve, Math.min(4000, failures * 1000)));
      }
    }
  }
}
