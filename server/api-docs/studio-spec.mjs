// Authoritative documentation metadata. Runtime route coverage is checked by Go
// tests; existing workflow/schedule JSON schemas are imported by build.mjs.
import { authoringPath, authoringCreatePath } from './authoring-spec.mjs';
import { deletionPaths, integrationCatalog } from './catalog-deletion-spec.mjs';

export function studioSpec(workflow, schedule) {
  const str = (description = '') => ({ type: 'string', ...(description ? { description } : {}) });
  const integer = (minimum = 0, maximum) => ({
    type: 'integer',
    minimum,
    ...(maximum === undefined ? {} : { maximum }),
  });
  const bool = { type: 'boolean' },
    any = {},
    object = { type: 'object', additionalProperties: true };
  const array = (items) => ({ type: 'array', items });
  const ref = (name) => ({ $ref: `#/components/schemas/${name}` });
  const obj = (properties, required = [], additionalProperties = false) => ({
    type: 'object',
    properties,
    ...(required.length ? { required } : {}),
    additionalProperties,
  });
  const enumeration = (values) => ({ type: 'string', enum: values });
  const date = { type: 'string', format: 'date-time' };
  const input = { type: ['object', 'null'], additionalProperties: true, default: {} };
  const status = enumeration([
    'running',
    'completed',
    'failed',
    'canceled',
    'terminated',
    'timedout',
    'continuedasnew',
  ]);
  const example = {
    schema_version: '1.0',
    id: 'example',
    name: 'Example workflow',
    kind: 'main',
    version: 0,
    trigger: { type: 'manual' },
    input_schema: { type: 'object' },
    steps: [{ id: 'echo', type: 'activity', activity: 'runloom.echo', with: { message: 'Hello' } }],
    output: '${steps.echo.output}',
  };
  const schemas = {
    ExecutionMetadata: {
      type: 'object',
      readOnly: true,
      description:
        'Immutable server-assigned execution identity. Indexed Temporal attributes are plaintext identifiers, never inputs, prompts or secrets. Clients cannot choose namespace or tags.',
      properties: {
        namespace: { type: 'string' },
        org_id: { type: 'string', format: 'uuid' },
        team_id: { type: 'string', format: 'uuid' },
        project_id: { type: 'string' },
        definition_id: { type: 'string' },
        definition_version: { type: 'integer', minimum: 0 },
        is_test: { type: 'boolean' },
        source: {
          type: 'string',
          enum: ['manual', 'consumer', 'webhook', 'schedule', 'child', 'draft_test', 'child_test'],
        },
        root_execution_id: {
          type: 'string',
          description: 'Root Temporal Workflow ID, not Temporal Run ID.',
        },
        snapshot_id: { type: 'string', pattern: '^[a-f0-9]{64}$' },
      },
    },
    DatabaseConnection: {
      type: 'object',
      additionalProperties: false,
      required: [
        'name',
        'provider',
        'host',
        'port',
        'database',
        'username',
        'tls',
        'access',
        'enabled',
      ],
      properties: {
        id: { type: 'string' },
        name: { type: 'string', maxLength: 120 },
        provider: { enum: ['mongodb', 'postgres', 'mysql', 'supabase'] },
        host: { type: 'string' },
        port: { type: 'integer', minimum: 1, maximum: 65535 },
        database: { type: 'string' },
        username: { type: 'string' },
        password: {
          type: 'string',
          writeOnly: true,
          description:
            'Required on create. Blank or omitted on update preserves the encrypted password; never returned.',
        },
        tls: { type: 'boolean' },
        srv: { type: 'boolean' },
        auth_source: { type: 'string' },
        access: { enum: ['read', 'write', 'schema'] },
        enabled: { type: 'boolean' },
        updated_at: { type: 'string', readOnly: true },
      },
    },
    DatabaseOperation: {
      type: 'object',
      additionalProperties: false,
      required: ['connection_id', 'operation'],
      properties: {
        connection_id: { type: 'string', pattern: '^[a-z][a-z0-9-]{0,63}$' },
        operation: { enum: ['select', 'insert', 'update', 'delete', 'sql', 'aggregate'] },
        schema: { type: 'string' },
        table: { type: 'string' },
        query: { type: 'string', maxLength: 32768 },
        parameters: { type: 'array' },
        values: { type: 'object' },
        filter: { type: 'object' },
        pipeline: { type: 'array', items: { type: 'object' } },
        limit: { type: 'integer', minimum: 1, maximum: 1000 },
      },
      allOf: [
        {
          if: { properties: { operation: { const: 'sql' } } },
          then: { required: ['query'] },
          else: { required: ['table'] },
        },
        {
          if: { properties: { operation: { enum: ['insert', 'update'] } } },
          then: { required: ['values'], properties: { values: { minProperties: 1 } } },
        },
        {
          if: { properties: { operation: { enum: ['update', 'delete'] } } },
          then: { required: ['filter'], properties: { filter: { minProperties: 1 } } },
        },
        {
          if: { properties: { operation: { const: 'aggregate' } } },
          then: { required: ['pipeline'], properties: { pipeline: { minItems: 1 } } },
        },
      ],
    },
    PromptTemplate: workflow.$defs.promptTemplate,
    PromptEntry: obj(
      {
        id: str(),
        name: str(),
        description: str(),
        tags: {
          type: 'array',
          maxItems: 20,
          uniqueItems: true,
          items: { type: 'string', minLength: 1, maxLength: 40 },
          description:
            'Optional library labels. Trimmed, lowercased and deduplicated on save. Captured in each revision and published version; excluded from rendered prompt content and its hash.',
        },
        revision: integer(),
        version: integer(),
        published: bool,
        template: ref('PromptTemplate'),
        hash: str(),
        actor: str(),
        updated_at: date,
      },
      ['id', 'name', 'revision', 'template'],
    ),
    PromptPreviewRequest: obj({ template: ref('PromptTemplate'), inputs: object }, [
      'template',
      'inputs',
    ]),
    VariableHint: obj(
      { path: str(), type: str(), optional: bool, description: str(), dynamic: bool },
      ['path', 'type', 'optional'],
    ),
    LLMOutput: {
      ...obj(
        {
          content: str('Unwrapped model answer; existing content references are preserved.'),
          hitl_required: bool,
          hitl_reason: str(
            'Required review explanation when true; empty when false. Maximum 2000 UTF-8 bytes.',
          ),
          model: str(),
          usage: object,
          usage_recorded: bool,
        },
        ['content', 'hitl_required', 'hitl_reason', 'model', 'usage'],
        true,
      ),
      description:
        'Output of new built-in LLM calls, including Action calls to runloom.llm. Route hitl_required through Condition/Approval when needed. A flag is not permission. Historical recorded results may predate these fields.',
    },
    AIDecisionChoice: {
      ...obj(
        {
          outcome: enumeration(['actions', 'finish', 'handoff']),
          actions: array(str()),
          reason: str(),
          hitl_required: bool,
          hitl_reason: str(),
        },
        ['outcome', 'actions', 'reason', 'hitl_required', 'hitl_reason'],
      ),
      description:
        'Every AI Decision round requires this structured choice. Handoff requires hitl_required true, nonempty hitl_reason and no actions. Actions/finish require false and an empty hitl_reason. Contradictions fail without dispatching actions. Handoff pauses for operator resume; resume continues after the decision node.',
    },
    WorkflowDefinition: { ...workflow, example },
    DraftWorkflowDefinition: obj(
      {
        ...workflow.properties,
        name: str(),
        description: str(),
        version: integer(),
        kind: str(),
        trigger: object,
        input_schema: object,
        steps: array(object),
      },
      ['schema_version', 'id'],
      false,
    ),
    ScheduleRequest: {
      ...schedule,
      example: {
        name: 'Hourly example',
        definition_id: 'example',
        version: 1,
        input: {},
        interval_seconds: 3600,
      },
    },
    Error: obj({ detail: str(), error: str(), lock: ref('EditLock') }, ['detail'], true),
    NameRequest: obj({ name: { type: 'string', minLength: 1, maxLength: 120 } }, ['name']),
    ProjectNameRequest: obj({ name: { type: 'string', minLength: 1, maxLength: 80 } }, ['name']),
    DefinitionSummary: obj(
      {
        id: str(),
        name: str(),
        description: str(),
        published_kind: str(),
        latest_version: { type: ['integer', 'null'], minimum: 1 },
        published_versions: array(integer(1)),
        has_draft: bool,
        project_id: str(),
      },
      [],
      true,
    ),
    Commit: obj(
      {
        commit_id: str(),
        definition_id: str(),
        sequence: integer(1),
        kind: str(),
        message: str(),
        actor: str(),
        created_at: date,
        published_version: { type: ['integer', 'null'] },
        body: ref('DraftWorkflowDefinition'),
      },
      [],
      true,
    ),
    EditLock: obj(
      {
        locked: bool,
        owner: str(),
        owner_name: str(),
        expires_at: date,
        writing: bool,
        ttl_seconds: integer(),
      },
      ['locked', 'writing', 'ttl_seconds'],
    ),
    Project: obj(
      { id: str(), name: str(), created_at: date, updated_at: date },
      ['id', 'name'],
      true,
    ),
    StartRequest: obj(
      {
        definition_id: str(),
        version: {
          type: ['integer', 'null'],
          minimum: 1,
          description: 'Omit/null selects latest published version.',
        },
        input,
        workflow_id: str(
          'Optional custom suffix. NOT a durable idempotency key; prefer generated IDs.',
        ),
      },
      ['definition_id'],
    ),
    StartResponse: obj(
      {
        workflow_id: str('Use this complete team-prefixed ID in Studio run URLs.'),
        run_id: str('Temporal execution ID; not the Studio URL identifier.'),
        definition_id: str(),
        definition_version: integer(),
        is_test: bool,
        accepted: bool,
      },
      ['workflow_id', 'run_id', 'definition_id', 'definition_version'],
    ),
    TestRequest: obj(
      { definition: ref('DraftWorkflowDefinition'), version: integer(), input },
      [],
      false,
    ),
    Event: obj(
      {
        step_id: { type: 'string', minLength: 1, maxLength: 64 },
        payload: any,
        actor: {
          ...str('Ignored for attribution; authenticated human identity is used.'),
          maxLength: 200,
        },
        signal_name: str(),
      },
      ['step_id'],
    ),
    Approval: obj(
      {
        step_id: { type: 'string', minLength: 1 },
        approved: { type: 'boolean', default: false },
        comment: { type: 'string', maxLength: 2000 },
        actor: { type: 'string', maxLength: 200 },
      },
      ['step_id'],
    ),
    Payload: obj({
      event_id: {
        type: 'string',
        format: 'uuid',
        description:
          'Optional idempotency key for independent signal handlers. Reuse only with the same sender and payload.',
      },
      payload: any,
      actor: { ...str('Authenticated identity overrides this field.'), maxLength: 200 },
    }),
    RunState: {
      type: 'object',
      properties: {
        signal_handlers: {
          type: 'object',
          additionalProperties: obj({
            id: str(),
            signal_name: str(),
            status: str(),
            accepting: bool,
            received: integer(),
            completed: integer(),
            pending: integer(),
            invocation_id: str(),
            error: str(),
            output: any,
          }),
        },
        metadata: { $ref: '#/components/schemas/ExecutionMetadata' },
        status: {
          type: 'string',
          description: 'Builder state, distinct from Temporal lifecycle. Pauses/waits remain open.',
        },
        definition_id: { type: 'string' },
        definition_version: { type: 'integer', minimum: 0 },
        current_step: {},
        steps: { type: 'object', additionalProperties: true },
        completed_steps: { type: 'array', items: { type: 'string' } },
        waiting_approval: {},
        waiting_callback: {},
        waiting_signal: {},
        paused_step: {},
        waiting_approvals: { type: 'object', additionalProperties: true },
        waiting_callbacks: { type: 'object', additionalProperties: true },
        waiting_signals: { type: 'object', additionalProperties: true },
        paused_steps: { type: 'object', additionalProperties: true },
        output: {},
        error: {},
        child_workflows: { type: 'object', additionalProperties: true },
        parent_workflow_id: { type: 'string' },
        is_test: { type: 'boolean' },
      },
      additionalProperties: true,
    },
    RunStatus: obj(
      {
        workflow_id: str(),
        temporal_status: status,
        state: { anyOf: [ref('RunState'), { type: 'null' }] },
      },
      ['workflow_id', 'temporal_status', 'state'],
    ),
    RunRecord: {
      type: 'object',
      properties: {
        metadata: { $ref: '#/components/schemas/ExecutionMetadata' },
        workflow_id: { type: 'string' },
        temporal_run_id: { type: 'string' },
        definition_id: { type: 'string' },
        definition_version: { type: 'integer', minimum: 0 },
        trigger_type: { type: 'string' },
        input: {},
        temporal_status: { type: 'string' },
        state: {},
        output: {},
        created_at: { type: 'string', format: 'date-time' },
        updated_at: { type: 'string', format: 'date-time' },
        completed_at: {},
        llm_cost: { type: 'object', additionalProperties: true },
      },
      additionalProperties: true,
    },
    Audit: obj(
      {
        event_id: str(),
        workflow_id: str(),
        definition_id: str(),
        event_type: str(),
        step_id: str(),
        actor: str(),
        payload: any,
        created_at: date,
      },
      [],
      true,
    ),
    Schedule: obj(
      { id: str(), ...schedule.properties, next_runs: array(date), recent_runs: array(str()) },
      ['id'],
      true,
    ),
    Member: obj(
      {
        email: str(),
        role: str(),
        user_id: str(),
        name: str(),
        inherited: {
          type: 'boolean',
          description:
            'Team roster only: true for effective admin access inherited from an organization owner/admin. These rows are read-only in the team UI; change organization membership instead.',
        },
      },
      [],
      true,
    ),
    ServiceAccountPolicy: obj(
      {
        name: { type: 'string', minLength: 1, maxLength: 120 },
        scopes: {
          ...array(enumeration(['workflows:read', 'runs:start', 'runs:read', 'runs:cancel'])),
          minItems: 1,
          maxItems: 4,
        },
        requests_per_minute: integer(1, 10000),
        max_concurrent_runs: integer(1, 100),
        grants: {
          ...array(
            obj(
              {
                definition_id: str(),
                versions: { ...array(integer(1)), minItems: 1, maxItems: 100 },
                input_fields: array(str()),
                output_fields: array(str()),
              },
              ['definition_id', 'versions'],
            ),
          ),
          minItems: 1,
          maxItems: 100,
        },
      },
      ['name', 'scopes', 'requests_per_minute', 'max_concurrent_runs', 'grants'],
    ),
    ServiceAccount: obj(
      {
        client_id: str(),
        team_id: str(),
        org_id: str(),
        status: str(),
        name: str(),
        scopes: array(str()),
        grants: array(object),
        requests_per_minute: integer(),
        max_concurrent_runs: integer(),
        created_at: date,
      },
      [],
      true,
    ),
    Credentials: obj(
      {
        account: ref('ServiceAccount'),
        client_id: str(),
        client_secret: str('One-time visible; never persist in browser or logs.'),
      },
      ['account', 'client_id', 'client_secret'],
    ),
    BudgetPolicy: obj(
      {
        id: str(),
        revision: integer(),
        name: { type: 'string', minLength: 1, maxLength: 100 },
        enabled: bool,
        soft_usd: str('Decimal USD string, or empty for no soft limit.'),
        hard_usd: str('Decimal USD string, or empty for no hard limit.'),
        projects: {
          ...array(str()),
          maxItems: 100,
          description: 'Empty list applies to all current/future projects.',
        },
        excluded_workflows: { ...array(str()), maxItems: 500 },
        reason: { type: 'string', minLength: 1, maxLength: 500 },
        updated_at: str(),
      },
      ['name', 'reason'],
    ),
  };
  schemas.BudgetPolicy.description =
    'At least one nonempty soft_usd or hard_usd is required. Nonnegative decimal strings with at most 12 decimals; soft must not exceed hard. Revision is 0 for creation or the last read revision for updates.';
  schemas.BudgetPolicy.example = {
    name: 'Monthly project budget',
    revision: 0,
    enabled: true,
    soft_usd: '80',
    hard_usd: '100',
    projects: [],
    excluded_workflows: [],
    reason: 'Set local test budget',
  };
  schemas.TestRequest.oneOf = [
    { required: ['definition'], properties: { version: { const: 0 } } },
    {
      required: ['version'],
      properties: { version: integer(1) },
      not: { required: ['definition'] },
    },
  ];
  const json = (schema) => ({ 'application/json': { schema } });
  const query = (name, schema, description = '') => ({
    name,
    in: 'query',
    required: false,
    schema,
    description,
  });
  const header = (name, required = false, description = '') => ({
    name,
    in: 'header',
    required,
    schema: str(),
    description,
  });
  const limit = (value, max) => query('limit', { ...integer(1, max), default: value });
  const edit = header(
    'X-Runloom-Edit-Token',
    false,
    'Editor session UUID. Required to write while its lease is held; never bypasses role checks.',
  );
  const commit = header('X-Runloom-Commit-Message', false, 'Optional commit description.');
  const paths = {
    '/definitions/{id}/authoring': authoringPath,
    '/authoring/workflows': authoringCreatePath,
  };
  const spec = {
    openapi: '3.1.0',
    info: {
      title: 'Runloom Studio API',
      version: '1.0.0',
      description:
        'APIs for the Angular host, workflow builder and management application. Verified human Studio access tokens only; machine tokens are rejected. Team viewers read; editors/admins author and operate team runs. Organization owners/admins have effective team-admin access. Project folders do not grant execution permissions. Try it out sends REAL requests. There is no general run pause/stop/terminate endpoint. Resume targets a designed or budget pause. Studio starts/tests/webhooks have NO durable Idempotency-Key guarantee.',
    },
    servers: [{ url: '/api/v1/studio' }],
    security: [{ studioBearer: [] }],
    paths,
    tags: [
      'Definitions',
      'Editing',
      'Projects',
      'Schedules',
      'Runs',
      'Events',
      'Identity',
      'Teams',
      'Service accounts',
      'Secrets',
      'Analytics',
      'Budgets',
    ].map((name) => ({ name })),
    components: {
      securitySchemes: {
        studioBearer: {
          type: 'http',
          scheme: 'bearer',
          bearerFormat: 'JWT',
          description:
            'Paste a verified human Studio access token, without the Bearer prefix. Never paste a password, client secret or consumer token.',
        },
      },
      schemas,
    },
  };
  function add(
    method,
    path,
    id,
    tag,
    summary,
    {
      body,
      result = object,
      code = 200,
      description = '',
      parameters = [],
      role,
      example: requestExample,
    } = {},
  ) {
    const management = path === '/me' || path.startsWith('/orgs');
    const permission =
      role || (method === 'get' ? 'team viewer/editor/admin' : 'team editor/admin');
    const params = [...path.matchAll(/\{([^}]+)\}/g)].map(([, name]) => ({
      name,
      in: 'path',
      required: true,
      schema: str(),
      description:
        name === 'id' && path.startsWith('/runs')
          ? 'Full workflow_id from the Studio start response, not the Temporal run_id.'
          : '',
    }));
    if (!management)
      params.push(
        header(
          'X-Runloom-Team',
          true,
          'Select an authorized team; cross-team run IDs are rejected before engine access.',
        ),
      );
    params.push(...parameters);
    const responses = {};
    responses[code] = {
      description: code === 201 ? 'Created' : code === 202 ? 'Accepted' : 'Success',
      content: json(result),
    };
    // Shared failures document gateway/validation/storage behavior; operation
    // descriptions call out special semantics rather than promising all codes.
    for (const [status, message] of Object.entries({
      400: 'Invalid request or filter',
      401: 'Human Studio access token required',
      403: 'Team/role denied',
      404: 'Resource missing or not in authorized team',
      409: 'Conflict; refresh state before retrying',
      422: 'Invalid workflow, input or event',
      423: 'Competing or expired edit lease',
      429: 'Code preview capacity exceeded (code/test only)',
      500: 'Internal storage error',
      502: 'Execution dependency failed; mutation may be ambiguous',
      503: 'Directory/storage/dependency unavailable; reconcile before retrying',
    }))
      responses[status] = { description: message, content: json(ref('Error')) };
    const operation = {
      operationId: id,
      tags: [tag],
      summary,
      description: `Required access: ${permission}. ${description}`,
      'x-required-role': permission,
      parameters: params,
      responses,
    };
    if (body)
      operation.requestBody = {
        required: true,
        content: {
          'application/json': {
            schema: body,
            ...(requestExample === undefined ? {} : { example: requestExample }),
          },
        },
      };
    paths[path] ??= {};
    paths[path][method] = operation;
    return operation;
  }
  add('get', '/prompts', 'listPrompts', 'Prompts', 'List team prompt library', {
    result: array(ref('PromptEntry')),
    description:
      'Bounded to 200 prompts per team. Human team access only. Entries represent editable drafts; version is the latest published version, not proof that the draft content is published.',
  });
  add('get', '/prompts/{id}', 'getPrompt', 'Prompts', 'Read draft or immutable published prompt', {
    result: ref('PromptEntry'),
    parameters: [
      query(
        'version',
        integer(),
        'Omit or use 0 for draft; positive values pin a published version.',
      ),
    ],
  });
  add('get', '/prompts/{id}/history', 'promptHistory', 'Prompts', 'Read prompt revision history', {
    result: array(ref('PromptEntry')),
    description:
      'Newest first; actor and time are server assigned. Library retention cap is 10000 revisions per team.',
  });
  const promptAction = add(
    'post',
    '/prompts/{id}/{action}',
    'savePrompt',
    'Prompts',
    'Save or publish a prompt',
    {
      body: ref('PromptEntry'),
      result: ref('PromptEntry'),
      description:
        'Send revision 0 to create; otherwise the last read revision. Stale writes return 409. Publish creates an immutable version. History is stored atomically with the write. Actor/hash/version are server assigned. Does not update workflows.',
    },
  );
  promptAction.parameters.find((p) => p.name === 'action').schema = enumeration([
    'save',
    'publish',
  ]);
  add(
    'post',
    '/prompts/preview',
    'previewPrompt',
    'Prompts',
    'Render typed template without a model call',
    {
      body: ref('PromptPreviewRequest'),
      result: obj({ system_prompt: str(), prompt: str(), prompt_hash: str() }),
      description:
        'Checks required variables, types and template references. Values are inserted once; embedded expressions are not evaluated. Never use secrets as sample data.',
    },
  );
  add('post', '/prompts/test', 'testPrompt', 'Prompts', 'Run a paid prompt test', {
    body: obj(
      { definition_id: str(), template: ref('PromptTemplate'), inputs: object, model: str() },
      ['definition_id', 'template', 'inputs'],
    ),
    result: ref('StartResponse'),
    code: 201,
    description:
      'Requires an existing team workflow draft. Runs one LLM node under that workflow project budget, run history and usage accounting. Real model cost; not retry-safe. Does not execute decision actions.',
  });
  add(
    'post',
    '/variables',
    'variableHints',
    'Definitions',
    'List execution-aware typed variables',
    {
      body: obj({ definition: ref('WorkflowDefinition'), step_id: str() }, [
        'definition',
        'step_id',
      ]),
      result: array(ref('VariableHint')),
      description:
        'Only input and earlier reachable node outputs. Conditional outputs are optional. Parallel siblings are unavailable before join; decision action outputs remain local. Unknown schemas are not type guarantees. Maximum 500 nodes, 20 nesting levels.',
    },
  );
  add('get', '/catalog', 'getCatalog', 'Definitions', 'List node and activity types', {
    result: obj({
      activities: array(object),
      step_types: array(str()),
      trigger_types: array(str()),
      workflow_kinds: array(str()),
    }),
  });
  const validation = add(
    'post',
    '/definitions/validate',
    'validateDefinition',
    'Definitions',
    'Validate workflow without execution',
    {
      body: ref('DraftWorkflowDefinition'),
      result: obj({ valid: bool, definition: ref('WorkflowDefinition') }),
      description:
        'Invalid definitions return 422 with the first workflow error and node diagnostics for all checked nodes, including nested paths. Each node reports its first configuration error and any reference-resolution error. No execution or model calls. Global/decode errors may have no node issues.',
    },
  );
  validation.responses[422].content = json(
    obj(
      {
        detail: str(),
        issues: array(
          obj({ node_id: str(), path: str(), message: str() }, ['node_id', 'path', 'message']),
        ),
      },
      ['detail'],
    ),
  );
  add(
    'post',
    '/definitions',
    'saveDraft',
    'Definitions',
    'Save an unfinished draft and create commit',
    {
      body: ref('DraftWorkflowDefinition'),
      result: ref('DraftWorkflowDefinition'),
      parameters: [
        edit,
        commit,
        header(
          'X-Runloom-Project',
          false,
          'Authoring project; omit to retain assignment or use General for a new workflow.',
        ),
      ],
      description:
        'Drafts may be empty or semantically invalid, including missing node configuration, child definitions and prompt references. Valid JSON with recognized field types, schema_version 1.0, a safe workflow ID, 1 MiB size limit, 500 nodes and 10 nesting levels are still required. Layout positions are metadata only. Saving neither publishes nor starts a run. Full validation is mandatory on publication and tests.',
    },
  );
  add('get', '/definitions', 'listDefinitions', 'Definitions', 'List team definitions', {
    result: array(ref('DefinitionSummary')),
    parameters: [query('project_id', str())],
  });
  add(
    'get',
    '/definitions/{id}',
    'getDefinition',
    'Definitions',
    'Get draft or published definition',
    {
      result: ref('DraftWorkflowDefinition'),
      parameters: [
        query(
          'version',
          integer(),
          '0 selects draft; positive values select a published version. Omit for latest published; 404 if none exists.',
        ),
      ],
    },
  );
  add(
    'get',
    '/definitions/{id}/history',
    'definitionHistory',
    'Definitions',
    'Read published versions and commits',
    {
      result: obj({
        definition_id: str(),
        latest_version: integer(),
        published_versions: array(integer(1)),
        commits: array(ref('Commit')),
      }),
      parameters: [limit(100, 500)],
    },
  );
  add(
    'get',
    '/definitions/{id}/commits/{commit}',
    'getCommit',
    'Definitions',
    'Read commit snapshot',
    { result: ref('Commit') },
  );
  add(
    'post',
    '/definitions/{id}/commits/{commit}/restore',
    'restoreCommit',
    'Definitions',
    'Restore commit into a new draft',
    {
      body: obj({ message: { type: 'string', maxLength: 500 } }),
      result: ref('DraftWorkflowDefinition'),
      parameters: [edit],
      description: 'Does not alter published versions.',
    },
  );
  add(
    'post',
    '/definitions/{id}/publish',
    'publishDefinition',
    'Definitions',
    'Validate and publish immutable version',
    {
      result: ref('WorkflowDefinition'),
      parameters: [edit, commit],
      description:
        'Invalid drafts return 422. Publication validates nodes, schemas, mappings, child dependencies and team-local prompt references. Failure leaves previous published versions intact and creates no new published version.',
    },
  );
  add(
    'post',
    '/definitions/{id}/test',
    'testDraft',
    'Definitions',
    'Test an unpublished main or child snapshot',
    {
      body: ref('TestRequest'),
      result: ref('StartResponse'),
      code: 201,
      example: { definition: example, input: {} },
      description:
        'Existing team-local draft required. Current snapshot runs as version 0 without saving/publishing. Pinned children remain published versions. Legacy version/input body tests published children only. Real side effects and costs apply; not idempotent.',
    },
  );
  for (const [method, action] of [
    ['get', 'Read'],
    ['post', 'Acquire'],
    ['put', 'Renew'],
    ['delete', 'Release'],
  ])
    add(
      method,
      '/definitions/{id}/edit-lock',
      `${action.toLowerCase()}EditLock`,
      'Editing',
      `${action} editing lease`,
      {
        result: ref('EditLock'),
        parameters:
          method === 'get'
            ? []
            : [
                header(
                  'X-Runloom-Edit-Token',
                  true,
                  'Session UUID used for this lease and subsequent writes.',
                ),
              ],
        description:
          'Only one editing session. Acquire then reload the latest draft. 90-second lease; renew before expiry.',
      },
    );
  add('post', '/code/test', 'testJavaScript', 'Definitions', 'Preview JavaScript in isolation', {
    body: obj({ code: workflow.$defs.code.properties.code, input }, ['code']),
    result: obj({ output: any }),
    example: {
      code: { runtime: 'quickjs-ng-0.11.0-v1', source: 'return {message: input.message};' },
      input: { message: 'Hello' },
    },
    description:
      'JavaScript only. Maximum source 32 KiB, input JSON 256 KiB, total request 512 KiB. Real code execution; no workflow start. Capacity failures return 429 with Retry-After: 1.',
  });
  add('get', '/projects', 'listProjects', 'Projects', 'List authoring folders', {
    result: array(ref('Project')),
  });
  add('post', '/projects', 'createProject', 'Projects', 'Create authoring folder', {
    body: ref('ProjectNameRequest'),
    result: ref('Project'),
    code: 201,
  });
  add(
    'post',
    '/projects/{project}/rename',
    'renameProject',
    'Projects',
    'Rename authoring folder',
    { body: ref('ProjectNameRequest'), result: ref('Project') },
  );
  add(
    'post',
    '/definitions/{id}/project',
    'moveDefinition',
    'Projects',
    'Move workflow between project folders',
    {
      body: obj({ project_id: str() }, ['project_id']),
      result: obj({ definition_id: str(), project_id: str() }),
      parameters: [edit],
      description: 'Move stays within the team and does not stop executions.',
    },
  );
  add('get', '/schedules', 'listSchedules', 'Schedules', 'List team schedules', {
    result: array(ref('Schedule')),
  });
  add('post', '/schedules', 'createSchedule', 'Schedules', 'Schedule a published main workflow', {
    body: ref('ScheduleRequest'),
    result: ref('Schedule'),
    code: 201,
    description:
      'Exactly one of interval_seconds, five-field cron, or future RFC3339 at. Pins published dependencies. Overlap skips; triggers cause real execution.',
  });
  add('get', '/schedules/{schedule}', 'getSchedule', 'Schedules', 'Get schedule and recent runs', {
    result: ref('Schedule'),
  });
  const scheduleAction = add(
    'post',
    '/schedules/{schedule}/{action}',
    'changeSchedule',
    'Schedules',
    'Pause, resume, trigger or delete schedule',
    {
      result: obj({ schedule_id: str(), action: str() }),
      description:
        'Pause/resume affects future triggers, not an existing workflow run. Trigger causes real execution.',
    },
  );
  scheduleAction.parameters.find((p) => p.name === 'action').schema = enumeration([
    'pause',
    'resume',
    'trigger',
    'delete',
  ]);
  add('delete', '/schedules/{schedule}', 'deleteSchedule', 'Schedules', 'Delete schedule', {
    result: obj({ schedule_id: str(), action: str() }),
  });
  add('post', '/runs', 'startRun', 'Runs', 'Start a published main workflow', {
    body: ref('StartRequest'),
    result: ref('StartResponse'),
    code: 201,
    example: { definition_id: 'example', version: 1, input: {} },
    description:
      'Child starts are forbidden. Omitted version selects latest published. NOT retry-safe: inspect team runs after a lost response before starting again.',
  });
  add('get', '/runs', 'listRuns', 'Runs', 'List recent team runs', {
    result: array(ref('RunRecord')),
    parameters: [
      limit(50, 200),
      query('include_usage', bool, 'Adds bounded usage summaries for up to 20 runs.'),
    ],
    description:
      'Persisted summaries, not a cursor-paginated list. Includes runs started by other authorized team callers.',
  });
  add('get', '/runs/{id}', 'getRun', 'Runs', 'Read live execution and builder state', {
    result: ref('RunStatus'),
    description:
      'Temporal status governs completion. state may be null if the worker cannot answer. Pauses remain temporal_status running; inspect state.paused_step/paused_steps.',
  });
  add(
    'get',
    '/runs/{id}/history',
    'getRunHistory',
    'Runs',
    'Read persisted run and audit history',
    {
      result: obj({ run: ref('RunRecord'), events: array(ref('Audit')) }),
      description: 'Application history, not raw Temporal replay events.',
    },
  );
  add(
    'get',
    '/runs/{id}/llm-usage',
    'getRunUsage',
    'Analytics',
    'Read attempt costs for run and descendants',
    {
      result: obj({
        summary: object,
        entries: array(object),
        next_cursor: str(),
        currency: { const: 'USD' },
        scope: { const: 'run_and_descendants' },
      }),
      parameters: [
        limit(50, 100),
        query('after', { type: 'string', format: 'uuid' }),
        query('model', { type: 'string', maxLength: 200 }),
        query('step_id', { type: 'string', maxLength: 100 }),
      ],
    },
  );
  add('get', '/audit', 'listAudit', 'Runs', 'List team workflow audit events', {
    result: array(ref('Audit')),
    parameters: [limit(200, 1000), query('workflow_id', str()), query('definition_id', str())],
  });
  for (const [action, title] of [
    ['approvals', 'Submit approval decision'],
    ['callbacks', 'Deliver callback payload'],
    ['resume', 'Resume a paused step'],
  ])
    add('post', `/runs/{id}/${action}`, `${action}Run`, 'Events', title, {
      body: ref(action === 'approvals' ? 'Approval' : 'Event'),
      result: obj({ accepted: bool }, ['accepted']),
      example:
        action === 'approvals'
          ? { step_id: 'review', approved: true, comment: 'Approved' }
          : { step_id: action === 'resume' ? 'review-pause' : 'callback', payload: {} },
      description:
        action === 'resume'
          ? 'Acceptance is signal delivery, not continuation. Read current pause first; early resumes may buffer by step ID. Budget enforcement is rechecked. A canceled/completed run cannot resume. Child pauses require the child run ID.'
          : 'Real event delivery; acceptance does not guarantee consumption or completion.',
    });
  add(
    'post',
    '/callbacks/{id}/{step}',
    'deliverCallback',
    'Events',
    'Deliver callback using step in URL',
    {
      body: ref('Payload'),
      result: obj({ accepted: bool }),
      description: 'Not anonymous. Human token and authorized team editor required.',
    },
  );
  add('post', '/runs/{id}/signals/{signal}', 'sendSignal', 'Events', 'Deliver named signal', {
    body: ref('Payload'),
    result: obj({ accepted: bool }),
    description:
      'Deliver to a declared independent signal handler or an inline named-signal wait. Human editor/admin authentication and team scope required. With event_id, known handler payloads are prevalidated; 409 means closed and 429 means a queue/event limit. Accepted does not mean processed. Consumer service-account delivery is not provided.',
  });
  add('post', '/runs/{id}/cancel', 'cancelRun', 'Runs', 'Request cooperative cancellation', {
    result: obj({ cancelled: bool }),
    description:
      '200 acknowledges the request, not completed cancellation. Poll temporal_status. Cannot undo effects or resume a canceled run. No force termination or general pause endpoint.',
  });
  add(
    'post',
    '/webhooks/{definition}/{path}',
    'startWebhook',
    'Runs',
    'Start published workflow through configured webhook',
    {
      body: object,
      result: ref('StartResponse'),
      code: 201,
      description:
        'Despite its name this endpoint is human-authenticated, team-scoped and write-authorized. Uses latest published main workflow; not idempotent.',
    },
  );
  add('get', '/me', 'getProfile', 'Identity', 'Read profile and accessible organizations/teams', {
    role: 'verified human',
    result: obj({ user: object, organizations: array(object) }, [], true),
  });
  add('post', '/orgs', 'createOrganization', 'Teams', 'Create organization', {
    role: 'organization owner/admin, or verified human with no organization memberships',
    body: obj({ name: str(), parent_id: str() }, ['name']),
    result: obj({ id: str() }),
    code: 201,
    description:
      'Creates a Keycloak-backed organization with a General team. Omit parent_id for an independent root, or specify a root organization the caller owns/administers. Only one child level is allowed. Child limits default to two per parent and can be configured by platform admins up to ten. Keycloak administrators can override Runloom constraints. Membership and org roles do not inherit. Users with only member roles receive 403.',
  });
  add(
    'get',
    '/orgs/creation',
    'getOrganizationCreationOptions',
    'Teams',
    'Read creation permissions, permitted parents and current child limit',
    {
      role: 'verified human',
      result: object,
      parameters: [
        query(
          'organization_id',
          str(),
          'Selected organization; caller must be a member. Only its family root can be returned as a parent. Without this parameter, the parent list is empty.',
        ),
      ],
      description:
        'Never lists parents from unrelated organization families, even when the caller administers them.',
    },
  );
  add(
    'get',
    '/orgs/{org}/hierarchy',
    'getOrganizationHierarchy',
    'Teams',
    'Read parent and child organization metadata',
    {
      role: 'member of the requested organization',
      result: object,
      description:
        'Members of a child can see parent and sibling names and IDs. Only explicit memberships permit switching or access to organization data. No member rosters, workflows or secrets are returned.',
    },
  );
  add(
    'patch',
    '/orgs/{org}',
    'renameOrganization',
    'Teams',
    'Rename an organization without changing its ID',
    {
      role: 'organization owner/admin',
      body: ref('NameRequest'),
      result: obj({ saved: bool }),
      description:
        'Only name is accepted. IDs, parent links, team access and Temporal namespaces remain unchanged. Renames are audited.',
    },
  );
  add('post', '/orgs/{org}/teams', 'createTeam', 'Teams', 'Create team', {
    role: 'organization owner/admin',
    body: ref('NameRequest'),
    result: obj({ id: str() }),
    code: 201,
  });
  for (const team of [false, true]) {
    const base = team ? '/orgs/{org}/teams/{team}/members' : '/orgs/{org}/members';
    const kind = team ? 'Team' : 'Organization',
      role = team ? 'effective team admin' : 'organization owner/admin';
    add('get', base, `list${kind}Members`, 'Teams', `List ${kind.toLowerCase()} members`, {
      role: team ? 'effective team viewer/editor/admin' : role,
      result: array(ref('Member')),
      description: team
        ? 'Read-only roster for the selected team. Includes explicit memberships plus joined organization owners/admins with inherited=true and effective admin role, deduplicated by email. Membership in another team or organization does not grant access.'
        : 'Organization directory remains owner/admin-only.',
    });
    add(
      'post',
      base,
      `set${kind}Member`,
      'Teams',
      `Add or update ${kind.toLowerCase()} membership`,
      {
        role,
        body: obj(
          {
            email: { type: 'string', format: 'email' },
            role: enumeration(team ? ['admin', 'editor', 'viewer'] : ['owner', 'admin', 'member']),
          },
          ['email', 'role'],
        ),
        result: obj({ saved: bool }),
        description: 'Last-owner protections apply.',
      },
    );
    add(
      'delete',
      base + '/{email}',
      `remove${kind}Member`,
      'Teams',
      `Remove ${kind.toLowerCase()} membership`,
      { role, result: obj({ removed: bool }) },
    );
  }
  add('get', '/orgs/{org}/audit', 'organizationAudit', 'Teams', 'Read organization audit', {
    role: 'organization owner/admin',
    result: array(object),
  });
  const accounts = '/orgs/{org}/teams/{team}/service-accounts';
  add(
    'get',
    accounts,
    'listServiceAccounts',
    'Service accounts',
    'List account metadata without secrets',
    { role: 'effective team admin', result: array(ref('ServiceAccount')) },
  );
  add(
    'post',
    accounts,
    'createServiceAccount',
    'Service accounts',
    'Create account and reveal client secret once',
    {
      role: 'effective team admin',
      body: ref('ServiceAccountPolicy'),
      result: ref('Credentials'),
      code: 201,
      description:
        'Requires provisioning setup. Grant exact published main versions only. Never store the response secret in browser persistence.',
    },
  );
  add(
    'put',
    accounts + '/{account}',
    'updateServiceAccount',
    'Service accounts',
    'Replace grants, exposure and quotas',
    {
      role: 'effective team admin',
      body: ref('ServiceAccountPolicy'),
      result: obj({ updated: bool }),
      description:
        'Path account is client_id. Scope reductions allowed; expansions require a new account. No secret returned.',
    },
  );
  const accountAction = add(
    'post',
    accounts + '/{account}/{action}',
    'changeServiceAccount',
    'Service accounts',
    'Rotate credentials or revoke account',
    {
      role: 'effective team admin',
      result: { oneOf: [ref('Credentials'), obj({ revoked: bool, provider_disabled: bool })] },
      description:
        'Rotate reveals secret once and invalidates prior generation. Revoke blocks subsequent requests but does not cancel runs; provider_disabled=false requires retrying provider cleanup.',
    },
  );
  accountAction.parameters.find((p) => p.name === 'action').schema = enumeration([
    'rotate',
    'revoke',
  ]);
  const secrets = '/orgs/{org}/teams/{team}/secrets';
  add('get', secrets, 'listSecrets', 'Secrets', 'List metadata, never values', {
    role: 'team viewer/editor/admin',
    result: obj({ configured: bool, items: array(obj({ name: str(), updated_at: date })) }),
  });
  add(
    'put',
    secrets + '/{name}',
    'saveSecret',
    'Secrets',
    'Create or replace external API secret',
    {
      role: 'effective team admin',
      body: obj(
        {
          value: {
            type: 'string',
            minLength: 1,
            maxLength: 8192,
            writeOnly: true,
            description: '1–8192 UTF-8 bytes, without CR, LF or NUL.',
          },
        },
        ['value'],
      ),
      result: obj({ saved: bool }),
      description:
        'Named team-scoped value, with no destination restriction. Value never appears in GET responses. Requires a configured encryption key; otherwise returns 503.',
    },
  );
  add('delete', secrets + '/{name}', 'deleteSecret', 'Secrets', 'Delete external API secret', {
    role: 'effective team admin',
    result: obj({ saved: bool }),
  });
  add(
    'get',
    '/orgs/{org}/llm-analytics',
    'getAnalytics',
    'Analytics',
    'Read authorized spend KPIs, trends and sortable rows',
    {
      role: 'organization member; results limited to authorized teams',
      parameters: [
        query('period', {
          ...enumeration(['1d', '1w', '1m', 'mtd', 'ytd', '6m', '1y']),
          default: '1m',
        }),
        query('as_of', date, 'Within past 24 hours; stable pagination snapshot.'),
        query('team', str()),
        query('mode', { ...enumeration(['all', 'test', 'production']), default: 'all' }),
        query('sort', {
          ...enumeration(['cost', 'name', 'tokens', 'requests', 'runs', 'latest']),
          default: 'cost',
        }),
        query('direction', { ...enumeration(['asc', 'desc']), default: 'desc' }),
        query('group', { ...enumeration(['runs', 'teams', 'models']), default: 'runs' }),
        query('page', { ...integer(0, 1000), default: 0 }),
      ],
      description:
        'Currency USD; reported/estimated usage is not a provider invoice. Aggregates only authorized teams; unbounded organization access is not implied.',
    },
  );
  const budgets = '/orgs/{org}/teams/{team}/llm-budget-policies';
  add('get', budgets, 'getBudgetPolicies', 'Budgets', 'Read policies and monthly project spend', {
    role: 'team viewer/editor/admin',
    result: obj({
      items: array(ref('BudgetPolicy')),
      projects: array(ref('Project')),
      workflows: array(ref('DefinitionSummary')),
      spend: array(object),
      alerts: array(object),
      events: array(ref('Audit')),
      changes: array(object),
      month: str(),
      resets_at: date,
      currency: { const: 'USD' },
      timezone: { const: 'UTC' },
    }),
  });
  add(
    'put',
    budgets + '/{policy}',
    'saveBudgetPolicy',
    'Budgets',
    'Save reusable soft and hard limits',
    {
      role: 'effective team admin',
      body: ref('BudgetPolicy'),
      result: ref('BudgetPolicy'),
      description:
        'Optimistic revision check; 409 requires refreshing. Separate monthly UTC allowance per project. Soft limit logs; hard limit pauses, never cancels/completes. Excluded workflows bypass policy.',
    },
  );
  // Keep generated documentation aligned with execution identity/visibility.
  spec.info.description =
    'APIs for the Angular host, workflow builder and management application. Verified human Studio access tokens only; machine tokens are rejected. Viewers read authoring resources; editors/admins author and operate workflows. Detailed execution, schedule and audit reads require a role in RUNLOOM_SENSITIVE_RUN_ROLES (admin by default). Organization owners/admins have effective team-admin access. Configured authentication-strength/freshness checks also apply. Workflow operations return X-Runloom-Request-ID audit receipts; a 503 may mean an operation completed but its outcome receipt could not be persisted. Project folders do not grant execution permissions. Try it out sends REAL requests. There is no general run pause/stop/terminate endpoint. Resume targets a designed or budget pause. Studio starts/tests/webhooks have NO durable Idempotency-Key guarantee: do not blindly retry uncertain starts.';
  spec.tags.push({ name: 'Connections' });
  paths['/orgs/{org}/teams/{team}/connections'] = {
    get: {
      operationId: 'listConnections',
      tags: ['Connections'],
      summary: 'List connection metadata',
      description:
        'Required access: team viewer/editor/admin. Team-scoped. Human Studio tokens only. Passwords are encrypted in the existing workflow vault and never returned. Disable or delete prevents future use; editing affects future node invocations, including published workflows. No automatic retry.',
      'x-required-role': 'team viewer/editor/admin',
      parameters: [
        { name: 'org', in: 'path', required: true, schema: { type: 'string' } },
        { name: 'team', in: 'path', required: true, schema: { type: 'string' } },
      ],
      responses: {
        200: {
          description: 'Success',
          content: {
            'application/json': {
              schema: {
                type: 'object',
                properties: {
                  configured: { type: 'boolean' },
                  items: {
                    type: 'array',
                    items: { $ref: '#/components/schemas/DatabaseConnection' },
                  },
                },
              },
            },
          },
        },
        400: { description: 'Invalid settings or sanitized database failure' },
        401: { description: 'Human Studio login required' },
        403: { description: 'Insufficient team access; consumer credentials rejected' },
        404: { description: 'Connection not available in this team' },
        503: { description: 'Vault or service unavailable' },
      },
    },
  };
  paths['/orgs/{org}/teams/{team}/connections/{connection}'] = {
    put: {
      operationId: 'saveConnection',
      tags: ['Connections'],
      summary: 'Create or update a database connection',
      description:
        'Required access: team admin. Team-scoped. Human Studio tokens only. Passwords are encrypted in the existing workflow vault and never returned. Disable or delete prevents future use; editing affects future node invocations, including published workflows. No automatic retry.',
      'x-required-role': 'team admin',
      parameters: [
        { name: 'org', in: 'path', required: true, schema: { type: 'string' } },
        { name: 'team', in: 'path', required: true, schema: { type: 'string' } },
        { name: 'connection', in: 'path', required: true, schema: { type: 'string' } },
      ],
      responses: {
        200: {
          description: 'Success',
          content: {
            'application/json': {
              schema: { type: 'object', properties: { ok: { type: 'boolean' } } },
            },
          },
        },
        400: { description: 'Invalid settings or sanitized database failure' },
        401: { description: 'Human Studio login required' },
        403: { description: 'Insufficient team access; consumer credentials rejected' },
        404: { description: 'Connection not available in this team' },
        503: { description: 'Vault or service unavailable' },
      },
      requestBody: {
        required: true,
        content: {
          'application/json': { schema: { $ref: '#/components/schemas/DatabaseConnection' } },
        },
      },
    },
    delete: {
      operationId: 'deleteConnection',
      tags: ['Connections'],
      summary: 'Delete connection and encrypted password',
      description:
        'Required access: team admin. Team-scoped. Human Studio tokens only. Passwords are encrypted in the existing workflow vault and never returned. Disable or delete prevents future use; editing affects future node invocations, including published workflows. No automatic retry.',
      'x-required-role': 'team admin',
      parameters: [
        { name: 'org', in: 'path', required: true, schema: { type: 'string' } },
        { name: 'team', in: 'path', required: true, schema: { type: 'string' } },
        { name: 'connection', in: 'path', required: true, schema: { type: 'string' } },
      ],
      responses: {
        200: {
          description: 'Success',
          content: {
            'application/json': {
              schema: { type: 'object', properties: { ok: { type: 'boolean' } } },
            },
          },
        },
        400: { description: 'Invalid settings or sanitized database failure' },
        401: { description: 'Human Studio login required' },
        403: { description: 'Insufficient team access; consumer credentials rejected' },
        404: { description: 'Connection not available in this team' },
        503: { description: 'Vault or service unavailable' },
      },
    },
  };
  paths['/orgs/{org}/teams/{team}/connections/{connection}/{action}'] = {
    post: {
      operationId: 'testConnection',
      tags: ['Connections'],
      summary: 'Test a saved connection',
      description:
        'Required access: team editor/admin. Team-scoped. Human Studio tokens only. Passwords are encrypted in the existing workflow vault and never returned. Disable or delete prevents future use; editing affects future node invocations, including published workflows. No automatic retry.',
      'x-required-role': 'team editor/admin',
      parameters: [
        { name: 'org', in: 'path', required: true, schema: { type: 'string' } },
        { name: 'team', in: 'path', required: true, schema: { type: 'string' } },
        { name: 'connection', in: 'path', required: true, schema: { type: 'string' } },
        { name: 'action', in: 'path', required: true, schema: { const: 'test' } },
      ],
      responses: {
        200: {
          description: 'Success',
          content: {
            'application/json': {
              schema: { type: 'object', properties: { ok: { type: 'boolean' } } },
            },
          },
        },
        400: { description: 'Invalid settings or sanitized database failure' },
        401: { description: 'Human Studio login required' },
        403: { description: 'Insufficient team access; consumer credentials rejected' },
        404: { description: 'Connection not available in this team' },
        503: { description: 'Vault or service unavailable' },
      },
    },
  };
  paths['/orgs/{org}/teams/{team}/connections/{connection}/schema'] = {
    get: {
      operationId: 'discoverConnectionSchema',
      tags: ['Connections'],
      summary: 'Discover tables and columns or sampled MongoDB fields',
      description:
        'Required access: team editor/admin. Team-scoped. Human Studio tokens only. Passwords are encrypted in the existing workflow vault and never returned. Disable or delete prevents future use; editing affects future node invocations, including published workflows. No automatic retry.',
      'x-required-role': 'team editor/admin',
      parameters: [
        { name: 'org', in: 'path', required: true, schema: { type: 'string' } },
        { name: 'team', in: 'path', required: true, schema: { type: 'string' } },
        { name: 'connection', in: 'path', required: true, schema: { type: 'string' } },
      ],
      responses: {
        200: {
          description: 'Success',
          content: {
            'application/json': {
              schema: {
                type: 'object',
                properties: {
                  tables: {
                    type: 'array',
                    items: {
                      type: 'object',
                      properties: {
                        schema: { type: 'string' },
                        name: { type: 'string' },
                        inferred: { type: 'boolean' },
                        columns: {
                          type: 'array',
                          items: {
                            type: 'object',
                            properties: {
                              name: { type: 'string' },
                              type: { type: 'string' },
                              nullable: { type: 'boolean' },
                            },
                          },
                        },
                      },
                    },
                  },
                },
              },
            },
          },
        },
        400: { description: 'Invalid settings or sanitized database failure' },
        401: { description: 'Human Studio login required' },
        403: { description: 'Insufficient team access; consumer credentials rejected' },
        404: { description: 'Connection not available in this team' },
        503: { description: 'Vault or service unavailable' },
      },
    },
  };
  paths['/runs'] = {
    post: {
      operationId: 'startRun',
      tags: ['Runs'],
      summary: 'Start a published main workflow',
      description:
        'Required access: team editor/admin. Child starts are forbidden. Omitted version selects latest published. NOT retry-safe: inspect team runs after a lost response before starting again.',
      'x-required-role': 'team editor/admin',
      parameters: [
        {
          name: 'X-Runloom-Team',
          in: 'header',
          required: true,
          schema: { type: 'string' },
          description:
            'Select an authorized team; cross-team run IDs are rejected before engine access.',
        },
      ],
      responses: {
        201: {
          description: 'Created',
          content: {
            'application/json': { schema: { $ref: '#/components/schemas/StartResponse' } },
          },
        },
        400: {
          description: 'Invalid request or filter',
          content: { 'application/json': { schema: { $ref: '#/components/schemas/Error' } } },
        },
        401: {
          description: 'Human Studio access token required',
          content: { 'application/json': { schema: { $ref: '#/components/schemas/Error' } } },
        },
        403: {
          description: 'Team/role denied',
          content: { 'application/json': { schema: { $ref: '#/components/schemas/Error' } } },
        },
        404: {
          description: 'Resource missing or not in authorized team',
          content: { 'application/json': { schema: { $ref: '#/components/schemas/Error' } } },
        },
        409: {
          description: 'Conflict; refresh state before retrying',
          content: { 'application/json': { schema: { $ref: '#/components/schemas/Error' } } },
        },
        422: {
          description: 'Invalid workflow, input or event',
          content: { 'application/json': { schema: { $ref: '#/components/schemas/Error' } } },
        },
        423: {
          description: 'Competing or expired edit lease',
          content: { 'application/json': { schema: { $ref: '#/components/schemas/Error' } } },
        },
        429: {
          description: 'Code preview capacity exceeded (code/test only)',
          content: { 'application/json': { schema: { $ref: '#/components/schemas/Error' } } },
        },
        500: {
          description: 'Internal storage error',
          content: { 'application/json': { schema: { $ref: '#/components/schemas/Error' } } },
        },
        502: {
          description: 'Execution dependency failed; mutation may be ambiguous',
          content: { 'application/json': { schema: { $ref: '#/components/schemas/Error' } } },
        },
        503: {
          description: 'Directory/storage/dependency unavailable; reconcile before retrying',
          content: { 'application/json': { schema: { $ref: '#/components/schemas/Error' } } },
        },
      },
      requestBody: {
        required: true,
        content: {
          'application/json': {
            schema: { $ref: '#/components/schemas/StartRequest' },
            example: { definition_id: 'example', version: 1, input: {} },
          },
        },
      },
    },
    get: {
      operationId: 'listRuns',
      tags: ['Runs'],
      summary: 'Search stored team runs and execution tags',
      description:
        'Required access: team membership and configured run-payload read policy (admin by default). Searches all stored runs in the selected team before pagination. Returns newest first, with workflow ID as a descending tie-breaker. Includes runs started by other authorized team callers. Search never examines inputs, outputs or state. Offset pagination is not a frozen snapshot: concurrent inserts/deletes can shift pages; clients should deduplicate by workflow_id or refresh. Missing historical metadata is not synthesized.',
      'x-required-role': 'team viewer/editor/admin',
      parameters: [
        {
          name: 'search',
          in: 'query',
          schema: { type: 'string', maxLength: 256 },
          description:
            'Case-insensitive literal substring of workflow ID, definition ID or an execution metadata value. Maximum 256 UTF-8 bytes.',
        },
        {
          name: 'tag_key',
          in: 'query',
          schema: {
            type: 'string',
            enum: [
              'namespace',
              'org_id',
              'team_id',
              'project_id',
              'definition_id',
              'definition_version',
              'is_test',
              'source',
              'root_execution_id',
              'snapshot_id',
            ],
          },
          description:
            'Exact metadata tag filter. Must be supplied together with tag_value; combined with search using AND. Does not broaden team authorization.',
        },
        {
          name: 'tag_value',
          in: 'query',
          schema: { type: 'string', maxLength: 256 },
          description:
            'Case-sensitive exact value, maximum 256 UTF-8 bytes. is_test accepts true or false; definition_version accepts a canonical non-negative integer (0 means draft).',
        },
        {
          name: 'offset',
          in: 'query',
          schema: { type: 'integer', minimum: 0, maximum: 1000000, default: 0 },
          description:
            'Number of matching runs to skip. A short page indicates the end of results.',
        },
        {
          name: 'X-Runloom-Team',
          in: 'header',
          required: true,
          schema: { type: 'string' },
          description:
            'Select an authorized team; cross-team run IDs are rejected before engine access.',
        },
        {
          name: 'limit',
          in: 'query',
          required: false,
          schema: { type: 'integer', minimum: 1, maximum: 200, default: 50 },
          description: '',
        },
        {
          name: 'include_usage',
          in: 'query',
          required: false,
          schema: { type: 'boolean' },
          description: 'Adds bounded usage summaries for up to 20 runs.',
        },
      ],
      responses: {
        200: {
          description: 'Success',
          content: {
            'application/json': {
              schema: { type: 'array', items: { $ref: '#/components/schemas/RunRecord' } },
            },
          },
        },
        400: {
          description: 'Invalid request or filter',
          content: { 'application/json': { schema: { $ref: '#/components/schemas/Error' } } },
        },
        401: {
          description: 'Human Studio access token required',
          content: { 'application/json': { schema: { $ref: '#/components/schemas/Error' } } },
        },
        403: {
          description: 'Team/role denied',
          content: { 'application/json': { schema: { $ref: '#/components/schemas/Error' } } },
        },
        404: {
          description: 'Resource missing or not in authorized team',
          content: { 'application/json': { schema: { $ref: '#/components/schemas/Error' } } },
        },
        409: {
          description: 'Conflict; refresh state before retrying',
          content: { 'application/json': { schema: { $ref: '#/components/schemas/Error' } } },
        },
        422: {
          description: 'Invalid workflow, input or event',
          content: { 'application/json': { schema: { $ref: '#/components/schemas/Error' } } },
        },
        423: {
          description: 'Competing or expired edit lease',
          content: { 'application/json': { schema: { $ref: '#/components/schemas/Error' } } },
        },
        429: {
          description: 'Code preview capacity exceeded (code/test only)',
          content: { 'application/json': { schema: { $ref: '#/components/schemas/Error' } } },
        },
        500: {
          description: 'Internal storage error',
          content: { 'application/json': { schema: { $ref: '#/components/schemas/Error' } } },
        },
        502: {
          description: 'Execution dependency failed; mutation may be ambiguous',
          content: { 'application/json': { schema: { $ref: '#/components/schemas/Error' } } },
        },
        503: {
          description: 'Directory/storage/dependency unavailable; reconcile before retrying',
          content: { 'application/json': { schema: { $ref: '#/components/schemas/Error' } } },
        },
      },
    },
  };
  for (const [path, methods] of Object.entries(deletionPaths)) {
    paths[path] = { ...paths[path], ...methods };
  }
  paths['/catalog'].get.responses['200'].content[
    'application/json'
  ].schema.properties.integrations = integrationCatalog;
  return spec;
}
