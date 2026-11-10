import test from 'node:test';
import assert from 'node:assert/strict';
import {createRequire} from 'node:module';
const require = createRequire(import.meta.url);
require('../license-compat.cjs')();
const {WebpackInnerModuleIterator} = require('license-webpack-plugin/dist/WebpackInnerModuleIterator');
const iterator = new WebpackInnerModuleIterator(require.resolve);
test('license extraction recognizes old and new federated module identifiers', () => {
  assert.equal(iterator.getActualFilename('provide module (default) rxjs@7.8.2 = /packages/rxjs/index.js'),'/packages/rxjs/index.js');
  assert.equal(iterator.getActualFilename('provide module (default) rxjs@7.8.2|/packages/rxjs/index.js'),'/packages/rxjs/index.js');
  assert.equal(iterator.getActualFilename('/packages/angular/core.mjs'),'/packages/angular/core.mjs');
  assert.equal(iterator.getActualFilename('container entry shared'),null);
});
