# Runloom Studio platform library

Independent npm library: `@runloom/studio-platform`. This directory is a complete project and can become its own repository. Its source, dependencies, lockfile, tests, compiler configuration, license and releases do not require any Studio application or parent configuration.

## Build and package

Use Node.js 26.13.0 (`nvm install` and `nvm use`). From this directory:

```sh
npm ci
npm test
npm run build
npm run pack
```

`dist/` contains Angular Package Format output compiled in partial mode. `releases/runloom-studio-platform-<version>.tgz` is the distributable npm package. Builds never install, build or modify consuming applications. The library is not an application or an Angular NgModule.

## Responsibilities and security

1. `AuthService` owns the Keycloak client, token refresh, profile, organization/team selection and permission state. Only the authenticated host initializes it. Tokens stay in memory; organization/team preferences are not credentials.
2. `authInterceptor` supplies authorization and team context to API requests. Backend authorization remains authoritative. The library does not turn UI permission checks into a security boundary.
3. `WorkspaceBridge` defines runtime navigation and discard-guard contracts. It imports no feature implementations. Dialog and theme services and common UI components live here too.
4. Keycloak owns its identity-session cookies. Applications do not read another application's cookies, copy sessions into localStorage, or receive client secrets. Remotes reuse the host's in-memory singleton in the same page. Independently hosted pages would require their own interactive authentication flow; this package does not implement cross-origin session transfer.

## Release and consumer updates

1. Change library source and tests, bump `package.json` version and refresh the lockfile. Run `npm ci`, `npm test` and `npm run pack`. Never overwrite an already distributed release version.
2. If an npm registry is available, explicitly publish the built `dist/` package using your organization's release process. No registry or publication credentials are configured here, and no build publishes automatically.
3. Until then, copy the release `.tgz` into each consumer's own `vendor/` directory and explicitly install that artifact there, for example `npm install ./vendor/runloom-studio-platform-1.7.0.tgz --save-exact`. Commit the artifact, package manifest and lockfile in that consumer's repository. Do not use sibling `file:../` dependencies, symlinks, npm workspaces or source aliases.
4. Consumers decide when to upgrade. Editing or rebuilding this repository alone has no effect on an installed application. Run cross-application compatibility tests before deploying changes to public contracts.

Module Federation shares this installed package as a strict-version singleton. Apps have independent builds/releases but require compatible Angular and platform versions when composed in the same browser. Breaking runtime-contract changes need coordinated releases or a backward-compatible transition. Use additive APIs and semantic versioning. The host also checks the remote application's numeric contract version.

Angular's [library packaging guide](https://angular.dev/tools/libraries/creating-libraries) describes the compiled-package boundary; consumers use built packages rather than library source files.
