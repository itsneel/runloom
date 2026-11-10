import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import MiniSearch from 'minisearch';
import { renderMarkdown } from '../scripts/markdown.mjs';
import { safeStudioUrl } from '../src/app/core/settings/destination.ts';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const read = (file) => fs.readFile(path.join(root, file), 'utf8');
const pages = JSON.parse(await read('content/pages.json'));

test('every page has an independent packaged source and unique route', async () => {
  assert.equal(new Set(pages.map((page) => page.slug)).size, pages.length);
  for (const page of pages) {
    const source = page.file || `imported/${path.basename(page.source)}`;
    assert.ok((await read('content/' + source)).startsWith('# '), source);
    assert.ok(page.title && page.description && page.group);
    assert.match(page.slug, /^[a-z0-9-]+\/[a-z0-9-]+$/);
  }
});
test('headings receive stable, unique anchors and the article owns the H1', () => {
  const result = renderMarkdown('# Main\n\n## Start here\n\n## Start here\n\n### Details');
  assert.deepEqual(
    result.headings.map((item) => item.id),
    ['start-here', 'start-here-1', 'details'],
  );
  assert.ok(!result.html.includes('<h1'));
});
test('imported articles have short summaries and retain complete references', async () => {
  for (const page of pages.filter((page) => page.source)) {
    assert.ok(page.summary, page.slug);
    const body = await read('content/' + page.summary);
    assert.ok(body.split(/\s+/).length <= 250, `${page.slug}: summary exceeds 250 words`);
    for (const paragraph of body.split(/\n\n/)) {
      if (/^(#|\d+\.|\||```)/.test(paragraph)) continue;
      assert.ok(paragraph.split(/\s+/).length <= 65, `${page.slug}: long paragraph`);
    }
    assert.doesNotMatch(body, /^\s*[-*+] /m);
    const generated = await read(
      `src/app/core/content/generated/${page.slug.replaceAll('/', '--')}.ts`,
    );
    const article = JSON.parse(generated.split('export default ')[1].trim().replace(/;$/, ''));
    assert.ok(article.reference.html.length > 0);
    const ids = [...article.headings, ...article.reference.headings].map((h) => h.id);
    assert.equal(new Set(ids).size, ids.length, page.slug);
    assert.ok(article.headings.every((h) => h.id.startsWith('quick-')));
    assert.equal(
      await read('public/generated/sources/' + page.source),
      await read('content/imported/' + path.basename(page.source)),
    );
  }
});
test('Markdown excludes raw executable HTML and unsafe links', () => {
  const result = renderMarkdown(
    '# Test\n\n<script>alert(1)</script>\n\n[bad](javascript:alert)\n\n[relative](//evil.test)\n\n[good](https://example.com)\n\n<img src=x onerror=alert(1)>',
  );
  assert.doesNotMatch(result.html, /<script|javascript:|onerror|href="\/\//);
  assert.match(result.html, /href="https:\/\/example.com"/);
});
test('code remains text and has accessible clipboard controls', () => {
  const result = renderMarkdown('```html\n<script>alert(1)</script>\n```');
  assert.match(result.html, /&lt;script&gt;/);
  assert.match(result.html, /aria-label="Copy code"/);
  assert.match(result.html, /tabindex="0"/);
});
test('search finds product terms and includes navigation metadata', async () => {
  const index = MiniSearch.loadJSON(await read('public/generated/search.json'), {
    fields: ['title', 'description', 'text'],
    storeFields: ['slug', 'title', 'description', 'group'],
  });
  for (const query of ['database', 'Lumi', 'publish', 'budget']) {
    const results = index.search(query, { prefix: true });
    assert.ok(results.length, query);
    assert.ok(results.every((result) => pages.some((page) => page.slug === result.slug)));
  }
});
test('downloadable OpenAPI preserves security and real endpoint definitions', async () => {
  for (const api of ['consumer', 'studio', 'platform']) {
    const spec = JSON.parse(await read(`content/contracts/${api}.json`));
    assert.ok(spec.openapi.startsWith('3.'));
    assert.ok(Object.keys(spec.paths).length > 0);
    assert.ok(Object.keys(spec.components.securitySchemes).length > 0);
    assert.deepEqual(JSON.parse(await read(`public/generated/contracts/${api}.json`)), spec);
  }
});
test('embedded Swagger disables execution, authorization persistence and remote validation', async () => {
  const source = await read('public/api-reference/reference.js');
  assert.match(source, /supportedSubmitMethods: \[\]/);
  assert.match(source, /persistAuthorization: false/);
  assert.match(source, /validatorUrl: null/);
  assert.match(source, /delete spec.components.securitySchemes/);
});
test('Studio destinations accept HTTPS and loopback only, never embedded secrets', () => {
  for (const url of [
    'http://localhost:4200',
    'http://127.0.0.1:34200',
    'http://[::1]:35200',
    'https://studio.example.com/',
  ])
    assert.ok(safeStudioUrl(url), url);
  for (const url of [
    'javascript:alert(1)',
    '//evil.test',
    'http://public.example.com',
    'https://user:password@example.com',
    'https://example.com/?token=test',
    'https://example.com/auth',
    'https://example.com/#token',
  ])
    assert.equal(safeStudioUrl(url), null, url);
});
test('project has no workspace or sibling build dependency', async () => {
  const pkg = JSON.parse(await read('package.json'));
  assert.equal(pkg.workspaces, undefined);
  for (const version of Object.values({ ...pkg.dependencies, ...pkg.devDependencies }))
    assert.doesNotMatch(version, /^(?:file:|link:|workspace:)/);
  for (const script of ['start', 'build', 'test'])
    assert.doesNotMatch(pkg.scripts[script], /sync|\.\.\//);
  const angular = JSON.parse(await read('angular.json'));
  assert.equal(angular.projects['runloom-docs'].architect.build.options.outputMode, 'static');
});
test('components use separate templates/styles and feature hierarchy', async () => {
  const files = await fs.readdir(path.join(root, 'src/app'), { recursive: true });
  for (const file of files.filter((file) => file.endsWith('.component.ts'))) {
    const source = await read('src/app/' + file);
    assert.doesNotMatch(source, /\btemplate\s*:|\bstyles\s*:/);
    assert.match(source, /templateUrl:/);
    assert.match(source, /styleUrl:/);
    assert.match(file, /^(features|layout|shared)\//);
  }
});
