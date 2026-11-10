import type { StepType, WorkflowStep } from '../../../core/models/workflow.types';
export interface PaletteItem {
  type: StepType;
  label: string;
  icon: string;
  summary: string;
  tone: string;
}

  export const workflowPalette: PaletteItem[] = [
  { type: 'integration', label: 'Integration', icon: '⇄', summary: 'Google Sheets, Gmail, Drive, Calendar, Slack, Teams, Outlook, Notion and HubSpot', tone: 'cyan' },
  { type: 'transform', label: 'Transform', icon: '⇄', summary: 'Map fields, apply defaults and convert types without code', tone: 'cyan' },
  { type: 'collection', label: 'Collection', icon: '▤', summary: 'Filter, sort, limit, deduplicate, split, compare, count or sum records', tone: 'blue' },
  { type: 'merge', label: 'Merge', icon: '⑂', summary: 'Append arrays or join records by matching fields', tone: 'violet' },
  { type: 'foreach', label: 'For Each', icon: '↻', summary: 'Process each item with bounded concurrency and collect results', tone: 'orange' },
  { type: 'fail', label: 'Fail workflow', icon: '!', summary: 'Stop with an explicit failure reason', tone: 'pink' },
  { type: 'database', label: 'Database', icon: '▤', summary: 'Query a database using a saved team connection', tone: 'cyan' },
  { type: 'email', label: 'Email', icon: '✉', summary: 'Send a message using configured SMTP', tone: 'blue' },
  { type: 'code', label: 'Custom Code', icon: '{ }', summary: 'Transform JSON with JavaScript', tone: 'amber' },
  { type: 'http', label: 'External API', icon: '⇄', summary: 'Call an HTTPS API using team secrets', tone: 'cyan' },
  { type: 'end', label: 'End workflow', icon: '■', summary: 'Finish this workflow successfully now', tone: 'green' },
  { type: 'activity', label: 'Action', icon: '↗', summary: 'Do work in another system', tone: 'violet' },
  { type: 'llm', label: 'AI / LLM', icon: '✦', summary: 'Generate or analyze with a model', tone: 'ai' },
  { type: 'decision', label: 'AI Decision', icon: '✦', summary: 'Choose actions with AI, once or repeatedly', tone: 'ai' },
  { type: 'subworkflow', label: 'Subworkflow', icon: '↳', summary: 'Call a published workflow and use its result', tone: 'violet' },
  { type: 'wait', label: 'Wait', icon: '◷', summary: 'Continue automatically after time', tone: 'blue' },
  { type: 'callback', label: 'Callback', icon: '↩', summary: 'Wait for data from a system', tone: 'cyan' },
  { type: 'signal', label: 'Signal', icon: 'ϟ', summary: 'Wait for a named business event', tone: 'indigo' },
  { type: 'approval', label: 'Approval', icon: '✓', summary: 'Wait for a human decision', tone: 'green' },
  { type: 'pause', label: 'Pause', icon: 'Ⅱ', summary: 'Stop until an operator resumes', tone: 'amber' },
  { type: 'condition', label: 'Decision', icon: '◇', summary: 'Choose a path using data', tone: 'pink' },
  { type: 'parallel', label: 'Parallel', icon: '⑂', summary: 'Run independent paths together', tone: 'orange' },
];

export function stepLabel(step: WorkflowStep): string {
  return workflowPalette.find((item) => item.type === step.type)?.label ?? step.type;
}

export function stepIcon(step: WorkflowStep): string {
  return workflowPalette.find((item) => item.type === step.type)?.icon ?? '•';
}

export function stepTone(step: WorkflowStep): string {
  return workflowPalette.find((item) => item.type === step.type)?.tone ?? 'violet';
}

export function stepSummary(step: WorkflowStep): string {
  switch (step.type) {
    case 'integration': return step.integration?.operation || 'Choose a business integration';
    case 'transform': return `${step.data?.fields?.length ?? 0} mapped fields`;
    case 'collection': case 'merge': return step.data?.operation || 'Choose an operation';
    case 'foreach': return `${step.foreach?.concurrency ?? 1} concurrent items · up to 100 items`;
    case 'fail': return step.message || 'Set the failure reason';
    case 'email': return step.email?.subject || 'Set subject and recipients';
    case 'code': return 'JavaScript · JSON input → JSON output';
    case 'http': return `${step.http?.method || 'GET'} ${step.http?.url || 'Set API URL'}`;
    case 'database': return `${step.database?.operation || 'select'} · ${step.database?.connection_id || 'Choose a connection'}`;
    case 'end': return 'Workflow ends here — no following nodes run';
    case 'activity': return step.activity ?? 'Choose an action';
    case 'llm': return `${step.model || 'default'} · ${step.prompt || 'Add a prompt'}`;
    case 'decision': return `${step.decision?.mode === 'agent' ? 'Decide repeatedly' : step.decision?.mode === 'multiple' ? 'Choose several' : 'Choose one'} · ${step.branches?.length ?? 0} actions`;
    case 'subworkflow': return `${step.definition_id || 'Choose a workflow'} · ${step.version ? 'v' + step.version : 'Draft — publish and pin'}`;
    case 'wait': return `Continue automatically after ${step.seconds ?? 0} seconds`;
    case 'callback': return `Wait for “${step.callback_key ?? 'external.event'}”`;
    case 'signal': return `Wait for signal “${step.signal_name ?? 'event.ready'}”`;
    case 'approval': return step.prompt ?? 'Wait for approval';
    case 'pause': return step.message ?? 'Wait for an operator';
    case 'condition': return 'Route using a condition';
    case 'parallel': return `${step.branches?.length ?? 0} paths run together`;
  }
}
