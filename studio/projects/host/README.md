# Runloom authentication host

This directory is a complete independent Angular application, ready to become a separate repository. There are no imports or build steps that access a sibling project or parent compiler configuration.

## Commands

Use Node.js 26.13.0 (`nvm install` and `nvm use`). Run these commands from this directory:

```sh
npm ci
npm test
npm run build
npm start
docker build -t runloom-ui:local .
```

Production output is `dist/browser/`; the development server defaults to port 4200. Dependencies and caches are local to this project. `npm test` verifies source boundaries, separate HTML/SCSS files and the license adapter. Building never starts or builds the feature applications or the platform library.

## Runtime configuration

The host owns login/logout, navigation and the browser session. `public/federation.manifest.json` identifies trusted same-origin remote entries. Their source code, package manifests and build artifacts are not needed to build or start this host. Unavailable remotes show an error with retry after sign-in.

The sidebar keeps **Workflows**, **Projects** and permanently expanded Management navigation in the same order on every page. Projects opens `/workflows/projects`; folders and workflow browsing belong in the main content, not the global sidebar. Navigation scrolls independently of the anchored appearance/account footer, and route changes preserve that scroll position. Organization and team selectors overlay the menu from the compact workspace summary. Desktop collapse retains an icon rail with hover and keyboard-focus labels; mobile uses a dismissible drawer. Unsaved-change confirmation applies to both workflow and management navigation.

The workflow remote accepts an optional `projectsView` boolean input (default `false`) alongside its navigation layout inputs. Deploy a compatible workflow remote before a host that supplies this input. The platform v1 `workflowNavigation` slot remains for compatibility, but the host no longer renders it. Each application still builds independently.

Run `node tests/sidebar.browser.mjs` against the three development apps to verify fixed menu positions, Projects actions, dirty-draft guards, direct links, collapsed tooltips, mobile layout and scroll preservation with synthetic APIs.

## Source hierarchy

```text
src/
  main.ts, index.html
  app/
    bootstrap.ts, app.config.ts
    routing/app.routes.ts
    core/federation/remote-loader.service.ts
    layout/
      shell/shell.component.{ts,html,scss}
      remote-outlet/remote-outlet.component.{ts,html,scss}
tests/
```

The shell owns session-aware navigation. Angular routes in `routing/app.routes.ts` mount `layout/workspace-page`, which selects the independent remote. The loader owns trusted remote resolution; the outlet owns mounting and cleanup. Routing remains a host responsibility, with no source/build dependency between applications and no changes to their versioned runtime contract.

## Browser URLs

The workflow application uses `/workflows`. Management uses `/management`, which redirects to `/management/teams`. Its separate tab URLs are `/management/teams`, `/management/organization`, `/management/service-accounts`, `/management/secrets`, `/management/analytics`, `/management/budgets`, `/management/prompts`, `/management/runs`, and `/management/schedules`. `/` defaults to workflows; unknown management paths fall back to teams and other unknown paths to workflows. These are browser routes, not API endpoints or the `/remotes/...` asset paths.

Refresh, direct links and browser Back/Forward restore the selected application/tab. Organization/team selection still uses the authenticated workspace preference; URLs do not grant access or encode credentials. Existing unsaved-change dialogs guard tab/app changes and browser history navigation. Management remains mounted across its tab changes. Initial routing waits for authentication so an OIDC callback is processed before URL redirects run; sign-in retains the requested tab.

Local development, Compose Nginx and the Helm UI proxy must serve the host's `index.html` for these browser paths (their existing SPA fallback), while `/api`, `/auth` and `/remotes` keep their separate handling. Do not point management browser links at the standalone remote's asset server.

With all three development apps running, run `node tests/navigation.browser.mjs` from this project to check all tab URLs, reloads, history navigation and unsaved-change cancellation using synthetic APIs. Supply `RUNLOOM_UI_URL`, `RUNLOOM_PLAYWRIGHT_MODULE` and `RUNLOOM_CHROME` for your test environment. This test does not create accounts or run workflows.

Keep only entry files at the `src/` root. Co-locate each standalone component's separate TypeScript, HTML and SCSS. `npm test` rejects misplaced components, missing local imports and core dependencies on layout/features. Authentication and shared UI continue to come from the independently packaged platform library; do not copy that source into this app.

For development, `proxy.conf.cjs` forwards remote paths to ports 4201 and 4202. Override `RUNLOOM_WORKFLOWS_PORT`, `RUNLOOM_MANAGEMENT_PORT`, `RUNLOOM_API_PROXY` and `RUNLOOM_AUTH_PROXY` as needed. Production `nginx.conf` uses runtime DNS for the API, identity and remote asset servers; adapt those service endpoints when deploying outside the Runloom Compose stack. This changes deployment wiring, not build dependencies.

## Shared library releases

`@runloom/studio-platform` is an independently versioned compiled npm package. The pinned `.tgz` under `vendor/` and its package-lock integrity allow installation when this application is copied out of the enclosing repository. Do not replace it with a source alias, sibling `file:` dependency or npm link. A library update is an explicit dependency update, never an app prebuild step. A registry release can replace the vendored package when a registry is configured.

Angular, RxJS and the platform package are shared only at runtime as strict-version singletons. Independent builds still require compatible runtime contracts. Keep feature contracts backward compatible or coordinate breaking releases. The host initializes authentication once; tokens remain in memory, and Keycloak manages its cookies.

The workflow and management applications are separate deployments. Their direct URL shows an availability screen; authenticated features are mounted by this host. This is runtime Module Federation, not Angular feature modules in one build.

## Release

Run the checks above, build and push this image alone, then update only the host deployment's image. No publication or deployment runs implicitly. Keep prior hashed assets available for active browser sessions. Run end-to-end federation/login tests against the intended remote versions before promoting a release.
