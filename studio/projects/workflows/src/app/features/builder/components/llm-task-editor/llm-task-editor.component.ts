import { CommonModule } from '@angular/common';
import { Component, EventEmitter, Input, Output } from '@angular/core';
import { FormsModule } from '@angular/forms';
import type { WorkflowStep } from '../../../../core/models/workflow.types';

@Component({
  selector: 'runloom-llm-task-editor',
  imports: [CommonModule, FormsModule],
  templateUrl: './llm-task-editor.component.html',
  styleUrl: './llm-task-editor.component.scss',
})
export class LLMTaskEditor {
  @Input({required: true}) step!: WorkflowStep;
  @Input() editable = false;
  @Output() changed = new EventEmitter<void>();
  setKind(kind: string): void {
    if (!this.editable) return;
    if (!kind) delete this.step.llm_task;
    else {
      this.step.llm_task = {kind: kind as NonNullable<WorkflowStep['llm_task']>['kind']};
      if (kind === 'extract') this.step.llm_task.fields = [{name: 'value', type: 'string', description: 'The value to extract'}];
      if (kind === 'classify') this.step.llm_task.labels = ['routine', 'escalation'];
    }
    this.changed.emit();
  }
  setLabels(text: string): void {
    if (!this.editable || !this.step.llm_task) return;
    this.step.llm_task.labels = text.split('\n').map(s => s.trim()).filter(Boolean);
    this.changed.emit();
  }
  addField(): void {
    if (!this.editable || !this.step.llm_task) return;
    this.step.llm_task.fields ??= [];
    this.step.llm_task.fields.push({name: `field_${this.step.llm_task.fields.length + 1}`, type: 'string'});
    this.changed.emit();
  }
  removeField(index: number): void {
    if (!this.editable) return;
    this.step.llm_task?.fields?.splice(index, 1); this.changed.emit();
  }
}
