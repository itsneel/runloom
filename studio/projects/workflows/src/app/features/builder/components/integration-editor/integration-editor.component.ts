import { CommonModule } from '@angular/common';
import { Component, EventEmitter, Input, Output, OnInit, OnDestroy, ChangeDetectorRef, inject } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { WorkflowApiService } from '../../../../core/api/workflow-api.service';
import type { WorkflowStep } from '../../../../core/models/workflow.types';
import type { IntegrationOperation } from '../../../../core/models/integration.types';

@Component({
  selector: 'runloom-integration-editor',
  imports: [CommonModule, FormsModule],
  templateUrl: './integration-editor.component.html',
  styleUrl: './integration-editor.component.scss',
})
export class IntegrationEditor implements OnInit, OnDestroy {
  @Input({required: true}) step!: WorkflowStep;
  @Input() editable = false;
  @Output() changed = new EventEmitter<void>();
  private readonly api = inject(WorkflowApiService);
  private readonly cd = inject(ChangeDetectorRef);
  private destroyed = false;
  operations: IntegrationOperation[] = [];
  loading = false;
  error = '';
  get config(): NonNullable<WorkflowStep['integration']> { return this.step.integration!; }
  get selected(): IntegrationOperation | undefined { return this.operations.find(op => op.id === this.config.operation); }
  ngOnInit(): void { void this.load(); }
  ngOnDestroy(): void { this.destroyed = true; }
  async load(): Promise<void> {
    this.loading = true; this.error = '';
    try {
      const catalog = await this.api.nodeCatalog();
      if (!Array.isArray(catalog.integrations)) throw new Error('Integration catalog unavailable');
      if (!this.destroyed) this.operations = catalog.integrations;
    } catch { if (!this.destroyed) this.error = 'Could not load integrations. Check the workflow service and retry.'; }
    finally { if (!this.destroyed) { this.loading = false; this.cd.markForCheck(); } }
  }
  select(id: string): void {
    if (!this.editable) return;
    this.config.operation = id; this.config.parameters = {};
    for (const p of this.selected?.parameters ?? []) this.config.parameters[p.name] = p.type === 'array' ? [] : '';
    this.changed.emit();
  }
  display(value: unknown): string { return typeof value === 'string' ? value : JSON.stringify(value) ?? ''; }
  set(name: string, type: string, text: string): void {
    if (!this.editable) return;
    let value: unknown = text;
    if (type === 'array') { try { value = JSON.parse(text); } catch { /* Preserve incomplete text in the draft; validation blocks publication. */ } }
    this.config.parameters[name] = value;
    this.changed.emit();
  }
}
