# Rexera lien tagging integration

Status: implemented for local integration review on 25 November 2026. No shared staging or production rollout is included. The companion R2 application adapter and its state/migration adoption remain a separate deployment and ownership review.

## Definition and activities

`examples/rexera-lien-tag.json` publishes through the normal Studio API and executes on the existing Go Temporal worker. It uses only the existing activity and For Each node contracts. The registered `rexera.lien.read`, `rexera.lien.classify`, `rexera.lien.save`, `rexera.lien.plan-qa`, `rexera.lien.submit-qa` and `rexera.lien.finish` activities are thin boundaries to the saved R2 source, existing Agent Tag classifier and Stage 7 QA entry. They do not implement QA checks, decisions, invalidation or downstream release. There is no new interpreter, platform store, classifier or OCR engine.

Input is a saved R2 `document_id` and `event_id`, both UUIDs. Trusted evidence, tags, order identity, source date and signed PDF URL are read from R2. Definition input cannot change endpoint configuration or credentials. Published versions are immutable; the R2 dispatcher must pin one granted version. Retrying the same admitted source event uses the same consumer Idempotency-Key and run.

The definition handles current source reads, optional classification, guarded saving and category fan-out. The saved R2 adapter controls tag policy, stale-result rejection and durable category delivery identities. A QA retry re-reads the current R2 handoff before posting it and reuses its stable key. The receiver must acknowledge durable acceptance with `{ "accepted": true, "handoff_id": "<matching key>" }` and deduplicate the key. Receiver acceptance is separate from QA approval. HTTP delivery is at least once; receiver idempotency is required after a lost acknowledgement.

Only the configured team can use these privileged activities. Team restriction uses the actual Temporal workflow identity created by Runloom admission, not an input field. Use a dedicated team and a least-privilege consumer account granted the selected definition and version. Other teams receive a non-retryable denial even when their definition names one of the activities.

## Worker configuration

The integration is inactive until its worker configuration is supplied. Add these settings to the mounted worker YAML and restart the worker:

```yaml
RUNLOOM_LIEN_TEAM_ID: "<dedicated-team-uuid>"
RUNLOOM_LIEN_R2_URL: "https://<r2-api-host>"
RUNLOOM_LIEN_R2_TOKEN_FILE: "/run/secrets/rexera-api-token"
RUNLOOM_LIEN_CLASSIFIER_URL: "https://<ai-service-host>/agents/tag/tag"
RUNLOOM_LIEN_CLASSIFIER_TOKEN_FILE: "/run/secrets/rexera-ai-token"
RUNLOOM_LIEN_QA_URL: "https://<stage-7-orchestrator>/document-entry"
RUNLOOM_LIEN_QA_TOKEN_FILE: "/run/secrets/rexera-qa-token"
```

Token files are deployment-owned read-only files. Credentials are never embedded in definitions or returned by these activities. Service failures report bounded status information without provider response bodies or secrets. Requests do not follow redirects. HTTPS is mandatory unless the explicit local fixture setting `RUNLOOM_LIEN_ALLOW_HTTP: "true"` is supplied. Do not enable that flag in a shared environment.

The classifier receives `order_type: "LIEN"`, R2's native order ID and a newly signed PDF URL. It contributes only its primary supported confident classification. An unreadable/unsupported response or exhausted classifier transport failure becomes manual review; the original stays available in R2 Files. Staff correct tags through the existing R2 editor. There is no manual Auto Tag/Retry operation in this definition.

The QA entry is an orchestrator receiver owned by TKT0023, not a direct AI extraction endpoint. Its R2 contract includes current document/file/category references, saved source/request/attempt/date where known, binding revisions, superseding references and removal notifications. Water/Sewer generates separate category submissions. The companion R2 guide `docs/lien-workflows/runloom-integration.md` is the source for that adapter contract and rollout requirements.

## Local verification

1. Install the full product using `make local-services-up SERVICES_DATABASE=postgres`. This includes Studio, gateway, Auth/Keycloak, independent services/stores, worker, Temporal and supporting local services. Follow `docs/service-split.md`; the native interpreter/handler trial alone is not a complete installation.
2. Run `scripts/services-smoke.mjs` with a configured Playwright module/browser and the preset Studio URL. It checks real browser login, team access, independent product routes, publication, worker execution, consumer grants/idempotency and credential revocation. It has no paid-model requirement.
3. Use the companion R2 `serverless-api/scripts/testing/lien-runloom-integration.ts` against an explicitly isolated local database and this checkout. It provisions a synthetic team/account, publishes this definition, mounts fixture credentials and rebuilds/restarts this local worker. It exercises actual R2 route/Prisma/trigger behavior and installed Runloom services. Supabase auth/storage, the classifier and pending QA receiver are contract fixtures. This does not establish shared staging or live AI/QA integration.
4. Run `go test ./internal/rexeralien ./internal/config ./internal/definition` from `server/workflows`. The publication test validates the checked-in definition and registered activity names. HTTP boundary tests check credential transport, R2 response envelopes and stable QA keys. Normal test runs skip the installed history replay.
5. The R2 integration runner invokes `TestInstalledLienHistoryReplay` against its completed run using the local payload encryption key. Replay registers no activities; it validates deterministic interpreter behavior without sending external calls. Keep encryption keys and local credential files outside Git. Synthetic encrypted history may be retained with test evidence; never publish a key alongside it.

On 25 November the complete local service smoke passed, and the installed lien integration passed thirteen scenarios including fixed tax roles, Water/Sewer fan-out and category audit history, unreadable/manual recovery, late results, lost acknowledgements, pending saved-link intake, exclusions, file replacement, date/reassociation changes, scope/cancellation, duplicate admission and encrypted Temporal replay. The fixtures are described above; this evidence does not claim a live QA implementation or production readiness. The companion R2 handoff-timeline component also passed its unit test and a browser fixture using saved local R2 audit records; this does not exercise the live R2 order screen. The Workflows UI build and its 73 tests passed. The action selector preserves a loaded registered custom activity name instead of displaying a blank selection.

## Diagnosis and recovery

Use the run ID and Temporal run ID saved by the actual Read activity to correlate Studio history, Temporal execution and the R2 source event. Studio history records step starts, retries and category delivery receipts. Worker-safe logs intentionally omit provider payloads. A failure before Finish is reconciled by the R2 dispatcher's consumer run-status read.

An ambiguous admission must retry with the original event/key. A QA transport retry must retain the current handoff/key. A stale source is superseded instead of overwriting staff changes. Direct manual tagging in Files is the recovery path for classification; QA execution recovery belongs to TKT0023. Do not change the definition's published version, ledger keys or live state to hide a failure. Resolve Runloom application-state ownership and the shared deployment target before a rollout; this implementation does not add another production runtime alongside Agent One without that decision.
