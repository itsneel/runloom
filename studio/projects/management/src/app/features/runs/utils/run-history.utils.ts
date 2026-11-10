import type { RunRecord } from '../models/run.models';

export const runTagFields = [
  { key: 'project_id', label: 'Project' },
  { key: 'definition_version', label: 'Version' },
  { key: 'source', label: 'Source' },
  { key: 'is_test', label: 'Test run' },
  { key: 'definition_id', label: 'Workflow' },
  { key: 'org_id', label: 'Organization' },
  { key: 'team_id', label: 'Team' },
  { key: 'root_execution_id', label: 'Root execution' },
  { key: 'snapshot_id', label: 'Snapshot' },
  { key: 'namespace', label: 'Namespace' },
] as const;

export function runTags(run: RunRecord) {
  const metadata = run.metadata;
  if (!metadata) return [];
  return runTagFields.flatMap(({ key, label }) => {
    const value = metadata[key];
    return value === undefined || value === null || value === ''
      ? []
      : [{ key, label, value: String(value) }];
  });
}

export function runStatus(run: RunRecord): string {
  // Closed Temporal status wins over an older builder snapshot.
  return run.temporal_status === 'running' ? run.state?.status || 'running' : run.temporal_status;
}
export function isTest(run: RunRecord): boolean {
  return (
    run.metadata?.is_test === true ||
    ['draft_test', 'child_test'].includes(run.trigger_type) ||
    run.state?.['is_test'] === true
  );
}
export function duration(run: RunRecord): number {
  if (!run.completed_at) return -1;
  const value = Date.parse(run.completed_at) - Date.parse(run.created_at);
  return Number.isFinite(value) ? Math.max(0, value) : -1;
}
