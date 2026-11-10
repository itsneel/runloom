# Runloom product website

A standalone Angular product website explaining Runloom, with an interactive workflow illustration, readable example JSON, feature explanations, developer API overview, and FAQs. This is not the workflow studio in `studio/`.

The website has its own package manifest, lockfile, Angular configuration, source, tests, and output directory. It does not import code or dependencies from `studio/` or `server/`, call the product API, require login, or use a database. No Dockerfile, Compose service, or Helm deployment is included for this website. Existing product infrastructure is unchanged.

## Requirements

Use Node.js 26.13.0 with npm 11.20.0 (`nvm install` and `nvm use`). No globally installed Angular CLI is needed. Commands below run inside `Website/` unless stated otherwise.

## Local development

```bash
cd Website
npm ci
npm start
```

Open http://127.0.0.1:4300. Stop the development server with Ctrl+C. To use another port, run `npm start -- --port 4400`.

From the repository root, the equivalent commands are `make setup-website` followed by `make run-website`. These do not start or install the product's other components.

## Production build

```bash
npm run build
```

The static browser files are written to `dist/runloom-website/browser/`. This includes `index.html`, hashed JavaScript/CSS, the favicon, and the project `LICENSE`. Angular writes `3rdpartylicenses.txt` under `dist/runloom-website/`; include that file in the distributed site's notice bundle as well. Preserve all applicable dependency license and NOTICE files when distributing.

The website uses Angular's `@angular/build:application` builder with esbuild. It produces a client-rendered static site, not a Node application server. The studio's separate Webpack configuration remains unchanged. See the [Angular build documentation](https://angular.dev/tools/cli/build).

From the repository root, use `make build-website`. The existing product `make setup`, `make test`, image builds, Compose configuration, and Helm chart deliberately do not include the website.

## Preview the build without containers

```bash
npm run preview
```

Open http://127.0.0.1:4301. Build first; preview never implicitly compiles the app. The equivalent root command is `make preview-website`. To override the port, use `npm run preview -- --port 4401`.

The small Node preview server is loopback-only, serves existing build files, rejects writes and path traversal, and is intended only for local testing. Do not use it as a production server.

## Static hosting

1. Run `npm ci` and `npm run build` in `Website/`.
2. Upload the contents of `dist/runloom-website/browser/` to your static web host, along with the generated third-party license notices. No Go server, identity service, database, containers, or server-side JavaScript are needed to serve it.
3. Configure the host to serve `index.html` at the website root over HTTPS. Navigation uses in-page anchors, so client-side routing rewrites are not needed. Do not cache `index.html` indefinitely; hashed bundles may be cached long-term.
4. For hosting under a subdirectory, build with `npm run build -- --base-href /runloom/` and serve the output at that exact path. The included preview server serves the default root build only.
5. Before publishing, review metadata in `src/index.html`, configure real links as described below, and verify the site at its final origin. This repository does not deploy or publish the site automatically.

The site is client rendered and needs JavaScript for the full experience. Basic product text is provided in a `noscript` fallback; prerendering/SSR is not configured. Metadata has no invented canonical URL or social image.

## Content and links

Edit `src/app/content.ts` for demo scenarios, FAQs, and `siteConfig`. The navigation's **Log in** button and **Open workflow studio** link open the separately hosted Studio, which handles authentication. The website never collects credentials or tokens. Locally, `studioUrl` defaults to `http://localhost:24200/` for the `make fresh-up` Docker Compose stack. Before publishing, change it to your public Studio HTTP(S) URL and rebuild; use an empty string to hide both Studio links when no instance is available. Set `repositoryUrl` to the real source repository URL; it defaults to an empty string, hiding the repository link.

From the repository root, `make k8s-open` builds this independent website and opens a local preview linked to the installed Kubernetes Studio. It prints connection details and local admin information before opening the browser; account setup and Kubernetes orchestration live outside this website. Keep its terminal open. The preview server accepts `RUNLOOM_WEBSITE_STUDIO_URL` as an HTTP loopback origin and inserts a `runloom-studio-url` metadata tag in the served HTML without modifying source or build files. Both Studio links use that metadata when present, otherwise the normal `siteConfig` default. No credentials or tokens enter the website. See [local Kubernetes setup](../docs/deployment.md#development-cluster).

Product copy is in `src/app/app.html`; styling is in `src/app/app.scss` and `src/styles.scss`. Icons and the workflow illustration are original code-native SVG/CSS; no external images, fonts, analytics, or cookies are loaded. Only copying example JSON uses the browser clipboard, after a button click; a readable fallback message appears if permission is denied.

The demo is explicitly illustrative. It runs a short local animation, waits for approval or a simulated callback, then completes. It does not execute workflows, send email, provision accounts, or call an AI model. Example JSON is a draft; using it in the actual product requires validation, publishing, appropriate team access, and any necessary model configuration. Keep product claims aligned with the implemented features and operator responsibilities.

## Tests

```bash
npm test
npm run build
```

These run dependency-free content/configuration checks and Angular's strict compilation with bundle budgets. From the root, use `make test-website`.

For desktop and mobile browser tests:

```bash
npx playwright install chromium
npm run test:e2e
```

Playwright builds and serves the production output on loopback port 4302, verifies navigation, layout overflow, approval/callback gates, scenario switching, JSON copying, FAQ behavior, and the absence of external network requests. It also checks the preview server's read-only boundary and runs axe accessibility checks on the page and expanded JSON view. Automated checks are not a complete accessibility audit. It stops its test server afterwards. If you already have Chrome installed, set `RUNLOOM_CHROME` to its executable path instead of installing Chromium. Browser tests are separate from `make test-website` so ordinary builds need no browser installation.

Original website code is MIT licensed under [LICENSE](LICENSE). Angular/CLI and esbuild are MIT licensed; RxJS and Playwright are Apache-2.0 licensed; TypeScript is Apache-2.0 licensed and tslib is 0BSD. The development-only axe accessibility tooling declares MPL-2.0. Dependency-provided licenses and notices remain authoritative. See the repository's third-party notices for the broader product, whose database/server dependencies are not website runtime dependencies.
