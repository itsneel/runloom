import { CommonModule } from '@angular/common';
import { ChangeDetectorRef, Component, EventEmitter, Input, OnChanges, OnDestroy, Output, inject } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { WorkflowStep } from '../../../../core/models/workflow.types';
import { WorkflowApiService } from '../../../../core/api/workflow-api.service';

@Component({
  selector: 'runloom-code-editor',
  imports: [CommonModule, FormsModule],
  styleUrl: './code-editor.component.scss',
  templateUrl: './code-editor.component.html',
})
export class CodeEditor implements OnChanges, OnDestroy {
  @Input({ required: true }) step!: WorkflowStep;
  @Input() editable = false;
  @Output() changed = new EventEmitter<void>();
  private readonly api = inject(WorkflowApiService);
  private readonly detector = inject(ChangeDetectorRef);
  private generation = 0;
  sample = '{"name":"Runloom"}';
  busy = false;
  result?: { signature: string; text: string; error: boolean };
  get signature(): string { return JSON.stringify([this.step.id, this.step.code, this.sample]); }
  ngOnChanges(): void { this.generation++; this.busy = false; this.result = undefined; }
  ngOnDestroy(): void { this.generation++; }
  setSource(source: string): void {
    if (!this.editable) return;
    this.step.code = { runtime: 'quickjs-ng-0.11.0-v1', source };
    this.result = undefined;
    this.changed.emit();
  }
  async test(): Promise<void> {
    if (!this.editable || this.busy || !this.step.code) return;
    const signature = this.signature, generation = this.generation;
    this.busy = true; this.result = undefined;
    try {
      const input: unknown = JSON.parse(this.sample);
      if (!input || typeof input !== 'object' || Array.isArray(input)) throw new Error('Sample input must be a JSON object.');
      const response = await this.api.testCode({ ...this.step.code }, input as Record<string, unknown>);
      if (generation === this.generation && signature === this.signature) this.result = { signature, text: JSON.stringify(response.output, null, 2), error: false };
    } catch (error: unknown) {
      const failure = error as { error?: { detail?: string }; message?: string };
      if (generation === this.generation && signature === this.signature) this.result = { signature, text: failure.error?.detail || failure.message || 'JavaScript test failed.', error: true };
    } finally {
      if (generation === this.generation) { this.busy = false; this.detector.markForCheck(); }
    }
  }
}
