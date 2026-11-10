import assert from 'node:assert/strict';
import test from 'node:test';
import { readFile } from 'node:fs/promises';
import { studioSpec } from '../studio-spec.mjs';

const workflow = JSON.parse(
  await readFile(new URL('../../../spec/workflow.schema.json', import.meta.url), 'utf8'),
);
const schedule = JSON.parse(
  await readFile(new URL('../../../spec/schedule.schema.json', import.meta.url), 'utf8'),
);
const spec = studioSpec(workflow, schedule);

test('generation retains draft-only workflow and empty-project deletion contracts', () => {
  for (const [path, id, extraStatus] of [
    ['/definitions/{id}', 'deleteDraftWorkflow', '423'],
    ['/projects/{project}', 'deleteEmptyProject', '409'],
  ]) {
    const operation = spec.paths[path].delete;
    assert.equal(operation.operationId, id);
    assert.equal(operation['x-required-role'], 'team editor/admin');
    for (const status of ['204', '401', '403', '404', '409', '503', extraStatus]) {
      assert(operation.responses[status], `${id}: missing ${status}`);
    }
    assert(
      operation.parameters.some(
        (p) => p.name === 'X-Runloom-Team' && p.in === 'header' && p.required,
      ),
    );
  }
  assert(spec.paths['/definitions/{id}'].get, 'Deletion must not replace existing reads');
  const editToken = spec.paths['/definitions/{id}'].delete.parameters.find(
    (parameter) => parameter.name === 'X-Runloom-Edit-Token',
  );
  assert.equal(editToken.required, false);
});

test('integration catalog includes action metadata and typed parameter mapping', () => {
  const catalog = spec.paths['/catalog'].get.responses['200'].content['application/json'].schema;
  const integrations = catalog.properties.integrations;
  assert.equal(integrations.type, 'array');
  assert.deepEqual(integrations.items.required, [
    'id',
    'provider',
    'name',
    'description',
    'parameters',
    'documentation',
    'mutates',
  ]);
  assert.equal(integrations.items.properties.mutates.type, 'boolean');
  assert.deepEqual(integrations.items.properties.parameters.items.properties.type.enum, [
    'string',
    'array',
  ]);
});

test('current workflow node schemas are carried into the generated Studio contract', () => {
  assert.deepEqual(spec.components.schemas.WorkflowDefinition.$defs, workflow.$defs);
  for (const type of ['integration', 'transform', 'collection', 'merge', 'foreach', 'fail']) {
    assert(
      Object.values(workflow.$defs).some(
        (schema) =>
          schema.properties?.type?.const === type || schema.properties?.type?.enum?.includes(type),
      ),
      `Missing current node schema: ${type}`,
    );
  }
});
