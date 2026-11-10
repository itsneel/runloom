# Runloom workflows application

The Workflows landing page is a project-scoped dashboard, not an automatically opened editor. It lists names, IDs, published versions (and published main/child kind), and whether a saved draft exists. Search by name or ID, or filter for published workflows and saved drafts. Those states can overlap. Selecting a project only changes the list; choose **Details** to load a workflow into the read-only builder, then **Edit workflow** to reserve editing. **New workflow** opens a new draft. **Back to workflows** releases the edit lease and asks before discarding unsaved changes. Refreshing the Workflows page returns to the dashboard. The list uses summary metadata without fetching individual workflow definitions or run payloads.

Dashboard regression checks live in `tests/workflow-dashboard.test.mjs` and `tests/dashboard.browser.mjs`. `npm test` runs the filtering checks. Run the browser script against a federated development host using `RUNLOOM_UI_URL`, `RUNLOOM_PLAYWRIGHT_MODULE` and optionally `RUNLOOM_CHROME`; the repository's `make test-ui-browser` includes it. With `RUNLOOM_DASHBOARD_LIVE_LOGIN=1`, it can also verify a local production build using the public local admin defaults, then mock workflow APIs for isolated interaction checks. It never saves definitions or starts runs.

Independent Angular application for workflow authoring, versions, schedules and run inspection. This directory is a complete repository boundary: it owns its dependencies, lockfile, tests, Angular/TypeScript/Webpack configuration, Dockerfile and output directory. It imports no host or management source.

## Commands

Use Node.js 26.13.0 (`nvm install` and `nvm use`). From this directory:

```sh
npm ci
npm test
npm run build
npm start
docker build -t runloom-workflows-ui:local .
```

The development server defaults to port 4201. Output is `dist/browser/`, including `remoteEntry.js`. Neither the host, management project nor platform library source is needed for any command. `npm test` checks independent source/configuration boundaries, external HTML/SCSS files, license extraction compatibility and nested graph helpers.

The canvas supports free node positioning, background panning, pointer-centred zoom, fit to view and automatic arrangement. Positions save as optional `layout.positions` authoring metadata; moving a card does not change execution order. Nested Parallel paths and Decision routes, branch management, explicit execution-path moves and subtree duplication use the structured JSON contract, not arbitrary edges or cycles. Graph logic, template and styles live separately in `src/app/features/builder/components/canvas/workflow-graph.component.ts`, `src/app/features/builder/components/canvas/workflow-graph.component.html` and `src/app/features/builder/components/canvas/workflow-graph.component.scss`. Upgrade the API before deploying this UI so saved layout metadata is accepted. When working in the full Runloom checkout, `make test-ui-browser` includes graph interaction and save/reopen checks against a development host with mocked APIs.

## Source hierarchy

```text
src/
  main.ts, index.html
  app/
    bootstrap.ts
    federation/entry.ts
    core/
      api/workflow-api.service.ts
      models/workflow.types.ts, prompt.types.ts
    features/
      dashboard/
        workflow-dashboard.component.{ts,html,scss}
        workflow-dashboard.model.ts
      builder/
        workflow-builder.component.{ts,html,scss}
        model/workflow-tree.ts
        components/
          canvas/
          code-editor/
          prompt-editor/
      runs/components/run-costs/
    shared/components/variable-editor/
tests/
```

The builder feature owns authoring and its private editors/canvas. Domain contracts and API access live in core. The typed-variable editor is app-shared UI; the run-cost component belongs to run inspection. All component folders contain separate `.component.ts`, `.component.html` and `.component.scss` files. Tests remain in this project's `tests/`, not mixed into production source.

`app/federation/entry.ts` preserves the public `./Application` export. This is a standalone-component application, not an NgModule tree. The host owns navigation; no duplicate remote router or empty route/module files are introduced. Lazy-loading changes are separate from this source-layout change. Core/shared must not import features, and features must not import host/layout implementations. `npm test` checks these boundaries and resolves relative imports.

## Runtime integration and releases

The runtime host loads `./Application` from this application's Module Federation entry after authenticating. The direct URL shows an Angular availability screen; authenticated workflow features require the host-provided session/context. Requests use relative `/api` URLs on the host origin. Publish this asset server independently; the host maps `/remotes/workflows/` to it through a reverse proxy. Missing assets return 404, never SPA HTML. The container listens on 8080 and exposes `/healthz`.

The only first-party package dependency is the compiled `@runloom/studio-platform` release vendored inside this repository. Install an updated tarball explicitly and commit its manifest, lockfile and artifact. Never link another project's source or invoke the library build during application builds. The same package can be installed from an npm registry when available.

Angular/RxJS/platform singletons and the exported numeric contract version are runtime compatibility requirements, not build-time project references. Preserve compatible public contracts for independent deployment; breaking changes require coordination. Only the host initializes authentication. Tokens and one-time secrets are not persisted in browser storage.

Build and push this image alone to release it. Retain old hashed chunks for active sessions. Use end-to-end host/remote tests before promoting a new version; local tests do not establish integration compatibility. No build publishes packages or deploys automatically.
