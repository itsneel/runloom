# Runloom Platform Admin

Independent Angular application for platform operations. It owns its package, lockfile, source, tests, Webpack federation entry, Docker context and `dist/browser`. No sibling source imports or builds are required. The versioned Studio platform library is vendored locally, like the other applications.

## Build and test

```sh
npm ci
npm test
npm run build
npm start
```

The development server listens on port 4203. The host proxies `/remotes/platform/` to it and loads this application's `./Application` export at `/platform/health`, `/platform/organizations`, `/platform/staff` and `/platform/audit`. Start the host separately; neither application builds the other. Opening this remote's standalone development origin displays the shared remote landing page, not a second login/session owner.

Authentication uses the host's shared, versioned `AuthService` at runtime. All authorization is enforced by Auth-owned `/api/v1/platform/*` APIs. Platform staff have read-only operational and organization metadata access; role management and platform audit require a platform admin. Organization/team roles do not grant platform permissions, and platform roles do not bypass tenant authorization.

`docker build -t runloom-platform-ui:local .` produces a non-root, read-only-compatible Nginx runtime. Compose and Helm provide its own `platform-ui` service and deployment.

See [platform administration](../../../docs/platform-administration.md) when this project is in the Runloom repository. The project itself can build without that document or any sibling project.
