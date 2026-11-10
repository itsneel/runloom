# Projects and workflow folders

Runloom organizes work as **organization → team → project → workflow**. Every workflow belongs to one project in its team. Each team starts with a General project (`default`); there is no unfiled workflow area.

Projects are authoring and testing workspaces, not execution containers or runtime permission boundaries. Published workflows remain executable independently of the project browser. Starting a run requires the workflow definition ID and optional published version, plus the existing authenticated team context; it does not require a project ID or an open Studio session. Project creation, renaming, or moving a workflow does not enable or disable execution.

The Studio's **Test workflow** action runs a snapshot of the current draft, including unsaved builder edits, without publishing. Save a brand-new workflow once to establish its project; subsequent tests neither save nor publish. Tests perform real activities and incur costs under the workflow's project policies. Team permissions and runtime availability still apply. Production starts continue to require published versions. Execution status, history, callbacks, signals, resume, and cancellation remain team/run-scoped rather than project-scoped.

## Using projects in the Studio

1. Select the organization and team in the workspace bar.
2. Select **Projects** in the sidebar to open `/workflows/projects`, then use **New project** to create a folder. Names must be unique within the team, ignoring case, and contain 1–80 characters after trimming whitespace.
3. Search the project table by name or ID and choose **Open project**. Each row shows the project name, ID, workflow count and permitted actions. Its workflow list and search show only that project's workflows. **Back to projects** returns to the directory. The separate **Workflows** also offers a Project selector.
4. Use **New workflow** beside the project name, build the workflow, and save it. New workflows and guided examples receive unique IDs and save into the selected project.
5. Use **Rename** on a project row, or **Rename project** in its workflow dashboard, to rename the folder without changing its ID.
6. Change **Project** above the canvas to move a saved workflow immediately. For an unsaved new workflow, this chooses where the next save will put it. Moving a saved workflow does not save outstanding builder edits.

Navigating from the builder to Projects asks before discarding unsaved edits. Project creation and renaming are available in the directory/dashboard, outside the editor. Failed moves keep the original selection. A workflow move does not rewrite published definitions, add a workflow version, alter callback/webhook URLs, or interrupt a run. Commit restores do not change project membership. The global Management navigation does not move when opening folders or switching pages.

Projects are one level of folders within a team, not nested directories. Workflow IDs remain unique across the team, not just within a project. Duplicate display names across projects are allowed. Cross-team moves and project-specific roles are not provided.

## Page URLs

| Page | URL |
| --- | --- |
| Workflow landing page | `/workflows` |
| Projects directory | `/workflows/projects` |
| Workflows in a project | `/workflows/projects/:projectId` |
| Saved workflow editor | `/workflows/definitions/:workflowId` |

**Open project**, the project selector and workflow **Details** navigate through the Studio router. The Projects directory highlights **Projects**; project workflow lists and editors highlight **Workflows**. Direct links and refresh load the selected resource in the signed-in user's selected team. A missing or inaccessible resource displays an error rather than silently opening General. Workflow URLs do not include the project ID, so moving a saved workflow does not invalidate its editor link.

Browser Back/Forward and navigation between workflow IDs check for unsaved changes before replacing the editor. Canceling preserves the URL and draft; accepting releases the editing session. New, unsaved drafts remain local editor state until persisted and are not recoverable through a shared link. The host owns route definitions; the independently built workflow app uses Angular's shared router and receives the selected IDs as inputs, with no imports from the host source.

## Deleting projects and drafts

Editors and admins can choose **Delete** on an empty project row. A project containing even one workflow, including a saved draft, cannot be deleted; move or delete those workflows first. General (stable ID `default`) remains the permanent default folder even if renamed.

The Workflows dashboard shows **Delete** only for saved workflows that have never been published. Having newer draft edits does not make a published workflow deletable. An Angular confirmation dialog explains the operation; cancellation changes nothing. The backend rechecks eligibility, team permissions and active editing leases, so stale lists cannot bypass these rules.

Workflow deletion removes the current draft and its project membership. Existing commit snapshots, run records and audit history remain; deletion is not a privacy-erasure operation and does not cancel an already-started test run. Drafts referencing a deleted child require correction before testing or publication. An authorized commit restore can recreate the draft. PostgreSQL and MongoDB commit draft removal and membership cleanup together; the single-process file adapter can leave a surviving draft in General if file removal fails.

Within Studio, **Ctrl+S** or **Cmd+S** suppresses the browser's HTML save dialog. In an editable workflow, it invokes the normal draft-save path, with its existing permissions, edit lock and validation behavior. It never publishes. Everywhere else—including management, projects, dashboards, sign-in, read-only workflows and open dialogs—the shortcut does nothing. Suppression runs before focused controls handle the key, so stopping event propagation cannot re-enable the browser download.

## Permissions and persistence

Every request uses `Authorization: Bearer <access-token>` and `X-Runloom-Team: <team-id>`, as described in [authentication](authentication.md). Viewers can list folders and workflows; editors and admins can create/rename projects and move workflows. Organization owners/admins retain their effective team-admin permissions. A project from another team cannot be used as a destination.

Project records and workflow-to-project membership are stored separately from executable workflow JSON. PostgreSQL uses the team's `runloom_entity_records` and revision heads; MongoDB uses its prefixed `metadata_entities` collection. Projects, edit leases, and budget policies have separate revision scopes. File storage uses `_projects/projects.json`, `leases.json`, and `policies.json`. Database updates use transactional compare-and-swap, including duplicate-name validation. The file backend is intended for one API process. Folder creation, rename, and workflow movement produce audit events with the authenticated actor.

Saving the workflow and assigning its project are separate persistence operations. If assignment fails after the draft commit succeeds, the API returns `503` with `draft saved but project assignment failed; retry save`. The Studio keeps the draft marked unsaved so the user can retry; the retry can create another draft commit. Metadata changes and audit writes are also separate operations, consistent with the existing audit API; this is not an exactly-once transaction or audit outbox.

## API

| Action | Method and path | Request | Success |
| --- | --- | --- | --- |
| List projects | `GET /api/v1/studio/projects` | None | `200`, project array |
| Create project | `POST /api/v1/studio/projects` | `{"name":"Finance"}` | `201`, project |
| Rename project | `POST /api/v1/studio/projects/{project_id}/rename` | `{"name":"Operations"}` | `200`, project |
| Delete empty project | `DELETE /api/v1/studio/projects/{project_id}` | None | `204` |
| Delete never-published workflow | `DELETE /api/v1/studio/definitions/{definition_id}` | None; owned `X-Runloom-Edit-Token` when editing | `204` |
| List project workflows | `GET /api/v1/studio/definitions?project_id={project_id}` | None | `200`, definition summaries with `project_id` |
| Move workflow | `POST /api/v1/studio/definitions/{definition_id}/project` | `{"project_id":"destination-id"}` | `200`, `definition_id` and `project_id` |
| Save into project | `POST /api/v1/studio/definitions` | Workflow JSON, plus `X-Runloom-Project: <project-id>` header | `200`, saved definition |

Omitting the project header on save keeps the workflow's current membership, or assigns General for a new workflow. Omitting the list filter returns all workflow summaries in the selected team. General is a real, renamable folder with the stable ID `default`.

Project responses have `id`, `name`, and optional `created_at` / `updated_at` timestamps. Unknown project/workflow destinations return `404`; invalid names return `400`; duplicate names return `409`; unauthorized writes return `403`; unavailable storage returns `503`. Unknown JSON request properties are rejected. The API does not accept an arbitrary filesystem path for a folder.

Deletion returns `409` for nonempty/default projects or workflows with any published version, and `423` for a conflicting or expired workflow edit lease. Repeating a successful deletion returns `404`. Folder and workflow deletion produce `project.deleted` and `definition.deleted` audit events in addition to request audit events. As with other mutations, an audit-completion failure can leave the outcome unknown; refresh before retrying.

## Verification

The Go store/API tests cover folder validation, concurrent duplicate creation, moves, persistence, immutable published snapshots, commit restoration, authenticated audit actors, viewer restrictions, and cross-team boundaries. Set `RUNLOOM_TEST_POSTGRES_URL` and `RUNLOOM_TEST_MONGODB_URL` to disposable databases to run their integration checks.

Verified locally on 2026-10-20: workflow service tests/build, independent host and workflows tests/builds, and `node scripts/test-authoring.mjs` against disposable PostgreSQL and MongoDB databases. The deletion contract includes retained history, published-workflow protection and concurrent publication/deletion.

The opt-in `server/workflows/tests/deletion_browser_test.go` fixture and `studio/projects/workflows/tests/deletion.browser.mjs` exercise real Angular and Go API interactions: cancellation, draft deletion, empty-project deletion, stale publication checks and Ctrl/Cmd+S without downloads. This fixture uses disposable file storage and synthetic identity on loopback, not the deployed authentication service. Run the Go fixture with `RUNLOOM_DELETION_BROWSER=1 go test ./tests -run '^TestDeletionBrowserAPI$' -v -count=1 -timeout 4m` from `server/workflows`, then run the browser script against local host/workflows development servers. The script stops the fixture on completion.

`studio/projects/workflows/tests/workflow-routes.browser.mjs` uses a fresh instance of the same fixture to verify project/editor direct links, refresh, sidebar highlights, parameter changes, guarded Back/Forward navigation, stable URLs after project moves, and missing-resource states. This routing suite and the independent host/workflows tests and builds passed locally on 2026-10-20; no cluster deployment is implied.
