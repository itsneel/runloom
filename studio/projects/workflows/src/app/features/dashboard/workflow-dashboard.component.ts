import { CommonModule } from '@angular/common';
import { Component, EventEmitter, Input, Output } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { UiIcon } from '@runloom/studio-platform';
import { DefinitionSummary, WorkflowProject } from '../../core/models/workflow.types';
import { filterWorkflows, WorkflowStatusFilter } from './workflow-dashboard.model';

@Component({
  selector: 'runloom-workflow-dashboard',
  imports: [CommonModule, FormsModule, UiIcon],
  templateUrl: './workflow-dashboard.component.html',
  styleUrl: './workflow-dashboard.component.scss',
})
export class WorkflowDashboard {
  @Input() workflows: DefinitionSummary[] = [];
  @Input() projectName = 'General';
  @Input() projects: WorkflowProject[] = [];
  @Input() projectId = 'default';
  @Input() showProjectsBack = false;
  @Input() loading = false;
  @Input() error = '';
  @Input() openingId = '';
  @Input() canCreate = false;
  @Input() canDelete = false;
  @Input() busy = false;
  @Input() showNavigationToggle = false;
  @Input() navigationExpanded = true;
  @Output() toggleNavigation = new EventEmitter<void>();
  @Output() details = new EventEmitter<string>();
  @Output() create = new EventEmitter<void>();
  @Output() reload = new EventEmitter<void>();
  @Output() selectProject = new EventEmitter<string>();
  @Output() renameProject = new EventEmitter<void>();
  @Output() example = new EventEmitter<'review' | 'parallel-subworkflows'>();
  @Output() backToProjects = new EventEmitter<void>();
  @Output() deleteWorkflow = new EventEmitter<string>();
  draftOnly(item: DefinitionSummary): boolean { return item.has_draft && !(item.latest_version > 0) && !item.published_versions?.length; }
  search = '';
  selectedExample: 'review' | 'parallel-subworkflows' = 'review';
  status: WorkflowStatusFilter = 'all';
  get visibleWorkflows(): DefinitionSummary[] { return filterWorkflows(this.workflows, this.search, this.status); }
  get publishedCount(): number { return this.workflows.filter(item => item.latest_version > 0).length; }
  get draftCount(): number { return this.workflows.filter(item => item.has_draft).length; }
  trackWorkflow(_index: number, item: DefinitionSummary): string { return item.id; }
  clearFilters(): void { this.search = ''; this.status = 'all'; }
}
