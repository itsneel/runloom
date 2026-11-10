export interface RunRecord {
  metadata?: {
    namespace: string;
    org_id: string;
    team_id: string;
    project_id: string;
    definition_id: string;
    definition_version: number;
    is_test: boolean;
    source: string;
    root_execution_id: string;
    snapshot_id: string;
  };
  workflow_id: string;
  temporal_run_id: string;
  definition_id: string;
  definition_version: number;
  temporal_status: string;
  trigger_type: string;
  created_at: string;
  updated_at: string;
  completed_at?: string;
  input: unknown;
  output?: unknown;
  state?: {
    status?: string;
    error?: unknown;
    steps?: Record<string, { status: string; output?: unknown }>;
    completed_steps?: string[];
    current_step?: { id: string; name?: string; type?: string } | null;
    [key: string]: unknown;
  } | null;
}
export interface RunAudit {
  event_id: string;
  event_type: string;
  actor: string;
  step_id?: string;
  created_at: string;
  payload: unknown;
}
export interface RunHistory {
  run: RunRecord;
  events: RunAudit[];
  trace?: { nodes: NodeExecution[]; omitted?: number };
}

export interface NodeExecution {
  sequence: number;
  parent?: number;
  step_id: string;
  name?: string;
  type?: string;
  scope?: string;
  status: string;
  started_at?: string;
  finished_at?: string;
  input?: unknown;
  output?: unknown;
  error?: string;
  input_omitted?: boolean;
  output_omitted?: boolean;
}
