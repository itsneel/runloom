# Workflow builder

Build a workflow on the canvas. Saving keeps your draft; publishing releases a validated version.

## Edit a workflow

1. Open it and select **Edit workflow**. Editors and admins can edit; other sessions stay read-only.
2. Add nodes and configure their inputs. Dragging changes layout, not execution order.
3. For concurrent work, add **Parallel**, then add nodes to each path. Execution continues after all paths join.
4. Save, check validation errors, and use **Test workflow** before publishing.

## Know the limits

Invalid drafts can be saved, but not published or run. Malformed JSON must be corrected first. Tests execute real integrations and may incur costs.

If the editing connection fails, preserve your unsaved JSON before reacquiring the lock. Reopening for editing reloads the saved draft.

The canvas supports structured branches, not arbitrary loops or backward connections. See [node configuration](/nodes/reference).
