import assert from 'node:assert/strict';
import {mkdtemp, mkdir, readFile, writeFile, copyFile, rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {spawnSync} from 'node:child_process';
import test from 'node:test';

const projects = ['server/auth', 'server/workflows', 'server/management', 'server/gateway', 'server/assistant', 'tools/database-setup'];

for (const fail of [false, true]) {
  test(`vendor refresh ${fail ? 'preserves all vendors on preparation failure' : 'copies dependencies without permanent SDK replacements'}`, async () => {
    const root = await mkdtemp(join(tmpdir(), 'runloom-vendor-test-'));
    try {
      await mkdir(join(root, 'scripts'));
      await copyFile(new URL('../vendor-go.sh', import.meta.url), join(root, 'scripts/vendor-go.sh'));
      for (const project of projects) {
        await mkdir(join(root, project, 'vendor'), {recursive: true});
        await writeFile(join(root, project, 'go.mod'), 'module fixture\n');
        await writeFile(join(root, project, 'go.sum'), 'original sums\n');
        await writeFile(join(root, project, 'vendor/original'), 'original vendor\n');
      }
      const go = join(root, 'mock-go');
      await writeFile(go, `#!/bin/sh
set -eu
[ "$GOWORK" = off ]
if [ "$2" = edit ]; then exit 0; fi
[ "$2" = vendor ]
${fail ? 'case "$PWD" in */tools/database-setup) exit 42;; esac' : ''}
mkdir -p "$5"
printf '# github.com/example/runloom/sdk/go-auth v0.1.0 => /local/sdk\n## explicit\ngithub.com/example/runloom/sdk/go-auth\n# github.com/example/runloom/sdk/go-auth => /local/sdk\n' > "$5/modules.txt"
`, {mode: 0o755});
      const result = spawnSync('sh', [join(root, 'scripts/vendor-go.sh')], {env: {...process.env, GO: go}, encoding: 'utf8'});
      assert.equal(result.status, fail ? 42 : 0, result.stderr);
      for (const project of projects) {
        assert.equal(await readFile(join(root, project, 'go.mod'), 'utf8'), 'module fixture\n');
        assert.equal(await readFile(join(root, project, 'go.sum'), 'utf8'), 'original sums\n');
        if (fail) assert.equal(await readFile(join(root, project, 'vendor/original'), 'utf8'), 'original vendor\n');
        else assert.equal(await readFile(join(root, project, 'vendor/modules.txt'), 'utf8'), '# github.com/example/runloom/sdk/go-auth v0.1.0\n## explicit\ngithub.com/example/runloom/sdk/go-auth\n');
      }
    } finally {
      await rm(root, {recursive: true, force: true});
    }
  });
}

test('vendored coverage.go files are not ignored', () => {
  for (const project of projects.filter(p => p !== 'server/gateway')) {
    const result = spawnSync('git', ['check-ignore', `${project}/vendor/golang.org/x/text/language/coverage.go`], {encoding: 'utf8'});
    assert.equal(result.status, 1, result.stdout);
  }
});
