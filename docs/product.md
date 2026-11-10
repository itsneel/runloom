# Product definition

## Users

The product serves two non-technical user modes. Builders assemble and publish workflows on a node canvas with a guided parameter panel. Operators choose a published template, provide inputs or call a webhook, inspect progress, approve or reject human gates, simulate or inspect callbacks and named signals, resume checkpoints, and cancel runs when necessary.

## Version 1 journey

Studio provides a private [assistant chat](assistant.md) with persistent messages and live responses. Updated Studio saves new standalone workflow drafts from general chat, fixes or enhancements to the selected saved workflow, and child draft attachments automatically when generation succeeds and current access permits. Older jobs retain explicit review. The backend enforces scope, permissions, locks, graph revisions and atomic writes. To edit a child, open that child as the selected workflow. It cannot publish, execute or activate schedules.

1. A builder creates or imports a definition.
2. The system validates structure, unique step identifiers, supported operators, bounds, and safe activity configuration.
3. Publishing creates an immutable numbered snapshot.
4. Saving creates an attributed draft commit, and restoring history creates another draft commit without rewriting past releases.
5. An operator starts a run from a published version and supplies JSON input.
6. The API passes the complete published definition and input into Temporal, so replay never reads mutable external definition state.
7. The operator watches progress and responds only to the gate currently shown: approval, callback, named signal, or resume.
8. Temporal durably completes, fails, times out, or cancels the run.
9. The application database records the original input, searchable status and output snapshots, and actor-attributed audit events for the operator history screen.

## Explicit non-goals for version 1

1. Unrestricted script execution or host access. Definitions may contain bounded JavaScript in the sandboxed [Custom Code node](custom-code.md); they cannot run arbitrary host processes or bypass the sandbox.
2. Unrestricted network or database access. [External API nodes](api-nodes-and-secrets.md) and [Database nodes](database-connections.md) support configured requests and queries through guarded activities and managed credentials, not direct I/O from workflow code.
3. Custom role definitions, per-node approval policies and SCIM. Standard account login, organization/team roles, team-level isolation and scoped consumer service accounts are supported; see [authentication](authentication.md) and [consumer execution](consumer-api.md).
4. Queue or broker event triggers. Manual, HTTP webhook, recurring schedule and one-time schedule starts are supported.
5. Editing a published version or changing an already-running definition.
6. Unbounded loops; they can grow workflow history without limit.

## Product invariants

1. Drafts are editable; published versions are immutable.
2. Runs always identify their exact definition version.
3. Definitions orchestrate supported node capabilities and may include sandboxed JavaScript, SQL or JSON queries, and HTTP request configuration. Credential values remain outside definitions; nodes reference stored secrets or connections. Side effects and Custom Code execution belong in activities, not deterministic workflow code.
4. Rejected approvals are data, not infrastructure failures, so a later condition can route the run.
5. Every side-effecting activity must be idempotent because Temporal can retry it.
6. A wait always resumes itself; it never depends on a callback or human action.
7. A callback accepts data from an external system for one exact step; it is not an approval.
8. A pause resumes only through an explicit operator action or its configured timeout rule.
9. Cancellation is permanent and is never presented as a pause.
10. Model provider credentials are worker configuration and never definition data.
11. Webhooks start only the latest immutable published version, never an editable draft.
12. Temporal Event History is the replay authority. Workflow code never reads application databases or files directly. Activities may read current state, including budget policies, and return decisions that Temporal records in history for deterministic replay.
13. Schedules pin published parent/child snapshots and saved input. Publishing never silently upgrades an existing schedule.
14. Reusable subworkflows are same-team published definitions with `kind: "child"`. Production execution requires a parent's Subworkflow node; normal API, webhook and schedule starts are blocked. Studio editors/admins may independently test main and child drafts without publishing through a separate authenticated test endpoint. Tests run real activities and are labeled in history. Parents wait for children; failures propagate and parent cancellation closes active children. See [schedules and subworkflows](schedules-and-subworkflows.md).

Independent [Temporal signal handler paths](signal-handlers.md) receive named events within a running workflow while its main path continues. They are separate canvas entry points within the same execution.
