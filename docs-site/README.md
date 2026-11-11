# Runloom documentation website

An independent Angular application with prerendered articles, light/dark themes, local full-text search and separate Studio, Consumer and Platform OpenAPI references. No authentication, database, sibling build, npm workspace, Docker or Helm deployment is required. The site is distinct from the marketing application in `Website/` and from Studio.

## Develop and build

Use the Node.js version range declared in `package.json`. Run inside this directory:

```sh
npm ci
npm start
```

Open http://127.0.0.1:4400. Content is compiled when starting; after changing Markdown, restart the development server or run `npm run content` in a second terminal. Angular source changes update through the development server.

```sh
npm test
npm run build
npm run preview
```

Build output is `dist/runloom-docs/browser/`. Serve that directory, including the generated route directories, assets, LICENSE and extracted third-party notices. No Node server is needed in production. The default preview binds to loopback port 4400; stop a development server on that port first. Choose another preview port with the native serve CLI if necessary.

The native aliases are `make setup-docs`, `make run-docs`, `make build-docs`, `make preview-docs`, `make test-docs` and `make test-docs-browser`. These commands do not change the product installation.

## Compose and Helm

Documentation uses a separate browser origin, never a Studio subpath. Studio's **Documentation** menu opens it in a new tab without navigating away from an unsaved workflow.

1. `make docker-build-docs` builds this directory alone. Its pinned multi-stage Dockerfile ships only static output and Nginx, running non-root with a read-only filesystem and `/tmp` scratch space. `make docker-up-docs` starts only documentation on http://localhost:4400; stop any native preview using that port first.
2. Normal `make local-services-up SERVICES_DATABASE=mongodb` includes docs at http://localhost:34400 with Studio at http://localhost:34200. PostgreSQL uses docs http://localhost:35400 and Studio http://localhost:35200. Both selections mount public JSON configuration for reciprocal links; no database or credentials are passed to docs.
3. Raw Compose uses `DOCS_PORT` (default 4400), `DOCS_IMAGE`, `RUNLOOM_DOCUMENTATION_CONFIG_PATH` and `RUNLOOM_DOCS_STUDIO_CONFIG_PATH`. The first JSON file contains `{"url":"https://docs.example.com"}` and mounts into Studio as `/documentation.json`; the second contains `{"studioUrl":"https://studio.example.com"}` and mounts into docs as `/studio.json`. Set the URL to the address reachable by the browser, not a container DNS name. Restart the corresponding container after replacing a mounted file.
4. Helm enables the independent docs Deployment and ClusterIP Service by default. Set `docs.image.repository`, `docs.image.tag` and `docs.publicURL`. Enable `docs.ingress.enabled` with a class and TLS configuration to serve the origin's root. Its hostname is derived from `docs.publicURL`, independently of Studio ingress. Set `docs.enabled=false` and an external `docs.publicURL` when hosting static docs elsewhere.
5. `make k8s-upgrade` builds and loads the docs image alongside the independent Studio images. `make k8s-docs` forwards the installed docs service to its configured localhost port (default 4400); keep that terminal open. `make k8s-open` forwards both Studio and docs. For a different local docs port, set `K8S_DOCS_PORT` during the upgrade so the chart, sidebar and forwarding agree. Existing data is preserved.

The docs site reads public `studio.json` after hydration, falling back to `site.config.json` if unavailable. A saved user preference takes precedence. Neither file contains authentication tokens. The independent marketing app in `Website/` is not part of these container builds.

## Content ownership and independence

1. `content/pages.json` defines stable routes, navigation groups and descriptions. Introductory tutorials are authored in `content/*.md`.
2. Detailed guides remain canonical in the repository's `docs/`, as required by AGENTS.md. `content/imported/` is a committed release snapshot, not a second editing location. API contracts in `content/contracts/` are explicit snapshots from the API owners. `content/sources/` contains directly linked public reference files, not runtime data or credentials.
   Each imported article has an editorial introduction in `content/summaries/`, selected by its `summary` field. Keep it under 250 words, use short paragraphs and actionable steps, and keep critical safety warnings visible. Full source content remains in the collapsed **Full technical reference** section. Summaries explain existing behavior; they do not define a second contract. Review them when syncing changed sources.
3. Run `npm run sync` or root `make sync-docs` explicitly when updating those guides or contracts. It records SHA-256 provenance in `content/snapshot.json`. `npm run sync:check` or `make check-docs-sync` compares snapshots with the repository. These are the only commands that read siblings and are intentionally not part of installation, build or start.
4. A separate checkout of this directory builds from its committed snapshots alone. Future independent repositories can exchange versioned content artifacts rather than introduce source imports or backend builds.
5. `npm run content` uses Marked, github-slugger and sanitize-html to generate safe article HTML, heading metadata, lazy route modules and a MiniSearch index. Raw HTML is stripped; executable links are rejected. Generated outputs are ignored. New tutorials must use site-absolute internal links; the build rejects nonexistent page routes. Historic references to repository directories or unavailable source files are plain text and listed in `public/generated/link-report.json`.
6. Keep component TypeScript, HTML and SCSS separate. Follow `core`, `layout`, `features`, `shared` and `routing` boundaries, as in the other independent Angular apps. Tests live in `tests/`.

The full reference preserves source wording, dated verification and limitations. Its existing heading links automatically expand the reference; quick-guide headings use a separate `quick-` prefix. Search indexes both views. Importing a guide is not a new live verification of every feature; check version-specific behavior against the installed release.

## API references and Studio links

The site packages real OpenAPI JSON and locally served Swagger assets. Embedded Swagger is read-only, has no authorization control, does not persist credentials and does not call a remote validator. The original downloadable contracts retain their security definitions. Loading an API reference does not authenticate or execute workflow operations.

**Open installed Swagger** opens `/api/docs/<interface>/` on the selected Studio origin. Authentication and actual requests happen there and can change real data. No credentials are copied between the two sites.

Set the build default in `site.config.json`. The header's **Studio connection** control offers Kind (4200), Compose MongoDB (34200) and Compose PostgreSQL (35200) presets or a custom HTTPS origin. This preference and the theme are stored locally in the browser; storage failures do not prevent reading documentation. URLs reject credentials, query strings and paths. HTTP is allowed only for loopback hosts. Verify your installation's printed URLs if ports were overridden.

## Static hosting

Deploy the contents of `dist/runloom-docs/browser/` to your chosen static host. Configure clean URLs so `/guides/first-workflow` serves `/guides/first-workflow/index.html`; redirecting to a trailing slash is also supported. Return HTTP 404 for unknown routes instead of serving the home page with 200. The default build targets an origin root, not a subdirectory. Configure HTTPS, compression and caching at the host; cache hashed JS/CSS immutably and revalidate HTML and unversioned generated assets on release.

No external fonts, CDN scripts, analytics or external search service are used. Search runs entirely in the browser. The reference iframe deliberately uses Swagger's own light theme for readable contract schemas in either surrounding site theme.

## Verification

```sh
npm test
npm run format:check
npm run test:e2e
```

Browser tests build the production site and start a loopback-only static server on 4402. Install Playwright Chromium with `npx playwright install chromium`, or set `RUNLOOM_CHROME` to your installed Chrome executable. Tests cover desktop/mobile navigation, deep-link refreshes, theme persistence, keyboard search, clipboard controls, Studio destination validation, local-only API references, static article HTML without JavaScript and automated accessibility checks. Automated accessibility checks do not replace a manual audit.

To run the same read-only browser checks against a running docs deployment, use `RUNLOOM_DOCS_TEST_URL=http://127.0.0.1:4400 npx --no-install playwright test`. This skips starting the preview server. The default installed-Swagger assertion assumes Studio at http://localhost:4200.

Use `npm run format` for this project's native Prettier formatter. There is no global npm package or commit hook. `npm audit` checks this project's dependency graph without changing other projects.

Original code and documentation use the included Apache 2.0 LICENSE. Preserve generated `3rdpartylicenses.txt` and `generated/swagger/` upstream notices when distributing the static output.
