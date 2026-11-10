import { CommonModule } from '@angular/common';
import {
  ChangeDetectorRef,
  Component,
  HostListener,
  Input,
  OnChanges,
  OnDestroy,
  inject,
  afterNextRender,
  ElementRef,
  Injector,
  ViewChild,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { AuthService, Notice, injectDialogs } from '@runloom/studio-platform';
import { DashboardApi } from '../../core/api/dashboard-api.service';
import { errorMessage, sorted } from '../../shared/utils/dashboard-data';
import type { PromptEntry, PromptVariable } from './models/prompt.models';
import { promptFieldErrors, suggestedPromptID } from './prompt-validation';
import { parsePromptTags, promptTagsError } from './models/prompt-tags';

@Component({
  selector: 'runloom-prompt-library',
  imports: [CommonModule, FormsModule, Notice],
  templateUrl: './prompt-library.component.html',
  styleUrls: ['../../shared/styles/dashboard.scss', './prompt-library.component.scss'],
})
export class PromptLibrary implements OnChanges, OnDestroy {
  @Input() teamId = '';
  readonly auth = inject(AuthService);
  private readonly api = inject(DashboardApi);
  private readonly cd = inject(ChangeDetectorRef);
  private readonly dialogs = injectDialogs();
  private readonly injector = inject(Injector);
  @ViewChild('editorPanel') private editorPanel?: ElementRef<HTMLElement>;
  private revealEditor(): void {
    const generation = this.generation;
    afterNextRender(
      () => {
        if (generation !== this.generation) return;
        this.editorPanel?.nativeElement.scrollIntoView({ block: 'start', behavior: 'smooth' });
        this.editorPanel?.nativeElement.focus({ preventScroll: true });
      },
      { injector: this.injector },
    );
  }
  private generation = 0;
  private baseline = '';
  private confirmedExit = '';
  entries: PromptEntry[] = [];
  history: PromptEntry[] = [];
  draft?: PromptEntry;
  defaults: string[] = [];
  defaultEnabled: boolean[] = [];
  busy = false;
  loading = false;
  error = '';
  notice = '';
  search = '';
  tag = '';
  tagsInput = '';
  status = 'all';
  sort: 'name' | 'updated' = 'updated';
  ascending = false;
  page = 0;
  samples = '{}';
  preview = '';
  historyError = '';
  validationAttempted = false;
  private automaticID = false;
  private idSuffix = '';
  get fieldErrors(): Record<string, string> {
    if (!this.validationAttempted || !this.draft) return {};
    const errors = promptFieldErrors(this.draft);
    const tagsError = promptTagsError(parsePromptTags(this.tagsInput));
    if (tagsError) errors['tags'] = tagsError;
    return errors;
  }
  renamePrompt(name: string): void {
    if (!this.draft || this.busy || !this.auth.canEdit()) return;
    this.draft.name = name;
    if (this.automaticID && !this.draft.revision)
      this.draft.id = suggestedPromptID(name, this.idSuffix);
  }
  editID(id: string): void {
    if (!this.draft || this.draft.revision || this.busy || !this.auth.canEdit()) return;
    this.automaticID = false;
    this.draft.id = id;
  }
  readonly types = ['string', 'number', 'integer', 'boolean', 'object', 'array'];
  get snapshot(): string {
    return JSON.stringify([this.draft, this.defaults, this.defaultEnabled, this.tagsInput]);
  }
  get dirty(): boolean {
    return !!this.draft && (this.busy || this.snapshot !== this.baseline);
  }
  get publishedCount(): number {
    return this.entries.filter((e) => e.version > 0).length;
  }
  get draftCount(): number {
    return this.entries.filter((e) => !e.published).length;
  }
  get filtered(): PromptEntry[] {
    const q = this.search.toLowerCase().trim();
    return sorted(
      this.entries.filter(
        (e) =>
          (!q || [e.name, e.id, e.description, ...(e.tags || [])].some((v) => v.toLowerCase().includes(q))) &&
          (!this.tag || (e.tags || []).includes(this.tag)) &&
          (this.status === 'all' || (this.status === 'published' ? e.published : !e.published)),
      ),
      (e) => (this.sort === 'name' ? e.name : e.updated_at || ''),
      this.ascending,
    );
  }
  get availableTags(): string[] {
    return [...new Set(this.entries.flatMap(entry => entry.tags || []))].sort((a, b) => a.localeCompare(b));
  }
  get pages(): number {
    return Math.max(1, Math.ceil(this.filtered.length / 15));
  }
  get rows(): PromptEntry[] {
    return this.filtered.slice(this.page * 15, this.page * 15 + 15);
  }
  setSort(sort: 'name' | 'updated'): void {
    this.ascending = this.sort === sort ? !this.ascending : sort === 'name';
    this.sort = sort;
    this.page = 0;
  }
  ngOnChanges(): void {
    this.generation++;
    this.entries = [];
    this.draft = undefined;
    this.history = [];
    this.busy = false;
    this.loading = false;
    this.error = '';
    this.notice = '';
    this.search = '';
    this.tag = '';
    this.tagsInput = '';
    this.status = 'all';
    this.page = 0;
    void this.load();
  }
  ngOnDestroy(): void {
    this.generation++;
  }
  async load(): Promise<void> {
    if (!this.teamId || this.loading) return;
    const generation = this.generation;
    this.loading = true;
    this.error = '';
    try {
      const entries = await this.api.get<PromptEntry[]>(this.teamId, 'prompts');
      if (generation === this.generation) {
        this.entries = entries;
        this.page = Math.min(this.page, this.pages - 1);
      }
    } catch (e) {
      if (generation === this.generation) this.error = errorMessage(e);
    } finally {
      if (generation === this.generation) this.loading = false;
      this.cd.markForCheck();
    }
  }
  async confirmDiscard(): Promise<boolean> {
    if (this.busy) return false;
    if (!this.dirty) return true;
    const snapshot = this.snapshot,
      generation = this.generation;
    return (
      (await this.dialogs.confirm('Discard unsaved changes to this library prompt?', {
        title: 'Unsaved prompt changes',
        confirmText: 'Discard changes',
        cancelText: 'Keep editing',
        danger: true,
      })) &&
      !this.busy &&
      snapshot === this.snapshot &&
      generation === this.generation
    );
  }
  async withConfirmedExit(work: () => Promise<void>): Promise<void> {
    this.confirmedExit = this.snapshot;
    try {
      await work();
    } finally {
      this.confirmedExit = '';
    }
  }
  @HostListener('window:beforeunload', ['$event']) beforeUnload(event: BeforeUnloadEvent): void {
    if (this.dirty && this.confirmedExit !== this.snapshot) {
      event.preventDefault();
      event.returnValue = '';
    }
  }
  private use(entry: PromptEntry): void {
    this.validationAttempted = false;
    this.automaticID = false;
    this.draft = structuredClone(entry);
    this.tagsInput = (entry.tags || []).join(', ');
    this.defaults = entry.template.variables.map((v) =>
      v.default === undefined
        ? ''
        : v.type === 'string'
          ? String(v.default)
          : JSON.stringify(v.default),
    );
    this.defaultEnabled = entry.template.variables.map((v) => v.default !== undefined);
    this.baseline = this.snapshot;
    this.samples = '{}';
    this.preview = '';
    this.historyError = '';
    this.notice = '';
  }
  async create(): Promise<void> {
    if (!this.auth.canEdit() || !(await this.confirmDiscard())) return;
    this.generation++;
    this.loading = false;
    this.history = [];
    this.error = '';
    this.idSuffix = crypto.randomUUID().slice(0, 8);
    this.use({
      id: suggestedPromptID('Untitled prompt', this.idSuffix),
      name: 'Untitled prompt',
      description: '',
      revision: 0,
      version: 0,
      published: false,
      template: { instructions: '', text: '', variables: [] },
    });
    this.automaticID = true;
    // A new prompt is unsaved even before the first keystroke.
    this.baseline = '';
    this.revealEditor();
  }
  async open(id: string): Promise<void> {
    if (!(await this.confirmDiscard())) return;
    const generation = ++this.generation;
    this.loading = false;
    this.busy = true;
    this.error = '';
    this.draft = undefined;
    this.history = [];
    try {
      const entry = await this.api.get<PromptEntry>(
        this.teamId,
        'prompts/' + encodeURIComponent(id),
      );
      if (generation !== this.generation) return;
      this.use(entry);
      this.revealEditor();
      await this.loadHistory(id, generation);
    } catch (e) {
      if (generation === this.generation) this.error = errorMessage(e);
    } finally {
      if (generation === this.generation) this.busy = false;
      this.cd.markForCheck();
    }
  }
  async close(): Promise<void> {
    if (await this.confirmDiscard()) {
      this.generation++;
      this.loading = false;
      this.draft = undefined;
      this.history = [];
      this.preview = '';
    }
  }
  private async loadHistory(id: string, generation: number): Promise<void> {
    try {
      const history = await this.api.get<PromptEntry[]>(
        this.teamId,
        `prompts/${encodeURIComponent(id)}/history`,
      );
      if (generation === this.generation) this.history = history;
    } catch (e) {
      if (generation === this.generation) this.historyError = errorMessage(e);
    }
  }
  addVariable(): void {
    if (
      !this.draft ||
      !this.auth.canEdit() ||
      this.busy ||
      this.draft.template.variables.length >= 50
    )
      return;
    let n = this.draft.template.variables.length + 1;
    while (this.draft.template.variables.some((v) => v.name === 'value' + n)) n++;
    this.draft.template.variables.push({ name: 'value' + n, type: 'string', required: true });
    this.defaults.push('');
    this.defaultEnabled.push(false);
  }
  removeVariable(index: number): void {
    if (!this.busy && this.auth.canEdit()) {
      this.draft?.template.variables.splice(index, 1);
      this.defaults.splice(index, 1);
      this.defaultEnabled.splice(index, 1);
    }
  }
  rename(variable: PromptVariable, name: string): void {
    if (!this.draft || this.busy || !this.auth.canEdit()) return;
    for (const field of ['text', 'instructions'] as const)
      this.draft.template[field] = this.draft.template[field]
        .split('${vars.' + variable.name + '}')
        .join('${vars.' + name + '}');
    variable.name = name;
  }
  insertVariable(variable: PromptVariable, editor: HTMLTextAreaElement): void {
    if (!this.draft || this.busy || !this.auth.canEdit()) return;
    const token = '${vars.' + variable.name + '}';
    const at = editor.selectionStart,
      end = editor.selectionEnd;
    this.draft.template.text =
      this.draft.template.text.slice(0, at) + token + this.draft.template.text.slice(end);
    editor.focus();
  }
  private prepared(): PromptEntry {
    const entry = structuredClone(this.draft!);
    entry.tags = parsePromptTags(this.tagsInput);
    entry.template.variables.forEach((v, index) => {
      const raw = this.defaults[index];
      if (!this.defaultEnabled[index]) delete v.default;
      else v.default = v.type === 'string' ? raw : JSON.parse(raw);
    });
    return entry;
  }
  async save(publish: boolean): Promise<void> {
    if (!this.draft || this.busy || !this.auth.canEdit()) return;
    this.validationAttempted = true;
    this.error = '';
    this.notice = '';
    if (Object.keys(this.fieldErrors).length) {
      this.error = 'Check the highlighted fields before saving or publishing this prompt.';
      this.cd.markForCheck();
      return;
    }
    const generation = this.generation;
    this.busy = true;
    this.error = '';
    this.notice = '';
    try {
      const payload = this.prepared();
      const saved = await this.api.post<PromptEntry>(
        this.teamId,
        `prompts/${encodeURIComponent(payload.id)}/${publish ? 'publish' : 'save'}`,
        payload,
      );
      if (generation !== this.generation) return;
      this.use(saved);
      this.entries = [...this.entries.filter((e) => e.id !== saved.id), saved];
      this.page = Math.min(this.page, this.pages - 1);
      this.notice = publish
        ? `Published ${saved.name} v${saved.version}. Existing workflows keep their pinned version.`
        : 'Prompt draft saved.';
      await this.loadHistory(saved.id, generation);
    } catch (e) {
      if (generation === this.generation) this.error = errorMessage(e);
    } finally {
      if (generation === this.generation) this.busy = false;
      this.cd.markForCheck();
    }
  }
  async copyRevision(entry: PromptEntry): Promise<void> {
    if (!this.draft || !this.auth.canEdit() || !(await this.confirmDiscard())) return;
    // Keep the current draft's revision for optimistic concurrency, never restore
    // an old revision number or mutate an immutable published snapshot.
    this.draft.template = structuredClone(entry.template);
    this.tagsInput = (entry.tags || []).join(', ');
    this.defaults = entry.template.variables.map((v) =>
      v.default === undefined
        ? ''
        : v.type === 'string'
          ? String(v.default)
          : JSON.stringify(v.default),
    );
    this.defaultEnabled = entry.template.variables.map((v) => v.default !== undefined);
    this.preview = '';
    this.notice = 'Revision copied into the editor. Save or publish to keep it.';
  }
  async render(): Promise<void> {
    if (!this.draft || this.busy || !this.auth.canEdit()) return;
    const generation = this.generation;
    this.busy = true;
    this.error = '';
    this.preview = '';
    try {
      const inputs = JSON.parse(this.samples);
      if (!inputs || typeof inputs !== 'object' || Array.isArray(inputs))
        throw new Error('Sample inputs must be a JSON object.');
      const result = await this.api.post(this.teamId, 'prompts/preview', {
        template: this.prepared().template,
        inputs,
      });
      if (generation === this.generation) this.preview = JSON.stringify(result, null, 2);
    } catch (e) {
      if (generation === this.generation) this.error = errorMessage(e);
    } finally {
      if (generation === this.generation) this.busy = false;
      this.cd.markForCheck();
    }
  }
}
