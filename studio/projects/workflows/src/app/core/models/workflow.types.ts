import type { PromptTemplate } from './prompt.types';
export type StepType =
  | 'transform'
  | 'collection'
  | 'merge'
  | 'foreach'
  | 'fail'
  | 'integration'
  | 'database'
  | 'email'
  | 'code'
  | 'http'
  | 'end'
  | 'activity'
  | 'llm'
  | 'decision'
  | 'subworkflow'
  | 'wait'
  | 'callback'
  | 'approval'
  | 'pause'
  | 'signal'
  | 'condition'
  | 'parallel';

export interface RetryPolicy {
  maximum_attempts: number;
  initial_interval_seconds?: number;
  backoff_coefficient?: number;
  maximum_interval_seconds?: number;
}

export interface WorkflowStep {
  integration?: { operation: string; secret: string; parameters: Record<string, unknown> };
  llm_task?: {
    kind: 'extract' | 'classify' | 'summarize' | 'sentiment';
    labels?: string[];
    fields?: Array<{ name: string; type: string; description?: string }>;
  };
  data?: DataOperation;
  foreach?: { items: unknown; output: unknown; concurrency: number };
  database?: DatabaseOperation;
  prompt_template?: PromptTemplate;
  prompt_ref?: { id: string; version: number; hash?: string };
  prompt_inputs?: Record<string, unknown>;
  output_schema?: Record<string, unknown>;
  decision?: {
    mode: 'single' | 'multiple' | 'agent';
    execution: 'sequential' | 'parallel';
    max_rounds: number;
    max_actions: number;
  };
  email?: {
    to: string[];
    cc?: string[];
    bcc?: string[];
    reply_to?: string;
    subject: string;
    text: string;
    html?: string;
  };
  code?: { runtime: 'quickjs-ng-0.11.0-v1'; source: string };
  http?: {
    url: string;
    method: string;
    headers?: Record<string, string>;
    secret_headers?: Record<string, { secret: string; prefix?: string }>;
    body?: unknown;
  };
  definition_id?: string;
  version?: number;
  id: string;
  name?: string;
  type: StepType;
  activity?: string;
  with?: Record<string, unknown>;
  model?: string;
  system_prompt?: string;
  temperature?: number;
  max_tokens?: number;
  timeout_seconds?: number;
  retry?: RetryPolicy;
  seconds?: number;
  callback_key?: string;
  prompt?: string;
  message?: string;
  signal_name?: string;
  on_timeout?: 'reject' | 'fail' | 'continue' | 'resume';
  if?: { left: unknown; operator: string; right?: unknown };
  then?: WorkflowStep[];
  else?: WorkflowStep[];
  branches?: Array<{ id: string; description?: string; steps: WorkflowStep[] }>;
}

export interface DataOperation {
  operation:
    | 'map'
    | 'filter'
    | 'sort'
    | 'limit'
    | 'unique'
    | 'count'
    | 'sum'
    | 'split'
    | 'compare'
    | 'append'
    | 'join';
  source?: unknown;
  other?: unknown;
  fields?: Array<{
    name: string;
    value: unknown;
    type?: string;
    use_default?: boolean;
    default?: unknown;
  }>;
  keep_source?: boolean;
  predicate?: { left: unknown; operator: string; right?: unknown };
  key?: string;
  other_key?: string;
  descending?: boolean;
  limit?: number;
}

export interface DatabaseOperation {
  connection_id: string;
  operation: 'select' | 'insert' | 'update' | 'delete' | 'sql' | 'aggregate';
  schema?: string;
  table?: string;
  query?: string;
  parameters?: unknown[];
  values?: Record<string, unknown>;
  filter?: Record<string, unknown>;
  pipeline?: Array<Record<string, unknown>>;
  limit?: number;
}

export interface WorkflowSignalHandler {
  id: string;
  name?: string;
  signal_name: string;
  input_schema?: Record<string, unknown>;
  steps: WorkflowStep[];
}

export interface WorkflowDefinition {
  signal_handlers?: WorkflowSignalHandler[];
  layout?: { positions: Record<string, { x: number; y: number }> };
  kind?: 'main' | 'child';
  schema_version: '1.0';
  id: string;
  name: string;
  description: string;
  version: number;
  trigger: WorkflowTrigger;
  input_schema: Record<string, unknown>;
  steps: WorkflowStep[];
  output: unknown;
}

export type WorkflowTrigger =
  { type: 'manual' } | { type: 'webhook'; path: string; method: 'POST' };

export interface DefinitionSummary {
  published_versions?: number[];
  published_kind?: 'main' | 'child';
  project_id: string;
  id: string;
  name: string;
  latest_version: number;
  has_draft: boolean;
}

export interface WorkflowProject {
  id: string;
  name: string;
  created_at?: string;
  updated_at?: string;
}

export interface DefinitionCommit {
  commit_id: string;
  definition_id: string;
  sequence: number;
  kind: 'save' | 'publish' | 'restore';
  actor: string;
  message: string;
  published_version?: number | null;
  restored_from_commit_id?: string | null;
  created_at: string;
  body?: WorkflowDefinition;
}

export interface DefinitionHistory {
  definition_id: string;
  latest_version?: number | null;
  published_versions: number[];
  commits: DefinitionCommit[];
}

export interface RunState {
  is_test?: boolean;
  child_workflows?: Record<string, string>;
  parent_workflow_id?: string;
  schedule_id?: string;
  status: string;
  current_step?: { id: string; name: string; type: StepType } | null;
  completed_steps?: string[];
  waiting_approval?: { step_id: string; prompt: string } | null;
  waiting_approvals?: Record<string, { step_id: string; prompt: string }>;
  waiting_callback?: { step_id: string; callback_key: string; prompt: string } | null;
  waiting_callbacks?: Record<string, { step_id: string; callback_key: string; prompt: string }>;
  paused_step?: PauseGate | null;
  paused_steps?: Record<string, PauseGate>;
  signal_handlers?: Record<
    string,
    {
      id: string;
      signal_name: string;
      status: string;
      accepting: boolean;
      received: number;
      completed: number;
      pending: number;
      invocation_id?: string;
      error?: string;
      output?: unknown;
    }
  >;
  waiting_signal?: { step_id: string; signal_name: string; prompt: string } | null;
  waiting_signals?: Record<string, { step_id: string; signal_name: string; prompt: string }>;
  output?: unknown;
  error?: string | null;
}

export interface PauseGate {
  step_id: string;
  message: string;
  budget?: {
    reason: string;
    policy_name?: string;
    project_name?: string;
    spend_usd?: string;
    limit_usd?: string;
    resets_at?: string;
    incomplete: boolean;
  };
}

export interface RunStatus {
  workflow_id: string;
  temporal_status: string;
  state?: RunState | null;
}

export interface WorkflowRunRecord {
  llm_cost?: LLMCostSummary;
  workflow_id: string;
  temporal_run_id: string;
  definition_id: string;
  definition_version: number;
  trigger_type:
    'manual' | 'webhook' | 'schedule' | 'child' | 'child_test' | 'draft_test' | 'consumer';
  input: Record<string, unknown>;
  temporal_status: string;
  state?: RunState | null;
  output?: unknown;
  created_at: string;
  updated_at: string;
  completed_at?: string | null;
}

export interface LLMCostSummary {
  nodes: Array<{
    workflow_id: string;
    step_id: string;
    requests: number;
    incomplete: number;
    estimated_usd: string;
  }>;
  models: string[];
  estimated_usd: string;
  direct_usd: string;
  child_usd: string;
  requests: number;
  input_tokens: number;
  output_tokens: number;
  unknown: number;
  unpriced: number;
  pending: number;
  complete: boolean;
  truncated: boolean;
}
export interface LLMUsageEntry {
  id: string;
  workflow_id: string;
  temporal_run_id: string;
  definition_id: string;
  definition_version: number;
  step_id: string;
  activity_id: string;
  attempt: number;
  is_test: boolean;
  provider: string;
  requested_model: string;
  model: string;
  provider_request_id?: string;
  started_at: string;
  finished_at?: string;
  duration_ms: number;
  status: string;
  accounting: string;
  input_tokens: number;
  output_tokens: number;
  cached_tokens: number;
  reasoning_tokens: number;
  cost_usd?: string;
  pricing?: {
    version: string;
    input_per_million: string;
    output_per_million: string;
    cached_input_per_million?: string;
  };
}
export interface LLMUsagePage {
  summary: LLMCostSummary;
  entries: LLMUsageEntry[];
  next_cursor: string;
  currency: 'USD';
  scope: string;
}

export interface AuditEvent {
  event_id: string;
  workflow_id?: string;
  definition_id?: string;
  event_type: string;
  step_id?: string;
  actor: string;
  payload: unknown;
  created_at: string;
}

export interface RunHistory {
  run: WorkflowRunRecord;
  events: AuditEvent[];
}
