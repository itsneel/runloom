import type { WorkflowStep, WorkflowSignalHandler } from '../../../../core/models/workflow.types';
import type { StepLocation } from '../../model/workflow-tree';
export interface Point {
  x: number;
  y: number;
}
export interface CanvasNode extends Point {
  key: string;
  kind: 'step' | 'trigger' | 'join' | 'finish' | 'handler';
  handler?: WorkflowSignalHandler;
  handlerId?: string;
  step?: WorkflowStep;
  location?: StepLocation;
  label?: string;
}
export interface CanvasEdge {
  key: string;
  from: string;
  to: string;
  label: string;
  insert?: StepLocation;
}
export interface CanvasScene {
  nodes: CanvasNode[];
  edges: CanvasEdge[];
}
export const NODE_WIDTH = 190,
  NODE_HEIGHT = 136;
const DX = 300,
  DY = 210;

export function buildCanvas(
  steps: WorkflowStep[],
  positions: Record<string, Point> = {},
  handlers: WorkflowSignalHandler[] = [],
): CanvasScene {
  const nodes: CanvasNode[] = [{ key: '@trigger', kind: 'trigger', x: 0, y: 0 }],
    edges: CanvasEdge[] = [];
  const branches = (step: WorkflowStep) =>
    step.type === 'parallel' || step.type === 'decision' || step.type === 'foreach'
      ? (step.branches ?? []).map((b) => ({
          label: step.type === 'foreach' ? 'For each item' : b.id,
          steps: b.steps,
        }))
      : step.type === 'condition'
        ? [
            { label: 'Yes', steps: step.then ?? [] },
            { label: 'No', steps: step.else ?? [] },
          ]
        : [];
  const height = (path: WorkflowStep[]): number =>
    Math.max(
      DY,
      ...path.map((step) => branches(step).reduce((sum, b) => sum + height(b.steps), 0)),
    );
  const edge = (from: string, to: string, label = '', insert?: StepLocation) =>
    edges.push({ key: `edge-${edges.length}`, from, to, label, insert });
  const path = (
    list: WorkflowStep[],
    start: string,
    startX: number,
    y: number,
    label = '',
    handlerId?: string,
  ): { key: string; x: number; ended: boolean } => {
    let previous = start,
      x = startX;
    for (let index = 0; index < list.length; index++) {
      const step = list[index];
      nodes.push({
        key: step.id,
        kind: 'step',
        handlerId,
        step,
        location: { steps: list, index },
        x,
        y,
      });
      edge(previous, step.id, index === 0 ? label : '', { steps: list, index });
      previous = step.id;
      const paths = branches(step);
      if (paths.length) {
        const total = paths.reduce((sum, branch) => sum + height(branch.steps), 0);
        let top = y - total / 2;
        const ends = paths.map((branch) => {
          const h = height(branch.steps),
            center = top + h / 2;
          top += h;
          return {
            branch,
            result: path(branch.steps, step.id, x + DX, center, branch.label, handlerId),
          };
        });
        x = Math.max(x + DX, ...ends.map((end) => end.result.x));
        const join = `@join:${step.id}`;
        nodes.push({
          key: join,
          kind: 'join',
          x,
          y,
          step,
          label:
            step.type === 'foreach'
              ? 'Collect · all items'
              : step.type === 'parallel'
                ? 'Join · wait for all paths'
                : step.type === 'decision'
                  ? step.decision?.mode === 'agent'
                    ? 'Decide again · until finished'
                    : 'Join · selected actions'
                  : 'Continue · selected route',
        });
        for (const { branch, result } of ends)
          if (!result.ended)
            edge(result.key, join, branch.steps.length ? `End of ${branch.label}` : branch.label, {
              steps: branch.steps,
              index: branch.steps.length,
            });
        previous = join;
      }
      x += DX;
      if (step.type === 'end' || step.type === 'fail') return { key: previous, x, ended: true };
    }
    return { key: previous, x, ended: false };
  };
  const last = path(steps, '@trigger', DX, 0);
  if (!last.ended) {
    nodes.push({ key: '@finish', kind: 'finish', x: last.x, y: 0, label: 'Workflow ends' });
    edge(last.key, '@finish', '', { steps, index: steps.length });
  }
  let bottom = Math.max(...nodes.map((node) => node.y + NODE_HEIGHT));
  for (const handler of handlers) {
    const span = height(handler.steps),
      y = bottom + DY + span / 2,
      key = '@handler:' + handler.id;
    nodes.push({
      key,
      kind: 'handler',
      handler,
      handlerId: handler.id,
      x: 0,
      y,
      label: handler.signal_name,
    });
    const end = path(handler.steps, key, DX, y, '', handler.id);
    if (!end.ended) {
      const finish = '@return:' + handler.id;
      nodes.push({
        key: finish,
        kind: 'finish',
        handlerId: handler.id,
        x: end.x,
        y,
        label: 'Return from handler',
      });
      edge(end.key, finish, '', { steps: handler.steps, index: handler.steps.length });
    }
    bottom = Math.max(...nodes.map((node) => node.y + NODE_HEIGHT));
  }
  for (const node of nodes) {
    const point = positions[node.key];
    if (point && Number.isFinite(point.x) && Number.isFinite(point.y)) {
      node.x = point.x;
      node.y = point.y;
    }
  }
  // A partially saved layout can put a newly inserted node on an older card.
  // Never combine those coordinates into an unreadable graph. Reflow the whole
  // scene; node identity, nesting and execution connections remain unchanged.
  if (
    Object.keys(positions).length &&
    nodes.some(
      (node) =>
        !positions[node.key] ||
        !Number.isFinite(positions[node.key].x) ||
        !Number.isFinite(positions[node.key].y),
    ) &&
    nodes.some((a, i) =>
      nodes
        .slice(i + 1)
        .some(
          (b) => Math.abs(a.x - b.x) < NODE_WIDTH + 16 && Math.abs(a.y - b.y) < NODE_HEIGHT + 16,
        ),
    )
  ) {
    return buildCanvas(steps, {}, handlers);
  }
  return { nodes, edges };
}
export function edgeGeometry(from: Point, to: Point): { path: string; middle: Point } {
  const x1 = from.x + NODE_WIDTH,
    y1 = from.y + NODE_HEIGHT / 2,
    x2 = to.x,
    y2 = to.y + NODE_HEIGHT / 2;
  const bend = Math.max(70, Math.abs(x2 - x1) / 2);
  return {
    path: `M ${x1} ${y1} C ${x1 + bend} ${y1}, ${x2 - bend} ${y2}, ${x2} ${y2}`,
    middle: { x: (x1 + x2) / 2, y: (y1 + y2) / 2 },
  };
}
export function zoomAt(pan: Point, current: number, next: number, anchor: Point): Point {
  return {
    x: anchor.x - ((anchor.x - pan.x) * next) / current,
    y: anchor.y - ((anchor.y - pan.y) * next) / current,
  };
}
