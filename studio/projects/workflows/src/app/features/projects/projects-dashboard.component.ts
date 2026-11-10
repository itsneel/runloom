import { CommonModule } from '@angular/common';
import { Component, EventEmitter, Input, Output } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { UiIcon } from '@runloom/studio-platform';
import { DefinitionSummary, WorkflowProject } from '../../core/models/workflow.types';

@Component({
  selector: 'runloom-projects-dashboard',
  imports: [CommonModule, FormsModule, UiIcon],
  templateUrl: './projects-dashboard.component.html',
  styleUrl: './projects-dashboard.component.scss',
})
export class ProjectsDashboard {
  @Input() projects: WorkflowProject[] = [];
  @Input() workflows: DefinitionSummary[] = [];
  @Input() loading = false;
  @Input() error = '';
  @Input() busy = false;
  @Input() canEdit = false;
  @Input() showNavigationToggle = false;
  @Input() navigationExpanded = true;
  @Output() toggleNavigation = new EventEmitter<void>();
  @Output() openProject = new EventEmitter<string>();
  @Output() create = new EventEmitter<void>();
  @Output() rename = new EventEmitter<string>();
  @Output() deleteProject = new EventEmitter<string>();
  @Output() reload = new EventEmitter<void>();
  search = '';

  trackProject(_index: number, project: WorkflowProject): string { return project.id; }

  get visibleProjects(): WorkflowProject[] {
    const query = this.search.trim().toLowerCase();
    return this.projects.filter(project => `${project.name} ${project.id}`.toLowerCase().includes(query))
      .sort((a, b) => a.name.localeCompare(b.name));
  }
  count(id: string): number {
    return this.workflows.filter(workflow => (workflow.project_id ?? 'default') === id).length;
  }
}
