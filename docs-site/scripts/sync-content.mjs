// Explicit release/import step only. Ordinary builds never read sibling projects.
import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { createHash } from 'node:crypto';
import { Marked } from 'marked';

const project = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const root = path.resolve(project, '..');
const check = process.argv.includes('--check');
const pages = JSON.parse(await fs.readFile(path.join(project, 'content/pages.json'), 'utf8'));
const manifest = {};
async function copy(source, destination) {
  const data = await fs.readFile(path.join(root, source));
  manifest[source] = { destination, sha256: createHash('sha256').update(data).digest('hex') };
  const target = path.join(project, destination);
  if (check) {
    if (!(await fs.readFile(target)).equals(data)) throw new Error(`Stale snapshot: ${source}`);
  } else {
    await fs.mkdir(path.dirname(target), { recursive: true });
    await fs.writeFile(target, data);
  }
  return data.toString('utf8');
}
const markdown = new Marked();
const sources = new Set(pages.filter((p) => p.source).map((p) => p.source));
const attachments = new Set();
for (const source of sources) {
  const body = await copy(source, `content/imported/${path.basename(source)}`);
  markdown.walkTokens(markdown.lexer(body), (token) => {
    if (token.type !== 'link' || /^(?:[a-z]+:|#|\/)/i.test(token.href)) return;
    const target = path.posix.normalize(
      path.posix.join(path.posix.dirname(source), token.href.split('#')[0]),
    );
    if (!target.startsWith('../') && !sources.has(target)) attachments.add(target);
  });
}
for (const source of attachments) {
  // Only explicitly linked public source documents; never credential or runtime files.
  if (
    !/\.(md|json|ya?ml|go|ts|mjs|txt)$/.test(source) ||
    /(?:^|\/)(?:secrets|data|var)\//.test(source) ||
    source === 'docs/rebranding.md'
  )
    continue;
  try {
    if ((await fs.stat(path.join(root, source))).isFile())
      await copy(source, `content/sources/${source}`);
  } catch (error) {
    if (error.code !== 'ENOENT') throw error;
  }
}
await copy('server/workflows/internal/api/studio.openapi.json', 'content/contracts/studio.json');
await copy(
  'server/workflows/internal/api/consumer.openapi.json',
  'content/contracts/consumer.json',
);
await copy('server/auth/internal/api/platform.openapi.json', 'content/contracts/platform.json');
await copy('spec/workflow.schema.json', 'content/contracts/workflow.schema.json');
await copy('LICENSE', 'LICENSE');
const encoded = JSON.stringify(manifest, null, 2) + '\n';
const target = path.join(project, 'content/snapshot.json');
if (check) {
  if ((await fs.readFile(target, 'utf8')) !== encoded)
    throw new Error('Snapshot manifest is stale');
} else await fs.writeFile(target, encoded);
console.log(
  `${check ? 'Verified' : 'Imported'} ${Object.keys(manifest).length} public source snapshots.`,
);
