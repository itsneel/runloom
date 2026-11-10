# Versions & publishing

Drafts are editable. Published versions are immutable. A running workflow keeps the snapshot it started with.

## Everyday actions

| Action | Result |
| --- | --- |
| Save | Update the draft and record a commit |
| Test workflow | Run a draft snapshot without publishing |
| Publish | Create the next numbered release |
| Restore a commit | Create a new draft; keep previous history |

## Release safely

1. Save the draft and fix validation errors.
2. Test with safe input. Tests run real activities and may incur costs.
3. Publish when ready for production starts.

Existing runs and schedules do not automatically adopt a new release. Subworkflow references pin a published child version. See [schedules and subworkflows](/guides/schedules-and-subworkflows).
