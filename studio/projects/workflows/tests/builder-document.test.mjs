import assert from 'node:assert/strict';
import test from 'node:test';
import {readFileSync} from 'node:fs';
import {loadTypeScript} from './helpers/load-typescript.mjs';

const deferred = () => {
  let resolve;
  const promise = new Promise(done => { resolve = done; });
  return {promise, resolve};
};
function builder(overrides = {}, confirm = async () => true) {
  const api = {
    listDefinitions: async () => [], definitionHistory: async () => ({commits: []}),
    validate: async definition => ({definition}),
    save: async definition => definition,
    ...overrides,
  };
  const auth = {canEdit: () => true};
  const core = {
    Component: () => value => value,
    ViewChild: () => () => {}, Input: () => () => {}, Output: () => () => {},
    HostListener: () => () => {}, EventEmitter: class {},
    ChangeDetectorRef: 'detector',
    inject: token => token === 'api' ? api : token === 'auth' ? auth :
      token === 'detector' ? {markForCheck() {}} : token === 'router' ? null : {},
  };
  const timers = new Map();
  const {App} = loadTypeScript(new URL('../src/app/features/builder/workflow-builder.component.ts', import.meta.url), {
    modules: {
      '@angular/core': core, '@angular/common': {}, '@angular/forms': {}, '@angular/router': {Router: 'router'},
      '@runloom/studio-platform': {AuthService: 'auth', WorkspaceBridge: 'bridge', injectDialogs: () => ({confirm})},
      '../../core/api/workflow-api.service': {WorkflowApiService: 'api'},
    },
    globals: {
      Node: class {},
      document: {querySelector: () => null},
      setTimeout: callback => { const id = Symbol(); timers.set(id, callback); return id; },
      clearTimeout: id => timers.delete(id),
    },
  });
  const app = new App();
  app.edit.ensureNewDraftLock = async () => true;
  return {app, api, timers};
}

test('draft snapshots ignore publishing version and normalize empty condition paths', () => {
  const {app} = builder();
  const definition = {...app.definition, steps: [{id: 'condition', type: 'condition'}]};
  app.replaceDefinition(definition);
  definition.version = 5;
  definition.steps[0].then = []; definition.steps[0].else = [];
  assert.equal(app.hasUnsavedChanges, false);
  definition.name = 'Changed';
  assert.equal(app.hasUnsavedChanges, true);
});

test('project browsing is in the main content, not the shared navigation slot', () => {
  const template = readFileSync(new URL('../src/app/features/builder/workflow-builder.component.html', import.meta.url), 'utf8');
  const navigation = template.split('</ng-template>')[0];
  assert.doesNotMatch(navigation, /Workflows|new-workflow-button/);
  assert.doesNotMatch(navigation, /aria-label="Project folders"/);
  assert.match(template, /<runloom-projects-dashboard/);
  assert.match(template, /projectsView && !projectOpened/);
});

test('opening a project respects dirty and busy guards; dashboard reset returns to the directory', async () => {
  for (const allowed of [true, false]) {
    const {app} = builder({}, async () => allowed);
    app.projectsView = true;
    app.definition.name = 'Unsaved draft';
    await app.openProject('second');
    assert.equal(app.projectOpened, allowed);
    assert.equal(app.activeProjectId, allowed ? 'second' : 'default');
    if (allowed) {
      await app.showDashboard(false);
      assert.equal(app.projectOpened, true);
      await app.showDashboard();
      assert.equal(app.projectOpened, false);
    }
  }
  const {app} = builder();
  app.projectBusy = true;
  await app.openProject('second');
  assert.equal(app.projectOpened, false);
  assert.equal(app.activeProjectId, 'default');
});

test('hosted project and definition navigation uses canonical URLs without mutating the current editor', async () => {
  const {app} = builder();
  const paths = [];
  app.router = {navigateByUrl: async path => { paths.push(path); return false; }};
  const original = app.definition;
  await app.openProject('team/project');
  await app.openDefinition('invoice');
  await app.selectProject('default');
  assert.deepEqual(paths, ['/workflows/projects/team%2Fproject', '/workflows/definitions/invoice', '/workflows/projects/default']);
  assert.equal(app.definition, original);
  assert.equal(app.builderOpen, false);
  assert.equal(app.activeProjectId, 'default');
});

test('navigation is blocked during writes and edit acquisition even when the draft is clean', async () => {
  for (const state of ['projectBusy', 'saving', 'acquiringEdit']) {
    const {app} = builder();
    if (state === 'projectBusy') app.projectBusy = true;
    if (state === 'saving') app.versions.saving = true;
    if (state === 'acquiringEdit') app.edit.acquiringEdit = true;
    assert.equal(await app.confirmDiscardChanges(), false);
  }
});

test('dashboard returns whether dirty navigation succeeded and only releases accepted edits', async () => {
  for (const allowed of [true, false]) {
    let released = 0;
    const {app} = builder({}, async () => allowed);
    app.builderOpen = true;
    app.definition.name = 'Unsaved draft';
    app.edit.releaseEditing = async () => { released++; };
    assert.equal(await app.showDashboard(), allowed);
    assert.equal(app.builderOpen, !allowed);
    assert.equal(released, allowed ? 1 : 0);
    if (!allowed) assert.equal(app.definition.name, 'Unsaved draft');
  }
});

test('dashboard navigation is blocked during saves, edit acquisition and workflow opening', async () => {
  const {app} = builder();
  app.builderOpen = true;
  for (const [target, key, value] of [
    [app.versions, 'saving', true], [app.edit, 'acquiringEdit', true], [app, 'openingDefinitionId', 'loading'],
  ]) {
    const previous = target[key];
    target[key] = value;
    assert.equal(await app.showDashboard(), false);
    assert.equal(app.builderOpen, true);
    target[key] = previous;
  }
});

test('invalid drafts can be committed but cannot be published', async () => {
  let saves = 0, publishes = 0;
  const {app} = builder({
    save: async definition => { saves++; return definition; },
    validate: async () => { throw {status: 422, error: {detail: 'Missing input', issues: [{node_id: 'node', message: 'Missing input'}]}}; },
    publish: async () => { publishes++; },
  });
  app.definition.steps.push({id: 'node', type: 'activity'});
  await app.versions.publish();
  assert.equal(saves, 1);
  assert.equal(publishes, 0);
  assert.equal(app.hasUnsavedChanges, false);
  assert.match(app.draftValidationError, /Missing input/);
  assert.match(app.nodeErrors.node, /Missing input/);
  assert.match(app.message, /publishing is blocked/);
});

test('save acknowledgements keep newer edits and do not publish them', async () => {
  const pending = deferred();
  let submitted, publishes = 0;
  const {app} = builder({
    save: definition => { submitted = definition; return pending.promise; },
    publish: async () => { publishes++; },
  });
  app.definition.name = 'Submitted';
  const publishing = app.versions.publish();
  await new Promise(resolve => setImmediate(resolve));
  app.definition.name = 'Newer edits';
  pending.resolve(submitted); await publishing;
  assert.equal(app.definition.name, 'Newer edits');
  assert.equal(app.hasUnsavedChanges, true);
  assert.equal(app.versions.saving, false);
  assert.equal(publishes, 0);
  assert.match(app.message, /Newer edits are still unsaved/);
});

test('publishing updates version without making the draft dirty', async () => {
  const {app} = builder({publish: async () => ({...app.definition, version: 4})});
  app.definition.name = 'Published';
  await app.versions.publish();
  assert.equal(app.definition.version, 4);
  assert.equal(app.hasUnsavedChanges, false);
  assert.equal(app.versions.commitMessage, 'Updated workflow');
  assert.match(app.message, /Version 4 is published/);
});

test('discard confirmation is invalidated by newer edits or document navigation', async () => {
  const pending = deferred();
  const {app} = builder({}, () => pending.promise);
  app.definition.name = 'Unsaved';
  const discarding = app.confirmDiscardChanges();
  app.definition.description = 'Newer edit';
  pending.resolve(true);
  assert.equal(await discarding, false);
  const next = deferred();
  app.dialogs.confirm = () => next.promise;
  const navigating = app.confirmDiscardChanges();
  app.documentGeneration++;
  next.resolve(true);
  assert.equal(await navigating, false);
});

test('unapplied JSON and invalid node fields block saving and remain dirty', async () => {
  let saves = 0;
  const {app} = builder({save: async definition => { saves++; return definition; }});
  app.openJson(); app.jsonDraft = '{';
  assert.equal(await app.versions.save(), false);
  assert.equal(app.hasUnsavedChanges, true);
  app.jsonOpen = false;
  app.editor.openNodePicker(); app.editor.addStep('activity');
  const step = app.editor.selectedStep;
  app.editor.setObjectField('with', '{');
  assert.equal(await app.versions.save(), false);
  assert.match(app.nodeErrors[step.id], /invalid JSON/);
  assert.equal(saves, 0);
  app.replaceDefinition({...app.definition, steps: []});
  assert.equal(app.editor.stepDataDrafts.size, 0);
  assert.equal(app.editor.selectedStep, undefined);
});

test('validation responses cannot mark errors on a newer draft', async () => {
  const pending = deferred();
  const {app} = builder({validate: () => pending.promise});
  const checking = app.checkDraftValidation(structuredClone(app.definition));
  app.definition.name = 'Newer draft';
  pending.resolve({definition: app.definition});
  await checking;
  assert.equal(Object.keys(app.nodeErrors).length, 0);
  assert.equal(app.definition.name, 'Newer draft');
});

test('failed confirmed exit restores the unsaved-change warning', async () => {
  const {app} = builder();
  app.definition.name = 'Unsaved';
  await assert.rejects(app.withConfirmedExit(async () => { throw new Error('Logout failed'); }));
  let prevented = false;
  const event = {preventDefault: () => { prevented = true; }, returnValue: undefined};
  app.beforeUnload(event);
  assert.equal(prevented, true);
  assert.equal(event.returnValue, '');
});

test('Ctrl/Cmd+S suppresses HTML download and saves only an editable workflow outside dialogs', () => {
  const {app} = builder();
  let saves = 0, prevented = 0;
  app.versions.save = async () => { saves++; return true; };
  const event = (overrides = {}) => ({key: 's', ctrlKey: true, metaKey: false, shiftKey: false, altKey: false, repeat: false, isComposing: false, preventDefault: () => prevented++, ...overrides});
  app.saveShortcut(event());
  assert.equal(saves, 0, 'dashboard does not save');
  assert.equal(prevented, 1);
  app.builderOpen = true;
  app.saveShortcut(event());
  app.saveShortcut(event({ctrlKey: false, metaKey: true, key: 'S'}));
  assert.equal(saves, 2);
  for (const overrides of [{repeat: true}, {isComposing: true}]) app.saveShortcut(event(overrides));
  app.jsonOpen = true;
  app.saveShortcut(event());
  app.jsonOpen = false;
  app.definitions = [{id: app.definition.id, latest_version: 0, has_draft: true}];
  app.saveShortcut(event());
  assert.equal(saves, 2, 'read-only workflow and modal do not save');
  const count = prevented;
  app.saveShortcut(event({shiftKey: true}));
  app.saveShortcut(event({altKey: true}));
  app.saveShortcut(event({ctrlKey: false}));
  assert.equal(prevented, count, 'unrelated shortcuts remain native');
});

test('session undo/redo restores graph and layout without publishing and fences text/dialog/busy actions', () => {
  const {app} = builder();
  app.builderOpen = true;
  const baseline = app.definition.steps.length;
  app.definition.steps.push({id: 'undo-node', type: 'wait', seconds: 2}); app.markChanged();
  app.editor.updateCanvasLayout({'undo-node': {x: 70, y: 90}});
  app.restoreHistory(); assert.equal(app.definition.layout, undefined);
  app.restoreHistory(); assert.equal(app.definition.steps.length, baseline);
  app.restoreHistory(true); assert.equal(app.definition.steps.length, baseline + 1);
  let prevented = 0;
  const event = {key: 'z', ctrlKey: true, preventDefault: () => prevented++};
  app.historyShortcut({...event, target: {tagName: 'TEXTAREA'}});
  assert.equal(app.definition.steps.length, baseline + 1);
  app.versions.saving = true; app.historyShortcut(event); assert.equal(prevented, 0);
  app.versions.saving = false; app.historyShortcut(event);
  assert.equal(prevented, 1); assert.equal(app.definition.steps.length, baseline);
  app.historyShortcut({...event, metaKey: true, ctrlKey: false, shiftKey: true});
  assert.equal(app.definition.steps.length, baseline + 1);
  app.replaceDefinition({...app.definition, id: 'another'});
  assert.equal(app.history.canUndo, false);
});

test('automatic saves commit with an autosave message, retain newer edits and never publish', async () => {
  const wait = deferred();
  let submitted, message, publishes = 0;
  const {app} = builder({save: (definition, text) => { submitted = definition; message = text; return wait.promise; },
    publish: async () => { publishes++; }});
  app.definition.name = 'Submitted'; app.markChanged();
  const saving = app.versions.save(true);
  await new Promise(resolve => setImmediate(resolve));
  app.definition.name = 'Newer'; app.markChanged();
  wait.resolve(submitted); await saving;
  assert.equal(message, 'Autosave draft');
  assert.equal(app.definition.name, 'Newer');
  assert.equal(app.hasUnsavedChanges, true);
  assert.equal(app.versions.lastWriteSucceeded, true);
  assert.equal(publishes, 0);
});

test('autosave requires a first save and editing access, pauses invalid JSON and recovers failed writes', async () => {
  let writes = 0, offline = true;
  const {app} = builder({save: async definition => { writes++; if (offline) throw new Error('offline'); return definition; }});
  app.builderOpen = true;
  app.definition.name = 'Changed'; app.markChanged();
  await app.autosave.retry(); assert.equal(writes, 0);
  app.definitions = [{id: app.definition.id, latest_version: 0, has_draft: true}];
  await app.autosave.retry(); assert.equal(writes, 0, 'read-only cannot autosave');
  app.edit.editingId = app.definition.id; app.edit.editingToken = 'owned'; app.edit.editUntil = Date.now() + 60000;
  app.editor.schemaDrafts.set(app.definition, '{');
  await app.autosave.retry(); assert.equal(writes, 0);
  app.editor.schemaDrafts.clear();
  await app.autosave.retry(); assert.equal(writes, 1);
  assert.equal(app.definition.name, 'Changed'); assert.equal(app.hasUnsavedChanges, true);
  assert.match(app.saveStatus, /Save failed/);
  offline = false;
  await app.autosave.retry(); assert.equal(writes, 2);
  assert.equal(app.hasUnsavedChanges, false);
});

test('applied workflow JSON is one reversible change and invalid raw JSON is retained', async () => {
  const {app} = builder();
  app.builderOpen = true;
  const before = app.definition.name;
  app.openJson(); app.jsonDraft = '{';
  await app.applyJson(); assert.equal(app.jsonDraft, '{');
  assert.equal(app.history.canUndo, false);
  app.jsonDraft = JSON.stringify({...app.definition, name: 'Imported'});
  await app.applyJson(); assert.equal(app.definition.name, 'Imported');
  app.restoreHistory(); assert.equal(app.definition.name, before);
  app.restoreHistory(true); assert.equal(app.definition.name, 'Imported');
});

test('a confirmed general-chat creation refreshes the dashboard without replacing the current document', async () => {
  const {app} = builder({listDefinitions: async () => [{id: 'workflow-new', name: 'Created', latest_version: 0, has_draft: true}]});
  const original = app.definition;
  await app.refreshAuthoringDraft('workflow-new');
  assert.equal(app.definitions[0].id, 'workflow-new');
  assert.equal(app.definition, original);
  assert.equal(app.builderOpen, false);
});

test('Lumi accepts a clean owned editing lease and saves pending edits before context reads',async()=>{
 let writes=0;const {app}=builder({save:async d=>{writes++;return d;}});
 app.builderOpen=true;app.definitions=[{id:app.definition.id,has_draft:true,latest_version:0}];
 app.edit.editingId=app.definition.id;app.edit.editingToken='own-lease';app.edit.editUntil=Date.now()+60000;
 assert.equal(app.assistantContext.canApply,true);
 app.definition.name='Pending canvas edit';app.markChanged();assert.equal(app.assistantContext.canApply,false);
 await app.prepareAuthoringDraft();assert.equal(writes,1);assert.equal(app.hasUnsavedChanges,false);assert.equal(app.edit.editingToken,'own-lease');assert.equal(app.assistantContext.canApply,true);
 app.jsonOpen=true;app.jsonDraft='{';await assert.rejects(app.prepareAuthoringDraft());assert.equal(app.jsonDraft,'{');assert.equal(writes,1);
 app.edit.editLost=true;assert.equal(app.assistantContext.canApply,false);await assert.rejects(app.prepareAuthoringDraft());
});

test('Lumi waits for an in-flight save and refuses a workflow switch during preparation',async()=>{
 const {app,timers}=builder();app.builderOpen=true;app.definitions=[{id:app.definition.id,has_draft:true,latest_version:0}];
 app.versions.saving=true;const pending=app.prepareAuthoringDraft();assert.equal(timers.size,1);
 app.versions.saving=false;for(const [id,run] of [...timers]){timers.delete(id);run();}await pending;
 app.versions.saving=true;const switched=app.prepareAuthoringDraft();app.documentGeneration++;app.versions.saving=false;
 for(const [id,run] of [...timers]){timers.delete(id);run();}await assert.rejects(switched);
});

test('canvas shortcuts use enabled actions and ignore typing, repeats and modified keys', async () => {
  const {app} = builder();
  app.builderOpen = true;
  const clicked = [];
  let disabled = false;
  app.host = {nativeElement: {querySelector(selector) {
    return selector.startsWith('button') ? {disabled, click: () => clicked.push(selector)} : null;
  }}};
  app.changeDetector.detectChanges = () => {};
  const press = (key, options = {}) => app.canvasShortcut({key, preventDefault() {}, ...options});
  for (const key of ['e', 'a', 't', 'h', '?']) await press(key);
  for (const options of [{target: {tagName: 'INPUT'}}, {repeat: true}, {ctrlKey: true}, {shiftKey: true}]) {
    await press('e', options);
  }
  disabled = true;
  await press('e');
  assert.deepEqual(clicked, ['E', 'A', 'T', 'H', '?'].map(key => `button[aria-keyshortcuts="${key}"]`));
});

test('keyboard publication requires confirmation for the same workflow', async () => {
  for (const decision of ['cancel', 'accept', 'changed']) {
    const {app} = builder({}, async message => {
      assert.ok(message.includes(app.definition.name));
      if (decision === 'changed') app.definition.name += ' edited';
      return decision !== 'cancel';
    });
    app.builderOpen = true;
    let clicks = 0;
    app.host = {nativeElement: {querySelector: () => ({isConnected: true, click: () => clicks++})}};
    await app.canvasShortcut({key: 'P', shiftKey: true, preventDefault() {}});
    assert.equal(clicks, decision === 'accept' ? 1 : 0);
  }
});
