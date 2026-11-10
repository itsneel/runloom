import type { DefinitionSummary } from '../../core/models/workflow.types';

export type WorkflowStatusFilter = 'all' | 'published' | 'draft';

export function filterWorkflows(items: DefinitionSummary[], search: string, status: WorkflowStatusFilter): DefinitionSummary[] {
  const query = search.trim().toLowerCase();
  return items.filter(item => (!query || `${item.name} ${item.id}`.toLowerCase().includes(query)) &&
    (status === 'all' || (status === 'published' ? item.latest_version > 0 : item.has_draft)))
    .sort((a, b) => a.name.localeCompare(b.name) || a.id.localeCompare(b.id));
}
