import { Component, ElementRef, EventEmitter, Input, Output, ViewChild } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { injectDialogs } from '@runloom/studio-platform';
import type {
  WorkflowDefinition,
  WorkflowSignalHandler,
} from '../../../../core/models/workflow.types';
import { allDefinitionSteps, availableID } from '../../model/workflow-tree';
import { slug } from '../../model/workflow-defaults';

@Component({
  selector: 'runloom-signal-handlers',
  imports: [CommonModule, FormsModule],
  templateUrl: './signal-handlers.component.html',
  styleUrl: './signal-handlers.component.scss',
})
export class SignalHandlers {
  @Input({ required: true }) definition!: WorkflowDefinition;
  @Input() editable = false;
  @Input() saving = false;
  @Output() changed = new EventEmitter<void>();
  @ViewChild('panel') panel?: ElementRef<HTMLDetailsElement>;
  readonly dialogs = injectDialogs();
  eventName = '';
  error = '';
  private issue(name: string, ignore?: WorkflowSignalHandler): string {
    if (
      !/^[a-z][a-z0-9.-]{0,127}$/.test(name) ||
      ['signal', 'approval', 'callback', 'resume'].includes(name)
    )
      return 'Use a unique event name such as customer.updated. Internal transport names are reserved.';
    if (
      (this.definition.signal_handlers ?? []).some((h) => h !== ignore && h.signal_name === name) ||
      allDefinitionSteps(this.definition).some((s) => s.type === 'signal' && s.signal_name === name)
    )
      return 'This signal name already belongs to another handler or wait node.';
    return '';
  }
  add(): void {
    if (!this.editable || this.saving) return;
    const name = this.eventName.trim();
    this.error = this.issue(name);
    if (this.error) return;
    if ((this.definition.signal_handlers?.length ?? 0) >= 20) {
      this.error = 'A workflow supports at most 20 signal handlers.';
      return;
    }
    const ids = new Set([
      ...allDefinitionSteps(this.definition).map((s) => s.id),
      ...(this.definition.signal_handlers ?? []).map((h) => h.id),
    ]);
    const id = availableID(slug('on-' + name), ids),
      end = availableID(id.slice(0, 50) + '-return', ids);
    (this.definition.signal_handlers ??= []).push({
      id,
      name,
      signal_name: name,
      steps: [{ id: end, name: 'Return from handler', type: 'end', with: {} }],
    });
    this.eventName = '';
    this.changed.emit();
    this.focusHandler(id);
  }
  edit(handler: WorkflowSignalHandler): void {
    this.error = this.issue(handler.signal_name, handler);
    this.changed.emit();
  }
  async remove(handler: WorkflowSignalHandler): Promise<void> {
    if (!this.editable || this.saving) return;
    const definition = this.definition;
    if (
      !(await this.dialogs.confirm(
        'Remove this signal handler and its action path from the draft?',
        { title: 'Remove signal handler', confirmText: 'Remove handler', danger: true },
      ))
    )
      return;
    if (definition !== this.definition || !this.editable || this.saving) return;
    this.definition.signal_handlers = (this.definition.signal_handlers ?? []).filter(
      (h) => h !== handler,
    );
    this.changed.emit();
  }
  focusHandler(id: string): void {
    const panel = this.panel?.nativeElement;
    if (!panel) return;
    panel.open = true;
    setTimeout(() =>
      panel.querySelector<HTMLInputElement>('[data-handler-id="' + id + '"]')?.focus(),
    );
  }
}
