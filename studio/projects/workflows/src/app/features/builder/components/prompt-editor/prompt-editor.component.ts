import { CommonModule } from '@angular/common';
import {
  ChangeDetectorRef,
  Component,
  EventEmitter,
  inject,
  Input,
  OnChanges,
  OnDestroy,
  Output,
  SimpleChanges,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { WorkspaceBridge, injectDialogs } from '@runloom/studio-platform';
import { WorkflowApiService } from '../../../../core/api/workflow-api.service';
import { VariableEditor } from '../../../../shared/components/variable-editor/variable-editor.component';
import type { PromptEntry, PromptVariable, VariableHint } from '../../../../core/models/prompt.types';
import type { WorkflowDefinition, WorkflowStep } from '../../../../core/models/workflow.types';

@Component({
  selector: 'runloom-prompt-editor',
  imports: [CommonModule, FormsModule, VariableEditor],
  templateUrl: './prompt-editor.component.html',
  styleUrl: './prompt-editor.component.scss',
})
export class PromptEditor implements OnChanges, OnDestroy {
  @Input({ required: true }) step!: WorkflowStep;
  @Input({ required: true }) definition!: WorkflowDefinition;
  @Input() editable = false;
  @Output() changed = new EventEmitter<void>();
  @Output() runCreated = new EventEmitter<string>();
  private readonly api = inject(WorkflowApiService);
  private readonly dialogs = injectDialogs();
  private readonly cdr = inject(ChangeDetectorRef);
  private generation = 0;
  hints: VariableHint[] = [];
  entries: PromptEntry[] = [];
  history: PromptEntry[] = [];
  selected = '';
  entry?: PromptEntry;
  libraryOpen = false;
  busy = false;
  samples = '{}';
  preview = '';
  error = '';
  status = '';
  types = ['string', 'number', 'integer', 'boolean', 'object', 'array'];
  ngOnChanges(changes: SimpleChanges): void {
    if (!changes['step'] && !changes['definition']) return;
    this.generation++;
    this.error = '';
    this.preview = '';
    this.entry = undefined;
    this.selected = '';
    this.history = [];
    void this.refreshHints();
  }
  ngOnDestroy(): void {
    this.generation++;
  }
  get locked(): boolean {
    return !this.editable || !!this.step.prompt_ref;
  }
  get templateHints(): VariableHint[] {
    return (this.step.prompt_template?.variables || []).map((v) => ({
      path: 'vars.' + v.name,
      type: v.type,
      optional: !v.required,
      description: v.description || '',
    }));
  }
  async refreshHints(): Promise<void> {
    const generation = this.generation;
    try {
      const hints = await this.api.variables(this.definition, this.step.id);
      if (generation === this.generation) this.hints = hints;
    } catch {
      if (generation === this.generation) this.hints = [];
    } finally {
      this.cdr.markForCheck();
    }
  }
  change(): void {
    this.preview = '';
    this.changed.emit();
  }
  legacy(field: 'prompt' | 'system_prompt', value: string): void {
    if (!this.editable) return;
    this.step[field] = value;
    this.change();
  }
  text(field: 'text' | 'instructions', value: string): void {
    if (this.locked) return;
    this.step.prompt_template![field] = value;
    this.change();
  }
  convert(): void {
    if (!this.editable) return;
    const variables: PromptVariable[] = [],
      mappings: Record<string, unknown> = {},
      names = new Map<string, string>();
    const convert = (text: string) =>
      text.replace(/\$\{([a-zA-Z0-9_.-]+)\}/g, (token, path: string) => {
        let name = names.get(path);
        if (!name) {
          name = 'value' + (names.size + 1);
          names.set(path, name);
          const type = this.hints.find((h) => h.path === path)?.type || 'string';
          variables.push({
            name,
            type: this.types.includes(type) ? type : 'string',
            required: true,
          });
          mappings[name] = token;
        }
        return '${vars.' + name + '}';
      });
    this.step.prompt_template = {
      instructions: convert(this.step.system_prompt || ''),
      text: convert(this.step.prompt || ''),
      variables,
    };
    this.step.prompt_inputs = mappings;
    delete this.step.prompt;
    delete this.step.system_prompt;
    this.change();
  }
  detach(): void {
    if (!this.editable) return;
    delete this.step.prompt_ref;
    this.change();
  }
  addVariable(): void {
    if (this.locked) return;
    const vars = this.step.prompt_template!.variables;
    let n = vars.length + 1;
    while (vars.some((v) => v.name === 'value' + n)) n++;
    vars.push({ name: 'value' + n, type: 'string', required: true });
    this.change();
  }
  removeVariable(v: PromptVariable): void {
    if (this.locked) return;
    this.step.prompt_template!.variables = this.step.prompt_template!.variables.filter(
      (x) => x !== v,
    );
    delete this.step.prompt_inputs?.[v.name];
    this.change();
  }
  rename(v: PromptVariable, name: string): void {
    if (this.locked) return;
    const old = v.name;
    v.name = name;
    const t = this.step.prompt_template!;
    for (const field of ['text', 'instructions'] as const)
      t[field] = t[field].split('${vars.' + old + '}').join('${vars.' + name + '}');
    const inputs = this.step.prompt_inputs || {};
    if (old in inputs) {
      inputs[name] = inputs[old];
      delete inputs[old];
    }
    this.change();
  }
  mapping(v: PromptVariable): string {
    const value = this.step.prompt_inputs?.[v.name];
    return value === undefined ? '' : typeof value === 'string' ? value : JSON.stringify(value);
  }
  setMapping(v: PromptVariable, text: string): void {
    if (!this.editable) return;
    this.error = '';
    const inputs = (this.step.prompt_inputs ||= {});
    if (text === '' && !v.required) {
      delete inputs[v.name];
      this.change();
      return;
    }
    try {
      inputs[v.name] =
        v.type === 'string' || /^\$\{[\w.-]+\}$/.test(text) ? text : JSON.parse(text);
      this.change();
    } catch {
      inputs[v.name] = text;
      this.change();
      this.error =
        'Use JSON for non-string literal values, or insert a complete variable reference.';
    }
  }
  defaultText(v: PromptVariable): string {
    return v.default === undefined
      ? ''
      : typeof v.default === 'string'
        ? v.default
        : JSON.stringify(v.default);
  }
  setDefault(v: PromptVariable, text: string): void {
    if (this.locked) return;
    try {
      if (!text && v.type !== 'string') delete v.default;
      else v.default = v.type === 'string' ? text : JSON.parse(text);
      this.error = '';
      this.change();
    } catch {
      v.default = text;
      this.change();
      this.error = 'Non-string defaults must be valid JSON matching the input type.';
    }
  }
  async task(work: () => Promise<void>): Promise<void> {
    if (this.busy) return;
    this.busy = true;
    this.error = '';
    this.status = '';
    const generation = this.generation;
    try {
      await work();
    } catch (e) {
      if (generation === this.generation) {
        const error = e as { error?: { error?: string; detail?: string }; message?: string };
        this.error =
          error.error?.detail || error.error?.error || error.message || 'Request failed.';
      }
    } finally {
      this.busy = false;
      this.cdr.markForCheck();
    }
  }
  async openLibrary(): Promise<void> {
    this.libraryOpen = !this.libraryOpen;
    if (this.libraryOpen)
      await this.task(async () => {
        this.entries = await this.api.prompts();
      });
  }
  async selectLibrary(id: string): Promise<void> {
    this.selected = id;
    this.entry = undefined;
    this.history = [];
    if (!id) {
      return;
    }
    await this.task(async () => {
      const e = await this.api.prompt(id);
      if (id !== this.selected) return;
      this.entry = e;
      this.history = await this.api.promptHistory(id);
    });
  }
  async useVersion(version: number): Promise<void> {
    if (!this.editable || !this.selected || version < 1) return;
    const step = this.step,
      generation = this.generation,
      id = this.selected;
    await this.task(async () => {
      const e = await this.api.prompt(id, version);
      if (generation !== this.generation || !this.editable) return;
      step.prompt_template = structuredClone(e.template);
      step.prompt_ref = { id: e.id, version: e.version, hash: e.hash };
      const inputs = step.prompt_inputs || {};
      step.prompt_inputs = Object.fromEntries(
        e.template.variables.filter((v) => v.name in inputs).map((v) => [v.name, inputs[v.name]]),
      );
      delete step.prompt;
      delete step.system_prompt;
      this.change();
      this.status = `Pinned ${e.name} v${e.version}. Map its inputs below.`;
    });
  }
  async copyDraft(e: PromptEntry): Promise<void> {
    const generation = this.generation;
    if (
      !this.editable ||
      !(await this.dialogs.confirm(
        'Replace this node’s prompt with a local editable copy? Existing workflow changes are not saved automatically.',
        { title: 'Copy prompt', confirmText: 'Use local copy' },
      ))
    )
      return;
    if (generation !== this.generation || !this.editable) return;
    this.step.prompt_template = structuredClone(e.template);
    delete this.step.prompt_ref;
    delete this.step.prompt;
    delete this.step.system_prompt;
    const inputs = this.step.prompt_inputs || {};
    this.step.prompt_inputs = Object.fromEntries(
      e.template.variables.filter((v) => v.name in inputs).map((v) => [v.name, inputs[v.name]]),
    );
    this.change();
  }
  private readonly bridge = inject(WorkspaceBridge);
  async manageLibrary(): Promise<void> { await this.bridge.host?.openManagement('prompts'); }
  async render(test: boolean): Promise<void> {
    if (!this.editable || !this.step.prompt_template) return;
    const generation = this.generation;
    if (
      test &&
      !(await this.dialogs.confirm(
        'This starts a real, billable LLM test in this workflow’s project. Budget policies apply. It does not execute decision actions. Save the workflow draft first.',
        { title: 'Run prompt test?', confirmText: 'Run paid test' },
      ))
    )
      return;
    if (generation !== this.generation) return;
    await this.task(async () => {
      const inputs: unknown = JSON.parse(this.samples);
      if (!inputs || Array.isArray(inputs) || typeof inputs !== 'object')
        throw new Error('Sample inputs must be a JSON object.');
      if (test) {
        const run = await this.api.testPrompt(
          this.definition.id,
          this.step.prompt_template!,
          inputs as Record<string, unknown>,
          this.step.model,
        );
        if (generation === this.generation) this.runCreated.emit(run.workflow_id);
      } else {
        const result = await this.api.previewPrompt(
          this.step.prompt_template!,
          inputs as Record<string, unknown>,
        );
        if (generation === this.generation) this.preview = JSON.stringify(result, null, 2);
      }
    });
  }
}
