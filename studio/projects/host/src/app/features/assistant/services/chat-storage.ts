import { ChatSession, restoreSession, sessionKey, sessionPrefix } from '../chat-session';

// Access lazily so callers retain the existing unavailable-storage recovery paths.
export class ChatStorage {
  get tab(): Storage { return sessionStorage; }
  get preferences(): Storage { return localStorage; }
  preferenceKey(scope: string): string { return 'runloom-assistant:' + scope; }
  remember(scope: string, id: string): void {
    try { this.tab.setItem(this.preferenceKey(scope) + ':selected', id); } catch {}
    try { this.preferences.setItem(this.preferenceKey(scope), id); } catch {}
  }
  selected(scope: string): string {
    let selected = '';
    try { selected = this.tab.getItem(this.preferenceKey(scope) + ':selected') || ''; } catch {}
    if (!selected) try { selected = this.preferences.getItem(this.preferenceKey(scope)) || ''; } catch {}
    return selected;
  }
  forgetSelection(scope: string): void {
    try { this.preferences.removeItem(this.preferenceKey(scope)); } catch {}
    try { this.tab.removeItem(this.preferenceKey(scope) + ':selected'); } catch {}
  }
  save(scope: string, id: string, session: ChatSession): void {
    const key = sessionKey(scope, id);
    if (!session.draft && !session.outbox.length) this.tab.removeItem(key);
    else this.tab.setItem(key, JSON.stringify(session));
  }
  restore(scope: string, id: string): ChatSession | undefined {
    return restoreSession(this.tab.getItem(sessionKey(scope, id)));
  }
  transfer(scope: string, oldID: string, id: string, session: ChatSession): void {
    // Write first: storage failure must never discard the only unsent copy.
    this.tab.setItem(sessionKey(scope, id), JSON.stringify(session));
    this.tab.removeItem(sessionKey(scope, oldID));
  }
  clearTabDrafts(): void {
    try { for (const key of Object.keys(this.tab)) if (key.startsWith(sessionPrefix)) this.tab.removeItem(key); } catch {}
  }
}
