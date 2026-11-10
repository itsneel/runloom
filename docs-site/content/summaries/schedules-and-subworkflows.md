# Schedules & subworkflows

Schedule a published workflow, or reuse a child workflow inside a parent.

## Schedule a workflow

1. Publish the main workflow.
2. Open **Schedules** in Management.
3. Choose the workflow/version, input, timing, time zone and overlap policy.
4. Review before activating. Pause the schedule to prevent future starts.

A schedule pins its workflow and child versions. Publishing again does not update it; recreate the schedule to use newer versions.

## Call a child workflow

1. Create a workflow with child kind in the same team, then publish it.
2. Add a **Subworkflow** node to the parent.
3. Choose the child version and map its input/output.
4. Test the parent before publishing.

Parents wait for their children. Failures propagate; parent cancellation closes active children.

Children cannot start independently through production APIs, webhooks or schedules. Authorized Studio editors/admins can test child drafts independently. Tests perform real work and may incur charges.
