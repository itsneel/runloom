import type { WorkflowDefinition } from '../../../core/models/workflow.types';

export const parallelExampleInput = { customer: 'Avery', reviewer: 'Jordan' };

export function parallelExample(suffix: string): { child: WorkflowDefinition; parent: WorkflowDefinition } {
  const child: WorkflowDefinition = {
    schema_version: '1.0', kind: 'child', id: `parallel-greeting-${suffix}`, version: 0,
    name: 'Demo · Reusable greeting',
    description: 'Wait five seconds, then return a greeting. Uses no external services.',
    trigger: { type: 'manual' },
    input_schema: { type: 'object', required: ['name'], properties: { name: { type: 'string', minLength: 1 } } },
    steps: [
      { id: 'wait-five-seconds', name: 'Wait five seconds', type: 'wait', seconds: 5 },
      { id: 'greeting', name: 'Return greeting', type: 'activity', activity: 'runloom.echo', with: { message: 'Hello, ${input.name}!' } },
    ],
    output: '${steps.greeting.output}',
  };
  const output = { customer: '${steps.greet-customer.output.message}', reviewer: '${steps.greet-reviewer.output.message}' };
  const parent: WorkflowDefinition = {
    schema_version: '1.0', kind: 'main', id: `parallel-parent-${suffix}`, version: 0,
    name: 'Demo · Parallel child workflows',
    description: 'Start two child workflows together, join their results, then end. Try with Avery and Jordan.',
    trigger: { type: 'manual' },
    input_schema: { type: 'object', required: ['customer', 'reviewer'], properties: { customer: { type: 'string', minLength: 1 }, reviewer: { type: 'string', minLength: 1 } } },
    steps: [
      { id: 'parallel-greetings', name: 'Run both greetings together', type: 'parallel', branches: [
        { id: 'customer', steps: [{ id: 'greet-customer', name: 'Greet customer', type: 'subworkflow', definition_id: child.id, version: 1, with: { name: '${input.customer}' }, timeout_seconds: 60 }] },
        { id: 'reviewer', steps: [{ id: 'greet-reviewer', name: 'Greet reviewer', type: 'subworkflow', definition_id: child.id, version: 1, with: { name: '${input.reviewer}' }, timeout_seconds: 60 }] },
      ] },
      { id: 'done', name: 'Return both greetings and end', type: 'end', message: 'Both children finished. Workflow will end now.', with: output },
    ],
    output,
  };
  return { child, parent };
}
