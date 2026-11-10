# Runloom management application

Independent Angular application for organizations, teams, members, schedules, service accounts, workflow secrets, prompt library, workflow run history, LLM analytics and budgets. This directory can become a separate repository without the host or workflows source. It owns its package/lockfile, dependencies, compiler configuration, tests, Dockerfile and output.

## Source hierarchy

```text
src/
  main.ts, index.html
  app/
    bootstrap.ts
    federation/entry.ts
    core/api/dashboard-api.service.ts
    layout/management-shell/
    features/
      analytics/
      budgets/
      prompts/models/
      runs/models/, utils/
      schedules/models/, utils/
      secrets/
      service-accounts/
    shared/styles/, utils/
tests/
```

Each feature contains its own `.component.ts`, `.component.html` and `.component.scss`. Prompt/run/schedule contracts and run/schedule helpers stay with their feature. Shared styles and generic sorting/date/error helpers are separate from feature models. The management shell owns section composition, membership administration and child unsaved-change guards; the core API service owns team-scoped HTTP access.

`app/federation/entry.ts` is the stable `./Application` export. The host still owns navigation. This reorganization preserves standalone components and current behavior: it does not add NgModules, feature lazy loading or a competing remote router. Keep core/shared independent of feature and shell implementations, and keep features independent of shells. `npm test` enforces these source boundaries and local import resolution. Each project retains its own root-level `tests/` directory.

## Prompt and run dashboards

**Prompt library** provides team-scoped search, status filters, sorting, paged results, template/typed-input editing, rendering previews and immutable version history. Viewers can browse; editors can save or publish. Revision conflicts leave the edited content intact. Prompt editors participate in host navigation, workspace switching, logout and browser-close guards. Real model tests remain in workflow nodes so their project budget applies.

**Run history** is a read-only dashboard separate from the builder's live-test panel. It shows the latest 200 persisted team runs, with workflow/status/test/date filters, sorting and 20-row pages. Summary counts describe the filtered recent window, not all-time totals. Details show stored inputs/output, execution state (including pause reasons) and up to 1,000 audit events. Refresh updates stored snapshots; it does not query live Temporal state or operate the run. Use **Cost analytics** for organization/team spend reporting. The API enforces team visibility; switching teams clears prior results.

All three Studio apps consume the compiled platform 1.3.0 package, including the additive `prompts` / `runs` / `schedules` navigation contract and shared control styling. No application imports sibling source.

Schedules are owned by Management: team-scoped listing, search/status filters, name sorting, creation, pause/resume, run-now and deletion. Forms retain unsaved-change protection across app/team navigation and sign-out. Recent-run links open Run history. Schedule mutations use the existing Studio API; viewers remain read-only, and child workflows cannot be scheduled.

## Commands

Use Node.js 26.13.0 (`nvm install` and `nvm use`). From this directory:

```sh
npm ci
npm test
npm run build
npm start
docker build -t runloom-management-ui:local .
```

Development defaults to port 4202. Production output is `dist/browser/`, including `remoteEntry.js`. No command builds a sibling or the platform library. Tests enforce independent source/configuration boundaries, external HTML/SCSS resources and license-extractor compatibility.

## Runtime integration and releases

The authenticated host loads the exposed `./Application` at runtime. A direct visit shows an Angular availability screen; authenticated management screens use the host's session, workspace context and guarded navigation. All APIs remain relative to the host origin. The reverse proxy maps `/remotes/management/` to this independently deployed asset server. The container listens on 8080, has `/healthz`, and returns 404 for missing chunks.

`@runloom/studio-platform` is a versioned compiled package installed from the release artifact inside this project's `vendor/` directory. Updating its independent source project does not update this app. Explicitly install a new artifact and commit its package/lockfile; a registry dependency may replace it later. Never use source aliases, sibling filesystem dependencies, npm linking or automatic library builds.

Runtime Angular/RxJS/platform singletons and the numeric application contract must stay compatible with the host. Independent compilation is not permission to break runtime APIs. Only the host initializes authentication; Keycloak manages identity cookies, and tokens stay in memory. One-time-secret and unsaved-form warnings still pass through the runtime bridge.

Build, push and update this deployment independently. Retain old hashed assets for active browser sessions and run cross-app login/navigation tests before promotion. No build or test publishes or deploys anything automatically.
