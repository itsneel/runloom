# Workflow definition versioning

Workflow definition history is application source control for JSON designs. It is separate from Temporal Event History and Temporal Worker Versioning.

## Lifecycle

1. Saving validates the complete JSON definition, writes the editable version `0` draft, and creates an immutable commit containing the full snapshot, actor, message, sequence number, kind, and timestamp.
2. Publishing copies the current draft into the next immutable numbered version and creates a `publish` commit linked through `published_version`.
3. Editing a published definition changes it to a draft in the studio. The previously published version remains unchanged and runnable.
4. Studio tests do not require publishing. After the first draft save, **Test workflow** snapshots the current builder, including unsaved edits, as a version-zero test. Testing does not save, commit or publish; later edits do not alter an active test. Production starts still require a published version. Subworkflow references still pin published children. Tests have real side effects and costs.
5. Restoring a commit copies its snapshot into the editable draft and creates a new `restore` commit. Restore never deletes commits or rewrites a published version.
6. Starting a production workflow embeds one exact numbered definition version and all pinned child definitions in Temporal input. Later saves, publishes, and restores cannot change that execution.
7. Schedule creation pins the selected published root and its transitive child snapshots. New publications do not update existing schedules; recreate a schedule to adopt them. A Subworkflow node always specifies an explicit published child version. See [scheduling and reuse](schedules-and-subworkflows.md).

Commit kinds are `save`, `publish`, and `restore`. Sequence numbers increase independently for each workflow definition. Commit messages are limited to 500 characters.

## API

Draft saving and publication have separate validation rules. Incomplete nodes, empty workflows, invalid input/output schemas, missing child workflows and unresolved prompt references can be saved and restored as draft commits. The storage envelope still requires valid JSON with recognized field types, `schema_version: "1.0"`, a safe workflow ID, at most 1 MiB, 500 nodes and 10 nesting levels. Malformed JSON editor text must be corrected/applied before saving; it is not a workflow definition.

Studio checks a saved draft and displays a publishing-blocked warning when validation fails. Save remains available. Editing clears the old warning; Check, Save and Publish revalidate the current content. Both the publication API and draft-test API reject invalid workflows with HTTP 422. Failed publication does not allocate a release or change an earlier published version. A draft with an unresolved prompt reference must be saved again after fixing that reference so its immutable template snapshot can be captured.

All definition requests require a verified access token and `X-Runloom-Team`; mutations require team editor/admin access. The API derives the actor from the verified identity, overriding any supplied `X-Runloom-Actor`. See [authentication](authentication.md) and the [workflow execution API](workflow-api.md).

1. `POST /api/v1/studio/definitions` saves a draft commit. Use `X-Runloom-Commit-Message` to describe the change.
2. `POST /api/v1/studio/definitions/{definition-id}/publish` creates the next numbered version and publish commit. It accepts the same commit-message header.
3. `GET /api/v1/studio/definitions/{definition-id}/history` returns commit summaries and published version numbers.
4. `GET /api/v1/studio/definitions/{definition-id}/commits/{commit-id}` returns one commit with its complete JSON snapshot.
5. `POST /api/v1/studio/definitions/{definition-id}/commits/{commit-id}/restore` accepts `{"message":"..."}` and creates a new draft from that snapshot.
6. `GET /api/v1/studio/definitions/{definition-id}?version=N` retrieves an immutable published version. Version `0` retrieves the current draft.

## Persistence

1. PostgreSQL stores commits in `runloom_definition_commits`. Draft save, publish, restore, and commit insertion are transactionally grouped and serialized per definition.
2. MongoDB stores commits in `definition_commits` and allocates per-definition sequences through `definition_counters`. Unique indexes protect commit identifiers and sequences.
3. Filesystem development mode stores commits below `_commits/{definition-id}` using atomic file replacement and an in-process lock.
4. The current MongoDB adapter remains compatible with standalone servers, compensates ordinary write failures, and cannot eliminate a process-crash window between collection writes. Strict cross-collection atomicity requires an additional session-transaction code path and a replica-set or sharded deployment.

## Temporal boundary

JSON definition versions choose what a new run executes. They do not version Go worker code. Changes to worker code that alter Temporal commands still require Temporal patching, workflow-type versioning, or Worker Deployment Versioning plus replay testing.
# Concurrent editing

Saved workflows use exclusive, renewable editing sessions. Select **Edit workflow** before changing, committing, publishing, or restoring a workflow. Other sessions remain read-only until the lock is released or an idle lease expires. See [workflow editing and API locking](workflow-editing.md) for behavior and recovery.

## Canvas validation feedback

The builder checks edited drafts automatically after a short typing pause. Nodes with validation problems have a red border and a “Needs attention” indicator in both light and dark mode. Hover over a marked node for its error, or select it to see the explanation in its settings. Nested nodes are checked even when their parent is incomplete. Fixing a node clears its marker after validation completes; workflow-level errors remain in the workflow banner.

These checks do not execute nodes or call models. Invalid drafts remain saveable; publishing and test execution still require a valid definition. Malformed JSON in a node editor is marked immediately and must be corrected before saving.
