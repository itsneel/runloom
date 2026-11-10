interface AutosaveContext {
  readonly eligible: boolean;
  readonly dirty: boolean;
  readonly busy: boolean;
  readonly blocked: boolean;
  readonly generation: number;
  save(): Promise<boolean>;
  markForCheck(): void;
}

// One pending debounce and one writer. Failures stay local until another edit or Retry.
export class WorkflowAutosave {
  private timer?: ReturnType<typeof setTimeout>;
  private disposed = false;
  failed = false;
  constructor(private readonly context: AutosaveContext) {}
  changed(): void {
    clearTimeout(this.timer);
    if (this.disposed || !this.context.eligible || !this.context.dirty) return;
    const generation = this.context.generation;
    this.timer = setTimeout(() => void this.flush(generation), 1000);
  }
  reset(): void { clearTimeout(this.timer); this.failed = false; }
  dispose(): void { this.disposed = true; this.reset(); }
  async retry(): Promise<void> { clearTimeout(this.timer); await this.flush(this.context.generation); }
  private async flush(generation: number): Promise<void> {
    if (this.disposed || generation !== this.context.generation || !this.context.eligible || !this.context.dirty) return;
    if (this.context.busy || this.context.blocked) { this.changed(); return; }
    const saved = await this.context.save();
    if (this.disposed || generation !== this.context.generation) return;
    // A successful older save may leave newer edits dirty; the next debounce handles them.
    this.failed = !saved;
    this.context.markForCheck();
    if (saved && this.context.dirty) this.changed();
  }
}
