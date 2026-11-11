# Exclusive workflow editing

1. Saved workflows open as read-only previews. An editor or admin selects **Edit workflow** to reserve that workflow for their browser session. The latest draft is loaded after acquisition, so an old preview cannot overwrite newer edits. Viewers cannot acquire locks.
2. Other team members and other tabs, including tabs belonging to the same person, remain read-only. The banner identifies the current editor. Reading definitions/history and executing published versions remain available according to the user's role.
3. Studio renews the 90-second lease every 20 seconds. **Finish editing**, switching workflows/teams, and signing out release it. Closing a browser or losing its connection can leave the lease until expiry. The UI stops permitting edits before its local renewal deadline and blocks saving on renewal failure; unsaved content remains available through the read-only JSON panel. Copy it before choosing **Edit workflow** again, which confirms discard and reloads the latest draft. Locks are not force-stealable, including by administrators.
4. Lumi uses this canvas session’s lease for requested draft changes. You can keep editing reserved while asking Lumi for an update. Studio saves valid pending edits before reading model context; invalid JSON remains local. Other sessions’ locks, including locks on referenced draft children, still block Lumi. After saving, Studio reloads the clean draft and attempts to resume the editing reservation.
5. New unsaved workflows remain local. First save acquires a lease and rejects an ID already present on the server. IDs of saved workflows cannot be changed in the editor or through its JSON panel; create a new workflow to import a copy.

## Autosave, undo and keyboard shortcuts

Implemented source behavior verified on 2026-11-09 with the workflows application's automated tests, production build and a browser check using mocked APIs. This does not establish deployment or live backend integration verification.

1. For a new workflow, select **Save** once to establish its ID and acquire editing access. Before that first save, changes remain local and the header says **Save once to enable autosave**. For an existing workflow, select **Edit workflow** first.
2. After the first save, draft edits autosave one second after editing pauses. The header shows **Unsaved changes**, **Saving…** or **Saved**. Autosaves use the existing draft-save API and add **Autosave draft** commits to **History**; they never publish an immutable version or start a run. Rapid typing is debounced, but frequent editing pauses can still create multiple history commits.
3. Unapplied workflow JSON and invalid node input/schema JSON pause autosave. Apply or discard the workflow JSON and fix invalid node JSON to continue. Invalid workflow structure can still be saved as a draft; validation errors block publishing and testing. Other open builder panels/dialogs and active writes delay autosave. Losing editing access prevents further writes and preserves the existing unsaved-change protection.
4. If saving fails, the header shows **Save failed · changes kept locally**. Select **Retry save**, use **Save**, or make another edit to attempt saving again. There is no continuous automatic retry after a failed write. Local unsaved changes and undo history are in memory, not a durable browser backup: preserve the JSON before closing or reloading after an error. A rejected lease must be recovered through the existing editing flow.
5. Select **Undo** or **Redo** to reverse or reapply workflow content edits, including node configuration, graph structure, applied workflow JSON and saved node positions. A drag is recorded when it finishes; successive typing in the same field is grouped while the gap between changes is under 750 milliseconds. Undo/redo also triggers autosave. Pan, zoom, project moves, publishing, test executions and external effects are not reversed by workflow undo.
6. History retains up to 100 edit actions in the current editing session. Opening/reloading a workflow, acquiring a fresh editing lease, opening a published version, restoring a commit or completing the first save establishes a new baseline. Making a new edit after undo discards the redo branch. History is not shared between browsers or preserved across refresh. To recover a saved server snapshot, use **History** and restore a commit as a new draft.
7. Select **Keyboard shortcuts** in the builder header to open the shortcut guide. Focus starts on **Close**, Tab stays in the native modal dialog, and Escape closes it and returns focus to the opening button. The guide includes canvas navigation shortcuts as well as the actions below.

| Action | Windows / Linux | macOS |
| --- | --- | --- |
| Undo workflow edit | Ctrl + Z | Command + Z |
| Redo workflow edit | Ctrl + Shift + Z | Command + Shift + Z |
| Save draft immediately | Ctrl + S | Command + S |

In text inputs, textareas, editable content and code editors, undo/redo retain their native text-editing behavior. Workflow undo and redo do not run behind an open dialog, in read-only mode, during a save, or while node JSON is invalid. **Save** remains subject to editing access and unfinished-JSON checks. The shortcut guide and toolbar buttons provide discoverable alternatives to memorizing shortcuts.

With a workflow open and focus outside text fields and dialogs, press **E** to start editing, **Shift + P** to review and confirm publication, **A** to search for a node, or **V** to check the draft. Press **T** to review test inputs, **H** to open History, or **?** to open the keyboard guide. These keys follow the corresponding button’s availability. Opening test inputs leaves execution under your control.

## Visual graph and parallel paths

The canvas renders the structured workflow as connected paths, including nested Parallel and Decision nodes. [Independent signal handlers](signal-handlers.md) appear as separate entry paths below the main flow; configure them in **Signal handler paths** and add actions using their own connection buttons. Select **Edit workflow** on a saved workflow before changing its graph. Drag a node anywhere to change its saved canvas position; its connections follow it. Coordinates are authoring metadata, never a source of execution order.

1. Select **Add node**, then **Parallel**. Its paths appear as separate lanes, followed by **Join · wait for all paths**. Execution continues after all paths finish.
2. Select the Parallel node to rename path IDs, add paths, or remove paths. At least two paths must remain. Removing a populated path requires confirmation. Every parallel path needs at least one node before validation or testing.
3. Use a path's **+** connection button to insert a node there. Select any nested node to edit its settings. Changing execution order is explicit: use the node's **Change execution path** action, then choose a **+** destination, or use **Move earlier** and **Move later** in settings. A node cannot move into its own descendants. Ordinary dragging only changes its visual position.
4. Decision settings define the value, comparison and expected value. Yes and No routes are editable on the canvas; the Yes route must contain a node, while an empty No route continues immediately. A nested Decision can be placed inside a parallel path.
5. Place End last in a path and never inside a parallel path, including nested decisions there. Put End after the parallel join to complete the workflow. **Check** validates the full graph against server limits, including nesting depth and node count. This is a structured split/join graph, not an arbitrary cyclic graph: loops, backward edges and cross-branch dependencies are not supported.
6. Saving persists the same nested workflow JSON. Graph edits trigger the existing unsaved-change protections. Duplicate regenerates nested node IDs and references within the copied subtree; review references to other paths before testing. Tests use draft snapshots and perform real activities without publishing.

Drag blank canvas space to pan, or use two-finger scrolling. Hold Space and drag to pan over nodes; middle-button dragging also pans. Use **+**/**−**, Ctrl/Command + wheel, or trackpad pinch for pointer-centred zoom (15–250%). Click the percentage for 100%, or the target icon for **Fit to view**. **Auto arrange** resets saved positions to the generated layout and requires editing permission. Focus a node and use Alt + arrow keys to nudge it; add Shift for larger steps. Escape cancels a drag or pending path move. Viewers can pan and zoom but cannot move or rearrange saved nodes.

Positions are saved with the draft and included in commits and published snapshots. Pan and zoom are temporary viewing state and do not create unsaved changes. Existing workflows without coordinates are laid out automatically. Lumi-created workflows and changes to graph structure automatically arrange the whole graph, including branches and joins; settings-only changes keep manual positions. Partial older layouts that overlap newly inserted nodes render with a clean automatic arrangement. **Auto arrange** saves positions for every displayed node, and the canvas fits structural changes into view. Deploy the updated API before the updated workflows application because older APIs reject the new optional `layout` field. No separate database table or migration is required.

The graph component keeps its Angular logic, HTML and SCSS in separate files inside the independent workflows application. Run `npm test` and `npm run build` there. `scripts/graph-builder-smoke.mjs` uses a development Studio, a synthetic session and mocked APIs to verify zoom, panning, free positioning, connectors, unchanged execution order, branch editing, duplication, save/reload and read-only controls. It does not prove a deployed runtime is updated.

## Studio confirmation dialogs

Studio uses a shared Angular modal for unsaved changes, project creation/rename, draft restoration, schedule deletion, member removal, and secret replacement, deletion, rotation or revocation. Destructive actions initially focus Cancel; Escape cancels, Tab stays inside the modal, and closing returns focus to the triggering control. Project names are validated inline. A pending decision is canceled when its owning screen is destroyed or its management team changes, and additional confirmations are not queued.

Browser refresh, tab/window close, and external navigation still use the browser-required `beforeunload` warning when there are unsaved changes or visible one-time credentials. Browsers do not permit custom Angular dialogs to replace that warning.

Leaving management, changing organization/team, or signing out also checks for a visible one-time service-account secret using an Angular dialog. Cancel keeps the secret visible. Explicitly confirmed sign-out suppresses the duplicate browser warning for unchanged state; a failed sign-out preserves the values and re-arms protection. Unsaved workflow-secret values follow the same sign-out behavior.

Run `make test-ui-browser test-ui-api-nodes` against an Angular development server for confirmation and cancellation regressions. These tests mock API calls; `make test-edit-locks` separately checks live multi-user editing.

## API contract

Use the existing verified-human Studio bearer token and `X-Runloom-Team` header. Editing does not grant extra team permissions, and consumer execution credentials are not accepted here.

| Method | Path | Behavior |
| --- | --- | --- |
| GET | `/api/v1/studio/definitions/{id}/edit-lock` | Read owner, expiry, and write-in-progress status; never returns the session token |
| POST | `/api/v1/studio/definitions/{id}/edit-lock` | Acquire a lease using a fresh random UUID in `X-Runloom-Edit-Token` |
| PUT | `/api/v1/studio/definitions/{id}/edit-lock` | Renew the same user's unexpired lease with the same token |
| DELETE | `/api/v1/studio/definitions/{id}/edit-lock` | Release that user's session when no write is in progress |

Send the same `X-Runloom-Edit-Token` on save, publish, restore, and workflow project-move requests. A missing, wrong-owner, stale, or competing session is rejected with HTTP **423 Locked** when a lease is active; the response includes a safe `lock` status. Legacy callers without an edit token can still mutate an unlocked workflow, but get a single-operation reservation and cannot bypass an active editor. Successful acquisition is not an optimistic version check: API clients must load the latest draft after acquiring, as Studio does.

## Storage, concurrency, and recovery

Locks are team-scoped metadata in the existing project catalog. Compare-and-swap coordinates MongoDB and PostgreSQL API replicas. File storage supports a single API process. The entire mutation is reserved, including validation, commit/version creation, project assignment, and auditing; concurrent requests cannot pass a check then overwrite each other. Running workflows and immutable published definitions are not changed by acquiring or releasing locks. Heartbeats do not create audits; acquisition and release do.

An in-progress write reservation deliberately does **not** expire. Otherwise a delayed process could finish a write after a replacement editor acquired the workflow. If an API process crashes during a mutation, or cannot clear its reservation after completion, that workflow fails closed with `writing: true` and requires operator recovery. There is no automatic takeover or force-unlock API. Stop all API writers, back up and inspect the affected workflow, commit history and project catalog, reconcile any partial write, then clear only its `edit_locks[definition_id]` record before restarting API writers. Never clear a live write reservation. Existing MongoDB/file multi-record commit limitations still apply; this feature serializes writers rather than making those storage operations transactional.

Test with `go test -race ./internal/store ./internal/api` from `server/`. Set `RUNLOOM_TEST_POSTGRES_URL` and `RUNLOOM_TEST_MONGODB_URL` to local test databases to exercise database-backed locks. `scripts/edit-locks-smoke.mjs` creates disposable synthetic local users, a test organization, and a workflow to exercise two members/tabs and recovery without logging their credentials.
