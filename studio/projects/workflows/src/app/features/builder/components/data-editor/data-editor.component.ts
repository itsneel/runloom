import { CommonModule } from '@angular/common';
import { Component, EventEmitter, Input, Output } from '@angular/core';
import { FormsModule } from '@angular/forms';
import type { DataOperation, WorkflowStep } from '../../../../core/models/workflow.types';

@Component({
  selector: 'runloom-data-editor',
  imports: [CommonModule, FormsModule],
  templateUrl: './data-editor.component.html',
  styleUrl: './data-editor.component.scss',
})
export class DataEditor {
  @Input({required: true}) step!: WorkflowStep;
  @Input() editable = false;
  @Input() draft: string | null = null;
  @Output() changed = new EventEmitter<void>();
  @Output() invalid = new EventEmitter<string | null>();
  @Output() addBodyNode = new EventEmitter<void>();
  readonly conversions = ['', 'string', 'number', 'integer', 'boolean', 'object', 'array'];
  readonly comparisons = ['equals', 'not_equals', 'contains', 'exists', 'greater_than', 'greater_than_or_equal', 'less_than', 'less_than_or_equal'];
  get data(): DataOperation { return this.step.data!; }
  get operations(): string[] {
    return this.step.type === 'merge' ? ['append', 'join'] : ['filter', 'sort', 'limit', 'unique', 'count', 'sum', 'split', 'compare'];
  }
  setOperation(value: string): void {
    if (!this.editable) return;
    this.step.data = {operation: value as DataOperation['operation'], source: this.data.source};
    if (value === 'filter') this.data.predicate = {left: '${item.active}', operator: 'equals', right: true};
    if (value === 'limit') this.data.limit = 100;
    if (['append', 'join', 'compare'].includes(value)) this.data.other = '${input.other}';
    if (['sort', 'sum', 'split', 'join'].includes(value)) this.data.key = '';
    if (value === 'join') this.data.other_key = '';
    this.invalid.emit(null); this.changed.emit();
  }
  addField(): void {
    if (!this.editable) return;
    this.data.fields ??= [];
    this.data.fields.push({name: `field-${this.data.fields.length + 1}`, value: ''});
    this.changed.emit();
  }
  removeField(index: number): void {
    if (!this.editable) return;
    this.data.fields?.splice(index, 1); this.changed.emit();
  }
  display(value: unknown): string {
    return typeof value === 'string' ? value : JSON.stringify(value) ?? '';
  }
  value(text: string): unknown {
    try { return JSON.parse(text); } catch { return text; }
  }
  get json(): string {
    return this.draft ?? JSON.stringify(this.step.type === 'foreach' ? this.step.foreach : this.step.data, null, 2);
  }
  setJSON(text: string): void {
    if (!this.editable) return;
    try {
      const value = JSON.parse(text);
      if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('object required');
      if (this.step.type === 'foreach') this.step.foreach = value;
      else this.step.data = value;
      this.invalid.emit(null); this.changed.emit();
    } catch { this.invalid.emit(text); }
  }
}
