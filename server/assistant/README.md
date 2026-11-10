# Runloom assistant

Independent Go service for private, team- and optionally workflow-scoped conversations, persistent streaming responses and authoring jobs. General chat supports standalone workflow draft creation. Updated Studio submissions opt into automatic draft saving; older jobs retain explicit review. Workflow chat supports fixes/enhancements to the selected workflow and creation/attachment of child drafts through the workflow owner's guarded API. The configured model receives the full authorized saved definitions and the bundled node schema; credential stores are excluded. It has no execution client, privileged internal service token, or access to another service's database. The current human bearer is forwarded only during context reads and authenticated draft-save requests; it is never persisted.

## Build and test

```sh
make build
make test
RUNLOOM_CONFIG_FILE=/absolute/path/config.yaml make run
```

The module and vendored SDKs build without sibling directories. From the repository root, `make test-assistant-live` starts disposable PostgreSQL and MongoDB containers and verifies storage, streaming, cancellation, idempotency, worker fencing and recovery. It removes only its own test containers and volumes.

See [the operator and user guide](../../docs/assistant.md) and [OpenAPI contract](api/openapi.yaml).

New message submissions may set `save_draft: true`. The persisted job records this opt-in (including duplicate-request matching); its context tells the model that Studio will attempt an automatic draft save. Studio uses the existing exact-hash apply API and current human credentials after generation. No bearer token is retained, so saving requires the conversation to be open or reopened. Existing jobs default to review mode.
