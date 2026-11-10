import assert from 'node:assert/strict';
import test from 'node:test';
import {loadTypeScript} from './helpers/load-typescript.mjs';

const feature = '../src/app/features/assistant/services/';
const load = (name, globals) => loadTypeScript(new URL(feature + name, import.meta.url), {globals});
const auth = {
  bearer: async () => 'synthetic-token', profile: () => ({user: {id: 'user'}}),
  organizationId: () => 'org', teamId: () => 'team',
};

test('HTTP requests retain workflow scope, method, timeout and structured limit errors', async () => {
  const calls = [];
  const {AssistantApi, ChatRequestError} = load('assistant-api.ts', {
    AbortSignal,
    fetch: async (...args) => {
      calls.push(args);
      return {ok: false, status: 409, json: async () => ({code: 'conversation_limit', detail: 'Conversation is full'})};
    },
  });
  const api = new AssistantApi({auth, epoch: () => 0, workflowId: () => 'parent'});
  const url = api.url('conversations?after=cursor');
  assert.equal(new URL(url, 'http://localhost').searchParams.get('authoring_version'), '2');
  assert.equal(new URL(url, 'http://localhost').searchParams.get('workflow_id'), 'parent');
  await assert.rejects(api.request(url, {request_id: 'same-id'}), error =>
    error instanceof ChatRequestError && error.code === 'conversation_limit' &&
    error.status === 409 && error.message === 'Conversation is full');
  assert.equal(calls[0][1].method, 'POST');
  assert.equal(calls[0][1].headers.Authorization, 'Bearer synthetic-token');
  assert(calls[0][1].signal instanceof AbortSignal);
  const general = new AssistantApi({auth, epoch: () => 0, workflowId: () => undefined});
  assert.equal(new URL(general.url('config'), 'http://localhost').searchParams.has('workflow_id'), false);
});

test('a scope/account change while acquiring a token prevents sending the HTTP request', async () => {
  let done, calls = 0, epoch = 0;
  const {AssistantApi} = load('assistant-api.ts', {AbortSignal, fetch: async () => { calls++; }});
  const api = new AssistantApi({
    auth: {...auth, bearer: () => new Promise(resolve => { done = resolve; })},
    epoch: () => epoch, workflowId: () => undefined,
  });
  const request = api.request('/config');
  epoch++; done('synthetic-token');
  await assert.rejects(request, /Chat context changed/);
  assert.equal(calls, 0);
});

function streamContext(overrides = {}) {
  return {
    auth, epoch: () => 0, isOpen: () => true, url: path => path,
    revision: () => 0, update() {}, activeJob: () => false,
    connection() {}, error() {}, accessChanged() {}, ...overrides,
  };
}

test('SSE decodes split UTF-8 events, filters stale revisions and releases the reader', async () => {
  const updates = [], states = [];
  let revision = 2, canceled = false;
  const bytes = new TextEncoder().encode('data: {"id":"chat","revision":1}\n\ndata: {"id":"chat","revision":3,"content":"Hello 🌟"}\n\n');
  const chunks = [bytes.slice(0, 12), bytes.slice(12, bytes.length - 6), bytes.slice(bytes.length - 6)];
  const {ConversationStream} = load('conversation-stream.ts', {
    AbortController, TextDecoder,
    fetch: async () => ({ok: true, body: {getReader: () => ({
      read: async () => chunks.length ? {value: chunks.shift(), done: false} : {done: true},
      cancel: async () => { canceled = true; },
    })}}),
  });
  const stream = new ConversationStream(streamContext({
    revision: () => revision,
    update: snapshot => { revision = snapshot.revision; updates.push(snapshot); },
    connection: value => states.push(value),
  }));
  await stream.watch('chat');
  assert.equal(updates.length, 1);
  assert.equal(updates[0].content, 'Hello 🌟');
  assert.deepEqual(states, ['Connected', 'Saved']);
  assert.equal(canceled, true);
});

test('SSE terminates on revoked access and bounds reconnect attempts', async () => {
  let calls = 0, status = 403, access = '', error = '';
  const {ConversationStream} = load('conversation-stream.ts', {
    AbortController, TextDecoder,
    setTimeout: callback => { queueMicrotask(callback); },
    fetch: async () => { calls++; return {ok: false, status}; },
  });
  await new ConversationStream(streamContext({accessChanged: value => { access = value; }})).watch('chat');
  assert.equal(calls, 1); assert.match(access, /access changed/);
  calls = 0; status = 503;
  await new ConversationStream(streamContext({error: value => { error = value; }})).watch('chat');
  assert.equal(calls, 5);
  assert.match(error, /job continues on the server/);
});

test('closing or changing scope during SSE authentication prevents an old connection', async () => {
  let release, calls = 0;
  const {ConversationStream} = load('conversation-stream.ts', {
    AbortController, TextDecoder, fetch: async () => { calls++; },
  });
  const stream = new ConversationStream(streamContext({
    auth: {bearer: () => new Promise(resolve => { release = resolve; })},
  }));
  const watching = stream.watch('chat');
  stream.controller.abort(); release('synthetic-token');
  await watching;
  assert.equal(calls, 0);
});

test('storage access stays lazy and account changes remove only tab draft keys', () => {
  const tab = {
    'runloom-chat-session:a': 'draft', unrelated: 'keep',
    removeItem(key) { delete this[key]; },
  };
  const {ChatStorage} = load('chat-storage.ts', {sessionStorage: tab, localStorage: {}});
  const storage = new ChatStorage();
  storage.clearTabDrafts();
  assert.equal(tab['runloom-chat-session:a'], undefined);
  assert.equal(tab.unrelated, 'keep');
  assert.equal(storage.preferenceKey('user:org:team'), 'runloom-assistant:user:org:team');
});
