import { CommonModule } from '@angular/common';
import { Component, EventEmitter, Input, Output } from '@angular/core';
import type { VariableHint } from '../../../core/models/prompt.types';

@Component({
  selector: 'runloom-variable-editor',
  imports: [CommonModule],
  templateUrl: './variable-editor.component.html',
  styleUrl: './variable-editor.component.scss',
})
export class VariableEditor {
  @Input() value = '';
  @Input() label = 'Expression';
  @Input() hints: VariableHint[] = [];
  @Input() readonly = false;
  @Input() rows = 3;
  @Output() valueChange = new EventEmitter<string>();
  open = false;
  selected = 0;
  start = 0;
  end = 0;
  matches: VariableHint[] = [];
  update(area: HTMLTextAreaElement, force = false): void {
    const prefix = area.value.slice(0, area.selectionStart),
      match = /\$\{([^}]*)$/.exec(prefix);
    this.open = !this.readonly && (!!match || force);
    this.start = match ? prefix.lastIndexOf('${') : area.selectionStart;
    this.end = area.selectionStart;
    const query = match?.[1].toLowerCase() || '';
    this.matches = this.hints.filter((h) => h.path.toLowerCase().includes(query)).slice(0, 40);
    this.selected = 0;
  }
  input(area: HTMLTextAreaElement): void {
    this.value = area.value;
    this.valueChange.emit(area.value);
    this.update(area);
  }
  key(event: KeyboardEvent, area: HTMLTextAreaElement): void {
    if ((event.ctrlKey || event.metaKey) && event.code === 'Space') {
      event.preventDefault();
      this.update(area, true);
      return;
    }
    if (!this.open) return;
    if (event.key === 'Escape') {
      event.preventDefault();
      this.open = false;
    } else if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      this.selected =
        (this.selected + (event.key === 'ArrowDown' ? 1 : -1) + this.matches.length) %
        Math.max(1, this.matches.length);
    } else if ((event.key === 'Enter' || event.key === 'Tab') && this.matches.length) {
      event.preventDefault();
      this.choose(this.matches[this.selected], area);
    }
  }
  choose(hint: VariableHint, area: HTMLTextAreaElement): void {
    const token = '${' + hint.path + '}',
      end = this.value[this.end] === '}' ? this.end + 1 : this.end;
    this.value = this.value.slice(0, this.start) + token + this.value.slice(end);
    this.valueChange.emit(this.value);
    area.value = this.value;
    this.open = false;
    area.focus();
    area.setSelectionRange(this.start + token.length, this.start + token.length);
  }
}
