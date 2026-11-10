# Your first workflow

Create a small workflow that completes without contacting an external service. You need a running installation and editor or administrator access to a team.

## Create a project

1. Sign in to Studio using an organization account, not a platform-only account.
2. Select your organization and team.
3. Open **Projects**, select **New project** and give it a name such as `Learning`.
4. Choose **Open project**, then **New workflow**.

## Build a small draft

Give the workflow a clear name. Use a manual start and add an **End workflow** node, with a message such as `First workflow complete`. The End node explicitly ends execution; ordinary workflows also finish after their final step.

Keep the first example free of email, API, database and model calls. You can add those later after configuring their connections and understanding their side effects.

## Save and test

Select **Save** once to establish the new workflow in its project. Select **Test workflow** and provide an empty JSON object as input:

```json
{}
```

Review the live execution panel for the completed result. You do **not** need to publish first. Subsequent draft tests use the current builder snapshot, including unsaved edits, without saving or publishing it.

An invalid draft can be saved, but it cannot be published or successfully executed. Correct the highlighted node errors before continuing.

## Publish when ready

Review the saved draft and select **Publish**. Publishing creates a numbered immutable version for production starts. A newer draft does not change that published version or an already-running workflow.

Draft tests are real runs. When you add integrations later, tests can write data, send email or incur model costs. Publishing is not a safety switch for external side effects.

## Next steps

Learn [expressions and node configuration](/nodes/reference), [connect a database](/integrations/connections), or [call a child workflow](/guides/schedules-and-subworkflows). Read [Versions & publishing](/guides/versioning) before restoring or releasing more complex changes.
