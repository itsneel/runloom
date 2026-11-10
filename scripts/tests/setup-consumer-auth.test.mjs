import test from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { mkdtemp, readFile, rm, stat, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { setup } from '../setup-consumer-auth.mjs';

test('operator bootstrap creates or restores a missing client without rewriting a saved credential', async t => {
  for (const reuseCredential of [false, true]) {
    const directory = await mkdtemp(join(tmpdir(), 'runloom-provisioner-protocol-'));
    const secretFile = join(directory, 'credential');
    const tokenFile = join(directory, 'token');
    const saved = 'synthetic-saved-credential-that-must-not-change\n';
    await writeFile(tokenFile, 'synthetic-admin-token', {mode: 0o600});
    if (reuseCredential) await writeFile(secretFile, saved, {mode: 0o640});
    const before = reuseCredential ? await stat(secretFile) : undefined;
    const requests = [];
    const server = createServer(async (req, res) => {
      let raw = '';
      for await (const chunk of req) raw += chunk;
      const body = raw ? JSON.parse(raw) : undefined;
      requests.push({method: req.method, path: req.url, body, authorization: req.headers.authorization});
      res.setHeader('Content-Type', 'application/json');
      if (req.method === 'POST' && req.url.endsWith('/clients')) {
        res.writeHead(201, {Location: '/admin/realms/runloom/clients/provisioner'});
        res.end();
      } else if (req.method !== 'GET') { res.writeHead(204); res.end(); }
      else if (req.url.endsWith('/service-account-user')) res.end(JSON.stringify({id: 'service-user'}));
      else if (req.url.includes('?clientId=realm-management')) res.end(JSON.stringify([{id: 'management'}]));
      else if (req.url.includes('/roles/')) res.end(JSON.stringify({id: req.url.split('/').at(-1), name: req.url.split('/').at(-1)}));
      else res.end('[]');
    });
    await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
    try {
      await setup({baseURL: `http://127.0.0.1:${server.address().port}`, realm: 'runloom', tokenFile, secretFile, reuseCredential});
      const credential = await readFile(secretFile, 'utf8');
      const client = requests.find(r => r.method === 'POST' && r.path.endsWith('/clients'));
      assert.equal(client.body.secret, credential.trim());
      assert.equal(client.body.enabled, false);
      assert.equal(client.body.serviceAccountsEnabled, true);
      assert.ok(requests.every(r => r.authorization === 'Bearer synthetic-admin-token'));
      assert.ok(requests.some(r => r.method === 'POST' && r.path.includes('/role-mappings/') && r.body.length === 3));
      assert.ok(requests.some(r => r.method === 'PUT' && r.body.enabled === true));
      if (reuseCredential) {
        assert.equal(credential, saved);
        const after = await stat(secretFile);
        assert.equal(after.mtimeMs, before.mtimeMs);
        assert.equal(after.mode, before.mode);
      } else assert.equal((await stat(secretFile)).mode & 0o777, 0o600);
      const requestCount = requests.length;
      await assert.rejects(setup({baseURL: `http://127.0.0.1:${server.address().port}`, realm: 'runloom', tokenFile, secretFile}), {code: 'EEXIST'});
      assert.equal(requests.length, requestCount);
      assert.equal(await readFile(secretFile, 'utf8'), credential);
    } finally {
      await new Promise(resolve => server.close(resolve));
      await rm(directory, {recursive: true, force: true});
    }
  }
});
