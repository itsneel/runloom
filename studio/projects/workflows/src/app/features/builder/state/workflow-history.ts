// Editing-session snapshots are independent of server commits and published versions.
export class WorkflowHistory {
  private entries: string[];
  private index = 0;
  private group?: object;
  private recordedAt = 0;

  constructor(snapshot: string, private readonly limit = 100) { this.entries = [snapshot]; }
  get canUndo(): boolean { return this.index > 0; }
  get canRedo(): boolean { return this.index < this.entries.length - 1; }
  reset(snapshot: string): void { this.entries = [snapshot]; this.index = 0; this.group = undefined; }
  record(snapshot: string, group?: object, now = Date.now()): void {
    if (snapshot === this.entries[this.index]) return;
    const merge = group && group === this.group && now - this.recordedAt < 750 && !this.canRedo && this.index > 0;
    this.entries.splice(this.index + 1);
    if (merge) this.entries[this.index] = snapshot;
    else { this.entries.push(snapshot); this.index++; }
    if (this.entries.length > this.limit + 1) { this.entries.shift(); this.index--; }
    this.group = group;
    this.recordedAt = now;
  }
  undo(): string | undefined {
    this.group = undefined;
    return this.canUndo ? this.entries[--this.index] : undefined;
  }
  redo(): string | undefined {
    this.group = undefined;
    return this.canRedo ? this.entries[++this.index] : undefined;
  }
}

export function isTextEditingTarget(target: EventTarget | null): boolean {
  const element = target as HTMLElement | null;
  return !!element && (['INPUT', 'TEXTAREA', 'SELECT'].includes(element.tagName) || element.isContentEditable ||
    !!element.closest?.('[contenteditable="true"], .monaco-editor, .cm-editor'));
}
