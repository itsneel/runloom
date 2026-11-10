import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import MiniSearch from 'minisearch';
import { renderMarkdown } from './markdown.mjs';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const read = (file) => fs.readFile(path.join(root, file), 'utf8');
const pages = JSON.parse(await read('content/pages.json'));
const snapshot = JSON.parse(await read('content/snapshot.json'));
const generated = path.join(root, 'src/app/core/content/generated');
const assets = path.join(root, 'public/generated');
// Both ignored directories are exclusively owned by this content build.
await fs.rm(generated, { recursive: true, force: true });
await fs.rm(assets, { recursive: true, force: true });
await fs.mkdir(generated, { recursive: true });
await fs.mkdir(assets, { recursive: true });
const sourceRoutes = new Map(pages.filter((p) => p.source).map((p) => [p.source, '/' + p.slug]));
const results = [];
const routes = [];
const detached = new Set();
for (const page of pages) {
  if (!/^[a-z0-9-]+\/[a-z0-9-]+$/.test(page.slug)) throw new Error(`Invalid slug ${page.slug}`);
  const file = page.source ? `imported/${path.basename(page.source)}` : page.file;
  const result = renderMarkdown(await read(`content/${file}`), (href) => {
    if (/^(https?:\/\/|#|\/[^/])/.test(href)) return href;
    if (!page.source) throw new Error(`Use absolute documentation links in ${file}: ${href}`);
    const [relative, anchor] = href.split('#');
    const target = path.posix.normalize(path.posix.join(path.posix.dirname(page.source), relative));
    if (sourceRoutes.has(target)) return sourceRoutes.get(target) + (anchor ? '#' + anchor : '');
    const entry = snapshot[target];
    if (entry) return '/generated/sources/' + target;
    // Historic source references that no longer exist are plain text, never dead links.
    detached.add(`${page.source}: ${href}`);
    return undefined;
  });
  let overview;
  if (page.summary) {
    if (!page.source) throw new Error(`A summary requires a source reference: ${page.slug}`);
    overview = renderMarkdown(
      await read(`content/${page.summary}`),
      (href) => {
        if (!/^(https?:\/\/|#|\/[^/])/.test(href))
          throw new Error(`Use site-absolute links in ${page.summary}: ${href}`);
        return href;
      },
      'quick-',
    );
  }
  const key = page.slug.replaceAll('/', '--');
  const { file: ignored, summary: ignoredSummary, ...metadata } = page;
  routes.push(
    `{ path: ${JSON.stringify(page.slug)}, title: ${JSON.stringify(page.title + ' · Runloom docs')}, data: { page: ${JSON.stringify(metadata)} }, resolve: { article: () => import('./${key}').then(m => m.default) }, loadComponent: () => import('../../../features/article/article.component').then(m => m.ArticleComponent) }`,
  );
  results.push({ ...metadata, ...result, overview });
}
if (new Set(pages.map((p) => p.slug)).size !== pages.length)
  throw new Error('Duplicate documentation slug');
for (const page of results) {
  for (const content of [page, page.overview].filter(Boolean)) {
    for (const href of content.links) {
      if (href.startsWith('http') || href.startsWith('/generated/')) continue;
      const [pathname, fragment] = href.split('#');
      const target = pathname ? results.find((p) => '/' + p.slug === pathname) : page;
      if (pathname === '/') continue;
      if (!target) throw new Error(`Broken documentation link in ${page.slug}: ${href}`);
      // Some old guides link H1 headings; those are displayed in the page header instead.
      if (
        fragment &&
        ![...target.headings, ...(target.overview?.headings || [])].some((h) => h.id === fragment)
      ) {
        content.html = content.html.replaceAll(
          `href="${href}"`,
          `href="${pathname || '/' + page.slug}"`,
        );
      }
    }
  }
  const key = page.slug.replaceAll('/', '--');
  await fs.writeFile(
    path.join(generated, `${key}.ts`),
    `// Generated from packaged Markdown.\nexport default ${JSON.stringify(page.overview ? { html: page.overview.html, headings: page.overview.headings, reference: { html: page.html, headings: page.headings } } : { html: page.html, headings: page.headings })};\n`,
  );
}
await fs.writeFile(
  path.join(generated, 'routes.ts'),
  `import { Routes } from '@angular/router';\nexport const articleRoutes: Routes = [${routes.join(',\n')}];\n`,
);
await fs.writeFile(
  path.join(generated, 'navigation.ts'),
  `import { PageMeta } from '../content.models';\nexport const pages: PageMeta[] = ${JSON.stringify(pages.map(({ file, summary, ...p }) => p))};\n`,
);
const index = new MiniSearch({
  fields: ['title', 'description', 'text'],
  storeFields: ['slug', 'title', 'description', 'group'],
});
index.addAll(results.map((p, id) => ({ ...p, text: (p.overview?.text || '') + ' ' + p.text, id })));
await fs.writeFile(path.join(assets, 'search.json'), JSON.stringify(index));
await fs.writeFile(
  path.join(assets, 'link-report.json'),
  JSON.stringify({ detachedSourceReferences: [...detached] }, null, 2),
);
await fs.cp(path.join(root, 'content/contracts'), path.join(assets, 'contracts'), {
  recursive: true,
});
for (const [source, entry] of Object.entries(snapshot)) {
  const target = path.join(assets, 'sources', source);
  await fs.mkdir(path.dirname(target), { recursive: true });
  await fs.copyFile(path.join(root, entry.destination), target);
}
const swagger = path.join(assets, 'swagger');
await fs.mkdir(swagger, { recursive: true });
for (const name of [
  'swagger-ui-bundle.js',
  'swagger-ui-bundle.js.LICENSE.txt',
  'swagger-ui.css',
  'LICENSE',
  'NOTICE',
]) {
  await fs.copyFile(
    path.join(root, 'node_modules/swagger-ui-dist', name),
    path.join(swagger, name),
  );
}
console.log(
  `Built ${pages.length} articles, search index and three OpenAPI references. ${detached.size} unavailable historic source links rendered as text; see generated/link-report.json.`,
);
