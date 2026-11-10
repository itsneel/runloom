import assert from 'node:assert/strict';
import test from 'node:test';
import {readFileSync} from 'node:fs';
import {loadTypeScript} from './helpers/load-typescript.mjs';

const {ProjectsDashboard} = loadTypeScript(new URL('../src/app/features/projects/projects-dashboard.component.ts', import.meta.url), {
  modules: {
    '@angular/core': { Component: () => value => value, Input: () => () => {}, Output: () => () => {}, EventEmitter: class {} },
    '@angular/common': {}, '@angular/forms': {}, '@runloom/studio-platform': {},
  },
});

test('projects use an accessible table with stable rows and unchanged action guards', () => {
  const page = new ProjectsDashboard();
  assert.equal(page.trackProject(0, {id: 'project-one', name: 'One'}), 'project-one');
  const template = readFileSync(new URL('../src/app/features/projects/projects-dashboard.component.html', import.meta.url), 'utf8');
  assert.match(template, /<table>/);
  assert.match(template, /<th scope="col">Project<\/th>/);
  assert.match(template, /<th scope="col">Workflows<\/th>/);
  assert.match(template, /trackBy: trackProject/);
  assert.match(template, /canEdit && project.id !== 'default'/);
  assert.match(template, /\[disabled\]="busy \|\| count\(project.id\) > 0"/);
  assert.doesNotMatch(template, /project-card|project-grid/);
});
test('projects search names and IDs, sorts without mutation, and counts default workflows', () => {
  const page = new ProjectsDashboard();
  page.projects = [{id: 'second', name: 'Messaging'}, {id: 'default', name: 'General'}];
  page.workflows = [{id: 'one'}, {id: 'two', project_id: 'default'}, {id: 'three', project_id: 'second'}];
  assert.deepEqual(page.visibleProjects.map(p => p.id), ['default', 'second']);
  assert.equal(page.projects[0].id, 'second');
  assert.equal(page.count('default'), 2);
  assert.equal(page.count('second'), 1);
  assert.equal(page.count('empty'), 0);
  page.search = ' MESS ';
  assert.deepEqual(page.visibleProjects.map(p => p.id), ['second']);
  page.search = 'default';
  assert.deepEqual(page.visibleProjects.map(p => p.id), ['default']);
});
