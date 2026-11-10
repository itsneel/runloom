# Contributing to Runloom

Contributions to the Go runtime, Angular studio, JSON contract, examples, and documentation are welcome. Start with the [README](README.md) and [architecture](docs/architecture.md). Use an issue to discuss substantial changes to the JSON contract, public API, persistence, or execution behavior before implementing them.

## Local setup

1. Fork or clone the repository and create a branch named `feature/<short-description>`.
2. Install Go 1.25.4 or later, Node.js 26.13.0 (run `nvm install` and `nvm use` from the repository root), npm, and Make. Docker with Compose is needed for container builds and bundled databases. The Temporal CLI is needed for the native local server.
3. Run `make setup` from the repository root to refresh Go dependencies, build the services and install UI dependencies. `make setup-go` performs only the Go preparation and builds; `make vendor-go` refreshes the six standalone vendor trees without building. Refreshing requires network access for uncached dependencies and copies the repository’s local SDK sources into each consuming service. It preserves the service `go.mod` and `go.sum` files and does not add permanent sibling-source replacements. Review and commit the refreshed vendor files with any SDK or dependency changes. Ordinary builds and Docker builds use the prepared vendor trees without refreshing them.
4. Run `make run-temporal`, `make run-go-worker`, `make run-go-api`, and `make run-ui` in separate terminals from the repository root. Open [the studio](http://localhost:4200). Native development defaults to filesystem storage.
5. Alternatively, run `make docker-up-go` for the Compose stack using PostgreSQL, or `make docker-up-go-mongo` for application storage in MongoDB. Stop it with `make docker-down-go`.

Use synthetic inputs and local credentials. Never include secrets, real customer data, Temporal histories containing private data, or runtime database files in an issue or pull request. See [config.example.yaml](config.example.yaml), [mounted configuration](docs/deployment.md#mounted-application-configuration), and [the database guide](docs/database.md). Native commands read this YAML only when `RUNLOOM_CONFIG_FILE` points to it.

## Formatting

Each Node/Angular project owns its Prettier dependency, lockfile, configuration and ignore rules. Install dependencies with `npm ci` inside that project, then run `npm run format` or `npm run format:check`. Go uses the standard `gofmt` tool. There is no root npm package, automatic commit-formatting hook or GitHub Actions formatting workflow.

1. `make format-node` runs each independent Node project's `npm run format` over its full source tree. `make format-node-check` runs each project's read-only `npm run format:check`. Existing formatting differences may cause a full-project check to fail.
2. `make format-go` formats changed tracked/staged Go files relative to `HEAD`, excluding vendor dependencies and generated filename patterns. Newly created files become eligible once added to Git. `make format-go-check` checks the same files without writing.
3. `FORMAT_BASE=<revision>` changes the Go comparison base only. For a full Go project, run `gofmt -w` on its first-party source directories, excluding its vendor tree.
4. `make format` runs both Node and Go formatting; `make format-check` runs both read-only checks. These commands use each project's native tooling, without a shared npm installation.

Manual formatting changes working-tree files; it does not stage or commit anything. When files are partially staged, review and stage the intended hunks afterward. Per-project Prettier ignore rules exclude dependencies, build outputs, packaged releases, npm lockfiles and generated filename patterns.

## Design and code conventions

1. Go owns the API, Temporal workflow interpreter, activities, and persistence. Angular lives under `studio/` and uses relative `/api` calls. Keep UI language understandable to non-technical users.
2. Treat [the JSON schema](spec/workflow.schema.json), [the contract guide](docs/json-contract.md), Go validation, and UI types as one public contract. Update affected examples and documentation alongside behavior changes.
3. Keep workflow code deterministic. Use Temporal timers and concurrency primitives; put network calls, database access, credentials, and other side effects in activities or API adapters. Definitions may configure supported HTTP/database nodes and contain sandboxed JavaScript through [Custom Code](docs/custom-code.md); never evaluate that code in the deterministic workflow interpreter or permit unrestricted host execution.
4. Preserve immutable published definitions and the snapshot embedded in each run. Changes to Temporal command sequences require a compatibility strategy and representative history replay checks; definition versioning alone does not version worker code.
5. Test the affected wait, callback, signal, approval, pause/resume, cancellation, and parallel paths when modifying execution behavior. Document any changed builder rules.
6. Format changed Go files with `gofmt`. Follow existing TypeScript, HTML, and SCSS conventions without reformatting unrelated files. Commit dependency lockfile updates when changing dependencies.
7. Add focused regression tests for behavior changes. Explain the expected behavior and reproduction steps for a bug fix.
8. Studio has four independent Angular projects under `studio/projects`: `host`, `workflows`, `management` and `platform-admin`. Each must build from its own directory alone, with its own package/lockfile, compiler configuration, tests, Dockerfile and dist. No sibling imports, shared-source aliases, npm workspaces, inherited parent configurations or automatic library builds. The separate independent library project is `studio/libraries/platform`; apps consume its versioned compiled `@runloom/studio-platform` package, currently vendored as a release tarball inside each app. Update consumers explicitly when releasing it. Host owns authentication; runtime federation shares its session. Run local `npm test` and `npm run build` in each changed project, and federation browser tests for contract changes.
9. Keep Angular component logic in `.ts`, templates in `.html`, and styles in `.scss`. Use `templateUrl` and `styleUrl`/`styleUrls`, not inline `template`/`styles`. Dynamic template bindings are appropriate for runtime values such as canvas positions; fixed presentation belongs in SCSS. Apply the same separation to the independent product website.
10. In each Studio app, reserve `src/` for `main.ts`, `index.html` and the `app/` tree. Put application services/contracts in `app/core`, shells in `app/layout`, domain screens and their private components/models/helpers in `app/features/<feature>`, and genuinely reusable components/styles/utilities in `app/shared`. Keep component `.component.ts`, `.component.html` and `.component.scss` files together. Core/shared code must not import feature or shell implementations; features must not import shells. Federation entry points belong in `app/federation`; host route configuration belongs in `app/routing`. Keep standalone components rather than adding empty NgModules or routing files for visual symmetry. Existing host navigation remains the owner of app/team transitions and unsaved-change guards. Tests stay in each independent project's `tests/` directory. `npm test` enforces layout, import resolution and these dependency boundaries.

### Consistent feature composition

Use the same composition rules in host, workflows and management. Do not introduce a different state framework, inheritance hierarchy or app-specific architecture merely because a feature grows.

1. Components own Angular inputs/outputs, DOM references, focus, animation and lifecycle wiring. Keep HTML and SCSS separate. Components compose feature-local controllers rather than inheriting from a large base component.
2. Put stateful feature controllers in `features/<feature>/state/`. Give each mutable value one owner. Pass typed, narrow context ports for current state and callbacks; never pass the component itself, import a component into a controller, or copy its entire state into another store. Existing signals and plain properties remain appropriate; this pattern does not require NgRx or another dependency.
3. Put transport and browser persistence adapters in feature-local `services/`, and pure transformations/types/defaults in the feature's existing model/helper directory. Keep application-wide API clients in `core/api`. Controllers coordinate use cases; adapters must not decide UI focus or animation.
4. Scope controllers to the component instance and dispose timers/streams with that instance. Preserve generation checks, request IDs, dirty-draft protection, retry limits and authorization checks during extraction. Extract by responsibility, not an arbitrary line limit.
5. Apply these conventions to management as features are changed; do not move unrelated screens solely for symmetry. Keep every app independently buildable, with its own tests and packaged platform dependency. Sharing a convention does not mean sharing sibling source files.
6. Test extracted behavior in the owning project's `tests/` directory and retain browser coverage for the component/controller bindings. Lumi's `AssistantSession` and the builder's edit-lock, editor, version and test-run controllers are examples of the same composition pattern.

## Checks before a pull request

Run the checks relevant to the files changed. The standard checks from the repository root are:

```bash
make test
make check-go
docker compose --project-directory . -f compose/compose.server.yaml config --quiet
```

For deployment changes, also run `make helm-lint`, `make docker-build`, and `make smoke` against an isolated local stack. The [deployment guide](docs/deployment.md) describes the prerequisites. Do not deploy to staging as part of a normal contribution check.

`make test` runs Go tests and the Angular production builds. It does not run browser interaction tests. Each Studio project's `npm test` checks source boundaries and build tooling; Angular component TestBed tests are not configured.

Install the workflow determinism checker before `make check-go`:

```bash
go install go.temporal.io/sdk/contrib/tools/workflowcheck@v0.5.0
```

The Makefile expects it in `$(go env GOPATH)/bin`; omit a custom `GOBIN` for this installation. Go may download the toolchain required by the checker.

For UI changes, also open the studio and exercise the affected controls against the Go API. For image or build changes, run `make docker-build`. Documentation-only changes normally need link and command review rather than the entire application suite.

PostgreSQL and MongoDB integration tests are skipped unless their test URLs are set. Use disposable databases:

```bash
cd server/workflows
RUNLOOM_TEST_POSTGRES_URL=postgresql://runloom:runloom@localhost:5432/runloom_test go test -count=1 ./internal/store -run TestPostgresStoreContract
RUNLOOM_TEST_MONGODB_URL=mongodb://localhost:27017 go test -count=1 ./internal/store -run TestMongoStoreContract
```

Create the PostgreSQL test database first; that test leaves records in it. The MongoDB test creates a uniquely named database and drops it during cleanup. Report skipped checks explicitly.

## Pull requests and review

The independent documentation application lives in `docs-site/`. Run `make test-docs` and `make test-docs-browser` for changes to its UI or rendering. Existing guides remain canonical in `docs/`; `make sync-docs` explicitly refreshes packaged guide and OpenAPI snapshots, while `make check-docs-sync` detects drift. Ordinary documentation builds never read or build sibling projects. New introductory tutorials belong in `docs-site/content/`. See [documentation maintenance](docs-site/README.md).

The independent product website lives in `Website/`. Run `make setup-website` and `make test-website` for website changes; use its documented Playwright checks for interactive or responsive changes. Keep it independent of the studio/backend, avoid unsupported product claims, and do not add it to the product's container or Helm builds. See [website development](Website/README.md).

Authentication changes must preserve PKCE, verified-email checks, token issuer/audience/signature validation, and server-side org/team authorization. Test cross-team reads and mutations, permission revocation, last-owner protection, and trusted audit attribution. Keep `deploy/keycloak/runloom-realm.json` and `charts/runloom/files/runloom-realm.json` identical. Use [the browser smoke test](docs/authentication.md#validation) for login/recovery changes; never record credentials or email action links in test output. New directory/team integration tests leave synthetic records and must use disposable databases.

1. Keep each pull request focused on one change and use a concise title describing its result.
2. Explain the problem, resulting behavior, relevant compatibility implications, and validation performed. Include screenshots for visual changes and a minimal JSON example for contract changes.
3. Keep the working branch to one meaningful commit when ready for review. Amend or squash follow-up fixes. Coordinate before rewriting a shared branch; use `--force-with-lease` if updating your own previously pushed branch.
4. Keep README, API, runtime, and deployment instructions consistent with code. Preserve existing authorship and avoid unrelated changes.
5. Discuss feedback respectfully and focus on evidence and behavior. Maintainers review changes before merge.
6. Treat staging deployment as a separate maintainer process; submitting or merging a contribution does not authorize deployment.

## Contribution licensing

By intentionally submitting a contribution for inclusion, you agree to license that contribution under the project's [MIT License](LICENSE). Submit only work you are entitled to contribute; you retain ownership of your contribution.

Preserve third-party copyright notices, license texts, and any required NOTICE files. Identify the source and license of copied code or assets in the pull request and update [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) when dependencies or attribution change. Do not represent third-party material as original MIT-licensed project code.

Keep the root `LICENSE` and the copies in `server/`, each independent service, `server/gateway/`, `tools/database-setup/`, `sdk/go-auth/`, `Website/`, `charts/runloom/`, each Studio application and the platform library identical. Preserve upstream licenses in vendored dependencies. Run `make check-branding` to verify naming and license consistency.

Backend services must build and test from their own directory alone. Do not add sibling imports, local module replacements, a parent Go workspace or hidden SDK builds. Each project owns its module, dependencies, Docker context and binary output. The token SDK is a pinned vendored dependency, updated explicitly. Use HTTP contracts and owner-restricted databases across services. Run `make check-service-boundaries` and both database selections of `make test-services-live` for cross-service changes.
