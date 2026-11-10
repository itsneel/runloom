import test from 'node:test';
import assert from 'node:assert/strict';
import {runInNewContext} from 'node:vm';
import {spawnSync} from 'node:child_process';
import {mongoDNSGate, mongoProbe} from '../local-mongodb.mjs';

function probe({primary = false, code, auth = 1, initialize = true, setName = 'runloom'} = {}) {
  let exit, initiated = 0;
  runInNewContext(mongoProbe('runloom-database:27017', initialize), {
    db: {getSiblingDB: () => ({auth: () => auth}), hello: () => ({isWritablePrimary: primary, setName})},
    require: () => ({readFileSync: () => 'synthetic-password'}),
    rs: {status: () => {if (code) throw {code};}, initiate: config => {
      initiated++;
      assert.equal(config._id, 'runloom');
      assert.equal(config.members[0].host, 'runloom-database:27017');
      return {ok: 1};
    }},
    quit: status => {exit = status;},
  });
  return {exit, initiated};
}

test('startup initializes only NotYetInitialized and waits for primary election', () => {
  assert.deepEqual(probe({code: 94}), {exit: 1, initiated: 1});
  assert.deepEqual(probe({primary: true}), {exit: 0, initiated: 0});
  assert.deepEqual(probe({primary: true, setName: '', code: 76}), {exit: 1, initiated: 0});
  for (const code of [93, 13, 23, 74, undefined]) {
    assert.deepEqual(probe({code}), {exit: 1, initiated: 0});
  }
  assert.deepEqual(probe({auth: 0, code: 94}), {exit: 1, initiated: 0});
  assert.deepEqual(probe({auth: {ok: 0}, code: 94}), {exit: 1, initiated: 0});
});

test('readiness never initializes or reconfigures an existing database', () => {
  assert.deepEqual(probe({initialize: false, code: 94}), {exit: 1, initiated: 0});
  assert.deepEqual(probe({initialize: false, primary: true}), {exit: 0, initiated: 0});
  assert(!mongoProbe('db:27017', false).includes('rs.'));
  assert(!mongoProbe('db:27017', true).includes('reconfig'));
});

test('DNS gate accepts only the current pod address, not a stale or missing address', () => {
  for (const [address, expected] of [['10.244.0.23', 0], ['10.244.0.99', 1], ['', 1]]) {
    const result = spawnSync('bash', ['-ec', `getent() { printf '%s STREAM database\n' "$TEST_DNS_ADDRESS"; }; sleep() { :; }; ${mongoDNSGate}`], {
      env: {...process.env, RUNLOOM_POD_IP: '10.244.0.23', RUNLOOM_MONGO_HOST: 'database', TEST_DNS_ADDRESS: address},
      encoding: 'utf8', timeout: 10000,
    });
    assert.equal(result.status, expected);
  }
});
