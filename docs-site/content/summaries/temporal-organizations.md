# Temporal namespaces

Each organization uses its own Temporal namespace. Runloom provisions namespaces and required search attributes automatically when configured with the necessary permissions.

## Find a run

1. Open run history for an authorized team.
2. Search run/workflow IDs or tag values.
3. Select a tag and exact value to narrow results.
4. Load older matches when needed.

Tags identify context such as organization, team, workflow version and test status. Search does not inspect workflow inputs or outputs.

## Operator notes

Workers discover provisioned namespaces without a rollout. Provisioning failures block starts rather than falling back to a shared default namespace.

Keep environment prefixes distinct and grant the documented Temporal permissions. A namespace adds execution isolation; application authorization still applies. Expand the reference for attribute names, retention and readiness rules.
