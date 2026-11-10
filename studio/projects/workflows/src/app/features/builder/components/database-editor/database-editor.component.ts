import { CommonModule } from '@angular/common';
import {
  Component,
  ChangeDetectorRef,
  EventEmitter,
  Input,
  OnChanges,
  OnDestroy,
  Output,
  SimpleChanges,
  inject,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { HttpClient } from '@angular/common/http';
import { firstValueFrom } from 'rxjs';
import { AuthService } from '@runloom/studio-platform';
import { DatabaseOperation, WorkflowStep } from '../../../../core/models/workflow.types';

interface Connection {
  id: string;
  name: string;
  provider: string;
  access: string;
  enabled: boolean;
}
interface Table {
  schema: string;
  name: string;
  inferred?: boolean;
  columns: Array<{ name: string; type: string; nullable: boolean }>;
}
@Component({
  selector: 'runloom-database-editor',
  imports: [CommonModule, FormsModule],
  templateUrl: './database-editor.component.html',
  styleUrl: './database-editor.component.scss',
})
export class DatabaseEditor implements OnChanges, OnDestroy {
  @Input({ required: true }) step!: WorkflowStep;
  @Input() editable = false;
  @Input() draft: string | null = null;
  @Output() changed = new EventEmitter<void>();
  @Output() invalid = new EventEmitter<string | null>();
  private readonly http = inject(HttpClient);
  readonly auth = inject(AuthService);
  private readonly cd = inject(ChangeDetectorRef);
  private generation = 0;
  connections: Connection[] = [];
  tables: Table[] = [];
  busy = false;
  error = '';
  get op(): DatabaseOperation {
    return this.step.database!;
  }
  get connection(): Connection | undefined {
    return this.connections.find((c) => c.id === this.op.connection_id);
  }
  get tableKey(): string {
    return `${this.op.schema || ''}.${this.op.table || ''}`;
  }
  get selectedTable(): Table | undefined {
    return this.tables.find((t) => `${t.schema}.${t.name}` === this.tableKey);
  }
  get endpoint(): string {
    return `/api/v1/studio/orgs/${encodeURIComponent(this.auth.organizationId())}/teams/${encodeURIComponent(this.auth.teamId())}/connections`;
  }
  get json(): string {
    return (
      this.draft ??
      JSON.stringify(
        {
          parameters: this.op.parameters || [],
          filter: this.op.filter || {},
          values: this.op.values || {},
          pipeline: this.op.pipeline || [],
        },
        null,
        2,
      )
    );
  }
  ngOnChanges(changes: SimpleChanges): void {
    if (!changes['step']) return;
    const g = ++this.generation;
    this.tables = [];
    this.error = '';
    this.busy = false;
    void this.load(g);
  }
  ngOnDestroy(): void {
    this.generation++;
  }
  private async load(g: number): Promise<void> {
    try {
      const r = await firstValueFrom(this.http.get<{ items: Connection[] }>(this.endpoint));
      if (g === this.generation) {
        this.connections = r.items;
      }
    } catch {
      if (g === this.generation)
        this.error = 'Could not load connections. Create one in Management → Connections.';
    } finally {
      this.cd.markForCheck();
    }
  }
  connectionChanged(): void {
    this.tables = [];
    this.op.schema = '';
    this.op.table = '';
    this.op.operation = 'select';
    delete this.op.query;
    this.op.parameters = [];
    this.op.values = {};
    this.op.filter = {};
    this.op.pipeline = [];
    this.invalid.emit(null);
    this.changed.emit();
  }
  operationChanged(): void {
    delete this.op.query;
    this.op.parameters = [];
    this.op.values = {};
    this.op.filter = {};
    this.op.pipeline = [];
    if (this.op.operation === 'sql') this.op.query = 'SELECT 1 AS connected';
    this.invalid.emit(null);
    this.changed.emit();
  }
  async discover(): Promise<void> {
    if (!this.op.connection_id || this.busy) return;
    const g = this.generation,
      id = this.op.connection_id;
    this.busy = true;
    this.error = '';
    try {
      const r = await firstValueFrom(
        this.http.get<{ tables: Table[] }>(`${this.endpoint}/${encodeURIComponent(id)}/schema`),
      );
      if (g === this.generation && id === this.op.connection_id) this.tables = r.tables;
    } catch {
      if (g === this.generation)
        this.error =
          'Schema discovery failed. Check the saved connection, network and permissions in Management.';
    } finally {
      if (g === this.generation) this.busy = false;
      this.cd.markForCheck();
    }
  }
  tableChanged(key: string): void {
    const t = this.tables.find((t) => `${t.schema}.${t.name}` === key);
    if (!t) return;
    this.op.table = t.name;
    this.op.schema = t.schema;
    this.op.values = {};
    this.op.filter = {};
    this.invalid.emit(null);
    this.changed.emit();
  }
  fieldValue(name: string): string {
    const v = this.op.values?.[name];
    return v === undefined ? '' : typeof v === 'string' ? v : JSON.stringify(v);
  }
  setField(name: string, value: string): void {
    this.op.values ??= {};
    if (value === '') {
      delete this.op.values[name];
    } else {
      try {
        this.op.values[name] = JSON.parse(value);
      } catch {
        this.op.values[name] = value;
      }
    }
    this.invalid.emit(null);
    this.changed.emit();
  }
  setJSON(raw: string): void {
    try {
      const v = JSON.parse(raw);
      if (
        !v ||
        typeof v !== 'object' ||
        Array.isArray(v) ||
        Object.keys(v).some((k) => !['parameters', 'filter', 'values', 'pipeline'].includes(k)) ||
        !Array.isArray(v.parameters ?? []) ||
        !Array.isArray(v.pipeline ?? []) ||
        !v.filter ||
        Array.isArray(v.filter) ||
        typeof v.filter !== 'object' ||
        !v.values ||
        Array.isArray(v.values) ||
        typeof v.values !== 'object'
      )
        throw Error();
      this.op.parameters = v.parameters ?? [];
      this.op.filter = v.filter;
      this.op.values = v.values;
      this.op.pipeline = v.pipeline ?? [];
      this.error = '';
      this.invalid.emit(null);
      this.changed.emit();
    } catch {
      this.error =
        'Use a JSON object containing parameters (array), filter (object), values (object) and pipeline (array).';
      this.invalid.emit(raw);
    }
  }
}
