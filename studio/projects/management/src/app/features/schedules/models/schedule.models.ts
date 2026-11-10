export interface ScheduledDefinition {
  id: string;
  name: string;
  latest_version: number;
  published_kind?: 'main' | 'child';
}
export interface ScheduleConfig {
  name: string;
  definition_id: string;
  version: number;
  input: Record<string, unknown>;
  interval_seconds?: number;
  cron?: string;
  time_zone?: string;
  at?: string;
  paused: boolean;
}
export interface WorkflowSchedule extends ScheduleConfig {
  id: string;
  next_runs: string[];
  recent_runs: string[];
}
export interface ScheduleDraft {
  name: string;
  definitionId: string;
  version: number;
  mode: 'interval' | 'cron' | 'once';
  minutes: number;
  cron: string;
  timeZone: string;
  at: string;
  input: string;
  paused: boolean;
}
