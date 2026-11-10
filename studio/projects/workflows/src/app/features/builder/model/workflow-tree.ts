import type { WorkflowDefinition, WorkflowStep } from '../../../core/models/workflow.types';

export interface StepLocation {
  steps: WorkflowStep[];
  index: number;
}
export function childPaths(step: WorkflowStep): WorkflowStep[][] {
  return [
    ...(step.then ? [step.then] : []),
    ...(step.else ? [step.else] : []),
    ...(step.branches ?? []).map((branch) => branch.steps),
  ];
}
export function allSteps(steps: WorkflowStep[]): WorkflowStep[] {
  return steps.flatMap((step) => [step, ...childPaths(step).flatMap(allSteps)]);
}
export function containsPath(step: WorkflowStep, target: WorkflowStep[]): boolean {
  return childPaths(step).some(
    (path) => path === target || path.some((child) => containsPath(child, target)),
  );
}
export function parallelPath(
  root: WorkflowStep[],
  target: WorkflowStep[],
  inside = false,
): boolean {
  if (root === target) return inside;
  return root.some((step) =>
    childPaths(step).some((path) =>
      parallelPath(
        path,
        target,
        inside || step.type === 'parallel' || step.type === 'decision' || step.type === 'foreach',
      ),
    ),
  );
}
export function decisionPath(
  root: WorkflowStep[],
  target: WorkflowStep[],
  inside = false,
): boolean {
  if (root === target) return inside;
  return root.some((step) =>
    childPaths(step).some((path) => decisionPath(path, target, inside || step.type === 'decision')),
  );
}
export function availableID(candidate: string, ids: Set<string>): string {
  const base = candidate.slice(0, 58) || 'node';
  let id = base,
    suffix = 2;
  while (ids.has(id)) id = `${base}-${suffix++}`;
  ids.add(id);
  return id;
}
export function duplicateTree(
  step: WorkflowStep,
  root: WorkflowStep[],
  labelAsCopy = true,
): WorkflowStep {
  let copy = structuredClone(step);
  const ids = new Set(allSteps(root).map((node) => node.id));
  const mapping = new Map<string, string>();
  const signals = new Set(
    allSteps(root)
      .map((node) => node.signal_name)
      .filter((name): name is string => !!name),
  );
  for (const node of allSteps([copy])) {
    const id = availableID(node.id, ids);
    mapping.set(node.id, id);
    node.id = id;
    if (node.type === 'signal' && node.signal_name)
      node.signal_name = availableID(node.signal_name, signals);
  }
  // Keep references within the copied subtree pointing to its copied nodes.
  copy = JSON.parse(
    JSON.stringify(copy, (_key, value) =>
      typeof value === 'string'
        ? value.replace(/\$\{steps\.([a-z][a-z0-9-]*)([.}])/g, (match, id, suffix) =>
            mapping.has(id) ? `\${steps.${mapping.get(id)}${suffix}` : match,
          )
        : value,
    ),
  );
  if (labelAsCopy) copy.name = `${copy.name || step.id} copy`;
  return copy;
}

export function rootPaths(definition: WorkflowDefinition): WorkflowStep[][] {
  return [definition.steps, ...(definition.signal_handlers ?? []).map((handler) => handler.steps)];
}
export function allDefinitionSteps(definition: WorkflowDefinition): WorkflowStep[] {
  return rootPaths(definition).flatMap(allSteps);
}
