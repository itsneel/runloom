import type { ScheduleConfig, ScheduleDraft } from '../models/schedule.models';

export function scheduleConfig(draft: ScheduleDraft, now = Date.now()): ScheduleConfig {
  if (!draft.name.trim()) throw new Error('Enter a schedule name.');
  if (!draft.definitionId || !Number.isInteger(draft.version) || draft.version < 1)
    throw new Error('Choose a published main workflow and a positive integer version.');
  let input: unknown;
  try {
    input = JSON.parse(draft.input);
  } catch {
    throw new Error('Schedule input must be valid JSON.');
  }
  if (!input || typeof input !== 'object' || Array.isArray(input))
    throw new Error('Schedule input must be a JSON object.');
  const config: ScheduleConfig = {
    name: draft.name.trim(),
    definition_id: draft.definitionId,
    version: draft.version,
    input: input as Record<string, unknown>,
    paused: draft.paused,
  };
  if (draft.mode === 'interval') {
    if (!Number.isInteger(draft.minutes) || draft.minutes < 1 || draft.minutes > 525600)
      throw new Error('The interval must be between 1 and 525600 whole minutes.');
    config.interval_seconds = draft.minutes * 60;
  } else if (draft.mode === 'cron') {
    if (draft.cron.trim().split(/\s+/).length !== 5)
      throw new Error('Use a five-field cron expression: minute hour day month weekday.');
    try {
      new Intl.DateTimeFormat('en', { timeZone: draft.timeZone }).format();
    } catch {
      throw new Error('Enter a valid IANA time zone, such as UTC or Asia/Kolkata.');
    }
    config.cron = draft.cron.trim();
    config.time_zone = draft.timeZone;
  } else {
    const at = new Date(draft.at).getTime();
    if (!Number.isFinite(at) || at <= now) throw new Error('Choose a future date and time.');
    config.at = new Date(at).toISOString();
  }
  return config;
}
