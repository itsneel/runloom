import { CommonModule } from '@angular/common';
import { Component, Input, OnChanges } from '@angular/core';
import type { RunHistory } from '../../models/run.models';
import { executionTimeline } from '../../utils/execution-timeline';

@Component({
  selector: 'runloom-execution-timeline',
  imports: [CommonModule],
  templateUrl: './execution-timeline.component.html',
  styleUrl: './execution-timeline.component.scss',
})
export class ExecutionTimelineComponent implements OnChanges {
  @Input({ required: true }) history!: RunHistory;
  timeline!: ReturnType<typeof executionTimeline>;
  selected = 0;
  private runId = '';
  ngOnChanges(): void {
    this.timeline = executionTimeline(this.history);
    if (
      this.runId !== this.history.run.temporal_run_id ||
      !this.timeline.rows.some((n) => n.sequence === this.selected)
    ) {
      this.selected =
        (this.timeline.rows.find((n) => n.status === 'failed') ?? this.timeline.rows[0])
          ?.sequence ?? 0;
    }
    this.runId = this.history.run.temporal_run_id;
  }
  get node() {
    return this.timeline.rows.find((n) => n.sequence === this.selected);
  }
  get parent() {
    const parent = this.node?.parent;
    return parent ? this.timeline.rows.find((n) => n.sequence === parent) : undefined;
  }
}
