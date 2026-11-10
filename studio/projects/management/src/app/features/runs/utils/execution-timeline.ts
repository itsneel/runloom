import type { NodeExecution, RunHistory } from '../models/run.models';

export function elapsed(ms: number): string {
  if (ms < 1000) return Math.round(ms) + ' ms';
  if (ms < 60_000) return Number((ms / 1000).toFixed(2)) + ' s';
  return Math.floor(ms / 60_000) + 'm ' + Math.floor((ms % 60_000) / 1000) + 's';
}

// Stored step results have neither exact times nor per-invocation inputs.
function storedNodes(history: RunHistory): NodeExecution[] {
  const state = history.run.state;
  const steps = state?.steps ?? {};
  const current = state?.current_step;
  const completed = new Set(state?.completed_steps ?? []);
  const ids = new Set([...completed, ...Object.keys(steps)]);
  if (current) ids.add(current.id);
  return [...ids].map((id, index) => {
    const step = steps[id];
    const active = current?.id === id;
    return {
      sequence: index + 1,
      step_id: id,
      name: active ? current.name : undefined,
      type: active ? current.type : undefined,
      status:
        step?.status ??
        (completed.has(id)
          ? 'completed'
          : history.run.temporal_status === 'running'
            ? 'running'
            : 'last observed'),
      output: step?.status === 'completed' ? step.output : undefined,
    };
  });
}

export function executionTimeline(history: RunHistory, observedAt = Date.now()) {
  const nodes = history.trace?.nodes ?? storedNodes(history);
  const starts = nodes
    .flatMap((n) => (n.started_at ? [Date.parse(n.started_at)] : []))
    .filter(Number.isFinite);
  const ends = nodes
    .flatMap((n) => (n.finished_at ? [Date.parse(n.finished_at)] : []))
    .filter(Number.isFinite);
  const first = starts.length ? Math.min(...starts) : 0;
  const open = history.run.temporal_status === 'running';
  const last = Math.max(first, ...starts, ...ends, ...(open && starts.length ? [observedAt] : []));
  const length = last - first;
  const rows = nodes.map((node) => {
    const start = Date.parse(node.started_at ?? '');
    const end = Date.parse(node.finished_at ?? '');
    const knownStart = Number.isFinite(start);
    const offset = knownStart ? start - first : 0;
    const ongoing = open && node.status === 'running';
    const duration = Number.isFinite(end) ? Math.max(0, end - start) : ongoing ? last - start : NaN;
    return {
      ...node,
      status: !open && node.status === 'running' ? 'last observed running' : node.status,
      offset: elapsed(offset),
      duration: Number.isFinite(duration) ? elapsed(duration) : undefined,
      left: length ? (offset / length) * 100 : 0,
      width: length && Number.isFinite(duration) ? (duration / length) * 100 : 0,
    };
  });
  return { rows, span: elapsed(length), timed: starts.length > 0 };
}
