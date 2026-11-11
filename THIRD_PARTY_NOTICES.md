# Third-party licenses

The [Apache License 2.0](LICENSE) applies to original Runloom code, documentation, schemas, and examples. It does not replace the licenses of dependencies, database servers, container base images, or externally hosted services.

This document identifies dependencies from the independent `server/*/go.mod`, `server/gateway/go.mod`, `tools/database-setup/go.mod`, `sdk/go-auth/go.mod`, Studio package lockfiles and the Website package lockfile. It is a license summary, not an exhaustive transitive dependency audit. Preserve the authoritative LICENSE and NOTICE files in each project's vendored dependencies.

## Go dependencies

The independent `sdk/go-observability` module and its service vendor snapshots use the OpenTelemetry Go SDK and OTLP exporters under Apache-2.0. Their upstream license is preserved under each service's `vendor/go.opentelemetry.io/otel/LICENSE`. Optional observability containers use OpenTelemetry Collector Contrib and Prometheus; preserve their bundled upstream licenses and notices when distributing those images.

The JavaScript runner embeds QuickJS-NG 0.11.0 via `github.com/paralin/go-quickjs-wasi` (MIT) and uses `github.com/tetratelabs/wazero` 1.11.0 (Apache-2.0). Their license texts are shipped in the server image under `/usr/share/licenses/runloom/code-runtime`. Preserve these when distributing the standalone `runloom-code` binary. See [QuickJS-NG](https://github.com/paralin/go-quickjs-wasi/blob/v0.11.0/LICENSE) and [wazero](https://github.com/tetratelabs/wazero/blob/v1.11.0/LICENSE).

| Module | License | Upstream license |
| --- | --- | --- |
| `github.com/coreos/go-oidc/v3` | Apache-2.0 | [go-oidc](https://github.com/coreos/go-oidc/blob/v3.21.0/LICENSE) |
| `go.temporal.io/sdk` | MIT | [Temporal Go SDK](https://github.com/temporalio/sdk-go/blob/v1.48.0/LICENSE) |
| `go.temporal.io/api` | MIT | [Temporal API](https://github.com/temporalio/api-go/blob/v1.63.4/LICENSE) |
| `github.com/google/uuid` | BSD-3-Clause | [UUID](https://github.com/google/uuid/blob/v1.6.0/LICENSE) |
| `github.com/jackc/pgx/v5` | MIT | [pgx](https://github.com/jackc/pgx/blob/v5.8.0/LICENSE) |
| `github.com/go-sql-driver/mysql` | MPL-2.0 | [MySQL driver](https://github.com/go-sql-driver/mysql/blob/v1.9.3/LICENSE) |
| `filippo.io/edwards25519` | BSD-3-Clause | [edwards25519](https://github.com/FiloSottile/edwards25519/blob/v1.1.0/LICENSE) |
| `github.com/santhosh-tekuri/jsonschema/v6` | Apache-2.0 | [JSON Schema](https://github.com/santhosh-tekuri/jsonschema/blob/v6.0.3/LICENSE) |
| `go.mongodb.org/mongo-driver/v2` | Apache-2.0 | [MongoDB Go driver](https://github.com/mongodb/mongo-go-driver/blob/v2.9.1/LICENSE) |
| `github.com/stretchr/testify` (tests) | MIT | [Testify](https://github.com/stretchr/testify/blob/v1.11.1/LICENSE) |

## UI and build dependencies

The independent `docs-site/` application uses Angular, MiniSearch (MIT), Marked (MIT), github-slugger (MIT), sanitize-html (MIT), and Swagger UI (Apache-2.0). Markdown processing occurs at build time; Swagger is loaded only for API references. Distributions retain Angular's extracted notices, the Runloom license and Swagger's LICENSE, NOTICE and bundled dependency notices. Playwright (Apache-2.0), axe (MPL-2.0), Prettier (MIT) and serve (MIT) are development tools, not a required hosting runtime. This project pins patched compression 1.8.2 for the local serve preview tool and Vite 8.3.3 for Angular development tooling.

The Go API embeds `swagger-ui-dist` 5.33.1 (Apache-2.0) for its interactive API documentation. Its upstream LICENSE, NOTICE and bundled third-party license notices are retained in `server/workflows/internal/api/docs/` and served under `/api/docs/assets/`. Preserve those files with the JS/CSS assets. The independent maintenance package `server/api-docs` pins its dependency lockfile; its OpenAPI validator is development tooling, not a runtime server dependency. Swagger UI is served locally without CDN or remote-validator requests. See [upstream installation](https://swagger.io/docs/open-source-tools/swagger-ui/usage/installation/) and [configuration](https://swagger.io/docs/open-source-tools/swagger-ui/usage/configuration/).

The independent `Website/` Angular application has its own lockfile and uses Angular's esbuild application builder. It reuses no studio runtime code. esbuild is [MIT licensed](https://github.com/evanw/esbuild/blob/main/LICENSE.md); its optional Playwright browser-test tooling is [Apache-2.0 licensed](https://github.com/microsoft/playwright/blob/main/LICENSE). Website distributions must preserve their own generated third-party notices and project license. Product database/server images are not dependencies of the static website.

Website accessibility tests use `@axe-core/playwright` and `axe-core`, whose installed packages declare MPL-2.0. These are development-only dependencies and are not shipped in the browser application. Preserve their package licenses if redistributing the test tooling.

| Packages | License | Upstream project |
| --- | --- | --- |
| Angular framework packages | MIT | [Angular](https://github.com/angular/angular) |
| Angular CLI and build tooling | MIT | [Angular CLI](https://github.com/angular/angular-cli) |
| RxJS | Apache-2.0 | [RxJS](https://github.com/ReactiveX/rxjs) |
| keycloak-js | Apache-2.0 | [Keycloak JavaScript adapter](https://github.com/keycloak/keycloak-js/blob/26.2.4/LICENSE.txt) |
| tslib | 0BSD | [tslib](https://github.com/microsoft/tslib) |
| TypeScript | Apache-2.0 | [TypeScript](https://github.com/microsoft/TypeScript) |
| Webpack | MIT | [Webpack](https://github.com/webpack/webpack) |
| @angular-architects/module-federation and runtime | MIT | [Angular Architects Module Federation](https://github.com/angular-architects/module-federation-plugin) |
| @angular-builders/custom-webpack | MIT | [Angular Builders](https://github.com/just-jeb/angular-builders) |
| ng-packagr (platform library build) | MIT | [ng-packagr](https://github.com/ng-packagr/ng-packagr) |
| Prettier | MIT | [Prettier](https://github.com/prettier/prettier) |

The installed packages contain their respective license texts. Production UI builds explicitly extract `3rdpartylicenses.txt` next to the browser bundles. Preserve that generated file and `LICENSE` when distributing or serving the UI. The Docker UI image copies both with the application assets.

## Database servers and containers

The optional independent `charts/runloom-ingress` chart vendors the unmodified F5 NGINX Ingress Controller Helm chart 2.8.0 (controller 5.7.0), from `oci://ghcr.io/nginx/charts/nginx-ingress`. Upstream source is [nginx/kubernetes-ingress](https://github.com/nginx/kubernetes-ingress/tree/v5.7.0), licensed Apache-2.0. Runloom's Apache 2.0 license applies only to the wrapper and original configuration. Keep the upstream archive and Apache license when distributing the wrapper. Controller and NGINX container dependencies retain their own licenses.

PostgreSQL uses the [PostgreSQL License](https://www.postgresql.org/about/licence/).

Keycloak is Apache-2.0 licensed; see its [license](https://github.com/keycloak/keycloak/blob/26.4.7/LICENSE.txt). Mailpit uses the [MIT license](https://github.com/axllent/mailpit/blob/v1.27.0/LICENSE). Their container dependencies retain their own notices.

MongoDB Community Server uses [SSPL-1.0](https://www.mongodb.com/legal/licensing/server-side-public-license), which is [not an OSI-approved open-source license](https://opensource.org/blog/the-sspl-is-not-an-open-source-license). This differs from the Apache-2.0 license of the MongoDB Go driver. See [MongoDB's licensing explanation](https://www.mongodb.com/legal/licensing/community-edition) for the server and driver distinction. The Compose file currently starts both database services, even when PostgreSQL is selected for application storage.

The Go, Node.js, Nginx, distroless, Temporal, PostgreSQL, and MongoDB images each include software with its own terms and notices. A container label declaring this application's Apache 2.0 license does not relicense its base image or dependencies. Review the exact image versions and their bundled notices when redistributing images.

## Distribution

1. Include the project's Apache 2.0 license with source, binaries, and browser distributions. The Go image includes it at `/usr/share/licenses/runloom/LICENSE`; the UI serves it as `/LICENSE`.
2. Preserve upstream copyright, license, and NOTICE files as required. For Go binary releases, collect applicable notices for the packages actually linked into the binaries, including transitive dependencies. The current Docker build copies the project license but does not generate that Go dependency notice bundle.
3. Preserve Angular's generated `3rdpartylicenses.txt` with the production browser assets. It covers licenses extracted by the bundler, not every build tool or container package.
4. Recheck dependency licenses and this document when changing Go manifests, any Studio application/library manifest or lockfile, website manifests or lockfiles, or container base images. Each independent project must carry its own project license and preserve applicable dependency notices.
