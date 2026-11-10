import assert from 'node:assert/strict';
import test from 'node:test';
import {readFileSync} from 'node:fs';
import {loadTypeScript} from './helpers/load-typescript.mjs';

const base = new URL('../src/app/features/builder/', import.meta.url);
const {makeStep, emptyDefinition} = loadTypeScript(new URL('model/workflow-defaults.ts', base));
const {workflowPalette, stepSummary} = loadTypeScript(new URL('model/node-presentation.ts', base));
const {allSteps, duplicateTree, parallelPath} = loadTypeScript(new URL('model/workflow-tree.ts', base));
const {buildCanvas} = loadTypeScript(new URL('components/canvas/canvas-layout.ts', base));
const plain = value => JSON.parse(JSON.stringify(value));
const decorators = {Component: () => value => value, Input: () => () => {}, Output: () => () => {}, EventEmitter: class { emit(value) { this.last = value; } }};

test('every new building node has a palette entry, configuration and summary', () => {
  for (const type of ['transform', 'collection', 'merge', 'foreach', 'fail', 'integration']) {
    const step = makeStep(type, emptyDefinition(), []);
    assert.equal(step.type, type);
    assert.ok(workflowPalette.find(item => item.type === type));
    assert.ok(stepSummary(step));
  }
});
test('For Each renders one body and collection join and participates in tree operations', () => {
  const step = makeStep('foreach', emptyDefinition(), []);
  const scene = buildCanvas([step]);
  assert.ok(scene.nodes.some(node => node.label === 'Collect · all items'));
  assert.equal(allSteps([step]).length, 2);
  assert.equal(parallelPath([step], step.branches[0].steps), true);
  const copy = duplicateTree(step, [step]);
  assert.notEqual(copy.id, step.id);
  assert.notEqual(copy.branches[0].steps[0].id, step.branches[0].steps[0].id);
  assert.equal(copy.branches[0].id, 'body');
});
test('Fail is terminal in the canvas', () => {
  const scene = buildCanvas([makeStep('fail', emptyDefinition(), [])]);
  assert.equal(scene.nodes.some(node => node.kind === 'finish'), false);
});
test('data editor preserves malformed JSON and resets incompatible settings on operation changes', () => {
  const {DataEditor} = loadTypeScript(new URL('components/data-editor/data-editor.component.ts', base), {modules: {'@angular/core': decorators, '@angular/common': {}, '@angular/forms': {}}});
  const editor = new DataEditor();
  editor.editable = true; editor.step = makeStep('collection', emptyDefinition(), []);
  const original = plain(editor.step.data);
  editor.setJSON('{');
  assert.equal(editor.invalid.last, '{');
  assert.deepEqual(plain(editor.step.data), original);
  editor.setOperation('limit');
  assert.deepEqual(plain(editor.step.data), {operation: 'limit', source: '${input.items}', limit: 100});
  editor.editable = false;
  editor.setOperation('sort');
  assert.equal(editor.step.data.operation, 'limit');
});
test('LLM task switching preserves prompt and replaces only task-specific options', () => {
  const {LLMTaskEditor} = loadTypeScript(new URL('components/llm-task-editor/llm-task-editor.component.ts', base), {modules: {'@angular/core': decorators, '@angular/common': {}, '@angular/forms': {}}});
  const editor = new LLMTaskEditor();
  editor.editable = true; editor.step = makeStep('llm', emptyDefinition(), []);
  const prompt = editor.step.prompt;
  editor.setKind('classify'); editor.setLabels('routine\nurgent\n');
  assert.deepEqual(plain(editor.step.llm_task.labels), ['routine', 'urgent']);
  editor.setKind('extract');
  assert.equal(editor.step.llm_task.labels, undefined);
  assert.equal(editor.step.llm_task.fields.length, 1);
  assert.equal(editor.step.prompt, prompt);
  editor.setKind('');
  assert.equal(editor.step.llm_task, undefined);
});
test('building controls use external templates and acknowledge real integration side effects', () => {
  const markup = readFileSync(new URL('components/integration-editor/integration-editor.component.html', base), 'utf8');
  assert.match(markup, /OAuth sign-in and automatic token refresh are not included/);
  assert.match(markup, /including during workflow tests/);
  assert.match(markup, /pages are not fetched automatically/);
});
