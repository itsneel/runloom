import type {
  DefinitionSummary,
  StepType,
  WorkflowDefinition,
  WorkflowStep,
} from '../../../core/models/workflow.types';
import { allSteps, allDefinitionSteps, availableID } from './workflow-tree';

export function slug(value: string): string {
  const slug = value
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9-]+/g, '-')
    .replace(/^-+|-+$/g, '');
  return /^[a-z]/.test(slug) ? slug.slice(0, 64) : `step-${slug}`.slice(0, 64);
}

export function makeStep(
  type: StepType,
  definition: WorkflowDefinition,
  publishedChildren: DefinitionSummary[],
): WorkflowStep {
  const uniqueId = (candidate: string) =>
    availableID(
      candidate,
      new Set([
        ...allDefinitionSteps(definition).map((step) => step.id),
        ...(definition.signal_handlers ?? []).map((handler) => handler.id),
      ]),
    );
  const id = uniqueId(`${type}-${definition.steps.length + 1}`);
  switch (type) {
    case 'integration':
      return {
        id,
        name: 'Business integration',
        type,
        integration: { operation: '', secret: '', parameters: {} },
      };
    case 'transform':
      return {
        id,
        name: 'Map fields',
        type,
        data: {
          operation: 'map',
          source: '${input}',
          fields: [
            { name: 'name', value: '${item.name}', type: 'string', use_default: true, default: '' },
          ],
        },
      };
    case 'collection':
      return {
        id,
        name: 'Filter records',
        type,
        data: {
          operation: 'filter',
          source: '${input.items}',
          predicate: { left: '${item.active}', operator: 'equals', right: true },
        },
      };
    case 'merge':
      return {
        id,
        name: 'Combine records',
        type,
        data: { operation: 'append', source: '${input.items}', other: '${input.other}' },
      };
    case 'foreach':
      return {
        id,
        name: 'Process each item',
        type,
        foreach: { items: '${input.items}', concurrency: 1, output: '${loop.item}' },
        branches: [
          {
            id: 'body',
            steps: [
              {
                id: `${id}-map`,
                type: 'transform',
                data: { operation: 'map', fields: [{ name: 'item', value: '${loop.item}' }] },
              },
            ],
          },
        ],
      };
    case 'fail':
      return { id, name: 'Fail workflow', type, message: 'The request could not be processed' };
    case 'database':
      return {
        id,
        name: 'Query database',
        type,
        database: { connection_id: '', operation: 'select', table: '', limit: 100 },
      };
    case 'email':
      return {
        id,
        name: 'Send email',
        type,
        email: {
          to: ['${input.email}'],
          subject: 'A message from Runloom',
          text: 'Hello! Your workflow has reached this step.',
        },
      };
    case 'code':
      return {
        id,
        name: 'Transform with JavaScript',
        type,
        code: {
          runtime: 'quickjs-ng-0.11.0-v1',
          source: 'return { message: `Hello, ${input.name || "world"}!` };',
        },
        with: { name: '${input.name}' },
      };
    case 'http':
      return {
        id,
        name: 'Call external API',
        type,
        http: {
          url: 'https://api.example.com/resource',
          method: 'GET',
          headers: {},
          secret_headers: {},
        },
        timeout_seconds: 30,
        retry: { maximum_attempts: 1 },
      };
    case 'end':
      return { id, name: 'End workflow', type, message: 'Workflow will end now', with: {} };
    case 'activity':
      return {
        id,
        name: 'New action',
        type,
        activity: 'runloom.echo',
        with: {},
        timeout_seconds: 60,
        retry: { maximum_attempts: 3 },
      };
    case 'llm':
      return {
        id,
        name: 'Ask a language model',
        type,
        model: 'default',
        system_prompt: 'You are a helpful business assistant.',
        prompt: 'Summarize this input: ${input.text}',
        temperature: 0.2,
        max_tokens: 2000,
        timeout_seconds: 120,
        retry: { maximum_attempts: 3 },
      };
    case 'decision':
      return {
        id,
        name: 'Decide next action',
        type,
        model: 'default',
        system_prompt:
          'Choose only the available actions. Request human review when information is insufficient.',
        prompt: 'Choose the next action for: ${input.text}',
        temperature: 0.2,
        max_tokens: 1000,
        timeout_seconds: 120,
        retry: { maximum_attempts: 1 },
        decision: { mode: 'single', execution: 'sequential', max_rounds: 1, max_actions: 1 },
        branches: ['respond', 'escalate'].map((action) => ({
          id: action,
          description:
            action === 'respond'
              ? 'Handle a routine request.'
              : 'Prepare a complex request for human support.',
          steps: [
            {
              id: uniqueId(`${id}-${action}`),
              type: 'activity',
              activity: 'runloom.echo',
              with: { action },
              timeout_seconds: 60,
              retry: { maximum_attempts: 1 },
            },
          ],
        })),
      };
    case 'subworkflow': {
      const target = publishedChildren.find((d) => d.id !== definition.id);
      return {
        id,
        name: 'Call subworkflow',
        type,
        definition_id: target?.id || '',
        version: target?.latest_version || 1,
        with: {},
        timeout_seconds: 3600,
      };
    }
    case 'wait':
      return { id, name: 'Wait a little', type, seconds: 60 };
    case 'callback':
      return {
        id,
        name: 'Wait for callback',
        type,
        callback_key: 'external.ready',
        prompt: 'Waiting for the external system to finish',
        on_timeout: 'fail',
      };
    case 'signal':
      return {
        id,
        name: 'Wait for signal',
        type,
        signal_name: 'business.ready',
        prompt: 'Waiting for the named business event',
        on_timeout: 'fail',
      };
    case 'approval':
      return {
        id,
        name: 'Get approval',
        type,
        prompt: 'Please review and approve this request',
        on_timeout: 'reject',
      };
    case 'pause':
      return {
        id,
        name: 'Operator checkpoint',
        type,
        message: 'Review the run, then resume when ready',
        on_timeout: 'fail',
      };
    case 'condition':
      return {
        id,
        name: 'Choose a route',
        type,
        if: { left: '${input.approved}', operator: 'equals', right: true },
        then: [
          {
            id: `${id}-yes`,
            type: 'activity',
            activity: 'runloom.echo',
            with: { route: 'yes' },
            timeout_seconds: 60,
            retry: { maximum_attempts: 3 },
          },
        ],
        else: [],
      };
    case 'parallel':
      return {
        id,
        name: 'Run together',
        type,
        branches: [
          {
            id: `${id}-a`,
            steps: [
              {
                id: `${id}-action-a`,
                type: 'activity',
                activity: 'runloom.echo',
                with: { branch: 'a' },
                timeout_seconds: 60,
                retry: { maximum_attempts: 3 },
              },
            ],
          },
          {
            id: `${id}-b`,
            steps: [
              {
                id: `${id}-action-b`,
                type: 'activity',
                activity: 'runloom.echo',
                with: { branch: 'b' },
                timeout_seconds: 60,
                retry: { maximum_attempts: 3 },
              },
            ],
          },
        ],
      };
  }
}

export function emptyDefinition(): WorkflowDefinition {
  return {
    schema_version: '1.0',
    id: `workflow-${crypto.randomUUID()}`,
    name: 'Untitled workflow',
    description: 'Describe what this workflow accomplishes.',
    version: 0,
    trigger: { type: 'manual' },
    input_schema: { type: 'object' },
    steps: [],
    output: null,
  };
}

export function exampleDefinition(): WorkflowDefinition {
  return {
    schema_version: '1.0',
    id: 'callback-review-demo',
    name: 'Callback and review demo',
    description:
      'Prepare a request, wait for an external callback, pause for review, then ask for approval.',
    version: 0,
    trigger: { type: 'webhook', path: 'ai-review-request', method: 'POST' },
    input_schema: {
      type: 'object',
      properties: { employee_name: { type: 'string' }, department: { type: 'string' } },
    },
    steps: [
      {
        id: 'prepare',
        name: 'Prepare request',
        type: 'activity',
        activity: 'runloom.compose',
        with: { template: 'Prepare access for ${input.employee_name}' },
        timeout_seconds: 60,
        retry: { maximum_attempts: 3 },
      },
      {
        id: 'ai-summary',
        name: 'Create AI summary',
        type: 'llm',
        model: 'default',
        system_prompt: 'You summarize onboarding requests for managers.',
        prompt:
          'Summarize this onboarding request for ${input.employee_name} in ${input.department}.',
        temperature: 0.2,
        max_tokens: 500,
        timeout_seconds: 120,
        retry: { maximum_attempts: 3 },
      },
      { id: 'settle', name: 'Allow systems to settle', type: 'wait', seconds: 5 },
      {
        id: 'provisioning-callback',
        name: 'Provisioning callback',
        type: 'callback',
        callback_key: 'provisioning.ready',
        prompt: 'Waiting for the provisioning system to report readiness',
        timeout_seconds: 86400,
        on_timeout: 'fail',
      },
      {
        id: 'review-pause',
        name: 'Review checkpoint',
        type: 'pause',
        message: 'Review the callback data before continuing',
        on_timeout: 'fail',
      },
      {
        id: 'documents-signal',
        name: 'Documents ready',
        type: 'signal',
        signal_name: 'documents.ready',
        prompt: 'Waiting for the document service',
        timeout_seconds: 86400,
        on_timeout: 'fail',
      },
      {
        id: 'manager-approval',
        name: 'Manager approval',
        type: 'approval',
        prompt: 'Approve access for ${input.employee_name}?',
        timeout_seconds: 86400,
        on_timeout: 'reject',
      },
    ],
    output: {
      approved: '${steps.manager-approval.output.approved}',
      callback: '${steps.provisioning-callback.output.payload}',
      summary: '${steps.ai-summary.output.content}',
      documents: '${steps.documents-signal.output.payload}',
    },
  };
}
